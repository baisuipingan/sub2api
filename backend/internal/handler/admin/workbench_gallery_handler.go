package admin

import (
	"errors"
	"net/http"
	"strconv"

	"github.com/Wei-Shaw/sub2api/internal/pkg/response"
	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/gin-gonic/gin"
)

func (h *WorkbenchHandler) ListGallery(c *gin.Context) {
	if !allowWorkbenchAdmin(c) {
		return
	}
	page, err := workbenchQueryInt(c, "page", 1)
	if err != nil {
		workbenchAdminError(c, err)
		return
	}
	size, err := workbenchQueryInt(c, "page_size", 20)
	if err != nil {
		workbenchAdminError(c, err)
		return
	}
	var userID int64
	if raw, ok := c.GetQuery("user_id"); ok {
		userID, err = strconv.ParseInt(raw, 10, 64)
		if err != nil || userID <= 0 {
			workbenchAdminError(c, service.ErrWorkbenchInvalid)
			return
		}
	}
	result, err := h.service.ListGallery(c.Request.Context(), service.WorkbenchGalleryFilter{Page: page, PageSize: size, UserID: userID, TaskID: c.Query("task_id")})
	if err != nil {
		workbenchAdminError(c, err)
		return
	}
	response.Success(c, result)
}

func (h *WorkbenchHandler) GalleryImageURL(c *gin.Context) {
	if !allowWorkbenchAdmin(c) {
		return
	}
	userID, err := strconv.ParseInt(c.Param("user_id"), 10, 64)
	if err != nil {
		workbenchAdminError(c, service.ErrWorkbenchInvalid)
		return
	}
	variant := c.DefaultQuery("variant", "thumbnail")
	result, err := h.service.GalleryImageURL(c.Request.Context(), userID, c.Param("task_id"), c.Param("image_id"), variant)
	if errors.Is(err, service.ErrGalleryNotFound) || errors.Is(err, service.ErrGalleryGone) {
		response.ErrorWithDetails(c, http.StatusNotFound, "gallery image not found", "GALLERY_NOT_FOUND", nil)
		return
	}
	if err != nil {
		workbenchAdminError(c, err)
		return
	}
	response.Success(c, result)
}
