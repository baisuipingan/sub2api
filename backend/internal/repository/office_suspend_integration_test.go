//go:build integration

package repository

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/stretchr/testify/require"
)

func TestOfficeSuspendFencesImmediatelyAndAcceptsOnlyOriginalLateResult(t *testing.T) {
	ctx := context.Background()
	repo, in, lease := officeAdmitAndClaim(t)
	op := officePrepared(t, repo, *lease, "running-step")
	_, err := repo.DispatchOperation(ctx, *lease, op.ID)
	require.NoError(t, err)
	wrong := *lease
	wrong.Run.UserID++
	_, err = repo.SuspendRun(ctx, wrong, "RUN_TIMEOUT", "safe server message")
	require.ErrorIs(t, err, service.ErrOfficeLeaseLost)
	run, err := repo.SuspendRun(ctx, *lease, "RUN_TIMEOUT", "safe server message")
	require.NoError(t, err)
	require.Equal(t, service.OfficeRunNeedsConfirmation, run.Status)
	require.ErrorIs(t, repo.CheckLease(ctx, *lease), service.ErrOfficeLeaseLost)
	stopped, err := repo.GetOperation(ctx, in.UserID, in.RunID, op.ID)
	require.NoError(t, err)
	require.Equal(t, service.OfficeOperationUnknown, stopped.State)
	require.Equal(t, lease.Token, stopped.DispatchToken)
	_, err = repo.RecordOperationResult(ctx, *lease, op.ID, service.OfficeOperationResult{Output: json.RawMessage(`{"late":"original"}`), HTTPStatus: 200})
	require.NoError(t, err, "original dispatched result can still settle without replaying or reviving the run")
	run, err = repo.GetRun(ctx, in.UserID, in.RunID)
	require.NoError(t, err)
	require.Equal(t, service.OfficeRunNeedsConfirmation, run.Status)
	_, err = repo.FinishRun(ctx, *lease, service.OfficeRunCompleted, json.RawMessage(`"cannot revive"`), "", "")
	require.ErrorIs(t, err, service.ErrOfficeLeaseLost)
	events, err := repo.ListEvents(ctx, in.UserID, in.RunID, 0, 100)
	require.NoError(t, err)
	count := 0
	for _, event := range events {
		if event.Type == "run.needs_confirmation" {
			count++
		}
	}
	require.Equal(t, 1, count)
	_, err = repo.CancelRun(ctx, in.UserID, in.RunID)
	require.NoError(t, err)
	_, err = repo.SuspendRun(ctx, *lease, "RUN_TIMEOUT", "cannot revive cancelled task")
	require.ErrorIs(t, err, service.ErrOfficeLeaseLost)
}

func TestOfficeSuspendAcceptsMatchingIdentityAfterTimeLimitWithoutNewCalls(t *testing.T) {
	ctx := context.Background()
	repo, in, lease := officeAdmitAndClaim(t)
	_, err := integrationDB.ExecContext(ctx, `UPDATE office_agent_runs SET started_at=NOW()-INTERVAL '65 seconds',lease_until=NOW()-INTERVAL '1 second' WHERE id=$1`, in.RunID)
	require.NoError(t, err)
	require.ErrorIs(t, repo.CheckLease(ctx, *lease), service.ErrOfficeLeaseLost)
	run, err := repo.SuspendRun(ctx, *lease, "RUN_TIMEOUT", "task stopped")
	require.NoError(t, err)
	require.Equal(t, service.OfficeRunNeedsConfirmation, run.Status)
}
