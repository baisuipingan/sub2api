package handler

import (
	"errors"
	"net/http"

	"github.com/Wei-Shaw/sub2api/internal/pkg/response"
	"github.com/Wei-Shaw/sub2api/internal/server/middleware"
	"github.com/Wei-Shaw/sub2api/internal/service"

	"github.com/gin-gonic/gin"
)

// ImageStudioHandler 提供生图工作台的前置能力。
//
// 核心是「用户无需手动配置 API Key」：由后端按管理员的「生图工作台使用分组」
// 为该用户幂等地准备一把专用密钥，前端拿到后直接调 /v1/images/*。
// 计费链路完全不改：请求仍走原有网关中间件，扣用户余额、写 usage_logs。
type ImageStudioHandler struct {
	apiKeyService  *service.APIKeyService
	settingService *service.SettingService
	groupService   *service.GroupService
}

// NewImageStudioHandler 构造生图工作台 handler。
func NewImageStudioHandler(
	apiKeyService *service.APIKeyService,
	settingService *service.SettingService,
	groupService *service.GroupService,
) *ImageStudioHandler {
	return &ImageStudioHandler{
		apiKeyService:  apiKeyService,
		settingService: settingService,
		groupService:   groupService,
	}
}

// GetKey 返回当前用户的生图工作台专用密钥（不存在则创建）。
//
// GET /api/v1/image-studio/key
func (h *ImageStudioHandler) GetKey(c *gin.Context) {
	subject, ok := middleware.GetAuthSubjectFromContext(c)
	if !ok {
		response.Error(c, http.StatusUnauthorized, "authentication required")
		return
	}

	settings, err := h.settingService.GetPublicSettings(c.Request.Context())
	if err != nil {
		response.Error(c, http.StatusInternalServerError, "failed to load settings")
		return
	}

	if !settings.ImageStudioEnabled {
		response.Error(c, http.StatusForbidden, "image studio is disabled")
		return
	}

	if settings.ImageStudioGroupID <= 0 {
		// 管理员还没配置分组：明确报错，避免创建一把没有分组、请求必然失败的密钥
		response.Error(c, http.StatusServiceUnavailable, "image studio group is not configured")
		return
	}

	groupID := settings.ImageStudioGroupID

	// 分组必须存在且对用户可见，否则请求会在网关阶段失败
	group, err := h.groupService.GetByID(c.Request.Context(), groupID)
	if err != nil || group == nil {
		response.Error(c, http.StatusServiceUnavailable, "configured image studio group is not available")
		return
	}

	key, err := h.apiKeyService.GetOrCreateImageStudioKey(
		c.Request.Context(),
		subject.UserID,
		&groupID,
	)
	if err != nil {
		if errors.Is(err, service.ErrAPIKeyNotFound) {
			response.Error(c, http.StatusNotFound, "key not found")
			return
		}
		response.Error(c, http.StatusInternalServerError, "failed to prepare key")
		return
	}

	response.Success(c, gin.H{
		"key":        key.Key,
		"group_id":   groupID,
		"group_name": group.Name,
	})
}
