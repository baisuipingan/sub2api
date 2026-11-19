//go:build integration

package handler

import (
	"bytes"
	"context"
	"encoding/json"
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

	"github.com/Wei-Shaw/sub2api/internal/config"
	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
)

type officePythonFinishRepo struct {
	service.OfficeAgentRepository
	lease    service.OfficeRunLease
	finishes atomic.Int32
	content  json.RawMessage
}

func (r *officePythonFinishRepo) ClaimRun(context.Context, string, time.Duration) (*service.OfficeRunLease, error) {
	return &r.lease, nil
}
func (r *officePythonFinishRepo) ListMessages(context.Context, int64, string, int64, int) ([]service.OfficeMessage, error) {
	return []service.OfficeMessage{{ID: uuid.NewString(), RunID: r.lease.Run.ID, Role: "user", Content: json.RawMessage(`"Reply with the fixture sentence."`)}}, nil
}
func (r *officePythonFinishRepo) GetRun(_ context.Context, user int64, id string) (*service.OfficeRun, error) {
	if user != r.lease.Run.UserID || id != r.lease.Run.ID {
		return nil, service.ErrOfficeNotFound
	}
	return &r.lease.Run, nil
}
func (r *officePythonFinishRepo) CheckLease(_ context.Context, lease service.OfficeRunLease) error {
	if lease.Epoch != r.lease.Epoch || lease.Token != r.lease.Token || lease.Owner != r.lease.Owner {
		return service.ErrOfficeLeaseLost
	}
	return nil
}
func (r *officePythonFinishRepo) FinishRun(_ context.Context, _ service.OfficeRunLease, status string, content json.RawMessage, _, _ string) (*service.OfficeRun, error) {
	r.content = append(json.RawMessage(nil), content...)
	r.finishes.Add(1)
	run := r.lease.Run
	run.Status = status
	return &run, nil
}

// This crosses the real Python polling-worker completion path and the actual
// Gin handler, including signed lease authentication and DisallowUnknownFields.
// Only the model answer is a fixture; no provider or billing acceptance is claimed.
func TestOfficePythonWorkerUsesStrictGoFinishEndpoint(t *testing.T) {
	python := os.Getenv("OFFICE_AGENT_TEST_PYTHON")
	if python == "" {
		t.Skip("set OFFICE_AGENT_TEST_PYTHON to the pinned office-agent environment")
	}
	gin.SetMode(gin.TestMode)
	started := time.Now()
	repo := &officePythonFinishRepo{lease: service.OfficeRunLease{
		Run: service.OfficeRun{ID: uuid.NewString(), ConversationID: uuid.NewString(), UserID: 17,
			Mode: "chat", Model: "public-fixture", Status: service.OfficeRunRunning, StartedAt: &started,
			Limits: service.OfficeRunLimits{MaxModelCalls: 4, MaxToolCalls: 8, MaxOutputTokens: 1024, MaxRunSeconds: 60}},
		Owner: "finish-contract", Token: uuid.NewString(), Epoch: 1, Until: started.Add(time.Minute),
	}}
	cfg := &config.Config{OfficeAgent: config.OfficeAgentConfig{Enabled: true, WorkerToken: strings.Repeat("fixture-", 5),
		MaxConcurrentCalls: 1, MaxModelCalls: 4, MaxToolCalls: 8, MaxOutputTokens: 1024, MaxRunSeconds: 60}}
	svc := service.NewOfficeAgentService(repo, cfg, nil, nil, nil, nil, nil, nil)
	svc.BindGatewayRouter(http.NotFoundHandler())
	manifest, err := svc.ClaimPreset(context.Background(), "finish-contract", "chat")
	require.NoError(t, err)
	require.NotNil(t, manifest)
	h := NewOfficeAgentHandler(svc)
	router := gin.New()
	router.Use(h.Protect)
	var modelCalls atomic.Int32
	finishPayloads := make(chan map[string]json.RawMessage, 2)
	prefix := "/internal/office-agent/v1/runs/" + repo.lease.Run.ID
	router.POST(prefix+"/model/chat/completions", func(c *gin.Context) {
		require.Equal(t, "Bearer "+manifest.LeaseToken, c.GetHeader("Authorization"))
		require.Equal(t, "1", c.GetHeader("X-Office-Lease"))
		modelCalls.Add(1)
		c.Data(http.StatusOK, "application/json", []byte(`{"choices":[{"message":{"role":"assistant","content":"Python worker finished through the strict Go endpoint."},"finish_reason":"stop"}]}`))
	})
	router.POST("/internal/office-agent/v1/runs/:id/finish", func(c *gin.Context) {
		body, readErr := io.ReadAll(c.Request.Body)
		require.NoError(t, readErr)
		var fields map[string]json.RawMessage
		require.NoError(t, json.Unmarshal(body, &fields))
		finishPayloads <- fields
		c.Request.Body = io.NopCloser(bytes.NewReader(body))
		h.Finish(c)
	})
	server := httptest.NewServer(router)
	defer server.Close()

	// Preserve the strict endpoint: the old payload must fail before FinishRun.
	old, err := http.NewRequest(http.MethodPost, server.URL+prefix+"/finish", strings.NewReader(`{"status":"completed","content":"old","error_code":"","error_message":"","clarification":null}`))
	require.NoError(t, err)
	old.Header.Set("Content-Type", "application/json")
	old.Header.Set("Authorization", "Bearer "+manifest.LeaseToken)
	old.Header.Set("X-Office-Lease", "1")
	response, err := server.Client().Do(old)
	require.NoError(t, err)
	require.Equal(t, http.StatusBadRequest, response.StatusCode)
	require.NoError(t, response.Body.Close())
	require.Zero(t, repo.finishes.Load())
	<-finishPayloads

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
	require.NoError(t, err, "Python Finish contract probe: %s", diagnostics.String())
	var result struct {
		Status  string `json:"status"`
		Content string `json:"content"`
	}
	require.NoError(t, json.Unmarshal(output, &result))
	require.Equal(t, service.OfficeRunCompleted, result.Status)
	require.EqualValues(t, 1, modelCalls.Load())
	require.EqualValues(t, 1, repo.finishes.Load())
	require.Contains(t, string(repo.content), "strict Go endpoint")
	finishFields := <-finishPayloads
	require.Len(t, finishFields, 4)
	for _, field := range []string{"status", "content", "error_code", "error_message"} {
		require.Contains(t, finishFields, field)
	}
	require.NotContains(t, string(output), manifest.LeaseToken)
}
