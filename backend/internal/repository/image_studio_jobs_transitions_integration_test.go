//go:build integration

package repository

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
	"unicode/utf8"

	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/stretchr/testify/require"
)

func studioTransitionClaim(t *testing.T, count int) (service.StudioJobRepository, service.StudioJobClaim) {
	t.Helper()
	ctx := context.Background()
	userID := galleryTestUser(t)
	repo := NewStudioJobRepository(integrationDB)
	t.Cleanup(func() {
		_, _ = integrationDB.ExecContext(ctx, `DELETE FROM image_studio_jobs WHERE user_id=$1`, userID)
	})
	_, err := repo.Create(ctx, service.StudioJobCreate{
		UserID: userID, APIKeyID: 1, TaskID: "transition-test", Digest: strings.Repeat("a", 64),
		Task:      json.RawMessage(`{"id":"transition-test","status":"running"}`),
		Spec:      service.StudioJobSpec{Protocol: "openai", Model: "gpt-image-2", ImageCount: count},
		CallCount: count, StorageEndpoint: "https://s3.test", StorageRegion: "auto", StorageBucket: "gallery",
	})
	require.NoError(t, err)
	claim, err := repo.Claim(ctx, "11111111-1111-4111-8111-111111111111")
	require.NoError(t, err)
	require.NotNil(t, claim)
	require.Equal(t, userID, claim.UserID)
	return repo, *claim
}

func TestStudioJobFailureTransitionsPersistWithoutRepeatingCalls(t *testing.T) {
	for _, tc := range []struct {
		name, code, billing   string
		dispatched, saving    bool
		cancelSiblings        bool
		rejectedWithoutOutput bool
	}{
		{name: "before dispatch", code: "INPUT_UNAVAILABLE", billing: "not_started"},
		{name: "gateway rejection", code: "GATEWAY_HTTP_403", billing: "not_started", dispatched: true},
		{name: "moderation business error", code: "MODERATION_BLOCKED", billing: "not_started", dispatched: true, cancelSiblings: true},
		{name: "empty result", code: "UPSTREAM_NO_IMAGE", billing: "not_started", dispatched: true},
		{name: "invalid response", code: "UPSTREAM_INVALID_RESPONSE", billing: "not_started", dispatched: true},
		{name: "provider-specific explicit rejection", code: "VENDOR_INVALID_INPUT", billing: "not_started", dispatched: true, rejectedWithoutOutput: true},
		{name: "paid image invalid", code: "INVALID_IMAGE_RESULT", billing: "pending", dispatched: true, saving: true, cancelSiblings: true},
		{name: "gallery quota", code: "GALLERY_QUOTA_EXCEEDED", billing: "pending", dispatched: true, saving: true, cancelSiblings: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			ctx := context.Background()
			repo, claim := studioTransitionClaim(t, 2)
			if tc.dispatched {
				require.NoError(t, repo.MarkDispatched(ctx, claim))
			}
			if tc.saving {
				require.NoError(t, repo.MarkSaving(ctx, claim, "/tmp/studio-test.json", "11111111-1111-4111-8111-111111111111"))
			}
			claim.RejectedWithoutOutput = tc.rejectedWithoutOutput
			require.NoError(t, repo.FailCall(ctx, claim, tc.code, "request rejected"))
			job, err := repo.Get(ctx, claim.UserID, claim.TaskID)
			require.NoError(t, err)
			require.Equal(t, service.StudioJobFailed, job.Calls[0].Status)
			require.Equal(t, tc.code, job.Calls[0].ErrorCode)
			require.Equal(t, "request rejected", job.Calls[0].ErrorMessage)
			require.Equal(t, tc.billing, job.Calls[0].BillingState)
			require.Empty(t, job.Calls[0].LeaseToken)
			require.Empty(t, job.Calls[0].SpoolPath)
			require.Equal(t, 1, job.Calls[0].Attempt)
			wantSibling := service.StudioJobQueued
			if tc.cancelSiblings {
				wantSibling = service.StudioJobCancelled
			}
			require.Equal(t, wantSibling, job.Calls[1].Status)
			// A stale worker cannot overwrite the terminal error or regain its lease.
			require.Error(t, repo.FailCall(ctx, claim, "OTHER_ERROR", "stale worker"))
			require.Error(t, repo.ExtendLease(ctx, claim))
		})
	}
}

func TestStudioFinishTimestampUsesCallCompletionInsteadOfScanTime(t *testing.T) {
	ctx := context.Background()
	repo, claim := studioTransitionClaim(t, 1)
	require.NoError(t, repo.MarkDispatched(ctx, claim))
	require.NoError(t, repo.FailCall(ctx, claim, "MODERATION_BLOCKED", "upstream rejected content"))
	// Simulate a recovery scan arriving much later. The admin end time must
	// remain the execution's end, while updated_at records reconciliation.
	_, err := integrationDB.ExecContext(ctx, `UPDATE image_studio_jobs SET created_at=NOW()-INTERVAL '1 minute'
        WHERE user_id=$1 AND task_id=$2`, claim.UserID, claim.TaskID)
	require.NoError(t, err)
	_, err = integrationDB.ExecContext(ctx, `UPDATE image_studio_job_calls SET updated_at=NOW()-INTERVAL '40 seconds'
        WHERE user_id=$1 AND task_id=$2`, claim.UserID, claim.TaskID)
	require.NoError(t, err)
	require.NoError(t, repo.Finalize(ctx, claim.UserID, claim.TaskID, service.StudioJobFailed,
		json.RawMessage(`{"id":"transition-test","status":"error","outputImages":[]}`)))
	var elapsed float64
	var sameEnd bool
	require.NoError(t, integrationDB.QueryRowContext(ctx, `SELECT EXTRACT(EPOCH FROM (j.updated_at-j.finished_at)),
        j.finished_at=c.updated_at FROM image_studio_jobs j JOIN image_studio_job_calls c USING(user_id,task_id)
        WHERE j.user_id=$1 AND j.task_id=$2`, claim.UserID, claim.TaskID).Scan(&elapsed, &sameEnd))
	require.True(t, sameEnd)
	require.Greater(t, elapsed, float64(39))
}

func TestStudioLongChineseErrorPersistsWithoutStalling(t *testing.T) {
	ctx := context.Background()
	repo, claim := studioTransitionClaim(t, 1)
	require.NoError(t, repo.MarkDispatched(ctx, claim))
	message := "[MODERATION_BLOCKED]: " + strings.Repeat("上游安全审核拒绝", 100)
	require.NoError(t, repo.FailCall(ctx, claim, "MODERATION_BLOCKED", message))
	job, err := repo.Get(ctx, claim.UserID, claim.TaskID)
	require.NoError(t, err)
	require.Equal(t, service.StudioJobFailed, job.Calls[0].Status)
	require.True(t, utf8.ValidString(job.Calls[0].ErrorMessage))
	require.LessOrEqual(t, len(job.Calls[0].ErrorMessage), 1000)
	require.Contains(t, job.Calls[0].ErrorMessage, "安全审核拒绝")
}

func TestStudioJobCompleteAndUnknownTerminalTransitions(t *testing.T) {
	ctx := context.Background()
	t.Run("saved output finalizes once", func(t *testing.T) {
		repo, claim := studioTransitionClaim(t, 1)
		require.NoError(t, repo.MarkDispatched(ctx, claim))
		require.Error(t, repo.Finalize(ctx, claim.UserID, claim.TaskID, service.StudioJobCompleted,
			json.RawMessage(`{"id":"transition-test","status":"done"}`)), "running calls cannot be finalized")
		require.NoError(t, repo.MarkSaving(ctx, claim, "/tmp/studio-test.json", "11111111-1111-4111-8111-111111111111"))
		require.NoError(t, repo.RetrySaving(ctx, claim, 0, "STORAGE_TEMPORARY"))
		claimPtr, err := repo.Claim(ctx, "11111111-1111-4111-8111-111111111111")
		require.NoError(t, err)
		require.NotNil(t, claimPtr)
		claim = *claimPtr
		require.NoError(t, repo.CompleteCall(ctx, claim, []string{"output-id"}, []string{"raw-id"}, "pending"))
		require.Error(t, repo.Finalize(ctx, claim.UserID, claim.TaskID, service.StudioJobCompleted,
			json.RawMessage(`{"id":"transition-test","status":"done"}`)), "pending billing cannot be finalized")
		require.NoError(t, repo.SetBillingState(ctx, claim, "recorded"))
		task := json.RawMessage(`{"id":"transition-test","status":"done","outputImages":["output-id"]}`)
		require.NoError(t, repo.Finalize(ctx, claim.UserID, claim.TaskID, service.StudioJobCompleted, task))
		job, err := repo.Get(ctx, claim.UserID, claim.TaskID)
		require.NoError(t, err)
		require.Equal(t, service.StudioJobCompleted, job.Status)
		require.Equal(t, "recorded", job.Calls[0].BillingState)
		require.Equal(t, []string{"output-id"}, job.Calls[0].OutputImageIDs)
		require.Equal(t, []string{"raw-id"}, job.Calls[0].RawImageIDs)
		require.Error(t, repo.CompleteCall(ctx, claim, nil, nil, "recorded"))
		require.Error(t, repo.Finalize(ctx, claim.UserID, claim.TaskID, service.StudioJobFailed, task))
	})
	t.Run("uncertain result cancels unsent siblings", func(t *testing.T) {
		repo, claim := studioTransitionClaim(t, 2)
		require.NoError(t, repo.MarkDispatched(ctx, claim))
		require.Error(t, repo.MarkUnknown(ctx, claim), "a live lease cannot be expired by recovery")
		require.NoError(t, repo.MarkUncertain(ctx, claim))
		job, err := repo.Get(ctx, claim.UserID, claim.TaskID)
		require.NoError(t, err)
		require.Equal(t, service.StudioJobUnknown, job.Calls[0].Status)
		require.Equal(t, "unknown", job.Calls[0].BillingState)
		require.Equal(t, service.StudioJobCancelled, job.Calls[1].Status)
		claimPtr, err := repo.Claim(ctx, "11111111-1111-4111-8111-111111111111")
		require.NoError(t, err)
		require.Nil(t, claimPtr, "an uncertain paid call must never be dispatched again")
	})
}
