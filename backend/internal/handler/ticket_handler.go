package handler

import (
	"context"
	"strconv"

	"github.com/Wei-Shaw/sub2api/internal/handler/ticketrequest"
	"strings"

	"github.com/Wei-Shaw/sub2api/internal/pkg/pagination"
	"github.com/Wei-Shaw/sub2api/internal/pkg/response"
	middleware2 "github.com/Wei-Shaw/sub2api/internal/server/middleware"
	"github.com/Wei-Shaw/sub2api/internal/service"

	"github.com/gin-gonic/gin"
)

// TicketHandler 用户侧工单接口：只看得到、只操作得了自己的工单。
type TicketHandler struct {
	ticketService *service.TicketService
	attachments   *service.TicketAttachmentService
}

func NewTicketHandler(ticketService *service.TicketService, attachments *service.TicketAttachmentService) *TicketHandler {
	return &TicketHandler{ticketService: ticketService, attachments: attachments}
}

// Create POST /api/v1/tickets; JSON text clients remain supported.
func (h *TicketHandler) Create(c *gin.Context) {
	subject, ok := middleware2.GetAuthSubjectFromContext(c)
	if !ok {
		response.Unauthorized(c, "User not authenticated")
		return
	}
	release, ok := h.prepareUpload(c)
	if !ok {
		return
	}
	defer release()
	req, err := ticketrequest.Parse(c)
	if err != nil {
		response.ErrorFrom(c, err)
		return
	}
	ticketrequest.Execute(c, "user:"+strconv.FormatInt(subject.UserID, 10), "ticket.create", 0, req,
		func(ctx context.Context, operationID string) (any, error) {
			return h.ticketService.CreateWithAttachments(ctx, subject.UserID, req.Title, req.Content, req.Priority, operationID, req.Uploads)
		})
}

// List GET /api/v1/tickets?status=&page=&page_size=
func (h *TicketHandler) List(c *gin.Context) {
	subject, ok := middleware2.GetAuthSubjectFromContext(c)
	if !ok {
		response.Unauthorized(c, "User not authenticated")
		return
	}
	page, pageSize := response.ParsePagination(c)
	params := pagination.PaginationParams{
		Page:      page,
		PageSize:  pageSize,
		SortBy:    strings.TrimSpace(c.Query("sort_by")),
		SortOrder: strings.TrimSpace(c.Query("sort_order")),
	}
	items, result, err := h.ticketService.ListMine(c.Request.Context(), subject.UserID, params, c.Query("status"))
	if err != nil {
		response.ErrorFrom(c, err)
		return
	}
	response.Paginated(c, items, result.Total, page, pageSize)
}

// Get GET /api/v1/tickets/:id
func (h *TicketHandler) Get(c *gin.Context) {
	subject, ok := middleware2.GetAuthSubjectFromContext(c)
	if !ok {
		response.Unauthorized(c, "User not authenticated")
		return
	}
	id, err := strconv.ParseInt(c.Param("id"), 10, 64)
	if err != nil || id <= 0 {
		response.BadRequest(c, "Invalid ticket id")
		return
	}
	ticket, messages, err := h.ticketService.GetDetail(c.Request.Context(), id, subject.UserID)
	if err != nil {
		response.ErrorFrom(c, err)
		return
	}
	response.Success(c, gin.H{"ticket": ticket, "messages": messages})
}

// Reply POST /api/v1/tickets/:id/messages.
func (h *TicketHandler) Reply(c *gin.Context) {
	subject, ok := middleware2.GetAuthSubjectFromContext(c)
	if !ok {
		response.Unauthorized(c, "User not authenticated")
		return
	}
	id, err := strconv.ParseInt(c.Param("id"), 10, 64)
	if err != nil || id <= 0 {
		response.BadRequest(c, "Invalid ticket id")
		return
	}
	release, ok := h.prepareUpload(c)
	if !ok {
		return
	}
	defer release()
	req, err := ticketrequest.Parse(c)
	if err != nil {
		response.ErrorFrom(c, err)
		return
	}
	ticketrequest.Execute(c, "user:"+strconv.FormatInt(subject.UserID, 10), "ticket.reply", id, req,
		func(ctx context.Context, operationID string) (any, error) {
			return h.ticketService.ReplyAsUserWithAttachments(ctx, id, subject.UserID, req.Content, operationID, req.Uploads)
		})
}

// prepareUpload bounds concurrent body buffering before multipart is consumed.
func (h *TicketHandler) prepareUpload(c *gin.Context) (func(), bool) {
	if !ticketrequest.IsMultipart(c) {
		return func() {}, true
	}
	release, err := h.attachments.AcquireUpload()
	if err != nil {
		response.ErrorFrom(c, err)
		return nil, false
	}
	return release, true
}

func (h *TicketHandler) AttachmentStatus(c *gin.Context) {
	enabled, err := h.attachments.Status(c.Request.Context())
	if err != nil {
		response.ErrorFrom(c, err)
		return
	}
	response.Success(c, gin.H{"enabled": enabled, "max_files": 3, "max_file_bytes": 2 << 20})
}

func (h *TicketHandler) AttachmentURL(c *gin.Context) {
	subject, ok := middleware2.GetAuthSubjectFromContext(c)
	if !ok {
		response.Unauthorized(c, "User not authenticated")
		return
	}
	id, err := strconv.ParseInt(c.Param("id"), 10, 64)
	if err != nil || id <= 0 {
		response.BadRequest(c, "Invalid ticket id")
		return
	}
	asset, err := h.attachments.GetURL(c.Request.Context(), id, c.Param("attachment_id"), subject.UserID, c.DefaultQuery("variant", "thumbnail"))
	if err != nil {
		response.ErrorFrom(c, err)
		return
	}
	c.Header("Cache-Control", "no-store")
	response.Success(c, asset)
}

// UnreadCount GET /api/v1/tickets/unread-count
// 侧栏红点用。放在 /:id 之前注册，避免被当成 id 解析。
func (h *TicketHandler) UnreadCount(c *gin.Context) {
	subject, ok := middleware2.GetAuthSubjectFromContext(c)
	if !ok {
		response.Unauthorized(c, "User not authenticated")
		return
	}
	count, err := h.ticketService.UnreadCount(c.Request.Context(), subject.UserID)
	if err != nil {
		response.ErrorFrom(c, err)
		return
	}
	response.Success(c, gin.H{"count": count})
}

// Delete DELETE /api/v1/tickets/:id
// 用户删自己的工单（清理误建）。软删除，数据仍在库里。
func (h *TicketHandler) Delete(c *gin.Context) {
	subject, ok := middleware2.GetAuthSubjectFromContext(c)
	if !ok {
		response.Unauthorized(c, "User not authenticated")
		return
	}
	id, err := strconv.ParseInt(c.Param("id"), 10, 64)
	if err != nil || id <= 0 {
		response.BadRequest(c, "Invalid ticket id")
		return
	}
	if err := h.ticketService.Delete(c.Request.Context(), id, subject.UserID); err != nil {
		response.ErrorFrom(c, err)
		return
	}
	response.Success(c, gin.H{"deleted": true})
}
