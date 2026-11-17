package repository

import (
	"context"
	"database/sql"
	"errors"
	"sort"

	"github.com/lib/pq"
	"github.com/shopspring/decimal"

	"github.com/Wei-Shaw/sub2api/internal/service"
)

// All USD command amounts have already been quantized to eight places by
// Normalize. Converting that decimal to the fixed credit scale is exact, so
// legacy, shadow and credit snapshots can share the same per-row aggregation.
// Never sum credits in int64: a valid wallet debit can span more than MaxInt64
// credits when moving from a positive balance into the allowed overdraft range.
// SQL does the arithmetic in numeric and casts the stored result to bigint,
// preserving PostgreSQL's overflow checks for the authoritative columns.
func usageBillingCommandCredits(engine string, credits int64, usd float64) decimal.Decimal {
	if engine == service.BillingEngineCredits && credits > 0 {
		return decimal.NewFromInt(credits)
	}
	if usd > 0 {
		return usageBillingAmount(usd).Shift(service.UsageBillingMonetaryScale)
	}
	return decimal.Zero
}

func usageBillingWalletCredits(cmd *service.UsageBillingCommand) decimal.Decimal {
	// The original wallet path gives explicit BalanceCredits precedence even
	// when a command does not carry an engine label.
	if cmd.BalanceCredits > 0 {
		return decimal.NewFromInt(cmd.BalanceCredits)
	}
	return usageBillingCommandCredits("", 0, cmd.BalanceCost)
}

type usageBillingCreditGroup struct {
	sum     decimal.Decimal
	indexes []int
}

func groupUsageBillingCredits(cmds []*service.UsageBillingCommand, applied []bool, pick func(*service.UsageBillingCommand) (int64, decimal.Decimal, bool)) (map[int64]*usageBillingCreditGroup, []int64) {
	groups := make(map[int64]*usageBillingCreditGroup)
	for i, cmd := range cmds {
		if !applied[i] {
			continue
		}
		id, amount, ok := pick(cmd)
		if !ok || !amount.IsPositive() {
			continue
		}
		group := groups[id]
		if group == nil {
			group = &usageBillingCreditGroup{}
			groups[id] = group
		}
		group.sum = group.sum.Add(amount)
		group.indexes = append(group.indexes, i)
	}
	ids := make([]int64, 0, len(groups))
	for id := range groups {
		ids = append(ids, id)
	}
	sort.Slice(ids, func(i, j int) bool { return ids[i] < ids[j] })
	return groups, ids
}

func applyUsageBillingBatchSubscriptions(ctx context.Context, tx *sql.Tx, cmds []*service.UsageBillingCommand, applied []bool) error {
	groups, ids := groupUsageBillingCredits(cmds, applied, func(cmd *service.UsageBillingCommand) (int64, decimal.Decimal, bool) {
		if cmd.SubscriptionID == nil {
			return 0, decimal.Zero, false
		}
		return *cmd.SubscriptionID, usageBillingCommandCredits(cmd.BillingEngine, cmd.SubscriptionCredits, cmd.SubscriptionCost), true
	})
	for _, id := range ids {
		group := groups[id]
		// Subscription USD counters have ten fractional places. An old USD
		// charge can therefore leave a sub-credit remainder, which subsequent
		// USD/shadow charges preserve. The first explicit credit charge instead
		// projects the rounded credit counter back to USD and clears that
		// remainder. Keep that order-dependent behavior while issuing one UPDATE.
		prefixUSD, afterCredits := decimal.Zero, decimal.Zero
		hasExplicitCredits := false
		for _, i := range group.indexes {
			cmd := cmds[i]
			if cmd.BillingEngine == service.BillingEngineCredits && cmd.SubscriptionCredits > 0 {
				hasExplicitCredits = true
			}
			if hasExplicitCredits {
				afterCredits = afterCredits.Add(usageBillingCommandCredits(cmd.BillingEngine, cmd.SubscriptionCredits, cmd.SubscriptionCost))
			} else {
				prefixUSD = prefixUSD.Add(usageBillingAmount(cmd.SubscriptionCost))
			}
		}
		var res sql.Result
		var err error
		if !hasExplicitCredits {
			// Only change USD here: the existing bidirectional trigger derives
			// rounded credit counters without dropping the old USD remainder.
			res, err = tx.ExecContext(ctx, `
				UPDATE user_subscriptions us
				SET daily_usage_usd = us.daily_usage_usd + $1::numeric,
					weekly_usage_usd = us.weekly_usage_usd + $1::numeric,
					monthly_usage_usd = us.monthly_usage_usd + $1::numeric,
					updated_at = NOW()
				FROM groups g
				WHERE us.id = $2 AND us.deleted_at IS NULL
					AND us.group_id = g.id AND g.deleted_at IS NULL
			`, prefixUSD, id)
		} else {
			// Round the USD prefix at the same point as sequential settlement.
			// This also preserves PostgreSQL rounding when a negative historical
			// counter crosses zero at a half-credit boundary before the first
			// explicit credit charge.
			res, err = tx.ExecContext(ctx, `
			UPDATE user_subscriptions us
			SET daily_usage_credits = ((CASE WHEN $1::numeric > 0 THEN ROUND((us.daily_usage_usd + $1::numeric) * 100000000)
					ELSE us.daily_usage_credits::numeric END) + $2::numeric)::bigint,
				weekly_usage_credits = ((CASE WHEN $1::numeric > 0 THEN ROUND((us.weekly_usage_usd + $1::numeric) * 100000000)
					ELSE us.weekly_usage_credits::numeric END) + $2::numeric)::bigint,
				monthly_usage_credits = ((CASE WHEN $1::numeric > 0 THEN ROUND((us.monthly_usage_usd + $1::numeric) * 100000000)
					ELSE us.monthly_usage_credits::numeric END) + $2::numeric)::bigint,
				daily_usage_usd = ((CASE WHEN $1::numeric > 0 THEN ROUND((us.daily_usage_usd + $1::numeric) * 100000000)
					ELSE us.daily_usage_credits::numeric END) + $2::numeric) / 100000000,
				weekly_usage_usd = ((CASE WHEN $1::numeric > 0 THEN ROUND((us.weekly_usage_usd + $1::numeric) * 100000000)
					ELSE us.weekly_usage_credits::numeric END) + $2::numeric) / 100000000,
				monthly_usage_usd = ((CASE WHEN $1::numeric > 0 THEN ROUND((us.monthly_usage_usd + $1::numeric) * 100000000)
					ELSE us.monthly_usage_credits::numeric END) + $2::numeric) / 100000000,
				updated_at = NOW()
			FROM groups g
			WHERE us.id = $3 AND us.deleted_at IS NULL
				AND us.group_id = g.id AND g.deleted_at IS NULL
			`, prefixUSD, afterCredits, id)
		}
		if err != nil {
			return err
		}
		affected, err := res.RowsAffected()
		if err != nil {
			return err
		}
		if affected == 0 {
			return service.ErrSubscriptionNotFound
		}
	}
	return nil
}

func applyUsageBillingBatchBalance(ctx context.Context, tx *sql.Tx, userID int64, cmds []*service.UsageBillingCommand, applied []bool, outcomes []service.UsageBillingBatchOutcome) error {
	groups, _ := groupUsageBillingCredits(cmds, applied, func(cmd *service.UsageBillingCommand) (int64, decimal.Decimal, bool) {
		return userID, usageBillingWalletCredits(cmd), true
	})
	group := groups[userID]
	if group == nil {
		return nil
	}
	var versionDelta int64
	for _, i := range group.indexes {
		if cmds[i].BalanceCredits > 0 {
			versionDelta++
		}
	}
	var remaining decimal.Decimal
	err := tx.QueryRowContext(ctx, `
		UPDATE users
		SET balance_credits = (balance_credits::numeric - $1::numeric)::bigint,
			balance = (balance_credits::numeric - $1::numeric) / 100000000,
			balance_credits_version = balance_credits_version + $3::bigint,
			updated_at = NOW()
		WHERE id = $2 AND deleted_at IS NULL
		RETURNING balance_credits
	`, group.sum, userID, versionDelta).Scan(&remaining)
	if errors.Is(err, sql.ErrNoRows) {
		return service.ErrUserNotFound
	}
	if err != nil {
		return err
	}
	// Positive deltas make all intermediate balances monotone. Once the old
	// and final bigint balances are valid, every per-request balance is too.
	running := remaining.Add(group.sum)
	for _, i := range group.indexes {
		amount := usageBillingWalletCredits(cmds[i])
		sufficient := running.GreaterThanOrEqual(amount)
		running = running.Sub(amount)
		balance := running.Shift(-service.UsageBillingMonetaryScale).InexactFloat64()
		outcomes[i].Result.NewBalance = &balance
		outcomes[i].Result.BalanceOverdrafted = !sufficient
		if cmds[i].BalanceCredits > 0 {
			credits := running.IntPart()
			outcomes[i].Result.NewBalanceCredits = &credits
		}
	}
	return nil
}

// Quota and rate windows share one UPDATE per Key. Writing both the integer
// columns and their exact USD projections keeps the bidirectional sync trigger
// deterministic even when a batch mixes snapshots from different engines.
const usageBillingBatchAPIKeyCreditsUpdate = `
	UPDATE api_keys
	SET quota_used_credits = (quota_used_credits::numeric + $1::numeric)::bigint,
		quota_used = (quota_used_credits::numeric + $1::numeric) / 100000000,
		status = CASE
			WHEN $1::numeric > 0 AND quota_credits > 0 AND status = $4
				AND quota_used_credits < quota_credits
				AND quota_used_credits::numeric + $1::numeric >= quota_credits
			THEN $5 ELSE status END,
		usage_5h_credits = CASE WHEN $2::numeric > 0 THEN
			(CASE WHEN window_5h_start IS NOT NULL AND window_5h_start + INTERVAL '5 hours' <= NOW()
			THEN $2::numeric ELSE usage_5h_credits::numeric + $2::numeric END)::bigint
			ELSE usage_5h_credits END,
		usage_1d_credits = CASE WHEN $2::numeric > 0 THEN
			(CASE WHEN window_1d_start IS NOT NULL AND window_1d_start + INTERVAL '24 hours' <= NOW()
			THEN $2::numeric ELSE usage_1d_credits::numeric + $2::numeric END)::bigint
			ELSE usage_1d_credits END,
		usage_7d_credits = CASE WHEN $2::numeric > 0 THEN
			(CASE WHEN window_7d_start IS NOT NULL AND window_7d_start + INTERVAL '7 days' <= NOW()
			THEN $2::numeric ELSE usage_7d_credits::numeric + $2::numeric END)::bigint
			ELSE usage_7d_credits END,
		usage_5h = CASE WHEN $2::numeric > 0 THEN
			(CASE WHEN window_5h_start IS NOT NULL AND window_5h_start + INTERVAL '5 hours' <= NOW()
			THEN $2::numeric ELSE usage_5h_credits::numeric + $2::numeric END) / 100000000
			ELSE usage_5h END,
		usage_1d = CASE WHEN $2::numeric > 0 THEN
			(CASE WHEN window_1d_start IS NOT NULL AND window_1d_start + INTERVAL '24 hours' <= NOW()
			THEN $2::numeric ELSE usage_1d_credits::numeric + $2::numeric END) / 100000000
			ELSE usage_1d END,
		usage_7d = CASE WHEN $2::numeric > 0 THEN
			(CASE WHEN window_7d_start IS NOT NULL AND window_7d_start + INTERVAL '7 days' <= NOW()
			THEN $2::numeric ELSE usage_7d_credits::numeric + $2::numeric END) / 100000000
			ELSE usage_7d END,
		window_5h_start = CASE WHEN $2::numeric > 0 AND (window_5h_start IS NULL OR window_5h_start + INTERVAL '5 hours' <= NOW())
			THEN NOW() ELSE window_5h_start END,
		window_1d_start = CASE WHEN $2::numeric > 0 AND (window_1d_start IS NULL OR window_1d_start + INTERVAL '24 hours' <= NOW())
			THEN date_trunc('day', NOW()) ELSE window_1d_start END,
		window_7d_start = CASE WHEN $2::numeric > 0 AND (window_7d_start IS NULL OR window_7d_start + INTERVAL '7 days' <= NOW())
			THEN date_trunc('day', NOW()) ELSE window_7d_start END,
		updated_at = NOW()
	WHERE id = $3 AND deleted_at IS NULL
	RETURNING quota_credits, quota_used_credits
`

func applyUsageBillingBatchAPIKeys(ctx context.Context, tx *sql.Tx, cmds []*service.UsageBillingCommand, applied []bool, outcomes []service.UsageBillingBatchOutcome) error {
	quotaGroups, quotaIDs := groupUsageBillingCredits(cmds, applied, func(cmd *service.UsageBillingCommand) (int64, decimal.Decimal, bool) {
		return cmd.APIKeyID, usageBillingCommandCredits(cmd.BillingEngine, cmd.APIKeyQuotaCredits, cmd.APIKeyQuotaCost), true
	})
	rateGroups, rateIDs := groupUsageBillingCredits(cmds, applied, func(cmd *service.UsageBillingCommand) (int64, decimal.Decimal, bool) {
		return cmd.APIKeyID, usageBillingCommandCredits(cmd.BillingEngine, cmd.APIKeyRateLimitCredits, cmd.APIKeyRateLimitCost), true
	})
	for _, id := range mergeSortedIDs(quotaIDs, rateIDs) {
		quotaSum, rateSum := decimal.Zero, decimal.Zero
		if group := quotaGroups[id]; group != nil {
			quotaSum = group.sum
		}
		if group := rateGroups[id]; group != nil {
			rateSum = group.sum
		}
		var limit, used decimal.Decimal
		err := tx.QueryRowContext(ctx, usageBillingBatchAPIKeyCreditsUpdate,
			quotaSum, rateSum, id, service.StatusAPIKeyActive, service.StatusAPIKeyQuotaExhausted,
		).Scan(&limit, &used)
		if errors.Is(err, sql.ErrNoRows) {
			// A deleted Key does not prevent wallet/subscription settlement.
			continue
		}
		if err != nil {
			return err
		}
		if group := quotaGroups[id]; group != nil && limit.IsPositive() {
			running := used.Sub(quotaSum)
			for _, i := range group.indexes {
				before := running
				running = running.Add(usageBillingCommandCredits(cmds[i].BillingEngine, cmds[i].APIKeyQuotaCredits, cmds[i].APIKeyQuotaCost))
				outcomes[i].Result.APIKeyQuotaExhausted = running.GreaterThanOrEqual(limit) && before.LessThan(limit)
			}
		}
	}
	return nil
}

// One insert preserves a separate ledger entry, balance_after and configuration
// snapshot for every applied credit-funded wallet request, in request order.
func insertUsageBillingBatchCreditLedger(ctx context.Context, tx *sql.Tx, cmds []*service.UsageBillingCommand, applied []bool, outcomes []service.UsageBillingBatchOutcome) error {
	var requestIDs, engines, usdCosts []string
	var charges, balances, scales, versions []int64
	var exchangeRates []float64
	var userID int64
	for i, cmd := range cmds {
		if !applied[i] || cmd.BalanceCredits <= 0 {
			continue
		}
		userID = cmd.UserID
		requestIDs = append(requestIDs, cmd.RequestID)
		engines = append(engines, cmd.BillingEngine)
		usdCosts = append(usdCosts, usageBillingAmount(cmd.BalanceCost).String())
		charges = append(charges, cmd.BalanceCredits)
		balances = append(balances, *outcomes[i].Result.NewBalanceCredits)
		scales = append(scales, cmd.CreditsPerUSD)
		versions = append(versions, cmd.BillingVersion)
		exchangeRates = append(exchangeRates, cmd.USDToCNYRate)
	}
	if len(requestIDs) == 0 {
		return nil
	}
	_, err := tx.ExecContext(ctx, `
		INSERT INTO billing_ledger (
			user_id, delta_credits, balance_after, bucket_type, bucket_id,
			reference_type, reference_id, request_id, metadata
		)
		SELECT $1, -t.charge, t.balance_after, 'wallet', $1,
			'usage', t.request_id, t.request_id,
			jsonb_build_object(
				'engine', t.engine,
				'usd_cost', t.usd_cost,
				'credits_per_usd', t.credit_scale,
				'usd_to_cny', t.exchange_rate,
				'billing_version', t.version
			)
		FROM unnest($2::text[], $3::bigint[], $4::bigint[], $5::text[],
			$6::numeric[], $7::bigint[], $8::numeric[], $9::bigint[])
			WITH ORDINALITY AS t(request_id, charge, balance_after, engine, usd_cost, credit_scale, exchange_rate, version, ord)
		ORDER BY t.ord
		ON CONFLICT (bucket_type, bucket_id, reference_type, reference_id) DO NOTHING
	`, userID, pq.Array(requestIDs), pq.Array(charges), pq.Array(balances), pq.Array(engines),
		pq.Array(usdCosts), pq.Array(scales), pq.Array(exchangeRates), pq.Array(versions))
	return err
}
