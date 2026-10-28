package service

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestBillingCurrency_USDToCredits(t *testing.T) {
	cases := []struct {
		name   string
		amount float64
		want   int64
	}{
		{name: "zero", amount: 0, want: 0},
		{name: "one usd", amount: 1, want: CreditsPerUSD},
		{name: "fractional usd", amount: 0.12345678, want: 12_345_678},
		{name: "eight decimal boundary", amount: 0.00000001, want: 1},
		{name: "rounds half away from zero", amount: -0.000000015, want: -2},
	}
	for _, tt := range cases {
		t.Run(tt.name, func(t *testing.T) {
			require.Equal(t, tt.want, USDToCredits(tt.amount))
		})
	}
}

func TestBillingCurrency_CreditsToDisplayAmount(t *testing.T) {
	cfg := DefaultBillingCurrencyConfig()
	cfg.DisplayCurrency = BillingDisplayCNY
	cfg.USDToCNY = 7.2
	require.InDelta(t, 7.2, CreditsToDisplayAmount(CreditsPerUSD, cfg), 1e-12)
	require.InDelta(t, 0.72, CreditsToDisplayAmount(CreditsPerUSD/10, cfg), 1e-12)

	cfg.DisplayCurrency = BillingDisplayUSD
	require.InDelta(t, 1, CreditsToDisplayAmount(CreditsPerUSD, cfg), 1e-12)
}

func TestBillingCurrency_EffectiveEngine(t *testing.T) {
	cfg := DefaultBillingCurrencyConfig()
	require.Equal(t, BillingEngineShadow, cfg.EffectiveEngine())

	cfg.Enabled = true
	cfg.Engine = BillingEngineShadow
	require.Equal(t, BillingEngineShadow, cfg.EffectiveEngine())

	cfg.Engine = BillingEngineCredits
	require.Equal(t, BillingEngineCredits, cfg.EffectiveEngine())

	cfg.Enabled = false
	require.Equal(t, BillingEngineLegacy, cfg.EffectiveEngine())
}

func TestBillingCurrency_RechargeRate(t *testing.T) {
	cfg := DefaultBillingCurrencyConfig()
	cfg.RechargeRates = map[string]float64{"CNY": 7.5}
	require.Equal(t, 7.5, cfg.RechargeRate("cny"))
	require.Equal(t, 1.0, cfg.RechargeRate("USD"))
	require.Equal(t, 1.0, cfg.RechargeRate(""))
}

func TestFormatBillingDisplayAmount(t *testing.T) {
	previous := GetBillingCurrencySettings(context.Background())
	cfg := DefaultBillingCurrencyConfig()
	cfg.DisplayCurrency = BillingDisplayCNY
	cfg.USDToCNY = 7.2
	publishBillingCurrencyConfig(cfg)
	t.Cleanup(func() { publishBillingCurrencyConfig(previous) })
	require.Equal(t, "¥7.20", FormatBillingDisplayAmount(1))
}

func TestParseBillingCurrencyConfig_NormalizesJSONShape(t *testing.T) {
	raw := `{
		"enabled": true,
		"engine": "credits",
		"credits_per_usd": 100000000,
		"display_currency": "cny",
		"usd_to_cny": 7.2,
		"recharge_rates": {"cny": 7.3, "usd": 1},
		"version": 4
	}`
	cfg, err := parseBillingCurrencyConfig(raw)
	require.NoError(t, err)
	require.True(t, cfg.Enabled)
	require.Equal(t, BillingEngineCredits, cfg.EffectiveEngine())
	require.Equal(t, BillingDisplayCNY, cfg.DisplayCurrency)
	require.Equal(t, 7.3, cfg.RechargeRates[BillingDisplayCNY])
	require.Equal(t, int64(4), cfg.Version)
}

func TestParseBillingCurrencyConfig_RejectsMutableLedgerScale(t *testing.T) {
	_, err := parseBillingCurrencyConfig(`{"credits_per_usd": 1}`)
	require.ErrorIs(t, err, ErrInvalidBillingCurrencyConfig)
}

func TestUpdateBillingCurrencyConfig_PublishesSnapshot(t *testing.T) {
	repo := &billingCurrencySettingRepo{values: map[string]string{}}
	svc := NewSettingService(repo, nil)
	ctx := context.Background()

	updated, err := svc.UpdateBillingCurrencyConfig(ctx, BillingCurrencyConfig{
		Enabled:         true,
		Engine:          BillingEngineCredits,
		DisplayCurrency: BillingDisplayCNY,
		USDToCNY:        7.2,
		RechargeRates:   map[string]float64{"CNY": 7.2, "USD": 1},
		Version:         1,
	})
	require.NoError(t, err)
	require.Equal(t, int64(2), updated.Version)
	require.Equal(t, BillingDisplayCNY, GetBillingCurrencySettings(ctx).DisplayCurrency)
	require.JSONEq(t, mustJSONForBillingCurrency(t, updated), repo.values[SettingKeyBillingCurrencyConfig])
}

func mustJSONForBillingCurrency(t *testing.T, value any) string {
	t.Helper()
	body, err := json.Marshal(value)
	require.NoError(t, err)
	return string(body)
}

type billingCurrencySettingRepo struct {
	values map[string]string
}

func (r *billingCurrencySettingRepo) Get(context.Context, string) (*Setting, error) {
	return nil, ErrSettingNotFound
}

func (r *billingCurrencySettingRepo) GetValue(_ context.Context, key string) (string, error) {
	value, ok := r.values[key]
	if !ok {
		return "", ErrSettingNotFound
	}
	return value, nil
}

func (r *billingCurrencySettingRepo) Set(_ context.Context, key, value string) error {
	r.values[key] = value
	return nil
}

func (r *billingCurrencySettingRepo) GetMultiple(_ context.Context, keys []string) (map[string]string, error) {
	out := make(map[string]string, len(keys))
	for _, key := range keys {
		if value, ok := r.values[key]; ok {
			out[key] = value
		}
	}
	return out, nil
}

func (r *billingCurrencySettingRepo) SetMultiple(_ context.Context, values map[string]string) error {
	for key, value := range values {
		r.values[key] = value
	}
	return nil
}

func (r *billingCurrencySettingRepo) GetAll(context.Context) (map[string]string, error) {
	out := make(map[string]string, len(r.values))
	for key, value := range r.values {
		out[key] = value
	}
	return out, nil
}

func (r *billingCurrencySettingRepo) Delete(_ context.Context, key string) error {
	delete(r.values, key)
	return nil
}
