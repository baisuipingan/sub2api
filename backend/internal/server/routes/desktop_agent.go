package routes

import (
	"github.com/Wei-Shaw/sub2api/internal/handler"
	"github.com/Wei-Shaw/sub2api/internal/server/middleware"
	"github.com/gin-gonic/gin"
)

func registerDesktopAgentUserRoutes(authenticated *gin.RouterGroup, h *handler.Handlers) {
	if h.DesktopAgent != nil {
		authenticated.POST("/desktop/bootstrap", h.DesktopAgent.Bootstrap)
	}
}

func registerDesktopAgentAdminRoutes(admin *gin.RouterGroup, h *handler.Handlers, stepUp middleware.StepUpAuthMiddleware) {
	if h.DesktopAgent != nil {
		admin.GET("/desktop/config", h.DesktopAgent.GetConfig)
		admin.GET("/desktop/models", h.DesktopAgent.GetModels)
		admin.POST("/desktop/config/disable", desktopAgentNoStore, gin.HandlerFunc(stepUp), h.DesktopAgent.DisableConfig)
		admin.PUT("/desktop/config", desktopAgentNoStore, gin.HandlerFunc(stepUp), h.DesktopAgent.UpdateConfig)
	}
}

func desktopAgentNoStore(c *gin.Context) { c.Header("Cache-Control", "no-store") }
