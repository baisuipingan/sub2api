package service

import (
	"testing"

	dbent "github.com/Wei-Shaw/sub2api/ent"
	"github.com/Wei-Shaw/sub2api/internal/payment"
	"github.com/stretchr/testify/require"
)

func TestSiteRechargeQuoteUsesPlatformCurrencyAndFX(t *testing.T) {
	cfg := &PaymentConfig{MinAmount: 1, MaxAmount: 100, BalanceRechargeMultiplier: 8}
	billing := DefaultBillingCurrencyConfig()
	billing.USDToCNY = 7.2
	billing.RechargeRates = map[string]float64{"CNY": 7, "USD": 1.25}

	cny, err := buildSiteRechargePrice(cfg, billing, 1)
	require.NoError(t, err)
	require.Equal(t, "CNY", cny.Currency)
	require.Equal(t, "1.00", cny.payAmountStr)
	require.EqualValues(t, 13_888_889, cny.CreditedCredits)
	require.Equal(t, 0.13888889, cny.CreditedAmount)

	billing.DisplayCurrency = BillingDisplayUSD
	usd, err := buildSiteRechargePrice(cfg, billing, 10)
	require.NoError(t, err)
	require.Equal(t, "10.00", usd.payAmountStr)
	require.Equal(t, 72.0, usd.payAmountCNY)
	require.EqualValues(t, 1_000_000_000, usd.CreditedCredits)
	require.Equal(t, RechargeAmountRange{MinAmount: .14, MaxAmount: 13.88}, usd.RechargeAmountRange)
	_, err = buildSiteRechargePrice(cfg, billing, .13)
	require.Error(t, err)
	_, err = buildSiteRechargePrice(cfg, billing, 13.89)
	require.Error(t, err)
	_, err = buildSiteRechargePrice(cfg, billing, 1.001)
	require.Error(t, err)
}

func TestSiteRechargeQuoteAppliesCNYPromotionThresholdBeforeFee(t *testing.T) {
	cfg := &PaymentConfig{
		MinAmount: 1, RechargeFeeRate: 3,
		RechargeBonusTiers: []RechargeBonusTier{{MinAmount: 70, BonusPercent: 20}},
	}
	billing := DefaultBillingCurrencyConfig()
	billing.DisplayCurrency = BillingDisplayUSD
	cfg.RechargeBonusMode = RechargeBonusModeDiscount
	discount, err := buildSiteRechargePrice(cfg, billing, 10)
	require.NoError(t, err)
	require.Equal(t, 8.24, discount.PayAmount)
	require.Equal(t, .24, discount.FeeAmount)
	require.Equal(t, 2.0, discount.BonusAmount)
	require.EqualValues(t, 1_000_000_000, discount.CreditedCredits)

	cfg.RechargeBonusMode = RechargeBonusModeBonus
	bonus, err := buildSiteRechargePrice(cfg, billing, 10)
	require.NoError(t, err)
	require.Equal(t, 10.3, bonus.PayAmount)
	require.Equal(t, .30, bonus.FeeAmount)
	require.Equal(t, 2.0, bonus.BonusAmount)
	require.EqualValues(t, 1_200_000_000, bonus.CreditedCredits)
}

func TestBEpusdtPaidMetadataMatchesOrderSnapshot(t *testing.T) {
	order := &dbent.PaymentOrder{
		PaymentType: payment.TypeUSDTBSC,
		PayAmount:   10,
		ProviderSnapshot: map[string]any{
			"schema_version": 3, "currency": "USD", "crypto_amount": "10.00005", "pay_amount_cny": 72.0,
		},
	}
	require.Equal(t, 72.0, payment.PaymentOrderLimitAmount(order))
	require.NoError(t, validateProviderSnapshotMetadata(order, payment.TypeBEpusdt, map[string]string{
		"currency": "USD", "crypto_amount": "10.000050",
	}))
	require.Error(t, validateProviderSnapshotMetadata(order, payment.TypeBEpusdt, map[string]string{
		"currency": "CNY", "crypto_amount": "10.00005",
	}))
	require.Error(t, validateProviderSnapshotMetadata(order, payment.TypeBEpusdt, map[string]string{
		"currency": "USD", "crypto_amount": "10.00006",
	}))
	require.Error(t, validateProviderSnapshotMetadata(order, payment.TypeBEpusdt, nil))

	order.ProviderSnapshot = nil
	require.Equal(t, 10.0, payment.PaymentOrderLimitAmount(order))
	require.NoError(t, validateProviderSnapshotMetadata(order, payment.TypeBEpusdt, map[string]string{"currency": "CNY"}))
	require.Error(t, validateProviderSnapshotMetadata(order, payment.TypeBEpusdt, map[string]string{"currency": "USD"}))
}
