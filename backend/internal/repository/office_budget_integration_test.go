//go:build integration

package repository

import (
	"context"
	"encoding/json"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
)

func officeBudgetLease(t *testing.T) (service.OfficeAgentRepository, service.OfficeRunAdmission, *service.OfficeRunLease) {
	t.Helper()
	repo, in := officeTestAdmission(t)
	in.Limits.MaxModelCalls, in.Limits.MaxChargeCredits = 8, 100
	_, _, err := repo.AdmitRun(context.Background(), in)
	require.NoError(t, err)
	lease, err := repo.ClaimRun(context.Background(), "budget-worker", time.Minute)
	require.NoError(t, err)
	require.NotNil(t, lease)
	require.Equal(t, in.RunID, lease.Run.ID)
	return repo, in, lease
}

func officeBudgetOperation(id string) service.OfficeOperationPrepare {
	return service.OfficeOperationPrepare{ID: id, Kind: service.OfficeOperationModel, InputHash: strings.Repeat("b", 64), Input: json.RawMessage(`{"model":"public-model","messages":[]}`), RequestID: "office-" + uuid.NewString()}
}

func TestOfficeBudgetOriginalLedgerGatesNewCallsButPreservesCompletedReplay(t *testing.T) {
	ctx := context.Background()
	repo, in, lease := officeBudgetLease(t)
	first, second := officeBudgetOperation("first"), officeBudgetOperation("second")
	_, created, err := repo.PrepareOperation(ctx, *lease, first)
	require.NoError(t, err)
	require.True(t, created)
	_, _, err = repo.PrepareOperation(ctx, *lease, second)
	require.ErrorIs(t, err, service.ErrOfficeCapacity, "parallel uncharged calls cannot multiply the permitted single-call overrun")
	_, created, err = repo.PrepareOperation(ctx, *lease, first)
	require.NoError(t, err)
	require.False(t, created)
	_, err = repo.DispatchOperation(ctx, *lease, first.ID)
	require.NoError(t, err)
	_, err = repo.RecordOperationResult(ctx, *lease, first.ID, service.OfficeOperationResult{HTTPStatus: 200, Output: json.RawMessage(`{"answer":"paid"}`)})
	require.NoError(t, err)
	_, _, err = repo.PrepareOperation(ctx, *lease, second)
	require.ErrorIs(t, err, service.ErrOfficeSettlementPending, "gateway usage writes asynchronously; missing settlement must not bypass the budget")
	var account, wrongKey int64
	require.NoError(t, integrationDB.QueryRowContext(ctx, `INSERT INTO accounts(name,platform,type) VALUES('budget-upstream','openai','apikey') RETURNING id`).Scan(&account))
	require.NoError(t, integrationDB.QueryRowContext(ctx, `INSERT INTO api_keys(user_id,group_id,key,name,key_type) VALUES($1,$2,$3,'budget-decoy','system') RETURNING id`, in.UserID, in.GroupID, "sk-"+uuid.NewString()).Scan(&wrongKey))
	t.Cleanup(func() {
		_, _ = integrationDB.ExecContext(context.Background(), `DELETE FROM accounts WHERE id=$1`, account)
	})
	insertUsage := func(key int64, request string, credits int) {
		t.Helper()
		_, err := integrationDB.ExecContext(ctx, `INSERT INTO usage_logs(user_id,api_key_id,account_id,request_id,model,input_tokens,output_tokens,charge_credits) VALUES($1,$2,$3,$4,'public-model',1,1,$5)`, in.UserID, key, account, "client:"+request, credits)
		require.NoError(t, err)
	}
	insertUsage(wrongKey, first.RequestID, 999999)
	_, _, err = repo.PrepareOperation(ctx, *lease, second)
	require.ErrorIs(t, err, service.ErrOfficeSettlementPending, "same request under another key does not settle this run")
	insertUsage(in.APIKeyID, first.RequestID, 40)
	_, created, err = repo.PrepareOperation(ctx, *lease, second)
	require.NoError(t, err)
	require.True(t, created, "wrong-key high charge must not exhaust this task")
	_, err = repo.DispatchOperation(ctx, *lease, second.ID)
	require.NoError(t, err)
	_, err = repo.RecordOperationResult(ctx, *lease, second.ID, service.OfficeOperationResult{HTTPStatus: 200, Output: json.RawMessage(`{"answer":"paid again"}`)})
	require.NoError(t, err)
	insertUsage(in.APIKeyID, second.RequestID, 70)
	_, _, err = repo.PrepareOperation(ctx, *lease, officeBudgetOperation("third"))
	require.ErrorIs(t, err, service.ErrOfficeBudgetExceeded)
	replayed, created, err := repo.PrepareOperation(ctx, *lease, first)
	require.NoError(t, err)
	require.False(t, created)
	require.Equal(t, service.OfficeOperationCompleted, replayed.State)
	require.JSONEq(t, `{"answer":"paid"}`, string(replayed.Result.Output))
	first.InputHash = strings.Repeat("c", 64)
	_, _, err = repo.PrepareOperation(ctx, *lease, first)
	require.ErrorIs(t, err, service.ErrOfficeConflict)
	run, err := repo.GetRun(ctx, in.UserID, in.RunID)
	require.NoError(t, err)
	require.Equal(t, 2, run.ModelCalls, "blocked attempts never consume model-call reservations")
	require.EqualValues(t, 100, run.Limits.MaxChargeCredits)
}

func TestOfficeBudgetAtomicSingleInFlightAdmissionAndUnbilledFailure(t *testing.T) {
	ctx := context.Background()
	repo, in, lease := officeBudgetLease(t)
	var admitted atomic.Int32
	var wg sync.WaitGroup
	errors := make(chan error, 4)
	for i := range 4 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			_, created, err := repo.PrepareOperation(ctx, *lease, officeBudgetOperation("parallel-"+string(rune('a'+i))))
			if created {
				admitted.Add(1)
			}
			if err != nil && err != service.ErrOfficeCapacity {
				errors <- err
			}
		}()
	}
	wg.Wait()
	close(errors)
	for err := range errors {
		require.NoError(t, err)
	}
	require.EqualValues(t, 1, admitted.Load())
	ops, err := repo.(service.OfficeChatRepository).ListOperations(ctx, in.UserID, in.RunID)
	require.NoError(t, err)
	require.Len(t, ops, 1)
	_, err = repo.DispatchOperation(ctx, *lease, ops[0].ID)
	require.NoError(t, err)
	_, err = repo.RecordOperationResult(ctx, *lease, ops[0].ID, service.OfficeOperationResult{HTTPStatus: 400, Output: json.RawMessage(`{"error":"invalid input"}`)})
	require.NoError(t, err)
	_, created, err := repo.PrepareOperation(ctx, *lease, officeBudgetOperation("after-failure"))
	require.NoError(t, err, "an unbilled rejected request must not wait forever for a nonexistent usage row")
	require.True(t, created)
}
