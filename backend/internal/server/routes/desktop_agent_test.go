//go:build unit

package routes

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/config"
	"github.com/Wei-Shaw/sub2api/internal/handler"
	"github.com/Wei-Shaw/sub2api/internal/server/middleware"
	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

type desktopRouteSettings struct {
	service.SettingRepository
	writes int
}

type desktopRouteUsers struct{ workbenchRouteUsers }

func (*desktopRouteUsers) UpdateUserLastActiveAt(context.Context, int64, time.Time) error {
	return nil
}

func (r *desktopRouteSettings) GetMultiple(context.Context, []string) (map[string]string, error) {
	return map[string]string{}, nil
}
func (r *desktopRouteSettings) SetMultiple(context.Context, map[string]string) error {
	r.writes++
	return nil
}
func TestDesktopAgentAdminRoutesRequireAdminAndStepUp(t *testing.T) {
	gin.SetMode(gin.TestMode)
	for _, role := range []string{service.RoleUser, service.RoleAdmin} {
		t.Run(role, func(t *testing.T) {
			cfg := &config.Config{JWT: config.JWTConfig{Secret: "desktop-route-fixture-secret", ExpireHour: 1}}
			auth := service.NewAuthService(nil, nil, nil, nil, cfg, nil, nil, nil, nil, nil, nil, nil, nil)
			users := service.NewUserService(&workbenchRouteUsers{role: role}, nil, nil, nil)
			token, err := auth.GenerateToken(context.Background(), &service.User{ID: 9, Role: role, Status: service.StatusActive})
			require.NoError(t, err)
			repo := &desktopRouteSettings{}
			svc := service.NewDesktopAgentService(service.NewSettingService(repo, nil), nil, nil, nil)
			h := &handler.Handlers{DesktopAgent: handler.NewDesktopAgentHandler(svc)}
			router := gin.New()
			admin := router.Group("/api/v1/admin")
			admin.Use(gin.HandlerFunc(middleware.NewAdminAuthMiddleware(auth, users, nil, nil)))
			registerDesktopAgentAdminRoutes(admin, h, middleware.StepUpAuthMiddleware(func(c *gin.Context) { c.AbortWithStatus(http.StatusForbidden) }))
			for _, method := range []string{http.MethodGet, http.MethodPut} {
				rec := httptest.NewRecorder()
				router.ServeHTTP(rec, httptest.NewRequest(method, "/api/v1/admin/desktop/config", nil))
				require.Equal(t, http.StatusUnauthorized, rec.Code)
			}
			req := httptest.NewRequest(http.MethodGet, "/api/v1/admin/desktop/config", nil)
			req.Header.Set("Authorization", "Bearer "+token)
			rec := httptest.NewRecorder()
			router.ServeHTTP(rec, req)
			if role == service.RoleAdmin {
				require.Equal(t, http.StatusOK, rec.Code)
			} else {
				require.Equal(t, http.StatusForbidden, rec.Code)
			}
			req = httptest.NewRequest(http.MethodPut, "/api/v1/admin/desktop/config", strings.NewReader(`{"enabled":false}`))
			req.Header.Set("Authorization", "Bearer "+token)
			rec = httptest.NewRecorder()
			router.ServeHTTP(rec, req)
			require.Equal(t, http.StatusForbidden, rec.Code)
			require.Zero(t, repo.writes)
			for _, endpoint := range []struct{ method, path string }{{http.MethodGet, "/api/v1/admin/desktop/models?group_id=0"}, {http.MethodPost, "/api/v1/admin/desktop/config/disable"}} {
				unauth := httptest.NewRecorder()
				router.ServeHTTP(unauth, httptest.NewRequest(endpoint.method, endpoint.path, nil))
				require.Equal(t, http.StatusUnauthorized, unauth.Code)
				request := httptest.NewRequest(endpoint.method, endpoint.path, nil)
				request.Header.Set("Authorization", "Bearer "+token)
				result := httptest.NewRecorder()
				router.ServeHTTP(result, request)
				if endpoint.method == http.MethodPost {
					require.Equal(t, http.StatusForbidden, result.Code)
					if role == service.RoleAdmin {
						require.Equal(t, "no-store", result.Header().Get("Cache-Control"))
					}
				} else if role == service.RoleUser {
					require.Equal(t, http.StatusForbidden, result.Code)
				} else {
					require.Equal(t, http.StatusBadRequest, result.Code)
				}
			}
			require.Zero(t, repo.writes)
		})
	}
}
func TestDesktopAgentUserRouteRequiresActualJWT(t *testing.T) {
	gin.SetMode(gin.TestMode)
	cfg := &config.Config{JWT: config.JWTConfig{Secret: "desktop-user-route-secret", ExpireHour: 1}}
	auth := service.NewAuthService(nil, nil, nil, nil, cfg, nil, nil, nil, nil, nil, nil, nil, nil)
	users := service.NewUserService(&desktopRouteUsers{workbenchRouteUsers{role: service.RoleUser}}, nil, nil, nil)
	token, err := auth.GenerateToken(context.Background(), &service.User{ID: 9, Role: service.RoleUser, Status: service.StatusActive})
	require.NoError(t, err)
	svc := service.NewDesktopAgentService(service.NewSettingService(&desktopRouteSettings{}, nil), nil, nil, nil)
	h := &handler.Handlers{DesktopAgent: handler.NewDesktopAgentHandler(svc)}
	router := gin.New()
	authenticated := router.Group("/api/v1")
	authenticated.Use(gin.HandlerFunc(middleware.NewJWTAuthMiddleware(auth, users, nil, nil)))
	registerDesktopAgentUserRoutes(authenticated, h)
	for _, authorization := range []string{"", "Bearer invalid", "Bearer " + token} {
		req := httptest.NewRequest(http.MethodPost, "/api/v1/desktop/bootstrap", strings.NewReader(`{"client_version":"fixture-1","protocol_version":1,"platform":"win32"}`))
		req.Header.Set("Authorization", authorization)
		rec := httptest.NewRecorder()
		router.ServeHTTP(rec, req)
		if authorization != "Bearer "+token {
			require.Equal(t, http.StatusUnauthorized, rec.Code)
		} else {
			require.Equal(t, http.StatusOK, rec.Code)
			require.Contains(t, rec.Body.String(), `"service_status":"disabled"`)
			require.NotContains(t, rec.Body.String(), "credential")
		}
	}
}
