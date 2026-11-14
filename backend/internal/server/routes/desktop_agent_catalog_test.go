//go:build unit

package routes

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/Wei-Shaw/sub2api/internal/config"
	"github.com/Wei-Shaw/sub2api/internal/handler"
	"github.com/Wei-Shaw/sub2api/internal/server/middleware"
	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

func TestDesktopAgentCatalogRoutesRequireFullAdminAndWriteStepUp(t *testing.T) {
	gin.SetMode(gin.TestMode)
	for _, role := range []string{service.RoleUser, service.RoleAdmin} {
		t.Run(role, func(t *testing.T) {
			cfg := &config.Config{JWT: config.JWTConfig{Secret: "catalog-route-fixture", ExpireHour: 1}}
			auth := service.NewAuthService(nil, nil, nil, nil, cfg, nil, nil, nil, nil, nil, nil, nil, nil)
			users := service.NewUserService(&workbenchRouteUsers{role: role}, nil, nil, nil)
			token, err := auth.GenerateToken(context.Background(), &service.User{ID: 9, Role: role, Status: service.StatusActive})
			require.NoError(t, err)
			repo := &desktopRouteSettings{}
			handlers := &handler.Handlers{DesktopAgent: handler.NewDesktopAgentHandler(service.NewDesktopAgentService(service.NewSettingService(repo, nil), nil, nil, nil))}
			router := gin.New()
			admin := router.Group("/api/v1/admin")
			admin.Use(gin.HandlerFunc(middleware.NewAdminAuthMiddleware(auth, users, nil, nil)))
			registerDesktopAgentAdminRoutes(admin, handlers, middleware.StepUpAuthMiddleware(func(c *gin.Context) { c.AbortWithStatus(http.StatusForbidden) }))
			for _, endpoint := range []struct{ method, path string }{{"GET", "/desktop/profile-catalog"}, {"POST", "/desktop/profile-catalog/refresh"}, {"PUT", "/desktop/profile-catalog/source"}} {
				unauth := httptest.NewRecorder()
				router.ServeHTTP(unauth, httptest.NewRequest(endpoint.method, "/api/v1/admin"+endpoint.path, nil))
				require.Equal(t, 401, unauth.Code)
				request := httptest.NewRequest(endpoint.method, "/api/v1/admin"+endpoint.path, nil)
				request.Header.Set("Authorization", "Bearer "+token)
				result := httptest.NewRecorder()
				router.ServeHTTP(result, request)
				if role == service.RoleAdmin && endpoint.method == "GET" {
					require.Equal(t, 200, result.Code)
					require.Contains(t, result.Body.String(), `"profile_count":37`)
				} else {
					require.Equal(t, 403, result.Code)
				}
				if role == service.RoleAdmin {
					require.Equal(t, "no-store", result.Header().Get("Cache-Control"))
				}
			}
			require.Zero(t, repo.writes)
		})
	}
}
