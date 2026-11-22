package handler

import (
	"github.com/Wei-Shaw/sub2api/internal/pkg/adminaccess"
	"github.com/Wei-Shaw/sub2api/internal/pkg/response"
	"github.com/Wei-Shaw/sub2api/internal/server/middleware"
	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/gin-gonic/gin"
	"net/http"
	"strconv"
	"strings"
)

func officeFullAdmin(c *gin.Context) bool {
	if adminaccess.RestrictedFromContext(c.Request.Context()) != nil {
		response.ErrorWithDetails(c, http.StatusForbidden, "workbench administration requires full administrator access", "FORBIDDEN", nil)
		return false
	}
	c.Header("Cache-Control", "no-store")
	return true
}
func officeAdminFilter(c *gin.Context) (service.OfficeAdminFilter, error) {
	f := service.OfficeAdminFilter{Page: 1, PageSize: 20, Mode: c.Query("mode"), Status: c.Query("status"), Model: c.Query("model"), Execution: c.Query("execution")}
	for name, ptr := range map[string]*int{"page": &f.Page, "page_size": &f.PageSize, "days": &f.Days} {
		if v, ok := c.GetQuery(name); ok {
			n, e := strconv.Atoi(v)
			if e != nil {
				return f, service.ErrOfficeInvalid
			}
			*ptr = n
		}
	}
	if v, ok := c.GetQuery("user_id"); ok {
		n, e := strconv.ParseInt(v, 10, 64)
		if e != nil || n <= 0 {
			return f, service.ErrOfficeInvalid
		}
		f.UserID = n
	}
	if f.Page < 1 || f.Page > 10000 || f.PageSize < 1 || f.PageSize > 100 || f.Days < 0 || f.Days > 90 || len(f.Model) > 128 || (f.Mode != "" && f.Mode != "chat" && f.Mode != "office") {
		return f, service.ErrOfficeInvalid
	}
	if f.Status != "" && !strings.Contains("|queued|running|recovering|waiting_user|needs_confirmation|completed|partially_completed|failed|cancelled|", "|"+f.Status+"|") {
		return f, service.ErrOfficeInvalid
	}
	if f.Execution != "" && f.Execution != "browser" && f.Execution != "historical" {
		return f, service.ErrOfficeInvalid
	}
	return f, nil
}
func (h *OfficeAgentHandler) AdminOverview(c *gin.Context) {
	if !officeFullAdmin(c) {
		return
	}
	days := 7
	if raw, ok := c.GetQuery("days"); ok {
		var e error
		days, e = strconv.Atoi(raw)
		if e != nil {
			officeHandlerError(c, service.ErrOfficeInvalid)
			return
		}
	}
	result, e := h.service.AdminOverview(c.Request.Context(), days)
	if officeHandlerError(c, e) {
		return
	}
	response.Success(c, result)
}

func (h *OfficeAgentHandler) AdminSearchUsage(c *gin.Context) {
	c.Header("Cache-Control", "no-store")
	page, pageSize := response.ParsePagination(c)
	start, end, ok := workbenchSearchUsageDates(c)
	if !ok {
		return
	}
	var userID int64
	if raw, exists := c.GetQuery("user_id"); exists {
		id, err := strconv.ParseInt(raw, 10, 64)
		if err != nil || id <= 0 {
			response.BadRequest(c, "Invalid user_id")
			return
		}
		userID = id
	}
	items, total, err := h.service.ListAdminSearchCharges(c.Request.Context(), userID, page, pageSize, start, end)
	if officeHandlerError(c, err) {
		return
	}
	response.Paginated(c, items, total, page, pageSize)
}

func (h *OfficeAgentHandler) AdminRuns(c *gin.Context) {
	if !officeFullAdmin(c) {
		return
	}
	filter, e := officeAdminFilter(c)
	if officeHandlerError(c, e) {
		return
	}
	repo, e := h.service.AdminRepository()
	if officeHandlerError(c, e) {
		return
	}
	result, e := repo.AdminRuns(c.Request.Context(), filter)
	if officeHandlerError(c, e) {
		return
	}
	response.Success(c, result)
}
func (h *OfficeAgentHandler) AdminRun(c *gin.Context) {
	if !officeFullAdmin(c) {
		return
	}
	repo, e := h.service.AdminRepository()
	if officeHandlerError(c, e) {
		return
	}
	result, e := repo.AdminRun(c.Request.Context(), c.Param("id"))
	if officeHandlerError(c, e) {
		return
	}
	response.Success(c, result)
}

func (h *OfficeAgentHandler) AdminRunMessages(c *gin.Context) {
	if !officeFullAdmin(c) {
		return
	}
	var before int64
	if raw, ok := c.GetQuery("before_seq"); ok {
		parsed, err := strconv.ParseInt(raw, 10, 64)
		if err != nil || parsed < 0 {
			officeHandlerError(c, service.ErrOfficeInvalid)
			return
		}
		before = parsed
	}
	repo, err := h.service.AdminRepository()
	if officeHandlerError(c, err) {
		return
	}
	result, err := repo.AdminRunMessages(c.Request.Context(), c.Param("id"), before)
	if !officeHandlerError(c, err) {
		response.Success(c, result)
	}
}

func (h *OfficeAgentHandler) AdminToolDetail(c *gin.Context) {
	if !officeFullAdmin(c) {
		return
	}
	item, err := h.service.WorkbenchToolDetail(c.Request.Context(), 0, c.Param("id"), c.Param("operation"), true)
	if !officeHandlerError(c, err) {
		response.Success(c, item)
	}
}

func (h *OfficeAgentHandler) AdminResources(c *gin.Context) {
	if !officeFullAdmin(c) {
		return
	}
	filter, e := officeAdminFilter(c)
	if officeHandlerError(c, e) {
		return
	}
	repo, e := h.service.AdminRepository()
	if officeHandlerError(c, e) {
		return
	}
	result, e := repo.AdminResources(c.Request.Context(), filter)
	if officeHandlerError(c, e) {
		return
	}
	response.Success(c, result)
}
func (h *OfficeAgentHandler) AdminResourceAccess(c *gin.Context) {
	if !officeFullAdmin(c) {
		return
	}
	variant := c.DefaultQuery("variant", "preview")
	if variant != "preview" && variant != "original" {
		officeHandlerError(c, service.ErrOfficeInvalid)
		return
	}
	middleware.SetAuditAction(c, "workbench.resource.access")
	result, e := h.service.AdminResourceAccess(c.Request.Context(), c.Param("version_id"), variant)
	if officeHandlerError(c, e) {
		return
	}
	response.Success(c, result)
}
