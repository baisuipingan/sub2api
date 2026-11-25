package service

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"maps"
	"net/http"
	"testing"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/pkg/ctxkey"
	"github.com/stretchr/testify/require"
)

type workbenchRetryTestRepo struct {
	*workbenchNativeBridgeRepo
	workbenchBrowserRepository
	OfficeChatRepository
}

func (r *workbenchRetryTestRepo) BrowserOperations(context.Context, int64, string) ([]OfficeOperation, error) {
	var operations []OfficeOperation
	for op := range maps.Values(r.ops) {
		operations = append(operations, op)
	}
	return operations, nil
}

func (r *workbenchRetryTestRepo) ContextMessages(_ context.Context, run OfficeRun) ([]OfficeMessage, error) {
	return []OfficeMessage{{RunID: run.ID, Role: "user", Content: json.RawMessage(`{"text":"What did you find?"}`)}}, nil
}

func (r *workbenchRetryTestRepo) BrowserDeliveredArtifacts(context.Context, int64, []string) (map[string][]OfficeResource, error) {
	return nil, nil
}

func TestWorkbenchBrowserRetriesModelAndReusesToolResults(t *testing.T) {
	const failedID = "browser:model:1"
	const retryID = "browser:model:1:retry:1"
	const toolID = "browser:tool:0:0"
	var bodies, requestIDs []string
	svc, base := officeBridgeFixture(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, err := io.ReadAll(r.Body)
		require.NoError(t, err)
		bodies = append(bodies, string(body))
		requestIDs = append(requestIDs, r.Context().Value(ctxkey.ClientRequestID).(string))
		w.Header().Set("Content-Type", "text/event-stream")
		if len(bodies) == 1 {
			_, _ = io.WriteString(w, "data: {\"choices\":[{\"index\":0,\"delta\":{\"content\":\"Draft\"}}]}\n\n"+
				"data: {\"choices\":[],\"usage\":{\"prompt_tokens\":20,\"completion_tokens\":2}}\n\ndata: [DONE]\n\n")
			return
		}
		_, _ = io.WriteString(w, "data: {\"choices\":[{\"index\":0,\"delta\":{\"content\":\"Recovered answer\"},\"finish_reason\":\"stop\"}]}\n\n")
	}))
	svc.settings = &SettingService{settingRepo: &desktopAgentSettingsStub{values: map[string]string{
		"workbench_chat_enabled": "true", SettingKeyDesktopAgentConfig: `{"enabled":true,"group_id":1}`,
	}}}
	repo := &workbenchRetryTestRepo{workbenchNativeBridgeRepo: &workbenchNativeBridgeRepo{officeBridgeTestRepo: base}}
	svc.repo = repo
	base.lease.Run.BrowserDriven = true
	base.lease.Run.Parameters = workbenchNativeJSON(workbenchBrowserParameters{Protocol: workbenchProtocolChatCompletions})
	base.ops["browser:model:0"] = workbenchReplayTestOperation(t, "browser:model:0", OfficeGatewayReply{
		Status: http.StatusOK,
		Native: &workbenchNativeReply{
			Native: json.RawMessage(`{"role":"assistant","tool_calls":[{"type":"function","id":"search-1","function":{"name":"web_search","arguments":"{}"}}]}`),
			Calls:  []workbenchNativeCall{{ID: "search-1", Name: "web_search", Arguments: json.RawMessage(`{}`)}},
		},
	})
	base.ops[toolID] = workbenchReplayTestOperation(t, toolID, map[string]string{"evidence": "saved search result"})
	ctx := context.Background()
	require.NoError(t, svc.executeBrowserStep(ctx, repo, base.lease))
	failed := base.ops[failedID]
	require.Equal(t, OfficeOperationCompleted, failed.State)
	require.Equal(t, http.StatusOK, failed.Result.HTTPStatus, "the original usage may settle after the gateway returns")
	var failure OfficeGatewayReply
	require.NoError(t, json.Unmarshal(failed.Result.Output, &failure))
	require.Equal(t, "MODEL_STREAM_FINISH_MISSING", failure.ErrorCode)
	require.True(t, failure.Retryable)
	require.NoError(t, svc.executeBrowserStep(ctx, repo, base.lease))
	require.NoError(t, svc.executeBrowserStep(ctx, repo, base.lease))
	require.Len(t, bodies, 2)
	require.Equal(t, bodies[0], bodies[1])
	require.Contains(t, bodies[1], "saved search result")
	require.Equal(t, []string{failed.RequestID, base.ops[retryID].RequestID}, requestIDs)
	require.NotEqual(t, requestIDs[0], requestIDs[1])
	require.Len(t, base.ops, 4, "the completed search is represented by just one operation")
	require.Contains(t, repo.events, `model.retrying:{"attempt":1,"max_retries":5,"operation_id":"browser:model:1"}`)
	require.Contains(t, string(base.finishedContent), "Recovered answer")
	require.NotContains(t, string(base.finishedContent), "Draft")
}

func TestWorkbenchRetryStopsAfterLimitAndFencesDispatchedAttempts(t *testing.T) {
	const initialID = "browser:model:0"
	const retryID = "browser:model:0:retry:1"
	const finalID = "browser:model:0:retry:5"
	first := workbenchReplayTestOperation(t, initialID, OfficeGatewayReply{
		Status: http.StatusServiceUnavailable, Retryable: true, RetryAfterMillis: 3000,
	})
	completedAt := time.Now()
	first.CompletedAt = &completedAt
	state, err := workbenchState(workbenchProtocolChatCompletions, []OfficeOperation{first})
	require.NoError(t, err)
	require.Equal(t, retryID, state.ModelID)
	require.Equal(t, completedAt.Add(3*time.Second), state.Retry.notBefore)

	journal := []OfficeOperation{first, {ID: retryID, State: OfficeOperationDispatched}}
	state, err = workbenchState(workbenchProtocolChatCompletions, journal)
	require.NoError(t, err)
	require.Equal(t, retryID, state.Uncertain)

	journal = journal[:1]
	for attempt := 1; attempt < 5; attempt++ {
		id := fmt.Sprintf("browser:model:0:retry:%d", attempt)
		journal = append(journal, workbenchReplayTestOperation(t, id, OfficeGatewayReply{Status: http.StatusBadGateway, Retryable: true}))
		state, err = workbenchState(workbenchProtocolChatCompletions, journal)
		require.NoError(t, err)
		require.Nil(t, state.ModelError)
		require.Equal(t, fmt.Sprintf("browser:model:0:retry:%d", attempt+1), state.ModelID)
		require.Equal(t, attempt+1, state.Retry.attempt)
	}
	journal = append(journal, workbenchReplayTestOperation(t, finalID, OfficeGatewayReply{Status: http.StatusGatewayTimeout, Retryable: true}))
	state, err = workbenchState(workbenchProtocolChatCompletions, journal)
	require.NoError(t, err)
	require.Equal(t, finalID, state.ModelID)
	require.Equal(t, http.StatusGatewayTimeout, state.ModelError.Status)
}

func TestWorkbenchModelRetryRecognizesTransientFailuresOnly(t *testing.T) {
	for _, test := range []struct {
		name  string
		reply OfficeGatewayReply
		want  bool
	}{
		{"temporary upstream overload", OfficeGatewayReply{Status: 503}, true},
		{"temporary stream network error", OfficeGatewayReply{Status: 200, IncompleteReason: "network_error"}, true},
		{"balance rejected after stream opened", OfficeGatewayReply{Status: 429, ErrorCode: "INSUFFICIENT_BALANCE"}, false},
		{"period quota exhausted", OfficeGatewayReply{Status: 429, ErrorCode: "API_KEY_RATE_5H_EXCEEDED"}, false},
		{"provider retry-after exceeds local cap", OfficeGatewayReply{Status: 429, RetryAfterMillis: 11000}, false},
		{"upstream authentication rejected", OfficeGatewayReply{Status: 502, ErrorCode: "MODEL_UPSTREAM_ACCESS_DENIED"}, false},
		{"invalid stream format", OfficeGatewayReply{Status: 502, ErrorCode: "MODEL_STREAM_INVALID_RESPONSE"}, false},
		{"invalid tool call", OfficeGatewayReply{Status: 502, ErrorCode: "MODEL_TOOL_CALL_INVALID"}, false},
		{"internal progress write failure", OfficeGatewayReply{Status: 502, ErrorCode: "MODEL_RESPONSE_WRITE_FAILED"}, false},
		{"safety blocked", OfficeGatewayReply{Status: 200, IncompleteReason: "content_filter"}, false},
	} {
		t.Run(test.name, func(t *testing.T) {
			require.Equal(t, test.want, isWorkbenchRetryableModelFailure(&test.reply))
		})
	}
}

func TestWorkbenchModelRetryRequiresRecognizableStreamBeforeMissingFinish(t *testing.T) {
	for _, test := range []struct {
		name, response, code string
		retry                bool
	}{
		{"bare JSON", `{"choices":[{"message":{"content":"hello"},"finish_reason":"stop"}]}`, "MODEL_STREAM_INVALID_RESPONSE", false},
		{"keepalive only", ": ping\n\n", "MODEL_STREAM_INVALID_RESPONSE", false},
		{"usage frame", "data: {\"choices\":[],\"usage\":{\"prompt_tokens\":2}}\n\n", "MODEL_STREAM_FINISH_MISSING", true},
		{"invalid tool call", "data: {\"choices\":[{\"delta\":{\"tool_calls\":[{\"function\":{\"name\":\"web_search\",\"arguments\":\"{\"}}]},\"finish_reason\":\"tool_calls\"}]}\n\n", "MODEL_TOOL_CALL_INVALID", false},
	} {
		t.Run(test.name, func(t *testing.T) {
			svc, repo := officeBridgeFixture(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
				w.Header().Set("Content-Type", "text/event-stream")
				_, _ = io.WriteString(w, test.response)
			}))
			repo.lease.Run.BrowserDriven = true
			reply, err := svc.invokeOfficeGateway(context.Background(), repo.lease, "browser:model:0", "/v1/chat/completions",
				[]byte(`{"model":"public-model","messages":[]}`), "", workbenchProtocolChatCompletions)
			require.NoError(t, err)
			require.Equal(t, test.code, reply.ErrorCode)
			require.Equal(t, test.retry, reply.Retryable)
		})
	}
}
