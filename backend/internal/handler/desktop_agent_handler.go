package handler

import (
	"errors"
	"io"
	"net/http"
	"strconv"
	"strings"

	"github.com/Wei-Shaw/sub2api/internal/pkg/adminaccess"
	"github.com/Wei-Shaw/sub2api/internal/pkg/response"
	"github.com/Wei-Shaw/sub2api/internal/server/middleware"
	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/gin-gonic/gin"
)

type DesktopAgentHandler struct{ service *service.DesktopAgentService }

func NewDesktopAgentHandler(svc *service.DesktopAgentService) *DesktopAgentHandler {
	return &DesktopAgentHandler{service: svc}
}

func desktopAgentError(c *gin.Context, err error) {
	if errors.Is(err, service.ErrDesktopAgentInvalid) {
		response.ErrorWithDetails(c, http.StatusBadRequest, "invalid desktop agent request or configuration", "DESKTOP_AGENT_INVALID", nil)
		return
	}
	// 不把配置、数据库异常或托管密钥拼进客户端错误/日志。
	response.ErrorWithDetails(c, http.StatusServiceUnavailable, "desktop agent service is unavailable", "DESKTOP_AGENT_UNAVAILABLE", nil)
}
func readDesktopBody(c *gin.Context, max int64) ([]byte, error) {
	c.Request.Body = http.MaxBytesReader(c.Writer, c.Request.Body, max)
	data, err := io.ReadAll(c.Request.Body)
	if err != nil {
		return nil, service.ErrDesktopAgentInvalid
	}
	return data, nil
}
func (h *DesktopAgentHandler) Bootstrap(c *gin.Context) {
	c.Header("Cache-Control", "no-store")
	subject, ok := middleware.GetAuthSubjectFromContext(c)
	if !ok {
		response.ErrorWithDetails(c, http.StatusUnauthorized, "authentication required", "UNAUTHORIZED", nil)
		return
	}
	raw, err := readDesktopBody(c, 2048)
	if err != nil {
		desktopAgentError(c, err)
		return
	}
	req, err := service.ParseDesktopAgentRequest(raw)
	if err != nil {
		desktopAgentError(c, err)
		return
	}
	result, err := h.service.BootstrapForProtocol(c.Request.Context(), subject.UserID, req.ProtocolVersion)
	if err != nil {
		desktopAgentError(c, err)
		return
	}
	response.Success(c, result)
}
func allowDesktopAgentAdmin(c *gin.Context) bool {
	c.Header("Cache-Control", "no-store")
	if adminaccess.RestrictedFromContext(c.Request.Context()) != nil {
		response.ErrorWithDetails(c, http.StatusForbidden, "desktop configuration requires full administrator access", "FORBIDDEN", nil)
		return false
	}
	return true
}
func (h *DesktopAgentHandler) GetConfig(c *gin.Context) {
	if !allowDesktopAgentAdmin(c) {
		return
	}
	result, err := h.service.GetConfig(c.Request.Context())
	if err != nil {
		desktopAgentError(c, err)
		return
	}
	response.Success(c, result)
}
func (h *DesktopAgentHandler) UpdateConfig(c *gin.Context) {
	if !allowDesktopAgentAdmin(c) {
		return
	}
	raw, err := readDesktopBody(c, 131072)
	if err != nil {
		desktopAgentError(c, err)
		return
	}
	result, err := h.service.UpdateConfigJSON(c.Request.Context(), raw)
	if err != nil {
		desktopAgentError(c, err)
		return
	}
	response.Success(c, result)
}

func (h *DesktopAgentHandler) GetModels(c *gin.Context) {
	if !allowDesktopAgentAdmin(c) {
		return
	}
	values, present := c.Request.URL.Query()["group_id"]
	if !present || len(values) != 1 || values[0] == "" || strings.TrimSpace(values[0]) != values[0] {
		desktopAgentError(c, service.ErrDesktopAgentInvalid)
		return
	}
	groupID, err := strconv.ParseInt(values[0], 10, 64)
	if err != nil || groupID <= 0 {
		desktopAgentError(c, service.ErrDesktopAgentInvalid)
		return
	}
	models, err := h.service.GetModels(c.Request.Context(), groupID)
	if err != nil {
		desktopAgentError(c, err)
		return
	}
	response.Success(c, gin.H{"group_id": groupID, "models": models})
}
func (h *DesktopAgentHandler) DisableConfig(c *gin.Context) {
	if !allowDesktopAgentAdmin(c) {
		return
	}
	// 关闭不是表单保存：拒绝任何携带草稿的body，避免操作者误以为修改已保存。
	raw, err := readDesktopBody(c, 1)
	if err != nil || len(raw) != 0 {
		desktopAgentError(c, service.ErrDesktopAgentInvalid)
		return
	}
	result, err := h.service.DisableConfig(c.Request.Context())
	if err != nil {
		desktopAgentError(c, err)
		return
	}
	response.Success(c, result)
}
