package service

import (
	"context"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestApplyUsageBillingCreditSnapshot_EngineModes(t *testing.T) {
	prev := GetBillingCurrencySettings(context.Background())
	t.Cleanup(func() { publishBillingCurrencyConfig(prev) })

	base := DefaultBillingCurrencyConfig()
	base.Enabled = true
	base.CreditsPerUSD = CreditsPerUSD

	t.Run("legacy", func(t *testing.T) {
		cfg := base
		cfg.Engine = BillingEngineLegacy
		publishBillingCurrencyConfig(cfg)
		cmd := &UsageBillingCommand{}
		applyUsageBillingCreditSnapshot(cmd, 1.25, true)
		require.Zero(t, cmd.ChargeCredits)
		require.Zero(t, cmd.BalanceCredits)
		require.Equal(t, BillingEngineLegacy, cmd.BillingEngine)
	})

	t.Run("shadow", func(t *testing.T) {
		cfg := base
		cfg.Engine = BillingEngineShadow
		publishBillingCurrencyConfig(cfg)
		cmd := &UsageBillingCommand{}
		applyUsageBillingCreditSnapshot(cmd, 1.25, true)
		require.Equal(t, int64(125_000_000), cmd.ChargeCredits)
		require.Zero(t, cmd.BalanceCredits)
		require.Equal(t, BillingEngineShadow, cmd.BillingEngine)
	})

	t.Run("credits wallet and quota buckets", func(t *testing.T) {
		cfg := base
		cfg.Engine = BillingEngineCredits
		publishBillingCurrencyConfig(cfg)
		cmd := &UsageBillingCommand{APIKeyQuotaCost: 1.25, APIKeyRateLimitCost: 1.25}
		applyUsageBillingCreditSnapshot(cmd, 1.25, true)
		require.Equal(t, int64(125_000_000), cmd.ChargeCredits)
		require.Equal(t, cmd.ChargeCredits, cmd.BalanceCredits)
		require.Equal(t, cmd.ChargeCredits, cmd.APIKeyQuotaCredits)
		require.Equal(t, cmd.ChargeCredits, cmd.APIKeyRateLimitCredits)
	})

	t.Run("credits subscription does not debit wallet", func(t *testing.T) {
		cfg := base
		cfg.Engine = BillingEngineCredits
		publishBillingCurrencyConfig(cfg)
		subscriptionID := int64(7)
		cmd := &UsageBillingCommand{SubscriptionID: &subscriptionID, SubscriptionCost: 1.25}
		applyUsageBillingCreditSnapshot(cmd, 1.25, false)
		require.Equal(t, int64(125_000_000), cmd.ChargeCredits)
		require.Equal(t, cmd.ChargeCredits, cmd.SubscriptionCredits)
		require.Zero(t, cmd.BalanceCredits)
	})
}

func TestUsageBillingFingerprintIgnoresDisplayOnlySnapshots(t *testing.T) {
	cmd := &UsageBillingCommand{
		RequestID:      "request-1",
		UserID:         1,
		AccountID:      2,
		APIKeyID:       3,
		Model:          "gpt-test",
		InputTokens:    100,
		OutputTokens:   20,
		BalanceCost:    0.0001,
		ChargeCredits:  10_000,
		BalanceCredits: 10_000,
		BillingEngine:  BillingEngineCredits,
		CreditsPerUSD:  CreditsPerUSD,
		USDToCNYRate:   7.2,
		BillingVersion: 1,
	}
	first := buildUsageBillingFingerprint(cmd)
	cmd.USDToCNYRate = 7.5
	cmd.BillingVersion = 2
	cmd.BillingEngine = BillingEngineShadow
	second := buildUsageBillingFingerprint(cmd)
	require.Equal(t, first, second)
}
