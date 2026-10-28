package admin

import (
	"github.com/Wei-Shaw/sub2api/internal/pkg/response"
	"github.com/Wei-Shaw/sub2api/internal/service"

	"github.com/gin-gonic/gin"
)

// GetBillingCurrencySettings returns the versioned display-currency settings.
// GET /api/v1/admin/settings/billing-currency
func (h *SettingHandler) GetBillingCurrencySettings(c *gin.Context) {
	if h == nil || h.settingService == nil {
		response.InternalError(c, "setting service is not configured")
		return
	}
	response.Success(c, h.settingService.GetBillingCurrencyConfig(c.Request.Context()))
}

// UpdateBillingCurrencySettings updates display currency and public FX rates.
// PUT /api/v1/admin/settings/billing-currency
func (h *SettingHandler) UpdateBillingCurrencySettings(c *gin.Context) {
	if h == nil || h.settingService == nil {
		response.InternalError(c, "setting service is not configured")
		return
	}
	var req service.BillingCurrencyConfig
	if err := c.ShouldBindJSON(&req); err != nil {
		response.BadRequest(c, "invalid billing currency configuration")
		return
	}
	updated, err := h.settingService.UpdateBillingCurrencyConfig(c.Request.Context(), req)
	if err != nil {
		response.ErrorFrom(c, err)
		return
	}
	response.Success(c, updated)
}
