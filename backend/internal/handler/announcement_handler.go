package handler

import (
	"net/http"
	"strconv"
	"strings"

	"github.com/Wei-Shaw/sub2api/internal/handler/dto"
	"github.com/Wei-Shaw/sub2api/internal/pkg/pagination"
	"github.com/Wei-Shaw/sub2api/internal/pkg/response"
	middleware2 "github.com/Wei-Shaw/sub2api/internal/server/middleware"
	"github.com/Wei-Shaw/sub2api/internal/service"

	"github.com/gin-gonic/gin"
)

// AnnouncementHandler handles user announcement operations
type AnnouncementHandler struct {
	announcementService *service.AnnouncementService
}

// NewAnnouncementHandler creates a new user announcement handler
func NewAnnouncementHandler(announcementService *service.AnnouncementService) *AnnouncementHandler {
	return &AnnouncementHandler{
		announcementService: announcementService,
	}
}

// List handles listing announcements visible to current user
// GET /api/v1/announcements
func (h *AnnouncementHandler) List(c *gin.Context) {
	subject, ok := middleware2.GetAuthSubjectFromContext(c)
	if !ok {
		response.Unauthorized(c, "User not found in context")
		return
	}

	unreadOnly := parseBoolQuery(c.Query("unread_only"))

	items, err := h.announcementService.ListForUser(c.Request.Context(), subject.UserID, unreadOnly)
	if err != nil {
		response.ErrorFrom(c, err)
		return
	}

	out := make([]dto.UserAnnouncement, 0, len(items))
	for i := range items {
		out = append(out, *dto.UserAnnouncementFromService(&items[i]))
	}
	response.Success(c, out)
}

// MarkRead marks an announcement as read for current user
// POST /api/v1/announcements/:id/read
func (h *AnnouncementHandler) MarkRead(c *gin.Context) {
	subject, ok := middleware2.GetAuthSubjectFromContext(c)
	if !ok {
		response.Unauthorized(c, "User not found in context")
		return
	}

	announcementID, err := strconv.ParseInt(c.Param("id"), 10, 64)
	if err != nil || announcementID <= 0 {
		response.BadRequest(c, "Invalid announcement ID")
		return
	}

	if err := h.announcementService.MarkRead(c.Request.Context(), subject.UserID, announcementID); err != nil {
		response.ErrorFrom(c, err)
		return
	}

	response.Success(c, gin.H{"message": "ok"})
}

// Summary returns complete visible timeline counts and pending popup content.
func (h *AnnouncementHandler) Summary(c *gin.Context) {
	subject, ok := middleware2.GetAuthSubjectFromContext(c)
	if !ok {
		response.Unauthorized(c, "User not found in context")
		return
	}
	result, err := h.announcementService.CenterSummary(c.Request.Context(), subject.UserID)
	if err != nil {
		response.ErrorFrom(c, err)
		return
	}
	c.Header("Cache-Control", "no-store")
	response.Success(c, dto.AnnouncementCenterSummaryFromService(result))
}

func (h *AnnouncementHandler) Center(c *gin.Context) {
	subject, ok := middleware2.GetAuthSubjectFromContext(c)
	if !ok {
		response.Unauthorized(c, "User not found in context")
		return
	}
	page, pageSize, err := announcementCenterPagination(c)
	if err != nil {
		response.BadRequest(c, "Invalid announcement pagination")
		return
	}
	mode := c.DefaultQuery("display_mode", "timeline")
	items, result, err := h.announcementService.ListCenterForUser(c.Request.Context(), subject.UserID, mode, pagination.PaginationParams{Page: page, PageSize: pageSize})
	if err != nil {
		response.ErrorFrom(c, err)
		return
	}
	out := make([]dto.UserAnnouncement, 0, len(items))
	for i := range items {
		out = append(out, *dto.UserAnnouncementFromService(&items[i]))
	}
	c.Header("Cache-Control", "no-store")
	response.Paginated(c, out, result.Total, page, pageSize)
}

func announcementCenterPagination(c *gin.Context) (int, int, error) {
	page, pageSize := 1, 20
	var err error
	if raw, exists := c.GetQuery("page"); exists {
		page, err = strconv.Atoi(raw)
		if err != nil || page < 1 || page > 10000 {
			return 0, 0, strconv.ErrSyntax
		}
	}
	if raw, exists := c.GetQuery("page_size"); exists {
		pageSize, err = strconv.Atoi(raw)
		if err != nil || pageSize < 1 || pageSize > 100 {
			return 0, 0, strconv.ErrSyntax
		}
	}
	return page, pageSize, nil
}

func (h *AnnouncementHandler) Get(c *gin.Context) {
	subject, ok := middleware2.GetAuthSubjectFromContext(c)
	if !ok {
		response.Unauthorized(c, "User not found in context")
		return
	}
	id, err := strconv.ParseInt(c.Param("id"), 10, 64)
	if err != nil || id <= 0 {
		response.BadRequest(c, "Invalid announcement ID")
		return
	}
	item, err := h.announcementService.GetForUser(c.Request.Context(), subject.UserID, id)
	if err != nil {
		response.ErrorFrom(c, err)
		return
	}
	c.Header("Cache-Control", "no-store")
	response.Success(c, dto.UserAnnouncementFromService(item))
}

func (h *AnnouncementHandler) MarkAllRead(c *gin.Context) {
	subject, ok := middleware2.GetAuthSubjectFromContext(c)
	if !ok {
		response.Unauthorized(c, "User not found in context")
		return
	}
	c.Request.Body = http.MaxBytesReader(c.Writer, c.Request.Body, 256<<10)
	var request struct {
		Snapshot string `json:"snapshot" binding:"required"`
	}
	if err := c.ShouldBindJSON(&request); err != nil {
		response.BadRequest(c, "Invalid announcement read snapshot")
		return
	}
	result, err := h.announcementService.MarkAllTimelineRead(c.Request.Context(), subject.UserID, request.Snapshot)
	if err != nil {
		response.ErrorFrom(c, err)
		return
	}
	middleware2.SetAuditExtra(c, map[string]any{"result": "success", "matched_count": len(result.MarkedIDs)})
	c.Header("Cache-Control", "no-store")
	response.Success(c, gin.H{"marked_ids": result.MarkedIDs, "summary": dto.AnnouncementCenterSummaryFromService(&result.Summary)})
}

func parseBoolQuery(v string) bool {
	switch strings.TrimSpace(strings.ToLower(v)) {
	case "1", "true", "yes", "y", "on":
		return true
	default:
		return false
	}
}
