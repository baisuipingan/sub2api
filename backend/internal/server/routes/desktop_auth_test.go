//go:build unit

package routes

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/handler"
	"github.com/Wei-Shaw/sub2api/internal/server/middleware"
	"github.com/alicebob/miniredis/v2"
	"github.com/gin-gonic/gin"
	"github.com/redis/go-redis/v9"
	"github.com/stretchr/testify/require"
)

func newDesktopAuthRouteTest(t *testing.T, rdb *redis.Client) *gin.Engine {
	t.Helper()
	gin.SetMode(gin.TestMode)
	router := gin.New()
	RegisterAuthRoutes(router.Group("/api/v1"), &handler.Handlers{Auth: &handler.AuthHandler{}, Setting: &handler.SettingHandler{}, DesktopAuth: handler.NewDesktopAuthHandler(nil)}, middleware.JWTAuthMiddleware(func(c *gin.Context) { c.AbortWithStatus(http.StatusUnauthorized) }), middleware.AuditLogMiddleware(func(c *gin.Context) { c.Next() }), rdb, nil, nil)
	return router
}

func desktopAuthRouteRequest(router *gin.Engine, path string) *httptest.ResponseRecorder {
	req := httptest.NewRequest(http.MethodPost, path, strings.NewReader(`{}`))
	req.Header.Set("Content-Type", "application/json")
	req.RemoteAddr = "203.0.113.10:43210"
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)
	return w
}

func TestDesktopAuthRoutesTokenBudgetAndJWTIsolation(t *testing.T) {
	mr := miniredis.RunT(t)
	rdb := redis.NewClient(&redis.Options{Addr: mr.Addr()})
	t.Cleanup(func() { _ = rdb.Close() })
	router := newDesktopAuthRouteTest(t, rdb)
	w := desktopAuthRouteRequest(router, "/api/v1/desktop/auth/authorize")
	require.Equal(t, http.StatusUnauthorized, w.Code)
	for range 20 {
		w = desktopAuthRouteRequest(router, "/api/v1/desktop/auth/token")
		require.Equal(t, http.StatusBadRequest, w.Code, w.Body.String())
		require.Equal(t, "no-store", w.Header().Get("Cache-Control"))
	}
	w = desktopAuthRouteRequest(router, "/api/v1/desktop/auth/token")
	require.Equal(t, http.StatusTooManyRequests, w.Code)
	require.Equal(t, "no-store", w.Header().Get("Cache-Control"))
}

func TestDesktopAuthRoutesTokenFailsClosedWithoutRedis(t *testing.T) {
	rdb := redis.NewClient(&redis.Options{Addr: "127.0.0.1:1", MaxRetries: -1, DialTimeout: 50 * time.Millisecond})
	t.Cleanup(func() { _ = rdb.Close() })
	w := desktopAuthRouteRequest(newDesktopAuthRouteTest(t, rdb), "/api/v1/desktop/auth/token")
	require.Equal(t, http.StatusTooManyRequests, w.Code)
	require.Equal(t, "no-store", w.Header().Get("Cache-Control"))
}
