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
		admin.PUT("/desktop/config", gin.HandlerFunc(stepUp), h.DesktopAgent.UpdateConfig)
	}
}
