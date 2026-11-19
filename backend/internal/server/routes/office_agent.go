package routes

import (
	"github.com/Wei-Shaw/sub2api/internal/handler"
	"github.com/gin-gonic/gin"
)

// registerOfficePilotRoutes retains the original authenticated admin diagnostic
// surface. The separate registerOfficeChatRoutes function owns user workbench UI
// APIs and enforces normal authenticated user ownership.
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
// authorizes an individual model invocation. Disabled workers return 404.
func RegisterOfficeAgentInternalRoutes(r *gin.Engine, h *handler.Handlers) {
	if h.OfficeAgent == nil {
		return
	}
	internal := r.Group("/internal/office-agent/v1", h.OfficeAgent.Protect)
	internal.POST("/claim", h.OfficeAgent.Claim)
	runs := internal.Group("/runs/:id")
	runs.POST("/heartbeat", h.OfficeAgent.Heartbeat)
	runs.POST("/suspend", h.OfficeAgent.Suspend)
	runs.GET("/checkpoint", h.OfficeAgent.Checkpoint)
	runs.POST("/checkpoint", h.OfficeAgent.SaveCheckpoint)
	runs.POST("/context-summary", h.OfficeAgent.SaveContextSummary)
	runs.POST("/wait", h.OfficeAgent.Wait)
	if h.OfficeResources != nil {
		runs.GET("/workspace", h.OfficeResources.GetWorkspace)
		runs.POST("/workspace", h.OfficeResources.SaveWorkspace)
		runs.POST("/artifacts", h.OfficeResources.PublishArtifact)
	}
	runs.POST("/search", h.OfficeAgent.Search)
	runs.POST("/web-read", h.OfficeAgent.ReadWeb)
	runs.POST("/events", h.OfficeAgent.Progress)
	runs.POST("/finish", h.OfficeAgent.Finish)
	runs.POST("/model/chat/completions", h.OfficeAgent.Model)
	runs.POST("/tools/:operation/prepare", h.OfficeAgent.PrepareTool)
	runs.POST("/tools/:operation/result", h.OfficeAgent.CompleteTool)
}

func registerOfficeChatRoutes(authenticated *gin.RouterGroup, h *handler.Handlers) {
	if h.OfficeAgent == nil {
		return
	}
	c := authenticated.Group("/workbench/chat", h.OfficeAgent.Protect)
	if h.OfficeResources != nil {
		c.GET("/resources", h.OfficeResources.List)
		c.POST("/resources", h.OfficeResources.Upload)
		c.GET("/resources/:version_id/access", h.OfficeResources.Access)
		c.DELETE("/resources/:resource_id", h.OfficeResources.Delete)
	}
	c.GET("/status", h.OfficeAgent.ChatStatus)
	c.GET("/models", h.OfficeAgent.ChatModels)
	c.GET("/conversations", h.OfficeAgent.Conversations)
	c.POST("/conversations", h.OfficeAgent.CreateConversation)
	c.PATCH("/conversations/:conversation", h.OfficeAgent.RenameConversation)
	c.DELETE("/conversations/:conversation", h.OfficeAgent.DeleteConversation)
	c.GET("/conversations/:conversation/messages", h.OfficeAgent.ConversationMessages)
	c.POST("/conversations/:conversation/runs", h.OfficeAgent.SubmitChat)
	c.GET("/runs/:id", h.OfficeAgent.ChatRun)
	c.DELETE("/runs/:id", h.OfficeAgent.ChatCancel)
	c.GET("/runs/:id/events", h.OfficeAgent.Stream)
	c.GET("/runs/:id/operations", h.OfficeAgent.ChatOperations)
	c.POST("/runs/:id/input", h.OfficeAgent.Input)
	c.POST("/runs/:id/select", h.OfficeAgent.SelectAnswer)
}
