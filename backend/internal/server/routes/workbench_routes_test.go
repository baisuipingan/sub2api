//go:build unit

package routes

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/config"
	"github.com/Wei-Shaw/sub2api/internal/handler"
	adminhandler "github.com/Wei-Shaw/sub2api/internal/handler/admin"
	"github.com/Wei-Shaw/sub2api/internal/pkg/adminaccess"
	"github.com/Wei-Shaw/sub2api/internal/server/middleware"
	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

type workbenchRouteUsers struct {
	service.UserRepository
	role string
}

func (r *workbenchRouteUsers) GetByID(context.Context, int64) (*service.User, error) {
	return &service.User{ID: 9, Role: r.role, Status: service.StatusActive}, nil
}
func (r *workbenchRouteUsers) GetUserAvatar(context.Context, int64) (*service.UserAvatar, error) {
	return nil, nil
}

type workbenchRouteRepo struct{ calls int }

func (r *workbenchRouteRepo) Overview(context.Context, time.Time, time.Time) (*service.WorkbenchOverview, error) {
	r.calls++
	return &service.WorkbenchOverview{}, nil
}
func (r *workbenchRouteRepo) ListTasks(context.Context, service.WorkbenchTaskFilter) (*service.WorkbenchTaskPage, error) {
	r.calls++
	return &service.WorkbenchTaskPage{Items: []service.WorkbenchTaskSummary{}}, nil
}

func TestWorkbenchRoutesRequireActualAdminAuthAndStepUp(t *testing.T) {
	gin.SetMode(gin.TestMode)
	for _, role := range []string{service.RoleUser, service.RoleAdmin} {
		cfg := &config.Config{JWT: config.JWTConfig{Secret: "workbench-test-secret", ExpireHour: 1}}
		auth := service.NewAuthService(nil, nil, nil, nil, cfg, nil, nil, nil, nil, nil, nil, nil, nil)
		users := service.NewUserService(&workbenchRouteUsers{role: role}, nil, nil, nil)
		token, err := auth.GenerateToken(context.Background(), &service.User{ID: 9, Role: role, Status: service.StatusActive})
		require.NoError(t, err)
		repo := &workbenchRouteRepo{}
		r := gin.New()
		admin := r.Group("/api/v1/admin")
		admin.Use(gin.HandlerFunc(middleware.NewAdminAuthMiddleware(auth, users, nil, nil)))
		h := &handler.Handlers{Admin: &handler.AdminHandlers{Workbench: adminhandler.NewWorkbenchHandler(service.NewWorkbenchAdminService(repo, nil, nil))}}
		step := middleware.StepUpAuthMiddleware(func(c *gin.Context) { c.AbortWithStatus(http.StatusForbidden) })
		registerWorkbenchRoutes(admin, h, step, nil)
		for _, path := range []string{"/overview", "/tasks", "/config"} {
			rec := httptest.NewRecorder()
			r.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/api/v1/admin/workbench"+path, nil))
			require.Equal(t, http.StatusUnauthorized, rec.Code)
		}
		request := httptest.NewRequest(http.MethodGet, "/api/v1/admin/workbench/tasks", nil)
		request.Header.Set("Authorization", "Bearer "+token)
		rec := httptest.NewRecorder()
		r.ServeHTTP(rec, request)
		if role == service.RoleUser {
			require.Equal(t, http.StatusForbidden, rec.Code)
			require.Zero(t, repo.calls)
		} else {
			require.Equal(t, http.StatusOK, rec.Code)
			require.Equal(t, 1, repo.calls)
		}
		request = httptest.NewRequest(http.MethodPut, "/api/v1/admin/workbench/config", nil)
		request.Header.Set("Authorization", "Bearer "+token)
		rec = httptest.NewRecorder()
		r.ServeHTTP(rec, request)
		require.Equal(t, http.StatusForbidden, rec.Code)
	}
}

func TestWorkbenchRoutesDenyScopedAdminKeyWithoutGlobalCapability(t *testing.T) {
	gin.SetMode(gin.TestMode)
	keys := &scopedRouteKeys{}
	settings := service.NewSettingService(nil, nil)
	settings.SetScopedAdminKeyRepository(keys)
	// Use a known valid policy: no known resource-scoped right grants all-user workbench access.
	key, _, err := settings.CreateScopedAdminKey(context.Background(), service.ScopedAdminKeyInput{Name: "workbench restricted", Enabled: true, Policy: adminaccess.Policy{Rules: []adminaccess.Rule{{Permissions: []string{"accounts.read"}, AllGroups: true, AccountTypes: []string{"apikey"}}}}}, 9)
	require.NoError(t, err)
	repo := &workbenchRouteRepo{}
	r := gin.New()
	admin := r.Group("/api/v1/admin")
	admin.Use(gin.HandlerFunc(middleware.NewAdminAuthMiddleware(nil, service.NewUserService(&scopedRouteUsers{}, nil, nil, nil), settings, nil)))
	h := &handler.Handlers{Admin: &handler.AdminHandlers{Workbench: adminhandler.NewWorkbenchHandler(service.NewWorkbenchAdminService(repo, nil, nil))}}
	registerWorkbenchRoutes(admin, h, middleware.StepUpAuthMiddleware(func(c *gin.Context) { c.Next() }), nil)
	for _, path := range []string{"/overview", "/tasks", "/config"} {
		req := httptest.NewRequest(http.MethodGet, "/api/v1/admin/workbench"+path, nil)
		req.Header.Set("x-api-key", key)
		rec := httptest.NewRecorder()
		r.ServeHTTP(rec, req)
		require.Equal(t, http.StatusForbidden, rec.Code)
	}
	require.Zero(t, repo.calls)
}
