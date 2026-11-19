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

type OfficeResourcesHandler struct {
	resources *service.OfficeResourcesService
	agent     *service.OfficeAgentService
}

func NewOfficeResourcesHandler(resources *service.OfficeResourcesService, agent *service.OfficeAgentService) *OfficeResourcesHandler {
	return &OfficeResourcesHandler{resources: resources, agent: agent}
}

func (h *OfficeResourcesHandler) user(c *gin.Context) (int64, bool) {
	subject, ok := middleware.GetAuthSubjectFromContext(c)
	if !ok || subject.UserID <= 0 {
		officeHandlerError(c, service.ErrOfficeUnauthorized)
		return 0, false
	}
	c.Header("Cache-Control", "no-store")
	return subject.UserID, true
}
func officeResourceHTTPError(c *gin.Context, err error) bool {
	if err == nil {
		return false
	}
	status, code, message := 0, "", ""
	switch {
	case errors.Is(err, service.ErrOfficeResourceType):
		status, code, message = http.StatusBadRequest, "FILE_TYPE_INVALID", "文件格式或内容不符合要求；支持 PDF、DOCX、XLSX、PPTX、PNG、JPEG、WebP 和 UTF-8 文本，不支持宏、加密文档或可执行文件"
	case errors.Is(err, service.ErrOfficeResourceTooLarge):
		status, code, message = http.StatusRequestEntityTooLarge, "FILE_TOO_LARGE", "单个文件及任务文件总量不能超过 24 MiB，任务最多保留 64 个文件"
	case errors.Is(err, service.ErrOfficeResourceQuota):
		status, code, message = http.StatusTooManyRequests, "FILE_QUOTA_EXCEEDED", "文件存储空间已满；请删除不再需要的文件或会话后重试"
	case errors.Is(err, service.ErrOfficeStorageUnavailable):
		status, code, message = http.StatusServiceUnavailable, "FILE_STORAGE_UNAVAILABLE", "文件存储暂不可用，请联系管理员检查私有对象存储配置"
	case errors.Is(err, service.ErrGalleryBusy):
		status, code, message = http.StatusServiceUnavailable, "FILE_PROCESSING_BUSY", "图片处理容量暂时已满，请稍后重试或使用较小图片"
	default:
		return officeHandlerError(c, err)
	}
	c.JSON(status, gin.H{"error": gin.H{"code": code, "message": message, "retryable": status == http.StatusServiceUnavailable}})
	return true
}

func (h *OfficeResourcesHandler) List(c *gin.Context) {
	user, ok := h.user(c)
	if !ok {
		return
	}
	limit := 50
	if raw := c.Query("limit"); raw != "" {
		value, err := strconv.Atoi(raw)
		if err != nil || value < 1 || value > 200 {
			officeResourceHTTPError(c, service.ErrOfficeInvalid)
			return
		}
		limit = value
	}
	items, next, err := h.resources.List(c.Request.Context(), user, c.Query("cursor"), limit)
	if officeResourceHTTPError(c, err) {
		return
	}
	response.Success(c, gin.H{"items": items, "next_cursor": next})
}
func (h *OfficeResourcesHandler) Upload(c *gin.Context) {
	user, ok := h.user(c)
	if !ok {
		return
	}
	if h.agent == nil {
		officeResourceHTTPError(c, service.ErrOfficeDisabled)
		return
	}
	status, err := h.agent.ChatStatus(c.Request.Context())
	if officeResourceHTTPError(c, err) {
		return
	}
	if !status.UploadsEnabled {
		officeResourceHTTPError(c, service.ErrOfficeDisabled)
		return
	}
	release, err := h.resources.AcquireIO()
	if officeResourceHTTPError(c, err) {
		return
	}
	defer release()
	c.Request.Body = http.MaxBytesReader(c.Writer, c.Request.Body, service.OfficeResourceMaxBytes+(1<<20))
	if err = c.Request.ParseMultipartForm(1 << 20); err != nil {
		var max *http.MaxBytesError
		if errors.As(err, &max) {
			officeResourceHTTPError(c, service.ErrOfficeResourceTooLarge)
		} else {
			officeResourceHTTPError(c, service.ErrOfficeInvalid)
		}
		return
	}
	defer func() { _ = c.Request.MultipartForm.RemoveAll() }()
	if len(c.Request.MultipartForm.File) != 1 || len(c.Request.MultipartForm.File["file"]) != 1 || len(c.Request.MultipartForm.Value) > 0 {
		officeResourceHTTPError(c, service.ErrOfficeInvalid)
		return
	}
	header := c.Request.MultipartForm.File["file"][0]
	if header.Size > service.OfficeResourceMaxBytes {
		officeResourceHTTPError(c, service.ErrOfficeResourceTooLarge)
		return
	}
	file, err := header.Open()
	if officeResourceHTTPError(c, err) {
		return
	}
	defer func() { _ = file.Close() }()
	data, err := io.ReadAll(io.LimitReader(file, service.OfficeResourceMaxBytes+1))
	if officeResourceHTTPError(c, err) {
		return
	}
	if int64(len(data)) > service.OfficeResourceMaxBytes {
		officeResourceHTTPError(c, service.ErrOfficeResourceTooLarge)
		return
	}
	v, err := h.resources.Upload(c.Request.Context(), user, header.Filename, data, c.GetHeader("Idempotency-Key"))
	if officeResourceHTTPError(c, err) {
		return
	}
	response.Success(c, v)
}
func (h *OfficeResourcesHandler) Access(c *gin.Context) {
	user, ok := h.user(c)
	if !ok {
		return
	}
	v, err := h.resources.Access(c.Request.Context(), user, c.Param("version_id"))
	if officeResourceHTTPError(c, err) {
		return
	}
	response.Success(c, v)
}
func (h *OfficeResourcesHandler) Delete(c *gin.Context) {
	user, ok := h.user(c)
	if !ok {
		return
	}
	if officeResourceHTTPError(c, h.resources.Delete(c.Request.Context(), user, c.Param("resource_id"))) {
		return
	}
	response.Success(c, gin.H{"deleted": true})
}

func (h *OfficeResourcesHandler) lease(c *gin.Context) (*service.OfficeRunLease, bool) {
	if h.agent == nil {
		officeHandlerError(c, service.ErrOfficeDisabled)
		return nil, false
	}
	lease, err := h.agent.AuthenticateLease(c.Request.Context(), c.Param("id"), c.GetHeader("X-Office-Lease"), strings.TrimPrefix(c.GetHeader("Authorization"), "Bearer "))
	if officeResourceHTTPError(c, err) {
		return nil, false
	}
	c.Header("Cache-Control", "no-store")
	return lease, true
}
func (h *OfficeResourcesHandler) GetWorkspace(c *gin.Context) {
	lease, ok := h.lease(c)
	if !ok {
		return
	}
	release, err := h.resources.AcquireIO()
	if officeResourceHTTPError(c, err) {
		return
	}
	defer release()
	var version *int64
	if raw, present := c.GetQuery("version"); present {
		v, e := strconv.ParseInt(raw, 10, 64)
		if e != nil || v < 0 {
			officeResourceHTTPError(c, service.ErrOfficeInvalid)
			return
		}
		version = &v
	}
	v, err := h.resources.GetWorkspace(c.Request.Context(), *lease, version)
	if officeResourceHTTPError(c, err) {
		return
	}
	response.Success(c, v)
}
func officeResourceDecode(c *gin.Context, in any, limit int64) bool {
	c.Request.Body = http.MaxBytesReader(c.Writer, c.Request.Body, limit)
	decoder := json.NewDecoder(c.Request.Body)
	decoder.DisallowUnknownFields()
	err := decoder.Decode(in)
	if err == nil {
		err = decoder.Decode(new(any))
		if err == io.EOF {
			return true
		}
	}
	var max *http.MaxBytesError
	if errors.As(err, &max) {
		officeResourceHTTPError(c, service.ErrOfficeResourceTooLarge)
	} else {
		officeResourceHTTPError(c, service.ErrOfficeInvalid)
	}
	return false
}
func (h *OfficeResourcesHandler) SaveWorkspace(c *gin.Context) {
	lease, ok := h.lease(c)
	if !ok {
		return
	}
	release, err := h.resources.AcquireIO()
	if officeResourceHTTPError(c, err) {
		return
	}
	defer release()
	var in service.OfficeWorkspaceSave
	if !officeResourceDecode(c, &in, 40<<20) {
		return
	}
	v, err := h.resources.SaveWorkspace(c.Request.Context(), *lease, in)
	if officeResourceHTTPError(c, err) {
		return
	}
	response.Success(c, gin.H{"version": v.Version, "snapshot_id": v.SnapshotID, "head_version": v.HeadVersion})
}
func (h *OfficeResourcesHandler) PublishArtifact(c *gin.Context) {
	lease, ok := h.lease(c)
	if !ok {
		return
	}
	release, err := h.resources.AcquireIO()
	if officeResourceHTTPError(c, err) {
		return
	}
	defer release()
	var in service.OfficeArtifactPublish
	if !officeResourceDecode(c, &in, 8192) {
		return
	}
	v, err := h.resources.PublishArtifact(c.Request.Context(), *lease, in)
	if officeResourceHTTPError(c, err) {
		return
	}
	response.Success(c, v)
}
