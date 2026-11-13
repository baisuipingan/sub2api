//go:build unit

package handler

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/Wei-Shaw/sub2api/internal/pkg/adminaccess"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

func TestDesktopReleaseStrictBoundedJSON(t *testing.T) {
	gin.SetMode(gin.TestMode)
	for _, body := range []string{`null`, `[]`, `{"key":"a","key":"b"}`, `{"key":"a","extra":true}`, `{"key":"a"} {}`, strings.Repeat(" ", 4097), `{"key":"` + strings.Repeat("a", 5000) + `"}`} {
		t.Run(body[:min(len(body), 40)], func(t *testing.T) {
			rec := httptest.NewRecorder()
			c, _ := gin.CreateTestContext(rec)
			c.Request = httptest.NewRequest(http.MethodPost, "/", strings.NewReader(body))
			var target struct {
				Key string `json:"key"`
			}
			require.False(t, readDesktopReleaseBody(c, &target))
			require.Equal(t, http.StatusBadRequest, rec.Code)
			require.NotContains(t, rec.Body.String(), "extra")
		})
	}
	rec := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(rec)
	c.Request = httptest.NewRequest(http.MethodPost, "/", strings.NewReader(`{"key":"valid"}`))
	var target struct {
		Key string `json:"key"`
	}
	require.True(t, readDesktopReleaseBody(c, &target))
	require.Equal(t, "valid", target.Key)
}

func TestDesktopReleaseRejectsRestrictedAdminBeforeService(t *testing.T) {
	gin.SetMode(gin.TestMode)
	for _, method := range []func(*DesktopReleaseStorageHandler, *gin.Context){(*DesktopReleaseStorageHandler).GetConfig, (*DesktopReleaseStorageHandler).UpdateConfig, (*DesktopReleaseStorageHandler).TestConnection, (*DesktopReleaseStorageHandler).HeadObject, (*DesktopReleaseStorageHandler).CreateUpload} {
		rec := httptest.NewRecorder()
		c, _ := gin.CreateTestContext(rec)
		c.Request = httptest.NewRequest(http.MethodGet, "/", nil)
		c.Request = c.Request.WithContext(adminaccess.WithPrincipal(c.Request.Context(), &adminaccess.Principal{Policy: adminaccess.Policy{}}))
		method(&DesktopReleaseStorageHandler{}, c)
		require.Equal(t, http.StatusForbidden, rec.Code)
		require.Equal(t, "no-store", rec.Header().Get("Cache-Control"))
	}
}
