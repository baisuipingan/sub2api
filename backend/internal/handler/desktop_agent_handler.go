package handler

import (
	"errors"
	"io"
	"net/http"

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
	if _, err = service.ParseDesktopAgentRequest(raw); err != nil {
		desktopAgentError(c, err)
		return
	}
	result, err := h.service.Bootstrap(c.Request.Context(), subject.UserID)
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
	raw, err := readDesktopBody(c, 16384)
	if err != nil {
		desktopAgentError(c, err)
		return
	}
	cfg, err := service.ParseDesktopAgentConfig(raw)
	if err != nil {
		desktopAgentError(c, err)
		return
	}
	result, err := h.service.UpdateConfig(c.Request.Context(), cfg)
	if err != nil {
		desktopAgentError(c, err)
		return
	}
	response.Success(c, result)
}
