//go:build unit

package service

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"

	dbent "github.com/Wei-Shaw/sub2api/ent"
	"github.com/Wei-Shaw/sub2api/internal/payment"
	infraerrors "github.com/Wei-Shaw/sub2api/internal/pkg/errors"
	"github.com/stretchr/testify/require"
)

func TestSiteRechargeQuoteCurrencySwitchAndCheckoutReplay(t *testing.T) {
	ctx := context.Background()
	client := newPaymentConfigServiceTestClient(t)
	account, err := client.User.Create().SetEmail("site-recharge@example.com").SetPasswordHash("hash").Save(ctx)
	require.NoError(t, err)
	merchantCreates := atomic.Int64{}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		merchantCreates.Add(1)
		var payload map[string]any
		if req.URL.Path != "/api/v1/order/create-transaction" || json.NewDecoder(req.Body).Decode(&payload) != nil {
			http.Error(w, "unexpected provider request", http.StatusBadRequest)
			return
		}
		data := map[string]any{
			"trade_id": "BepusdtTestTrade01", "order_id": payload["order_id"],
			"trade_type": "usdt.bep20", "fiat": payload["fiat"], "amount": payload["amount"],
			"status": 1, "actual_amount": "3.00", "expiration_time": 600,
		}
		_ = json.NewEncoder(w).Encode(map[string]any{"status_code": 200, "data": data})
	}))
	defer server.Close()
	providerConfig, err := json.Marshal(map[string]string{
		"apiBase": server.URL, "checkoutOrigin": "https://checkout.example", "notifyUrl": "https://app.example/api/v1/payment/webhook/bepusdt",
		"returnUrl": "https://app.example/payment/result", "apiToken": "fixture-token",
	})
	require.NoError(t, err)
	addRechargeTestProvider(t, client, payment.TypeBEpusdt, payment.TypeUSDTBSC, string(providerConfig))
	addRechargeTestProvider(t, client, payment.TypeAlipay, payment.TypeAlipay, `{}`)
	settings := &paymentConfigSettingRepoStub{values: map[string]string{
		SettingPaymentEnabled: "true", SettingEnabledPaymentTypes: "usdt_bsc,alipay", SettingMinRechargeAmount: "1", SettingDailyRechargeLimit: "25",
	}}
	setPlatformCurrency := func(currency string, version int64) {
		billing := DefaultBillingCurrencyConfig()
		billing.DisplayCurrency = currency
		billing.USDToCNY = 6.75
		billing.Version = version
		raw, err := json.Marshal(billing)
		require.NoError(t, err)
		settings.values[SettingKeyBillingCurrencyConfig] = string(raw)
	}
	setPlatformCurrency(BillingDisplayUSD, 1)
	cfg := NewPaymentConfigService(client, settings, nil)
	svc := NewPaymentService(client, nil, payment.NewDefaultLoadBalancer(client, nil), nil, nil, cfg,
		&mockUserRepo{getByIDUser: &User{ID: account.ID, Email: account.Email, Status: payment.EntityStatusActive}}, nil, nil)
	svc.resumeService = NewPaymentResumeService([]byte("recharge-test-signing-key"))
	req := CreateOrderRequest{UserID: account.ID, PaymentType: payment.TypeUSDTBSC, Amount: 3,
		Currency: BillingDisplayUSD, BillingVersion: 1, SrcHost: "app.example",
		ReturnURL: "https://app.example/payment/result"}
	_, err = svc.CreateOrder(ctx, req)
	require.Equal(t, "RECHARGE_QUOTE_REQUIRED", infraerrors.Reason(err))
	staleRequest := req
	staleRequest.BillingVersion = 0
	_, err = svc.RechargeQuote(ctx, staleRequest)
	require.Equal(t, "RECHARGE_QUOTE_CHANGED", infraerrors.Reason(err))
	alipayReq := req
	alipayReq.PaymentType = payment.TypeAlipay
	_, err = svc.RechargeQuote(ctx, alipayReq)
	require.Equal(t, "PAYMENT_METHOD_CURRENCY_MISMATCH", infraerrors.Reason(err))

	firstQuote, err := svc.RechargeQuote(ctx, req)
	require.NoError(t, err)
	require.EqualValues(t, 300_000_000, firstQuote.CreditedCredits)
	require.Equal(t, 3.0, firstQuote.CreditedDisplayAmount)
	require.Equal(t, "3.00", firstQuote.CryptoAmount)
	staleQuote, err := svc.RechargeQuote(ctx, req)
	require.NoError(t, err)
	req.QuoteID = firstQuote.QuoteID

	wrongOwner := req
	wrongOwner.UserID++
	_, err = svc.CreateOrder(ctx, wrongOwner)
	require.Equal(t, "RECHARGE_QUOTE_CHANGED", infraerrors.Reason(err))
	wrongAmount := req
	wrongAmount.Amount = 1
	_, err = svc.CreateOrder(ctx, wrongAmount)
	require.Equal(t, "RECHARGE_QUOTE_CHANGED", infraerrors.Reason(err))
	created, err := svc.CreateOrder(ctx, req)
	require.NoError(t, err)
	require.Equal(t, "3", created.CryptoAmount)
	require.Equal(t, BillingDisplayUSD, created.Currency)
	order, err := client.PaymentOrder.Get(ctx, created.OrderID)
	require.NoError(t, err)
	require.EqualValues(t, 300_000_000, order.CreditedCredits)
	require.EqualValues(t, 1.0, order.RechargeRateSnapshot)
	require.Equal(t, "USD", order.ProviderSnapshot["platform_currency"])

	setPlatformCurrency(BillingDisplayCNY, 2)
	req.QuoteID = staleQuote.QuoteID
	_, err = svc.CreateOrder(ctx, req)
	require.Equal(t, "RECHARGE_QUOTE_CHANGED", infraerrors.Reason(err))
	req.QuoteID = firstQuote.QuoteID
	settings.values[SettingPaymentEnabled] = "false"
	replayed, err := svc.CreateOrder(ctx, req)
	require.NoError(t, err)
	require.Equal(t, created.OrderID, replayed.OrderID)
	require.Equal(t, created.PayURL, replayed.PayURL)
	require.Equal(t, created.ResumeToken, replayed.ResumeToken)
	require.EqualValues(t, 1, merchantCreates.Load())
	require.EqualValues(t, 1, client.PaymentOrder.Query().CountX(ctx))

	settings.values[SettingPaymentEnabled] = "true"
	alipayReq.Currency, alipayReq.BillingVersion, alipayReq.Amount = BillingDisplayCNY, 2, 6.75
	settings.values[SettingEnabledPaymentTypes] = "usdt_bsc"
	_, err = svc.RechargeQuote(ctx, alipayReq)
	require.Equal(t, "PAYMENT_METHOD_CURRENCY_MISMATCH", infraerrors.Reason(err))
	settings.values[SettingEnabledPaymentTypes] = "usdt_bsc,alipay"
	cnyQuote, err := svc.RechargeQuote(ctx, alipayReq)
	require.NoError(t, err)
	require.EqualValues(t, 100_000_000, cnyQuote.CreditedCredits)
	require.Equal(t, 6.75, cnyQuote.CreditedDisplayAmount)
	require.Empty(t, cnyQuote.CryptoAmount)
	alipayReq.QuoteID = cnyQuote.QuoteID
	_, err = svc.CreateOrder(ctx, alipayReq)
	require.Equal(t, "DAILY_LIMIT_EXCEEDED", infraerrors.Reason(err)) // The unpaid USD order reserves ¥20.25.
	require.EqualValues(t, 1, client.PaymentOrder.Query().CountX(ctx))
}

func TestSiteRechargeUSDTWithholdsAdjustedAmount(t *testing.T) {
	for _, tc := range []struct {
		name          string
		gatewayStatus int
		wantCode      int
		wantReason    string
	}{
		{"cancelled", 4, http.StatusConflict, usdtAmountBusy},
		{"still confirming", 5, http.StatusServiceUnavailable, usdtCheckoutUnavailable},
	} {
		t.Run(tc.name, func(t *testing.T) {
			ctx := context.Background()
			client := newPaymentConfigServiceTestClient(t)
			account, err := client.User.Create().SetEmail("usdt-recharge@example.com").SetPasswordHash("hash").Save(ctx)
			require.NoError(t, err)
			const tradeID = "BepusdtTestTrade01"
			requests := make(chan string, 6)
			var merchantOrder string
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
				requests <- req.URL.Path
				var payload map[string]any
				if err := json.NewDecoder(req.Body).Decode(&payload); err != nil {
					t.Error(err)
					w.WriteHeader(http.StatusBadRequest)
					return
				}
				var data map[string]any
				switch req.URL.Path {
				case "/api/v1/order/create-transaction":
					merchantOrder = fmt.Sprint(payload["order_id"])
					data = map[string]any{"trade_id": tradeID, "order_id": merchantOrder,
						"trade_type": "usdt.bep20", "fiat": "USD", "amount": "10", "status": 1, "actual_amount": "10.01", "expiration_time": 600}
				case "/api/v1/order/cancel-transaction":
					data = map[string]any{"trade_id": tradeID}
				case "/api/v1/pay/info":
					data = map[string]any{"trade_id": tradeID, "order_id": merchantOrder,
						"trade_type": "usdt.bep20", "fiat": "USD", "money": "10", "status": tc.gatewayStatus, "actual_amount": "10.01"}
				default:
					w.WriteHeader(http.StatusNotFound)
					return
				}
				_ = json.NewEncoder(w).Encode(map[string]any{"status_code": 200, "data": data})
			}))
			defer server.Close()
			providerConfig, err := json.Marshal(map[string]string{
				"apiBase": server.URL, "checkoutOrigin": "https://checkout.example", "apiToken": "fixture-token",
				"notifyUrl": "https://app.example/api/v1/payment/webhook/bepusdt", "returnUrl": "https://app.example/payment/result",
			})
			require.NoError(t, err)
			addRechargeTestProvider(t, client, payment.TypeBEpusdt, payment.TypeUSDTBSC, string(providerConfig))
			billing := DefaultBillingCurrencyConfig()
			billing.DisplayCurrency = BillingDisplayUSD
			billingJSON, err := json.Marshal(billing)
			require.NoError(t, err)
			settings := &paymentConfigSettingRepoStub{values: map[string]string{
				SettingPaymentEnabled: "true", SettingEnabledPaymentTypes: payment.TypeUSDTBSC,
				SettingKeyBillingCurrencyConfig: string(billingJSON),
			}}
			cfg := NewPaymentConfigService(client, settings, nil)
			svc := NewPaymentService(client, nil, payment.NewDefaultLoadBalancer(client, nil), nil, nil, cfg,
				&mockUserRepo{getByIDUser: &User{ID: account.ID, Email: account.Email, Status: payment.EntityStatusActive}}, nil, nil)
			svc.resumeService = NewPaymentResumeService([]byte("recharge-test-signing-key"))
			req := CreateOrderRequest{UserID: account.ID, PaymentType: payment.TypeUSDTBSC, Amount: 10,
				Currency: BillingDisplayUSD, BillingVersion: billing.Version, SrcHost: "app.example", ReturnURL: "https://app.example/payment/result"}
			quote, err := svc.RechargeQuote(ctx, req)
			require.NoError(t, err)
			req.QuoteID = quote.QuoteID
			for range 2 {
				response, err := svc.CreateOrder(ctx, req)
				require.Nil(t, response)
				require.Equal(t, tc.wantCode, infraerrors.Code(err))
				require.Equal(t, tc.wantReason, infraerrors.Reason(err))
			}
			require.Len(t, requests, 3)
			require.Equal(t, "/api/v1/order/create-transaction", <-requests)
			require.Equal(t, "/api/v1/order/cancel-transaction", <-requests)
			require.Equal(t, "/api/v1/pay/info", <-requests)
			order, err := client.PaymentOrder.Query().Only(ctx)
			require.NoError(t, err)
			require.Equal(t, OrderStatusFailed, order.Status)
			require.Equal(t, tc.wantReason, *order.FailedReason)
			require.Nil(t, order.PayURL)
			require.Empty(t, order.PaymentTradeNo)
			logs, err := svc.GetOrderAuditLogs(ctx, order.ID)
			require.NoError(t, err)
			require.Len(t, logs, 1)
			require.Equal(t, "USDT_CHECKOUT_WITHHELD", logs[0].Action)
			require.Contains(t, logs[0].Detail, tradeID)
		})
	}
}

func addRechargeTestProvider(t *testing.T, client *dbent.Client, key, method, config string) {
	t.Helper()
	_, err := client.PaymentProviderInstance.Create().SetProviderKey(key).SetName(key).SetConfig(config).
		SetSupportedTypes(method).SetPaymentMode("redirect").SetEnabled(true).Save(context.Background())
	require.NoError(t, err)
}
