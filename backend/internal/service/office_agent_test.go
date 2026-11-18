package service

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/config"
	"github.com/Wei-Shaw/sub2api/internal/pkg/ctxkey"
	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
)

type officeBridgeTestRepo struct {
	OfficeAgentRepository
	mu              sync.Mutex
	lease           OfficeRunLease
	active          bool
	ops             map[string]OfficeOperation
	finishedContent json.RawMessage
	messageReads    int
}

func (r *officeBridgeTestRepo) ListMessages(_ context.Context, userID int64, conversation string, _ int64, _ int) ([]OfficeMessage, error) {
	r.messageReads++
	if userID != r.lease.Run.UserID || conversation != r.lease.Run.ConversationID {
		return nil, ErrOfficeNotFound
	}
	return []OfficeMessage{{RunID: r.lease.Run.ID, Role: "assistant", Content: json.RawMessage(`"stored answer"`)}}, nil
}

func (r *officeBridgeTestRepo) FinishRun(_ context.Context, _ OfficeRunLease, status string, content json.RawMessage, _, _ string) (*OfficeRun, error) {
	r.finishedContent = content
	run := r.lease.Run
	run.Status = status
	return &run, nil
}

func (r *officeBridgeTestRepo) CheckLease(_ context.Context, lease OfficeRunLease) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	if !r.active || lease.Token != r.lease.Token || lease.Epoch != r.lease.Epoch {
		return ErrOfficeLeaseLost
	}
	return nil
}
func (r *officeBridgeTestRepo) GetRun(_ context.Context, userID int64, id string) (*OfficeRun, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.lease.Run.UserID != userID || r.lease.Run.ID != id {
		return nil, ErrOfficeNotFound
	}
	run := r.lease.Run
	return &run, nil
}
func (r *officeBridgeTestRepo) PrepareOperation(_ context.Context, _ OfficeRunLease, in OfficeOperationPrepare) (*OfficeOperation, bool, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if op, exists := r.ops[in.ID]; exists {
		if op.InputHash != in.InputHash || op.Kind != in.Kind {
			return nil, false, ErrOfficeConflict
		}
		return &op, false, nil
	}
	op := OfficeOperation{ID: in.ID, Kind: in.Kind, State: OfficeOperationPrepared, InputHash: in.InputHash, Input: in.Input, RequestID: in.RequestID}
	r.ops[in.ID] = op
	return &op, true, nil
}
func (r *officeBridgeTestRepo) DispatchOperation(_ context.Context, _ OfficeRunLease, id string) (*OfficeOperation, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	op := r.ops[id]
	if op.State != OfficeOperationPrepared {
		return nil, ErrOfficeOperationDispatched
	}
	op.State = OfficeOperationDispatched
	r.ops[id] = op
	return &op, nil
}
func (r *officeBridgeTestRepo) RecordOperationResult(_ context.Context, _ OfficeRunLease, id string, result OfficeOperationResult) (*OfficeOperation, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	op := r.ops[id]
	op.State, op.Result = OfficeOperationCompleted, &result
	r.ops[id] = op
	return &op, nil
}
func (r *officeBridgeTestRepo) MarkOperationUnknown(_ context.Context, _ OfficeRunLease, id, _ string) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	op := r.ops[id]
	op.State = OfficeOperationUnknown
	r.ops[id] = op
	r.active = false
	return nil
}

func officeBridgeFixture(router http.Handler) (*OfficeAgentService, *officeBridgeTestRepo) {
	started := time.Now()
	lease := OfficeRunLease{Run: OfficeRun{ID: uuid.NewString(), UserID: 42, ConversationID: uuid.NewString(),
		Model: "public-model", StartedAt: &started, Status: OfficeRunRunning,
		Limits: OfficeRunLimits{MaxModelCalls: 8, MaxToolCalls: 16, MaxOutputTokens: 8192, MaxRunSeconds: 600}},
		Owner: "test-worker", Token: uuid.NewString(), Epoch: 1, Until: time.Now().Add(time.Minute)}
	repo := &officeBridgeTestRepo{lease: lease, active: true, ops: make(map[string]OfficeOperation)}
	svc := &OfficeAgentService{repo: repo, cfg: config.OfficeAgentConfig{Enabled: true, WorkerToken: strings.Repeat("test", 8),
		MaxConcurrentCalls: 1, MaxModelCalls: 8, MaxToolCalls: 16, MaxOutputTokens: 8192, MaxRunSeconds: 600},
		modelSlots: make(chan struct{}, 1), authorize: func(context.Context, OfficeRun) (string, error) { return "private-key", nil }}
	svc.BindGatewayRouter(router)
	return svc, repo
}

func officeBridgeBody(content string) []byte {
	return []byte(fmt.Sprintf(`{"model":"public-model","messages":[{"role":"user","content":%q}],"max_tokens":1024}`, content))
}

func TestOfficeBridgeUsesExistingRouterAndReplaysInboxWithoutSecondCall(t *testing.T) {
	var calls atomic.Int32
	svc, repo := officeBridgeFixture(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		require.Equal(t, "/v1/chat/completions", r.URL.Path)
		require.Equal(t, "Bearer private-key", r.Header.Get("Authorization"))
		require.Equal(t, "127.0.0.1:0", r.RemoteAddr)
		require.True(t, strings.HasPrefix(r.Context().Value(ctxkey.ClientRequestID).(string), "office-"))
		require.Empty(t, r.Header.Get("X-Office-Lease"))
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, `{"choices":[{"message":{"role":"assistant","content":"ok"}}],"usage":{"total_tokens":9}}`)
	}))
	ctx := context.Background()
	first, err := svc.InvokeModel(ctx, repo.lease, "model-1", officeBridgeBody("hello"))
	require.NoError(t, err)
	require.Equal(t, 200, first.Status)
	second, err := svc.InvokeModel(ctx, repo.lease, "model-1", officeBridgeBody("hello"))
	require.NoError(t, err)
	require.Equal(t, first, second)
	require.Equal(t, int32(1), calls.Load())
	_, err = svc.InvokeModel(ctx, repo.lease, "model-1", officeBridgeBody("changed"))
	require.ErrorIs(t, err, ErrOfficeConflict)
	require.Equal(t, int32(1), calls.Load())
}

func TestOfficeBridgeInFlightDuplicateCannotReachSupplier(t *testing.T) {
	entered, release := make(chan struct{}), make(chan struct{})
	var calls atomic.Int32
	svc, repo := officeBridgeFixture(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		close(entered)
		<-release
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, `{"choices":[]}`)
	}))
	done := make(chan error, 1)
	go func() {
		_, err := svc.InvokeModel(context.Background(), repo.lease, "model-1", officeBridgeBody("hello"))
		done <- err
	}()
	<-entered
	_, err := svc.InvokeModel(context.Background(), repo.lease, "model-1", officeBridgeBody("hello"))
	require.ErrorIs(t, err, ErrOfficeResultUnknown)
	close(release)
	require.NoError(t, <-done)
	require.Equal(t, int32(1), calls.Load())
}

func TestOfficeBridgeTruncatedOrOversizedResponseStopsReplay(t *testing.T) {
	for _, body := range []string{
		`data: {"choices":[{"delta":{"content":"literal data: [DONE]"}}]}` + "\n\n",
		strings.Repeat("x", officeMaxModelResponse+1),
	} {
		svc, repo := officeBridgeFixture(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Content-Type", "text/event-stream")
			_, _ = io.WriteString(w, body)
		}))
		_, err := svc.InvokeModel(context.Background(), repo.lease, "model-1", officeBridgeBody("hello"))
		require.ErrorIs(t, err, ErrOfficeResultUnknown)
		require.Equal(t, OfficeOperationUnknown, repo.ops["model-1"].State)
	}
}

func TestOfficeBridgeCompleteSSEAndLatePaidResponseAreRetained(t *testing.T) {
	var repo *officeBridgeTestRepo
	svc, r := officeBridgeFixture(http.HandlerFunc(func(w http.ResponseWriter, request *http.Request) {
		repo.mu.Lock()
		repo.active = false // Cancel the run while supplier completes.
		repo.mu.Unlock()
		w.Header().Set("Content-Type", "text/event-stream")
		_, _ = io.WriteString(w, "data: {\"choices\":[]}\r\n\r\ndata: [DONE]\r\n\r\n")
	}))
	repo = r
	reply, err := svc.InvokeModel(context.Background(), repo.lease, "model-1", officeBridgeBody("hello"))
	require.NoError(t, err)
	require.Contains(t, string(reply.Body), "[DONE]")
	require.Equal(t, OfficeOperationCompleted, repo.ops["model-1"].State)
	require.False(t, repo.active) // Inbox deposit cannot resurrect the run.
}

func TestOfficeLeaseCapabilityIsBoundToRunEpochAndExpires(t *testing.T) {
	svc, repo := officeBridgeFixture(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {}))
	token := svc.leaseCredential(repo.lease)
	lease, err := svc.AuthenticateLease(context.Background(), repo.lease.Run.ID, "1", token)
	require.NoError(t, err)
	require.Equal(t, repo.lease.Token, lease.Token)
	for _, v := range []struct{ id, epoch, token string }{
		{uuid.NewString(), "1", token}, {repo.lease.Run.ID, "2", token}, {repo.lease.Run.ID, "1", token + "x"},
	} {
		_, err := svc.AuthenticateLease(context.Background(), v.id, v.epoch, v.token)
		require.Error(t, err)
	}
	repo.active = false
	_, err = svc.AuthenticateLease(context.Background(), repo.lease.Run.ID, "1", token)
	require.ErrorIs(t, err, ErrOfficeLeaseLost)
	_, err = svc.AuthenticateResultLease(context.Background(), repo.lease.Run.ID, "1", token)
	require.NoError(t, err) // Only repository dispatch identity can accept a result.
	old := time.Now().Add(-time.Hour)
	repo.lease.Run.StartedAt = &old
	_, err = svc.AuthenticateLease(context.Background(), repo.lease.Run.ID, "1", svc.leaseCredential(repo.lease))
	require.ErrorIs(t, err, ErrOfficeUnauthorized)
}

func TestOfficeModelRequestCannotEscapeModelToolsOrBudget(t *testing.T) {
	run := OfficeRun{Model: "public-model", Limits: OfficeRunLimits{MaxOutputTokens: 1000}}
	for _, body := range []string{
		`{"model":"other","messages":[{"role":"user","content":"hi"}],"max_tokens":1}`,
		`{"model":"public-model","messages":[{"role":"user","content":"hi"}]}`,
		`{"model":"public-model","messages":[{"role":"user","content":"hi"}],"max_tokens":1001}`,
		`{"model":"public-model","messages":[{"role":"user","content":"hi"}],"max_tokens":10,"base_url":"http://metadata"}`,
		`{"model":"public-model","messages":[{"role":"user","content":"hi"}],"max_tokens":10,"tools":[{"type":"function","function":{"name":"shell"}}]}`,
		`{"model":"public-model","messages":[{"role":"user","content":[{"type":"image_url","image_url":{"url":"http://localhost"}}]}],"max_tokens":10}`,
	} {
		_, err := normalizeOfficeModelRequest([]byte(body), run)
		require.Error(t, err, body)
	}
	require.True(t, officeModelID("模型-办公"))
	require.False(t, officeModelID("model\n"))
}

func TestOfficeToolCanonicalizationPreservesLargeIntegersAndRechecksPolicy(t *testing.T) {
	svc, repo := officeBridgeFixture(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {}))
	op, execute, err := svc.PrepareTool(context.Background(), repo.lease, "tool-1", json.RawMessage(`{"id":9007199254740993}`))
	require.NoError(t, err)
	require.True(t, execute)
	require.Contains(t, string(op.Input), "9007199254740993")
	svc.authorize = func(context.Context, OfficeRun) (string, error) { return "", ErrOfficeRouteChanged }
	_, _, err = svc.PrepareTool(context.Background(), repo.lease, "tool-2", json.RawMessage(`{}`))
	require.ErrorIs(t, err, ErrOfficeRouteChanged)
}

func TestOfficeChatModelCatalogUsesChatEndpointWithoutChangingDesktop(t *testing.T) {
	accounts := &desktopModelAccounts{accounts: []Account{desktopModelAccount(PlatformOpenAI, map[string]any{"target": "target"})}}
	routes := compositeRouteRepoStub{routes: []CompositeModelRoute{
		{ID: 1, GroupID: 12, Enabled: true, PublicModel: "chat-only", MatchType: CompositeRouteMatchExact, Endpoint: CompositeRouteEndpointChatCompletions, TargetPlatform: PlatformOpenAI, UpstreamModel: "target"},
		{ID: 2, GroupID: 12, Enabled: true, PublicModel: "responses-only", MatchType: CompositeRouteMatchExact, Endpoint: CompositeRouteEndpointResponses, TargetPlatform: PlatformOpenAI, UpstreamModel: "target"},
	}}
	group := &Group{ID: 12, Platform: PlatformComposite}
	chat, err := NewWorkbenchChatModelCatalog(accounts, routes, nil).ModelsForGroup(context.Background(), group)
	require.NoError(t, err)
	require.Contains(t, chat, "chat-only")
	require.NotContains(t, chat, "responses-only")
	desktop, err := NewDesktopAgentModelCatalog(accounts, routes).ModelsForGroup(context.Background(), group)
	require.NoError(t, err)
	require.Contains(t, desktop, "responses-only")
	require.NotContains(t, desktop, "chat-only")
}

func TestOfficeBridgeCapacityDoesNotDispatchAnotherOperation(t *testing.T) {
	svc, repo := officeBridgeFixture(http.HandlerFunc(func(http.ResponseWriter, *http.Request) { panic("must not dispatch") }))
	svc.modelSlots <- struct{}{}
	_, err := svc.InvokeModel(context.Background(), repo.lease, "model-1", officeBridgeBody("hello"))
	require.True(t, errors.Is(err, ErrOfficeCapacity))
	require.Equal(t, OfficeOperationPrepared, repo.ops["model-1"].State)
}

func TestOfficeMessagesEnforcesOwnerBeforeConversationLookup(t *testing.T) {
	svc, repo := officeBridgeFixture(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {}))
	_, err := svc.Messages(context.Background(), 43, repo.lease.Run.ID, 0)
	require.ErrorIs(t, err, ErrOfficeNotFound)
	require.Zero(t, repo.messageReads)
	rows, err := svc.Messages(context.Background(), 42, repo.lease.Run.ID, 0)
	require.NoError(t, err)
	require.Len(t, rows, 1)
	require.Equal(t, 1, repo.messageReads)
}

func TestOfficeFinishDoesNotPersistEmptyFailureReply(t *testing.T) {
	svc, repo := officeBridgeFixture(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {}))
	_, err := svc.Finish(context.Background(), repo.lease, OfficeRunFailed, "", "TEST_ERROR", "failed before a reply")
	require.NoError(t, err)
	require.Nil(t, repo.finishedContent)
	_, err = svc.Finish(context.Background(), repo.lease, OfficeRunCompleted, " ", "", "")
	require.ErrorIs(t, err, ErrOfficeInvalid)
}
