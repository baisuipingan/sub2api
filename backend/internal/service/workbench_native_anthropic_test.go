package service

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
	"github.com/tidwall/gjson"
)

func TestWorkbenchNativeAnthropicPreservesOrderedBlocksAcrossUserTurns(t *testing.T) {
	wire := workbenchAnthropicTestSSE(
		`{"type":"message_start","message":{"type":"message","role":"assistant","content":[],"stop_reason":null}}`,
		`{"type":"content_block_start","index":0,"content_block":{"type":"thinking","thinking":"","signature":""}}`,
		`{"type":"content_block_delta","index":0,"delta":{"type":"thinking_delta","thinking":"private thought"}}`,
		`{"type":"content_block_delta","index":0,"delta":{"type":"signature_delta","signature":"opaque-signature"}}`,
		`{"type":"content_block_stop","index":0}`,
		`{"type":"content_block_start","index":1,"content_block":{"type":"redacted_thinking","data":"encrypted"}}`,
		`{"type":"content_block_stop","index":1}`,
		`{"type":"content_block_start","index":2,"content_block":{"type":"text","text":"I will "}}`,
		`{"type":"content_block_delta","index":2,"delta":{"type":"text_delta","text":"check."}}`,
		`{"type":"content_block_stop","index":2}`,
		`{"type":"content_block_start","index":3,"content_block":{"type":"tool_use","id":"toolu_read","name":"read_docx","input":{}}}`,
		`{"type":"content_block_delta","index":3,"delta":{"type":"input_json_delta","partial_json":"{\"vers"}}`,
		`{"type":"ping"}`,
		`{"type":"content_block_delta","index":3,"delta":{"type":"input_json_delta","partial_json":"ion_id\":\"file-v1\"}"}}`,
		`{"type":"content_block_stop","index":3}`,
		`{"type":"content_block_start","index":4,"content_block":{"type":"tool_use","id":"toolu_search","name":"web_search","input":{"query":"catalog"}}}`,
		`{"type":"content_block_stop","index":4}`,
		`{"type":"message_delta","delta":{"stop_reason":"tool_use","stop_sequence":null}}`,
		`{"type":"message_stop"}`,
	)
	stream := workbenchNativeStream{protocol: workbenchProtocolAnthropic}
	var text strings.Builder
	for index := 0; index < len(wire); index += 17 {
		delta, err := stream.Write([]byte(wire[index:min(index+17, len(wire))]))
		require.NoError(t, err)
		require.Empty(t, delta.ReasoningSummary)
		_, _ = text.WriteString(delta.Text)
	}
	reply, err := stream.Result()
	require.NoError(t, err)
	require.Equal(t, "I will check.", text.String())
	require.Equal(t, text.String(), reply.Text)
	require.Empty(t, reply.ReasoningSummary)
	require.Len(t, reply.Calls, 2)
	require.Equal(t, "toolu_read", reply.Calls[0].ID)
	require.JSONEq(t, `{"version_id":"file-v1"}`, string(reply.Calls[0].Arguments))
	require.Equal(t, "toolu_search", reply.Calls[1].ID)
	request := workbenchNativeRequest{
		Protocol: workbenchProtocolAnthropic, Model: "claude-office", ProfileModel: "claude-sonnet-4-6",
		System: "Verify the file.", ReasoningEffort: "low", MaxOutputTokens: 2048,
		Tools:   []workbenchNativeTool{{Name: "read_docx", Description: "Inspect Word", Schema: json.RawMessage(`{"type":"object"}`)}},
		History: []workbenchNativeMessage{{Role: "user", Text: "Check this", Images: []workbenchNativeImage{{MIME: "image/webp", Data: "dGVzdA=="}}}},
		Transcript: []workbenchNativeExchange{{Native: reply.Native, Results: []workbenchNativeToolResult{
			{ID: "toolu_search", Name: "web_search", Output: json.RawMessage(`{"error":"TIMEOUT"}`)},
			{ID: "toolu_read", Name: "read_docx", Output: json.RawMessage(`{"title":"sample"}`)},
		}}},
	}
	endpoint, body, err := buildWorkbenchNativeRequest(request)
	require.NoError(t, err)
	require.Equal(t, "/v1/messages", endpoint)
	parsed := gjson.ParseBytes(body)
	require.Equal(t, "Verify the file.", parsed.Get("system").String())
	require.Equal(t, "adaptive", parsed.Get("thinking.type").String())
	require.Equal(t, "omitted", parsed.Get("thinking.display").String())
	require.Equal(t, "low", parsed.Get("output_config.effort").String())
	require.Equal(t, "object", parsed.Get("tools.0.input_schema.type").String())
	require.Equal(t, "dGVzdA==", parsed.Get("messages.0.content.1.source.data").String())
	require.JSONEq(t, string(reply.Native), parsed.Get("messages.1.content").Raw)
	require.Equal(t, "opaque-signature", parsed.Get("messages.1.content.0.signature").String())
	require.Equal(t, "encrypted", parsed.Get("messages.1.content.1.data").String())
	require.Equal(t, "user", parsed.Get("messages.2.role").String())
	require.Equal(t, "toolu_read", parsed.Get("messages.2.content.0.tool_use_id").String())
	require.Equal(t, "toolu_search", parsed.Get("messages.2.content.1.tool_use_id").String())
	require.JSONEq(t, `{"title":"sample"}`, parsed.Get("messages.2.content.0.content").String())
	require.True(t, parsed.Get("messages.2.content.1.is_error").Bool())

	request.History = append(request.History,
		workbenchNativeMessage{Role: "assistant", Native: reply.Native, Results: request.Transcript[0].Results},
		workbenchNativeMessage{Role: "assistant", Native: json.RawMessage(`[{"type":"text","text":"Checked."}]`)},
		workbenchNativeMessage{Role: "user", Text: "Continue"},
	)
	request.Transcript = nil
	_, next, err := buildWorkbenchNativeRequest(request)
	require.NoError(t, err)
	nextMessages := gjson.GetBytes(next, "messages").Array()
	for index, previous := range parsed.Get("messages").Array() {
		require.Equal(t, previous.Raw, nextMessages[index].Raw)
	}
	require.Equal(t, "Checked.", nextMessages[3].Get("content.0.text").String())
	require.Equal(t, "Continue", nextMessages[4].Get("content.0.text").String())
}

func TestWorkbenchNativeAnthropicRejectsUnfinishedAndMalformedToolCalls(t *testing.T) {
	start := `{"type":"message_start","message":{"type":"message","role":"assistant","content":[]}}`
	call := `{"type":"content_block_start","index":0,"content_block":{"type":"tool_use","id":"toolu_1","name":"lookup","input":{}}}`
	stop := `{"type":"content_block_stop","index":0}`
	toolUse := `{"type":"message_delta","delta":{"stop_reason":"tool_use"}}`
	end := `{"type":"message_stop"}`
	for _, tc := range []struct {
		name   string
		frames []string
	}{
		{"missing message stop", []string{start, call, stop, toolUse}},
		{"missing block stop", []string{start, call, toolUse, end}},
		{"stream error", []string{start, call, stop, `{"type":"error","error":{"type":"overloaded_error"}}`}},
		{"ordinary stop with tools", []string{start, call, stop, `{"type":"message_delta","delta":{"stop_reason":"end_turn"}}`, end}},
		{"tool stop without tools", []string{start, toolUse, end}},
		{"partial arguments", []string{start, call, `{"type":"content_block_delta","index":0,"delta":{"type":"input_json_delta","partial_json":"{\"bad\":"}}`, stop, toolUse, end}},
		{"mismatched index", []string{start, call, `{"type":"content_block_stop","index":1}`, toolUse, end}},
		{"duplicate native ids", []string{start, call, stop, strings.Replace(call, `"index":0`, `"index":1`, 1), `{"type":"content_block_stop","index":1}`, toolUse, end}},
		{"missing thinking signature", []string{start, `{"type":"content_block_start","index":0,"content_block":{"type":"thinking","thinking":"private","signature":""}}`, stop, toolUse, end}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			reply, err := parseWorkbenchNativeResponse(workbenchProtocolAnthropic, []byte(workbenchAnthropicTestSSE(tc.frames...)))
			require.Error(t, err)
			require.Empty(t, reply.Calls)
		})
	}
}

func TestWorkbenchNativeResponsesCanPlaceStableDeveloperInstructionsFirst(t *testing.T) {
	request := workbenchNativeRequest{
		Protocol: workbenchProtocolResponses, Model: "gpt-6.2-sol", System: "Fixed instructions",
		StableDeveloperInstructions: true, History: []workbenchNativeMessage{{Role: "user", Text: "Hello"}},
	}
	_, body, err := buildWorkbenchNativeRequest(request)
	require.NoError(t, err)
	require.False(t, gjson.GetBytes(body, "instructions").Exists())
	require.Equal(t, "developer", gjson.GetBytes(body, "input.0.role").String())
	require.Equal(t, "input_text", gjson.GetBytes(body, "input.0.content.0.type").String())
	require.Equal(t, "Fixed instructions", gjson.GetBytes(body, "input.0.content.0.text").String())
	require.Equal(t, "Hello", gjson.GetBytes(body, "input.1.content").String())
	request.StableDeveloperInstructions = false
	_, body, err = buildWorkbenchNativeRequest(request)
	require.NoError(t, err)
	require.Equal(t, "Fixed instructions", gjson.GetBytes(body, "instructions").String())
	require.Equal(t, "Hello", gjson.GetBytes(body, "input.0.content").String())
}

func workbenchAnthropicTestSSE(frames ...string) string {
	return "data: " + strings.Join(frames, "\n\ndata: ") + "\n\n"
}
