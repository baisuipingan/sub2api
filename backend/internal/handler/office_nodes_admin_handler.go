package handler

import (
	"errors"
	"log/slog"
	"net/http"

	"github.com/Wei-Shaw/sub2api/internal/pkg/response"
	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/gin-gonic/gin"
)

func officeNodeAdminError(c *gin.Context, err error) bool {
	if err == nil {
		return false
	}
	switch {
	case errors.Is(err, service.ErrOfficeNodeInvalid):
		response.ErrorWithDetails(c, http.StatusBadRequest, "请检查节点名称、连接地址、令牌和并发设置。", "OFFICE_NODE_INVALID", nil)
	case errors.Is(err, service.ErrOfficeNodeBusy):
		response.ErrorWithDetails(c, http.StatusConflict, "节点还有已派发的任务。请先排空，再修改连接、停用或移除。", "OFFICE_NODE_BUSY", nil)
	case errors.Is(err, service.ErrOfficeNodeDuplicate):
		response.ErrorWithDetails(c, http.StatusConflict, "该连接地址或节点身份已登记。", "OFFICE_NODE_DUPLICATE", nil)
	case errors.Is(err, service.ErrOfficeNodeUnavailable):
		response.ErrorWithDetails(c, http.StatusBadGateway, "连接节点或验证节点令牌失败。", "OFFICE_NODE_UNAVAILABLE", nil)
	case errors.Is(err, service.ErrOfficeNodeIdentity):
		response.ErrorWithDetails(c, http.StatusConflict, "该地址返回了不同的节点身份。请核对实例，或在排空后登记新节点。", "OFFICE_NODE_IDENTITY_CHANGED", nil)
	case errors.Is(err, service.ErrOfficeNodeSecret):
		response.ErrorWithDetails(c, http.StatusServiceUnavailable, "节点令牌加密不可用，请检查站点的固定加密密钥。", "OFFICE_NODE_SECRET_UNAVAILABLE", nil)
	case errors.Is(err, service.ErrOfficeConflict):
		response.ErrorWithDetails(c, http.StatusConflict, "节点已被更新，请刷新后重试。", "OFFICE_NODE_CONFLICT", nil)
	case errors.Is(err, service.ErrOfficeNotFound):
		response.ErrorWithDetails(c, http.StatusNotFound, "节点已移除或不存在。", "OFFICE_NODE_NOT_FOUND", nil)
	default:
		// The registry protects credential-bearing configuration and transport diagnostics.
		slog.Warn("workbench office node administration failed", "operation", c.Request.Method)
		response.ErrorWithDetails(c, http.StatusInternalServerError, "无法完成节点管理操作。", "OFFICE_NODE_ERROR", nil)
	}
	return true
}

func (h *OfficeAgentHandler) AdminListOfficeNodes(c *gin.Context) {
	if !officeFullAdmin(c) {
		return
	}
	nodes, err := h.service.AdminOfficeNodes(c.Request.Context())
	if !officeNodeAdminError(c, err) {
		response.Success(c, gin.H{"items": nodes})
	}
}

func (h *OfficeAgentHandler) AdminSaveOfficeNode(c *gin.Context) {
	if !officeFullAdmin(c) {
		return
	}
	var in service.OfficeNodeUpdate
	if !officeDecode(c, &in, 4096) {
		return
	}
	node, err := h.service.SaveAdminOfficeNode(c.Request.Context(), c.Param("id"), in)
	if !officeNodeAdminError(c, err) {
		response.Success(c, node)
	}
}

func (h *OfficeAgentHandler) AdminCheckOfficeNode(c *gin.Context) {
	if !officeFullAdmin(c) {
		return
	}
	node, err := h.service.CheckAdminOfficeNode(c.Request.Context(), c.Param("id"))
	if !officeNodeAdminError(c, err) {
		response.Success(c, node)
	}
}

func (h *OfficeAgentHandler) AdminDeleteOfficeNode(c *gin.Context) {
	if !officeFullAdmin(c) {
		return
	}
	if !officeNodeAdminError(c, h.service.DeleteAdminOfficeNode(c.Request.Context(), c.Param("id"))) {
		c.Status(http.StatusNoContent)
	}
}
