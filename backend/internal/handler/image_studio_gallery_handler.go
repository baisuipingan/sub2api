package handler

import (
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"strconv"
	"strings"

	"github.com/Wei-Shaw/sub2api/internal/pkg/response"
	"github.com/Wei-Shaw/sub2api/internal/server/middleware"
	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/gin-gonic/gin"
)

type ImageStudioGalleryHandler struct {
	gallery *service.ImageStudioGalleryService
}

func NewImageStudioGalleryHandler(gallery *service.ImageStudioGalleryService) *ImageStudioGalleryHandler {
	return &ImageStudioGalleryHandler{gallery: gallery}
}

func (h *ImageStudioGalleryHandler) userID(c *gin.Context) (int64, bool) {
	subject, ok := middleware.GetAuthSubjectFromContext(c)
	if !ok || subject.UserID <= 0 {
		response.ErrorWithDetails(c, http.StatusUnauthorized, "authentication required", "UNAUTHORIZED", nil)
		return 0, false
	}
	return subject.UserID, true
}

func galleryHTTPError(c *gin.Context, err error) {
	switch {
	case errors.Is(err, service.ErrGalleryDisabled):
		response.ErrorWithDetails(c, http.StatusServiceUnavailable, "gallery uploads are disabled", "GALLERY_DISABLED", nil)
	case errors.Is(err, service.ErrGalleryBusy):
		response.ErrorWithDetails(c, http.StatusServiceUnavailable, "gallery image processing is busy", "GALLERY_BUSY", nil)
	case errors.Is(err, service.ErrGalleryQuota):
		response.ErrorWithDetails(c, http.StatusTooManyRequests, "gallery storage quota exceeded", "GALLERY_QUOTA_EXCEEDED", nil)
	case errors.Is(err, service.ErrGalleryUnavailable):
		response.ErrorWithDetails(c, http.StatusServiceUnavailable, "gallery storage is unavailable", "GALLERY_UNAVAILABLE", nil)
	case errors.Is(err, service.ErrGalleryNotFound):
		response.ErrorWithDetails(c, http.StatusNotFound, "gallery item not found", "GALLERY_NOT_FOUND", nil)
	case errors.Is(err, service.ErrGalleryGone):
		response.ErrorWithDetails(c, http.StatusGone, "gallery task was deleted", "GALLERY_GONE", nil)
	case errors.Is(err, service.ErrGalleryConflict):
		response.ErrorWithDetails(c, http.StatusConflict, "gallery item conflicts with existing content", "GALLERY_CONFLICT", nil)
	case errors.Is(err, service.ErrGalleryInvalid):
		response.ErrorWithDetails(c, http.StatusBadRequest, "invalid gallery item", "GALLERY_INVALID", nil)
	case errors.Is(err, service.ErrGalleryPublicStorage):
		response.ErrorWithDetails(c, http.StatusBadRequest, "gallery requires private object storage", "GALLERY_PUBLIC_STORAGE", nil)
	default:
		response.ErrorWithDetails(c, http.StatusInternalServerError, "gallery operation failed", "GALLERY_ERROR", nil)
	}
}

func (h *ImageStudioGalleryHandler) Status(c *gin.Context) {
	userID, ok := h.userID(c)
	if !ok {
		return
	}
	enabled, err := h.gallery.Status(c.Request.Context())
	if err != nil {
		galleryHTTPError(c, err)
		return
	}
	c.Header("Cache-Control", "no-store")
	response.Success(c, gin.H{"enabled": enabled, "user_id": userID})
}

func (h *ImageStudioGalleryHandler) PutAsset(c *gin.Context) {
	userID, ok := h.userID(c)
	if !ok {
		return
	}
	enabled, err := h.gallery.Status(c.Request.Context())
	if err != nil {
		galleryHTTPError(c, err)
		return
	}
	if !enabled {
		galleryHTTPError(c, service.ErrGalleryDisabled)
		return
	}
	release, err := h.gallery.AcquireConversion()
	if err != nil {
		galleryHTTPError(c, err)
		return
	}
	defer release()
	c.Request.Body = http.MaxBytesReader(c.Writer, c.Request.Body, service.GalleryMaxImageBytes)
	data, err := io.ReadAll(c.Request.Body)
	if err != nil {
		var maxErr *http.MaxBytesError
		if errors.As(err, &maxErr) {
			response.ErrorWithDetails(c, http.StatusRequestEntityTooLarge, "gallery image is too large", "GALLERY_TOO_LARGE", nil)
			return
		}
		galleryHTTPError(c, service.ErrGalleryInvalid)
		return
	}
	id := c.Param("image_id")
	if err := h.gallery.PutAsset(c.Request.Context(), userID, id, c.GetHeader("Content-Type"), data); err != nil {
		galleryHTTPError(c, err)
		return
	}
	response.Success(c, gin.H{"id": id})
}

func (h *ImageStudioGalleryHandler) SaveTask(c *gin.Context) {
	userID, ok := h.userID(c)
	if !ok {
		return
	}
	c.Request.Body = http.MaxBytesReader(c.Writer, c.Request.Body, 256<<10)
	var request struct {
		Task json.RawMessage `json:"task"`
	}
	if err := c.ShouldBindJSON(&request); err != nil {
		var maxErr *http.MaxBytesError
		if errors.As(err, &maxErr) {
			response.ErrorWithDetails(c, http.StatusRequestEntityTooLarge, "gallery task is too large", "GALLERY_TOO_LARGE", nil)
			return
		}
		galleryHTTPError(c, service.ErrGalleryInvalid)
		return
	}
	task, err := h.gallery.SaveTask(c.Request.Context(), userID, request.Task)
	if err != nil {
		galleryHTTPError(c, err)
		return
	}
	response.Success(c, gin.H{"task": task})
}

func (h *ImageStudioGalleryHandler) ListTasks(c *gin.Context) {
	userID, ok := h.userID(c)
	if !ok {
		return
	}
	limit := 20
	if raw := strings.TrimSpace(c.Query("limit")); raw != "" {
		parsed, err := strconv.Atoi(raw)
		if err != nil || parsed < 1 || parsed > 100 {
			galleryHTTPError(c, service.ErrGalleryInvalid)
			return
		}
		limit = parsed
	}
	page, err := h.gallery.ListTasks(c.Request.Context(), userID, c.Query("cursor"), limit)
	if err != nil {
		galleryHTTPError(c, err)
		return
	}
	c.Header("Cache-Control", "no-store")
	response.Success(c, page)
}

func (h *ImageStudioGalleryHandler) GetTask(c *gin.Context) {
	userID, ok := h.userID(c)
	if !ok {
		return
	}
	task, err := h.gallery.GetTask(c.Request.Context(), userID, c.Param("id"))
	if err != nil {
		galleryHTTPError(c, err)
		return
	}
	c.Header("Cache-Control", "no-store")
	response.Success(c, gin.H{"task": task})
}

func (h *ImageStudioGalleryHandler) AssetURL(c *gin.Context) {
	userID, ok := h.userID(c)
	if !ok {
		return
	}
	url, err := h.gallery.AssetURL(c.Request.Context(), userID, c.Param("image_id"), c.DefaultQuery("variant", "original"))
	if err != nil {
		galleryHTTPError(c, err)
		return
	}
	c.Header("Cache-Control", "no-store")
	response.Success(c, url)
}

func (h *ImageStudioGalleryHandler) DeleteTask(c *gin.Context) {
	userID, ok := h.userID(c)
	if !ok {
		return
	}
	if err := h.gallery.DeleteTask(c.Request.Context(), userID, c.Param("id")); err != nil {
		galleryHTTPError(c, err)
		return
	}
	response.Success(c, gin.H{"deleted": true})
}
