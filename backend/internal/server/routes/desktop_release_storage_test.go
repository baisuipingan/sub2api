//go:build unit

package routes

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/Wei-Shaw/sub2api/internal/config"
	"github.com/Wei-Shaw/sub2api/internal/handler"
	"github.com/Wei-Shaw/sub2api/internal/server/middleware"
	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

type desktopReleaseStepUpRepo struct{ service.SettingRepository }

func (*desktopReleaseStepUpRepo) GetValue(context.Context, string) (string, error) {
	return "true", nil
}

func TestDesktopReleaseRoutesRequireFullAdminAndStepUp(t *testing.T) {
	gin.SetMode(gin.TestMode)
	endpoints := []struct{ method, path string }{{http.MethodGet, ""}, {http.MethodPut, ""}, {http.MethodPost, "/test"}, {http.MethodPost, "/objects/head"}, {http.MethodPost, "/uploads"}}
	for _, role := range []string{service.RoleUser, service.RoleAdmin} {
		cfg := &config.Config{JWT: config.JWTConfig{Secret: "desktop-release-route-secret", ExpireHour: 1}}
		auth := service.NewAuthService(nil, nil, nil, nil, cfg, nil, nil, nil, nil, nil, nil, nil, nil)
		users := service.NewUserService(&workbenchRouteUsers{role: role}, nil, nil, nil)
		token, err := auth.GenerateToken(context.Background(), &service.User{ID: 9, Role: role, Status: service.StatusActive})
		require.NoError(t, err)
		h := &handler.Handlers{DesktopReleaseStorage: handler.NewDesktopReleaseStorageHandler(nil)}
		router := gin.New()
		admin := router.Group("/api/v1/admin")
		admin.Use(gin.HandlerFunc(middleware.NewAdminAuthMiddleware(auth, users, nil, nil)))
		registerDesktopAgentAdminRoutes(admin, h, middleware.StepUpAuthMiddleware(func(c *gin.Context) { c.AbortWithStatus(http.StatusForbidden) }))
		for _, endpoint := range endpoints {
			rec := httptest.NewRecorder()
			router.ServeHTTP(rec, httptest.NewRequest(endpoint.method, "/api/v1/admin/desktop/release-storage"+endpoint.path, nil))
			require.Equal(t, http.StatusUnauthorized, rec.Code)
			req := httptest.NewRequest(endpoint.method, "/api/v1/admin/desktop/release-storage"+endpoint.path, strings.NewReader(`{}`))
			req.Header.Set("Authorization", "Bearer "+token)
			rec = httptest.NewRecorder()
			router.ServeHTTP(rec, req)
			if role == service.RoleUser || endpoint.method == http.MethodPut || endpoint.path == "/test" || endpoint.path == "/uploads" {
				require.Equal(t, http.StatusForbidden, rec.Code)
			} else {
				require.NotEqual(t, http.StatusNotFound, rec.Code)
			}
			if role == service.RoleAdmin {
				require.Equal(t, "no-store", rec.Header().Get("Cache-Control"))
			}
		}
	}
}

func TestDesktopReleaseStepUpRejectsMachineCredentials(t *testing.T) {
	gin.SetMode(gin.TestMode)
	settings := service.NewSettingService(&desktopReleaseStepUpRepo{}, nil)
	stepUp := middleware.NewStepUpAuthMiddleware(nil, nil, settings)
	router := gin.New()
	admin := router.Group("/api/v1/admin")
	admin.Use(func(c *gin.Context) { c.Set("auth_method", service.AuditAuthMethodAdminAPIKey); c.Next() })
	registerDesktopAgentAdminRoutes(admin, &handler.Handlers{DesktopReleaseStorage: handler.NewDesktopReleaseStorageHandler(nil)}, stepUp)
	for _, endpoint := range []struct{ method, path string }{{http.MethodPut, ""}, {http.MethodPost, "/test"}, {http.MethodPost, "/uploads"}} {
		rec := httptest.NewRecorder()
		router.ServeHTTP(rec, httptest.NewRequest(endpoint.method, "/api/v1/admin/desktop/release-storage"+endpoint.path, strings.NewReader(`{}`)))
		require.Equal(t, http.StatusForbidden, rec.Code)
		require.Contains(t, rec.Body.String(), "STEP_UP_ADMIN_API_KEY_FORBIDDEN")
		require.Equal(t, "no-store", rec.Header().Get("Cache-Control"))
	}
}

type desktopDownloadRouteRepo struct{ service.SettingRepository }

func (*desktopDownloadRouteRepo) GetValue(context.Context, string) (string, error) {
	return "", service.ErrSettingNotFound
}

func TestDesktopDownloadsRouteIsPublicAndOnlyExposesDownloadMetadata(t *testing.T) {
	gin.SetMode(gin.TestMode)
	svc := service.NewDesktopReleaseStorageService(&desktopDownloadRouteRepo{}, nil, nil)
	router := gin.New()
	registerDesktopDownloadRoutes(router.Group("/api/v1"), &handler.Handlers{DesktopReleaseStorage: handler.NewDesktopReleaseStorageHandler(svc)}, nil)
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/api/v1/desktop/downloads", nil))
	require.Equal(t, http.StatusOK, rec.Code)
	require.Equal(t, "public, max-age=60", rec.Header().Get("Cache-Control"))
	require.Contains(t, rec.Body.String(), `"platform":"win32-x64","available":false`)
	require.Contains(t, rec.Body.String(), `"platform":"darwin-arm64","available":false`)
	for _, secret := range []string{"bucket", "access_key", "secret", "endpoint", "signed", "public_base_url"} {
		require.NotContains(t, rec.Body.String(), secret)
	}
	// The public route cannot reach upload, configuration or object APIs.
	for _, path := range []string{"uploads", "objects/head", "release-storage"} {
		rec = httptest.NewRecorder()
		router.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/api/v1/desktop/"+path, nil))
		require.Equal(t, http.StatusNotFound, rec.Code)
	}
}
