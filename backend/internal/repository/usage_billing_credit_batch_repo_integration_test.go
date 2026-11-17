//go:build integration

package repository

import (
	"context"
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"

	"github.com/Wei-Shaw/sub2api/internal/service"
)

type creditBatchLedgerEntry struct {
	Delta, BalanceAfter int64
}

type creditBatchState struct {
	Legacy                                batchBillingState
	BalanceCredits, BalanceVersion        int64
	SubscriptionDaily, SubscriptionWeekly int64
	SubscriptionMonthly                   int64
	KeyAQuota, KeyA5h, KeyA1d, KeyA7d     int64
	KeyBQuota, KeyB5h, KeyB1d, KeyB7d     int64
	Ledger                                []creditBatchLedgerEntry
}

func readCreditBatchState(t *testing.T, ctx context.Context, f batchBillingFixture) creditBatchState {
	t.Helper()
	s := creditBatchState{Legacy: readBatchBillingState(t, ctx, f)}
	require.NoError(t, integrationDB.QueryRowContext(ctx, `
		SELECT balance_credits, balance_credits_version FROM users WHERE id = $1
	`, f.userID).Scan(&s.BalanceCredits, &s.BalanceVersion))
	require.NoError(t, integrationDB.QueryRowContext(ctx, `
		SELECT daily_usage_credits, weekly_usage_credits, monthly_usage_credits FROM user_subscriptions WHERE id = $1
	`, f.subscriptionID).Scan(&s.SubscriptionDaily, &s.SubscriptionWeekly, &s.SubscriptionMonthly))
	require.NoError(t, integrationDB.QueryRowContext(ctx, `
		SELECT quota_used_credits, usage_5h_credits, usage_1d_credits, usage_7d_credits FROM api_keys WHERE id = $1
	`, f.keyA).Scan(&s.KeyAQuota, &s.KeyA5h, &s.KeyA1d, &s.KeyA7d))
	require.NoError(t, integrationDB.QueryRowContext(ctx, `
		SELECT quota_used_credits, usage_5h_credits, usage_1d_credits, usage_7d_credits FROM api_keys WHERE id = $1
	`, f.keyB).Scan(&s.KeyBQuota, &s.KeyB5h, &s.KeyB1d, &s.KeyB7d))
	rows, err := integrationDB.QueryContext(ctx, `
		SELECT delta_credits, balance_after FROM billing_ledger
		WHERE user_id = $1 AND reference_type = 'usage' ORDER BY id
	`, f.userID)
	require.NoError(t, err)
	defer func() { _ = rows.Close() }()
	for rows.Next() {
		var entry creditBatchLedgerEntry
		require.NoError(t, rows.Scan(&entry.Delta, &entry.BalanceAfter))
		s.Ledger = append(s.Ledger, entry)
	}
	require.NoError(t, rows.Err())
	return s
}

func setBatchCommandCredits(cmds []*service.UsageBillingCommand) {
	for _, cmd := range cmds {
		cmd.BillingEngine = service.BillingEngineCredits
		cmd.CreditsPerUSD = service.CreditsPerUSD
		cmd.USDToCNYRate = 7.2
		cmd.BillingVersion = 1
		cmd.BalanceCredits = int64(cmd.BalanceCost * float64(service.CreditsPerUSD))
		cmd.SubscriptionCredits = int64(cmd.SubscriptionCost * float64(service.CreditsPerUSD))
		cmd.APIKeyQuotaCredits = int64(cmd.APIKeyQuotaCost * float64(service.CreditsPerUSD))
		cmd.APIKeyRateLimitCredits = int64(cmd.APIKeyRateLimitCost * float64(service.CreditsPerUSD))
	}
}

func TestUsageBillingRepositoryApplyBatch_CreditsMatchSequentialAndDeduplicate(t *testing.T) {
	ctx := context.Background()
	client := testEntClient(t)
	repo := NewUsageBillingRepository(client, integrationDB)
	steps := []batchBillingStep{
		{key: 'A', account: 'P', balance: 1.5, keyQuota: 1.5, keyRate: 1.5, accountQuota: 1.5},
		{key: 'A', account: 'P', balance: 2.25, keyQuota: 2.25, keyRate: 2.25, accountQuota: 2.25},
		{key: 'B', account: 'Q', balance: 3, keyRate: 3, accountQuota: 3},
		{key: 'A', account: 'P', subscription: 1, keyQuota: 1},
		{key: 'A', account: 'P', balance: 4, keyQuota: 0.5, accountQuota: 0.5},
		{key: 'B', account: 'P', balance: 0.75, keyRate: 0.125, accountQuota: 2},
	}
	sequential := newBatchBillingFixture(t, client, 10)
	batched := newBatchBillingFixture(t, client, 10)
	cleanupBillingUser(t, sequential.userID)
	cleanupBillingUser(t, batched.userID)
	seqCmds := sequential.commands("credit-seq-"+uuid.NewString(), steps)
	batchCmds := batched.commands("credit-batch-"+uuid.NewString(), steps)
	setBatchCommandCredits(seqCmds)
	setBatchCommandCredits(batchCmds)

	seqResults := make([]*service.UsageBillingApplyResult, len(seqCmds))
	for i, cmd := range seqCmds {
		result, err := repo.Apply(ctx, cmd)
		require.NoError(t, err)
		seqResults[i] = result
	}
	outcomes, err := repo.ApplyBatch(ctx, batchCmds)
	require.NoError(t, err)
	require.Len(t, outcomes, len(seqCmds))
	for i := range seqCmds {
		require.NoError(t, outcomes[i].Err)
		require.Equal(t, seqResults[i], outcomes[i].Result, "command %d", i)
	}
	state := readCreditBatchState(t, ctx, batched)
	require.Equal(t, readCreditBatchState(t, ctx, sequential), state)
	require.Len(t, state.Ledger, 5, "every wallet charge must have its own ledger entry")
	require.Negative(t, state.BalanceCredits, "admitted requests preserve the existing overdraft policy")
	require.Equal(t, int64(100_000_000), state.SubscriptionDaily)
	require.Equal(t, int64(525_000_000), state.KeyAQuota)
	require.Equal(t, service.StatusAPIKeyQuotaExhausted, state.Legacy.keyAStatus)

	outcomes, err = repo.ApplyBatch(ctx, batchCmds)
	require.NoError(t, err)
	for _, out := range outcomes {
		require.NoError(t, out.Err)
		require.False(t, out.Result.Applied)
	}
	require.Equal(t, state, readCreditBatchState(t, ctx, batched), "retry must not charge or append ledger rows again")
}

func TestUsageBillingRepositoryApplyBatch_CreditsRollBackLedgerAndBalances(t *testing.T) {
	ctx := context.Background()
	client := testEntClient(t)
	repo := NewUsageBillingRepository(client, integrationDB)
	f := newBatchBillingFixture(t, client, 10)
	cleanupBillingUser(t, f.userID)
	_, err := integrationDB.ExecContext(ctx, `UPDATE user_subscriptions SET deleted_at = NOW() WHERE id = $1`, f.subscriptionID)
	require.NoError(t, err)
	before := readCreditBatchState(t, ctx, f)
	cmds := f.commands("credit-rollback-"+uuid.NewString(), []batchBillingStep{
		{key: 'A', balance: 1.5, keyQuota: 1.5, keyRate: 1.5},
		{key: 'B', subscription: 1},
	})
	setBatchCommandCredits(cmds)
	_, err = repo.ApplyBatch(ctx, cmds)
	require.ErrorIs(t, err, service.ErrSubscriptionNotFound)
	require.Equal(t, before, readCreditBatchState(t, ctx, f), "a later error must roll back the earlier wallet, ledger, key counters and dedup claims")
}

func TestUsageBillingRepositoryApplyBatch_MixedCreditAndShadowSnapshots(t *testing.T) {
	ctx := context.Background()
	client := testEntClient(t)
	repo := NewUsageBillingRepository(client, integrationDB)
	sequential := newBatchBillingFixture(t, client, 10)
	batched := newBatchBillingFixture(t, client, 10)
	cleanupBillingUser(t, sequential.userID)
	cleanupBillingUser(t, batched.userID)
	steps := []batchBillingStep{
		{key: 'A', balance: 1, keyQuota: 1, keyRate: 1},
		{key: 'A', balance: 1, keyQuota: 1, keyRate: 1},
		{key: 'A', balance: 1, keyQuota: 1, keyRate: 1},
	}
	seqCmds := sequential.commands("mixed-credit-seq-"+uuid.NewString(), steps)
	batchCmds := batched.commands("mixed-credit-batch-"+uuid.NewString(), steps)
	for _, cmds := range [][]*service.UsageBillingCommand{seqCmds, batchCmds} {
		setBatchCommandCredits(cmds)
		// A settings change can leave an older shadow snapshot next to credit
		// requests in the same user's queue. Each command keeps its own engine.
		cmds[1].BillingEngine = service.BillingEngineShadow
		cmds[1].ChargeCredits = service.CreditsPerUSD
		cmds[1].BalanceCredits = 0
		cmds[1].APIKeyQuotaCredits = 0
		cmds[1].APIKeyRateLimitCredits = 0
	}
	seqResults := make([]*service.UsageBillingApplyResult, len(seqCmds))
	for i, cmd := range seqCmds {
		result, err := repo.Apply(ctx, cmd)
		require.NoError(t, err)
		seqResults[i] = result
	}
	outcomes, err := repo.ApplyBatch(ctx, batchCmds)
	require.NoError(t, err)
	for i := range seqResults {
		require.NoError(t, outcomes[i].Err)
		require.Equal(t, seqResults[i], outcomes[i].Result)
	}
	state := readCreditBatchState(t, ctx, batched)
	require.Equal(t, readCreditBatchState(t, ctx, sequential), state)
	require.Equal(t, int64(700_000_000), state.BalanceCredits)
	require.Len(t, state.Ledger, 2, "shadow records its credit snapshot in usage logs but must not add a wallet ledger entry")

	// A retry after another settings change still refers to the original
	// request, rather than charging again under a different engine.
	for _, cmd := range batchCmds {
		retry := *cmd
		retry.RequestFingerprint = ""
		retry.BillingEngine = service.BillingEngineShadow
		retry.BalanceCredits = 0
		retry.APIKeyQuotaCredits = 0
		retry.APIKeyRateLimitCredits = 0
		result, err := repo.Apply(ctx, &retry)
		require.NoError(t, err)
		require.False(t, result.Applied)
	}
	require.Equal(t, state, readCreditBatchState(t, ctx, batched))
}
