//go:build unit

package handler

import (
	"bytes"
	"context"
	"encoding/json"
	"image"
	"image/png"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/config"
	"github.com/Wei-Shaw/sub2api/internal/server/middleware"
	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
	"github.com/tidwall/gjson"
)

type galleryHandlerSettings struct {
	service.SettingRepository
	raw string
}

func (s *galleryHandlerSettings) GetValue(context.Context, string) (string, error) { return s.raw, nil }

type galleryHandlerEncryptor struct{}

func (galleryHandlerEncryptor) Encrypt(value string) (string, error) { return value, nil }
func (galleryHandlerEncryptor) Decrypt(value string) (string, error) { return value, nil }

type galleryHandlerStore struct{}

func (galleryHandlerStore) Put(context.Context, string, string, []byte) error { return nil }
func (galleryHandlerStore) Delete(context.Context, string) error              { return nil }
func (galleryHandlerStore) PresignGet(_ context.Context, key string, _ time.Duration) (string, error) {
	return "https://s3.test/" + key, nil
}

type galleryHandlerRepo struct {
	service.GalleryRepository
	asset *service.GalleryAsset
	task  json.RawMessage
}

func (r *galleryHandlerRepo) LockTarget(context.Context) (func(), error) { return func() {}, nil }
func (r *galleryHandlerRepo) Stats(context.Context, int64) (service.GalleryStats, error) {
	return service.GalleryStats{Tasks: 1, DoneTasks: 1, OutputImages: 1, Assets: 1, UsedBytes: 4, MaxTasks: 1000, MaxAssets: 500, MaxBytes: 1 << 30}, nil
}
func (r *galleryHandlerRepo) GetAsset(_ context.Context, userID int64, id string) (*service.GalleryAsset, error) {
	if r.asset == nil || r.asset.UserID != userID || r.asset.ImageID != id {
		return nil, service.ErrGalleryNotFound
	}
	return r.asset, nil
}
func (r *galleryHandlerRepo) ReserveAsset(_ context.Context, a service.GalleryAsset) (*service.GalleryAsset, error) {
	r.asset = &a
	return &a, nil
}
func (r *galleryHandlerRepo) MarkAssetReady(context.Context, int64, string) error {
	r.asset.State = "ready"
	return nil
}
func (r *galleryHandlerRepo) SaveTask(_ context.Context, _ int64, _ string, _ string, task json.RawMessage, _ []service.GalleryTaskLink) (json.RawMessage, error) {
	r.task = task
	return task, nil
}
func (r *galleryHandlerRepo) SaveAcceptedTask(ctx context.Context, userID int64, taskID, digest string, task json.RawMessage, links []service.GalleryTaskLink) (json.RawMessage, error) {
	return r.SaveTask(ctx, userID, taskID, digest, task, links)
}
func (r *galleryHandlerRepo) GetTask(_ context.Context, _ int64, _ string) (json.RawMessage, error) {
	return r.task, nil
}
func (r *galleryHandlerRepo) ListTasks(_ context.Context, _ int64, _ string, _ int) (service.GalleryTaskPage, error) {
	return service.GalleryTaskPage{Items: []json.RawMessage{r.task}}, nil
}
func (r *galleryHandlerRepo) DeleteTask(context.Context, int64, string) ([]service.GalleryAsset, error) {
	return nil, nil
}

func TestImageStudioGalleryCanonicalResponseEnvelope(t *testing.T) {
	gin.SetMode(gin.TestMode)
	settings := &galleryHandlerSettings{raw: `{"studio_gallery_enabled":true,"enabled":false,"bucket":"gallery","endpoint":"https://s3.test","region":"auto","access_key_id":"ak","secret_access_key":"secret"}`}
	storageSettings := service.NewImageStorageSettingService(settings, galleryHandlerEncryptor{}, nil, nil, config.ImageStorageConfig{})
	repo := &galleryHandlerRepo{}
	gallery := service.NewImageStudioGalleryService(repo, storageSettings, func(context.Context, *config.ImageStorageConfig) (service.GalleryObjectStore, error) {
		return galleryHandlerStore{}, nil
	})
	h := NewImageStudioGalleryHandler(gallery)
	router := gin.New()
	router.Use(func(c *gin.Context) {
		c.Set(string(middleware.ContextKeyUser), middleware.AuthSubject{UserID: 42})
		c.Next()
	})
	router.GET("/gallery/status", h.Status)
	router.PUT("/gallery/assets/:image_id", h.PutAsset)
	router.GET("/gallery/assets/:image_id/url", h.AssetURL)
	router.POST("/gallery/tasks", h.SaveTask)
	router.GET("/gallery/tasks", h.ListTasks)
	router.GET("/gallery/tasks/:id", h.GetTask)
	router.DELETE("/gallery/tasks/:id", h.DeleteTask)
	request := func(method, path string, body []byte, contentType string) *httptest.ResponseRecorder {
		rec := httptest.NewRecorder()
		req := httptest.NewRequest(method, path, bytes.NewReader(body))
		if contentType != "" {
			req.Header.Set("Content-Type", contentType)
		}
		router.ServeHTTP(rec, req)
		return rec
	}
	status := request(http.MethodGet, "/gallery/status", nil, "")
	require.Equal(t, http.StatusOK, status.Code)
	require.Equal(t, int64(42), gjson.GetBytes(status.Body.Bytes(), "data.user_id").Int())
	require.True(t, gjson.GetBytes(status.Body.Bytes(), "data.enabled").Bool())
	require.False(t, gjson.GetBytes(status.Body.Bytes(), "data.jobs_enabled").Bool())
	require.Equal(t, int64(1), gjson.GetBytes(status.Body.Bytes(), "data.stats.tasks").Int())
	require.Equal(t, int64(4), gjson.GetBytes(status.Body.Bytes(), "data.stats.used_bytes").Int())

	var pngBody bytes.Buffer
	require.NoError(t, png.Encode(&pngBody, image.NewNRGBA(image.Rect(0, 0, 2, 2))))
	put := request(http.MethodPut, "/gallery/assets/image-one", pngBody.Bytes(), "image/png")
	require.Equal(t, http.StatusOK, put.Code, put.Body.String())
	require.Equal(t, "image-one", gjson.GetBytes(put.Body.Bytes(), "data.id").String())
	task := []byte(`{"task":{"id":"task-one","status":"done","outputImages":["image-one"]}}`)
	post := request(http.MethodPost, "/gallery/tasks", task, "application/json")
	require.Equal(t, http.StatusOK, post.Code, post.Body.String())
	require.Equal(t, "task-one", gjson.GetBytes(post.Body.Bytes(), "data.task.id").String())
	list := request(http.MethodGet, "/gallery/tasks", nil, "")
	require.Equal(t, http.StatusOK, list.Code)
	require.Equal(t, "task-one", gjson.GetBytes(list.Body.Bytes(), "data.items.0.id").String())
	require.True(t, gjson.GetBytes(list.Body.Bytes(), "data.next_cursor").Exists())
	get := request(http.MethodGet, "/gallery/tasks/task-one", nil, "")
	require.Equal(t, "task-one", gjson.GetBytes(get.Body.Bytes(), "data.task.id").String())
	url := request(http.MethodGet, "/gallery/assets/image-one/url?variant=original", nil, "")
	require.Equal(t, http.StatusOK, url.Code, url.Body.String())
	require.Contains(t, gjson.GetBytes(url.Body.Bytes(), "data.url").String(), "https://s3.test/")
	require.Equal(t, int64(pngBody.Len()), gjson.GetBytes(url.Body.Bytes(), "data.bytes").Int())
	require.Equal(t, "image/png", gjson.GetBytes(url.Body.Bytes(), "data.mime").String())
	require.Equal(t, int64(2), gjson.GetBytes(url.Body.Bytes(), "data.width").Int())
	require.Equal(t, int64(2), gjson.GetBytes(url.Body.Bytes(), "data.height").Int())
	require.NotEmpty(t, gjson.GetBytes(url.Body.Bytes(), "data.expires_at").String())
	del := request(http.MethodDelete, "/gallery/tasks/task-one", nil, "")
	require.True(t, gjson.GetBytes(del.Body.Bytes(), "data.deleted").Bool())

	settings.raw = `{"studio_gallery_enabled":false,"enabled":false,"bucket":"gallery","endpoint":"https://s3.test","region":"auto","access_key_id":"ak","secret_access_key":"secret"}`
	require.False(t, gjson.GetBytes(request(http.MethodGet, "/gallery/status", nil, "").Body.Bytes(), "data.enabled").Bool())
	require.Equal(t, http.StatusServiceUnavailable, request(http.MethodPut, "/gallery/assets/image-two", pngBody.Bytes(), "image/png").Code)
	require.Equal(t, http.StatusOK, request(http.MethodGet, "/gallery/tasks", nil, "").Code)
	require.Equal(t, http.StatusOK, request(http.MethodGet, "/gallery/assets/image-one/url?variant=original", nil, "").Code)
}

func TestGalleryValidationHTTPResponsePreservesSafeReasonAndLimits(t *testing.T) {
	gin.SetMode(gin.TestMode)
	for _, tc := range []struct {
		reason   string
		status   int
		metadata map[string]string
	}{
		{"GALLERY_IMAGE_TOO_MANY_PIXELS", http.StatusBadRequest, map[string]string{"max_pixels": "20000000", "width": "5000", "height": "5000"}},
		{"GALLERY_TOO_LARGE", http.StatusRequestEntityTooLarge, map[string]string{"max_bytes": "33554432"}},
	} {
		rec := httptest.NewRecorder()
		c, _ := gin.CreateTestContext(rec)
		galleryHTTPError(c, &service.GalleryValidationError{Reason: tc.reason, Message: "reference image exceeds the supported limit", Metadata: tc.metadata})
		require.Equal(t, tc.status, rec.Code)
		require.Equal(t, tc.reason, gjson.GetBytes(rec.Body.Bytes(), "reason").String())
		for key, value := range tc.metadata {
			require.Equal(t, value, gjson.GetBytes(rec.Body.Bytes(), "metadata."+key).String())
		}
	}
}
