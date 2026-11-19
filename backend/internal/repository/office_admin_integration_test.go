//go:build integration

package repository

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
)

func TestOfficeAdminOverviewCountsRunsAndQueueWithJoinedUserStatus(t *testing.T) {
	ctx := context.Background()
	base, input := officeTestAdmission(t)
	input.Mode = "chat"
	admin, ok := base.(service.OfficeAdminRepository)
	require.True(t, ok)
	before, err := admin.AdminOverview(ctx, 7)
	require.NoError(t, err)
	_, _, err = base.AdmitRun(ctx, input)
	require.NoError(t, err)
	queued, err := admin.AdminOverview(ctx, 7)
	require.NoError(t, err)
	require.Equal(t, before.TotalRuns+1, queued.TotalRuns)
	require.Equal(t, before.ChatRuns+1, queued.ChatRuns)
	require.Equal(t, before.Queued+1, queued.Queued)
	_, err = integrationDB.ExecContext(ctx, `UPDATE office_agent_runs SET mode='office',status='completed',model_calls=2,tool_calls=1 WHERE id=$1`, input.RunID)
	require.NoError(t, err)
	completed, err := admin.AdminOverview(ctx, 7)
	require.NoError(t, err)
	require.Equal(t, before.OfficeRuns+1, completed.OfficeRuns)
	require.Equal(t, before.Completed+1, completed.Completed)
	require.Equal(t, before.ModelCalls+2, completed.ModelCalls)
	require.Equal(t, before.ToolCalls+1, completed.ToolCalls)
	require.Equal(t, before.Queued, completed.Queued)
	_, err = integrationDB.ExecContext(ctx, `UPDATE users SET deleted_at=NOW() WHERE id=$1`, input.UserID)
	require.NoError(t, err)
	deleted, err := admin.AdminOverview(ctx, 7)
	require.NoError(t, err)
	require.Equal(t, before.TotalRuns, deleted.TotalRuns)
	require.Equal(t, before.ModelCalls, deleted.ModelCalls)
}

func TestOfficeAdminPageAndUsageReadOriginalLedgerWithExactUserAndKey(t *testing.T) {
	ctx := context.Background()
	base, input, lease := officeAdmitAndClaim(t)
	admin, ok := base.(service.OfficeAdminRepository)
	require.True(t, ok)
	_, other := officeTestAdmission(t)
	var account, wrongKey int64
	require.NoError(t, integrationDB.QueryRowContext(ctx, `INSERT INTO accounts(name,platform,type) VALUES('observed-upstream','openai','apikey') RETURNING id`).Scan(&account))
	require.NoError(t, integrationDB.QueryRowContext(ctx, `INSERT INTO api_keys(user_id,group_id,key,name,key_type) VALUES($1,$2,$3,'other-system-key','system') RETURNING id`, input.UserID, input.GroupID, "sk-"+uuid.NewString()).Scan(&wrongKey))
	t.Cleanup(func() {
		_, _ = integrationDB.ExecContext(context.Background(), `DELETE FROM accounts WHERE id=$1`, account)
	})
	op := officePrepared(t, base, *lease, "model-one")
	unsettled := officePrepared(t, base, *lease, "model-two")
	insertUsage := func(user, key int64, request string, tokens, credits int64) {
		t.Helper()
		_, err := integrationDB.ExecContext(ctx, `INSERT INTO usage_logs(user_id,api_key_id,account_id,request_id,model,upstream_model,input_tokens,output_tokens,cache_read_tokens,actual_cost,charge_credits,duration_ms) VALUES($1,$2,$3,$4,'public-model','observed-model',$5,7,3,0.123,$6,456)`, user, key, account, "client:"+request, tokens, credits)
		require.NoError(t, err)
	}
	insertUsage(input.UserID, input.APIKeyID, op.RequestID, 11, 12345)
	insertUsage(input.UserID, wrongKey, op.RequestID, 999, 99999)
	insertUsage(other.UserID, input.APIKeyID, unsettled.RequestID, 777, 77777)
	for range 35 {
		_, err := integrationDB.ExecContext(ctx, `INSERT INTO ops_error_logs(client_request_id,user_id,api_key_id,account_id,error_phase,error_type,upstream_status_code,upstream_model) VALUES($1,$2,$3,$4,'upstream','upstream_error',503,'failed-model')`, op.RequestID, input.UserID, input.APIKeyID, account)
		require.NoError(t, err)
	}
	_, err := integrationDB.ExecContext(ctx, `INSERT INTO ops_error_logs(client_request_id,user_id,api_key_id,account_id,error_phase,error_type,upstream_status_code) VALUES($1,$2,$3,$4,'upstream','upstream_error',500),($1,$5,$6,$4,'upstream','upstream_error',500)`, unsettled.RequestID, input.UserID, wrongKey, account, other.UserID, input.APIKeyID)
	require.NoError(t, err)
	t.Cleanup(func() {
		_, _ = integrationDB.ExecContext(context.Background(), `DELETE FROM ops_error_logs WHERE client_request_id IN($1,$2)`, op.RequestID, unsettled.RequestID)
	})
	page, err := admin.AdminRuns(ctx, service.OfficeAdminFilter{Page: 1, PageSize: 20, UserID: input.UserID})
	require.NoError(t, err)
	require.EqualValues(t, 1, page.Total)
	require.Len(t, page.Items, 1)
	require.Equal(t, input.RunID, page.Items[0].ID)
	require.EqualValues(t, 11, page.Items[0].InputTokens)
	require.EqualValues(t, 7, page.Items[0].OutputTokens)
	require.EqualValues(t, 12345, page.Items[0].ChargeCredits)
	require.Equal(t, input.Limits, page.Items[0].Limits, "SQL JSON projection preserves the public run contract")
	emptyPage, err := admin.AdminRuns(ctx, service.OfficeAdminFilter{Page: 2, PageSize: 20, UserID: input.UserID})
	require.NoError(t, err)
	require.Empty(t, emptyPage.Items)
	require.EqualValues(t, 1, emptyPage.Total, "out-of-range page still returns the exact count from the same SQL snapshot")
	filtered, err := admin.AdminRuns(ctx, service.OfficeAdminFilter{Page: 1, PageSize: 20, UserID: other.UserID})
	require.NoError(t, err)
	require.Empty(t, filtered.Items)
	detail, err := admin.AdminRun(ctx, input.RunID)
	require.NoError(t, err)
	require.Len(t, detail.Operations, 2)
	byID := map[string]service.OfficeCallDetail{}
	for _, call := range detail.Operations {
		byID[call.ID] = call
	}
	require.True(t, byID[op.ID].Usage.Settled)
	require.EqualValues(t, 11, byID[op.ID].Usage.InputTokens)
	require.EqualValues(t, 3, byID[op.ID].Usage.CacheReadTokens)
	require.InDelta(t, 0.123, byID[op.ID].Usage.ActualCost, 1e-9)
	require.Len(t, byID[op.ID].Usage.Upstreams, 31, "one observed usage plus at most thirty failed attempts per operation")
	require.False(t, byID[unsettled.ID].Usage.Settled, "same request under a different owner cannot settle this operation")
	require.Empty(t, byID[unsettled.ID].Usage.Upstreams)
	public, err := admin.RunUsage(ctx, input.UserID, input.RunID, false)
	require.NoError(t, err)
	for _, call := range public {
		require.Empty(t, call.Usage.Upstreams, "public call records never expose observed account identities")
	}
	encoded, err := json.Marshal(public)
	require.NoError(t, err)
	require.NotContains(t, string(encoded), "observed-upstream")
	require.NotContains(t, string(encoded), "observed-model")
	_, err = admin.RunUsage(ctx, other.UserID, input.RunID, false)
	require.ErrorIs(t, err, service.ErrOfficeNotFound)
	var usageCount int
	require.NoError(t, integrationDB.QueryRowContext(ctx, `SELECT COUNT(*) FROM usage_logs WHERE account_id=$1`, account).Scan(&usageCount))
	require.Equal(t, 3, usageCount, "admin reads do not write or reprice the original ledger")
}
