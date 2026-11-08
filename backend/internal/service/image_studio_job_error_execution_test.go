//go:build unit

package service

import (
	"context"
	"encoding/json"
	"net/http"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

type studioBusinessKeyRepo struct {
	APIKeyRepository
	key *APIKey
}

func (r studioBusinessKeyRepo) GetByID(context.Context, int64) (*APIKey, error) { return r.key, nil }

type studioBusinessFailureRepo struct {
	*studioFinishRepo
	dispatches int
	rejected   bool
}

func (r *studioBusinessFailureRepo) MarkDispatched(context.Context, StudioJobClaim) error {
	r.dispatches++
	return nil
}
func (r *studioBusinessFailureRepo) FailCall(_ context.Context, claim StudioJobClaim, code, message string) error {
	r.rejected = claim.RejectedWithoutOutput
	call := &r.job.Calls[claim.Call.Ordinal]
	call.Status, call.ErrorCode, call.ErrorMessage = StudioJobFailed, code, message
	call.BillingState, call.UpdatedAt = "not_started", time.Now()
	return nil
}

func TestStudioBusinessErrorAfterKeepaliveFailsImmediatelyWithoutRedispatch(t *testing.T) {
	gallery, _, _, _ := galleryTestService(t)
	created := time.Now().Add(-time.Second)
	task, err := json.Marshal(map[string]any{"id": "blocked-generation", "status": "running", "createdAt": created.UnixMilli(), "inputImageIds": []string{}, "outputImages": []string{}})
	require.NoError(t, err)
	profile := ImageStudioModelProfile{Provider: "openai", Profile: "gpt-image-2"}
	job := StudioJob{UserID: 9, TaskID: "blocked-generation", APIKeyID: 11, Status: StudioJobGenerating,
		CreatedAt: created, Task: task, Spec: StudioJobSpec{Protocol: "openai", Model: "gpt-image-2", Params: json.RawMessage(`{}`), AdmissionProfiles: []ImageStudioModelProfile{profile}},
		Calls: []StudioJobCall{{Ordinal: 0, Status: StudioJobGenerating, BillingState: "not_started"}}}
	repo := &studioBusinessFailureRepo{studioFinishRepo: &studioFinishRepo{job: job}}
	group := &Group{ID: 14, Platform: PlatformOpenAI, Status: StatusActive, AllowImageGeneration: true}
	gateway := studioModelsGateway(&imageStudioModelsAccountRepo{accounts: []Account{studioModelsAccount(1, PlatformOpenAI, map[string]any{"gpt-image-2": "gpt-image-2"})}})
	requests := 0
	svc := &StudioJobService{repo: repo, gallery: gallery, spoolDir: t.TempDir(), owner: "11111111-1111-4111-8111-111111111111", gateway: gateway,
		keys: &APIKeyService{apiKeyRepo: studioBusinessKeyRepo{key: &APIKey{ID: 11, UserID: 9, Key: "test-only-key", Group: group}}},
		router: http.HandlerFunc(func(w http.ResponseWriter, request *http.Request) {
			requests++
			require.Equal(t, "/v1/images/generations", request.URL.Path)
			w.WriteHeader(http.StatusOK)
			_, writeErr := w.Write([]byte(" \n"))
			require.NoError(t, writeErr)
			_, writeErr = w.Write([]byte(`{"error":{"code":"moderation_blocked","message":"Rejected by the safety system. access_token=private-test-secret"}}`))
			require.NoError(t, writeErr)
		})}
	svc.executeStudioClaim(context.Background(), StudioJobClaim{StudioJob: job, Call: job.Calls[0]})
	require.Equal(t, 1, requests)
	require.Equal(t, 1, repo.dispatches)
	require.True(t, repo.rejected, "only a proven zero-output rejection is marked nonbillable")
	require.Equal(t, "MODERATION_BLOCKED", repo.job.Calls[0].ErrorCode)
	require.NotContains(t, repo.job.Calls[0].ErrorMessage, "private-test-secret")
	require.Equal(t, "not_started", repo.job.Calls[0].BillingState)
	require.Equal(t, StudioJobFailed, repo.job.Status)
	require.Equal(t, 1, repo.finalized, "the parent is finished in the same execution, without a periodic scan")
	require.Contains(t, string(repo.job.Task), "MODERATION_BLOCKED")
}
