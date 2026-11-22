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

func TestWorkbenchRetentionPreservesLateSearchSettlementAndPartialAnswer(t *testing.T) {
	ctx := context.Background()
	const each = int64(75_000)
	repo, lease := workbenchSearchBillingFixture(t, each)
	op := prepareWorkbenchSearchBilling(t, repo, lease, "browser:tool:0:0")
	quote := service.WorkbenchSearchQuote{Provider: "deepseek", AmountUSD: .00075, Billing: service.DefaultBillingCurrencyConfig()}
	_, err := repo.DispatchWorkbenchSearch(ctx, lease, op.ID, quote)
	require.NoError(t, err)
	for _, chunk := range []struct{ id, kind, text string }{
		{"delta:a", "assistant.delta", `{"text":"部分"}`},
		{"summary:a", "reasoning.delta", `{"text":"摘要"}`},
		{"delta:b", "assistant.delta", `{"text":"回答"}`},
	} {
		_, err := repo.AppendEvent(ctx, lease, chunk.id, chunk.kind, json.RawMessage(chunk.text))
		require.NoError(t, err)
	}
	_, err = repo.CancelRun(ctx, lease.Run.UserID, lease.Run.ID)
	require.NoError(t, err)
	_, err = integrationDB.ExecContext(ctx, `UPDATE office_agent_runs SET finished_at=NOW()-INTERVAL '2 days' WHERE id=$1`, lease.Run.ID)
	require.NoError(t, err)
	cutoff := time.Now().Add(-24 * time.Hour)
	require.NoError(t, repo.SweepWorkbenchPayloads(ctx, cutoff, cutoff, 20))
	var retained, purged int
	require.NoError(t, integrationDB.QueryRowContext(ctx, `SELECT COUNT(*) FILTER(WHERE event_type='assistant.partial'),
		COUNT(*) FILTER(WHERE event_type IN ('assistant.delta','reasoning.delta')) FROM office_agent_run_events WHERE run_id=$1`, lease.Run.ID).Scan(&retained, &purged))
	require.Equal(t, 1, retained)
	require.Zero(t, purged)
	var snapshot string
	require.NoError(t, integrationDB.QueryRowContext(ctx, `SELECT payload::text FROM office_agent_run_events
		WHERE run_id=$1 AND event_type='assistant.partial'`, lease.Run.ID).Scan(&snapshot))
	require.JSONEq(t, `{"text":"部分回答","reasoning_summary":"摘要"}`, snapshot)
	// A late result can still settle with the original dispatch identity; it only
	// stores a bounded projection and its replay fingerprint after compaction.
	result := service.OfficeOperationResult{Output: json.RawMessage(`{"provider":"deepseek","results":[{"url":"https://example.org/","title":"来源","snippet":"temporary payload"}]}`)}
	for range 2 {
		stored, err := repo.CompleteWorkbenchSearch(ctx, lease, op.ID, result, true)
		require.NoError(t, err)
		require.NotNil(t, stored.Result)
		require.JSONEq(t, `null`, string(stored.Result.Output))
		require.JSONEq(t, `{"tool":"web_search"}`, string(stored.Input))
		require.NotNil(t, stored.ToolProjection)
		require.Equal(t, "completed", stored.ToolProjection.Detail.State)
		require.Equal(t, "交付", stored.ToolProjection.Detail.Query)
		require.Len(t, stored.ToolProjection.Citations, 1)
	}
	changed := service.OfficeOperationResult{Output: json.RawMessage(`{"provider":"deepseek","results":[]}`)}
	_, err = repo.CompleteWorkbenchSearch(ctx, lease, op.ID, changed, true)
	require.ErrorIs(t, err, service.ErrOfficeConflict)
	assertWorkbenchSearchWallet(t, lease.Run.UserID, 0, 0)
	var ledgers int
	require.NoError(t, integrationDB.QueryRowContext(ctx, `SELECT COUNT(*) FROM billing_ledger WHERE user_id=$1 AND request_id=$2`,
		lease.Run.UserID, "client:"+op.RequestID).Scan(&ledgers))
	require.Equal(t, 1, ledgers)
	require.NoError(t, repo.SweepWorkbenchPayloads(ctx, cutoff, cutoff, 20))
}

func TestWorkbenchRetentionProtectsLegacyActiveHistoryUntilRunExpires(t *testing.T) {
	ctx := context.Background()
	base, historical := officeTestAdmission(t)
	repo := base.(*officeAgentRepository)
	historical.Mode, historical.BrowserDriven = "chat", true
	_, _, err := repo.AdmitRun(ctx, historical)
	require.NoError(t, err)
	lease, _, err := repo.ClaimBrowserStep(ctx, historical.UserID, historical.RunID, time.Minute)
	require.NoError(t, err)
	op := officePrepared(t, repo, *lease, "browser:model:0")
	_, err = repo.DispatchOperation(ctx, *lease, op.ID)
	require.NoError(t, err)
	result := service.OfficeOperationResult{Output: json.RawMessage(`{"private":"temporary raw model result"}`), HTTPStatus: 200}
	_, err = repo.RecordOperationResult(ctx, *lease, op.ID, result)
	require.NoError(t, err)
	_, err = repo.FinishRun(ctx, *lease, service.OfficeRunCompleted, json.RawMessage(`"visible answer"`), "", "")
	require.NoError(t, err)
	_, err = integrationDB.ExecContext(ctx, `UPDATE office_agent_runs SET finished_at=NOW()-INTERVAL '2 days' WHERE id=$1`, historical.RunID)
	require.NoError(t, err)
	active := historical
	active.RunID, active.IdempotencyKey = uuid.NewString(), uuid.NewString()
	_, _, err = repo.AdmitRun(ctx, active)
	require.NoError(t, err)
	cutoff := time.Now().Add(-24 * time.Hour)
	require.NoError(t, repo.SweepWorkbenchPayloads(ctx, cutoff, cutoff, 20))
	stored, err := repo.GetOperation(ctx, historical.UserID, historical.RunID, op.ID)
	require.NoError(t, err)
	require.JSONEq(t, string(result.Output), string(stored.Result.Output))
	_, err = integrationDB.ExecContext(ctx, `UPDATE office_agent_runs SET updated_at=NOW()-INTERVAL '2 days' WHERE id=$1`, active.RunID)
	require.NoError(t, err)
	require.NoError(t, repo.SweepWorkbenchPayloads(ctx, cutoff, cutoff, 20))
	stored, err = repo.GetOperation(ctx, historical.UserID, historical.RunID, op.ID)
	require.NoError(t, err)
	require.JSONEq(t, `null`, string(stored.Result.Output))
	require.Equal(t, 200, stored.Result.HTTPStatus)
	require.JSONEq(t, `{}`, string(stored.Input))
	require.NotEmpty(t, stored.ResultHash)
	expired, err := repo.GetRun(ctx, active.UserID, active.RunID)
	require.NoError(t, err)
	require.Equal(t, service.OfficeRunFailed, expired.Status)
	require.Equal(t, "WORKBENCH_RUN_EXPIRED", expired.ErrorCode)
	stored, err = repo.RecordOperationResult(ctx, *lease, op.ID, result)
	require.NoError(t, err)
	require.JSONEq(t, `null`, string(stored.Result.Output))
	messages, err := repo.ListMessages(ctx, historical.UserID, historical.ConversationID, 0, 10)
	require.NoError(t, err)
	require.Len(t, messages, 3)
	require.JSONEq(t, `"visible answer"`, string(messages[1].Content))
}
