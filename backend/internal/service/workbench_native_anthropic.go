package service

import (
	"bytes"
	"encoding/json"
	"strings"

	"github.com/Wei-Shaw/sub2api/internal/pkg/apicompat"
)

func buildWorkbenchAnthropicRequest(in workbenchNativeRequest) (map[string]any, error) {
	messages := make([]apicompat.AnthropicMessage, 0, len(in.History)+2*len(in.Transcript))
	err := visitWorkbenchHistory(in, func(message workbenchNativeMessage) error {
		parts := make([]apicompat.AnthropicContentBlock, 0, 1+len(message.Images))
		if message.Text != "" {
			parts = append(parts, apicompat.AnthropicContentBlock{Type: "text", Text: message.Text})
		}
		for _, image := range message.Images {
			parts = append(parts, apicompat.AnthropicContentBlock{Type: "image", Source: &apicompat.AnthropicImageSource{
				Type: "base64", MediaType: image.MIME, Data: image.Data,
			}})
		}
		if len(parts) > 0 {
			messages = append(messages, apicompat.AnthropicMessage{Role: message.Role, Content: workbenchNativeJSON(parts)})
		}
		return nil
	}, func(exchange workbenchNativeExchange) error {
		var blocks []json.RawMessage
		if json.Unmarshal(exchange.Native, &blocks) != nil || len(blocks) == 0 {
			return ErrOfficeInvalid
		}
		calls, err := workbenchAnthropicCalls(blocks)
		if err != nil {
			return err
		}
		results, err := orderWorkbenchToolResults(calls, exchange.Results)
		if err != nil {
			return err
		}
		messages = append(messages, apicompat.AnthropicMessage{Role: "assistant", Content: exchange.Native})
		if len(results) > 0 {
			parts := make([]apicompat.AnthropicContentBlock, 0, len(results))
			for _, result := range results {
				parts = append(parts, apicompat.AnthropicContentBlock{
					Type: "tool_result", ToolUseID: result.ID, Content: workbenchNativeJSON(string(result.Output)),
					IsError: workbenchAnthropicToolFailed(result.Output),
				})
			}
			messages = append(messages, apicompat.AnthropicMessage{Role: "user", Content: workbenchNativeJSON(parts)})
		}
		return nil
	})
	if err != nil || len(messages) == 0 || in.MaxOutputTokens <= 0 {
		return nil, ErrOfficeInvalid
	}
	body := map[string]any{"model": in.Model, "messages": messages, "stream": true, "max_tokens": in.MaxOutputTokens}
	if in.System != "" {
		body["system"] = in.System
	}
	if len(in.Tools) > 0 {
		tools := make([]apicompat.AnthropicTool, 0, len(in.Tools))
		for _, tool := range in.Tools {
			tools = append(tools, apicompat.AnthropicTool{Name: tool.Name, Description: tool.Description, InputSchema: tool.Schema})
		}
		body["tools"] = tools
	}
	configureWorkbenchAnthropicThinking(body, in)
	return body, nil
}

func configureWorkbenchAnthropicThinking(body map[string]any, in workbenchNativeRequest) {
	model := strings.ToLower(strings.TrimSpace(in.ProfileModel))
	if model == "" {
		model = strings.ToLower(strings.TrimSpace(in.Model))
	}
	// Submission already validates the selected level against the public model
	// profile. Keep newly published Profiles usable without a second model list.
	switch in.ReasoningEffort {
	case "low", "medium", "high", "xhigh", "max":
	default:
		return
	}
	body["output_config"] = apicompat.AnthropicOutputConfig{Effort: in.ReasoningEffort}
	// Opus 4.5 accepts effort but only supports legacy extended thinking.
	if !strings.HasPrefix(model, "claude-opus-4-5") {
		body["thinking"] = map[string]string{"type": "adaptive", "display": "omitted"}
	}
}

func workbenchAnthropicToolFailed(output json.RawMessage) bool {
	var result struct {
		Error  json.RawMessage `json:"error"`
		Status string          `json:"status"`
	}
	if json.Unmarshal(output, &result) != nil {
		return false
	}
	err := string(bytes.TrimSpace(result.Error))
	return result.Status == "failed" || result.Status == "cancelled" || err != "" && err != "null" && err != `""` && err != "false"
}

type workbenchAnthropicStream struct {
	started  bool
	stopped  bool
	upstream string
	finish   string
	blocks   []json.RawMessage
	block    *workbenchAnthropicBlock
}

type workbenchAnthropicBlock struct {
	raw       map[string]json.RawMessage
	typ       string
	text      strings.Builder
	thinking  strings.Builder
	signature strings.Builder
	input     strings.Builder

	citations []json.RawMessage
}

type workbenchAnthropicFrame struct {
	Type    string                       `json:"type"`
	Index   *int                         `json:"index"`
	Message *apicompat.AnthropicResponse `json:"message"`
	Block   json.RawMessage              `json:"content_block"`
	Delta   struct {
		apicompat.AnthropicDelta
		Citation json.RawMessage `json:"citation"`
	} `json:"delta"`
}

func (s *workbenchAnthropicStream) readFrame(payload []byte) (workbenchNativeDelta, error) {
	var frame workbenchAnthropicFrame
	if json.Unmarshal(payload, &frame) != nil || frame.Type == "" {
		return workbenchNativeDelta{}, workbenchNativeParseError("invalid Anthropic SSE data")
	}
	switch frame.Type {
	case "message_start", "content_block_start", "content_block_delta", "content_block_stop", "message_delta", "message_stop", "error":
	default:
		// Anthropic may add metadata events independently of content and framing.
		return workbenchNativeDelta{}, nil
	}
	if s.stopped || s.upstream != "" {
		return workbenchNativeDelta{}, workbenchNativeParseError("unexpected Anthropic event after stream finish")
	}
	if frame.Type == "error" {
		s.upstream = "upstream_error"
		return workbenchNativeDelta{}, nil
	}
	switch frame.Type {
	case "message_start":
		if s.started || frame.Message == nil || frame.Message.Role != "assistant" || len(frame.Message.Content) != 0 {
			return workbenchNativeDelta{}, workbenchNativeParseError("invalid Anthropic message start")
		}
		s.started = true
	case "content_block_start":
		if !s.blockEventValid(frame.Index) || s.block != nil {
			return workbenchNativeDelta{}, workbenchNativeParseError("invalid Anthropic block start")
		}
		block, initial, err := newWorkbenchAnthropicBlock(frame.Block)
		if err != nil {
			return workbenchNativeDelta{}, err
		}
		s.block = block
		return initial, nil
	case "content_block_delta":
		if !s.blockEventValid(frame.Index) || s.block == nil {
			return workbenchNativeDelta{}, workbenchNativeParseError("invalid Anthropic block delta")
		}
		return s.block.apply(frame.Delta.AnthropicDelta, frame.Delta.Citation)
	case "content_block_stop":
		if !s.blockEventValid(frame.Index) || s.block == nil {
			return workbenchNativeDelta{}, workbenchNativeParseError("invalid Anthropic block stop")
		}
		block, err := s.block.complete()
		if err != nil {
			return workbenchNativeDelta{}, err
		}
		s.blocks = append(s.blocks, block)
		s.block = nil
	case "message_delta":
		if !s.started || s.block != nil || (s.finish != "" && frame.Delta.StopReason != "" && s.finish != frame.Delta.StopReason) {
			return workbenchNativeDelta{}, workbenchNativeParseError("invalid Anthropic message delta")
		}
		if frame.Delta.StopReason != "" {
			s.finish = frame.Delta.StopReason
		}
	case "message_stop":
		if !s.started || s.finish == "" || s.block != nil {
			return workbenchNativeDelta{}, workbenchNativeParseError("invalid Anthropic message stop")
		}
		s.stopped = true
	}
	return workbenchNativeDelta{}, nil
}

func (s *workbenchAnthropicStream) blockEventValid(index *int) bool {
	return s.started && s.finish == "" && index != nil && *index == len(s.blocks)
}

func newWorkbenchAnthropicBlock(raw json.RawMessage) (*workbenchAnthropicBlock, workbenchNativeDelta, error) {
	block := &workbenchAnthropicBlock{}
	if json.Unmarshal(raw, &block.raw) != nil || json.Unmarshal(block.raw["type"], &block.typ) != nil || block.typ == "" {
		return nil, workbenchNativeDelta{}, workbenchNativeParseError("invalid Anthropic content block")
	}
	var initial apicompat.AnthropicContentBlock
	if json.Unmarshal(raw, &initial) != nil {
		return nil, workbenchNativeDelta{}, workbenchNativeParseError("invalid Anthropic content block")
	}
	_, _ = block.text.WriteString(initial.Text)
	_, _ = block.thinking.WriteString(initial.Thinking)
	_, _ = block.signature.WriteString(initial.Signature)
	if data := block.raw["citations"]; len(data) > 0 && json.Unmarshal(data, &block.citations) != nil {
		return nil, workbenchNativeDelta{}, workbenchNativeParseError("invalid Anthropic citations")
	}
	if block.typ == "text" {
		return block, workbenchNativeDelta{Text: initial.Text}, nil
	}
	return block, workbenchNativeDelta{}, nil
}

func (b *workbenchAnthropicBlock) apply(delta apicompat.AnthropicDelta, citation json.RawMessage) (workbenchNativeDelta, error) {
	switch {
	case b.typ == "text" && delta.Type == "text_delta":
		_, _ = b.text.WriteString(delta.Text)
		return workbenchNativeDelta{Text: delta.Text}, nil
	case b.typ == "text" && delta.Type == "citations_delta" && workbenchJSONObject(citation):
		b.citations = append(b.citations, citation)
	case b.typ == "thinking" && delta.Type == "thinking_delta":
		_, _ = b.thinking.WriteString(delta.Thinking)
	case b.typ == "thinking" && delta.Type == "signature_delta":
		_, _ = b.signature.WriteString(delta.Signature)
	case b.typ == "tool_use" && delta.Type == "input_json_delta":
		_, _ = b.input.WriteString(delta.PartialJSON)
	default:
		return workbenchNativeDelta{}, workbenchNativeParseError("unsupported Anthropic content delta")
	}
	return workbenchNativeDelta{}, nil
}

func (b *workbenchAnthropicBlock) complete() (json.RawMessage, error) {
	switch b.typ {
	case "text":
		b.raw["text"] = workbenchNativeJSON(b.text.String())
		if len(b.citations) > 0 {
			b.raw["citations"] = workbenchNativeJSON(b.citations)
		}
	case "thinking":
		b.raw["thinking"] = workbenchNativeJSON(b.thinking.String())
		b.raw["signature"] = workbenchNativeJSON(b.signature.String())
	case "tool_use":
		if b.input.Len() > 0 {
			input := json.RawMessage(b.input.String())
			if !workbenchJSONObject(input) {
				// A token limit can end a tool call mid-JSON. Read through the
				// stop event so that the gateway can settle the usage.
				input = json.RawMessage("null")
			}
			b.raw["input"] = input
		}
	}
	return json.Marshal(b.raw)
}

func workbenchAnthropicCalls(blocks []json.RawMessage) ([]workbenchProtocolCall, error) {
	var calls []workbenchProtocolCall
	for _, raw := range blocks {
		var block apicompat.AnthropicContentBlock
		if json.Unmarshal(raw, &block) != nil || block.Type == "" {
			return nil, workbenchNativeParseError("invalid Anthropic content block")
		}
		if block.Type == "thinking" && block.Signature == "" || block.Type == "redacted_thinking" && block.Data == "" {
			return nil, workbenchNativeParseError("incomplete Anthropic thinking block")
		}
		if block.Type == "tool_use" {
			calls = append(calls, workbenchProtocolCall{workbenchNativeCall: workbenchNativeCall{ID: block.ID, Name: block.Name, Arguments: block.Input}})
		}
	}
	return calls, nil
}

func (s *workbenchAnthropicStream) result(text string) (workbenchNativeReply, error) {
	if s.upstream != "" {
		return workbenchNativeReply{}, &workbenchNativeUpstreamError{Reason: s.upstream}
	}
	if !s.stopped {
		return workbenchNativeReply{}, workbenchNativeParseError("missing Anthropic message stop")
	}
	reply := workbenchNativeReply{Text: text, Native: workbenchNativeJSON(s.blocks), FinishReason: s.finish}
	calls, err := workbenchAnthropicCalls(s.blocks)
	if err != nil {
		return reply, err
	}
	switch s.finish {
	case "tool_use":
		if len(calls) == 0 {
			return reply, workbenchNativeParseError("Anthropic tool stop without a tool call")
		}
	case "end_turn", "stop_sequence", "max_tokens":
		if len(calls) != 0 {
			return reply, &workbenchNativeUpstreamError{Reason: s.finish}
		}
	default:
		return reply, &workbenchNativeUpstreamError{Reason: s.finish}
	}
	return finishWorkbenchNativeReply(reply, calls)
}
