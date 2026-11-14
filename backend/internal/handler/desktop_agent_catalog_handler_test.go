package handler

import (
	"net/http"
	"testing"

	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/stretchr/testify/require"
)

func TestDesktopAgentCatalogSourceBodyRejectsAmbiguityAndRestrictedAdmins(t *testing.T) {
	h := NewDesktopAgentHandler(service.NewDesktopAgentService(nil, nil, nil, nil))
	router := desktopHandlerRouter(h, true, false)
	router.PUT("/catalog/source", h.SetProfileCatalogSource)
	router.POST("/catalog/refresh", h.RefreshProfileCatalog)
	for _, body := range []string{`{}`, `{"source_url":null}`, `{"source_url":"","source_url":"https://example.com/file"}`, `{"Source_URL":""}`, `{"source_url":"","group_id":12}`, `{"source_url":""}{}`} {
		result := desktopRequest(t, router, http.MethodPut, "/catalog/source", body)
		require.Equal(t, http.StatusBadRequest, result.Code, body)
	}
	result := desktopRequest(t, router, http.MethodPost, "/catalog/refresh", "{}")
	require.Equal(t, http.StatusBadRequest, result.Code)
	restricted := desktopHandlerRouter(h, true, true)
	restricted.GET("/catalog", h.GetProfileCatalogStatus)
	restricted.POST("/catalog/refresh", h.RefreshProfileCatalog)
	restricted.PUT("/catalog/source", h.SetProfileCatalogSource)
	for _, endpoint := range []struct{ method, path string }{{"GET", "/catalog"}, {"POST", "/catalog/refresh"}, {"PUT", "/catalog/source"}} {
		result := desktopRequest(t, restricted, endpoint.method, endpoint.path, "")
		require.Equal(t, 403, result.Code)
	}
}
