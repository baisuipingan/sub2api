//go:build unit

package handler

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/config"
	middleware2 "github.com/Wei-Shaw/sub2api/internal/server/middleware"
	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

type toggleSettingRepo struct {
	mu     sync.Mutex
	values map[string]string
}

func (r *toggleSettingRepo) Get(context.Context, string) (*service.Setting, error) { return nil, nil }
func (r *toggleSettingRepo) GetValue(_ context.Context, key string) (string, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.values[key], nil
}

func (r *toggleSettingRepo) Set(_ context.Context, key, value string) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.values[key] = value
	return nil
}

func (r *toggleSettingRepo) GetMultiple(context.Context, []string) (map[string]string, error) {
	return map[string]string{}, nil
}
func (r *toggleSettingRepo) SetMultiple(context.Context, map[string]string) error { return nil }
func (r *toggleSettingRepo) GetAll(context.Context) (map[string]string, error) {
	return map[string]string{}, nil
}
func (r *toggleSettingRepo) Delete(context.Context, string) error { return nil }

type passthroughEncryptor struct{}

func (passthroughEncryptor) Encrypt(plaintext string) (string, error)  { return plaintext, nil }
func (passthroughEncryptor) Decrypt(ciphertext string) (string, error) { return ciphertext, nil }

type noopImageStorage struct{}

func (noopImageStorage) Save(context.Context, string, string, []byte) (string, error) {
	return "https://cdn.example.test/object.png", nil
}

type capturedAsyncImageStorage struct {
	mu    sync.Mutex
	keys  []string
	bytes [][]byte
}

type blockingContextImageStorage struct{ started chan struct{} }

func (s *blockingContextImageStorage) Save(ctx context.Context, _, _ string, _ []byte) (string, error) {
	close(s.started)
	<-ctx.Done()
	return "", ctx.Err()
}

type contextCheckingAsyncTaskStore struct{ *asyncImageMemoryStore }

func (s *contextCheckingAsyncTaskStore) Save(ctx context.Context, task *service.ImageTaskRecord, ttl time.Duration) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	return s.asyncImageMemoryStore.Save(ctx, task, ttl)
}

func (s *contextCheckingAsyncTaskStore) Get(ctx context.Context, id string) (*service.ImageTaskRecord, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	return s.asyncImageMemoryStore.Get(ctx, id)
}

func (s *capturedAsyncImageStorage) Save(_ context.Context, key, _ string, data []byte) (string, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.keys = append(s.keys, key)
	s.bytes = append(s.bytes, append([]byte(nil), data...))
	return "https://cdn.example.test/" + key, nil
}

// TestAsyncImageEnablesWithoutRestart drives the actual HTTP path for the bug behind
// #4458 and #4542: with object storage unconfigured the async endpoint 404s, and the
// only way to turn it on used to be editing config.yaml and restarting the container.
// Flipping the admin setting must flip the endpoint over in the same process.
func TestAsyncImageEnablesWithoutRestart(t *testing.T) {
	gin.SetMode(gin.TestMode)

	repo := &toggleSettingRepo{values: map[string]string{}}
	// A fixed encryption key is required to persist a new S3 secret (#4524).
	backup := service.NewBackupService(repo, &config.Config{
		Totp: config.TotpConfig{EncryptionKeyConfigured: true},
	}, passthroughEncryptor{}, nil, nil)
	factory := func(context.Context, *config.ImageStorageConfig) (service.ImageStorage, error) {
		return noopImageStorage{}, nil
	}
	settings := service.NewImageStorageSettingService(repo, passthroughEncryptor{}, backup, factory, config.ImageStorageConfig{})

	store := &asyncImageMemoryStore{tasks: make(map[string]*service.ImageTaskRecord)}
	tasks := service.NewImageTaskServiceWithResolver(store, settings.Resolver(), time.Hour, time.Minute)

	h := &AsyncImageHandler{tasks: tasks}
	h.execute = func(_ string, c *gin.Context) {
		c.JSON(http.StatusOK, gin.H{"created": 1, "data": []gin.H{{"url": "https://upstream.test/i.png"}}})
	}

	router := gin.New()
	router.Use(func(c *gin.Context) {
		groupID := int64(3)
		c.Set(string(middleware2.ContextKeyAPIKey), &service.APIKey{
			ID: 9, UserID: 7, GroupID: &groupID,
			Group: &service.Group{ID: groupID, Platform: service.PlatformOpenAI, AllowImageGeneration: true},
		})
		c.Next()
	})
	router.POST("/v1/images/generations/async", h.Submit)
	router.GET("/v1/images/tasks/:task_id", h.Get)

	submit := func() *httptest.ResponseRecorder {
		req := httptest.NewRequest(http.MethodPost, "/v1/images/generations/async",
			strings.NewReader(`{"model":"gpt-image-1","prompt":"a lighthouse"}`))
		req.Header.Set("Content-Type", "application/json")
		rec := httptest.NewRecorder()
		router.ServeHTTP(rec, req)
		return rec
	}

	rec := submit()
	require.Equal(t, http.StatusNotFound, rec.Code, "disabled until an admin configures object storage")
	require.Contains(t, rec.Body.String(), "async image tasks are not enabled")

	// The admin saves the setting — no restart, same process.
	_, err := settings.Update(context.Background(), service.ImageStorageSettings{
		Enabled: true, Bucket: "my-images",
		Endpoint: "https://acct.r2.cloudflarestorage.com", AccessKeyID: "ak", SecretAccessKey: "sk",
	})
	require.NoError(t, err)

	rec = submit()
	require.Equal(t, http.StatusAccepted, rec.Code, "the endpoint must go live as soon as the setting is saved")

	var accepted struct {
		TaskID  string `json:"task_id"`
		PollURL string `json:"poll_url"`
	}
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &accepted))
	require.NotEmpty(t, accepted.TaskID)

	// Turning the feature back off must not strand a task that was already accepted.
	_, err = settings.Update(context.Background(), service.ImageStorageSettings{
		Enabled: false, Bucket: "my-images", Endpoint: "https://acct.r2.cloudflarestorage.com", AccessKeyID: "ak",
	})
	require.NoError(t, err)

	require.Equal(t, http.StatusNotFound, submit().Code, "new submissions are refused again")

	pollRec := httptest.NewRecorder()
	router.ServeHTTP(pollRec, httptest.NewRequest(http.MethodGet, accepted.PollURL, nil))
	require.Equal(t, http.StatusOK, pollRec.Code, "an already-accepted task stays pollable after the switch is turned off")
}

func TestAsyncImageOffloadsAfterAdminDisablesStorage(t *testing.T) {
	gin.SetMode(gin.TestMode)
	repo := &toggleSettingRepo{values: map[string]string{}}
	backup := service.NewBackupService(repo, &config.Config{
		Totp: config.TotpConfig{EncryptionKeyConfigured: true},
	}, passthroughEncryptor{}, nil, nil)
	storage := &capturedAsyncImageStorage{}
	settings := service.NewImageStorageSettingService(repo, passthroughEncryptor{}, backup,
		func(context.Context, *config.ImageStorageConfig) (service.ImageStorage, error) { return storage, nil },
		config.ImageStorageConfig{})
	_, err := settings.Update(context.Background(), service.ImageStorageSettings{
		Enabled: true, Bucket: "original-images", Endpoint: "https://s3.example.test",
		Region: "auto", AccessKeyID: "ak", SecretAccessKey: "sk",
	})
	require.NoError(t, err)

	store := &asyncImageMemoryStore{tasks: make(map[string]*service.ImageTaskRecord)}
	tasks := service.NewImageTaskServiceWithResolver(store, settings.Resolver(), time.Hour, time.Minute)
	started := make(chan struct{})
	release := make(chan struct{})
	var releaseOnce sync.Once
	t.Cleanup(func() { releaseOnce.Do(func() { close(release) }) })
	imageBytes := []byte("image bytes returned by one upstream call")
	encoded := base64.StdEncoding.EncodeToString(imageBytes)
	h := &AsyncImageHandler{tasks: tasks}
	h.execute = func(_ string, c *gin.Context) {
		close(started)
		<-release
		c.JSON(http.StatusOK, gin.H{"created": 1, "data": []gin.H{{"b64_json": encoded}}})
	}
	router := gin.New()
	router.Use(func(c *gin.Context) {
		groupID := int64(3)
		c.Set(string(middleware2.ContextKeyAPIKey), &service.APIKey{
			ID: 9, UserID: 7, GroupID: &groupID,
			Group: &service.Group{ID: groupID, Platform: service.PlatformOpenAI, AllowImageGeneration: true},
		})
		c.Next()
	})
	router.POST("/v1/images/generations/async", h.Submit)
	router.GET("/v1/images/tasks/:task_id", h.Get)
	submit := func() *httptest.ResponseRecorder {
		req := httptest.NewRequest(http.MethodPost, "/v1/images/generations/async",
			strings.NewReader("{\"model\":\"gpt-image-1\",\"prompt\":\"cat\"}"))
		req.Header.Set("Content-Type", "application/json")
		rec := httptest.NewRecorder()
		router.ServeHTTP(rec, req)
		return rec
	}

	rec := submit()
	require.Equal(t, http.StatusAccepted, rec.Code)
	var accepted map[string]any
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &accepted))
	taskID, ok := accepted["task_id"].(string)
	require.True(t, ok)
	pollURL, ok := accepted["poll_url"].(string)
	require.True(t, ok)
	select {
	case <-started:
	case <-time.After(time.Second):
		t.Fatal("accepted task did not start")
	}
	_, err = settings.Update(context.Background(), service.ImageStorageSettings{
		Enabled: true, Bucket: "different-images", Endpoint: "https://s3.example.test",
		Region: "auto", AccessKeyID: "ak",
	})
	require.ErrorIs(t, err, service.ErrAsyncImageStorageTargetInUse)
	_, err = settings.Update(context.Background(), service.ImageStorageSettings{
		Enabled: false, Bucket: "original-images", Endpoint: "https://s3.example.test",
		Region: "auto", AccessKeyID: "ak",
	})
	require.NoError(t, err)
	require.Equal(t, http.StatusNotFound, submit().Code)

	releaseOnce.Do(func() { close(release) })
	owner := service.ImageTaskOwner{UserID: 7, APIKeyID: 9}
	require.Eventually(t, func() bool {
		got, err := tasks.Get(context.Background(), owner, taskID)
		return err == nil && got.Status == service.ImageTaskStatusCompleted
	}, 3*time.Second, 10*time.Millisecond)
	stored, err := store.Get(context.Background(), taskID)
	require.NoError(t, err)
	require.NotContains(t, string(stored.Result), "b64_json")
	require.NotContains(t, string(stored.Result), encoded)
	require.Contains(t, string(stored.Result), "https://cdn.example.test/")
	storage.mu.Lock()
	require.Len(t, storage.bytes, 1)
	require.Equal(t, imageBytes, storage.bytes[0])
	storage.mu.Unlock()
	poll := httptest.NewRecorder()
	router.ServeHTTP(poll, httptest.NewRequest(http.MethodGet, pollURL, nil))
	require.Equal(t, http.StatusOK, poll.Code)
	require.NotContains(t, poll.Body.String(), encoded)
}

func TestAsyncImageBlockedOffloadTimesOutAndReleasesTarget(t *testing.T) {
	gin.SetMode(gin.TestMode)
	repo := &toggleSettingRepo{values: map[string]string{}}
	backup := service.NewBackupService(repo, &config.Config{
		Totp: config.TotpConfig{EncryptionKeyConfigured: true},
	}, passthroughEncryptor{}, nil, nil)
	storage := &blockingContextImageStorage{started: make(chan struct{})}
	settings := service.NewImageStorageSettingService(repo, passthroughEncryptor{}, backup,
		func(context.Context, *config.ImageStorageConfig) (service.ImageStorage, error) { return storage, nil },
		config.ImageStorageConfig{})
	_, err := settings.Update(context.Background(), service.ImageStorageSettings{
		Enabled: true, Bucket: "original-images", Endpoint: "https://s3.example.test",
		Region: "auto", AccessKeyID: "ak", SecretAccessKey: "sk",
	})
	require.NoError(t, err)
	store := &contextCheckingAsyncTaskStore{&asyncImageMemoryStore{tasks: make(map[string]*service.ImageTaskRecord)}}
	tasks := service.NewImageTaskServiceWithResolver(store, settings.Resolver(), time.Hour, time.Minute)
	h := &AsyncImageHandler{tasks: tasks, offloadTimeout: 40 * time.Millisecond}
	encoded := base64.StdEncoding.EncodeToString([]byte("already generated image bytes"))
	h.execute = func(_ string, c *gin.Context) {
		c.JSON(http.StatusOK, gin.H{"data": []gin.H{{"b64_json": encoded}}})
	}
	router := gin.New()
	router.Use(func(c *gin.Context) {
		groupID := int64(3)
		c.Set(string(middleware2.ContextKeyAPIKey), &service.APIKey{
			ID: 9, UserID: 7, GroupID: &groupID,
			Group: &service.Group{ID: groupID, Platform: service.PlatformOpenAI, AllowImageGeneration: true},
		})
		c.Next()
	})
	router.POST("/v1/images/generations/async", h.Submit)
	request := httptest.NewRequest(http.MethodPost, "/v1/images/generations/async", strings.NewReader("{\"model\":\"gpt-image-1\",\"prompt\":\"cat\"}"))
	request.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, request)
	require.Equal(t, http.StatusAccepted, rec.Code)
	var accepted struct {
		TaskID string `json:"task_id"`
	}
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &accepted))
	require.NotEmpty(t, accepted.TaskID)
	select {
	case <-storage.started:
	case <-time.After(time.Second):
		t.Fatal("object storage was not reached")
	}
	owner := service.ImageTaskOwner{UserID: 7, APIKeyID: 9}
	require.Eventually(t, func() bool {
		job, getErr := tasks.Get(context.Background(), owner, accepted.TaskID)
		return getErr == nil && job.Status == service.ImageTaskStatusFailed && job.HTTPStatus == http.StatusGatewayTimeout &&
			strings.Contains(string(job.Error), "timeout_error") && len(job.Result) == 0
	}, time.Second, 10*time.Millisecond)
	require.Eventually(t, func() bool {
		_, updateErr := settings.Update(context.Background(), service.ImageStorageSettings{
			Enabled: true, Bucket: "next-images", Endpoint: "https://s3.example.test",
			Region: "auto", AccessKeyID: "ak",
		})
		return updateErr == nil
	}, time.Second, 10*time.Millisecond, "timeout must release the pinned storage target")
	job, err := tasks.Get(context.Background(), owner, accepted.TaskID)
	require.NoError(t, err)
	require.NotContains(t, string(job.Error), encoded)
}
