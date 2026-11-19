//go:build integration

package repository

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
)

func officeChatRepo(t *testing.T, base service.OfficeAgentRepository) service.OfficeChatRepository {
	t.Helper()
	repo, ok := base.(service.OfficeChatRepository)
	require.True(t, ok)
	return repo
}

func officeNextAdmission(in service.OfficeRunAdmission, text string) service.OfficeRunAdmission {
	in.RunID, in.IdempotencyKey = uuid.NewString(), uuid.NewString()
	in.UserContent, _ = json.Marshal(map[string]string{"text": text})
	in.RegenerationOf = ""
	return in
}

func officeClaimPreset(t *testing.T, repo service.OfficeChatRepository, mode, run string) *service.OfficeRunLease {
	t.Helper()
	lease, err := repo.ClaimPreset(context.Background(), "chat-integration-"+uuid.NewString(), mode, time.Minute)
	require.NoError(t, err)
	require.NotNil(t, lease)
	require.Equal(t, run, lease.Run.ID)
	return lease
}

func officeFinishText(t *testing.T, repo service.OfficeAgentRepository, lease service.OfficeRunLease, answer string) {
	t.Helper()
	content, err := json.Marshal(map[string]string{"text": answer})
	require.NoError(t, err)
	_, err = repo.FinishRun(context.Background(), lease, service.OfficeRunCompleted, content, "", "")
	require.NoError(t, err)
}

func officeContextTexts(t *testing.T, rows []service.OfficeMessage) []string {
	t.Helper()
	texts := make([]string, 0, len(rows))
	for _, row := range rows {
		var body struct {
			Text string `json:"text"`
		}
		require.NoError(t, json.Unmarshal(row.Content, &body))
		texts = append(texts, body.Text)
	}
	return texts
}

func TestOfficeChatDeleteRacingCompletionDoesNotDeadlock(t *testing.T) {
	base, input := officeTestAdmission(t)
	repo := officeChatRepo(t, base)
	input.Mode = "chat"
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	for i := range 12 {
		if i > 0 {
			input = officeNextAdmission(input, "completion racing deletion")
			input.ConversationID = uuid.NewString()
			_, err := base.CreateConversation(ctx, input.UserID, input.ConversationID, "concurrent deletion")
			require.NoError(t, err)
		}
		_, _, err := base.AdmitRun(ctx, input)
		require.NoError(t, err)
		lease := officeClaimPreset(t, repo, "chat", input.RunID)
		start := make(chan struct{})
		finished := make(chan error, 1)
		deleted := make(chan error, 1)
		go func() {
			<-start
			_, err := base.FinishRun(ctx, *lease, service.OfficeRunCompleted, json.RawMessage(`{"text":"done"}`), "", "")
			finished <- err
		}()
		go func() {
			<-start
			deleted <- repo.DeleteConversation(ctx, input.UserID, input.ConversationID)
		}()
		close(start)
		require.NoError(t, <-deleted)
		err = <-finished
		require.True(t, err == nil || errors.Is(err, service.ErrOfficeLeaseLost), "a cancelled completion may lose its lease, never deadlock: %v", err)
		_, err = base.GetConversation(ctx, input.UserID, input.ConversationID)
		require.ErrorIs(t, err, service.ErrOfficeNotFound)
	}
}

func TestOfficeChatModesHaveIndependentUserCapsButOneConversationWriter(t *testing.T) {
	ctx := context.Background()
	base, input := officeTestAdmission(t)
	repo := officeChatRepo(t, base)
	input.Mode = "chat"
	_, _, err := base.AdmitRun(ctx, input)
	require.NoError(t, err)
	office := officeNextAdmission(input, "office question")
	office.Mode, office.ConversationID = "office", uuid.NewString()
	_, err = base.CreateConversation(ctx, input.UserID, office.ConversationID, "office")
	require.NoError(t, err)
	_, _, err = base.AdmitRun(ctx, office)
	require.NoError(t, err, "one chat run must not consume the user's separate office slot")
	sameConversation := officeNextAdmission(input, "competing writer")
	sameConversation.Mode, sameConversation.MaxActiveRuns = "office", 2
	_, _, err = base.AdmitRun(ctx, sameConversation)
	require.ErrorIs(t, err, service.ErrOfficeActiveRun, "chat and office still serialize the same conversation")
	blocked := officeNextAdmission(input, "another chat")
	blocked.ConversationID = uuid.NewString()
	_, err = base.CreateConversation(ctx, input.UserID, blocked.ConversationID, "another")
	require.NoError(t, err)
	_, _, err = base.AdmitRun(ctx, blocked)
	require.ErrorIs(t, err, service.ErrOfficeActiveRun, "the configured cap applies within the chat pool")
	chatLease := officeClaimPreset(t, repo, "chat", input.RunID)
	officeLease := officeClaimPreset(t, repo, "office", office.RunID)
	require.Equal(t, "chat", chatLease.Run.Mode)
	require.Equal(t, "office", officeLease.Run.Mode)
}

func TestOfficeChatConcurrentAdmissionHonorsConfiguredCapAcrossConversations(t *testing.T) {
	ctx := context.Background()
	base, input := officeTestAdmission(t)
	input.Mode, input.MaxActiveRuns = "chat", 2
	inputs := []service.OfficeRunAdmission{input}
	for range 2 {
		next := officeNextAdmission(input, "parallel question")
		next.ConversationID = uuid.NewString()
		_, err := base.CreateConversation(ctx, input.UserID, next.ConversationID, "parallel")
		require.NoError(t, err)
		inputs = append(inputs, next)
	}
	results := make(chan error, len(inputs))
	var wait sync.WaitGroup
	for _, candidate := range inputs {
		wait.Add(1)
		go func(in service.OfficeRunAdmission) {
			defer wait.Done()
			_, _, err := base.AdmitRun(ctx, in)
			results <- err
		}(candidate)
	}
	wait.Wait()
	close(results)
	allowed, blocked := 0, 0
	for err := range results {
		if err == nil {
			allowed++
		} else {
			require.ErrorIs(t, err, service.ErrOfficeActiveRun)
			blocked++
		}
	}
	require.Equal(t, 2, allowed)
	require.Equal(t, 1, blocked)
	var messages int
	require.NoError(t, integrationDB.QueryRowContext(ctx, `SELECT COUNT(*) FROM office_agent_messages WHERE user_id=$1`, input.UserID).Scan(&messages))
	require.Equal(t, 2, messages, "rejected admission leaves no phantom user turn")
}

func TestOfficeChatRegenerationUsesOriginalQuestionAndOnlySelectedCandidate(t *testing.T) {
	ctx := context.Background()
	base, first := officeTestAdmission(t)
	repo := officeChatRepo(t, base)
	first.Mode, first.UserContent = "chat", json.RawMessage(`{"text":"first question"}`)
	_, _, err := base.AdmitRun(ctx, first)
	require.NoError(t, err)
	officeFinishText(t, base, *officeClaimPreset(t, repo, "chat", first.RunID), "first answer")
	second := officeNextAdmission(first, "original second question")
	_, _, err = base.AdmitRun(ctx, second)
	require.NoError(t, err)
	officeFinishText(t, base, *officeClaimPreset(t, repo, "chat", second.RunID), "candidate A")
	root, question, err := repo.RegenerationSource(ctx, first.UserID, first.ConversationID, second.RunID)
	require.NoError(t, err)
	require.Equal(t, second.RunID, root.ID)
	require.Equal(t, "user", question.Role)
	regenerated := officeNextAdmission(second, "unused")
	regenerated.RegenerationOf, regenerated.UserContent = root.ID, question.Content
	_, _, err = base.AdmitRun(ctx, regenerated)
	require.NoError(t, err)
	lease := officeClaimPreset(t, repo, "chat", regenerated.RunID)
	contextRows, err := repo.ContextMessages(ctx, lease.Run)
	require.NoError(t, err)
	require.Equal(t, []string{"first question", "first answer", "original second question"}, officeContextTexts(t, contextRows))
	officeFinishText(t, base, *lease, "candidate B")
	third := officeNextAdmission(second, "third question")
	_, _, err = base.AdmitRun(ctx, third)
	require.NoError(t, err)
	thirdRun, err := base.GetRun(ctx, first.UserID, third.RunID)
	require.NoError(t, err)
	contextRows, err = repo.ContextMessages(ctx, *thirdRun)
	require.NoError(t, err)
	require.Equal(t, []string{"first question", "first answer", "original second question", "candidate B", "third question"}, officeContextTexts(t, contextRows))
	_, err = repo.SelectAnswer(ctx, first.UserID, second.RunID)
	require.ErrorIs(t, err, service.ErrOfficeActiveRun, "candidate switching cannot alter a running turn's history")
	_, err = base.CancelRun(ctx, first.UserID, third.RunID)
	require.NoError(t, err)
	_, err = repo.SelectAnswer(ctx, first.UserID, second.RunID)
	require.NoError(t, err)
	fourth := officeNextAdmission(second, "new third question")
	_, _, err = base.AdmitRun(ctx, fourth)
	require.NoError(t, err)
	fourthRun, err := base.GetRun(ctx, first.UserID, fourth.RunID)
	require.NoError(t, err)
	contextRows, err = repo.ContextMessages(ctx, *fourthRun)
	require.NoError(t, err)
	require.Equal(t, []string{"first question", "first answer", "original second question", "candidate A", "new third question"}, officeContextTexts(t, contextRows))
	root, original, err := repo.RegenerationSource(ctx, first.UserID, first.ConversationID, regenerated.RunID)
	require.NoError(t, err)
	require.Equal(t, second.RunID, root.ID)
	require.JSONEq(t, string(question.Content), string(original.Content))
}

func TestOfficeChatSummaryCASAndRegenerationInvalidatesBoundary(t *testing.T) {
	ctx := context.Background()
	base, first := officeTestAdmission(t)
	repo := officeChatRepo(t, base)
	first.Mode = "chat"
	_, _, err := base.AdmitRun(ctx, first)
	require.NoError(t, err)
	officeFinishText(t, base, *officeClaimPreset(t, repo, "chat", first.RunID), "original answer")
	second := officeNextAdmission(first, "next question")
	_, _, err = base.AdmitRun(ctx, second)
	require.NoError(t, err)
	lease := officeClaimPreset(t, repo, "chat", second.RunID)
	rows, err := repo.ContextMessages(ctx, lease.Run)
	require.NoError(t, err)
	require.Len(t, rows, 3)
	hash := sha256.Sum256([]byte(strings.Join([]string{rows[0].ID, rows[1].ID}, "\n")))
	summary := service.OfficeContextSummary{Content: "faithful context", ThroughMessageID: rows[1].ID, SourceHash: hex.EncodeToString(hash[:])}
	version, err := repo.SaveContextSummary(ctx, *lease, summary, 0, "summary-one")
	require.NoError(t, err)
	require.EqualValues(t, 1, version)
	repeated, err := repo.SaveContextSummary(ctx, *lease, summary, 0, "summary-one")
	require.NoError(t, err)
	require.Equal(t, version, repeated)
	_, err = repo.SaveContextSummary(ctx, *lease, summary, 0, "stale-cas")
	require.ErrorIs(t, err, service.ErrOfficeConflict)
	changed := summary
	changed.Content = "different summary under same operation"
	_, err = repo.SaveContextSummary(ctx, *lease, changed, 1, "summary-one")
	require.ErrorIs(t, err, service.ErrOfficeConflict)
	currentQuestion := summary
	currentQuestion.ThroughMessageID = rows[2].ID
	_, err = repo.SaveContextSummary(ctx, *lease, currentQuestion, 1, "cannot-hide-question")
	require.ErrorIs(t, err, service.ErrOfficeConflict)
	loaded, err := repo.ContextSummary(ctx, *lease)
	require.NoError(t, err)
	require.Equal(t, summary.Content, loaded.Content)
	_, err = base.CancelRun(ctx, first.UserID, second.RunID)
	require.NoError(t, err)
	regenerated := officeNextAdmission(first, "regenerate original")
	regenerated.RegenerationOf = first.RunID
	_, _, err = base.AdmitRun(ctx, regenerated)
	require.NoError(t, err)
	regenLease := officeClaimPreset(t, repo, "chat", regenerated.RunID)
	loaded, err = repo.ContextSummary(ctx, *regenLease)
	require.NoError(t, err)
	require.Empty(t, loaded.Content, "a summary containing the old candidate cannot leak into its regeneration")
	require.EqualValues(t, 1, loaded.Version, "invalidating content keeps the durable CAS version")
}

func TestOfficeChatCheckpointEpochClarificationReplayAndCancellation(t *testing.T) {
	ctx := context.Background()
	base, input, old := officeAdmitAndClaim(t)
	repo := officeChatRepo(t, base)
	payload := json.RawMessage(`{"request_id":"clarify-one","title":"用途","questions":[{"id":"purpose","question":"用途？","options":[]}]}`)
	_, err := repo.WaitForInput(ctx, *old, "clarify-one", payload)
	require.ErrorIs(t, err, service.ErrOfficeResultUnknown, "waiting requires a durable checkpoint")
	checkpoint := json.RawMessage(`{"format":"office-langgraph-json-v1","records":[],"workspace_version":0}`)
	version, err := repo.SaveCheckpoint(ctx, *old, checkpoint, 0, "checkpoint-one")
	require.NoError(t, err)
	require.EqualValues(t, 1, version)
	version, err = repo.SaveCheckpoint(ctx, *old, checkpoint, 0, "checkpoint-one")
	require.NoError(t, err)
	require.EqualValues(t, 1, version)
	_, err = repo.SaveCheckpoint(ctx, *old, json.RawMessage(`{"changed":true}`), 1, "checkpoint-one")
	require.ErrorIs(t, err, service.ErrOfficeConflict)
	_, err = repo.SaveCheckpoint(ctx, *old, checkpoint, 0, "checkpoint-stale")
	require.ErrorIs(t, err, service.ErrOfficeConflict)
	waiting, err := repo.WaitForInput(ctx, *old, "clarify-one", payload)
	require.NoError(t, err)
	require.Equal(t, service.OfficeRunWaitingUser, waiting.Status)
	require.ErrorIs(t, base.CheckLease(ctx, *old), service.ErrOfficeLeaseLost)
	reply := json.RawMessage(`{"request_id":"clarify-one","answers":[{"question_id":"purpose","values":["internal"]}]}`)
	_, err = repo.ResumeInput(ctx, input.UserID, input.RunID, "different-request", reply)
	require.ErrorIs(t, err, service.ErrOfficeConflict)
	resumed, err := repo.ResumeInput(ctx, input.UserID, input.RunID, "clarify-one", reply)
	require.NoError(t, err)
	require.Equal(t, service.OfficeRunQueued, resumed.Status)
	repeated, err := repo.ResumeInput(ctx, input.UserID, input.RunID, "clarify-one", reply)
	require.NoError(t, err)
	require.Equal(t, resumed.Status, repeated.Status)
	_, err = repo.ResumeInput(ctx, input.UserID, input.RunID, "clarify-one", json.RawMessage(`{"different":true}`))
	require.ErrorIs(t, err, service.ErrOfficeConflict)
	next := officeClaimPreset(t, repo, "office", input.RunID)
	require.Greater(t, next.Epoch, old.Epoch)
	_, err = repo.SaveCheckpoint(ctx, *old, checkpoint, 1, "late-old-worker")
	require.ErrorIs(t, err, service.ErrOfficeLeaseLost)
	stored, version, err := repo.Checkpoint(ctx, *next)
	require.NoError(t, err)
	require.EqualValues(t, 1, version)
	require.JSONEq(t, string(checkpoint), string(stored))
	_, err = repo.SaveCheckpoint(ctx, *next, checkpoint, 1, "checkpoint-new-worker")
	require.NoError(t, err)
	_, err = base.CancelRun(ctx, input.UserID, input.RunID)
	require.NoError(t, err)
	afterCancel, err := repo.ResumeInput(ctx, input.UserID, input.RunID, "clarify-one", reply)
	require.NoError(t, err)
	require.Equal(t, service.OfficeRunCancelled, afterCancel.Status, "idempotent reply must never resurrect a cancelled run")
	_, err = repo.ResumeInput(ctx, input.UserID, input.RunID, "new-request", reply)
	require.ErrorIs(t, err, service.ErrOfficeConflict)
	_, err = repo.SaveCheckpoint(ctx, *next, checkpoint, 2, "after-cancel")
	require.ErrorIs(t, err, service.ErrOfficeLeaseLost)
	claim, err := repo.ClaimPreset(ctx, "after-cancel-worker", "office", time.Minute)
	require.NoError(t, err)
	require.Nil(t, claim)
	var resumeEvents int
	require.NoError(t, integrationDB.QueryRowContext(ctx, `SELECT COUNT(*) FROM office_agent_run_events WHERE run_id=$1 AND event_id='resume:clarify-one'`, input.RunID).Scan(&resumeEvents))
	require.Equal(t, 1, resumeEvents)
}

func TestOfficeChatClarificationPausePreservesActiveExecutionBudget(t *testing.T) {
	ctx := context.Background()
	base, input, lease := officeAdmitAndClaim(t)
	repo := officeChatRepo(t, base)
	checkpoint := json.RawMessage(`{"format":"office-langgraph-json-v1","records":[],"workspace_version":0}`)
	_, err := repo.SaveCheckpoint(ctx, *lease, checkpoint, 0, "before-long-wait")
	require.NoError(t, err)
	_, err = repo.WaitForInput(ctx, *lease, "long-wait", json.RawMessage(`{"request_id":"long-wait","questions":[{"id":"q","question":"补充？"}]}`))
	require.NoError(t, err)
	// Model twenty seconds of actual work, followed by two minutes waiting for
	// a human. The configured active budget is sixty seconds.
	_, err = integrationDB.ExecContext(ctx, `UPDATE office_agent_runs SET started_at=clock_timestamp()-INTERVAL '140 seconds',paused_at=clock_timestamp()-INTERVAL '120 seconds' WHERE id=$1`, input.RunID)
	require.NoError(t, err)
	_, err = repo.ResumeInput(ctx, input.UserID, input.RunID, "long-wait", json.RawMessage(`{"request_id":"long-wait","answers":[{"question_id":"q","values":["回答"]}]}`))
	require.NoError(t, err)
	resumed := officeClaimPreset(t, repo, "office", input.RunID)
	require.NoError(t, base.CheckLease(ctx, *resumed), "human waiting time must not exhaust the active run budget")
	var pausedSeconds int64
	var actualAge float64
	require.NoError(t, integrationDB.QueryRowContext(ctx, `SELECT paused_seconds,EXTRACT(EPOCH FROM(clock_timestamp()-started_at)) FROM office_agent_runs WHERE id=$1`, input.RunID).Scan(&pausedSeconds, &actualAge))
	require.GreaterOrEqual(t, pausedSeconds, int64(119))
	require.Greater(t, actualAge, float64(130), "the original start time remains truthful")
	// Resuming does not reset already consumed execution time.
	_, err = integrationDB.ExecContext(ctx, `UPDATE office_agent_runs SET started_at=clock_timestamp()-((paused_seconds+61)*INTERVAL '1 second') WHERE id=$1`, input.RunID)
	require.NoError(t, err)
	require.ErrorIs(t, base.CheckLease(ctx, *resumed), service.ErrOfficeLeaseLost)
}
