package service

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"strings"
	"testing"

	"github.com/Wei-Shaw/sub2api/internal/pkg/ctxkey"
	"github.com/stretchr/testify/require"
	"github.com/tidwall/gjson"
)

func TestWorkbenchNativeGeminiParallelToolsPreserveParts(t *testing.T) {
	stream := &workbenchNativeStream{protocol: workbenchProtocolGemini}
	wire := strings.Join([]string{
		": keep-alive\r\n\r\n",
		"data: {\"candidates\":[{\"content\":{\"role\":\"model\",\"parts\":[{\"text\":\"看资料\",\"thought\":true},{\"text\":\"我先查找。\"}]}}]}\r\n\r\n",
		"data: {\"candidates\":[{\"content\":{\"role\":\"model\",\"parts\":[{\"functionCall\":{\"id\":\"gem-1\",\"name\":\"search\",\"args\":{\"query\":\"第一条\"}},\"thoughtSignature\":\"c2lnMQ==\"},{\"functionCall\":{\"name\":\"search\",\"args\":{\"query\":\"第二条\"}}}]}}]}\n\n",
		"data: {\"candidates\":[{\"content\":{\"parts\":[{\"text\":\"\",\"thoughtSignature\":\"c2lnMg==\"}]},\"finishReason\":\"STOP\"}]}\n\n",
		"data: {\"usageMetadata\":{\"totalTokenCount\":41}}",
	}, "")
	var text, summary string
	for i := 0; i < len(wire); i += 19 {
		delta, err := stream.Write([]byte(wire[i:min(i+19, len(wire))]))
		require.NoError(t, err)
		text += delta.Text
		summary += delta.ReasoningSummary
	}
	reply, err := stream.Result()
	require.NoError(t, err)
	require.Equal(t, "我先查找。", text)
	require.Equal(t, "看资料", summary)
	require.Equal(t, text, reply.Text)
	require.Equal(t, summary, reply.ReasoningSummary)
	require.Len(t, reply.Calls, 2)
	require.Equal(t, "gem-1", reply.Calls[0].ID)
	require.Equal(t, "gemini-part-3", reply.Calls[1].ID)

	endpoint, body, err := buildWorkbenchNativeRequest(workbenchNativeRequest{
		Protocol: workbenchProtocolGemini, Model: "gemini-office", ProfileModel: "gemini-4-hydrogen", System: "Work carefully.", ReasoningEffort: "xhigh",
		History: []workbenchNativeMessage{{Role: "user", Text: "请帮我查资料", Images: []workbenchNativeImage{{MIME: "image/png", Data: "aW1hZ2U="}}}},
		Tools:   []workbenchNativeTool{{Name: "search", Description: "Find sources", Schema: json.RawMessage(`{"type":"object","properties":{"query":{"type":"string"}}}`)}},
		Transcript: []workbenchNativeExchange{{Native: reply.Native, Results: []workbenchNativeToolResult{
			{ID: reply.Calls[1].ID, Name: "search", Output: json.RawMessage(`{"results":[2]}`)},
			{ID: reply.Calls[0].ID, Name: "search", Output: json.RawMessage(`{"results":[1]}`)},
		}}},
	})
	require.NoError(t, err)
	require.Equal(t, "/v1beta/models/gemini-office:streamGenerateContent?alt=sse", endpoint)
	require.Equal(t, "XHIGH", gjson.GetBytes(body, "generationConfig.thinkingConfig.thinkingLevel").String())
	require.Equal(t, "Work carefully.", gjson.GetBytes(body, "systemInstruction.parts.0.text").String())
	require.Equal(t, "aW1hZ2U=", gjson.GetBytes(body, "contents.0.parts.1.inlineData.data").String())
	require.JSONEq(t, string(reply.Native), gjson.GetBytes(body, "contents.1").Raw)
	require.Equal(t, "c2lnMQ==", gjson.GetBytes(body, "contents.1.parts.2.thoughtSignature").String())
	require.Equal(t, "c2lnMg==", gjson.GetBytes(body, "contents.1.parts.4.thoughtSignature").String())
	require.Equal(t, "gem-1", gjson.GetBytes(body, "contents.2.parts.0.functionResponse.id").String())
	require.EqualValues(t, 1, gjson.GetBytes(body, "contents.2.parts.0.functionResponse.response.results.0").Int())
	require.False(t, gjson.GetBytes(body, "contents.2.parts.1.functionResponse.id").Exists())
	require.EqualValues(t, 2, gjson.GetBytes(body, "contents.2.parts.1.functionResponse.response.results.0").Int())
}

func TestWorkbenchNativeGemini25ThinkingLeavesRoomForOutput(t *testing.T) {
	for _, tc := range []struct {
		model        string
		output, want int
		constrain    bool
	}{
		{"gemini-2.5-pro", 4000, 2000, true},
		{"gemini-2.5-pro", 200, gemini25ProMinThinkingBudget, true},
		{"gemini-2.5-flash-lite", 600, 0, true},
		{"gemini-2.5-flash", 32000, 24576, true},
		{"gemini-2.5-flash", 4000, 24576, false},
	} {
		_, body, err := buildWorkbenchNativeRequest(workbenchNativeRequest{
			Protocol: workbenchProtocolGemini, Model: "gemini-office", ProfileModel: tc.model,
			ReasoningEffort: "high", MaxOutputTokens: tc.output, ConstrainGeminiThinkingBudget: tc.constrain,
			History: []workbenchNativeMessage{{Role: "user", Text: "Explain this"}},
		})
		require.NoError(t, err)
		require.EqualValues(t, tc.output, gjson.GetBytes(body, "generationConfig.maxOutputTokens").Int())
		thinking := gjson.GetBytes(body, "generationConfig.thinkingConfig")
		require.EqualValues(t, tc.want, thinking.Get("thinkingBudget").Int(), tc.model)
		require.Equal(t, tc.want > 0, thinking.Get("includeThoughts").Bool(), tc.model)
		require.False(t, thinking.Get("thinkingLevel").Exists(), tc.model)
	}
}

func TestWorkbenchNativeResponsesPreservesOriginalItemsAcrossUserTurns(t *testing.T) {
	wire := `event: response.reasoning_summary_text.delta
data: {"type":"response.reasoning_summary_text.delta","delta":"Needs a source."}

event: response.output_text.delta
data: {"type":"response.output_text.delta","delta":"Checking."}

event: response.output_item.done
data: {"type":"response.output_item.done","output_index":2,"item":{"type":"function_call","id":"fc_1","call_id":"call_1","name":"search","arguments":"{\"query\":\"example\"}","status":"completed"}}

event: response.output_item.done
data: {"type":"response.output_item.done","output_index":1,"item":{"type":"message","id":"msg_1","role":"assistant","phase":"commentary","status":"completed","content":[{"type":"output_text","text":"Checking.","annotations":[]}]}}

event: response.output_item.done
data: {"type":"response.output_item.done","output_index":0,"item":{"type":"reasoning","id":"rs_1","summary":[{"type":"summary_text","text":"Needs a source."}],"encrypted_content":"provider-opaque"}}

event: response.completed
data: {"type":"response.completed","response":{"id":"resp_1","status":"completed","output":[]}}

`
	reply, err := parseWorkbenchNativeResponse(workbenchProtocolResponses, []byte(wire))
	require.NoError(t, err)
	require.Equal(t, "Checking.", reply.Text)
	require.Equal(t, "Needs a source.", reply.ReasoningSummary)
	require.Len(t, reply.Calls, 1)
	require.Equal(t, "call_1", reply.Calls[0].ID)
	require.JSONEq(t, `{"query":"example"}`, string(reply.Calls[0].Arguments))

	request := workbenchNativeRequest{
		Protocol: workbenchProtocolResponses, Model: "gpt-office", System: "Check sources.", ReasoningEffort: "high", MaxOutputTokens: 16384,
		Tools: []workbenchNativeTool{{Name: "search", Description: "Find sources", Schema: json.RawMessage(`{"type":"object"}`)}},
		History: []workbenchNativeMessage{
			{Role: "user", Text: "look it up"},
			{Role: "assistant", Text: "Repeated display content", Native: reply.Native, Results: []workbenchNativeToolResult{{ID: "call_1", Name: "search", Output: json.RawMessage(`{"results":[]}`)}}},
			{Role: "assistant", Text: "I found no references."},
			{Role: "user", Text: "continue"},
		},
	}
	endpoint, body, err := buildWorkbenchNativeRequest(request)
	require.NoError(t, err)
	require.Equal(t, "/v1/responses", endpoint)
	require.Equal(t, "reasoning.encrypted_content", gjson.GetBytes(body, "include.0").String())
	require.False(t, gjson.GetBytes(body, "store").Bool())
	require.Equal(t, "high", gjson.GetBytes(body, "reasoning.effort").String())
	require.Equal(t, "provider-opaque", gjson.GetBytes(body, "input.1.encrypted_content").String())
	require.Equal(t, "commentary", gjson.GetBytes(body, "input.2.phase").String())
	require.Equal(t, "fc_1", gjson.GetBytes(body, "input.3.id").String())
	require.Equal(t, "function_call_output", gjson.GetBytes(body, "input.4.type").String())
	require.Equal(t, "call_1", gjson.GetBytes(body, "input.4.call_id").String())
	require.JSONEq(t, `{"results":[]}`, gjson.GetBytes(body, "input.4.output").String())
	require.Equal(t, "I found no references.", gjson.GetBytes(body, "input.5.content").String())
	require.Equal(t, "continue", gjson.GetBytes(body, "input.6.content").String())
	require.NotContains(t, string(body), "Repeated display content")

	request.History[1].Results[0].ID = "fc_1"
	_, _, err = buildWorkbenchNativeRequest(request)
	require.ErrorIs(t, err, ErrOfficeInvalid)
}

func TestWorkbenchNativeChatCompletionsReconstructsToolArguments(t *testing.T) {
	wire := `data: {"choices":[{"index":0,"delta":{"reasoning_content":"provider reasoning","tool_calls":[{"index":0,"id":"call_legacy","type":"function","function":{"name":"search","arguments":"{\"query\":"}}]}}]}

data: {"choices":[{"index":0,"delta":{"tool_calls":[{"index":0,"function":{"arguments":"\"term\"}"}}]}}]}

data: {"choices":[{"index":0,"delta":{},"finish_reason":"tool_calls"}]}

data: [DONE]

`
	reply, err := parseWorkbenchNativeResponse(workbenchProtocolChatCompletions, []byte(wire))
	require.NoError(t, err)
	require.Empty(t, reply.ReasoningSummary)
	require.Len(t, reply.Calls, 1)
	require.Equal(t, "call_legacy", reply.Calls[0].ID)
	require.JSONEq(t, `{"query":"term"}`, string(reply.Calls[0].Arguments))
	input := workbenchNativeRequest{
		Protocol: workbenchProtocolChatCompletions, Model: "deepseek-v4-pro", ReplayToolReasoning: true,
		History: []workbenchNativeMessage{{Role: "user", Text: "look it up"}},
		Transcript: []workbenchNativeExchange{{Native: reply.Native, Results: []workbenchNativeToolResult{
			{ID: reply.Calls[0].ID, Name: "search", Output: json.RawMessage(`{"ok":true}`)},
		}}},
	}
	endpoint, body, err := buildWorkbenchNativeRequest(input)
	require.NoError(t, err)
	require.Equal(t, "/v1/chat/completions", endpoint)
	require.Equal(t, "provider reasoning", gjson.GetBytes(body, "messages.1.reasoning_content").String())
	require.Equal(t, "call_legacy", gjson.GetBytes(body, "messages.2.tool_call_id").String())
	input.ReplayToolReasoning = false
	_, body, err = buildWorkbenchNativeRequest(input)
	require.NoError(t, err)
	require.False(t, gjson.GetBytes(body, "messages.1.reasoning_content").Exists())
}

func TestWorkbenchNativePartialStreamCannotDispatchTools(t *testing.T) {
	responses := `data: {"type":"response.output_item.done","output_index":0,"item":{"type":"function_call","call_id":"call_bad","name":"search","arguments":"{}"}}

`
	reply, err := parseWorkbenchNativeResponse(workbenchProtocolResponses, []byte(responses))
	require.ErrorIs(t, err, ErrOfficeResultUnknown)
	require.Empty(t, reply.Calls)
	responses += `data: {"type":"response.incomplete","response":{"status":"incomplete","incomplete_details":{"reason":"max_output_tokens"}}}`
	reply, err = parseWorkbenchNativeResponse(workbenchProtocolResponses, []byte(responses))
	var upstream *workbenchNativeUpstreamError
	require.ErrorAs(t, err, &upstream)
	require.Equal(t, "max_output_tokens", upstream.Reason)
	require.Empty(t, reply.Calls)
}

type workbenchNativeBridgeRepo struct {
	*officeBridgeTestRepo
	events []string
}

func (r *workbenchNativeBridgeRepo) AppendEvent(_ context.Context, _ OfficeRunLease, _, kind string, payload json.RawMessage) (*OfficeRunEvent, error) {
	r.events = append(r.events, kind+":"+string(payload))
	return &OfficeRunEvent{}, nil
}

func TestWorkbenchNativeBridgePreservesGatewayFailureReason(t *testing.T) {
	cases := []struct {
		name     string
		body     string
		wantCode string
		wantText string
	}{
		{
			name:     "gateway authentication balance rejection",
			body:     `{"code":"INSUFFICIENT_BALANCE","message":"Insufficient account balance"}`,
			wantCode: "INSUFFICIENT_BALANCE",
			wantText: "余额不足，请充值后重新发送消息。",
		},
		{
			name:     "existing nested code",
			body:     `{"code":403,"error":{"code":"insufficient_credits","message":"Insufficient credits"}}`,
			wantCode: "INSUFFICIENT_BALANCE",
			wantText: "余额不足，请充值后重新发送消息。",
		},
		{
			name:     "nested code takes precedence",
			body:     `{"code":"INSUFFICIENT_BALANCE","error":{"code":"permission_denied"}}`,
			wantCode: "MODEL_HTTP_403",
			wantText: "当前账号或上游未授权使用此模型。",
		},
		{
			name:     "provider permission error with a numeric status",
			body:     `{"error":{"code":403,"status":"PERMISSION_DENIED","message":"Permission denied"}}`,
			wantCode: "MODEL_HTTP_403",
			wantText: "当前账号或上游未授权使用此模型。",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			svc, repo := officeBridgeFixture(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
				w.Header().Set("Content-Type", "application/json")
				w.WriteHeader(http.StatusForbidden)
				_, _ = io.WriteString(w, tc.body)
			}))
			repo.lease.Run.BrowserDriven = true
			const operation = "browser:model:0"
			_, err := svc.invokeOfficeGateway(context.Background(), repo.lease, operation,
				"/v1/responses", []byte(`{"model":"public-model","input":"hello"}`), "", workbenchProtocolResponses)
			require.NoError(t, err)
			require.NotContains(t, string(repo.ops[operation].Result.Output), `"body"`)

			state, err := workbenchState(workbenchProtocolResponses, []OfficeOperation{repo.ops[operation]})
			require.NoError(t, err)
			require.NotNil(t, state.ModelError)
			code, message := workbenchModelError(state.ModelError)
			require.Equal(t, tc.wantCode, code)
			require.Equal(t, tc.wantText, message)
		})
	}
}

func TestWorkbenchBridgeRetainsLocalFailureAcrossGatewayProtocols(t *testing.T) {
	cases := []struct {
		name       string
		protocol   workbenchProtocol
		wireStatus int
		status     int
		code       string
		body       string
		wantText   string
	}{
		{"Gemini balance error", workbenchProtocolGemini, http.StatusForbidden, http.StatusForbidden,
			"INSUFFICIENT_BALANCE", `{"error":{"code":403,"status":"PERMISSION_DENIED","message":"Insufficient account balance"}}`,
			"余额不足"},
		{"Responses balance error after keepalive", workbenchProtocolResponses, http.StatusOK, http.StatusForbidden,
			"INSUFFICIENT_BALANCE", "data: {\"type\":\"response.failed\",\"response\":{\"status\":\"failed\",\"error\":{\"code\":\"billing_error\"}}}\n\n",
			"余额不足"},
		{"model price missing", workbenchProtocolChatCompletions, http.StatusForbidden, http.StatusForbidden,
			"MODEL_PRICING_UNAVAILABLE", `{"error":{"type":"billing_error","message":"insufficient balance"}}`,
			"当前模型暂时无法计费"},
		{"platform quota", workbenchProtocolAnthropic, http.StatusTooManyRequests, http.StatusTooManyRequests,
			"USER_PLATFORM_DAILY_QUOTA_EXHAUSTED", `{"error":{"type":"rate_limit_exceeded"}}`,
			"模型平台上的周期额度"},
		{"gateway queue full after keepalive", workbenchProtocolResponses, http.StatusOK, http.StatusTooManyRequests,
			"GATEWAY_QUEUE_FULL", "data: {\"type\":\"response.failed\",\"response\":{\"status\":\"failed\"}}\n\n",
			"等待中的请求太多"},
		{"no upstream after keepalive", workbenchProtocolResponses, http.StatusOK, http.StatusServiceUnavailable,
			"MODEL_UPSTREAM_UNAVAILABLE", "data: {\"type\":\"response.failed\",\"response\":{\"status\":\"failed\"}}\n\n",
			"暂时没有可用的上游"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			svc, repo := officeBridgeFixture(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if tc.wireStatus == http.StatusOK {
					w.Header().Set("Content-Type", "text/event-stream")
					_, _ = io.WriteString(w, ": keepalive\n\n")
				} else {
					w.Header().Set("Content-Type", "application/json")
					w.WriteHeader(tc.wireStatus)
				}
				MarkWorkbenchGatewayFailure(r.Context(), tc.status, tc.code)
				if tc.code == "MODEL_PRICING_UNAVAILABLE" {
					MarkWorkbenchGatewayFailure(r.Context(), tc.status, "INSUFFICIENT_BALANCE")
				}
				_, _ = io.WriteString(w, tc.body)
			}))
			repo.lease.Run.BrowserDriven = true
			reply, err := svc.invokeOfficeGateway(context.Background(), repo.lease, "browser:model:0",
				"/v1/responses", []byte(`{"model":"public-model","input":"hello"}`), "", tc.protocol)
			require.NoError(t, err)
			require.Equal(t, tc.status, reply.Status)
			require.Empty(t, reply.Body)
			code, message := workbenchModelError(reply)
			require.Equal(t, tc.code, code)
			require.Contains(t, message, tc.wantText)
		})
	}
}

func TestWorkbenchModelErrorFallbacks(t *testing.T) {
	code, message := workbenchModelError(&OfficeGatewayReply{Status: http.StatusPaymentRequired})
	require.Equal(t, "MODEL_HTTP_402", code)
	require.NotContains(t, message, "请充值")
	code, _ = workbenchModelError(&OfficeGatewayReply{Status: http.StatusRequestEntityTooLarge})
	require.Equal(t, "MODEL_HTTP_413", code)
	code, message = workbenchModelError(&OfficeGatewayReply{Status: http.StatusServiceUnavailable,
		Body: []byte(`{"error":{"type":"billing_service_error","message":"private diagnostic"}}`)})
	require.Equal(t, "BILLING_SERVICE_ERROR", code)
	require.NotContains(t, message, "private diagnostic")
	for reason, expected := range map[string]string{
		"max_output_tokens": "MODEL_OUTPUT_LIMIT",
		"MAX_TOKENS":        "MODEL_OUTPUT_LIMIT",
		"SAFETY":            "MODEL_CONTENT_BLOCKED",
		"empty_output":      "EMPTY_MODEL_RESULT",
		"failed":            "MODEL_STREAM_FAILED",
		"unrecognized":      "MODEL_INCOMPLETE",
	} {
		got, _ := workbenchModelIncompleteError(reason)
		require.Equal(t, expected, got, reason)
	}
}

func TestWorkbenchNativeBridgeSendsToolResultsAndReplaysGatewayInbox(t *testing.T) {
	for _, tc := range []struct {
		protocol          workbenchProtocol
		model             string
		path              string
		query             string
		requestFirstUser  string
		requestSignature  string
		requestReceiptID  string
		expectedSignature string
		expectedReceiptID string
		toolCallingSSE    string
		finalSSE          string
	}{
		{
			protocol:          workbenchProtocolGemini,
			model:             "gemini-3.6-flash",
			path:              "/v1beta/models/gemini-3.6-flash:streamGenerateContent",
			query:             "alt=sse",
			requestFirstUser:  "contents.0.parts.0.text",
			requestSignature:  "contents.1.parts.0.thoughtSignature",
			requestReceiptID:  "contents.2.parts.0.functionResponse.id",
			expectedSignature: "cHJpdmF0ZV9zaWc=",
			expectedReceiptID: "gem_call",
			toolCallingSSE:    "data: {\"candidates\":[{\"content\":{\"role\":\"model\",\"parts\":[{\"functionCall\":{\"name\":\"search\",\"id\":\"gem_call\",\"args\":{\"query\":\"go\"}},\"thoughtSignature\":\"cHJpdmF0ZV9zaWc=\"}]},\"finishReason\":\"STOP\"}]}\n\n",
			finalSSE:          "data: {\"candidates\":[{\"content\":{\"role\":\"model\",\"parts\":[{\"text\":\"Found it.\"}]},\"finishReason\":\"STOP\"}]}\n\n",
		},
		{
			protocol:          workbenchProtocolResponses,
			model:             "gpt-6-sol",
			path:              "/v1/responses",
			requestFirstUser:  "input.0.content",
			requestSignature:  "input.1.encrypted_content",
			requestReceiptID:  "input.3.call_id",
			expectedSignature: "provider-opaque",
			expectedReceiptID: "call_1",
			toolCallingSSE:    "data: {\"type\":\"response.completed\",\"response\":{\"status\":\"completed\",\"output\":[{\"type\":\"reasoning\",\"id\":\"rs_1\",\"summary\":[],\"encrypted_content\":\"provider-opaque\"},{\"type\":\"function_call\",\"id\":\"fc_1\",\"call_id\":\"call_1\",\"name\":\"search\",\"arguments\":\"{\\\"query\\\":\\\"go\\\"}\"}]}}\n\n",
			finalSSE: `data: {"type":"response.output_text.delta","delta":"Found it."}

data: {"type":"response.completed","response":{"status":"completed","output":[{"type":"message","role":"assistant","phase":"final_answer","content":[{"type":"output_text","text":"Found it."}]}]}}

`,
		},
		{
			protocol:          workbenchProtocolAnthropic,
			model:             "claude-sonnet-4-6",
			path:              "/v1/messages",
			requestFirstUser:  "messages.0.content.0.text",
			requestSignature:  "messages.1.content.0.signature",
			requestReceiptID:  "messages.2.content.0.tool_use_id",
			expectedSignature: "claude-opaque",
			expectedReceiptID: "toolu_search",
			toolCallingSSE: workbenchAnthropicTestSSE(
				`{"type":"message_start","message":{"type":"message","role":"assistant","content":[]}}`,
				`{"type":"content_block_start","index":0,"content_block":{"type":"thinking","thinking":"","signature":""}}`,
				`{"type":"content_block_delta","index":0,"delta":{"type":"signature_delta","signature":"claude-opaque"}}`,
				`{"type":"content_block_stop","index":0}`,
				`{"type":"content_block_start","index":1,"content_block":{"type":"tool_use","id":"toolu_search","name":"search","input":{"query":"go"}}}`,
				`{"type":"content_block_stop","index":1}`,
				`{"type":"message_delta","delta":{"stop_reason":"tool_use"}}`,
				`{"type":"message_stop"}`,
			),
			finalSSE: workbenchAnthropicTestSSE(
				`{"type":"message_start","message":{"type":"message","role":"assistant","content":[]}}`,
				`{"type":"content_block_start","index":0,"content_block":{"type":"text","text":""}}`,
				`{"type":"content_block_delta","index":0,"delta":{"type":"text_delta","text":"Found it."}}`,
				`{"type":"content_block_stop","index":0}`,
				`{"type":"message_delta","delta":{"stop_reason":"end_turn"}}`,
				`{"type":"message_stop"}`,
			),
		},
		{
			protocol:          workbenchProtocolChatCompletions,
			model:             "deepseek-flash",
			path:              "/v1/chat/completions",
			requestFirstUser:  "messages.0.content",
			requestSignature:  "messages.1.reasoning_content",
			requestReceiptID:  "messages.2.tool_call_id",
			expectedSignature: "reasoning to replay",
			expectedReceiptID: "deepseek_call",
			toolCallingSSE: `data: {"choices":[{"delta":{"reasoning_content":"reasoning to replay","tool_calls":[{"index":0,"id":"deepseek_call","type":"function","function":{"name":"search","arguments":"{\"query\":\"go\"}"}}]},"finish_reason":"tool_calls"}]}

`,
			finalSSE: `data: {"choices":[{"delta":{"content":"Found it."},"finish_reason":"stop"}]}

`,
		},
	} {
		t.Run(string(tc.protocol), func(t *testing.T) {
			var requests int
			svc, baseRepo := officeBridgeFixture(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				requests++
				require.Equal(t, tc.path, r.URL.Path)
				require.Equal(t, tc.query, r.URL.RawQuery)
				require.Equal(t, "Bearer private-key", r.Header.Get("Authorization"))
				require.Equal(t, int64(31), r.Context().Value(ctxkey.WorkbenchGroupID))
				require.NotEmpty(t, r.Header.Get("session_id"))
				body, err := io.ReadAll(r.Body)
				require.NoError(t, err)
				require.Equal(t, "Look up Go.", gjson.GetBytes(body, tc.requestFirstUser).String())
				w.Header().Set("Content-Type", "text/event-stream")
				if requests == 1 {
					_, _ = io.WriteString(w, tc.toolCallingSSE)
					return
				}
				require.Equal(t, tc.expectedSignature, gjson.GetBytes(body, tc.requestSignature).String())
				require.Equal(t, tc.expectedReceiptID, gjson.GetBytes(body, tc.requestReceiptID).String())
				_, _ = io.WriteString(w, tc.finalSSE)
			}))
			baseRepo.lease.Run.BrowserDriven = true
			baseRepo.lease.Run.GroupID = 31
			repo := &workbenchNativeBridgeRepo{officeBridgeTestRepo: baseRepo}
			svc.repo = repo
			in := workbenchNativeRequest{Protocol: tc.protocol, Model: tc.model, MaxOutputTokens: 2048, ReplayToolReasoning: true, History: []workbenchNativeMessage{{Role: "user", Text: "Look up Go."}}}
			endpoint, body, err := buildWorkbenchNativeRequest(in)
			require.NoError(t, err)
			ctx := context.Background()
			first, err := svc.invokeOfficeGateway(ctx, repo.lease, "model-1", endpoint, body, "", tc.protocol)
			require.NoError(t, err)
			replayed, err := svc.invokeOfficeGateway(ctx, repo.lease, "model-1", endpoint, body, "", tc.protocol)
			require.NoError(t, err)
			require.Equal(t, first, replayed)
			require.Equal(t, 1, requests)
			require.Empty(t, repo.ops["model-1"].Input)
			require.NotEmpty(t, repo.ops["model-1"].InputHash)
			require.Empty(t, first.Body)
			require.NotContains(t, string(repo.ops["model-1"].Result.Output), `"body"`)
			parsed, err := first.workbenchResult(tc.protocol)
			require.NoError(t, err)
			require.Len(t, parsed.Calls, 1)
			in.Transcript = []workbenchNativeExchange{{Native: parsed.Native, Results: []workbenchNativeToolResult{
				{ID: parsed.Calls[0].ID, Name: parsed.Calls[0].Name, Output: json.RawMessage(`{"results":["go.dev"]}`)},
			}}}
			_, body, err = buildWorkbenchNativeRequest(in)
			require.NoError(t, err)
			final, err := svc.invokeOfficeGateway(ctx, repo.lease, "model-2", endpoint, body, "", tc.protocol)
			require.NoError(t, err)
			parsed, err = final.workbenchResult(tc.protocol)
			require.NoError(t, err)
			require.Equal(t, "Found it.", parsed.Text)
			require.Equal(t, 2, requests)
			require.NotEmpty(t, repo.events)
			require.NotContains(t, strings.Join(repo.events, ""), tc.expectedSignature)
		})
	}
}
