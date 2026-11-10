package handler

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/Wei-Shaw/sub2api/internal/server/middleware"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

func announcementBoundaryRouter(authenticated bool) *gin.Engine {
	gin.SetMode(gin.TestMode)
	router := gin.New()
	if authenticated {
		router.Use(func(c *gin.Context) {
			c.Set(string(middleware.ContextKeyUser), middleware.AuthSubject{UserID: 7})
		})
	}
	// A nil service intentionally proves rejected requests stop at the HTTP
	// boundary instead of doing a query or changing read state.
	h := NewAnnouncementHandler(nil)
	router.GET("/announcements/summary", h.Summary)
	router.GET("/announcements/center", h.Center)
	router.POST("/announcements/read-all", h.MarkAllRead)
	router.GET("/announcements/:id", h.Get)
	router.POST("/announcements/:id/read", h.MarkRead)
	return router
}

func TestAnnouncementCenterRequiresAuthenticationBeforeServiceAccess(t *testing.T) {
	router := announcementBoundaryRouter(false)
	for _, tc := range []struct{ method, path string }{
		{http.MethodGet, "/announcements/summary"},
		{http.MethodGet, "/announcements/center"},
		{http.MethodGet, "/announcements/7"},
		{http.MethodPost, "/announcements/read-all"},
		{http.MethodPost, "/announcements/7/read"},
	} {
		t.Run(tc.method+tc.path, func(t *testing.T) {
			recorder := httptest.NewRecorder()
			router.ServeHTTP(recorder, httptest.NewRequest(tc.method, tc.path, nil))
			require.Equal(t, http.StatusUnauthorized, recorder.Code)
		})
	}
}

func TestAnnouncementCenterRejectsMalformedPaginationAndIDs(t *testing.T) {
	router := announcementBoundaryRouter(true)
	for _, path := range []string{
		"/announcements/center?page=0", "/announcements/center?page=-1",
		"/announcements/center?page=10001", "/announcements/center?page=x",
		"/announcements/center?page_size=0", "/announcements/center?page_size=101",
		"/announcements/center?page_size=bogus", "/announcements/center?page=999999999999999999999",
		"/announcements/0", "/announcements/-7", "/announcements/not-an-id",
	} {
		t.Run(path, func(t *testing.T) {
			recorder := httptest.NewRecorder()
			router.ServeHTTP(recorder, httptest.NewRequest(http.MethodGet, path, nil))
			require.Equal(t, http.StatusBadRequest, recorder.Code)
		})
	}
}

func TestAnnouncementCenterReadAllRejectsMissingAndOversizedSnapshot(t *testing.T) {
	router := announcementBoundaryRouter(true)
	for _, body := range []string{"", `{}`, `{"snapshot":""}`, `{"snapshot":null}`, `{"snapshot":7}`, `{"snapshot":"` + strings.Repeat("a", 256<<10) + `"}`} {
		recorder := httptest.NewRecorder()
		request := httptest.NewRequest(http.MethodPost, "/announcements/read-all", strings.NewReader(body))
		request.Header.Set("Content-Type", "application/json")
		router.ServeHTTP(recorder, request)
		require.Equal(t, http.StatusBadRequest, recorder.Code)
	}
}

func TestAnnouncementCenterPaginationBounds(t *testing.T) {
	for _, tc := range []struct {
		query          string
		page, pageSize int
	}{
		{"", 1, 20}, {"?page=10000&page_size=100", 10000, 100}, {"?page=2&page_size=1", 2, 1},
	} {
		c, _ := gin.CreateTestContext(httptest.NewRecorder())
		c.Request = httptest.NewRequest(http.MethodGet, "/announcements/center"+tc.query, nil)
		page, size, err := announcementCenterPagination(c)
		require.NoError(t, err)
		require.Equal(t, tc.page, page)
		require.Equal(t, tc.pageSize, size)
	}
}
