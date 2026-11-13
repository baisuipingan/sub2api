package handler

import (
	"bytes"
	"encoding/json"
	"io"
	"net/http"

	"github.com/Wei-Shaw/sub2api/internal/pkg/response"
	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/gin-gonic/gin"
)

type DesktopReleaseStorageHandler struct {
	service *service.DesktopReleaseStorageService
}

func NewDesktopReleaseStorageHandler(s *service.DesktopReleaseStorageService) *DesktopReleaseStorageHandler {
	return &DesktopReleaseStorageHandler{service: s}
}

func (h *DesktopReleaseStorageHandler) GetDownloads(c *gin.Context) {
	result, err := h.service.PublicDownloads(c.Request.Context())
	if err != nil {
		c.Header("Cache-Control", "no-store")
		response.ErrorWithDetails(c, http.StatusServiceUnavailable, "desktop downloads are temporarily unavailable", "DESKTOP_DOWNLOADS_UNAVAILABLE", nil)
		return
	}
	c.Header("Cache-Control", "public, max-age=60")
	response.Success(c, result)
}

func readDesktopReleaseBody(c *gin.Context, target any) bool {
	c.Request.Body = http.MaxBytesReader(c.Writer, c.Request.Body, 4096)
	raw, err := io.ReadAll(c.Request.Body)
	if err != nil || len(bytes.TrimSpace(raw)) == 0 {
		desktopReleaseStorageError(c, service.ErrDesktopReleaseStorageInvalid)
		return false
	}
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.DisallowUnknownFields()
	// Reject null/non-object and duplicate keys instead of silently accepting
	// json.Unmarshal's last-wins interpretation at a publishing boundary.
	if !desktopReleaseUniqueObject(raw) {
		desktopReleaseStorageError(c, service.ErrDesktopReleaseStorageInvalid)
		return false
	}
	if decoder.Decode(target) != nil || decoder.Decode(new(any)) != io.EOF {
		desktopReleaseStorageError(c, service.ErrDesktopReleaseStorageInvalid)
		return false
	}
	return true
}

func desktopReleaseUniqueObject(raw []byte) bool {
	decoder := json.NewDecoder(bytes.NewReader(raw))
	token, err := decoder.Token()
	if err != nil || token != json.Delim('{') {
		return false
	}
	seen := map[string]bool{}
	for decoder.More() {
		token, err = decoder.Token()
		key, ok := token.(string)
		if err != nil || !ok || seen[key] {
			return false
		}
		seen[key] = true
		var value json.RawMessage
		if decoder.Decode(&value) != nil {
			return false
		}
	}
	token, err = decoder.Token()
	return err == nil && token == json.Delim('}') && decoder.Decode(new(any)) == io.EOF
}
func desktopReleaseStorageError(c *gin.Context, err error) {
	// Service and repository errors are already normalized. Never expose/log the
	// provider's raw response, credentials or presigned URL on this endpoint.
	switch err {
	case service.ErrDesktopReleaseStorageInvalid:
		response.ErrorWithDetails(c, http.StatusBadRequest, "invalid desktop release storage request or configuration", "DESKTOP_RELEASE_STORAGE_INVALID", nil)
	case service.ErrDesktopReleaseStorageTestFailed:
		response.ErrorWithDetails(c, http.StatusBadRequest, "desktop release storage test failed; verify bucket permissions and the public domain mapping", "DESKTOP_RELEASE_STORAGE_TEST_FAILED", nil)
	default:
		response.ErrorWithDetails(c, http.StatusServiceUnavailable, "desktop release storage is unavailable; configure a separate bucket and backup S3 credentials", "DESKTOP_RELEASE_STORAGE_UNAVAILABLE", nil)
	}
}
func (h *DesktopReleaseStorageHandler) GetConfig(c *gin.Context) {
	if !allowDesktopAgentAdmin(c) {
		return
	}
	result, err := h.service.Get(c.Request.Context())
	if err != nil {
		desktopReleaseStorageError(c, err)
		return
	}
	response.Success(c, result)
}
func (h *DesktopReleaseStorageHandler) UpdateConfig(c *gin.Context) {
	if !allowDesktopAgentAdmin(c) {
		return
	}
	var request service.DesktopReleaseStorageConfig
	if !readDesktopReleaseBody(c, &request) {
		return
	}
	result, err := h.service.Update(c.Request.Context(), request)
	if err != nil {
		desktopReleaseStorageError(c, err)
		return
	}
	response.Success(c, result)
}
func (h *DesktopReleaseStorageHandler) TestConnection(c *gin.Context) {
	if !allowDesktopAgentAdmin(c) {
		return
	}
	var request service.DesktopReleaseStorageConfig
	if !readDesktopReleaseBody(c, &request) {
		return
	}
	if err := h.service.TestConnection(c.Request.Context(), request); err != nil {
		desktopReleaseStorageError(c, err)
		return
	}
	response.Success(c, gin.H{"success": true})
}
func (h *DesktopReleaseStorageHandler) HeadObject(c *gin.Context) {
	if !allowDesktopAgentAdmin(c) {
		return
	}
	var request struct {
		Key string `json:"key"`
	}
	if !readDesktopReleaseBody(c, &request) {
		return
	}
	result, err := h.service.Head(c.Request.Context(), request.Key)
	if err != nil {
		desktopReleaseStorageError(c, err)
		return
	}
	response.Success(c, result)
}
func (h *DesktopReleaseStorageHandler) CreateUpload(c *gin.Context) {
	if !allowDesktopAgentAdmin(c) {
		return
	}
	var request service.DesktopReleaseUploadRequest
	if !readDesktopReleaseBody(c, &request) {
		return
	}
	result, err := h.service.PresignUpload(c.Request.Context(), request)
	if err != nil {
		desktopReleaseStorageError(c, err)
		return
	}
	response.Success(c, result)
}
