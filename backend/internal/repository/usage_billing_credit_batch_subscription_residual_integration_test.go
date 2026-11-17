//go:build integration

package repository

import (
	"context"
	"strings"
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"

	"github.com/Wei-Shaw/sub2api/internal/service"
)

// Historical subscription USD counters have ten decimal places, while the
// integer ledger represents eight. USD/shadow charges retain an existing
// remainder; an explicit credit charge clears it at that point in the sequence.
func TestUsageBillingCreditBatch_SubscriptionUSDResidualMatchesSequential(t *testing.T) {
	ctx := context.Background()
	client := testEntClient(t)
	repo := NewUsageBillingRepository(client, integrationDB)
	for _, seed := range []struct {
		name                   string
		daily, weekly, monthly string
	}{
		{"positive fractional credits", "0.1234567891", "0.2345678949", "0.3456789050"},
		{"negative half credits across zero", "-0.0000000050", "-0.0000000150", "-0.0000000250"},
	} {
		for _, sequence := range []string{"LL", "LSL", "CCC", "CLS", "LCS", "LSC", "LCSCL"} {
			t.Run(seed.name+"/"+sequence, func(t *testing.T) {
				seq := newBatchBillingFixture(t, client, 100)
				batch := newBatchBillingFixture(t, client, 100)
				cleanupBillingUser(t, seq.userID)
				cleanupBillingUser(t, batch.userID)
				for _, f := range []batchBillingFixture{seq, batch} {
					_, err := integrationDB.ExecContext(ctx, `
						UPDATE user_subscriptions SET daily_usage_usd = $1::numeric,
							weekly_usage_usd = $2::numeric, monthly_usage_usd = $3::numeric
						WHERE id = $4
					`, seed.daily, seed.weekly, seed.monthly, f.subscriptionID)
					require.NoError(t, err)
				}
				commands := func(f batchBillingFixture) []*service.UsageBillingCommand {
					steps := make([]batchBillingStep, len(sequence))
					for i := range steps {
						steps[i] = batchBillingStep{key: 'A', account: 'P', subscription: 0.00000001}
					}
					cmds := f.commands("credit-sub-residual-"+uuid.NewString(), steps)
					for i, kind := range sequence {
						switch kind {
						case 'C':
							cmds[i].BillingEngine = service.BillingEngineCredits
							cmds[i].SubscriptionCredits = 1
						case 'S':
							cmds[i].BillingEngine = service.BillingEngineShadow
							// Shadow must ignore its sampled credit amount.
							cmds[i].SubscriptionCredits = 99
						}
					}
					return cmds
				}
				seqCmds, batchCmds := commands(seq), commands(batch)
				results := make([]*service.UsageBillingApplyResult, len(seqCmds))
				for i, cmd := range seqCmds {
					var err error
					results[i], err = repo.Apply(ctx, cmd)
					require.NoError(t, err)
				}
				outcomes, err := repo.ApplyBatch(ctx, batchCmds)
				require.NoError(t, err)
				for i, out := range outcomes {
					require.NoError(t, out.Err)
					require.Equal(t, results[i], out.Result)
				}
				got := readCreditBatchState(t, ctx, batch)
				require.Equal(t, readCreditBatchState(t, ctx, seq), got)
				if seed.name == "positive fractional credits" && sequence == "LSL" {
					require.Equal(t, "0.1234568191", got.Legacy.subDaily, "USD/shadow updates must retain the historical sub-credit remainder")
				}
				if seed.name == "negative half credits across zero" && sequence == "LCS" {
					require.Equal(t, "0.0000000300", got.Legacy.subDaily, "rounding the USD prefix before its first credit charge must match PostgreSQL")
				}
				if strings.Contains(sequence, "C") {
					var exactProjection bool
					require.NoError(t, integrationDB.QueryRowContext(ctx, `
						SELECT daily_usage_usd = daily_usage_credits::numeric / 100000000
							AND weekly_usage_usd = weekly_usage_credits::numeric / 100000000
							AND monthly_usage_usd = monthly_usage_credits::numeric / 100000000
						FROM user_subscriptions WHERE id = $1
					`, batch.subscriptionID).Scan(&exactProjection))
					require.True(t, exactProjection, "the first explicit credit charge clears all old USD remainders")
				}
			})
		}
	}
}
