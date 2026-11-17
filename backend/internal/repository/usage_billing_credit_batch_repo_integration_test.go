//go:build integration

package repository

import (
	"context"
	"math"
	"testing"
	"time"

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
	seqState := readCreditBatchState(t, ctx, sequential)
	// Quota cache invalidation is coalesced once per affected account, as in
	// the upstream USD batch path; monetary state and results remain per request.
	require.Positive(t, seqState.Legacy.pOutbox)
	require.Positive(t, seqState.Legacy.qOutbox)
	require.Equal(t, 1, state.Legacy.pOutbox)
	require.Equal(t, 1, state.Legacy.qOutbox)
	comparable := state
	seqState.Legacy.pOutbox, seqState.Legacy.qOutbox = 0, 0
	comparable.Legacy.pOutbox, comparable.Legacy.qOutbox = 0, 0
	require.Equal(t, seqState, comparable)
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

func TestUsageBillingRepositoryApplyBatch_CreditWindowsAndEnginePrecedence(t *testing.T) {
	ctx := context.Background()
	client := testEntClient(t)
	repo := NewUsageBillingRepository(client, integrationDB)
	for _, window := range []string{"expired", "unexpired", "null"} {
		t.Run(window, func(t *testing.T) {
			sequential := newBatchBillingFixture(t, client, 10)
			batched := newBatchBillingFixture(t, client, 10)
			cleanupBillingUser(t, sequential.userID)
			cleanupBillingUser(t, batched.userID)
			var start any
			switch window {
			case "expired":
				start = time.Now().UTC().Add(-9 * 24 * time.Hour)
			case "unexpired":
				start = time.Now().UTC().Add(-time.Hour)
			}
			for _, f := range []batchBillingFixture{sequential, batched} {
				_, err := integrationDB.ExecContext(ctx, `
					UPDATE api_keys SET usage_5h_credits = 7, usage_1d_credits = 7, usage_7d_credits = 7,
						window_5h_start = $1, window_1d_start = $1, window_7d_start = $1 WHERE id = $2
				`, start, f.keyA)
				require.NoError(t, err)
			}
			steps := []batchBillingStep{
				{key: 'A', balance: 1, keyQuota: 1, keyRate: 1},
				{key: 'A', balance: 1, keyQuota: 1, keyRate: 1},
				{key: 'A', balance: 1, keyQuota: 1, keyRate: 1},
				{key: 'B', subscription: 1},
			}
			seq := sequential.commands("window-seq-"+uuid.NewString(), steps)
			batch := batched.commands("window-batch-"+uuid.NewString(), steps)
			for _, cmds := range [][]*service.UsageBillingCommand{seq, batch} {
				setBatchCommandCredits(cmds)
				// Explicit credits are authoritative even when the USD snapshot
				// differs. Shadow/legacy subscription and Key counters use USD;
				// an explicit wallet amount has precedence regardless of engine.
				cmds[0].BalanceCredits, cmds[0].APIKeyQuotaCredits, cmds[0].APIKeyRateLimitCredits = 3, 4, 9
				cmds[1].BillingEngine = service.BillingEngineShadow
				cmds[1].BalanceCredits, cmds[1].APIKeyQuotaCredits, cmds[1].APIKeyRateLimitCredits = 0, 4, 9
				cmds[2].BillingEngine = service.BillingEngineLegacy
				cmds[2].BalanceCredits, cmds[2].APIKeyQuotaCredits, cmds[2].APIKeyRateLimitCredits = 5, 4, 9
				cmds[3].SubscriptionCredits = 11
			}
			want := make([]*service.UsageBillingApplyResult, len(seq))
			for i, cmd := range seq {
				var err error
				want[i], err = repo.Apply(ctx, cmd)
				require.NoError(t, err)
			}
			outcomes, err := repo.ApplyBatch(ctx, batch)
			require.NoError(t, err)
			for i, out := range outcomes {
				require.NoError(t, out.Err)
				require.Equal(t, want[i], out.Result)
			}
			state := readCreditBatchState(t, ctx, batched)
			require.Equal(t, readCreditBatchState(t, ctx, sequential), state)
			wantWindow := int64(200_000_009)
			if window != "expired" {
				wantWindow += 7 // NULL starts retain usage, exactly like single settlement.
			}
			require.Equal(t, wantWindow, state.KeyA5h)
			require.Equal(t, wantWindow, state.KeyA1d)
			require.Equal(t, wantWindow, state.KeyA7d)
			require.Equal(t, int64(11), state.SubscriptionDaily)
			var engine, usd, exchange string
			var scale, version int64
			require.NoError(t, integrationDB.QueryRowContext(ctx, `
				SELECT metadata->>'engine', metadata->>'usd_cost', metadata->>'usd_to_cny',
					(metadata->>'credits_per_usd')::bigint, (metadata->>'billing_version')::bigint
				FROM billing_ledger WHERE user_id = $1 AND request_id = $2
			`, batched.userID, batch[0].RequestID).Scan(&engine, &usd, &exchange, &scale, &version))
			require.Equal(t, service.BillingEngineCredits, engine)
			require.Equal(t, "1", usd)
			require.Equal(t, "7.2", exchange)
			require.Equal(t, service.CreditsPerUSD, scale)
			require.Equal(t, int64(1), version)
		})
	}
}

func TestUsageBillingRepositoryApplyBatch_CreditAggregateBeyondInt64(t *testing.T) {
	ctx := context.Background()
	client := testEntClient(t)
	repo := NewUsageBillingRepository(client, integrationDB)
	seq := newBatchBillingFixture(t, client, 10)
	batch := newBatchBillingFixture(t, client, 10)
	cleanupBillingUser(t, seq.userID)
	cleanupBillingUser(t, batch.userID)
	var expected []*service.UsageBillingApplyResult
	for _, f := range []batchBillingFixture{seq, batch} {
		_, err := integrationDB.ExecContext(ctx, `UPDATE users SET balance_credits = $1 WHERE id = $2`, int64(math.MaxInt64), f.userID)
		require.NoError(t, err)
		cmds := f.commands("large-credit-"+uuid.NewString(), []batchBillingStep{{key: 'A', balance: 1}, {key: 'B', balance: 1}})
		setBatchCommandCredits(cmds)
		for _, cmd := range cmds {
			cmd.BalanceCredits = math.MaxInt64/2 + 1
		}
		if f.userID == seq.userID {
			for _, cmd := range cmds {
				result, err := repo.Apply(ctx, cmd)
				require.NoError(t, err)
				expected = append(expected, result)
			}
		} else {
			outcomes, err := repo.ApplyBatch(ctx, cmds)
			require.NoError(t, err)
			for i, out := range outcomes {
				require.NoError(t, out.Err)
				require.Equal(t, expected[i], out.Result)
			}
		}
	}
	state := readCreditBatchState(t, ctx, batch)
	require.Equal(t, readCreditBatchState(t, ctx, seq), state)
	require.Equal(t, int64(-1), state.BalanceCredits)
	require.Equal(t, int64(2), state.BalanceVersion)
	require.Len(t, state.Ledger, 2)
}

func TestUsageBillingRepositoryApplyBatch_CreditOverflowRollsBack(t *testing.T) {
	ctx := context.Background()
	client := testEntClient(t)
	repo := NewUsageBillingRepository(client, integrationDB)
	for _, target := range []string{"balance", "version", "subscription", "quota", "rate", "negative"} {
		t.Run(target, func(t *testing.T) {
			f := newBatchBillingFixture(t, client, 10)
			cleanupBillingUser(t, f.userID)
			cmds := f.commands("credit-overflow-"+uuid.NewString(), []batchBillingStep{
				{key: 'A', balance: 0.00000001, keyQuota: 0.00000001, keyRate: 0.00000001},
				{key: 'B', balance: 0.00000001},
			})
			setBatchCommandCredits(cmds)
			var err error
			switch target {
			case "balance":
				_, err = integrationDB.ExecContext(ctx, `UPDATE users SET balance_credits = $1 WHERE id = $2`, int64(math.MinInt64+1), f.userID)
			case "version":
				_, err = integrationDB.ExecContext(ctx, `UPDATE users SET balance_credits_version = $1 WHERE id = $2`, int64(math.MaxInt64), f.userID)
			case "subscription":
				_, err = integrationDB.ExecContext(ctx, `UPDATE user_subscriptions SET daily_usage_credits = $1 WHERE id = $2`, int64(999_999_999_999_999_999), f.subscriptionID)
				cmds[1].SubscriptionID, cmds[1].SubscriptionCredits = &f.subscriptionID, 1
			case "quota":
				_, err = integrationDB.ExecContext(ctx, `UPDATE api_keys SET quota_used_credits = $1 WHERE id = $2`, int64(math.MaxInt64), f.keyA)
			case "rate":
				_, err = integrationDB.ExecContext(ctx, `UPDATE api_keys SET usage_5h_credits = $1 WHERE id = $2`, int64(math.MaxInt64), f.keyA)
			case "negative":
				cmds[1].BalanceCredits = -1
			}
			require.NoError(t, err)
			before := readCreditBatchState(t, ctx, f)
			_, err = repo.ApplyBatch(ctx, cmds)
			require.Error(t, err)
			require.Equal(t, before, readCreditBatchState(t, ctx, f), "failed settlement must not claim requests, change counters, or append a ledger")
		})
	}
}

func TestUsageBillingRepositoryApplyBatch_CreditsWithDeletedKey(t *testing.T) {
	ctx := context.Background()
	client := testEntClient(t)
	repo := NewUsageBillingRepository(client, integrationDB)
	f := newBatchBillingFixture(t, client, 10)
	cleanupBillingUser(t, f.userID)
	_, err := integrationDB.ExecContext(ctx, `UPDATE api_keys SET deleted_at = NOW() WHERE id = $1`, f.keyA)
	require.NoError(t, err)
	cmds := f.commands("deleted-credit-key-"+uuid.NewString(), []batchBillingStep{
		{key: 'A', balance: 1, keyQuota: 1, keyRate: 1},
		{key: 'B', balance: 1, keyRate: 1},
	})
	setBatchCommandCredits(cmds)
	outcomes, err := repo.ApplyBatch(ctx, cmds)
	require.NoError(t, err)
	for _, out := range outcomes {
		require.NoError(t, out.Err)
		require.True(t, out.Result.Applied)
	}
	state := readCreditBatchState(t, ctx, f)
	require.Equal(t, int64(800_000_000), state.BalanceCredits)
	require.Equal(t, int64(2), state.BalanceVersion)
	require.Zero(t, state.KeyAQuota)
	require.Zero(t, state.KeyA5h)
	require.Equal(t, int64(100_000_000), state.KeyB5h)
	require.Len(t, state.Ledger, 2)
}
