package handler

import (
	"errors"
	"net/http"
	"strconv"
	"strings"

	"github.com/Wei-Shaw/sub2api/internal/pkg/response"
	"github.com/Wei-Shaw/sub2api/internal/server/middleware"
	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/gin-gonic/gin"
)

// StudioJobHandler exposes only user-owned durable Studio jobs. It never
// accepts a browser API key: the existing system-managed key is resolved on
// submission and the gateway revalidates it when the worker executes a call.
type StudioJobHandler struct {
	jobs   *service.StudioJobService
	studio *ImageStudioHandler
}

func NewStudioJobHandler(jobs *service.StudioJobService, studio *ImageStudioHandler) *StudioJobHandler {
	return &StudioJobHandler{jobs: jobs, studio: studio}
}

func studioJobUserID(c *gin.Context) (int64, bool) {
	subject, ok := middleware.GetAuthSubjectFromContext(c)
	if !ok || subject.UserID <= 0 {
		response.ErrorWithDetails(c, http.StatusUnauthorized, "authentication required", "UNAUTHORIZED", nil)
		return 0, false
	}
	return subject.UserID, true
}

func studioJobHTTPError(c *gin.Context, err error) {
	switch {
	case errors.Is(err, service.ErrStudioJobInvalid):
		response.ErrorWithDetails(c, http.StatusBadRequest, "invalid studio task", "STUDIO_JOB_INVALID", nil)
	case errors.Is(err, service.ErrStudioJobConflict):
		response.ErrorWithDetails(c, http.StatusConflict, "studio task ID conflicts with a previous request", "STUDIO_JOB_CONFLICT", nil)
	case errors.Is(err, service.ErrStudioJobNotFound):
		response.ErrorWithDetails(c, http.StatusNotFound, "studio task not found", "STUDIO_JOB_NOT_FOUND", nil)
	case errors.Is(err, service.ErrGalleryDisabled):
		response.ErrorWithDetails(c, http.StatusServiceUnavailable, "cloud studio tasks are disabled", "STUDIO_JOBS_DISABLED", nil)
	case errors.Is(err, service.ErrStudioJobUnavailable), errors.Is(err, service.ErrGalleryUnavailable):
		response.ErrorWithDetails(c, http.StatusServiceUnavailable, "studio task storage is unavailable", "STUDIO_JOB_UNAVAILABLE", nil)
	case errors.Is(err, service.ErrGalleryQuota):
		response.ErrorWithDetails(c, http.StatusConflict, "cloud gallery has insufficient capacity for this task", "STUDIO_GALLERY_QUOTA", nil)
	case errors.Is(err, service.ErrStudioJobQueueFull):
		response.ErrorWithDetails(c, http.StatusTooManyRequests, "too many Studio tasks are already running", "STUDIO_JOB_QUEUE_FULL", nil)
	default:
		response.ErrorWithDetails(c, http.StatusInternalServerError, "studio task operation failed", "STUDIO_JOB_ERROR", nil)
	}
}

func (h *StudioJobHandler) Submit(c *gin.Context) {
	userID, ok := studioJobUserID(c)
	if !ok {
		return
	}
	if h == nil || h.jobs == nil || h.studio == nil || !h.jobs.Enabled(c.Request.Context()) {
		studioJobHTTPError(c, service.ErrGalleryDisabled)
		return
	}
	group, ok := h.studio.resolveStudioGroup(c)
	if !ok {
		return
	}
	c.Request.Body = http.MaxBytesReader(c.Writer, c.Request.Body, 128<<10)
	var in service.StudioJobSubmit
	if err := c.ShouldBindJSON(&in); err != nil {
		var maxErr *http.MaxBytesError
		if errors.As(err, &maxErr) {
			response.ErrorWithDetails(c, http.StatusRequestEntityTooLarge, "studio task request is too large", "STUDIO_JOB_TOO_LARGE", nil)
			return
		}
		studioJobHTTPError(c, service.ErrStudioJobInvalid)
		return
	}
	groupID := group.ID
	key, err := h.studio.apiKeyService.GetOrCreateImageStudioKey(c.Request.Context(), userID, &groupID)
	if err != nil || key == nil || key.ID <= 0 {
		studioJobHTTPError(c, service.ErrStudioJobUnavailable)
		return
	}
	job, err := h.jobs.Submit(c.Request.Context(), userID, key.ID, in)
	if err != nil {
		studioJobHTTPError(c, err)
		return
	}
	c.Header("Cache-Control", "no-store")
	c.Header("Location", "/api/v1/image-studio/jobs/"+job.TaskID)
	response.Accepted(c, gin.H{"job": job})
}

func (h *StudioJobHandler) Get(c *gin.Context) {
	userID, ok := studioJobUserID(c)
	if !ok {
		return
	}
	if h == nil || h.jobs == nil {
		studioJobHTTPError(c, service.ErrStudioJobUnavailable)
		return
	}
	job, err := h.jobs.Get(c.Request.Context(), userID, c.Param("id"))
	if err != nil {
		studioJobHTTPError(c, err)
		return
	}
	c.Header("Cache-Control", "no-store")
	response.Success(c, gin.H{"job": job})
}

func (h *StudioJobHandler) List(c *gin.Context) {
	userID, ok := studioJobUserID(c)
	if !ok {
		return
	}
	if h == nil || h.jobs == nil {
		studioJobHTTPError(c, service.ErrStudioJobUnavailable)
		return
	}
	limit := 50
	if raw := strings.TrimSpace(c.Query("limit")); raw != "" {
		parsed, err := strconv.Atoi(raw)
		if err != nil || parsed < 1 || parsed > 100 {
			studioJobHTTPError(c, service.ErrStudioJobInvalid)
			return
		}
		limit = parsed
	}
	active := c.DefaultQuery("active", "0")
	if active != "0" && active != "1" {
		studioJobHTTPError(c, service.ErrStudioJobInvalid)
		return
	}
	page, err := h.jobs.List(c.Request.Context(), userID, c.Query("cursor"), limit, active == "1")
	if err != nil {
		studioJobHTTPError(c, err)
		return
	}
	c.Header("Cache-Control", "no-store")
	response.Success(c, page)
}

func (h *StudioJobHandler) Delete(c *gin.Context) {
	userID, ok := studioJobUserID(c)
	if !ok {
		return
	}
	if h == nil || h.jobs == nil {
		studioJobHTTPError(c, service.ErrStudioJobUnavailable)
		return
	}
	if err := h.jobs.Cancel(c.Request.Context(), userID, c.Param("id")); err != nil {
		studioJobHTTPError(c, err)
		return
	}
	c.Header("Cache-Control", "no-store")
	response.Success(c, gin.H{"deleted": true})
}
