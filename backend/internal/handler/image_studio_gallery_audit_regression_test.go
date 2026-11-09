//go:build unit

package handler

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/base64"
	"encoding/binary"
	"encoding/hex"
	"image"
	"image/jpeg"
	"image/png"
	"math/rand/v2"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/Wei-Shaw/sub2api/internal/config"
	"github.com/Wei-Shaw/sub2api/internal/server/middleware"
	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

type galleryAuditObjectStore struct {
	galleryHandlerStore
	objects map[string][]byte
}

func (s *galleryAuditObjectStore) Put(_ context.Context, key, _ string, data []byte) error {
	s.objects[key] = append([]byte(nil), data...)
	return nil
}

// Reference uploads travel through the authenticated panel audit middleware.
// Exercise bodies larger than its captured prefix so a future body-restore
// regression cannot turn valid new reference images into GALLERY_INVALID.
func TestImageStudioReferenceUploadPreservesBinaryThroughAudit(t *testing.T) {
	gin.SetMode(gin.TestMode)
	imageBytes := func(t *testing.T, format string, transparent bool) []byte {
		t.Helper()
		img := image.NewNRGBA(image.Rect(0, 0, 640, 480))
		rng := rand.New(rand.NewPCG(17, 42))
		for i := 0; i < len(img.Pix); i += 4 {
			img.Pix[i], img.Pix[i+1], img.Pix[i+2] = byte(rng.Uint32()), byte(rng.Uint32()), byte(rng.Uint32())
			img.Pix[i+3] = 255
			if transparent {
				img.Pix[i+3] = byte(rng.Uint32())
			}
		}
		var out bytes.Buffer
		if format == "jpeg" {
			require.NoError(t, jpeg.Encode(&out, img, &jpeg.Options{Quality: 100}))
		} else {
			require.NoError(t, png.Encode(&out, img))
		}
		require.Greater(t, out.Len(), service.AuditRequestBodyCaptureLimit)
		return out.Bytes()
	}
	// Lossless WebP from golang.org/x/image's BSD-licensed gopher-doc fixture.
	webp, err := base64.StdEncoding.DecodeString("UklGRrIBAABXRUJQVlA4TKUBAAAvSsAYAA8w//M///MfeJAkbXvaSG7m8Q3GfYSBJekwQztm/IcZlgwnmWImn2BK7aFmBtnVir6q//8VOkFE/xm4baTIu8c48ArEo6+B3zFKYln3pqClSCKX0begFTAXFOLXHSyF8cCNcZEG4OywuA4KVVfJCiArU7GAgJI8+lJP/OKMT/fBAjevg1cYB7YVkFuWga2lyPi5I0HFy5YTpWIHg0RZpkniRVW9odHAKOwosWuOGdxIyn2OvaCDvhg/we6TwadPBPbqBV58MsLmMJ8yZnOWk8SRz4N+QoyPL+MnamzMvcE1rHNEr91F9GKZPVUcS9w7PhhH36suB9qPeYb/oLk6cuTiJ0wOK3m5h1cKjW6EVZCYMK7dxcKCBdgP9HkKr9gkAO2P8GKZGWVdIAatQa+1IDpt6qyorVwdy01xdW8Jkfk6xjEXmVQQ+HQdFr6OKhIN34dXWq0+0qr6EJSCeeVLH9+gvGTLyqM65PQ44ihzlTXxQKjKbAvshXgir7Lil9w4L2bvMycmjQcqXaMCO6BlY28i+FOLzbfI1vEqxAhotocAAA==")
	require.NoError(t, err)
	// A valid ignorable RIFF JUNK chunk crosses the same audit capture boundary.
	junk := make([]byte, service.AuditRequestBodyCaptureLimit+8)
	copy(junk, "JUNK")
	binary.LittleEndian.PutUint32(junk[4:8], uint32(len(junk)-8))
	webp = append(webp, junk...)
	binary.LittleEndian.PutUint32(webp[4:8], uint32(len(webp)-8))
	for _, tc := range []struct {
		name, mime string
		data       []byte
	}{
		{"opaque-png", "image/png", imageBytes(t, "png", false)},
		{"transparent-png", "image/png", imageBytes(t, "png", true)},
		{"jpeg", "image/jpeg", imageBytes(t, "jpeg", false)},
		{"webp", "image/webp", webp},
	} {
		t.Run(tc.name, func(t *testing.T) {
			settings := &galleryHandlerSettings{raw: `{"studio_gallery_enabled":true,"enabled":false,"bucket":"gallery","endpoint":"https://s3.test","region":"auto","access_key_id":"ak","secret_access_key":"secret"}`}
			storageSettings := service.NewImageStorageSettingService(settings, galleryHandlerEncryptor{}, nil, nil, config.ImageStorageConfig{})
			repo := &galleryHandlerRepo{}
			store := &galleryAuditObjectStore{objects: map[string][]byte{}}
			gallery := service.NewImageStudioGalleryService(repo, storageSettings, func(context.Context, *config.ImageStorageConfig) (service.GalleryObjectStore, error) {
				return store, nil
			})
			router := gin.New()
			router.Use(func(c *gin.Context) {
				c.Set(string(middleware.ContextKeyUser), middleware.AuthSubject{UserID: 42})
				c.Next()
			})
			// No writer goroutine is needed: Record still exercises body capture.
			audit := service.NewAuditLogService(nil, nil)
			defer audit.Stop()
			router.Use(gin.HandlerFunc(middleware.NewAuditLogMiddleware(audit)))
			router.PUT("/api/v1/image-studio/gallery/assets/:image_id", NewImageStudioGalleryHandler(gallery).PutAsset)
			req := httptest.NewRequest(http.MethodPut, "/api/v1/image-studio/gallery/assets/reference-one", bytes.NewReader(tc.data))
			req.Header.Set("Content-Type", tc.mime)
			response := httptest.NewRecorder()
			router.ServeHTTP(response, req)
			require.Equal(t, http.StatusOK, response.Code, response.Body.String())
			require.NotNil(t, repo.asset)
			require.Equal(t, tc.data, store.objects[repo.asset.OriginalKey])
			digest := sha256.Sum256(tc.data)
			require.Equal(t, hex.EncodeToString(digest[:]), repo.asset.SHA256)
			require.Equal(t, tc.mime, repo.asset.MIME)
		})
	}
}
