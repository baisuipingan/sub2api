package handler

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/config"
	"github.com/Wei-Shaw/sub2api/internal/server/middleware"
	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
)

type officeHandlerRepo struct {
	service.OfficeAgentRepository
	claims int
	userID int64
	lease  *service.OfficeRunLease
}

func (r *officeHandlerRepo) ClaimRun(context.Context, string, time.Duration) (*service.OfficeRunLease, error) {
	r.claims++
	return r.lease, nil
}
func (r *officeHandlerRepo) GetRun(_ context.Context, userID int64, id string) (*service.OfficeRun, error) {
	r.userID = userID
	if r.lease != nil && r.lease.Run.UserID == userID && r.lease.Run.ID == id {
		return &r.lease.Run, nil
	}
	if userID != 7 {
		return nil, service.ErrOfficeNotFound
	}
	return &service.OfficeRun{ID: id, UserID: userID, ConversationID: uuid.NewString()}, nil
}
func (r *officeHandlerRepo) CheckLease(context.Context, service.OfficeRunLease) error {
	return nil
}
func (r *officeHandlerRepo) ListMessages(_ context.Context, userID int64, _ string, _ int64, _ int) ([]service.OfficeMessage, error) {
	return []service.OfficeMessage{{Role: "assistant", Content: json.RawMessage(`"stored reply"`)}}, nil
}

func officeHandlerFixture(t *testing.T, enabled, authenticated bool) (*gin.Engine, *officeHandlerRepo, string) {
	t.Helper()
	gin.SetMode(gin.TestMode)
	r := gin.New()
	repo := &officeHandlerRepo{}
	token := strings.Repeat("office-test-", 4)
	cfg := &config.Config{Server: config.ServerConfig{MaxRequestBodySize: 512 << 10}, Gateway: config.GatewayConfig{MaxBodySize: 640 << 10},
		OfficeAgent: config.OfficeAgentConfig{Enabled: enabled, WorkerToken: token,
			MaxConcurrentCalls: 1, MaxModelCalls: 8, MaxToolCalls: 16, MaxOutputTokens: 8192, MaxRunSeconds: 600}}
	svc := service.NewOfficeAgentService(repo, cfg, nil, nil, nil, nil, nil, nil)
	svc.BindGatewayRouter(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {}))
	h := NewOfficeAgentHandler(svc)
	r.Use(h.Protect)
	r.Use(func(c *gin.Context) {
		if authenticated {
			c.Set(string(middleware.ContextKeyUser), middleware.AuthSubject{UserID: 7})
		}
	})
	r.POST("/claim", h.Claim)
	r.POST("/runs", h.Submit)
	r.GET("/runs/:id", h.Get)
	r.GET("/runs/:id/messages", h.Messages)
	r.GET("/runs/:id/events", h.Events)
	r.POST("/runs/:id/model", h.Model)
	return r, repo, token
}

func TestOfficeModelIngressAppliesGatewaySizeWithValidLease(t *testing.T) {
	r, repo, workerToken := officeHandlerFixture(t, true, false)
	repo.lease = &service.OfficeRunLease{
		Run:   service.OfficeRun{ID: uuid.NewString(), UserID: 7, ConversationID: uuid.NewString(), Model: "test-model", Limits: service.OfficeRunLimits{MaxOutputTokens: 1024}},
		Owner: "worker-1", Token: uuid.NewString(), Epoch: 1, Until: time.Now().Add(time.Minute),
	}
	claimed := officeHandlerRequest(r, "POST", "/claim", `{"worker_id":"worker-1"}`, workerToken)
	require.Equal(t, http.StatusOK, claimed.Code, claimed.Body.String())
	var response struct {
		Data service.OfficeWorkerManifest `json:"data"`
	}
	require.NoError(t, json.Unmarshal(claimed.Body.Bytes(), &response))
	call := func(size int, contentLengthKnown bool) *httptest.ResponseRecorder {
		body := `{"model":"test-model","messages":[{"role":"user","content":"` + strings.Repeat("x", size) + `"}],"max_tokens":512}`
		req := httptest.NewRequest(http.MethodPost, "/runs/"+repo.lease.Run.ID+"/model", strings.NewReader(body))
		if !contentLengthKnown {
			req.ContentLength = -1
		}
		req.Header.Set("Authorization", "Bearer "+response.Data.LeaseToken)
		req.Header.Set("X-Office-Lease", "1")
		req.Header.Set("X-Office-Operation-ID", "test-request")
		recorder := httptest.NewRecorder()
		r.ServeHTTP(recorder, req)
		return recorder
	}
	// Fixture lacks a configured provider. Route authorization is reached only
	// after the authenticated body has been read and normalized.
	admitted := call(320<<10, true)
	require.Equal(t, http.StatusConflict, admitted.Code, admitted.Body.String())
	require.Contains(t, admitted.Body.String(), "MODEL_ROUTE_CHANGED")
	for _, contentLengthKnown := range []bool{true, false} {
		rejected := call(512<<10, contentLengthKnown)
		require.Equal(t, http.StatusRequestEntityTooLarge, rejected.Code, rejected.Body.String())
		require.Contains(t, rejected.Body.String(), "MODEL_INPUT_TOO_LARGE")
		require.Empty(t, rejected.Header().Get("X-Office-Operation-State"))
	}
}

func officeHandlerRequest(r *gin.Engine, method, path, body, token string) *httptest.ResponseRecorder {
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(method, path, strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	r.ServeHTTP(rec, req)
	return rec
}

func TestOfficeHandlerDisabledAndWorkerCredentialsAreIndependentOfJWT(t *testing.T) {
	r, repo, token := officeHandlerFixture(t, false, true)
	rec := officeHandlerRequest(r, "POST", "/claim", `{"worker_id":"worker-1"}`, token)
	require.Equal(t, 404, rec.Code)
	require.Equal(t, "no-store", rec.Header().Get("Cache-Control"))
	require.Zero(t, repo.claims)
	r, repo, token = officeHandlerFixture(t, true, true)
	for _, bad := range []string{"", "browser-jwt", token + "bad"} {
		rec = officeHandlerRequest(r, "POST", "/claim", `{"worker_id":"worker-1"}`, bad)
		require.Equal(t, 401, rec.Code)
	}
	require.Zero(t, repo.claims)
	rec = officeHandlerRequest(r, "POST", "/claim", `{"worker_id":"worker-1"}`, token)
	require.Equal(t, 204, rec.Code)
	require.Equal(t, 1, repo.claims)
	// A bootstrap credential can only claim; it cannot impersonate a run lease.
	rec = officeHandlerRequest(r, "POST", "/runs/"+uuid.NewString()+"/model", `{}`, token)
	require.Equal(t, 401, rec.Code)
}

func TestOfficeHandlerRejectsAmbiguousOversizedAndEncodedInputs(t *testing.T) {
	r, repo, token := officeHandlerFixture(t, true, false)
	for _, body := range []string{`{}`, `{"worker_id":"worker-1","user_id":7}`, `{"worker_id":"worker-1"}{}`, `null`, `{"worker_id":"../worker"}`} {
		rec := officeHandlerRequest(r, "POST", "/claim", body, token)
		require.Equal(t, 400, rec.Code, body)
	}
	rec := officeHandlerRequest(r, "POST", "/claim", strings.Repeat("x", 1025), token)
	require.Equal(t, 413, rec.Code)
	req := httptest.NewRequest("POST", "/claim", strings.NewReader(`{"worker_id":"worker-1"}`))
	req.Header.Set("Content-Encoding", "gzip")
	req.Header.Set("Authorization", "Bearer "+token)
	rec = httptest.NewRecorder()
	r.ServeHTTP(rec, req)
	require.Equal(t, 400, rec.Code)
	require.Zero(t, repo.claims)
}

func TestOfficeHandlerReadsOnlyAuthenticatedUserAndStrictCursor(t *testing.T) {
	r, repo, _ := officeHandlerFixture(t, true, true)
	id := uuid.NewString()
	rec := officeHandlerRequest(r, "GET", "/runs/"+id+"/messages?after=0", "", "")
	require.Equal(t, 200, rec.Code)
	require.EqualValues(t, 7, repo.userID)
	require.Contains(t, rec.Body.String(), "stored reply")
	for _, query := range []string{"-1", "abc", "", "1&after=2", "9223372036854775808"} {
		rec = officeHandlerRequest(r, "GET", "/runs/"+id+"/events?after="+query, "", "")
		require.Equal(t, 400, rec.Code, query)
	}
	r, _, _ = officeHandlerFixture(t, true, false)
	rec = officeHandlerRequest(r, "GET", "/runs/"+id+"/messages", "", "")
	require.Equal(t, 401, rec.Code)
}

func TestOfficeHandlerUncertainResultsAreExplicitWithoutLeakingInternalError(t *testing.T) {
	for _, err := range []error{service.ErrOfficeResultUnknown, service.ErrOfficeOperationDispatched} {
		rec := httptest.NewRecorder()
		ctx, _ := gin.CreateTestContext(rec)
		require.True(t, officeHandlerError(ctx, err))
		require.Equal(t, 409, rec.Code)
		require.Contains(t, rec.Body.String(), `"needs_confirmation":true`)
		require.Contains(t, rec.Body.String(), `"retryable":false`)
	}
	rec := httptest.NewRecorder()
	ctx, _ := gin.CreateTestContext(rec)
	officeHandlerError(ctx, errors.New("private-key-and-database-credentials"))
	require.Equal(t, 500, rec.Code)
	require.NotContains(t, rec.Body.String(), "private-key")
}

func TestOfficeHandlerManagedAgentKeyFailureReasons(t *testing.T) {
	for _, tc := range []struct {
		err    error
		status int
		code   string
	}{
		{service.ErrAPIKeyExpired, http.StatusForbidden, "API_KEY_EXPIRED"},
		{service.ErrOfficeAgentKeyDisabled, http.StatusForbidden, "API_KEY_DISABLED"},
		{service.ErrAPIKeyQuotaExhausted, http.StatusTooManyRequests, "API_KEY_QUOTA_EXHAUSTED"},
		{service.ErrUserNotActive, http.StatusForbidden, "USER_INACTIVE"},
	} {
		t.Run(tc.code, func(t *testing.T) {
			rec := httptest.NewRecorder()
			ctx, _ := gin.CreateTestContext(rec)
			require.True(t, officeHandlerError(ctx, tc.err))
			require.Equal(t, tc.status, rec.Code)
			require.Contains(t, rec.Body.String(), `"code":"`+tc.code+`"`)
			require.Contains(t, rec.Body.String(), `"retryable":false`)
		})
	}
}

func TestOfficeModelReplyMarksOnlyConfirmedInboxResponses(t *testing.T) {
	for _, status := range []int{http.StatusOK, http.StatusBadRequest, http.StatusBadGateway} {
		recorder := httptest.NewRecorder()
		c, _ := gin.CreateTestContext(recorder)
		writeOfficeModelReply(c, &service.OfficeGatewayReply{Status: status, ContentType: "application/json", Body: []byte(`{"error":{"code":"fixture"}}`), RequestID: "office-fixture"})
		require.Equal(t, status, recorder.Code)
		require.Equal(t, "completed", recorder.Header().Get("X-Office-Operation-State"))
		require.Equal(t, "office-fixture", recorder.Header().Get("X-Client-Request-ID"))
	}
	recorder := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(recorder)
	officeHandlerError(c, service.ErrOfficeResultUnknown)
	require.Empty(t, recorder.Header().Get("X-Office-Operation-State"), "unknown outcomes never claim inbox completion")
}
