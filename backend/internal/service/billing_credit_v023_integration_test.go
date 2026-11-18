//go:build unit

package service

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

// Observe the fork's real RecordUsage -> atomic billing command boundary. The
// expected prices are independent constants, so an upstream USD fix cannot pass
// while credit, subscription, Key or account quota snapshots retain old amounts.
func TestRecordUsageV023PricingReachesEveryBillingEngine(t *testing.T) {
	previous := GetBillingCurrencySettings(context.Background())
	t.Cleanup(func() { publishBillingCurrencyConfig(previous) })
	before := time.Date(2027, 3, 1, 7, 59, 59, 0, time.UTC)
	after := before.Add(time.Second)
	type scenario struct {
		name, model, tier string
		at                time.Time
		usage             ClaudeUsage
		card              *ChannelModelPricing
		customStats       bool
		subscription      bool
		withoutResolver   bool
		wantTotal         float64
		wantCache         float64
		wantCharge        float64
		wantCredits       int64
	}
	baseUsage := ClaudeUsage{InputTokens: 1000, OutputTokens: 500, CacheCreationInputTokens: 400, CacheReadInputTokens: 200}
	channelCard := &ChannelModelPricing{
		Platform: PlatformOpenAI, Models: []string{"gpt-6.2-sol"}, BillingMode: BillingModeToken,
		InputPrice: float64Ptr(3e-6), OutputPrice: float64Ptr(4e-6),
		CacheWritePrice: float64Ptr(2e-6), CacheReadPrice: float64Ptr(1e-6),
		UltrafastMultiplier: float64Ptr(1.25),
	}
	cacheCard := &ChannelModelPricing{
		Platform: PlatformOpenAI, Models: []string{"claude-sonnet-4"}, BillingMode: BillingModeToken,
		InputPrice: float64Ptr(1e-6), OutputPrice: float64Ptr(2e-6),
		CacheWritePrice: float64Ptr(2e-6), CacheWrite1hPrice: float64Ptr(4e-6),
		CacheReadPrice: float64Ptr(0.1e-6),
	}
	cases := []scenario{
		// The date is captured at request start, even when RecordUsage executes
		// much later. Both model families must retain the correct 3x/6x price.
		{"Sol promotion", "gpt-6.2-sol", "ultrafast", before, baseUsage, nil, false, true, false, 0.02406, 0.003, 0.06015, 6_015_000},
		{"Sol promotion ended", "gpt-6.2-sol", "ultrafast", after, baseUsage, nil, false, true, false, 0.04812, 0.006, 0.1203, 12_030_000},
		{"Astra promotion", "gpt-6.2-astra", "ultrafast", before, baseUsage, nil, false, true, false, 0.09624, 0.012, 0.2406, 24_060_000},
		{"Astra promotion ended", "gpt-6.2-astra", "ultrafast", after, baseUsage, nil, false, true, false, 0.19248, 0.024, 0.4812, 48_120_000},
		// The resolver-free compatibility path also has to use request time.
		{"Sol promotion without resolver", "gpt-6.2-sol", "ultrafast", before, baseUsage, nil, false, true, true, 0.02406, 0.003, 0.06015, 6_015_000},
		// Channel Ultra Fast is the final multiplier, including across the
		// official promotion boundary. Group and subscription peak still apply.
		{"channel Ultra Fast during promotion", "gpt-6.2-sol", "ultrafast", before, baseUsage, channelCard, false, true, false, 0.0075, 0.001, 0.01875, 1_875_000},
		{"channel Ultra Fast after promotion", "gpt-6.2-sol", "ultrafast", after, baseUsage, channelCard, false, true, false, 0.0075, 0.001, 0.01875, 1_875_000},
		// 11k aggregate / 3k 1h detail leaves 8k unclassified writes. The
		// remainder belongs to 5m, and account-statistics custom cards agree.
		{"partial 1h cache plus 5m remainder", "claude-sonnet-4", "", before,
			ClaudeUsage{InputTokens: 1000, OutputTokens: 500, CacheCreationInputTokens: 11000, CacheCreation1hTokens: 3000, CacheReadInputTokens: 1000},
			cacheCard, true, false, false, 0.0301, 0.028, 0.037625, 3_762_500},
		{"all cache writes are 1h", "claude-sonnet-4", "", before,
			ClaudeUsage{InputTokens: 1000, OutputTokens: 500, CacheCreationInputTokens: 11000, CacheCreation1hTokens: 11000, CacheReadInputTokens: 1000},
			cacheCard, true, false, false, 0.0461, 0.044, 0.057625, 5_762_500},
	}
	for _, engine := range []string{BillingEngineLegacy, BillingEngineShadow, BillingEngineCredits} {
		for _, path := range []string{"OpenAI", "native gateway"} {
			for _, tc := range cases {
				t.Run(engine+"/"+path+"/"+tc.name, func(t *testing.T) {
					currency := DefaultBillingCurrencyConfig()
					currency.Engine = engine
					publishBillingCurrencyConfig(currency)
					logs, repo := &openAIRecordUsageLogRepoStub{inserted: true}, &openAIRecordUsageBillingRepoStub{}
					group := &Group{ID: 3, Platform: PlatformOpenAI, Hydrated: true, RateMultiplier: 1.25}
					var subscription *UserSubscription
					if tc.subscription {
						group.SubscriptionType = SubscriptionTypeSubscription
						group.PeakRateEnabled = true
						group.PeakStart, group.PeakEnd, group.PeakRateMultiplier = "00:00", "23:59", 2
						subscription = &UserSubscription{ID: 5}
					}
					key := &APIKey{ID: 2, GroupID: &group.ID, Group: group, Quota: 10, RateLimit5h: 10}
					account := &Account{ID: 4, Platform: PlatformOpenAI, Type: AccountTypeAPIKey,
						RateMultiplier: float64Ptr(0.4), Extra: map[string]any{"quota_limit": float64(100)}}
					var channels *ChannelService
					if tc.card != nil {
						channel := Channel{ID: 9, Status: StatusActive, GroupIDs: []int64{group.ID},
							ModelPricing: []ChannelModelPricing{*tc.card}, ApplyPricingToAccountStats: !tc.customStats}
						if tc.customStats {
							channel.AccountStatsPricingRules = []AccountStatsPricingRule{{AccountIDs: []int64{account.ID}, Pricing: []ChannelModelPricing{*tc.card}}}
						}
						channels = &ChannelService{}
						channels.cache.Store(populateChannelCache([]Channel{channel}, map[int64]string{group.ID: PlatformOpenAI}))
					}
					var tier *string
					if tc.tier != "" {
						tier = &tc.tier
					}
					quota := &openAIRecordUsageAPIKeyQuotaStub{}
					if path == "OpenAI" {
						svc := newOpenAIRecordUsageServiceWithBillingRepoForTest(logs, repo, &openAIRecordUsageUserRepoStub{}, &openAIRecordUsageSubRepoStub{}, nil)
						svc.channelService = channels
						if !tc.withoutResolver {
							svc.resolver = NewModelPricingResolver(channels, svc.billingService)
						}
						// OpenAI includes cache read/write inside input_tokens;
						// native Anthropic keeps those buckets separate.
						usage := OpenAIUsage{InputTokens: tc.usage.InputTokens + tc.usage.CacheCreationInputTokens + tc.usage.CacheReadInputTokens,
							OutputTokens: tc.usage.OutputTokens, CacheCreationInputTokens: tc.usage.CacheCreationInputTokens,
							CacheCreation5mTokens: tc.usage.CacheCreation5mTokens, CacheCreation1hTokens: tc.usage.CacheCreation1hTokens,
							CacheReadInputTokens: tc.usage.CacheReadInputTokens}
						require.NoError(t, svc.RecordUsage(context.Background(), &OpenAIRecordUsageInput{
							APIKey: key, User: &User{ID: 1}, Account: account, Subscription: subscription, APIKeyService: quota,
							PricingAt: tc.at, Result: &OpenAIForwardResult{RequestID: t.Name(), Model: tc.model, ServiceTier: tier, Usage: usage},
						}))
					} else {
						svc := newGatewayRecordUsageServiceWithBillingRepoForTest(logs, repo, &openAIRecordUsageUserRepoStub{}, &openAIRecordUsageSubRepoStub{})
						svc.channelService = channels
						if !tc.withoutResolver {
							svc.resolver = NewModelPricingResolver(channels, svc.billingService)
						}
						require.NoError(t, svc.RecordUsage(context.Background(), &RecordUsageInput{
							APIKey: key, User: &User{ID: 1}, Account: account, Subscription: subscription, APIKeyService: quota,
							PricingAt: tc.at, Result: &ForwardResult{RequestID: t.Name(), Model: tc.model, ServiceTier: tier, Usage: tc.usage},
						}))
					}
					require.Equal(t, 1, repo.calls)
					cmd, log := repo.lastCmd, logs.lastLog
					require.NotNil(t, cmd)
					require.NotNil(t, log)
					require.Equal(t, engine, cmd.BillingEngine)
					require.InDelta(t, tc.wantCharge, log.ActualCost, 1e-12)
					require.InDelta(t, tc.wantTotal, log.TotalCost, 1e-12)
					require.InDelta(t, tc.wantCache, log.CacheCreationCost, 1e-12)
					require.Equal(t, tc.usage.InputTokens, log.InputTokens)
					require.Equal(t, tc.usage.CacheCreationInputTokens, log.CacheCreationTokens)
					require.Equal(t, tc.usage.CacheCreation1hTokens, log.CacheCreation1hTokens)
					require.InDelta(t, tc.wantCharge, cmd.APIKeyQuotaCost, 1e-12)
					require.InDelta(t, tc.wantCharge, cmd.APIKeyRateLimitCost, 1e-12)
					require.InDelta(t, QuantizeUsageBillingAmount(tc.wantTotal*0.4), cmd.AccountQuotaCost, 1e-12)
					if tc.card != nil {
						require.NotNil(t, log.AccountStatsCost)
						require.InDelta(t, tc.wantTotal, *log.AccountStatsCost, 1e-12)
					}
					if tc.subscription {
						require.Zero(t, cmd.BalanceCost)
						require.InDelta(t, tc.wantCharge, cmd.SubscriptionCost, 1e-12)
					} else {
						require.Zero(t, cmd.SubscriptionCost)
						require.InDelta(t, tc.wantCharge, cmd.BalanceCost, 1e-12)
					}
					wantSnapshot := tc.wantCredits
					if engine == BillingEngineLegacy {
						wantSnapshot = 0
					}
					require.Equal(t, wantSnapshot, cmd.ChargeCredits)
					require.Equal(t, wantSnapshot, log.ChargeCredits)
					if engine == BillingEngineCredits {
						require.Equal(t, wantSnapshot, cmd.APIKeyQuotaCredits)
						require.Equal(t, wantSnapshot, cmd.APIKeyRateLimitCredits)
						if tc.subscription {
							require.Zero(t, cmd.BalanceCredits)
							require.Equal(t, wantSnapshot, cmd.SubscriptionCredits)
						} else {
							require.Equal(t, wantSnapshot, cmd.BalanceCredits)
							require.Zero(t, cmd.SubscriptionCredits)
						}
					} else {
						require.Zero(t, cmd.BalanceCredits)
						require.Zero(t, cmd.SubscriptionCredits)
						require.Zero(t, cmd.APIKeyQuotaCredits)
						require.Zero(t, cmd.APIKeyRateLimitCredits)
					}
				})
			}
		}
	}
}
