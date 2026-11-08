package admin

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/Wei-Shaw/sub2api/internal/pkg/adminaccess"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

func TestWorkbenchGalleryRestrictedKeysCannotReadImagesOrMetadata(t *testing.T) {
	gin.SetMode(gin.TestMode)
	h := NewWorkbenchHandler(nil)
	router := gin.New()
	router.Use(func(c *gin.Context) {
		c.Request = c.Request.WithContext(adminaccess.WithPrincipal(context.Background(), &adminaccess.Principal{Policy: adminaccess.Policy{}}))
	})
	router.GET("/gallery", h.ListGallery)
	router.GET("/gallery/:user_id/tasks/:task_id/images/:image_id/url", h.GalleryImageURL)
	for _, path := range []string{"/gallery", "/gallery/7/tasks/task-one/images/image-one/url"} {
		rec := httptest.NewRecorder()
		router.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, path, nil))
		require.Equal(t, http.StatusForbidden, rec.Code)
	}
}
