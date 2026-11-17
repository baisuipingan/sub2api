//go:build unit

package service

import (
	"context"
	"testing"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/config"
	"github.com/Wei-Shaw/sub2api/internal/pkg/timezone"
	"github.com/stretchr/testify/require"
)

// The fork snapshots the final charge into integer credits after the shared
// gateway pricing path. Exercise upstream pricing changes through RecordUsage,
// so a correct USD calculation cannot silently leave wallet/quota credits stale.
func TestRecordUsageOfficialPricingFactorsReachCreditSnapshots(t *testing.T) {
	previous := GetBillingCurrencySettings(context.Background())
	t.Cleanup(func() { publishBillingCurrencyConfig(previous) })
	currency := DefaultBillingCurrencyConfig()
	currency.Enabled = true
	currency.Engine = BillingEngineCredits
	publishBillingCurrencyConfig(currency)

	assertCharge := func(t *testing.T, repo *openAIRecordUsageBillingRepoStub, logs *openAIRecordUsageLogRepoStub, wantUSD float64, wantCredits int64, subscription bool) {
		t.Helper()
		require.Equal(t, 1, repo.calls)
		require.NotNil(t, repo.lastCmd)
		require.Equal(t, wantCredits, repo.lastCmd.ChargeCredits)
		require.Equal(t, wantCredits, repo.lastCmd.APIKeyQuotaCredits)
		require.Equal(t, wantCredits, repo.lastCmd.APIKeyRateLimitCredits)
		if subscription {
			require.Zero(t, repo.lastCmd.BalanceCredits)
			require.Equal(t, wantCredits, repo.lastCmd.SubscriptionCredits)
			require.InDelta(t, wantUSD, repo.lastCmd.SubscriptionCost, 1e-12)
		} else {
			require.Zero(t, repo.lastCmd.SubscriptionCredits)
			require.Equal(t, wantCredits, repo.lastCmd.BalanceCredits)
			require.InDelta(t, wantUSD, repo.lastCmd.BalanceCost, 1e-12)
		}
		require.NotNil(t, logs.lastLog)
		require.Equal(t, wantCredits, logs.lastLog.ChargeCredits)
		require.InDelta(t, wantUSD, logs.lastLog.ActualCost, 1e-12)
		require.Equal(t, BillingEngineCredits, repo.lastCmd.BillingEngine)
		require.Equal(t, currency.Version, logs.lastLog.BillingVersion)
	}
	keyFor := func(group *Group) *APIKey {
		return &APIKey{ID: 2, GroupID: &group.ID, Group: group, Quota: 10, RateLimit5h: 10}
	}
	user := &User{ID: 1}
	quota := &openAIRecordUsageAPIKeyQuotaStub{}

	t.Run("MiloCode ultrafast and subscription peak", func(t *testing.T) {
		logs, repo := &openAIRecordUsageLogRepoStub{inserted: true}, &openAIRecordUsageBillingRepoStub{}
		svc := newOpenAIRecordUsageServiceWithBillingRepoForTest(logs, repo, &openAIRecordUsageUserRepoStub{}, &openAIRecordUsageSubRepoStub{}, nil)
		pricing := newStubPricingServiceFromJSON(t, `{"gpt-6.1-sol":{"input_cost_per_token":0.000002,"output_cost_per_token":0.00001}}`)
		svc.billingService = NewBillingService(svc.cfg, pricing)
		svc.resolver = NewModelPricingResolver(nil, svc.billingService)
		group := &Group{ID: 3, Platform: PlatformOpenAI, Hydrated: true, RateMultiplier: 1.25,
			SubscriptionType: SubscriptionTypeSubscription, PeakRateEnabled: true,
			PeakStart: "00:00", PeakEnd: "23:59", PeakRateMultiplier: 2}
		tier := "ultrafast"
		require.NoError(t, svc.RecordUsage(context.Background(), &OpenAIRecordUsageInput{
			APIKey: keyFor(group), User: user, Account: &Account{ID: 4, Platform: PlatformOpenAI, Type: AccountTypeAPIKey},
			Subscription: &UserSubscription{ID: 5}, APIKeyService: quota,
			PricingAt: time.Date(2026, 11, 17, 12, 0, 0, 0, timezone.Location()),
			Result: &OpenAIForwardResult{RequestID: "credit-ultrafast", Model: "gpt-6.1-sol-high", ServiceTier: &tier,
				Usage: OpenAIUsage{InputTokens: 1000, OutputTokens: 500}},
		}))
		// ($0.002 + $0.005) × UltraFast 6 × group 1.25 × peak 2.
		assertCharge(t, repo, logs, 0.105, 10_500_000, true)
	})

	t.Run("Vertex multiregion token surcharge and cache storage", func(t *testing.T) {
		logs, repo := &openAIRecordUsageLogRepoStub{inserted: true}, &openAIRecordUsageBillingRepoStub{}
		svc := newGatewayRecordUsageServiceWithBillingRepoForTest(logs, repo, &openAIRecordUsageUserRepoStub{}, &openAIRecordUsageSubRepoStub{})
		pricing := newStubPricingServiceFromJSON(t, `{"gemini-3.7-flash":{"litellm_provider":"gemini","input_cost_per_token":0.00000075,"output_cost_per_token":0.00000375,"cache_read_input_token_cost":0.000000075,"cache_storage_cost_per_token_per_hour":0.000001}}`)
		svc.billingService = NewBillingService(svc.cfg, pricing)
		svc.resolver = NewModelPricingResolver(nil, svc.billingService)
		group := &Group{ID: 3, Platform: PlatformGemini, Hydrated: true, RateMultiplier: 2}
		require.NoError(t, svc.RecordUsage(context.Background(), &RecordUsageInput{
			APIKey: keyFor(group), User: user, Account: &Account{ID: 4, Platform: PlatformGemini, Type: AccountTypeAPIKey}, APIKeyService: quota,
			Result: &ForwardResult{RequestID: "credit-vertex", Model: "gemini-3.7-flash", UpstreamPriceMultiplier: 1.1,
				Usage: ClaudeUsage{InputTokens: 10000, OutputTokens: 1000, CacheReadInputTokens: 5000}, CacheStorageTokenHours: 20000},
		}))
		// Token $0.011625 × region 1.1, plus storage $0.02, then group 2.
		assertCharge(t, repo, logs, 0.065575, 6_557_500, false)
	})

	t.Run("DeepSeek explicit zero override during official peak", func(t *testing.T) {
		logs, repo := &openAIRecordUsageLogRepoStub{inserted: true}, &openAIRecordUsageBillingRepoStub{}
		svc := newGatewayRecordUsageServiceWithBillingRepoForTest(logs, repo, &openAIRecordUsageUserRepoStub{}, &openAIRecordUsageSubRepoStub{})
		pricing := newHotReloadPricingService(t, "", `{"deepseek-flash":{"input_cost_per_token":0.000002,"output_cost_per_token":0,"cache_read_input_token_cost":0.0000004}}`)
		svc.billingService = NewBillingService(&config.Config{}, pricing)
		svc.resolver = NewModelPricingResolver(nil, svc.billingService)
		group := &Group{ID: 3, Platform: PlatformOpenAI, Hydrated: true, RateMultiplier: 1.25}
		require.NoError(t, svc.RecordUsage(context.Background(), &RecordUsageInput{
			APIKey: keyFor(group), User: user, Account: &Account{ID: 4, Platform: PlatformOpenAI, Type: AccountTypeAPIKey}, APIKeyService: quota,
			PricingAt: time.Date(2026, 8, 24, 2, 0, 0, 0, time.UTC),
			Result: &ForwardResult{RequestID: "credit-deepseek", Model: "deepseek-flash",
				Usage: ClaudeUsage{InputTokens: 1000, OutputTokens: 500, CacheReadInputTokens: 1000}},
		}))
		// Explicit overrides, including free output, do not gain official peak 2×.
		assertCharge(t, repo, logs, 0.003, 300_000, false)
	})

	t.Run("Studio image tier keeps its independent multiplier", func(t *testing.T) {
		logs, repo := &openAIRecordUsageLogRepoStub{inserted: true}, &openAIRecordUsageBillingRepoStub{}
		svc := newOpenAIRecordUsageServiceWithBillingRepoForTest(logs, repo, &openAIRecordUsageUserRepoStub{}, &openAIRecordUsageSubRepoStub{}, nil)
		svc.resolver = NewModelPricingResolver(nil, svc.billingService)
		imagePrice := 0.4
		group := &Group{ID: 3, Platform: PlatformOpenAI, Hydrated: true, RateMultiplier: 1.25,
			SubscriptionType: SubscriptionTypeSubscription, PeakRateEnabled: true,
			PeakStart: "00:00", PeakEnd: "23:59", PeakRateMultiplier: 2,
			ImagePrice2K: &imagePrice, ImageRateIndependent: true, ImageRateMultiplier: 0.25}
		require.NoError(t, svc.RecordUsage(context.Background(), &OpenAIRecordUsageInput{
			APIKey: keyFor(group), User: user, Account: &Account{ID: 4, Platform: PlatformOpenAI, Type: AccountTypeAPIKey}, APIKeyService: quota,
			PricingAt: time.Date(2026, 11, 17, 12, 0, 0, 0, timezone.Location()),
			Result:    &OpenAIForwardResult{RequestID: "credit-studio-image", Model: "gpt-image-2", ImageCount: 1, ImageSize: "2K"},
		}))
		// Image pricing ignores the token peak and uses its independent 0.25×.
		assertCharge(t, repo, logs, 0.1, 10_000_000, false)
	})
}
