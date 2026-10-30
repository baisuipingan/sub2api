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
	pricingService *service.ImageStudioPricingService
}

// NewImageStudioHandler 构造生图工作台 handler。
func NewImageStudioHandler(
	apiKeyService *service.APIKeyService,
	settingService *service.SettingService,
	groupService *service.GroupService,
	pricingService *service.ImageStudioPricingService,
) *ImageStudioHandler {
	return &ImageStudioHandler{
		apiKeyService:  apiKeyService,
		settingService: settingService,
		groupService:   groupService,
		pricingService: pricingService,
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

// imageStudioPricingRequest 是预估请求体。
type imageStudioPricingRequest struct {
	// Models 是前端当前可选模型的「计费模型名」（如 gpt-image-2 / gpt-image-2-4K）。
	Models []string `json:"models"`
}

const (
	imageStudioMaxPricingModels   = 20
	imageStudioMaxPricingModelLen = 120
)

// EstimatePricing 返回各模型 × 各尺寸档位的单张实际成本（已含倍率）。
//
// POST /api/v1/image-studio/pricing
//
// 工作台用它替代原先写死的常量表，保证「页面显示 = 真实扣费」。
// 价格只涉及调用者自己的成本，不存在越权泄露。
func (h *ImageStudioHandler) EstimatePricing(c *gin.Context) {
	subject, ok := middleware.GetAuthSubjectFromContext(c)
	if !ok {
		response.Error(c, http.StatusUnauthorized, "authentication required")
		return
	}

	var req imageStudioPricingRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		response.Error(c, http.StatusBadRequest, "invalid request body")
		return
	}

	if len(req.Models) == 0 || len(req.Models) > imageStudioMaxPricingModels {
		response.Error(c, http.StatusBadRequest, "invalid model list")
		return
	}
	for _, model := range req.Models {
		if len(model) == 0 || len(model) > imageStudioMaxPricingModelLen {
			response.Error(c, http.StatusBadRequest, "invalid model name")
			return
		}
	}

	group, ok := h.resolveStudioGroup(c)
	if !ok {
		return
	}

	table := h.pricingService.EstimateUnitPrices(
		c.Request.Context(),
		subject.UserID,
		group,
		req.Models,
	)

	response.Success(c, gin.H{"pricing": table})
}

// resolveStudioGroup 读取并校验「生图工作台使用分组」。
// 校验失败时已写好响应，返回 false 即可直接结束。
func (h *ImageStudioHandler) resolveStudioGroup(c *gin.Context) (*service.Group, bool) {
	settings, err := h.settingService.GetPublicSettings(c.Request.Context())
	if err != nil {
		response.Error(c, http.StatusInternalServerError, "failed to load settings")
		return nil, false
	}
	if settings.ImageStudioGroupID <= 0 {
		response.Error(c, http.StatusServiceUnavailable, "image studio group is not configured")
		return nil, false
	}

	group, err := h.groupService.GetByID(c.Request.Context(), settings.ImageStudioGroupID)
	if err != nil || group == nil {
		response.Error(c, http.StatusServiceUnavailable, "configured image studio group is not available")
		return nil, false
	}
	return group, true
}
