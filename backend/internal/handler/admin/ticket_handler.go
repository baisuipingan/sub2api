package admin

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

// TicketHandler 管理端工单接口：可看全部用户的工单、回复、改状态。
type TicketHandler struct {
	ticketService *service.TicketService
	attachments   *service.TicketAttachmentService
}

func NewTicketHandler(ticketService *service.TicketService, attachments *service.TicketAttachmentService) *TicketHandler {
	return &TicketHandler{ticketService: ticketService, attachments: attachments}
}

// List GET /api/v1/admin/tickets?status=&search=&page=&page_size=
func (h *TicketHandler) List(c *gin.Context) {
	page, pageSize := response.ParsePagination(c)
	params := pagination.PaginationParams{
		Page:      page,
		PageSize:  pageSize,
		SortBy:    strings.TrimSpace(c.Query("sort_by")),
		SortOrder: strings.TrimSpace(c.Query("sort_order")),
	}
	filters := service.TicketListFilters{
		Status: c.Query("status"),
		Search: c.Query("search"),
	}
	items, result, err := h.ticketService.ListAll(c.Request.Context(), params, filters)
	if err != nil {
		response.ErrorFrom(c, err)
		return
	}
	response.Paginated(c, items, result.Total, page, pageSize)
}

// Get GET /api/v1/admin/tickets/:id
func (h *TicketHandler) Get(c *gin.Context) {
	id, err := strconv.ParseInt(c.Param("id"), 10, 64)
	if err != nil || id <= 0 {
		response.BadRequest(c, "Invalid ticket id")
		return
	}
	// userID 传 0 表示管理端，跳过归属校验
	ticket, messages, err := h.ticketService.GetDetail(c.Request.Context(), id, 0)
	if err != nil {
		response.ErrorFrom(c, err)
		return
	}
	response.Success(c, gin.H{"ticket": ticket, "messages": messages})
}

// Reply POST /api/v1/admin/tickets/:id/messages.
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
	if ticketrequest.IsMultipart(c) {
		release, err := h.attachments.AcquireUpload()
		if err != nil {
			response.ErrorFrom(c, err)
			return
		}
		defer release()
	}
	req, err := ticketrequest.Parse(c)
	if err != nil {
		response.ErrorFrom(c, err)
		return
	}
	ticketrequest.Execute(c, adminActorScope(c), "ticket.admin.reply", id, req,
		func(ctx context.Context, operationID string) (any, error) {
			return h.ticketService.ReplyAsAdminWithAttachments(ctx, id, subject.UserID, req.Content, operationID, req.Uploads)
		})
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
	id, err := strconv.ParseInt(c.Param("id"), 10, 64)
	if err != nil || id <= 0 {
		response.BadRequest(c, "Invalid ticket id")
		return
	}
	asset, err := h.attachments.GetURL(c.Request.Context(), id, c.Param("attachment_id"), 0, c.DefaultQuery("variant", "thumbnail"))
	if err != nil {
		response.ErrorFrom(c, err)
		return
	}
	c.Header("Cache-Control", "no-store")
	response.Success(c, asset)
}

type updateTicketRequest struct {
	Status string `json:"status"`
}

// Update PUT /api/v1/admin/tickets/:id
func (h *TicketHandler) Update(c *gin.Context) {
	id, err := strconv.ParseInt(c.Param("id"), 10, 64)
	if err != nil || id <= 0 {
		response.BadRequest(c, "Invalid ticket id")
		return
	}
	var req updateTicketRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		response.BadRequest(c, "Invalid request: "+err.Error())
		return
	}
	ticket, err := h.ticketService.UpdateStatus(c.Request.Context(), id, req.Status)
	if err != nil {
		response.ErrorFrom(c, err)
		return
	}
	response.Success(c, ticket)
}

// UnreadCount GET /api/v1/admin/tickets/unread-count
func (h *TicketHandler) UnreadCount(c *gin.Context) {
	count, err := h.ticketService.UnreadCount(c.Request.Context(), 0)
	if err != nil {
		response.ErrorFrom(c, err)
		return
	}
	response.Success(c, gin.H{"count": count})
}

// Delete DELETE /api/v1/admin/tickets/:id
func (h *TicketHandler) Delete(c *gin.Context) {
	id, err := strconv.ParseInt(c.Param("id"), 10, 64)
	if err != nil || id <= 0 {
		response.BadRequest(c, "Invalid ticket id")
		return
	}
	if err := h.ticketService.Delete(c.Request.Context(), id, 0); err != nil {
		response.ErrorFrom(c, err)
		return
	}
	response.Success(c, gin.H{"deleted": true})
}
