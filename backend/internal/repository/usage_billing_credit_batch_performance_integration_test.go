//go:build integration

package repository

import (
	"context"
	"fmt"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"

	"github.com/Wei-Shaw/sub2api/internal/service"
)

func creditBatchPerformanceCommands(f batchBillingFixture, count int) []*service.UsageBillingCommand {
	steps := make([]batchBillingStep, count)
	for i := range steps {
		// Exercising both funding buckets in a repository command is deliberate:
		// this checks aggregation of every affected row using one shared fixture.
		steps[i] = batchBillingStep{
			key: 'A', account: 'P', balance: 0.01, subscription: 0.02,
			keyQuota: 0.01, keyRate: 0.01, accountQuota: 0.01,
		}
	}
	cmds := f.commands("credit-perf-"+uuid.NewString(), steps)
	setBatchCommandCredits(cmds)
	return cmds
}

// applyCreditBatchWithPerCommandEffects is the old compatibility baseline:
// batch deduplication and one transaction, but an UPDATE/ledger INSERT for every
// command. It is test-only and intentionally shares the authoritative single
// command effects, so the comparison checks behavior as well as elapsed time.
func applyCreditBatchWithPerCommandEffects(ctx context.Context, repo *usageBillingRepository, cmds []*service.UsageBillingCommand) (_ []service.UsageBillingBatchOutcome, err error) {
	if _, err := validateUsageBillingBatch(cmds); err != nil {
		return nil, err
	}
	tx, err := repo.db.BeginTx(ctx, nil)
	if err != nil {
		return nil, err
	}
	defer func() { _ = tx.Rollback() }()
	outcomes := make([]service.UsageBillingBatchOutcome, len(cmds))
	applied, err := claimUsageBillingBatch(ctx, tx, cmds, outcomes)
	if err != nil {
		return nil, err
	}
	for i, cmd := range cmds {
		if !applied[i] {
			continue
		}
		outcomes[i].Result = &service.UsageBillingApplyResult{Applied: true}
		if err := repo.applyUsageBillingEffects(ctx, tx, cmd, outcomes[i].Result); err != nil {
			return nil, err
		}
	}
	if err := tx.Commit(); err != nil {
		return nil, err
	}
	return outcomes, nil
}

func TestUsageBillingCreditBatch_ThreeSettlementPathsHaveSameResults(t *testing.T) {
	ctx := context.Background()
	client := testEntClient(t)
	repo := &usageBillingRepository{db: integrationDB}
	const count = 128
	var referenceState *creditBatchState
	var referenceResults []*service.UsageBillingApplyResult
	for _, mode := range []string{"separate transactions", "one transaction with per-command effects", "aggregated SQL"} {
		f := newBatchBillingFixture(t, client, 100)
		cleanupBillingUser(t, f.userID)
		cmds := creditBatchPerformanceCommands(f, count)
		results := make([]*service.UsageBillingApplyResult, count)
		started := time.Now()
		if mode == "separate transactions" {
			for i, cmd := range cmds {
				result, err := repo.Apply(ctx, cmd)
				require.NoError(t, err)
				results[i] = result
			}
		} else {
			var outcomes []service.UsageBillingBatchOutcome
			var err error
			if mode == "one transaction with per-command effects" {
				outcomes, err = applyCreditBatchWithPerCommandEffects(ctx, repo, cmds)
			} else {
				outcomes, err = repo.ApplyBatch(ctx, cmds)
			}
			require.NoError(t, err)
			require.Len(t, outcomes, count)
			for i, out := range outcomes {
				require.NoError(t, out.Err)
				results[i] = out.Result
			}
		}
		elapsed := time.Since(started)
		// These are uninstrumented local observations, not a production SLA or
		// a stable speedup assertion. Fixture setup and state reads are excluded.
		t.Logf("credit settlement: mode=%q commands=%d elapsed=%s average=%s", mode, count, elapsed, elapsed/time.Duration(count))
		state := readCreditBatchState(t, ctx, f)
		if referenceState == nil {
			referenceState = &state
			referenceResults = results
		} else {
			require.Equal(t, *referenceState, state, "%s must preserve all stored balances, versions, counters and ledger rows", mode)
			require.Equal(t, referenceResults, results, "%s must preserve each request's result", mode)
		}
	}
}

// instrumentCreditBatchWrites installs observers scoped to this fixture's IDs.
// Row triggers count actual UPDATEd rows; a statement trigger with a transition
// table counts ledger INSERT statements and inserted rows separately. The
// observers are removed before fixture cleanup and do not count other tests.
func instrumentCreditBatchWrites(t *testing.T, f batchBillingFixture) func() map[string]int64 {
	t.Helper()
	ctx := context.Background()
	base := "cb_" + strings.ReplaceAll(uuid.NewString(), "-", "")
	countTable := base + "_counts"
	rowFunction := base + "_count_update"
	ledgerFunction := base + "_count_ledger"
	cleanup := []string{}
	t.Cleanup(func() {
		for i := len(cleanup) - 1; i >= 0; i-- {
			if _, err := integrationDB.ExecContext(ctx, cleanup[i]); err != nil {
				t.Errorf("remove credit write observer: %v", err)
			}
		}
	})
	_, err := integrationDB.ExecContext(ctx, fmt.Sprintf(`CREATE TABLE %s (kind text PRIMARY KEY, amount bigint NOT NULL)`, countTable))
	require.NoError(t, err)
	cleanup = append(cleanup, fmt.Sprintf(`DROP TABLE IF EXISTS %s`, countTable))
	_, err = integrationDB.ExecContext(ctx, fmt.Sprintf(`
		CREATE FUNCTION %s() RETURNS trigger LANGUAGE plpgsql AS $observer$
		BEGIN
			INSERT INTO %s(kind, amount) VALUES (TG_ARGV[0], 1)
			ON CONFLICT (kind) DO UPDATE SET amount = %s.amount + EXCLUDED.amount;
			RETURN NEW;
		END;
		$observer$;
	`, rowFunction, countTable, countTable))
	require.NoError(t, err)
	cleanup = append(cleanup, fmt.Sprintf(`DROP FUNCTION IF EXISTS %s()`, rowFunction))
	for _, target := range []struct {
		table, kind, condition string
	}{
		{"users", "wallet_updates", fmt.Sprintf("NEW.id = %d", f.userID)},
		{"api_keys", "key_updates", fmt.Sprintf("NEW.id IN (%d, %d)", f.keyA, f.keyB)},
		{"user_subscriptions", "subscription_updates", fmt.Sprintf("NEW.id = %d", f.subscriptionID)},
		{"accounts", "account_updates", fmt.Sprintf("NEW.id IN (%d, %d)", f.accountP, f.accountQ)},
	} {
		name := base + "_" + target.kind
		_, err := integrationDB.ExecContext(ctx, fmt.Sprintf(`
			CREATE TRIGGER %s AFTER UPDATE ON %s FOR EACH ROW WHEN (%s)
			EXECUTE FUNCTION %s('%s')
		`, name, target.table, target.condition, rowFunction, target.kind))
		require.NoError(t, err)
		cleanup = append(cleanup, fmt.Sprintf(`DROP TRIGGER IF EXISTS %s ON %s`, name, target.table))
	}
	_, err = integrationDB.ExecContext(ctx, fmt.Sprintf(`
		CREATE FUNCTION %s() RETURNS trigger LANGUAGE plpgsql AS $observer$
		DECLARE inserted_count bigint;
		BEGIN
			SELECT count(*) INTO inserted_count FROM inserted_credit_rows
			WHERE user_id = %d AND reference_type = 'usage';
			IF inserted_count > 0 THEN
				INSERT INTO %s(kind, amount) VALUES ('ledger_statements', 1), ('ledger_rows', inserted_count)
				ON CONFLICT (kind) DO UPDATE SET amount = %s.amount + EXCLUDED.amount;
			END IF;
			RETURN NULL;
		END;
		$observer$;
	`, ledgerFunction, f.userID, countTable, countTable))
	require.NoError(t, err)
	cleanup = append(cleanup, fmt.Sprintf(`DROP FUNCTION IF EXISTS %s()`, ledgerFunction))
	ledgerTrigger := base + "_ledger"
	_, err = integrationDB.ExecContext(ctx, fmt.Sprintf(`
		CREATE TRIGGER %s AFTER INSERT ON billing_ledger
		REFERENCING NEW TABLE AS inserted_credit_rows
		FOR EACH STATEMENT EXECUTE FUNCTION %s()
	`, ledgerTrigger, ledgerFunction))
	require.NoError(t, err)
	cleanup = append(cleanup, fmt.Sprintf(`DROP TRIGGER IF EXISTS %s ON billing_ledger`, ledgerTrigger))
	return func() map[string]int64 {
		rows, err := integrationDB.QueryContext(ctx, fmt.Sprintf(`SELECT kind, amount FROM %s`, countTable))
		require.NoError(t, err)
		defer func() { _ = rows.Close() }()
		counts := make(map[string]int64)
		for rows.Next() {
			var kind string
			var amount int64
			require.NoError(t, rows.Scan(&kind, &amount))
			counts[kind] = amount
		}
		require.NoError(t, rows.Err())
		return counts
	}
}

func TestUsageBillingCreditBatch_AggregatesPhysicalDatabaseWrites(t *testing.T) {
	ctx := context.Background()
	client := testEntClient(t)
	repo := NewUsageBillingRepository(client, integrationDB)
	f := newBatchBillingFixture(t, client, 100)
	cleanupBillingUser(t, f.userID)
	const count = 128
	cmds := creditBatchPerformanceCommands(f, count)
	readCounts := instrumentCreditBatchWrites(t, f)
	outcomes, err := repo.ApplyBatch(ctx, cmds)
	require.NoError(t, err)
	require.Len(t, outcomes, count)
	for _, out := range outcomes {
		require.NoError(t, out.Err)
		require.True(t, out.Result.Applied)
	}
	want := map[string]int64{
		"wallet_updates": 1, "key_updates": 1, "subscription_updates": 1,
		"account_updates": 1, "ledger_statements": 1, "ledger_rows": count,
	}
	require.Equal(t, want, readCounts(), "physical database writes must be aggregated, not just wrapped in one transaction")
	t.Logf("credit SQL aggregation: commands=%d actual_writes=%v", count, readCounts())
	outcomes, err = repo.ApplyBatch(ctx, cmds)
	require.NoError(t, err)
	for _, out := range outcomes {
		require.NoError(t, out.Err)
		require.False(t, out.Result.Applied)
	}
	require.Equal(t, want, readCounts(), "deduplicated retries must not update counters or append ledger rows")
}

func TestUsageBillingCreditLane_ThreeHundredConcurrentRequests(t *testing.T) {
	ctx := context.Background()
	client := testEntClient(t)
	repo := NewUsageBillingRepository(client, integrationDB)
	lanes := service.NewUsageBillingLaneCombiner(repo)
	defer lanes.Stop()
	f := newBatchBillingFixture(t, client, 100)
	cleanupBillingUser(t, f.userID)
	const count = 300
	cmds := creditBatchPerformanceCommands(f, count)
	results := make([]*service.UsageBillingApplyResult, count)
	errs := make([]error, count)
	var wg sync.WaitGroup
	start := make(chan struct{})
	for i, cmd := range cmds {
		wg.Add(1)
		go func(i int, cmd *service.UsageBillingCommand) {
			defer wg.Done()
			<-start
			callCtx, cancel := context.WithTimeout(ctx, 45*time.Second)
			defer cancel()
			results[i], errs[i] = lanes.Apply(callCtx, cmd)
		}(i, cmd)
	}
	started := time.Now()
	close(start)
	wg.Wait()
	elapsed := time.Since(started)
	seen := make(map[int64]struct{}, count)
	for i, result := range results {
		require.NoError(t, errs[i], "request %d", i)
		require.NotNil(t, result)
		require.True(t, result.Applied)
		require.False(t, result.BalanceOverdrafted)
		require.NotNil(t, result.NewBalanceCredits)
		_, duplicate := seen[*result.NewBalanceCredits]
		require.False(t, duplicate, "every request must receive its own post-charge balance")
		seen[*result.NewBalanceCredits] = struct{}{}
	}
	state := readCreditBatchState(t, ctx, f)
	require.Equal(t, int64(9_700_000_000), state.BalanceCredits)
	require.Equal(t, int64(count), state.BalanceVersion)
	require.Equal(t, int64(600_000_000), state.SubscriptionDaily)
	require.Equal(t, int64(300_000_000), state.KeyAQuota)
	require.Equal(t, int64(300_000_000), state.KeyA5h)
	require.Len(t, state.Ledger, count)
	require.Equal(t, count, state.Legacy.dedupRows)
	stats := lanes.Stats()
	require.Positive(t, stats.Batches)
	require.Zero(t, stats.BatchFallbacks)
	require.Equal(t, uint64(count), stats.SingleApplies+stats.BatchedCommands)
	t.Logf("credit lane concurrent settlement: commands=%d elapsed=%s stats=%+v", count, elapsed, stats)
}
