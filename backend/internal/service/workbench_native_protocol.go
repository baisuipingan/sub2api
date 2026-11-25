package service

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/url"
	"slices"
	"strings"

	"github.com/Wei-Shaw/sub2api/internal/pkg/ctxkey"
)

type workbenchProtocol string

const (
	workbenchProtocolGemini          workbenchProtocol = "gemini"
	workbenchProtocolResponses       workbenchProtocol = "responses"
	workbenchProtocolAnthropic       workbenchProtocol = "anthropic"
	workbenchProtocolChatCompletions workbenchProtocol = "chat_completions"
)

type workbenchNativeRequest struct {
	ConstrainGeminiThinkingBudget bool
	Protocol                      workbenchProtocol
	Model                         string
	ProfileModel                  string // Canonical capability profile, including when Model is a public alias.
	System                        string
	ReasoningEffort               string
	StableDeveloperInstructions   bool
	ReplayToolReasoning           bool // Chat Completions suppliers that require reasoning_content replay.
	History                       []workbenchNativeMessage
	Tools                         []workbenchNativeTool
	Transcript                    []workbenchNativeExchange
	MaxOutputTokens               int
}

type workbenchNativeMessage struct {
	ContextReferences []string // File and source references from saved history.
	Role              string
	Text              string
	Images            []workbenchNativeImage
	Native            json.RawMessage // An earlier assistant exchange in the same protocol.
	Results           []workbenchNativeToolResult
}

type workbenchNativeImage struct {
	MIME string
	Data string // Base64 of a server-authorized resource version.
}

type workbenchNativeTool struct {
	Name        string
	Description string
	Schema      json.RawMessage
}

type workbenchNativeExchange struct {
	Native  json.RawMessage
	Results []workbenchNativeToolResult
}

type workbenchNativeToolResult struct {
	ID     string
	Name   string
	Output json.RawMessage
}

type workbenchNativeCall struct {
	ID        string          `json:"id"`
	Name      string          `json:"name"`
	Arguments json.RawMessage `json:"arguments"`
}

type workbenchNativeReply struct {
	Text             string                `json:"text"`
	ReasoningSummary string                `json:"reasoning_summary,omitempty"`
	Native           json.RawMessage       `json:"native"`
	Calls            []workbenchNativeCall `json:"calls,omitempty"`
	FinishReason     string                `json:"finish_reason,omitempty"`
}

type workbenchNativeDelta struct {
	Text             string
	ReasoningSummary string
}

type workbenchGeminiContent struct {
	Role  string            `json:"role,omitempty"`
	Parts []json.RawMessage `json:"parts"`
}

func isWorkbenchGatewayRequest(ctx context.Context) bool {
	groupID, _ := ctx.Value(ctxkey.WorkbenchGroupID).(int64)
	return groupID > 0
}

func buildWorkbenchNativeRequest(in workbenchNativeRequest) (string, []byte, error) {
	if !officeModelID(in.Model) || len(in.History) == 0 {
		return "", nil, ErrOfficeInvalid
	}
	for _, tool := range in.Tools {
		if !officeIdentifier(tool.Name, 64) || !workbenchJSONObject(tool.Schema) {
			return "", nil, ErrOfficeInvalid
		}
	}
	var endpoint string
	var body map[string]any
	var err error
	switch in.Protocol {
	case workbenchProtocolGemini:
		if !IsSafeGeminiModelPathSegment(in.Model) {
			return "", nil, ErrOfficeInvalid
		}
		endpoint = "/v1beta/models/" + url.PathEscape(in.Model) + ":streamGenerateContent?alt=sse"
		body, err = buildWorkbenchGeminiRequest(in)
	case workbenchProtocolResponses:
		endpoint = "/v1/responses"
		body, err = buildWorkbenchResponsesRequest(in)
	case workbenchProtocolAnthropic:
		endpoint = "/v1/messages"
		body, err = buildWorkbenchAnthropicRequest(in)
	case workbenchProtocolChatCompletions:
		endpoint = "/v1/chat/completions"
		body, err = buildWorkbenchChatRequest(in)
	default:
		return "", nil, ErrOfficeInvalid
	}
	if err != nil {
		return "", nil, err
	}
	encoded, err := json.Marshal(body)
	return endpoint, encoded, err
}

func buildWorkbenchGeminiRequest(in workbenchNativeRequest) (map[string]any, error) {
	contents := make([]workbenchGeminiContent, 0, len(in.History)+2*len(in.Transcript))
	err := visitWorkbenchHistory(in, func(message workbenchNativeMessage) error {
		role := message.Role
		if role == "assistant" {
			role = "model"
		}
		parts := make([]json.RawMessage, 0, 1+len(message.Images))
		if message.Text != "" {
			parts = append(parts, workbenchNativeJSON(map[string]string{"text": message.Text}))
		}
		for _, image := range message.Images {
			parts = append(parts, workbenchNativeJSON(map[string]any{"inlineData": map[string]string{"mimeType": image.MIME, "data": image.Data}}))
		}
		if len(parts) > 0 {
			contents = append(contents, workbenchGeminiContent{Role: role, Parts: parts})
		}
		return nil
	}, func(exchange workbenchNativeExchange) error {
		var model workbenchGeminiContent
		if json.Unmarshal(exchange.Native, &model) != nil || model.Role != "model" || len(model.Parts) == 0 {
			return ErrOfficeInvalid
		}
		calls, err := workbenchGeminiCalls(model.Parts)
		if err != nil {
			return err
		}
		results, err := orderWorkbenchToolResults(calls, exchange.Results)
		if err != nil {
			return err
		}
		contents = append(contents, model)
		if len(results) > 0 {
			parts := make([]json.RawMessage, 0, len(results))
			for index, result := range results {
				response := map[string]any{"name": result.Name, "response": workbenchGeminiObject(result.Output)}
				if calls[index].nativeID != "" {
					response["id"] = calls[index].nativeID
				}
				parts = append(parts, workbenchNativeJSON(map[string]any{"functionResponse": response}))
			}
			contents = append(contents, workbenchGeminiContent{Role: "user", Parts: parts})
		}
		return nil
	})
	if err != nil || len(contents) == 0 {
		return nil, ErrOfficeInvalid
	}
	body := map[string]any{"contents": contents}
	if in.System != "" {
		body["systemInstruction"] = workbenchGeminiContent{Parts: []json.RawMessage{workbenchNativeJSON(map[string]string{"text": in.System})}}
	}
	if len(in.Tools) > 0 {
		declarations := make([]map[string]any, 0, len(in.Tools))
		for _, tool := range in.Tools {
			declarations = append(declarations, map[string]any{"name": tool.Name, "description": tool.Description, "parameters": tool.Schema})
		}
		body["tools"] = []map[string]any{{"functionDeclarations": declarations}}
	}
	generation := make(map[string]any)
	if in.MaxOutputTokens > 0 {
		generation["maxOutputTokens"] = in.MaxOutputTokens
	}
	if thinking := workbenchGeminiThinking(in); len(thinking) > 0 {
		generation["thinkingConfig"] = thinking
	}
	if len(generation) > 0 {
		body["generationConfig"] = generation
	}
	return body, nil
}

func workbenchGeminiDefaultReasoningEffort(model string) string {
	switch {
	case strings.HasPrefix(model, "gemini-4-hydrogen"), strings.HasPrefix(model, "gemini-4-neon"):
		return "high"
	case strings.HasPrefix(model, "gemini-4"):
		return "medium"
	default:
		return ""
	}
}

func workbenchGeminiAcceptsEffort(model, effort string) bool {
	if strings.HasPrefix(model, "gemini-2.5") {
		return slices.Contains([]string{"minimal", "low", "medium", "high"}, effort)
	}
	return slices.Contains([]string{"minimal", "low", "medium", "high", "xhigh"}, effort)
}

func workbenchGeminiThinking(in workbenchNativeRequest) map[string]any {
	model := strings.ToLower(strings.TrimSpace(in.ProfileModel))
	if model == "" {
		model = strings.ToLower(in.Model)
	}
	effort := in.ReasoningEffort
	if effort == "" {
		effort = workbenchGeminiDefaultReasoningEffort(model)
	}
	if strings.HasPrefix(model, "gemini-2.5") {
		if effort == "" {
			return map[string]any{"includeThoughts": true}
		}
		budgets := map[string]int{"minimal": 512, "low": 1024, "medium": 8192, "high": 24576}
		if budget, ok := budgets[effort]; ok {
			if in.ConstrainGeminiThinkingBudget && in.MaxOutputTokens > 0 && budget >= in.MaxOutputTokens {
				// Gemini 2.5 counts thinking and the visible response in the same
				// output budget. Reserve room for an answer if they would collide.
				budget = in.MaxOutputTokens / 2
				switch {
				case strings.Contains(model, "pro"):
					budget = max(budget, gemini25ProMinThinkingBudget)
				case strings.Contains(model, "flash-lite") && budget < gemini25FlashLiteMinThinkingBudget:
					budget = 0
				}
			}
			return map[string]any{"includeThoughts": budget > 0, "thinkingBudget": budget}
		}
		return map[string]any{"includeThoughts": true}
	}
	if slices.Contains([]string{"minimal", "low", "medium", "high", "xhigh"}, effort) {
		return map[string]any{"includeThoughts": true, "thinkingLevel": strings.ToUpper(effort)}
	}
	if strings.HasPrefix(model, "gemini-3") || strings.HasPrefix(model, "gemini-4") {
		return map[string]any{"includeThoughts": true}
	}
	return nil
}

func buildWorkbenchResponsesRequest(in workbenchNativeRequest) (map[string]any, error) {
	input := make([]json.RawMessage, 0, len(in.History)+2*len(in.Transcript))
	if in.StableDeveloperInstructions && in.System != "" {
		input = append(input, workbenchNativeJSON(map[string]any{
			"role":    "developer",
			"content": []map[string]string{{"type": "input_text", "text": in.System}},
		}))
	}
	err := visitWorkbenchHistory(in, func(message workbenchNativeMessage) error {
		if len(message.Images) == 0 {
			if message.Text != "" {
				input = append(input, workbenchNativeJSON(map[string]string{"role": message.Role, "content": message.Text}))
			}
			return nil
		}
		parts := make([]map[string]string, 0, 1+len(message.Images))
		if message.Text != "" {
			parts = append(parts, map[string]string{"type": "input_text", "text": message.Text})
		}
		for _, image := range message.Images {
			parts = append(parts, map[string]string{"type": "input_image", "image_url": workbenchNativeImageURL(image), "detail": "auto"})
		}
		input = append(input, workbenchNativeJSON(map[string]any{"role": message.Role, "content": parts}))
		return nil
	}, func(exchange workbenchNativeExchange) error {
		var items []json.RawMessage
		if json.Unmarshal(exchange.Native, &items) != nil || len(items) == 0 {
			return ErrOfficeInvalid
		}
		calls, err := workbenchResponsesCalls(items)
		if err != nil {
			return err
		}
		results, err := orderWorkbenchToolResults(calls, exchange.Results)
		if err != nil {
			return err
		}
		input = append(input, items...)
		for _, result := range results {
			input = append(input, workbenchNativeJSON(map[string]string{"type": "function_call_output", "call_id": result.ID, "output": string(result.Output)}))
		}
		return nil
	})
	if err != nil || len(input) == 0 {
		return nil, ErrOfficeInvalid
	}
	body := map[string]any{"model": in.Model, "input": input, "stream": true, "store": false}
	if in.System != "" && !in.StableDeveloperInstructions {
		body["instructions"] = in.System
	}
	if in.ReasoningEffort != "" {
		body["reasoning"] = map[string]string{"effort": in.ReasoningEffort, "summary": "auto"}
	}
	// Encrypted reasoning is needed for stateless tool continuation, including
	// when the provider chooses its default effort.
	body["include"] = []string{"reasoning.encrypted_content"}
	if in.MaxOutputTokens > 0 {
		body["max_output_tokens"] = in.MaxOutputTokens
	}
	if len(in.Tools) > 0 {
		tools := make([]map[string]any, 0, len(in.Tools))
		for _, tool := range in.Tools {
			tools = append(tools, map[string]any{"type": "function", "name": tool.Name, "description": tool.Description, "parameters": tool.Schema, "strict": false})
		}
		body["tools"] = tools
	}
	return body, nil
}

func buildWorkbenchChatRequest(in workbenchNativeRequest) (map[string]any, error) {
	messages := make([]json.RawMessage, 0, len(in.History)+2*len(in.Transcript)+1)
	if in.System != "" {
		messages = append(messages, workbenchNativeJSON(map[string]string{"role": "system", "content": in.System}))
	}
	err := visitWorkbenchHistory(in, func(message workbenchNativeMessage) error {
		if len(message.Images) == 0 {
			if message.Text != "" {
				messages = append(messages, workbenchNativeJSON(map[string]string{"role": message.Role, "content": message.Text}))
			}
			return nil
		}
		parts := make([]map[string]any, 0, 1+len(message.Images))
		if message.Text != "" {
			parts = append(parts, map[string]any{"type": "text", "text": message.Text})
		}
		for _, image := range message.Images {
			parts = append(parts, map[string]any{"type": "image_url", "image_url": map[string]string{"url": workbenchNativeImageURL(image)}})
		}
		messages = append(messages, workbenchNativeJSON(map[string]any{"role": message.Role, "content": parts}))
		return nil
	}, func(exchange workbenchNativeExchange) error {
		var message workbenchChatMessage
		if json.Unmarshal(exchange.Native, &message) != nil || message.Role != "assistant" {
			return ErrOfficeInvalid
		}
		results, err := orderWorkbenchToolResults(workbenchChatCalls(message.ToolCalls), exchange.Results)
		if err != nil {
			return err
		}
		if !in.ReplayToolReasoning || len(message.ToolCalls) == 0 {
			message.Reasoning = ""
		}
		messages = append(messages, workbenchNativeJSON(message))
		for _, result := range results {
			messages = append(messages, workbenchNativeJSON(map[string]string{"role": "tool", "tool_call_id": result.ID, "content": string(result.Output)}))
		}
		return nil
	})
	if err != nil || len(messages) == 0 {
		return nil, ErrOfficeInvalid
	}
	body := map[string]any{"model": in.Model, "messages": messages, "stream": true, "stream_options": map[string]bool{"include_usage": true}}
	if in.ReasoningEffort != "" {
		body["reasoning_effort"] = in.ReasoningEffort
	}
	if in.MaxOutputTokens > 0 {
		body["max_tokens"] = in.MaxOutputTokens
	}
	if len(in.Tools) > 0 {
		tools := make([]map[string]any, 0, len(in.Tools))
		for _, tool := range in.Tools {
			tools = append(tools, map[string]any{"type": "function", "function": map[string]any{"name": tool.Name, "description": tool.Description, "parameters": tool.Schema}})
		}
		body["tools"] = tools
	}
	return body, nil
}

func visitWorkbenchHistory(in workbenchNativeRequest, plain func(workbenchNativeMessage) error, native func(workbenchNativeExchange) error) error {
	for _, message := range in.History {
		if message.Role != "user" && message.Role != "assistant" || len(message.Images) > 0 && message.Role != "user" {
			return ErrOfficeInvalid
		}
		if len(message.Native) == 0 {
			if err := plain(message); err != nil {
				return err
			}
			continue
		}
		if message.Role != "assistant" {
			return ErrOfficeInvalid
		}
		if err := native(workbenchNativeExchange{Native: message.Native, Results: message.Results}); err != nil {
			return err
		}
	}
	for _, exchange := range in.Transcript {
		if err := native(exchange); err != nil {
			return err
		}
	}
	return nil
}

type workbenchProtocolCall struct {
	workbenchNativeCall
	nativeID string
}

func orderWorkbenchToolResults(calls []workbenchProtocolCall, results []workbenchNativeToolResult) ([]workbenchNativeToolResult, error) {
	if len(calls) != len(results) {
		return nil, ErrOfficeInvalid
	}
	byID := make(map[string]workbenchNativeToolResult, len(results))
	for _, result := range results {
		byID[result.ID] = result
	}
	ordered := make([]workbenchNativeToolResult, 0, len(calls))
	for _, call := range calls {
		result, ok := byID[call.ID]
		if !ok || result.Name != call.Name || !json.Valid(result.Output) {
			return nil, ErrOfficeInvalid
		}
		delete(byID, call.ID)
		ordered = append(ordered, result)
	}
	return ordered, nil
}

func workbenchNativeJSON(value any) json.RawMessage {
	encoded, _ := json.Marshal(value)
	return encoded
}

func workbenchJSONObject(raw json.RawMessage) bool {
	trimmed := bytes.TrimSpace(raw)
	return len(trimmed) > 0 && trimmed[0] == '{' && json.Valid(trimmed)
}

func workbenchGeminiObject(raw json.RawMessage) json.RawMessage {
	if workbenchJSONObject(raw) {
		return raw
	}
	return workbenchNativeJSON(map[string]json.RawMessage{"result": raw})
}

func workbenchNativeImageURL(image workbenchNativeImage) string {
	return "data:" + image.MIME + ";base64," + image.Data
}

type workbenchNativeParseFailure struct{ reason string }

func (e workbenchNativeParseFailure) Error() string {
	return ErrOfficeResultUnknown.Error() + ": " + e.reason
}
func (workbenchNativeParseFailure) Unwrap() error { return ErrOfficeResultUnknown }

func workbenchNativeParseError(reason string) error {
	return workbenchNativeParseFailure{reason: reason}
}

type workbenchNativeUpstreamError struct {
	Reason string
}

func (e *workbenchNativeUpstreamError) Error() string { return "model stream ended with " + e.Reason }

// workbenchNativeStream can receive arbitrary HTTP writer chunks. Frame data
// and opaque reasoning stay server-side. Only text and public summaries are
// returned from Write.
type workbenchNativeStream struct {
	protocol workbenchProtocol
	pending  []byte
	frame    []byte
	text     strings.Builder
	summary  strings.Builder
	finish   string
	sawEvent bool

	geminiParts   []json.RawMessage
	responseItems map[int]json.RawMessage
	finalOutput   []json.RawMessage
	chatReasoning strings.Builder
	chatToolCalls map[int]*workbenchChatToolCall
	anthropic     workbenchAnthropicStream
}

func parseWorkbenchNativeResponse(protocol workbenchProtocol, body []byte) (workbenchNativeReply, error) {
	stream := &workbenchNativeStream{protocol: protocol}
	if _, err := stream.Write(body); err != nil {
		return workbenchNativeReply{}, err
	}
	return stream.Result()
}

func (s *workbenchNativeStream) Write(data []byte) (workbenchNativeDelta, error) {
	var text, summary strings.Builder
	s.pending = append(s.pending, data...)
	for {
		end := bytes.IndexByte(s.pending, '\n')
		if end < 0 {
			break
		}
		line := bytes.TrimSuffix(s.pending[:end], []byte{'\r'})
		s.pending = s.pending[end+1:]
		part, err := s.readLine(line)
		if err != nil {
			return workbenchNativeDelta{Text: text.String(), ReasoningSummary: summary.String()}, err
		}
		_, _ = text.WriteString(part.Text)
		_, _ = summary.WriteString(part.ReasoningSummary)
	}
	return workbenchNativeDelta{Text: text.String(), ReasoningSummary: summary.String()}, nil
}

func (s *workbenchNativeStream) readLine(line []byte) (workbenchNativeDelta, error) {
	if len(line) == 0 {
		if len(s.frame) == 0 {
			return workbenchNativeDelta{}, nil
		}
		delta, err := s.readFrame(s.frame)
		s.frame = s.frame[:0]
		if err == nil {
			_, _ = s.text.WriteString(delta.Text)
			_, _ = s.summary.WriteString(delta.ReasoningSummary)
		}
		return delta, err
	}
	if data, ok := bytes.CutPrefix(line, []byte("data:")); ok {
		if len(s.frame) > 0 {
			s.frame = append(s.frame, '\n')
		}
		s.frame = append(s.frame, bytes.TrimPrefix(data, []byte{' '})...)
	}
	return workbenchNativeDelta{}, nil
}

func (s *workbenchNativeStream) readFrame(payload []byte) (workbenchNativeDelta, error) {
	if bytes.Equal(bytes.TrimSpace(payload), []byte("[DONE]")) {
		return workbenchNativeDelta{}, nil
	}
	switch s.protocol {
	case workbenchProtocolGemini:
		return s.readGeminiFrame(payload)
	case workbenchProtocolResponses:
		return s.readResponsesFrame(payload)
	case workbenchProtocolAnthropic:
		return s.anthropic.readFrame(payload)
	case workbenchProtocolChatCompletions:
		return s.readChatFrame(payload)
	default:
		return workbenchNativeDelta{}, ErrOfficeInvalid
	}
}

func (s *workbenchNativeStream) Result() (workbenchNativeReply, error) {
	if len(s.pending) > 0 {
		if _, err := s.readLine(bytes.TrimSuffix(s.pending, []byte{'\r'})); err != nil {
			return workbenchNativeReply{}, err
		}
		s.pending = nil
	}
	if _, err := s.readLine(nil); err != nil {
		return workbenchNativeReply{}, err
	}
	if s.protocol == workbenchProtocolAnthropic {
		return s.anthropic.result(s.text.String())
	}
	if s.finish == "" {
		if !s.sawEvent {
			return workbenchNativeReply{}, workbenchNativeParseError("no model stream events")
		}
		return workbenchNativeReply{}, workbenchNativeParseError(workbenchMissingFinish)
	}
	reply := workbenchNativeReply{Text: s.text.String(), ReasoningSummary: s.summary.String(), FinishReason: s.finish}
	var calls []workbenchProtocolCall
	var err error
	switch s.protocol {
	case workbenchProtocolGemini:
		if len(s.geminiParts) > 0 {
			reply.Native = workbenchNativeJSON(workbenchGeminiContent{Role: "model", Parts: s.geminiParts})
		}
		calls, err = workbenchGeminiCalls(s.geminiParts)
	case workbenchProtocolResponses:
		items := s.finalOutput
		if len(items) == 0 {
			items = workbenchOutputsInOrder(s.responseItems)
		}
		if len(items) > 0 {
			reply.Native = workbenchNativeJSON(items)
			var evidence workbenchNativeDelta
			evidence, err = workbenchResponsesText(items)
			if evidence.Text != "" {
				reply.Text = evidence.Text
			}
			if evidence.ReasoningSummary != "" {
				reply.ReasoningSummary = evidence.ReasoningSummary
			}
		}
		if err == nil {
			calls, err = workbenchResponsesCalls(items)
		}
	case workbenchProtocolChatCompletions:
		message := workbenchChatMessage{Role: "assistant", Reasoning: s.chatReasoning.String(), ToolCalls: workbenchOutputsInOrder(s.chatToolCalls)}
		if reply.Text != "" {
			message.Content = &reply.Text
		}
		reply.Native = workbenchNativeJSON(message)
		calls = workbenchChatCalls(message.ToolCalls)
	}
	if err != nil {
		return reply, err
	}
	if s.finish != "completed" && s.finish != "STOP" && s.finish != "stop" && s.finish != "tool_calls" {
		// A token-limited answer can still be shown. An interrupted function
		// call can have partial arguments and must never be dispatched.
		if (s.finish == "MAX_TOKENS" || s.finish == "max_output_tokens" || s.finish == "length") && len(calls) == 0 && reply.Text != "" {
			return reply, nil
		}
		return reply, &workbenchNativeUpstreamError{Reason: s.finish}
	}
	return finishWorkbenchNativeReply(reply, calls)
}

func finishWorkbenchNativeReply(reply workbenchNativeReply, calls []workbenchProtocolCall) (workbenchNativeReply, error) {
	seen := make(map[string]bool, len(calls))
	for _, call := range calls {
		if call.ID == "" || call.Name == "" || !workbenchJSONObject(call.Arguments) || seen[call.ID] {
			return reply, workbenchNativeParseError(workbenchInvalidCall)
		}
		seen[call.ID] = true
	}
	for _, call := range calls {
		reply.Calls = append(reply.Calls, call.workbenchNativeCall)
	}
	if reply.Text == "" && len(reply.Calls) == 0 {
		return reply, &workbenchNativeUpstreamError{Reason: "empty_output"}
	}
	return reply, nil
}

func workbenchOutputsInOrder[V any](byIndex map[int]V) []V {
	indices := make([]int, 0, len(byIndex))
	for index := range byIndex {
		indices = append(indices, index)
	}
	slices.Sort(indices)
	values := make([]V, 0, len(indices))
	for _, index := range indices {
		values = append(values, byIndex[index])
	}
	return values
}

type workbenchGeminiFunction struct {
	ID        string          `json:"id"`
	Name      string          `json:"name"`
	Arguments json.RawMessage `json:"args"`
}

type workbenchGeminiPart struct {
	Text     string                   `json:"text"`
	Thought  bool                     `json:"thought"`
	Function *workbenchGeminiFunction `json:"functionCall"`
}

func (s *workbenchNativeStream) readGeminiFrame(payload []byte) (workbenchNativeDelta, error) {
	var frame struct {
		Candidates []struct {
			Index        int                    `json:"index"`
			Content      workbenchGeminiContent `json:"content"`
			FinishReason string                 `json:"finishReason"`
		} `json:"candidates"`
		PromptFeedback struct {
			BlockReason string `json:"blockReason"`
		} `json:"promptFeedback"`
		Error json.RawMessage `json:"error"`
	}
	var delta workbenchNativeDelta
	if json.Unmarshal(payload, &frame) != nil {
		return delta, workbenchNativeParseError("invalid Gemini SSE data")
	}
	s.sawEvent = s.sawEvent || len(frame.Candidates) > 0
	if len(frame.Error) > 0 && string(frame.Error) != "null" {
		s.finish = "upstream_error"
	}
	if frame.PromptFeedback.BlockReason != "" && frame.PromptFeedback.BlockReason != "BLOCK_REASON_UNSPECIFIED" {
		s.finish = frame.PromptFeedback.BlockReason
	}
	for _, candidate := range frame.Candidates {
		if candidate.Index != 0 {
			continue
		}
		for _, raw := range candidate.Content.Parts {
			var part workbenchGeminiPart
			if json.Unmarshal(raw, &part) != nil {
				return delta, workbenchNativeParseError("invalid Gemini content part")
			}
			s.geminiParts = append(s.geminiParts, raw)
			if part.Thought {
				delta.ReasoningSummary += part.Text
			} else {
				delta.Text += part.Text
			}
		}
		if candidate.FinishReason != "" && candidate.FinishReason != "FINISH_REASON_UNSPECIFIED" {
			s.finish = candidate.FinishReason
		}
	}
	return delta, nil
}

func workbenchGeminiCalls(parts []json.RawMessage) ([]workbenchProtocolCall, error) {
	var calls []workbenchProtocolCall
	for i, raw := range parts {
		var part workbenchGeminiPart
		if json.Unmarshal(raw, &part) != nil {
			return nil, workbenchNativeParseError("invalid Gemini content part")
		}
		if part.Function == nil {
			continue
		}
		id := part.Function.ID
		if id == "" {
			// Older GenerateContent models identify the result by name and
			// order. Give those calls a stable browser-facing ID only.
			id = fmt.Sprintf("gemini-part-%d", i)
		}
		arguments := part.Function.Arguments
		if len(arguments) == 0 {
			arguments = json.RawMessage(`{}`)
		}
		calls = append(calls, workbenchProtocolCall{workbenchNativeCall: workbenchNativeCall{ID: id, Name: part.Function.Name, Arguments: arguments}, nativeID: part.Function.ID})
	}
	return calls, nil
}

type workbenchResponsesItem struct {
	Type      string `json:"type"`
	CallID    string `json:"call_id"`
	Name      string `json:"name"`
	Arguments string `json:"arguments"`
	Content   []struct {
		Type    string `json:"type"`
		Text    string `json:"text"`
		Refusal string `json:"refusal"`
	} `json:"content"`
	Summary []struct {
		Type string `json:"type"`
		Text string `json:"text"`
	} `json:"summary"`
}

func (s *workbenchNativeStream) readResponsesFrame(payload []byte) (workbenchNativeDelta, error) {
	var frame struct {
		Type        string          `json:"type"`
		Delta       string          `json:"delta"`
		OutputIndex int             `json:"output_index"`
		Item        json.RawMessage `json:"item"`
		Response    struct {
			Status            string            `json:"status"`
			Output            []json.RawMessage `json:"output"`
			IncompleteDetails struct {
				Reason string `json:"reason"`
			} `json:"incomplete_details"`
		} `json:"response"`
	}
	var delta workbenchNativeDelta
	if json.Unmarshal(payload, &frame) != nil {
		return delta, workbenchNativeParseError("invalid Responses SSE data")
	}
	s.sawEvent = s.sawEvent || strings.HasPrefix(frame.Type, "response.")
	switch frame.Type {
	case "response.output_text.delta", "response.refusal.delta":
		delta.Text = frame.Delta
	case "response.reasoning_summary_text.delta":
		delta.ReasoningSummary = frame.Delta
	case "response.output_item.done":
		if len(frame.Item) > 0 && string(frame.Item) != "null" {
			if s.responseItems == nil {
				s.responseItems = make(map[int]json.RawMessage)
			}
			s.responseItems[frame.OutputIndex] = frame.Item
		}
	case "response.completed", "response.done", "response.incomplete", "response.failed", "response.cancelled", "response.canceled":
		s.finalOutput = frame.Response.Output
		s.finish = frame.Response.Status
		if s.finish == "" {
			s.finish = strings.TrimPrefix(frame.Type, "response.")
			if s.finish == "done" {
				s.finish = "completed"
			}
		}
		if s.finish == "incomplete" && frame.Response.IncompleteDetails.Reason != "" {
			s.finish = frame.Response.IncompleteDetails.Reason
		}
	case "error":
		s.finish = "upstream_error"
	}
	return delta, nil
}

func workbenchResponsesCalls(items []json.RawMessage) ([]workbenchProtocolCall, error) {
	var calls []workbenchProtocolCall
	for _, raw := range items {
		var item workbenchResponsesItem
		if json.Unmarshal(raw, &item) != nil {
			return nil, workbenchNativeParseError("invalid Responses output item")
		}
		if item.Type == "function_call" {
			arguments := json.RawMessage(item.Arguments)
			if len(arguments) == 0 {
				arguments = json.RawMessage(`{}`)
			}
			calls = append(calls, workbenchProtocolCall{workbenchNativeCall: workbenchNativeCall{ID: item.CallID, Name: item.Name, Arguments: arguments}})
		}
	}
	return calls, nil
}

func workbenchResponsesText(items []json.RawMessage) (workbenchNativeDelta, error) {
	var content workbenchNativeDelta
	for _, raw := range items {
		var item workbenchResponsesItem
		if json.Unmarshal(raw, &item) != nil {
			return content, workbenchNativeParseError("invalid Responses output item")
		}
		if item.Type == "message" {
			for _, part := range item.Content {
				switch part.Type {
				case "output_text":
					content.Text += part.Text
				case "refusal":
					content.Text += part.Refusal
				}
			}
		}
		if item.Type == "reasoning" {
			for _, part := range item.Summary {
				if part.Type == "summary_text" {
					content.ReasoningSummary += part.Text
				}
			}
		}
	}
	return content, nil
}

type workbenchChatMessage struct {
	Role      string                   `json:"role"`
	Content   *string                  `json:"content"`
	Reasoning string                   `json:"reasoning_content,omitempty"`
	ToolCalls []*workbenchChatToolCall `json:"tool_calls,omitempty"`
}

type workbenchChatToolCall struct {
	ID       string `json:"id"`
	Type     string `json:"type"`
	Function struct {
		Name      string `json:"name"`
		Arguments string `json:"arguments"`
	} `json:"function"`
}

func (s *workbenchNativeStream) readChatFrame(payload []byte) (workbenchNativeDelta, error) {
	var frame struct {
		Choices []struct {
			Index        int    `json:"index"`
			FinishReason string `json:"finish_reason"`
			Delta        struct {
				Content   string `json:"content"`
				Refusal   string `json:"refusal"`
				Reasoning string `json:"reasoning_content"`
				ToolCalls []struct {
					workbenchChatToolCall
					Index int `json:"index"`
				} `json:"tool_calls"`
			} `json:"delta"`
		} `json:"choices"`
		Error json.RawMessage `json:"error"`
		Usage json.RawMessage `json:"usage"`
	}
	var delta workbenchNativeDelta
	if json.Unmarshal(payload, &frame) != nil {
		return delta, workbenchNativeParseError("invalid Chat Completions SSE data")
	}
	s.sawEvent = s.sawEvent || len(frame.Choices) > 0 || workbenchJSONObject(frame.Usage)
	if len(frame.Error) > 0 && string(frame.Error) != "null" {
		s.finish = "upstream_error"
	}
	for _, choice := range frame.Choices {
		if choice.Index != 0 {
			continue
		}
		delta.Text += choice.Delta.Content + choice.Delta.Refusal
		_, _ = s.chatReasoning.WriteString(choice.Delta.Reasoning)
		if choice.FinishReason != "" {
			s.finish = choice.FinishReason
		}
		for _, part := range choice.Delta.ToolCalls {
			if s.chatToolCalls == nil {
				s.chatToolCalls = make(map[int]*workbenchChatToolCall)
			}
			call := s.chatToolCalls[part.Index]
			if call == nil {
				call = &workbenchChatToolCall{Type: "function"}
				s.chatToolCalls[part.Index] = call
			}
			if part.ID != "" {
				call.ID = part.ID
			}
			if part.Type != "" {
				call.Type = part.Type
			}
			call.Function.Name += part.Function.Name
			call.Function.Arguments += part.Function.Arguments
		}
	}
	return delta, nil
}

func workbenchChatCalls(native []*workbenchChatToolCall) []workbenchProtocolCall {
	calls := make([]workbenchProtocolCall, 0, len(native))
	for _, call := range native {
		if call != nil && call.Type == "function" {
			arguments := json.RawMessage(call.Function.Arguments)
			if len(arguments) == 0 {
				arguments = json.RawMessage(`{}`)
			}
			calls = append(calls, workbenchProtocolCall{workbenchNativeCall: workbenchNativeCall{ID: call.ID, Name: call.Function.Name, Arguments: arguments}})
		}
	}
	return calls
}
