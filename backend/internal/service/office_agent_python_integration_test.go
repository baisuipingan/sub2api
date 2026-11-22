//go:build integration

package service

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

// This uses a real Python process and the pinned DeepAgents graph, not a fake
// agent loop. The HTTP adapter below is intentionally only a contract harness:
// handler auth/ingress and real PostgreSQL fencing have separate tests. Supplier
// responses are fixtures, so this is not evidence of production billing.
func TestOfficePythonGraphThroughGoModelBridge(t *testing.T) {
	python := os.Getenv("OFFICE_AGENT_TEST_PYTHON")
	if python == "" {
		t.Skip("set OFFICE_AGENT_TEST_PYTHON to the pinned office-agent environment")
	}
	const legacyModelBodyLimit = 256 << 10
	// Each page fits the web reader's text limit. Their combined results push
	// the later request past the former bridge limit alongside the conversation.
	pageURLs := []string{"https://example.test/report/a", "https://example.test/report/b", "https://example.test/report/c"}
	pages := make(map[string]string, len(pageURLs))
	for i, pageURL := range pageURLs {
		pages[pageURL] = fmt.Sprintf("资料 %d\n", i+1) + strings.Repeat("网页资料", 3450)
	}
	var calls, firstRequestBytes, finalRequestBytes atomic.Int32
	svc, repo := officeBridgeFixture(http.HandlerFunc(func(w http.ResponseWriter, request *http.Request) {
		call := int(calls.Add(1))
		if request.Header.Get("Authorization") != "Bearer private-key" {
			http.Error(w, "supplier key missing", http.StatusUnauthorized)
			return
		}
		body, err := io.ReadAll(request.Body)
		if err != nil {
			http.Error(w, "invalid supplier request", http.StatusBadRequest)
			return
		}
		if call == 1 {
			firstRequestBytes.Store(int32(len(body)))
		}
		if call > len(pageURLs)+1 {
			http.Error(w, "unexpected supplier call", http.StatusBadRequest)
			return
		}
		message := map[string]any{"role": "assistant", "content": "真实 Agent 图已通过本站模型桥完成对话。"}
		finish := "stop"
		if call <= len(pageURLs) {
			arguments, _ := json.Marshal(map[string]string{"url": pageURLs[call-1]})
			message["content"] = ""
			message["tool_calls"] = []map[string]any{{
				"id": fmt.Sprintf("read-%d", call), "type": "function",
				"function": map[string]string{"name": "web_read", "arguments": string(arguments)},
			}}
			finish = "tool_calls"
		} else {
			finalRequestBytes.Store(int32(len(body)))
			for i := range pageURLs {
				if !bytes.Contains(body, fmt.Appendf(nil, "资料 %d", i+1)) {
					http.Error(w, "prior tool result missing", http.StatusBadRequest)
					return
				}
			}
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{"id": "fixture", "choices": []map[string]any{{"message": message, "finish_reason": finish}}})
	}))
	repo.lease.Run.Mode, repo.lease.Run.SearchEnabled = "office", true
	prefix := "/internal/office-agent/v1/runs/" + repo.lease.Run.ID + "/"
	var finished atomic.Bool
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost || !strings.HasPrefix(r.URL.Path, prefix) {
			http.NotFound(w, r)
			return
		}
		lease, err := svc.AuthenticateLease(r.Context(), repo.lease.Run.ID, r.Header.Get("X-Office-Lease"), strings.TrimPrefix(r.Header.Get("Authorization"), "Bearer "))
		if err != nil {
			http.Error(w, "invalid lease", http.StatusUnauthorized)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		switch strings.TrimPrefix(r.URL.Path, prefix) {
		case "model/chat/completions":
			body, readErr := io.ReadAll(r.Body)
			if readErr != nil {
				http.Error(w, "invalid body", 400)
				return
			}
			reply, invokeErr := svc.InvokeModel(r.Context(), *lease, r.Header.Get("X-Office-Operation-ID"), body)
			if invokeErr != nil {
				http.Error(w, "bridge rejected", 409)
				return
			}
			w.WriteHeader(reply.Status)
			_, _ = w.Write(reply.Body)
		case "events":
			_, _ = io.WriteString(w, `{"code":0,"data":{}}`)
		case "web-read":
			var in struct {
				URL string `json:"url"`
			}
			if json.NewDecoder(r.Body).Decode(&in) != nil || pages[in.URL] == "" {
				http.Error(w, "invalid fixture page", http.StatusBadRequest)
				return
			}
			_ = json.NewEncoder(w).Encode(map[string]string{"url": in.URL, "text": pages[in.URL]})
		case "finish":
			var in struct {
				Status       string `json:"status"`
				Content      string `json:"content"`
				ErrorCode    string `json:"error_code"`
				ErrorMessage string `json:"error_message"`
			}
			if json.NewDecoder(r.Body).Decode(&in) != nil {
				http.Error(w, "invalid result", 400)
				return
			}
			_, err := svc.Finish(r.Context(), *lease, in.Status, in.Content, in.ErrorCode, in.ErrorMessage)
			if err != nil {
				http.Error(w, "invalid finish", 409)
				return
			}
			finished.Store(true)
			_, _ = io.WriteString(w, `{"code":0,"data":{}}`)
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()
	manifest := OfficeWorkerManifest{RunID: repo.lease.Run.ID, ConversationID: repo.lease.Run.ConversationID,
		ThreadID: repo.lease.Run.ConversationID, LeaseEpoch: repo.lease.Epoch, LeaseToken: svc.leaseCredential(repo.lease),
		LeaseUntil: repo.lease.Until, Model: repo.lease.Run.Model, BridgePath: prefix + "model/chat/completions",
		Mode: "office", SearchEnabled: true, Parameters: json.RawMessage(`{}`), InputResources: []OfficeResourceVersion{},
		Limits: repo.lease.Run.Limits, Messages: []OfficeWorkerMessage{
			{Role: "user", Content: "请整理已有资料。"},
			{Role: "assistant", Content: strings.Repeat("已有资料", 6400)},
			{Role: "user", Content: "请补充此前资料。"},
			{Role: "assistant", Content: strings.Repeat("补充资料", 6400)},
			{Role: "user", Content: "请参考此前资料并查阅网页，然后用中文回复。"},
		}}
	input, err := json.Marshal(manifest)
	require.NoError(t, err)
	root, err := filepath.Abs("../../../services/office-agent")
	require.NoError(t, err)
	ctx, cancel := context.WithTimeout(context.Background(), 45*time.Second)
	defer cancel()
	command := exec.CommandContext(ctx, python, filepath.Join(root, "tests/go_bridge_smoke.py"), server.URL)
	command.Dir = root
	command.Stdin = bytes.NewReader(input)
	var diagnostics bytes.Buffer
	command.Stderr = &diagnostics
	output, err := command.Output()
	require.NoError(t, err, "Python contract probe stderr: %s; public result: %s", diagnostics.String(), string(output))
	var result struct {
		Status  string `json:"status"`
		Content string `json:"content"`
	}
	require.NoError(t, json.Unmarshal(output, &result))
	require.Equal(t, OfficeRunCompleted, result.Status)
	require.Contains(t, result.Content, "本站模型桥")
	require.EqualValues(t, len(pageURLs)+1, calls.Load())
	require.Less(t, int(firstRequestBytes.Load()), legacyModelBodyLimit)
	require.Greater(t, int(finalRequestBytes.Load()), legacyModelBodyLimit)
	t.Logf("supplier received %d-byte initial and %d-byte follow-up requests", firstRequestBytes.Load(), finalRequestBytes.Load())
	require.True(t, finished.Load(), "the real worker completion protocol must finish the Go run")
	require.NotContains(t, string(output), "private-key")
	require.Contains(t, string(repo.finishedContent), "本站模型桥")
}
