package handler

import (
	"encoding/json"
	"net/http"

	"github.com/Wei-Shaw/sub2api/internal/pkg/response"
	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/gin-gonic/gin"
)

func (h *DesktopAgentHandler) GetProfileCatalogStatus(c *gin.Context) {
	if !allowDesktopAgentAdmin(c) {
		return
	}
	response.Success(c, h.service.ProfileCatalogStatus())
}
func (h *DesktopAgentHandler) RefreshProfileCatalog(c *gin.Context) {
	if !allowDesktopAgentAdmin(c) {
		return
	}
	raw, err := readDesktopBody(c, 1)
	if err != nil || len(raw) != 0 {
		response.ErrorWithDetails(c, http.StatusBadRequest, "invalid catalog request", "DESKTOP_AGENT_INVALID", nil)
		return
	}
	if err = h.service.RefreshProfileCatalog(c.Request.Context()); err != nil {
		response.ErrorWithDetails(c, http.StatusServiceUnavailable, "profile catalog refresh failed; the last valid catalog is retained", "DESKTOP_PROFILE_CATALOG_UNAVAILABLE", nil)
		return
	}
	response.Success(c, h.service.ProfileCatalogStatus())
}
func (h *DesktopAgentHandler) SetProfileCatalogSource(c *gin.Context) {
	if !allowDesktopAgentAdmin(c) {
		return
	}
	raw, err := readDesktopBody(c, 4096)
	var fields map[string]json.RawMessage
	if err != nil || json.Unmarshal(raw, &fields) != nil || len(fields) != 1 {
		response.ErrorWithDetails(c, http.StatusBadRequest, "invalid catalog source", "DESKTOP_AGENT_INVALID", nil)
		return
	}
	var source string
	// A tiny exact object keeps the source setting separate from the model draft.
	var body struct {
		SourceURL string `json:"source_url"`
	}
	value, ok := fields["source_url"]
	if !ok || string(value) == "null" || json.Unmarshal(value, &source) != nil || json.Unmarshal(raw, &body) != nil {
		response.ErrorWithDetails(c, http.StatusBadRequest, "invalid catalog source", "DESKTOP_AGENT_INVALID", nil)
		return
	}
	// Reject duplicate field spellings as well as duplicate source_url keys.
	if !service.ValidDesktopAgentProfileSourceBody(raw) {
		response.ErrorWithDetails(c, http.StatusBadRequest, "invalid catalog source", "DESKTOP_AGENT_INVALID", nil)
		return
	}
	if err = h.service.SetProfileCatalogSource(c.Request.Context(), source); err != nil {
		desktopAgentError(c, err)
		return
	}
	response.Success(c, h.service.ProfileCatalogStatus())
}
