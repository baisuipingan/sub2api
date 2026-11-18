package routes

import (
	"github.com/Wei-Shaw/sub2api/internal/handler"
	"github.com/gin-gonic/gin"
)

// registerOfficePilotRoutes is inside the existing authenticated, audited admin
// group. It deliberately does not create a public user workbench endpoint yet.
func registerOfficePilotRoutes(admin *gin.RouterGroup, h *handler.Handlers) {
	if h.OfficeAgent == nil {
		return
	}
	pilot := admin.Group("/workbench/office-pilot", h.OfficeAgent.Protect)
	pilot.GET("/status", h.OfficeAgent.PilotStatus)
	pilot.POST("/runs", h.OfficeAgent.Submit)
	pilot.GET("/runs/:id", h.OfficeAgent.Get)
	pilot.GET("/runs/:id/messages", h.OfficeAgent.Messages)
	pilot.GET("/runs/:id/events", h.OfficeAgent.Events)
	pilot.DELETE("/runs/:id", h.OfficeAgent.Cancel)
}

// RegisterOfficeAgentInternalRoutes authenticates the worker bootstrap and
// per-run lease in the handler; neither a browser JWT nor the bootstrap token
// authorizes an individual model invocation. Disabled pilots return 404.
func RegisterOfficeAgentInternalRoutes(r *gin.Engine, h *handler.Handlers) {
	if h.OfficeAgent == nil {
		return
	}
	internal := r.Group("/internal/office-agent/v1", h.OfficeAgent.Protect)
	internal.POST("/claim", h.OfficeAgent.Claim)
	runs := internal.Group("/runs/:id")
	runs.POST("/heartbeat", h.OfficeAgent.Heartbeat)
	runs.POST("/events", h.OfficeAgent.Progress)
	runs.POST("/finish", h.OfficeAgent.Finish)
	runs.POST("/model/chat/completions", h.OfficeAgent.Model)
	runs.POST("/tools/:operation/prepare", h.OfficeAgent.PrepareTool)
	runs.POST("/tools/:operation/result", h.OfficeAgent.CompleteTool)
}
