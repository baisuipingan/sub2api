package routes

import (
	"github.com/Wei-Shaw/sub2api/internal/handler"
	"github.com/Wei-Shaw/sub2api/internal/server/middleware"
	"github.com/gin-gonic/gin"
)

func registerDesktopDownloadRoutes(v1 *gin.RouterGroup, h *handler.Handlers, limiter *middleware.PanelRateLimiter) {
	if h.DesktopReleaseStorage != nil {
		v1.GET("/desktop/downloads", limiter.PublicIP(), h.DesktopReleaseStorage.GetDownloads)
	}
}

func registerDesktopAgentUserRoutes(authenticated *gin.RouterGroup, h *handler.Handlers) {
	if h.DesktopAgent != nil {
		authenticated.POST("/desktop/bootstrap", h.DesktopAgent.Bootstrap)
	}
}

func registerDesktopAgentAdminRoutes(admin *gin.RouterGroup, h *handler.Handlers, stepUp middleware.StepUpAuthMiddleware) {
	if h.DesktopReleaseStorage != nil {
		release := admin.Group("/desktop/release-storage", desktopAgentNoStore)
		release.GET("", h.DesktopReleaseStorage.GetConfig)
		release.PUT("", gin.HandlerFunc(stepUp), h.DesktopReleaseStorage.UpdateConfig)
		release.POST("/test", gin.HandlerFunc(stepUp), h.DesktopReleaseStorage.TestConnection)
		release.POST("/objects/head", h.DesktopReleaseStorage.HeadObject)
		release.POST("/uploads", gin.HandlerFunc(stepUp), h.DesktopReleaseStorage.CreateUpload)
	}
	if h.DesktopAgent != nil {
		admin.GET("/desktop/config", h.DesktopAgent.GetConfig)
		admin.GET("/desktop/models", h.DesktopAgent.GetModels)
		admin.GET("/desktop/profiles", h.DesktopAgent.GetProfiles)
		admin.POST("/desktop/users/:id/credential/revoke", desktopAgentNoStore, gin.HandlerFunc(stepUp), handler.DesktopAgentCredentialStepUpPassed, h.DesktopAgent.AdminRevokeCredential)
		admin.POST("/desktop/users/:id/credential/reset", desktopAgentNoStore, gin.HandlerFunc(stepUp), handler.DesktopAgentCredentialStepUpPassed, h.DesktopAgent.AdminResetCredential)
		admin.POST("/desktop/config/disable", desktopAgentNoStore, gin.HandlerFunc(stepUp), h.DesktopAgent.DisableConfig)
		admin.PUT("/desktop/config", desktopAgentNoStore, gin.HandlerFunc(stepUp), h.DesktopAgent.UpdateConfig)
	}
}

func desktopAgentNoStore(c *gin.Context) { c.Header("Cache-Control", "no-store") }
