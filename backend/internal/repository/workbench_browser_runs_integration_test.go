//go:build integration

package repository

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/stretchr/testify/require"
)

func TestWorkbenchBrowserLeaseKeepsDispatchSingleAndOutsideWorkerQueue(t *testing.T) {
	ctx := context.Background()
	base, input := officeTestAdmission(t)
	repo := base.(*officeAgentRepository)
	input.BrowserDriven = true
	input.Mode = "chat"
	run, created, err := repo.AdmitRun(ctx, input)
	require.NoError(t, err)
	require.True(t, created)
	require.True(t, run.BrowserDriven)

	worker, err := repo.ClaimPreset(ctx, "legacy", "chat", time.Minute)
	require.NoError(t, err)
	require.Nil(t, worker)
	_, _, err = repo.ClaimBrowserStep(ctx, input.UserID+1000, input.RunID, time.Minute)
	require.ErrorIs(t, err, service.ErrOfficeNotFound)
	lease, _, err := repo.ClaimBrowserStep(ctx, input.UserID, input.RunID, time.Minute)
	require.NoError(t, err)
	_, _, err = repo.ClaimBrowserStep(ctx, input.UserID, input.RunID, time.Minute)
	require.ErrorIs(t, err, service.ErrOfficeStepBusy)

	op := officePrepared(t, repo, *lease, "browser:model:0")
	_, err = repo.DispatchOperation(ctx, *lease, op.ID)
	require.NoError(t, err)
	_, err = integrationDB.ExecContext(ctx, `UPDATE office_agent_runs SET lease_until=clock_timestamp()-INTERVAL '1 second' WHERE id=$1`, input.RunID)
	require.NoError(t, err)
	recovered, _, err := repo.ClaimBrowserStep(ctx, input.UserID, input.RunID, time.Minute)
	require.NoError(t, err)
	require.Greater(t, recovered.Epoch, lease.Epoch)
	require.NoError(t, repo.ResolveBrowserDispatch(ctx, *recovered, op.ID))

	_, final, err := repo.ClaimBrowserStep(ctx, input.UserID, input.RunID, time.Minute)
	require.NoError(t, err)
	require.Equal(t, service.OfficeRunFailed, final.Status)
	require.Equal(t, 1, final.ModelCalls)
	require.Equal(t, "OPERATION_RESULT_UNKNOWN", final.ErrorCode)
	_, err = repo.RecordOperationResult(ctx, *lease, op.ID, service.OfficeOperationResult{Output: json.RawMessage(`{"late":true}`)})
	require.NoError(t, err)
	journal, err := repo.BrowserOperations(ctx, input.UserID, input.RunID)
	require.NoError(t, err)
	require.Len(t, journal, 1)
	require.Equal(t, service.OfficeOperationCompleted, journal[0].State)
}
