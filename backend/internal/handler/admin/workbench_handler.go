package admin

import (
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"net/http"
	"strconv"
	"strings"

	"github.com/Wei-Shaw/sub2api/internal/pkg/adminaccess"
	"github.com/Wei-Shaw/sub2api/internal/pkg/response"
	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/gin-gonic/gin"
)

type WorkbenchHandler struct {
	service *service.WorkbenchAdminService
}

func NewWorkbenchHandler(svc *service.WorkbenchAdminService) *WorkbenchHandler {
	return &WorkbenchHandler{service: svc}
}

func allowWorkbenchAdmin(c *gin.Context) bool {
	// No resource-scoped permission currently grants a cross-user Studio view.
	// Keep the explicit-deny behavior even when this handler is called directly.
	if adminaccess.RestrictedFromContext(c.Request.Context()) != nil {
		response.ErrorWithDetails(c, http.StatusForbidden, "workbench administration requires full administrator access", "FORBIDDEN", nil)
		return false
	}
	c.Header("Cache-Control", "no-store")
	return true
}

func workbenchAdminError(c *gin.Context, err error) {
	if errors.Is(err, service.ErrWorkbenchInvalid) {
		response.ErrorWithDetails(c, http.StatusBadRequest, "invalid workbench query or configuration", "WORKBENCH_INVALID", nil)
		return
	}
	slog.Error("workbench admin operation failed", "error", err)
	response.ErrorWithDetails(c, http.StatusInternalServerError, "workbench operation failed", "WORKBENCH_ERROR", nil)
}

func workbenchQueryInt(c *gin.Context, name string, fallback int) (int, error) {
	values, exists := c.Request.URL.Query()[name]
	if !exists {
		return fallback, nil
	}
	if len(values) != 1 || values[0] == "" {
		return 0, service.ErrWorkbenchInvalid
	}
	raw := values[0]
	value, err := strconv.Atoi(raw)
	if err != nil {
		return 0, service.ErrWorkbenchInvalid
	}
	return value, nil
}

func (h *WorkbenchHandler) Overview(c *gin.Context) {
	if !allowWorkbenchAdmin(c) {
		return
	}
	days, err := workbenchQueryInt(c, "days", 7)
	if err != nil {
		workbenchAdminError(c, err)
		return
	}
	result, err := h.service.Overview(c.Request.Context(), days)
	if err != nil {
		workbenchAdminError(c, err)
		return
	}
	response.Success(c, result)
}

func (h *WorkbenchHandler) ListTasks(c *gin.Context) {
	if !allowWorkbenchAdmin(c) {
		return
	}
	page, err := workbenchQueryInt(c, "page", 1)
	if err != nil {
		workbenchAdminError(c, err)
		return
	}
	pageSize, err := workbenchQueryInt(c, "page_size", 20)
	if err != nil {
		workbenchAdminError(c, err)
		return
	}
	days, err := workbenchQueryInt(c, "days", 0)
	if err != nil {
		workbenchAdminError(c, err)
		return
	}
	var userID int64
	if raw, exists := c.GetQuery("user_id"); exists {
		userID, err = strconv.ParseInt(raw, 10, 64)
		if err != nil || userID <= 0 {
			workbenchAdminError(c, service.ErrWorkbenchInvalid)
			return
		}
	}
	result, err := h.service.ListTasks(c.Request.Context(), service.WorkbenchTaskFilter{Page: page, PageSize: pageSize, Days: days,
		Status: strings.TrimSpace(c.Query("status")), UserID: userID, Model: c.Query("model")})
	if err != nil {
		workbenchAdminError(c, err)
		return
	}
	response.Success(c, result)
}

func (h *WorkbenchHandler) GetConfig(c *gin.Context) {
	if !allowWorkbenchAdmin(c) {
		return
	}
	result, err := h.service.GetConfig(c.Request.Context())
	if err != nil {
		workbenchAdminError(c, err)
		return
	}
	response.Success(c, result)
}

func (h *WorkbenchHandler) UpdateConfig(c *gin.Context) {
	if !allowWorkbenchAdmin(c) {
		return
	}
	c.Request.Body = http.MaxBytesReader(c.Writer, c.Request.Body, 4096)
	var body struct {
		Enabled *bool  `json:"image_studio_enabled"`
		GroupID *int64 `json:"image_studio_group_id"`
	}
	decoder := json.NewDecoder(c.Request.Body)
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&body); err != nil || body.Enabled == nil || body.GroupID == nil {
		workbenchAdminError(c, service.ErrWorkbenchInvalid)
		return
	}
	if decoder.Decode(&struct{}{}) != io.EOF {
		workbenchAdminError(c, service.ErrWorkbenchInvalid)
		return
	}
	result, err := h.service.UpdateConfig(c.Request.Context(), service.WorkbenchConfig{ImageStudioEnabled: *body.Enabled, ImageStudioGroupID: *body.GroupID})
	if err != nil {
		workbenchAdminError(c, err)
		return
	}
	response.Success(c, result)
}
