//go:build integration

package repository

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
)

func admitReplayFixture(t *testing.T, repo *officeAgentRepository, admission service.OfficeRunAdmission, regeneration, text string) *service.OfficeRunLease {
	t.Helper()
	ctx := context.Background()
	admission.Mode, admission.BrowserDriven = "chat", true
	admission.Parameters = json.RawMessage(`{"history_version":2,"protocol":"responses"}`)
	admission.RunID, admission.IdempotencyKey, admission.RegenerationOf = uuid.NewString(), uuid.NewString(), regeneration
	admission.UserContent, _ = json.Marshal(text)
	_, _, err := repo.AdmitRun(ctx, admission)
	require.NoError(t, err)
	lease, _, err := repo.ClaimBrowserStep(ctx, admission.UserID, admission.RunID, time.Minute)
	require.NoError(t, err)
	require.NotNil(t, lease)
	return lease
}

func finishReplayFixture(t *testing.T, repo *officeAgentRepository, lease *service.OfficeRunLease, text string) (service.OfficeOperationResult, *service.OfficeOperation) {
	t.Helper()
	ctx := context.Background()
	op := officePrepared(t, repo, *lease, "browser:model:0")
	_, err := repo.DispatchOperation(ctx, *lease, op.ID)
	require.NoError(t, err)
	result := service.OfficeOperationResult{Output: json.RawMessage(`{"native_reply":{"text":"saved model response"}}`), HTTPStatus: 200}
	_, err = repo.RecordOperationResult(ctx, *lease, op.ID, result)
	require.NoError(t, err)
	_, err = repo.AppendEvent(ctx, *lease, "delta", "assistant.delta", json.RawMessage(`{"text":"saved model response"}`))
	require.NoError(t, err)
	content, _ := json.Marshal(text)
	_, err = repo.FinishRun(ctx, *lease, service.OfficeRunCompleted, content, "", "")
	require.NoError(t, err)
	return result, op
}

func TestWorkbenchReplayPersistsCheckpointAndProtectsReferencedReceipts(t *testing.T) {
	ctx := context.Background()
	base, admission := officeTestAdmission(t)
	repo := base.(*officeAgentRepository)

	first := admitReplayFixture(t, repo, admission, "", "initial question")
	state, err := repo.BrowserReplayState(ctx, first.Run)
	require.NoError(t, err)
	require.Len(t, state.Turns, 1)
	require.False(t, state.Pinned)
	firstSequence := state.Turns[0].Sequence
	require.NoError(t, repo.PinBrowserReplay(ctx, *first, nil, firstSequence))
	state, err = repo.BrowserReplayState(ctx, first.Run)
	require.NoError(t, err)
	require.True(t, state.Pinned)
	require.Nil(t, state.Checkpoint)
	result, op := finishReplayFixture(t, repo, first, "first answer")

	second := admitReplayFixture(t, repo, admission, "", "follow-up")
	state, err = repo.BrowserReplayState(ctx, second.Run)
	require.NoError(t, err)
	require.Len(t, state.Turns, 2)
	require.Equal(t, first.Run.ID, state.Turns[0].RunID)
	require.True(t, state.Turns[0].NativeAvailable)
	require.NoError(t, repo.PinBrowserReplay(ctx, *second, nil, firstSequence))
	require.NoError(t, repo.ReleaseBrowserStep(ctx, *second))
	second, _, err = repo.ClaimBrowserStep(ctx, admission.UserID, second.Run.ID, time.Minute)
	require.NoError(t, err)
	state, err = repo.BrowserReplayState(ctx, second.Run)
	require.NoError(t, err)
	require.True(t, state.Pinned)
	require.Equal(t, firstSequence, state.NativeFrom)
	require.ErrorIs(t, repo.PinBrowserReplay(ctx, *second, nil, state.Turns[1].Sequence), service.ErrOfficeConflict)

	_, err = integrationDB.ExecContext(ctx, `UPDATE office_agent_runs SET finished_at=NOW()-INTERVAL '2 days' WHERE id=$1`, first.Run.ID)
	require.NoError(t, err)
	cutoff := time.Now().Add(-24 * time.Hour)
	require.NoError(t, repo.SweepWorkbenchPayloads(ctx, cutoff, cutoff, 20))
	stored, err := repo.GetOperation(ctx, admission.UserID, first.Run.ID, op.ID)
	require.NoError(t, err)
	require.JSONEq(t, string(result.Output), string(stored.Result.Output))
	var deltas int
	require.NoError(t, integrationDB.QueryRowContext(ctx, `SELECT COUNT(*) FROM office_agent_run_events
		WHERE run_id=$1 AND event_type='assistant.delta'`, first.Run.ID).Scan(&deltas))
	require.Zero(t, deltas)
	finishReplayFixture(t, repo, second, "second answer")

	third := admitReplayFixture(t, repo, admission, "", "another question")
	state, err = repo.BrowserReplayState(ctx, third.Run)
	require.NoError(t, err)
	require.Len(t, state.Turns, 3)
	checkpoint := service.OfficeReplayCheckpoint{BeforeSequence: state.Turns[1].Sequence, Content: json.RawMessage(`{"text":"first turn summarized"}`)}
	require.NoError(t, repo.PinBrowserReplay(ctx, *third, &checkpoint, state.Turns[2].Sequence))
	state, err = repo.BrowserReplayState(ctx, third.Run)
	require.NoError(t, err)
	require.True(t, state.Pinned)
	require.NotEmpty(t, state.Checkpoint.ID)
	require.JSONEq(t, string(checkpoint.Content), string(state.Checkpoint.Content))
	require.Len(t, state.Turns, 2)
	finishReplayFixture(t, repo, third, "third answer")
	require.NoError(t, repo.SweepWorkbenchPayloads(ctx, cutoff, cutoff, 20))
	stored, err = repo.GetOperation(ctx, admission.UserID, first.Run.ID, op.ID)
	require.NoError(t, err)
	require.JSONEq(t, `null`, string(stored.Result.Output))
	require.NotEmpty(t, stored.ResultHash)

	fourth := admitReplayFixture(t, repo, admission, "", "after compaction")
	resumed, err := repo.BrowserReplayState(ctx, fourth.Run)
	require.NoError(t, err)
	require.False(t, resumed.Pinned)
	require.Equal(t, state.Checkpoint.ID, resumed.Checkpoint.ID)
	require.Len(t, resumed.Turns, 3)
	require.Equal(t, second.Run.ID, resumed.Turns[0].RunID)
	require.Equal(t, state.Checkpoint.ID, resumed.Turns[1].CheckpointID)
}

func TestWorkbenchReplayRegenerationUsesLogicalTurnOrder(t *testing.T) {
	ctx := context.Background()
	base, admission := officeTestAdmission(t)
	repo := base.(*officeAgentRepository)
	first := admitReplayFixture(t, repo, admission, "", "earlier question")
	require.NoError(t, repo.PinBrowserReplay(ctx, *first, nil, 1))
	finishReplayFixture(t, repo, first, "first answer")
	second := admitReplayFixture(t, repo, admission, "", "later question")
	state, err := repo.BrowserReplayState(ctx, second.Run)
	require.NoError(t, err)
	secondSequence := state.Turns[1].Sequence
	oldCheckpoint := service.OfficeReplayCheckpoint{BeforeSequence: secondSequence, Content: json.RawMessage(`{"text":"first answer summarized"}`)}
	require.NoError(t, repo.PinBrowserReplay(ctx, *second, &oldCheckpoint, secondSequence))
	finishReplayFixture(t, repo, second, "later answer")

	regeneration := admitReplayFixture(t, repo, admission, first.Run.ID, "earlier question")
	state, err = repo.BrowserReplayState(ctx, regeneration.Run)
	require.NoError(t, err)
	require.Nil(t, state.Checkpoint)
	require.Len(t, state.Turns, 1)
	require.Equal(t, regeneration.Run.ID, state.Turns[0].RunID)
	require.EqualValues(t, 1, state.Turns[0].Sequence)
	require.NoError(t, repo.PinBrowserReplay(ctx, *regeneration, nil, 1))
	finishReplayFixture(t, repo, regeneration, "replacement answer")

	third := admitReplayFixture(t, repo, admission, "", "new message")
	state, err = repo.BrowserReplayState(ctx, third.Run)
	require.NoError(t, err)
	require.Nil(t, state.Checkpoint)
	require.Len(t, state.Turns, 3)
	require.Equal(t, []string{regeneration.Run.ID, second.Run.ID, third.Run.ID},
		[]string{state.Turns[0].RunID, state.Turns[1].RunID, state.Turns[2].RunID})
	require.JSONEq(t, `"replacement answer"`, string(state.Turns[0].Assistant.Content))
	require.True(t, state.Turns[0].NativeAvailable)
	require.False(t, state.Turns[1].NativeAvailable)
	require.ErrorIs(t, repo.PinBrowserReplay(ctx, *third, nil, 1), service.ErrOfficeResultUnknown)
	_, err = repo.SelectAnswer(ctx, admission.UserID, first.Run.ID)
	require.ErrorIs(t, err, service.ErrOfficeActiveRun)
	_, err = repo.CancelRun(ctx, admission.UserID, third.Run.ID)
	require.NoError(t, err)
	_, err = repo.SelectAnswer(ctx, admission.UserID, first.Run.ID)
	require.NoError(t, err)
	fourth := admitReplayFixture(t, repo, admission, "", "after answer selection")
	state, err = repo.BrowserReplayState(ctx, fourth.Run)
	require.NoError(t, err)
	require.Equal(t, first.Run.ID, state.Turns[0].RunID)
	require.JSONEq(t, `"first answer"`, string(state.Turns[0].Assistant.Content))
}

func TestWorkbenchReplayBoundsUncompressedVisibleHistory(t *testing.T) {
	ctx := context.Background()
	base, admission := officeTestAdmission(t)
	repo := base.(*officeAgentRepository)
	first := admitReplayFixture(t, repo, admission, "", "initial question")
	require.NoError(t, repo.PinBrowserReplay(ctx, *first, nil, 1))
	finishReplayFixture(t, repo, first, "first answer")
	// The sample history compresses well. The replay read still counts its
	// full serialized size before transferring it to the application.
	_, err := integrationDB.ExecContext(ctx, `UPDATE office_agent_messages SET content=to_jsonb(repeat('文本'::text,5000))
		WHERE run_id=$1 AND role='assistant'`, first.Run.ID)
	require.NoError(t, err)
	next := admitReplayFixture(t, repo, admission, "", "follow-up")
	tx, err := integrationDB.BeginTx(ctx, nil)
	require.NoError(t, err)
	defer func() { _ = tx.Rollback() }()
	turns, truncated, err := loadWorkbenchReplayTurns(ctx, tx, next.Run, next.Run.ContextBoundary+1,
		next.Run.ContextBoundary, 0, 12<<10)
	require.NoError(t, err)
	require.True(t, truncated)
	require.Len(t, turns, 1)
	require.Equal(t, next.Run.ID, turns[0].RunID)
}
