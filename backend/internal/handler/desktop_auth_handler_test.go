//go:build unit

package handler

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

func TestDesktopAuthHandlerRejectsAmbiguousOrUnboundedBody(t *testing.T) {
	gin.SetMode(gin.TestMode)
	h := NewDesktopAuthHandler(nil)
	for _, test := range []struct{ body, content string }{
		{`{}`, "text/plain"}, {`{}`, "application/x-www-form-urlencoded"},
		{`{"code":null}`, "application/json"}, {`{"code":"a","code":"b"}`, "application/json"},
		{`{"code":"a","Code":"b"}`, "application/json"}, {`{"unknown":true}`, "application/json"},
		{`{} {}`, "application/json"}, {strings.Repeat(" ", 4097), "application/json"},
	} {
		router := gin.New()
		router.POST("/token", h.Token)
		req := httptest.NewRequest(http.MethodPost, "/token", strings.NewReader(test.body))
		req.Header.Set("Content-Type", test.content)
		rec := httptest.NewRecorder()
		router.ServeHTTP(rec, req)
		require.Equal(t, http.StatusBadRequest, rec.Code)
		require.Contains(t, rec.Body.String(), "DESKTOP_AUTH_INVALID")
		require.Equal(t, "no-store", rec.Header().Get("Cache-Control"))
	}
}

func TestDesktopAuthHandlerAuthorizeRequiresAuthenticatedSubject(t *testing.T) {
	gin.SetMode(gin.TestMode)
	router := gin.New()
	router.POST("/authorize", NewDesktopAuthHandler(nil).Authorize)
	req := httptest.NewRequest(http.MethodPost, "/authorize", strings.NewReader(`{}`))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)
	require.Equal(t, http.StatusUnauthorized, rec.Code)
	require.Equal(t, "no-store", rec.Header().Get("Cache-Control"))
}
