package handler

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strconv"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/pkg/response"
	"github.com/Wei-Shaw/sub2api/internal/pkg/timezone"
	"github.com/Wei-Shaw/sub2api/internal/server/middleware"
	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/gin-gonic/gin"
)

func officeUser(c *gin.Context) (int64, bool) {
	subject, ok := middleware.GetAuthSubjectFromContext(c)
	if !ok {
		officeHandlerError(c, service.ErrOfficeUnauthorized)
		return 0, false
	}
	return subject.UserID, true
}
func (h *OfficeAgentHandler) ChatStatus(c *gin.Context) {
	status, e := h.service.ChatStatus(c.Request.Context())
	if officeHandlerError(c, e) {
		return
	}
	response.Success(c, status)
}
func (h *OfficeAgentHandler) ChatModels(c *gin.Context) {
	models, e := h.service.ChatModels(c.Request.Context())
	if officeHandlerError(c, e) {
		return
	}
	response.Success(c, gin.H{"models": models})
}
func (h *OfficeAgentHandler) ChatSearchUsage(c *gin.Context) {
	user, ok := officeUser(c)
	if !ok {
		return
	}
	page, pageSize := response.ParsePagination(c)
	start, end, ok := workbenchSearchUsageDates(c)
	if !ok {
		return
	}
	items, total, err := h.service.ListSearchCharges(c.Request.Context(), user, page, pageSize, start, end)
	if officeHandlerError(c, err) {
		return
	}
	response.Paginated(c, items, total, page, pageSize)
}

func workbenchSearchUsageDates(c *gin.Context) (start, end time.Time, ok bool) {
	const dateFormat = "2006-01-02"
	var err error
	if raw := c.Query("start_date"); raw != "" {
		start, err = timezone.ParseInUserLocation(dateFormat, raw, c.Query("timezone"))
		if err != nil {
			response.BadRequest(c, "Invalid start_date format, use YYYY-MM-DD")
			return start, end, false
		}
	}
	if raw := c.Query("end_date"); raw != "" {
		end, err = timezone.ParseInUserLocation(dateFormat, raw, c.Query("timezone"))
		if err != nil {
			response.BadRequest(c, "Invalid end_date format, use YYYY-MM-DD")
			return start, end, false
		}
		end = end.AddDate(0, 0, 1)
	}
	if !start.IsZero() && !end.IsZero() && !start.Before(end) {
		response.BadRequest(c, "Invalid date range")
		return start, end, false
	}
	return start, end, true
}
func (h *OfficeAgentHandler) Conversations(c *gin.Context) {
	u, ok := officeUser(c)
	if !ok {
		return
	}
	items, next, e := h.service.Conversations(c.Request.Context(), u, c.Query("cursor"))
	if officeHandlerError(c, e) {
		return
	}
	response.Success(c, gin.H{"items": items, "next_cursor": next})
}
func (h *OfficeAgentHandler) CreateConversation(c *gin.Context) {
	u, ok := officeUser(c)
	if !ok {
		return
	}
	var in struct {
		ID    string `json:"id"`
		Title string `json:"title"`
	}
	if !officeDecode(c, &in, 2048) {
		return
	}
	item, e := h.service.CreateConversation(c.Request.Context(), u, in.ID, in.Title)
	if officeHandlerError(c, e) {
		return
	}
	response.Success(c, item)
}
func (h *OfficeAgentHandler) RenameConversation(c *gin.Context) {
	u, ok := officeUser(c)
	if !ok {
		return
	}
	var in struct {
		Title string `json:"title"`
	}
	if !officeDecode(c, &in, 2048) {
		return
	}
	repo, e := h.service.ChatRepository()
	if officeHandlerError(c, e) {
		return
	}
	item, e := repo.RenameConversation(c.Request.Context(), u, c.Param("conversation"), in.Title)
	if officeHandlerError(c, e) {
		return
	}
	response.Success(c, item)
}
func (h *OfficeAgentHandler) DeleteConversation(c *gin.Context) {
	u, ok := officeUser(c)
	if !ok {
		return
	}
	repo, e := h.service.ChatRepository()
	if officeHandlerError(c, e) {
		return
	}
	if officeHandlerError(c, repo.DeleteConversation(c.Request.Context(), u, c.Param("conversation"))) {
		return
	}
	response.Success(c, gin.H{"deleted": true})
}
func (h *OfficeAgentHandler) ConversationMessages(c *gin.Context) {
	u, ok := officeUser(c)
	if !ok {
		return
	}
	before := int64(0)
	if raw := c.Query("before_seq"); raw != "" {
		v, e := strconv.ParseInt(raw, 10, 64)
		if e != nil || v <= 0 {
			officeHandlerError(c, service.ErrOfficeInvalid)
			return
		}
		before = v
	}
	repo, e := h.service.ChatRepository()
	if officeHandlerError(c, e) {
		return
	}
	messages, runs, more, next, e := repo.MessagePage(c.Request.Context(), u, c.Param("conversation"), before)
	if officeHandlerError(c, e) {
		return
	}
	response.Success(c, gin.H{"messages": messages, "runs": runs, "has_more": more, "next_before": next})
}
func (h *OfficeAgentHandler) SubmitChat(c *gin.Context) {
	u, ok := officeUser(c)
	if !ok {
		return
	}
	var in service.WorkbenchChatSubmit
	if !officeDecode(c, &in, 40<<10) {
		return
	}
	run, e := h.service.SubmitChat(c.Request.Context(), u, c.Param("conversation"), in)
	if officeHandlerError(c, e) {
		return
	}
	response.Accepted(c, run)
}
func (h *OfficeAgentHandler) ChatAdvance(c *gin.Context) {
	u, ok := officeUser(c)
	if !ok {
		return
	}
	run, err := h.service.AdvanceWorkbench(c.Request.Context(), u, c.Param("id"))
	if officeHandlerError(c, err) {
		return
	}
	response.Success(c, run)
}

func (h *OfficeAgentHandler) ChatRun(c *gin.Context) {
	u, ok := officeUser(c)
	if !ok {
		return
	}
	run, e := h.service.PublicRun(c.Request.Context(), u, c.Param("id"))
	if officeHandlerError(c, e) {
		return
	}
	response.Success(c, run)
}
func (h *OfficeAgentHandler) ChatCancel(c *gin.Context) {
	u, ok := officeUser(c)
	if !ok {
		return
	}
	run, e := h.service.PublicCancel(c.Request.Context(), u, c.Param("id"))
	if officeHandlerError(c, e) {
		return
	}
	response.Success(c, run)
}
func (h *OfficeAgentHandler) ChatOperations(c *gin.Context) {
	u, ok := officeUser(c)
	if !ok {
		return
	}
	repo, e := h.service.AdminRepository()
	if officeHandlerError(c, e) {
		return
	}
	items, e := repo.RunUsage(c.Request.Context(), u, c.Param("id"), false)
	if officeHandlerError(c, e) {
		return
	}
	response.Success(c, gin.H{"items": items})
}

func (h *OfficeAgentHandler) ChatToolSummaries(c *gin.Context) {
	user, ok := officeUser(c)
	if !ok {
		return
	}
	c.Header("Cache-Control", "no-store")
	items, err := h.service.WorkbenchToolSummaries(c.Request.Context(), user, c.Param("id"))
	if !officeHandlerError(c, err) {
		response.Success(c, gin.H{"items": items})
	}
}

func (h *OfficeAgentHandler) ChatToolDetail(c *gin.Context) {
	user, ok := officeUser(c)
	if !ok {
		return
	}
	c.Header("Cache-Control", "no-store")
	item, err := h.service.WorkbenchToolDetail(c.Request.Context(), user, c.Param("id"), c.Param("operation"), false)
	if !officeHandlerError(c, err) {
		response.Success(c, item)
	}
}

func (h *OfficeAgentHandler) Input(c *gin.Context) {
	u, ok := officeUser(c)
	if !ok {
		return
	}
	var in struct {
		RequestID string `json:"request_id"`
		Answers   []struct {
			QuestionID string   `json:"question_id"`
			Values     []string `json:"values"`
		} `json:"answers"`
	}
	if !officeDecode(c, &in, 32768) {
		return
	}
	repo, e := h.service.ChatRepository()
	if officeHandlerError(c, e) {
		return
	}
	value, _ := json.Marshal(in)
	run, e := repo.ResumeInput(c.Request.Context(), u, c.Param("id"), in.RequestID, value)
	if officeHandlerError(c, e) {
		return
	}
	response.Success(c, run)
}
func (h *OfficeAgentHandler) SelectAnswer(c *gin.Context) {
	u, ok := officeUser(c)
	if !ok {
		return
	}
	repo, e := h.service.ChatRepository()
	if officeHandlerError(c, e) {
		return
	}
	run, e := repo.SelectAnswer(c.Request.Context(), u, c.Param("id"))
	if officeHandlerError(c, e) {
		return
	}
	response.Success(c, run)
}

// SSE is a read-only subscription. It never owns the run or reissues a model
// call. Each event was committed first; slow/disconnected clients reconnect.
func (h *OfficeAgentHandler) Stream(c *gin.Context) {
	u, ok := officeUser(c)
	if !ok {
		return
	}
	after, ok := officeAfter(c)
	if !ok {
		return
	}
	if raw := c.GetHeader("Last-Event-ID"); raw != "" && after == 0 {
		v, e := strconv.ParseInt(raw, 10, 64)
		if e != nil || v < 0 {
			officeHandlerError(c, service.ErrOfficeInvalid)
			return
		}
		after = v
	}
	run, e := h.service.PublicRun(c.Request.Context(), u, c.Param("id"))
	if officeHandlerError(c, e) {
		return
	}
	if _, ok = c.Writer.(http.Flusher); !ok {
		response.Error(c, 500, "Streaming unavailable")
		return
	}
	h.streamMu.Lock()
	if h.streams == nil {
		h.streams = make(map[int64]int)
	}
	if h.streamTotal >= 128 || h.streams[u] >= 4 {
		h.streamMu.Unlock()
		response.Error(c, 429, "Too many active streams")
		return
	}
	h.streams[u]++
	h.streamTotal++
	h.streamMu.Unlock()
	defer func() {
		h.streamMu.Lock()
		h.streams[u]--
		h.streamTotal--
		if h.streams[u] == 0 {
			delete(h.streams, u)
		}
		h.streamMu.Unlock()
	}()
	c.Header("Content-Type", "text/event-stream")
	c.Header("Cache-Control", "no-cache, no-store")
	c.Header("X-Accel-Buffering", "no")
	c.Status(200)
	c.Writer.Flush()
	controller := http.NewResponseController(c.Writer)
	ctx, cancel := context.WithTimeout(c.Request.Context(), 5*time.Minute)
	defer cancel()
	tick := time.NewTicker(500 * time.Millisecond)
	defer tick.Stop()
	heart := time.NewTicker(15 * time.Second)
	defer heart.Stop()
	send := func() bool {
		_ = controller.SetWriteDeadline(time.Now().Add(20 * time.Second))
		events, err := h.service.PublicEvents(ctx, u, run.ID, after)
		if err != nil {
			return false
		}
		for _, event := range events {
			data, _ := json.Marshal(event)
			if _, err = fmt.Fprintf(c.Writer, "id: %d\nevent: office\ndata: %s\n\n", event.Sequence, data); err != nil {
				return false
			}
			after = event.Sequence
		}
		c.Writer.Flush()
		latest, err := h.service.PublicRun(ctx, u, run.ID)
		if err != nil {
			return false
		}
		// A terminal snapshot can appear while its committed event backlog still
		// exceeds one page. Drain the next batch before closing this stream.
		return len(events) > 0 || latest.Status == service.OfficeRunQueued || latest.Status == service.OfficeRunRunning || latest.Status == service.OfficeRunRecovering
	}
	for {
		if !send() {
			return
		}
		select {
		case <-ctx.Done():
			return
		case <-tick.C:
		case <-heart.C:
			if _, e = fmt.Fprint(c.Writer, ": heartbeat\n\n"); e != nil {
				return
			}
			c.Writer.Flush()
		}
	}
}
func (h *OfficeAgentHandler) Checkpoint(c *gin.Context) {
	lease := h.lease(c, false)
	if lease == nil {
		return
	}
	repo, e := h.service.ChatRepository()
	if officeHandlerError(c, e) {
		return
	}
	data, version, e := repo.Checkpoint(c.Request.Context(), *lease)
	if officeHandlerError(c, e) {
		return
	}
	response.Success(c, gin.H{"data": data, "version": version})
}
func (h *OfficeAgentHandler) SaveCheckpoint(c *gin.Context) {
	lease := h.lease(c, false)
	if lease == nil {
		return
	}
	var in struct {
		ExpectedVersion int64           `json:"expected_version"`
		WriteID         string          `json:"write_id"`
		Data            json.RawMessage `json:"data"`
	}
	if !officeDecode(c, &in, (1<<20)+4096) {
		return
	}
	repo, e := h.service.ChatRepository()
	if officeHandlerError(c, e) {
		return
	}
	v, e := repo.SaveCheckpoint(c.Request.Context(), *lease, in.Data, in.ExpectedVersion, in.WriteID)
	if officeHandlerError(c, e) {
		return
	}
	response.Success(c, gin.H{"version": v})
}
func (h *OfficeAgentHandler) Wait(c *gin.Context) {
	lease := h.lease(c, false)
	if lease == nil {
		return
	}
	var in struct {
		RequestID string          `json:"request_id"`
		Payload   json.RawMessage `json:"payload"`
	}
	if !officeDecode(c, &in, 40<<10) {
		return
	}
	repo, e := h.service.ChatRepository()
	if officeHandlerError(c, e) {
		return
	}
	run, e := repo.WaitForInput(c.Request.Context(), *lease, in.RequestID, in.Payload)
	if officeHandlerError(c, e) {
		return
	}
	response.Success(c, run)
}
func (h *OfficeAgentHandler) Search(c *gin.Context) {
	lease := h.lease(c, false)
	if lease == nil {
		return
	}
	var in struct {
		OperationID string `json:"operation_id"`
		Query       string `json:"query"`
		MaxResults  int    `json:"max_results"`
	}
	if !officeDecode(c, &in, 4096) {
		return
	}
	value, e := h.service.Search(c.Request.Context(), *lease, in.OperationID, in.Query, in.MaxResults)
	if officeHandlerError(c, e) {
		return
	}
	response.Success(c, value)
}
func (h *OfficeAgentHandler) ReadWeb(c *gin.Context) {
	lease := h.lease(c, false)
	if lease == nil {
		return
	}
	var in struct {
		OperationID string `json:"operation_id"`
		URL         string `json:"url"`
	}
	if !officeDecode(c, &in, 8192) {
		return
	}
	value, e := h.service.ReadWeb(c.Request.Context(), *lease, in.OperationID, in.URL)
	if officeHandlerError(c, e) {
		return
	}
	response.Success(c, value)
}

func (h *OfficeAgentHandler) SaveContextSummary(c *gin.Context) {
	lease := h.lease(c, false)
	if lease == nil {
		return
	}
	var in struct {
		OperationID      string `json:"operation_id"`
		ExpectedVersion  int64  `json:"expected_version"`
		Content          string `json:"content"`
		ThroughMessageID string `json:"through_message_id"`
		SourceHash       string `json:"source_hash"`
	}
	if !officeDecode(c, &in, 20<<10) {
		return
	}
	repo, e := h.service.ChatRepository()
	if officeHandlerError(c, e) {
		return
	}
	version, e := repo.SaveContextSummary(c.Request.Context(), *lease, service.OfficeContextSummary{Content: in.Content, ThroughMessageID: in.ThroughMessageID, SourceHash: in.SourceHash}, in.ExpectedVersion, in.OperationID)
	if officeHandlerError(c, e) {
		return
	}
	response.Success(c, gin.H{"version": version})
}
