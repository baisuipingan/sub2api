//go:build integration

package repository

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
)

func officeTestAdmission(t *testing.T) (service.OfficeAgentRepository, service.OfficeRunAdmission) {
	t.Helper()
	ctx := context.Background()
	var userID, groupID, keyID int64
	require.NoError(t, integrationDB.QueryRowContext(ctx, `INSERT INTO users(email,password_hash) VALUES($1,'office-test') RETURNING id`, uuid.NewString()+"@office.test").Scan(&userID))
	require.NoError(t, integrationDB.QueryRowContext(ctx, `INSERT INTO groups(name) VALUES($1) RETURNING id`, "office-"+uuid.NewString()).Scan(&groupID))
	require.NoError(t, integrationDB.QueryRowContext(ctx, `INSERT INTO api_keys(user_id,group_id,key,name,key_type) VALUES($1,$2,$3,'office-test','system') RETURNING id`, userID, groupID, "sk-"+uuid.NewString()).Scan(&keyID))
	t.Cleanup(func() {
		// Summaries reference both users and messages without delete cascades.
		// Delete this fixture's derived records before their parent records;
		// relying on the users cascade leaves the entire DELETE rolled back.
		for _, table := range []string{"office_agent_context_summaries", "office_agent_conversations", "users"} {
			column := "user_id"
			if table == "users" {
				column = "id"
			}
			_, err := integrationDB.ExecContext(context.Background(), `DELETE FROM `+table+` WHERE `+column+`=$1`, userID)
			require.NoError(t, err, "clean office fixture %s for user %d", table, userID)
		}
		_, err := integrationDB.ExecContext(context.Background(), `DELETE FROM groups WHERE id=$1`, groupID)
		require.NoError(t, err, "clean office fixture group %d", groupID)
	})
	repo := NewOfficeAgentRepository(integrationDB)
	conversationID := uuid.NewString()
	_, err := repo.CreateConversation(ctx, userID, conversationID, "办公对话")
	require.NoError(t, err)
	return repo, service.OfficeRunAdmission{UserID: userID, ConversationID: conversationID, RunID: uuid.NewString(), IdempotencyKey: uuid.NewString(), RequestHash: strings.Repeat("a", 64), APIKeyID: keyID, GroupID: groupID, Model: "public-model", UserContent: json.RawMessage(`{"text":"生成一份中文文档"}`), Limits: service.OfficeRunLimits{MaxModelCalls: 2, MaxToolCalls: 2, MaxOutputTokens: 1024, MaxRunSeconds: 60}}
}

func officeAdmitAndClaim(t *testing.T) (service.OfficeAgentRepository, service.OfficeRunAdmission, *service.OfficeRunLease) {
	t.Helper()
	repo, in := officeTestAdmission(t)
	_, created, err := repo.AdmitRun(context.Background(), in)
	require.NoError(t, err)
	require.True(t, created)
	lease, err := repo.ClaimRun(context.Background(), "test-worker", time.Minute)
	require.NoError(t, err)
	require.NotNil(t, lease)
	require.Equal(t, in.RunID, lease.Run.ID)
	return repo, in, lease
}

func officePrepared(t *testing.T, repo service.OfficeAgentRepository, lease service.OfficeRunLease, id string) *service.OfficeOperation {
	t.Helper()
	op, created, err := repo.PrepareOperation(context.Background(), lease, service.OfficeOperationPrepare{ID: id, Kind: service.OfficeOperationModel, InputHash: strings.Repeat("b", 64), Input: json.RawMessage(`{"model":"public-model","prompt":"hello"}`), RequestID: uuid.NewString()})
	require.NoError(t, err)
	require.True(t, created)
	return op
}

func TestOfficeAgentAdmissionAtomicIdempotentAndUserScoped(t *testing.T) {
	ctx := context.Background()
	repo, in := officeTestAdmission(t)
	var created atomic.Int32
	var wg sync.WaitGroup
	failures := make(chan error, 8)
	for i := 0; i < 8; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			run, isNew, err := repo.AdmitRun(ctx, in)
			if err != nil {
				failures <- err
				return
			}
			if run.ID != in.RunID {
				failures <- fmt.Errorf("different run %s", run.ID)
			}
			if isNew {
				created.Add(1)
			}
		}()
	}
	wg.Wait()
	close(failures)
	for err := range failures {
		require.NoError(t, err)
	}
	require.EqualValues(t, 1, created.Load())
	messages, err := repo.ListMessages(ctx, in.UserID, in.ConversationID, 0, 20)
	require.NoError(t, err)
	require.Len(t, messages, 1)
	var outbox int
	require.NoError(t, integrationDB.QueryRowContext(ctx, `SELECT COUNT(*) FROM office_agent_run_outbox WHERE run_id=$1`, in.RunID).Scan(&outbox))
	require.Equal(t, 1, outbox)
	changed := in
	changed.RequestHash = strings.Repeat("c", 64)
	_, _, err = repo.AdmitRun(ctx, changed)
	require.ErrorIs(t, err, service.ErrOfficeConflict)
	_, err = repo.GetRun(ctx, in.UserID+999, in.RunID)
	require.ErrorIs(t, err, service.ErrOfficeNotFound)
	_, err = repo.ListMessages(ctx, in.UserID+999, in.ConversationID, 0, 20)
	require.ErrorIs(t, err, service.ErrOfficeNotFound)
	_, err = repo.ListEvents(ctx, in.UserID+999, in.RunID, 0, 20)
	require.ErrorIs(t, err, service.ErrOfficeNotFound)
	second := in
	second.RunID = uuid.NewString()
	second.IdempotencyKey = uuid.NewString()
	_, _, err = repo.AdmitRun(ctx, second)
	require.ErrorIs(t, err, service.ErrOfficeActiveRun)
	messages, err = repo.ListMessages(ctx, in.UserID, in.ConversationID, 0, 20)
	require.NoError(t, err)
	require.Len(t, messages, 1, "failed admission must roll back message/sequence/outbox")
	second.ConversationID = uuid.NewString()
	_, err = repo.CreateConversation(ctx, in.UserID, second.ConversationID, "第二个会话")
	require.NoError(t, err)
	_, _, err = repo.AdmitRun(ctx, second)
	require.ErrorIs(t, err, service.ErrOfficeActiveRun, "the pilot cap is per user, not just per conversation")
}

func TestOfficeAgentRunDeadlineCannotBeExtendedByHeartbeatOrReclaim(t *testing.T) {
	ctx := context.Background()
	repo, in, lease := officeAdmitAndClaim(t)
	_, err := integrationDB.ExecContext(ctx, `UPDATE office_agent_runs SET started_at=clock_timestamp()-INTERVAL '2 minutes' WHERE id=$1`, in.RunID)
	require.NoError(t, err)
	require.ErrorIs(t, repo.CheckLease(ctx, *lease), service.ErrOfficeLeaseLost)
	_, err = repo.RenewLease(ctx, *lease, time.Minute)
	require.ErrorIs(t, err, service.ErrOfficeLeaseLost)
	_, _, err = repo.PrepareOperation(ctx, *lease, service.OfficeOperationPrepare{ID: "too-late", Kind: service.OfficeOperationModel, InputHash: strings.Repeat("b", 64), Input: json.RawMessage(`{}`), RequestID: uuid.NewString()})
	require.ErrorIs(t, err, service.ErrOfficeLeaseLost)
	_, err = integrationDB.ExecContext(ctx, `UPDATE office_agent_runs SET lease_until=clock_timestamp()-INTERVAL '1 second' WHERE id=$1`, in.RunID)
	require.NoError(t, err)
	next, err := repo.ClaimRun(ctx, "new-worker", time.Minute)
	require.NoError(t, err)
	require.Nil(t, next)
	run, err := repo.GetRun(ctx, in.UserID, in.RunID)
	require.NoError(t, err)
	require.Equal(t, service.OfficeRunFailed, run.Status)
	require.Equal(t, "RUN_TIME_LIMIT", run.ErrorCode)
	require.NotNil(t, run.FinishedAt)
}

func TestOfficeAgentConcurrentDifferentAdmissionHasOneWinner(t *testing.T) {
	ctx := context.Background()
	repo, in := officeTestAdmission(t)
	inputs := []service.OfficeRunAdmission{in, in}
	inputs[1].RunID = uuid.NewString()
	inputs[1].IdempotencyKey = uuid.NewString()
	var wg sync.WaitGroup
	results := make(chan error, 2)
	for _, input := range inputs {
		wg.Add(1)
		go func(input service.OfficeRunAdmission) {
			defer wg.Done()
			_, _, err := repo.AdmitRun(ctx, input)
			results <- err
		}(input)
	}
	wg.Wait()
	close(results)
	success, blocked := 0, 0
	for err := range results {
		if err == nil {
			success++
		} else {
			require.ErrorIs(t, err, service.ErrOfficeActiveRun)
			blocked++
		}
	}
	require.Equal(t, 1, success)
	require.Equal(t, 1, blocked)
}

func TestOfficeAgentOperationInboxPreventsRedispatchAndChargesOncePerIdentity(t *testing.T) {
	ctx := context.Background()
	repo, in, lease := officeAdmitAndClaim(t)
	op := officePrepared(t, repo, *lease, "model:one")
	replayed, created, err := repo.PrepareOperation(ctx, *lease, service.OfficeOperationPrepare{ID: op.ID, Kind: op.Kind, InputHash: op.InputHash, Input: op.Input, RequestID: op.RequestID})
	require.NoError(t, err)
	require.False(t, created)
	require.Equal(t, op.RequestID, replayed.RequestID)
	_, _, err = repo.PrepareOperation(ctx, *lease, service.OfficeOperationPrepare{ID: op.ID, Kind: op.Kind, InputHash: op.InputHash, Input: json.RawMessage(`{"different":true}`), RequestID: op.RequestID})
	require.ErrorIs(t, err, service.ErrOfficeConflict)
	_, err = repo.DispatchOperation(ctx, *lease, op.ID)
	require.NoError(t, err)
	_, err = repo.DispatchOperation(ctx, *lease, op.ID)
	require.ErrorIs(t, err, service.ErrOfficeOperationDispatched)
	_, err = repo.FinishRun(ctx, *lease, service.OfficeRunCompleted, json.RawMessage(`{"text":"done"}`), "", "")
	require.ErrorIs(t, err, service.ErrOfficeResultUnknown)
	result := service.OfficeOperationResult{HTTPStatus: 200, Output: json.RawMessage(`{"text":"model output"}`)}
	stored, err := repo.RecordOperationResult(ctx, *lease, op.ID, result)
	require.NoError(t, err)
	require.Equal(t, service.OfficeOperationCompleted, stored.State)
	_, err = repo.RecordOperationResult(ctx, *lease, op.ID, result)
	require.NoError(t, err)
	result.Output = json.RawMessage(`{"text":"different response"}`)
	_, err = repo.RecordOperationResult(ctx, *lease, op.ID, result)
	require.ErrorIs(t, err, service.ErrOfficeConflict)
	finished, err := repo.FinishRun(ctx, *lease, service.OfficeRunCompleted, json.RawMessage(`{"text":"done"}`), "", "")
	require.NoError(t, err)
	require.Equal(t, service.OfficeRunCompleted, finished.Status)
	require.Equal(t, 1, finished.ModelCalls)
	messages, err := repo.ListMessages(ctx, in.UserID, in.ConversationID, 0, 20)
	require.NoError(t, err)
	require.Len(t, messages, 2)
	require.ErrorIs(t, repo.CheckLease(ctx, *lease), service.ErrOfficeLeaseLost)
}

func TestOfficeAgentPreparedBudgetIsAtomic(t *testing.T) {
	ctx := context.Background()
	repo, in := officeTestAdmission(t)
	in.Limits.MaxModelCalls = 1
	_, _, err := repo.AdmitRun(ctx, in)
	require.NoError(t, err)
	lease, err := repo.ClaimRun(ctx, "budget-worker", time.Minute)
	require.NoError(t, err)
	require.NotNil(t, lease)
	var wg sync.WaitGroup
	results := make(chan error, 8)
	for i := 0; i < 8; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			_, _, err := repo.PrepareOperation(ctx, *lease, service.OfficeOperationPrepare{ID: fmt.Sprintf("op:%d", i), Kind: service.OfficeOperationModel, InputHash: strings.Repeat("b", 64), Input: json.RawMessage(`{}`), RequestID: uuid.NewString()})
			results <- err
		}(i)
	}
	wg.Wait()
	close(results)
	winners := 0
	for err := range results {
		if err == nil {
			winners++
		} else {
			require.ErrorIs(t, err, service.ErrOfficeBudgetExceeded)
		}
	}
	require.Equal(t, 1, winners)
	run, err := repo.GetRun(ctx, in.UserID, in.RunID)
	require.NoError(t, err)
	require.Equal(t, 1, run.ModelCalls)
}

func TestOfficeAgentFencingRecoveryAndCompletedOperationGate(t *testing.T) {
	ctx := context.Background()
	repo, in, old := officeAdmitAndClaim(t)
	op := officePrepared(t, repo, *old, "prepared")
	_, err := integrationDB.ExecContext(ctx, `UPDATE office_agent_runs SET lease_until=NOW()-INTERVAL '1 second' WHERE id=$1`, in.RunID)
	require.NoError(t, err)
	next, err := repo.ClaimRun(ctx, "new-worker", time.Minute)
	require.NoError(t, err)
	require.NotNil(t, next)
	require.Greater(t, next.Epoch, old.Epoch)
	_, err = repo.DispatchOperation(ctx, *old, op.ID)
	require.ErrorIs(t, err, service.ErrOfficeLeaseLost)
	_, err = repo.AppendEvent(ctx, *old, "late", "progress", json.RawMessage(`{}`))
	require.ErrorIs(t, err, service.ErrOfficeLeaseLost)
	_, err = repo.DispatchOperation(ctx, *next, op.ID)
	require.NoError(t, err)
	_, err = repo.RecordOperationResult(ctx, *next, op.ID, service.OfficeOperationResult{HTTPStatus: 200, Output: json.RawMessage(`{}`)})
	require.NoError(t, err)
	_, err = integrationDB.ExecContext(ctx, `UPDATE office_agent_runs SET lease_until=NOW()-INTERVAL '1 second' WHERE id=$1`, in.RunID)
	require.NoError(t, err)
	again, err := repo.ClaimRun(ctx, "third-worker", time.Minute)
	require.NoError(t, err)
	require.Nil(t, again)
	run, err := repo.GetRun(ctx, in.UserID, in.RunID)
	require.NoError(t, err)
	require.Equal(t, service.OfficeRunNeedsConfirmation, run.Status, "until checkpoint recovery is validated, even completed operations prevent automatic graph replay")
	_, err = repo.RenewLease(ctx, *next, time.Minute)
	require.ErrorIs(t, err, service.ErrOfficeLeaseLost)
}

func TestOfficeAgentCancelledOrUnknownRunAcceptsLateInboxOnly(t *testing.T) {
	for _, cancel := range []bool{true, false} {
		t.Run(fmt.Sprintf("cancel=%t", cancel), func(t *testing.T) {
			ctx := context.Background()
			repo, in, lease := officeAdmitAndClaim(t)
			op := officePrepared(t, repo, *lease, strings.Repeat("a", 128))
			_, err := repo.DispatchOperation(ctx, *lease, op.ID)
			require.NoError(t, err)
			want := service.OfficeRunNeedsConfirmation
			if cancel {
				want = service.OfficeRunCancelled
				_, err = repo.CancelRun(ctx, in.UserID, in.RunID)
			} else {
				err = repo.MarkOperationUnknown(ctx, *lease, op.ID, "provider acknowledgement was lost")
			}
			require.NoError(t, err)
			_, err = repo.RecordOperationResult(ctx, *lease, op.ID, service.OfficeOperationResult{HTTPStatus: 200, Output: json.RawMessage(`{"late":true}`)})
			require.NoError(t, err)
			run, err := repo.GetRun(ctx, in.UserID, in.RunID)
			require.NoError(t, err)
			require.Equal(t, want, run.Status)
			_, err = repo.FinishRun(ctx, *lease, service.OfficeRunCompleted, nil, "", "")
			require.ErrorIs(t, err, service.ErrOfficeLeaseLost)
			forged := *lease
			forged.Token = uuid.NewString()
			_, err = repo.RecordOperationResult(ctx, forged, op.ID, service.OfficeOperationResult{Output: json.RawMessage(`{}`)})
			require.ErrorIs(t, err, service.ErrOfficeLeaseLost)
		})
	}
}

func TestOfficeAgentEventDedupSequenceAndReservedSystemIDs(t *testing.T) {
	ctx := context.Background()
	repo, in, lease := officeAdmitAndClaim(t)
	var wg sync.WaitGroup
	events := make(chan *service.OfficeRunEvent, 8)
	errs := make(chan error, 8)
	for i := 0; i < 8; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			event, err := repo.AppendEvent(ctx, *lease, "token:one", "message.delta", json.RawMessage(`{"text":"好"}`))
			events <- event
			errs <- err
		}()
	}
	wg.Wait()
	close(events)
	close(errs)
	for err := range errs {
		require.NoError(t, err)
	}
	var seq int64
	for event := range events {
		if seq == 0 {
			seq = event.Sequence
		}
		require.Equal(t, seq, event.Sequence)
	}
	_, err := repo.AppendEvent(ctx, *lease, "token:one", "message.delta", json.RawMessage(`{"text":"不同"}`))
	require.ErrorIs(t, err, service.ErrOfficeConflict)
	_, err = repo.AppendEvent(ctx, *lease, "system:finished", "run.completed", json.RawMessage(`{}`))
	require.ErrorIs(t, err, service.ErrOfficeInvalid)
	page, err := repo.ListEvents(ctx, in.UserID, in.RunID, 0, 20)
	require.NoError(t, err)
	require.Len(t, page, 3)
	for i, event := range page {
		require.EqualValues(t, i+1, event.Sequence)
	}
	next, err := repo.ListEvents(ctx, in.UserID, in.RunID, page[1].Sequence, 20)
	require.NoError(t, err)
	require.Len(t, next, 1)
	_, err = repo.CancelRun(ctx, in.UserID, in.RunID)
	require.NoError(t, err)
	_, err = repo.AppendEvent(ctx, *lease, "after-cancel", "message.delta", json.RawMessage(`{}`))
	require.ErrorIs(t, err, service.ErrOfficeLeaseLost)
}

func TestOfficeAgentConcurrentClaimAndEventBudgetKeepCancellationAvailable(t *testing.T) {
	ctx := context.Background()
	repo, in := officeTestAdmission(t)
	_, _, err := repo.AdmitRun(ctx, in)
	require.NoError(t, err)
	var wg sync.WaitGroup
	claimed := make(chan *service.OfficeRunLease, 4)
	errs := make(chan error, 4)
	for i := 0; i < 4; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			lease, err := repo.ClaimRun(ctx, fmt.Sprintf("claim-%d", i), time.Minute)
			claimed <- lease
			errs <- err
		}(i)
	}
	wg.Wait()
	close(claimed)
	close(errs)
	for err := range errs {
		require.NoError(t, err)
	}
	var lease *service.OfficeRunLease
	winners := 0
	for next := range claimed {
		if next != nil {
			winners++
			lease = next
		}
	}
	require.Equal(t, 1, winners)
	_, err = integrationDB.ExecContext(ctx, `UPDATE office_agent_runs SET event_bytes=16711680,next_event_seq=9900 WHERE id=$1`, in.RunID)
	require.NoError(t, err)
	_, err = repo.AppendEvent(ctx, *lease, "extra", "progress", json.RawMessage(`{}`))
	require.ErrorIs(t, err, service.ErrOfficeBudgetExceeded)
	run, err := repo.CancelRun(ctx, in.UserID, in.RunID)
	require.NoError(t, err)
	require.Equal(t, service.OfficeRunCancelled, run.Status, "exhausting progress-event storage must never prevent cancellation")
}

func TestOfficeAgentCannotPublishSuccessWithPreparedOperation(t *testing.T) {
	ctx := context.Background()
	repo, _, lease := officeAdmitAndClaim(t)
	officePrepared(t, repo, *lease, "not-dispatched")
	_, err := repo.FinishRun(ctx, *lease, service.OfficeRunCompleted, json.RawMessage(`"done"`), "", "")
	require.ErrorIs(t, err, service.ErrOfficeConflict)
	run, err := repo.FinishRun(ctx, *lease, service.OfficeRunFailed, nil, "CAPACITY_UNAVAILABLE", "No operation was dispatched")
	require.NoError(t, err)
	require.Equal(t, service.OfficeRunFailed, run.Status)
}

func TestOfficeAgentLeaseExpiresWhileWaitingForRunLock(t *testing.T) {
	ctx := context.Background()
	repo, in, lease := officeAdmitAndClaim(t)
	_, err := integrationDB.ExecContext(ctx, `UPDATE office_agent_runs SET lease_until=clock_timestamp()+INTERVAL '250 milliseconds' WHERE id=$1`, in.RunID)
	require.NoError(t, err)
	locker, err := integrationDB.BeginTx(ctx, nil)
	require.NoError(t, err)
	defer locker.Rollback()
	var id string
	require.NoError(t, locker.QueryRowContext(ctx, `SELECT id::text FROM office_agent_runs WHERE id=$1 FOR UPDATE`, in.RunID).Scan(&id))
	checked := make(chan error, 1)
	go func() { checked <- repo.CheckLease(ctx, *lease) }()
	// The waiting check starts before expiry, but it may progress only after
	// the lock is released. A transaction-start NOW() check would pass here.
	time.Sleep(350 * time.Millisecond)
	require.NoError(t, locker.Commit())
	require.ErrorIs(t, <-checked, service.ErrOfficeLeaseLost)
}
