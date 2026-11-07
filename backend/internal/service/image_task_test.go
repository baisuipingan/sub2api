package service

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

type imageTaskMemoryStore struct {
	task    *ImageTaskRecord
	ttl     time.Duration
	saveErr error
	getErr  error
}

func (s *imageTaskMemoryStore) Save(_ context.Context, task *ImageTaskRecord, ttl time.Duration) error {
	if s.saveErr != nil {
		return s.saveErr
	}
	copy := *task
	s.task = &copy
	s.ttl = ttl
	return nil
}

func (s *imageTaskMemoryStore) Get(_ context.Context, _ string) (*ImageTaskRecord, error) {
	if s.getErr != nil {
		return nil, s.getErr
	}
	if s.task == nil {
		return nil, ErrImageTaskNotFound
	}
	copy := *s.task
	return &copy, nil
}

func TestImageTaskServiceLifecycleAndOwnership(t *testing.T) {
	store := &imageTaskMemoryStore{}
	svc := NewImageTaskServiceWithOptions(store, time.Hour, 10*time.Minute)
	owner := ImageTaskOwner{UserID: 7, APIKeyID: 9}

	created, err := svc.Create(context.Background(), owner)
	require.NoError(t, err)
	require.Equal(t, ImageTaskStatusProcessing, created.Status)
	require.Equal(t, created.ID, created.TaskID)
	require.Equal(t, "image.generation.task", created.Object)
	require.Equal(t, time.Hour, store.ttl)
	require.Equal(t, owner.UserID, store.task.UserID)
	require.Equal(t, owner.APIKeyID, store.task.APIKeyID)

	_, err = svc.Get(context.Background(), ImageTaskOwner{UserID: 7, APIKeyID: 10}, created.ID)
	require.ErrorIs(t, err, ErrImageTaskNotFound)

	result := json.RawMessage(`{"created":123,"data":[{"url":"https://example.test/image.png"}]}`)
	require.NoError(t, svc.Complete(context.Background(), created.ID, http.StatusOK, result))

	completed, err := svc.Get(context.Background(), owner, created.ID)
	require.NoError(t, err)
	require.Equal(t, ImageTaskStatusCompleted, completed.Status)
	require.Equal(t, http.StatusOK, completed.HTTPStatus)
	require.Equal(t, "https://example.test/image.png", completed.ImageURL)
	require.JSONEq(t, string(result), string(completed.Result))
	require.NotNil(t, completed.CompletedAt)
}

func TestImageTaskServiceInvalidResultBecomesFailed(t *testing.T) {
	store := &imageTaskMemoryStore{}
	svc := NewImageTaskServiceWithOptions(store, time.Hour, time.Minute)
	created, err := svc.Create(context.Background(), ImageTaskOwner{UserID: 1, APIKeyID: 2})
	require.NoError(t, err)

	require.NoError(t, svc.Complete(context.Background(), created.ID, http.StatusOK, json.RawMessage(`not-json`)))
	got, err := svc.Get(context.Background(), ImageTaskOwner{UserID: 1, APIKeyID: 2}, created.ID)
	require.NoError(t, err)
	require.Equal(t, ImageTaskStatusFailed, got.Status)
	require.Equal(t, http.StatusBadGateway, got.HTTPStatus)
	require.Contains(t, string(got.Error), "non-JSON")
}

func TestImageTaskServiceMapsStoreFailures(t *testing.T) {
	store := &imageTaskMemoryStore{saveErr: errors.New("redis down")}
	svc := NewImageTaskService(store)

	_, err := svc.Create(context.Background(), ImageTaskOwner{UserID: 1, APIKeyID: 2})
	require.ErrorIs(t, err, ErrImageTaskUnavailable)
}

func TestImageTaskServiceRejectsInlineMediaWithoutUploader(t *testing.T) {
	store := &imageTaskMemoryStore{}
	svc := NewImageTaskServiceWithUploader(store, nil, time.Hour, time.Minute)
	owner := ImageTaskOwner{UserID: 1, APIKeyID: 2}
	task, err := svc.Create(context.Background(), owner)
	require.NoError(t, err)

	media := base64.StdEncoding.EncodeToString([]byte("private image bytes"))
	result, err := json.Marshal(map[string]any{"data": []map[string]string{{"b64_json": media}}})
	require.NoError(t, err)
	require.NoError(t, svc.Complete(context.Background(), task.ID, http.StatusOK, result))

	got, err := svc.Get(context.Background(), owner, task.ID)
	require.NoError(t, err)
	require.Equal(t, ImageTaskStatusFailed, got.Status)
	require.Empty(t, got.Result)
	require.NotContains(t, string(store.task.Error), media)
}

func TestImageTaskServiceRejectsOversizedResultAndError(t *testing.T) {
	store := &imageTaskMemoryStore{}
	svc := NewImageTaskServiceWithOptions(store, time.Hour, time.Minute)
	owner := ImageTaskOwner{UserID: 1, APIKeyID: 2}
	task, err := svc.Create(context.Background(), owner)
	require.NoError(t, err)
	result, err := json.Marshal(map[string]any{
		"data":           []map[string]string{{"url": "https://cdn.example.test/image.png"}},
		"revised_prompt": strings.Repeat("x", maxImageTaskResultBytes),
	})
	require.NoError(t, err)
	require.NoError(t, svc.Complete(context.Background(), task.ID, http.StatusOK, result))
	require.Equal(t, ImageTaskStatusFailed, store.task.Status)
	require.Empty(t, store.task.Result)
	require.Less(t, len(store.task.Error), maxImageTaskErrorBytes)

	task, err = svc.Create(context.Background(), owner)
	require.NoError(t, err)
	largeError, err := json.Marshal(map[string]string{"message": strings.Repeat("x", maxImageTaskErrorBytes)})
	require.NoError(t, err)
	require.NoError(t, svc.Fail(context.Background(), task.ID, http.StatusBadGateway, largeError))
	require.Less(t, len(store.task.Error), maxImageTaskErrorBytes)
	require.NotContains(t, string(store.task.Error), strings.Repeat("x", 50))
}

func TestImageTaskServiceLostRuntimeBindingFailsClosed(t *testing.T) {
	store := &imageTaskMemoryStore{}
	storage := NewImageResultUploader(&fakeImageStorage{}, "images/", 0, nil)
	resolver := func() (*ImageResultUploader, func(), bool) { return storage, nil, true }
	creator := NewImageTaskServiceWithResolver(store, resolver, time.Hour, time.Minute)
	owner := ImageTaskOwner{UserID: 1, APIKeyID: 2}
	task, err := creator.Create(context.Background(), owner)
	require.NoError(t, err)

	// A replacement service has no in-memory binding. It must not silently use
	// the current target or put an upstream image into Redis.
	replacement := NewImageTaskServiceWithResolver(store, resolver, time.Hour, time.Minute)
	media := base64.StdEncoding.EncodeToString([]byte("private image bytes"))
	result, err := json.Marshal(map[string]any{"data": []map[string]string{{"b64_json": media}}})
	require.NoError(t, err)
	require.NoError(t, replacement.Complete(context.Background(), task.ID, http.StatusOK, result))
	require.Equal(t, ImageTaskStatusFailed, store.task.Status)
	require.Empty(t, store.task.Result)
	require.NotContains(t, string(store.task.Error), media)
}

func TestImageTaskResultSafetyRejectsEmptyAndHiddenInlineImages(t *testing.T) {
	valid := map[string]any{"data": []any{map[string]any{"url": "https://cdn.example.test/a.png"}}}
	for _, result := range []map[string]any{
		{"data": []any{}},
		{"data": valid["data"], "extra": map[string]any{"b64_json": "aGVsbG8="}},
		{"data": valid["data"], "extra": "data:image/png;base64,aGVsbG8="},
		{"data": []any{map[string]any{"url": "data:image/png;base64,aGVsbG8="}}},
	} {
		encoded, err := json.Marshal(result)
		require.NoError(t, err)
		require.False(t, safeImageTaskResult(encoded), string(encoded))
	}
	encoded, err := json.Marshal(valid)
	require.NoError(t, err)
	require.True(t, safeImageTaskResult(encoded))
	require.True(t, unsafeImageTaskError(json.RawMessage("{\"message\":\"data\\u003aimage/png;base64,aGVsbG8=\"}")))
}

type transientImageTaskStorage struct {
	attempts int
	failAt   int
	keys     []string
}

func (s *transientImageTaskStorage) Save(_ context.Context, key, _ string, _ []byte) (string, error) {
	s.attempts++
	s.keys = append(s.keys, key)
	if s.attempts == s.failAt {
		return "", errors.New("temporary object store failure")
	}
	return "https://cdn.example.test/image.png", nil
}

func TestImageTaskServiceRetriesOffloadWithoutRegenerating(t *testing.T) {
	store := &imageTaskMemoryStore{}
	storage := &transientImageTaskStorage{failAt: 1}
	svc := NewImageTaskServiceWithUploader(store, NewImageResultUploader(storage, "images/", 0, nil), time.Hour, time.Minute)
	owner := ImageTaskOwner{UserID: 1, APIKeyID: 2}
	task, err := svc.Create(context.Background(), owner)
	require.NoError(t, err)
	media := base64.StdEncoding.EncodeToString([]byte("one already-generated image"))
	result, err := json.Marshal(map[string]any{"data": []map[string]string{{"b64_json": media}}})
	require.NoError(t, err)
	require.NoError(t, svc.Complete(context.Background(), task.ID, http.StatusOK, result))
	require.Equal(t, 2, storage.attempts)
	require.Equal(t, ImageTaskStatusCompleted, store.task.Status)
	require.Contains(t, string(store.task.Result), "https://cdn.example.test/image.png")
	require.NotContains(t, string(store.task.Result), media)
}

func TestImageTaskServiceRetriesPartialOffloadWithStableObjectKeys(t *testing.T) {
	store := &imageTaskMemoryStore{}
	storage := &transientImageTaskStorage{failAt: 2}
	svc := NewImageTaskServiceWithUploader(store, NewImageResultUploader(storage, "images/", 0, nil), time.Hour, time.Minute)
	task, err := svc.Create(context.Background(), ImageTaskOwner{UserID: 1, APIKeyID: 2})
	require.NoError(t, err)
	media := base64.StdEncoding.EncodeToString([]byte("already-generated image"))
	result, err := json.Marshal(map[string]any{"data": []map[string]string{{"b64_json": media}, {"b64_json": media}}})
	require.NoError(t, err)
	require.NoError(t, svc.Complete(context.Background(), task.ID, http.StatusOK, result))
	require.Equal(t, ImageTaskStatusCompleted, store.task.Status)
	require.Len(t, storage.keys, 4)
	require.Equal(t, storage.keys[0], storage.keys[2], "retry overwrites the first already-uploaded object")
	require.Equal(t, storage.keys[1], storage.keys[3], "retry uses the second object's original key")
	require.NotContains(t, string(store.task.Result), media)
}
