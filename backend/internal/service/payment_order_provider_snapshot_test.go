//go:build unit

package service

import (
	"context"
	"strconv"
	"testing"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/payment"
	"github.com/stretchr/testify/require"
)

func TestCreateUSDTOrderLocksRateAndNormalizesDailyLimits(t *testing.T) {
	ctx := context.Background()
	client := newPaymentConfigServiceTestClient(t)
	user, err := client.User.Create().SetEmail("usdt@example.com").SetPasswordHash("hash").Save(ctx)
	require.NoError(t, err)
	instance, err := client.PaymentProviderInstance.Create().
		SetProviderKey(payment.TypeBEpusdt).SetName("USDT").SetConfig("{}").
		SetSupportedTypes(payment.TypeUSDTBSC).SetPaymentMode("redirect").SetEnabled(true).Save(ctx)
	require.NoError(t, err)

	cfg := &PaymentConfig{DailyLimit: 80}
	billing := DefaultBillingCurrencyConfig()
	billing.RechargeRates = map[string]float64{"USD": 1.25, "CNY": 7.2}
	selection := &payment.InstanceSelection{ProviderKey: payment.TypeBEpusdt, InstanceID: strconv.FormatInt(instance.ID, 10)}
	svc := &PaymentService{entClient: client}
	merchantUser := &User{ID: user.ID, Email: user.Email}
	placeOrder := func(amount float64, currency string) error {
		billing.DisplayCurrency = currency
		quote, quoteErr := buildSiteRechargePrice(cfg, billing, amount)
		require.NoError(t, quoteErr)
		req := CreateOrderRequest{UserID: user.ID, Currency: currency, PaymentType: payment.TypeUSDTBSC, OrderType: payment.OrderTypeBalance, rechargeQuote: quote}
		order, createErr := svc.createOrderInTx(ctx, req, merchantUser, nil, cfg, quote.CreditedAmount, quote.payBase, quote.FeeRate, quote.PayAmount, quote.BonusAmount, selection)
		if createErr != nil {
			return createErr
		}
		require.Equal(t, quote.CreditedCredits, order.CreditedCredits)
		require.Equal(t, quote.rechargeRate, order.RechargeRateSnapshot)
		require.Equal(t, currency, PaymentOrderCurrency(order))
		require.Equal(t, quote.payAmountCNY, payment.PaymentOrderLimitAmount(order))
		require.NoError(t, client.PaymentOrder.UpdateOneID(order.ID).SetStatus(OrderStatusPaid).SetPaidAt(time.Now()).Exec(ctx))
		return nil
	}

	require.NoError(t, placeOrder(10, "USD"))
	billing.USDToCNY = 7.1
	require.ErrorContains(t, placeOrder(9, "CNY"), "daily_limit_exceeded")
	require.NoError(t, placeOrder(8, "CNY"))
}

func TestBuildPaymentOrderProviderSnapshot_ExcludesSensitiveConfig(t *testing.T) {
	t.Parallel()

	sel := &payment.InstanceSelection{
		InstanceID:     "12",
		ProviderKey:    payment.TypeWxpay,
		SupportedTypes: "wxpay,wxpay_direct",
		PaymentMode:    "popup",
		Config: map[string]string{
			"privateKey": "secret",
			"apiV3Key":   "secret-v3",
			"appId":      "wx-app-id",
		},
	}

	snapshot := buildPaymentOrderProviderSnapshot(sel, CreateOrderRequest{})
	require.Equal(t, map[string]any{
		"schema_version":       2,
		"provider_instance_id": "12",
		"provider_key":         payment.TypeWxpay,
		"payment_mode":         "popup",
		"merchant_app_id":      "wx-app-id",
		"currency":             "CNY",
	}, snapshot)
	require.NotContains(t, snapshot, "config")
	require.NotContains(t, snapshot, "privateKey")
	require.NotContains(t, snapshot, "apiV3Key")
	require.NotContains(t, snapshot, "supported_types")
	require.NotContains(t, snapshot, "instance_name")
	require.NotContains(t, snapshot, "merchant_id")
}

func TestCreateOrderInTx_WritesProviderSnapshot(t *testing.T) {
	ctx := context.Background()
	previousBilling := GetBillingCurrencySettings(ctx)
	billing := DefaultBillingCurrencyConfig()
	billing.USDToCNY = 7.2
	billing.RechargeRates = map[string]float64{BillingDisplayCNY: 7.2, BillingDisplayUSD: 1}
	publishBillingCurrencyConfig(billing)
	t.Cleanup(func() { publishBillingCurrencyConfig(previousBilling) })
	client := newPaymentConfigServiceTestClient(t)

	user, err := client.User.Create().
		SetEmail("snapshot@example.com").
		SetPasswordHash("hash").
		SetUsername("snapshot-user").
		Save(ctx)
	require.NoError(t, err)

	instance, err := client.PaymentProviderInstance.Create().
		SetProviderKey(payment.TypeAlipay).
		SetName("Primary Alipay").
		SetConfig(`{"secretKey":"do-not-copy"}`).
		SetSupportedTypes("alipay,alipay_direct").
		SetPaymentMode("redirect").
		SetEnabled(true).
		Save(ctx)
	require.NoError(t, err)

	svc := &PaymentService{entClient: client}
	order, err := svc.createOrderInTx(
		ctx,
		CreateOrderRequest{
			UserID:      user.ID,
			PaymentType: payment.TypeAlipay,
			OrderType:   payment.OrderTypeBalance,
			ClientIP:    "127.0.0.1",
			SrcHost:     "app.example.com",
		},
		&User{
			ID:       user.ID,
			Email:    user.Email,
			Username: user.Username,
		},
		nil,
		&PaymentConfig{
			MaxPendingOrders: 3,
			OrderTimeoutMin:  30,
		},
		88,
		88,
		0,
		88,
		0,
		&payment.InstanceSelection{
			InstanceID:     strconv.FormatInt(instance.ID, 10),
			ProviderKey:    payment.TypeAlipay,
			SupportedTypes: "alipay,alipay_direct",
			PaymentMode:    "redirect",
			Config: map[string]string{
				"secretKey": "do-not-copy",
			},
		},
	)
	require.NoError(t, err)
	require.Equal(t, strconv.FormatInt(instance.ID, 10), valueOrEmpty(order.ProviderInstanceID))
	require.Equal(t, payment.TypeAlipay, valueOrEmpty(order.ProviderKey))
	require.Equal(t, float64(2), order.ProviderSnapshot["schema_version"])
	require.Equal(t, strconv.FormatInt(instance.ID, 10), order.ProviderSnapshot["provider_instance_id"])
	require.Equal(t, payment.TypeAlipay, order.ProviderSnapshot["provider_key"])
	require.Equal(t, "redirect", order.ProviderSnapshot["payment_mode"])
	require.NotContains(t, order.ProviderSnapshot, "config")
	require.NotContains(t, order.ProviderSnapshot, "secretKey")
	require.NotContains(t, order.ProviderSnapshot, "supported_types")
	require.NotContains(t, order.ProviderSnapshot, "instance_name")
	require.Equal(t, USDToCredits(88), order.CreditedCredits)
	require.Equal(t, CreditsPerUSD, order.CreditsPerUsdSnapshot)
	require.InDelta(t, 7.2, order.UsdToCnySnapshot, 1e-12)
	require.InDelta(t, 7.2, order.RechargeRateSnapshot, 1e-12)
	require.Equal(t, billing.Version, order.BillingVersion)
}

func TestBuildPaymentOrderProviderSnapshot_UsesWxpayJSAPIAppIDForOpenIDOrders(t *testing.T) {
	t.Parallel()

	snapshot := buildPaymentOrderProviderSnapshot(&payment.InstanceSelection{
		InstanceID:  "88",
		ProviderKey: payment.TypeWxpay,
		Config: map[string]string{
			"appId":   "wx-open-app",
			"mpAppId": "wx-mp-app",
			"mchId":   "mch-88",
		},
		PaymentMode: "jsapi",
	}, CreateOrderRequest{OpenID: "openid-123"})

	require.Equal(t, "wx-mp-app", snapshot["merchant_app_id"])
	require.Equal(t, "mch-88", snapshot["merchant_id"])
	require.Equal(t, "CNY", snapshot["currency"])
}

func TestBuildPaymentOrderProviderSnapshot_IncludesAlipayMerchantIdentity(t *testing.T) {
	t.Parallel()

	snapshot := buildPaymentOrderProviderSnapshot(&payment.InstanceSelection{
		InstanceID:  "21",
		ProviderKey: payment.TypeAlipay,
		Config: map[string]string{
			"appId":      "alipay-app-21",
			"privateKey": "secret",
		},
		PaymentMode: "redirect",
	}, CreateOrderRequest{})

	require.Equal(t, "alipay-app-21", snapshot["merchant_app_id"])
	require.NotContains(t, snapshot, "privateKey")
}

func TestBuildPaymentOrderProviderSnapshot_IncludesEasyPayMerchantIdentity(t *testing.T) {
	t.Parallel()

	snapshot := buildPaymentOrderProviderSnapshot(&payment.InstanceSelection{
		InstanceID:  "66",
		ProviderKey: payment.TypeEasyPay,
		Config: map[string]string{
			"pid":  "easypay-merchant-66",
			"pkey": "secret",
		},
		PaymentMode: "popup",
	}, CreateOrderRequest{PaymentType: payment.TypeAlipay})

	require.Equal(t, "easypay-merchant-66", snapshot["merchant_id"])
	require.NotContains(t, snapshot, "pkey")
}

func TestBuildPaymentOrderProviderSnapshot_IncludesProviderCurrency(t *testing.T) {
	t.Parallel()

	stripeSnapshot := buildPaymentOrderProviderSnapshot(&payment.InstanceSelection{
		InstanceID:  "77",
		ProviderKey: payment.TypeStripe,
		Config: map[string]string{
			"currency": "hkd",
		},
	}, CreateOrderRequest{})
	require.Equal(t, "HKD", stripeSnapshot["currency"])

	airwallexSnapshot := buildPaymentOrderProviderSnapshot(&payment.InstanceSelection{
		InstanceID:  "78",
		ProviderKey: payment.TypeAirwallex,
		Config: map[string]string{
			"currency":  "usd",
			"accountId": "acct-78",
		},
	}, CreateOrderRequest{})
	require.Equal(t, "USD", airwallexSnapshot["currency"])
	require.Equal(t, "acct-78", airwallexSnapshot["merchant_id"])
}

func valueOrEmpty(v *string) string {
	if v == nil {
		return ""
	}
	return *v
}
