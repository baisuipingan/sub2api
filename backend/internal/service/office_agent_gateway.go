package service

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"image"
	_ "image/jpeg"
	_ "image/png"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/pkg/ctxkey"
)

const officeMaxModelResponse = 1 << 20

// OfficeGatewayReply is an internal, bounded response inbox. It is never a
// public conversation message and never includes an Authorization header.
type OfficeGatewayReply struct {
	Status      int    `json:"status"`
	ContentType string `json:"content_type"`
	Body        []byte `json:"body"`
	RequestID   string `json:"request_id"`
}

// InvokeModel uses the ordinary gateway router and persists bounded results
// before returning them to the worker. Assistant SSE text is exposed separately
// as Go-owned conversation events; completed inbox replay never calls upstream.
func (s *OfficeAgentService) InvokeModel(ctx context.Context, lease OfficeRunLease, operationID string, body []byte) (*OfficeGatewayReply, error) {
	return s.InvokeModelPurpose(ctx, lease, operationID, body, "")
}
func (s *OfficeAgentService) InvokeModelPurpose(ctx context.Context, lease OfficeRunLease, operationID string, body []byte, purpose string) (*OfficeGatewayReply, error) {
	maxInput := OfficeMaxOperationInputBytes
	if purpose == "context_compaction" {
		maxInput = OfficeMaxCompactionInputBytes
	} else if purpose != "" {
		return nil, ErrOfficeInvalid
	}
	if !s.Enabled() {
		return nil, ErrOfficeDisabled
	}
	if !officeIdentifier(operationID, 128) || len(body) == 0 || len(body) > maxInput {
		return nil, ErrOfficeInvalid
	}
	normalized, err := normalizeOfficeModelRequestLimit(body, lease.Run, maxInput)
	if err != nil {
		return nil, err
	}
	if err := s.repo.CheckLease(ctx, lease); err != nil {
		return nil, err
	}
	key, err := s.authorize(ctx, lease.Run)
	if err != nil {
		return nil, err
	}
	requestID := "office-" + officeDigest([]byte(lease.Run.ID + ":" + operationID))[:40]
	op, err := s.prepareOfficeModelOperation(ctx, lease, OfficeOperationPrepare{ID: operationID, Kind: OfficeOperationModel,
		InputHash: officeDigest(append([]byte(purpose+":"), normalized...)), Input: normalized, RequestID: requestID}, 5*time.Second)
	if err != nil {
		if errors.Is(err, ErrOfficeSettlementPending) {
			pauseCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			defer cancel()
			if _, pauseErr := s.Suspend(pauseCtx, lease, "BILLING_PENDING"); pauseErr != nil {
				return nil, pauseErr
			}
		}
		return nil, err
	}
	if op.State == OfficeOperationCompleted && op.Result != nil {
		var reply OfficeGatewayReply
		if json.Unmarshal(op.Result.Output, &reply) != nil {
			return nil, ErrOfficeResultUnknown
		}
		return &reply, nil
	}
	if op.State != OfficeOperationPrepared {
		return nil, ErrOfficeResultUnknown
	}
	select {
	case s.modelSlots <- struct{}{}:
		defer func() { <-s.modelSlots }()
	default:
		return nil, ErrOfficeCapacity // Still prepared; no supplier call was sent.
	}
	if _, err := s.repo.DispatchOperation(ctx, lease, operationID); err != nil {
		return nil, err
	}
	// No request-derived Gin context or response writer is retained. The bounded
	// call may finish its usage/result after the worker connection disconnects.
	deadline := time.Now().Add(time.Duration(min(lease.Run.Limits.MaxRunSeconds, 300)) * time.Second)
	if lease.Run.StartedAt != nil {
		deadline = minOfficeTime(deadline, lease.Run.StartedAt.Add(time.Duration(int64(lease.Run.Limits.MaxRunSeconds)+lease.Run.PausedSeconds)*time.Second))
	}
	callCtx, cancel := context.WithDeadline(context.WithoutCancel(ctx), deadline)
	defer cancel()
	callCtx = context.WithValue(callCtx, ctxkey.ClientRequestID, requestID)
	request, err := http.NewRequestWithContext(callCtx, http.MethodPost, "/v1/chat/completions", bytes.NewReader(normalized))
	if err != nil {
		return nil, s.unknownOfficeOperation(lease, operationID)
	}
	request.Header.Set("Authorization", "Bearer "+key)
	request.Header.Set("Content-Type", "application/json")
	request.Header.Set("User-Agent", "sub2api-office-agent-pilot")
	request.RemoteAddr = "127.0.0.1:0"
	s.mu.RLock()
	router := s.router
	s.mu.RUnlock()
	writer := &officeBoundedWriter{header: make(http.Header), limit: officeMaxModelResponse, cancel: cancel}
	var requested struct {
		Stream bool `json:"stream"`
	}
	_ = json.Unmarshal(normalized, &requested)
	var live *officeLiveStream
	if requested.Stream && purpose != "context_compaction" {
		live = &officeLiveStream{ctx: callCtx, service: s, lease: lease, operation: operationID}
		writer.onWrite = live.write
	}
	// Cancellation/lease loss stops new work; bounded monitoring also terminates
	// this in-flight request. Any late usage remains the original gateway's job.
	stop := make(chan struct{})
	done := make(chan struct{})
	go func() {
		defer close(done)
		ticker := time.NewTicker(2 * time.Second)
		defer ticker.Stop()
		for {
			select {
			case <-stop:
				return
			case <-callCtx.Done():
				return
			case <-ticker.C:
				check, checkCancel := context.WithTimeout(callCtx, time.Second)
				err := s.repo.CheckLease(check, lease)
				checkCancel()
				if err != nil {
					cancel()
					return
				}
			}
		}
	}()
	func() {
		defer close(stop)
		defer func() {
			if recover() != nil {
				writer.failed = true
			}
		}()
		router.ServeHTTP(writer, request)
	}()
	<-done
	if live != nil && live.flush() != nil {
		writer.failed = true
	}
	if writer.failed || !officeResponseComplete(writer) {
		return nil, s.unknownOfficeOperation(lease, operationID)
	}
	reply := &OfficeGatewayReply{Status: writer.status, ContentType: writer.header.Get("Content-Type"),
		Body: writer.body.Bytes(), RequestID: requestID}
	encoded, err := json.Marshal(reply)
	if err != nil || len(encoded) > OfficeMaxOperationResultBytes {
		return nil, s.unknownOfficeOperation(lease, operationID)
	}
	// A lost downstream acknowledgement can re-read this inbox; no new model
	// invocation is made. A cancelled run may accept only this original result.
	storeCtx, storeCancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer storeCancel()
	_, err = s.repo.RecordOperationResult(storeCtx, lease, operationID, OfficeOperationResult{Output: encoded, HTTPStatus: reply.Status})
	if err != nil {
		return nil, ErrOfficeResultUnknown
	}
	return reply, nil
}

// Only admission is polled, before any new provider request exists. The
// ordinary gateway writes usage asynchronously; a short delay must neither
// exhaust a valid task budget nor permit another charge before it is visible.
func (s *OfficeAgentService) prepareOfficeModelOperation(ctx context.Context, lease OfficeRunLease, in OfficeOperationPrepare, waitBudget time.Duration) (*OfficeOperation, error) {
	until := time.Now().Add(waitBudget)
	for {
		op, _, err := s.repo.PrepareOperation(ctx, lease, in)
		if !errors.Is(err, ErrOfficeSettlementPending) || !time.Now().Before(until) {
			return op, err
		}
		timer := time.NewTimer(min(100*time.Millisecond, time.Until(until)))
		select {
		case <-ctx.Done():
			timer.Stop()
			return nil, ctx.Err()
		case <-timer.C:
		}
	}
}

func (s *OfficeAgentService) unknownOfficeOperation(lease OfficeRunLease, operationID string) error {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	_ = s.repo.MarkOperationUnknown(ctx, lease, operationID, "model result could not be confirmed; automatic replay is disabled")
	return ErrOfficeResultUnknown
}

func normalizeOfficeModelRequest(body []byte, run OfficeRun) ([]byte, error) {
	return normalizeOfficeModelRequestLimit(body, run, OfficeMaxOperationInputBytes)
}
func normalizeOfficeModelRequestLimit(body []byte, run OfficeRun, maxInput int) ([]byte, error) {
	var fields map[string]json.RawMessage
	if json.Unmarshal(body, &fields) != nil || fields == nil {
		return nil, ErrOfficeInvalid
	}
	allowed := map[string]bool{"model": true, "messages": true, "tools": true, "tool_choice": true,
		"parallel_tool_calls": true, "max_tokens": true, "max_completion_tokens": true, "stream": true,
		"stream_options": true, "temperature": true, "top_p": true, "reasoning_effort": true, "response_format": true}
	for name := range fields {
		if !allowed[name] {
			return nil, ErrOfficeInvalid
		}
	}
	var model string
	if json.Unmarshal(fields["model"], &model) != nil || model != run.Model {
		return nil, ErrOfficeRouteChanged
	}
	var messages []struct {
		Role      string          `json:"role"`
		Content   json.RawMessage `json:"content"`
		ToolCalls json.RawMessage `json:"tool_calls"`
		ToolID    string          `json:"tool_call_id"`
	}
	if json.Unmarshal(fields["messages"], &messages) != nil || len(messages) == 0 || len(messages) > 2000 {
		return nil, ErrOfficeInvalid
	}
	var vision struct {
		SupportsVision bool `json:"supports_vision"`
	}
	_ = json.Unmarshal(run.Parameters, &vision)
	images := 0
	for _, message := range messages {
		if message.Role != "system" && message.Role != "user" && message.Role != "assistant" && message.Role != "tool" {
			return nil, ErrOfficeInvalid
		}
		if len(message.Content) == 0 || string(message.Content) == "null" {
			continue
		}
		var text string
		if json.Unmarshal(message.Content, &text) == nil {
			continue
		}
		if !vision.SupportsVision || run.Mode != "office" || message.Role != "user" {
			return nil, ErrOfficeInvalid
		}
		var parts []struct {
			Type     string `json:"type"`
			Text     string `json:"text"`
			ImageURL *struct {
				URL string `json:"url"`
			} `json:"image_url"`
		}
		if json.Unmarshal(message.Content, &parts) != nil || len(parts) == 0 || len(parts) > 4 {
			return nil, ErrOfficeInvalid
		}
		for _, part := range parts {
			switch part.Type {
			case "text":
				if part.ImageURL != nil {
					return nil, ErrOfficeInvalid
				}
			case "image_url":
				images++
				if images > 1 || part.ImageURL == nil {
					return nil, ErrOfficeInvalid
				}
				u := part.ImageURL.URL
				var encoded, mime string
				if strings.HasPrefix(u, "data:image/png;base64,") {
					mime = "png"
					encoded = strings.TrimPrefix(u, "data:image/png;base64,")
				} else if strings.HasPrefix(u, "data:image/jpeg;base64,") {
					mime = "jpeg"
					encoded = strings.TrimPrefix(u, "data:image/jpeg;base64,")
				} else {
					return nil, ErrOfficeInvalid
				}
				if len(encoded) > 128<<10 {
					return nil, ErrOfficeInvalid
				}
				data, e := base64.StdEncoding.Strict().DecodeString(encoded)
				if e != nil || len(data) > 96<<10 {
					return nil, ErrOfficeInvalid
				}
				cfg, format, e := image.DecodeConfig(bytes.NewReader(data))
				if e != nil || format != mime || cfg.Width < 1 || cfg.Height < 1 || cfg.Width > 4096 || cfg.Height > 4096 || int64(cfg.Width)*int64(cfg.Height) > 4<<20 {
					return nil, ErrOfficeInvalid
				}
			default:
				return nil, ErrOfficeInvalid
			}
		}
	}
	var maxTokens, maxCompletion int
	for name, ptr := range map[string]*int{"max_tokens": &maxTokens, "max_completion_tokens": &maxCompletion} {
		if raw, ok := fields[name]; ok {
			if json.Unmarshal(raw, ptr) != nil || *ptr < 1 || *ptr > run.Limits.MaxOutputTokens {
				return nil, ErrOfficeBudgetExceeded
			}
		}
	}
	if maxTokens != 0 && maxCompletion != 0 {
		return nil, ErrOfficeInvalid
	}
	if maxTokens == 0 && maxCompletion == 0 {
		return nil, ErrOfficeBudgetExceeded // Never silently permit unbounded output.
	}
	if raw, ok := fields["tools"]; ok {
		var tools []struct {
			Type     string `json:"type"`
			Function struct {
				Name string `json:"name"`
			} `json:"function"`
		}
		if json.Unmarshal(raw, &tools) != nil || len(tools) > 12 {
			return nil, ErrOfficeInvalid
		}
		for _, tool := range tools {
			if tool.Function.Name == "inspect_rendered_image" && !vision.SupportsVision {
				return nil, ErrOfficeInvalid
			}
			if tool.Type != "function" || !officeApprovedTool(tool.Function.Name) || ((tool.Function.Name == "web_search" || tool.Function.Name == "web_read") && !run.SearchEnabled) || (run.Mode == "chat" && tool.Function.Name != "web_search" && tool.Function.Name != "web_read") {
				return nil, ErrOfficeInvalid
			}
		}
	}
	// The runtime may request several tool calls; it must execute them serially.
	if len(fields["tools"]) > 0 {
		fields["parallel_tool_calls"] = json.RawMessage("false")
	} else {
		delete(fields, "parallel_tool_calls")
	}
	normalized, err := json.Marshal(fields)
	if err != nil || len(normalized) > maxInput {
		return nil, ErrOfficeInvalid
	}
	return normalized, nil
}

func officeApprovedTool(name string) bool {
	switch name {
	case "execute_office_python", "read_file", "write_file", "edit_file", "ls", "glob", "grep", "write_todos", "ask_user", "web_search", "web_read", "inspect_rendered_image":
		return true
	default:
		return false
	}
}

type officeBoundedWriter struct {
	header  http.Header
	body    bytes.Buffer
	status  int
	limit   int
	failed  bool
	cancel  context.CancelFunc
	onWrite func([]byte) error
}

func (w *officeBoundedWriter) Header() http.Header { return w.header }
func (w *officeBoundedWriter) WriteHeader(code int) {
	if w.status == 0 {
		w.status = code
	}
}
func (w *officeBoundedWriter) Write(data []byte) (int, error) {
	if w.status == 0 {
		w.status = http.StatusOK
	}
	if w.failed || len(data) > w.limit-w.body.Len() {
		w.failed = true
		w.cancel()
		return 0, io.ErrShortBuffer
	}
	if w.onWrite != nil && w.status >= 200 && w.status < 300 && strings.HasPrefix(w.header.Get("Content-Type"), "text/event-stream") {
		if err := w.onWrite(data); err != nil {
			w.failed = true
			w.cancel()
			return 0, err
		}
	}
	return w.body.Write(data)
}
func (w *officeBoundedWriter) Flush() {
	if w.status == 0 {
		w.status = http.StatusOK
	}
}

func officeResponseComplete(w *officeBoundedWriter) bool {
	if w.status < 200 || w.status > 599 || w.body.Len() == 0 {
		return false
	}
	if strings.HasPrefix(w.header.Get("Content-Type"), "text/event-stream") {
		// A model can mention the literal text in a JSON delta. Only an actual
		// complete SSE data frame terminates a stream.
		var dataLines []string
		for _, line := range strings.Split(strings.ReplaceAll(w.body.String(), "\r\n", "\n"), "\n") {
			if line == "" {
				if strings.Join(dataLines, "\n") == "[DONE]" {
					return true
				}
				dataLines = dataLines[:0]
				continue
			}
			if strings.HasPrefix(line, "data:") {
				dataLines = append(dataLines, strings.TrimPrefix(strings.TrimPrefix(line, "data:"), " "))
			}
		}
		return false
	}
	return json.Valid(w.body.Bytes())
}

func (s *OfficeAgentService) PrepareTool(ctx context.Context, lease OfficeRunLease, operationID string, input json.RawMessage) (*OfficeOperation, bool, error) {
	if !officeIdentifier(operationID, 128) || len(input) == 0 || len(input) > OfficeMaxOperationInputBytes || !json.Valid(input) {
		return nil, false, ErrOfficeInvalid
	}
	if _, err := s.authorize(ctx, lease.Run); err != nil {
		return nil, false, err
	}
	var canonical any
	decoder := json.NewDecoder(bytes.NewReader(input))
	decoder.UseNumber()
	if decoder.Decode(&canonical) != nil {
		return nil, false, ErrOfficeInvalid
	}
	encoded, _ := json.Marshal(canonical)
	op, _, err := s.repo.PrepareOperation(ctx, lease, OfficeOperationPrepare{ID: operationID, Kind: OfficeOperationTool,
		InputHash: officeDigest(encoded), Input: encoded, RequestID: "office-tool-" + officeDigest([]byte(lease.Run.ID + ":" + operationID))[:32]})
	if err != nil {
		return nil, false, err
	}
	if op.State == OfficeOperationCompleted {
		return op, false, nil
	}
	if op.State != OfficeOperationPrepared {
		return nil, false, ErrOfficeResultUnknown
	}
	op, err = s.repo.DispatchOperation(ctx, lease, operationID)
	return op, err == nil, err
}

func (s *OfficeAgentService) CompleteTool(ctx context.Context, lease OfficeRunLease, operationID string, result OfficeOperationResult) (*OfficeOperation, error) {
	if !officeIdentifier(operationID, 128) || len(result.Output) > OfficeMaxOperationResultBytes || !json.Valid(result.Output) {
		return nil, ErrOfficeInvalid
	}
	op, err := s.repo.GetOperation(ctx, lease.Run.UserID, lease.Run.ID, operationID)
	if err != nil || op.Kind != OfficeOperationTool {
		return nil, fmt.Errorf("%w: tool operation", ErrOfficeNotFound)
	}
	return s.repo.RecordOperationResult(ctx, lease, operationID, result)
}
