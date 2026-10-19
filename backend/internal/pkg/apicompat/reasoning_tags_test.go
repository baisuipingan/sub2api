package apicompat

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

func collectTextDeltas(chunks []ChatCompletionsChunk) (reasoning, content string) {
	var reasoningBuilder, contentBuilder strings.Builder
	for _, chunk := range chunks {
		if len(chunk.Choices) == 0 {
			continue
		}
		delta := chunk.Choices[0].Delta
		if delta.ReasoningContent != nil {
			_, _ = reasoningBuilder.WriteString(*delta.ReasoningContent)
		}
		if delta.Content != nil {
			_, _ = contentBuilder.WriteString(*delta.Content)
		}
	}
	return reasoningBuilder.String(), contentBuilder.String()
}

func TestResponsesToChatCompletions_MovesLeadingThinkingTagToReasoning(t *testing.T) {
	resp := &ResponsesResponse{
		ID:     "resp_tagged",
		Status: "completed",
		Output: []ResponsesOutput{{
			Type:    "message",
			Role:    "assistant",
			Content: []ResponsesContentPart{{Type: "output_text", Text: "<thinking>inspect first</thinking>\nVisible answer."}},
		}},
	}

	chat := ResponsesToChatCompletions(resp, "deepseek-flash")
	require.Len(t, chat.Choices, 1)
	require.Equal(t, "inspect first", chat.Choices[0].Message.ReasoningContent)
	var content string
	require.NoError(t, json.Unmarshal(chat.Choices[0].Message.Content, &content))
	require.Equal(t, "Visible answer.", content)
}

func TestResponsesToChatCompletions_HidesUnterminatedLeadingThinkingTag(t *testing.T) {
	resp := &ResponsesResponse{
		Status: "completed",
		Output: []ResponsesOutput{{
			Type:    "message",
			Role:    "assistant",
			Content: []ResponsesContentPart{{Type: "output_text", Text: "<think>unfinished internal plan"}},
		}},
	}

	chat := ResponsesToChatCompletions(resp, "deepseek-flash")
	require.Len(t, chat.Choices, 1)
	require.Equal(t, "unfinished internal plan", chat.Choices[0].Message.ReasoningContent)
	require.Nil(t, chat.Choices[0].Message.Content)
}

func TestResponsesEventToChatChunks_SplitsStreamedLeadingThinkingTag(t *testing.T) {
	state := NewResponsesEventToChatState()
	var chunks []ChatCompletionsChunk
	for _, delta := range []string{"  <thi", "nking>inspect", " first</thin", "king>\nVisible", " answer."} {
		chunks = append(chunks, ResponsesEventToChatChunks(&ResponsesStreamEvent{
			Type:  "response.output_text.delta",
			Delta: delta,
		}, state)...)
	}
	chunks = append(chunks, ResponsesEventToChatChunks(&ResponsesStreamEvent{
		Type: "response.completed",
		Response: &ResponsesResponse{
			ID:     "resp_stream_tagged",
			Status: "completed",
		},
	}, state)...)

	reasoning, content := collectTextDeltas(chunks)
	require.Equal(t, "inspect first", reasoning)
	require.Equal(t, "Visible answer.", content)
}

func TestResponsesEventToChatChunks_UnterminatedThinkingTagStaysHidden(t *testing.T) {
	state := NewResponsesEventToChatState()
	chunks := ResponsesEventToChatChunks(&ResponsesStreamEvent{
		Type:  "response.output_text.delta",
		Delta: "<think>internal plan without a close",
	}, state)
	chunks = append(chunks, ResponsesEventToChatChunks(&ResponsesStreamEvent{
		Type:     "response.completed",
		Response: &ResponsesResponse{Status: "completed"},
	}, state)...)

	reasoning, content := collectTextDeltas(chunks)
	require.Equal(t, "internal plan without a close", reasoning)
	require.Empty(t, content)
}

func TestResponsesEventToChatChunks_LeavesLiteralTagInsideAnswer(t *testing.T) {
	state := NewResponsesEventToChatState()
	chunks := ResponsesEventToChatChunks(&ResponsesStreamEvent{
		Type:  "response.output_text.delta",
		Delta: "Use the literal string <thinking> in your template.",
	}, state)

	reasoning, content := collectTextDeltas(chunks)
	require.Empty(t, reasoning)
	require.Equal(t, "Use the literal string <thinking> in your template.", content)
}

func TestChatCompletionsToResponses_MovesTaggedAssistantHistoryToReasoningItem(t *testing.T) {
	req := &ChatCompletionsRequest{
		Model: "deepseek-flash",
		Messages: []ChatMessage{
			{Role: "user", Content: json.RawMessage(`"Continue"`)},
			{Role: "assistant", Content: json.RawMessage(`"<thinking>old leaked plan</thinking>\nVisible answer"`)},
		},
	}

	resp, err := ChatCompletionsToResponses(req)
	require.NoError(t, err)

	var items []ResponsesInputItem
	require.NoError(t, json.Unmarshal(resp.Input, &items))
	require.Len(t, items, 2)

	var parts []ResponsesContentPart
	require.NoError(t, json.Unmarshal(items[1].Content, &parts))
	require.Len(t, parts, 1)
	require.Equal(t, "Visible answer", parts[0].Text)
}
