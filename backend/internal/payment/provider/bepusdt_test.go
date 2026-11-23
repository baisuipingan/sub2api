package provider

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/payment"
	"github.com/stretchr/testify/require"
)

func TestBEpusdtOrderLifecycle(t *testing.T) {
	for _, currency := range []string{"CNY", "USD"} {
		t.Run(currency, func(t *testing.T) {
			testBEpusdtOrderLifecycle(t, currency)
		})
	}
}

func testBEpusdtOrderLifecycle(t *testing.T, currency string) {
	t.Helper()
	const (
		tradeID = "BepusdtTestTrade01"
		orderID = "sub2_test_usdt"
	)
	status, reportedOrder := bepusdtWaiting, orderID
	var provider *BEpusdt
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		var payload map[string]any
		if err := json.NewDecoder(req.Body).Decode(&payload); err != nil {
			t.Error(err)
			w.WriteHeader(http.StatusBadRequest)
			return
		}
		var result map[string]any
		switch req.URL.Path {
		case "/api/v1/order/create-transaction":
			timeout, _ := payload["timeout"].(float64)
			if payload["signature"] != provider.sign(payload) || payload["fiat"] != currency ||
				payload["trade_type"] != "usdt.bep20" || payload["amount"] != 20.15 ||
				timeout < 1750 || payload["order_id"] != orderID {
				t.Errorf("unexpected create order payload: %v", payload)
			}
			result = map[string]any{
				"trade_id": tradeID, "order_id": orderID, "trade_type": "usdt.bep20", "fiat": currency,
				"status": bepusdtWaiting, "amount": "20.15", "actual_amount": "2.925",
				"expiration_time": 1799, "payment_url": "http://internal:8080/pay/checkout/" + tradeID,
			}
		case "/api/v1/pay/info":
			if payload["trade_id"] != tradeID || payload["signature"] != nil {
				t.Errorf("unexpected query payload: %v", payload)
			}
			result = map[string]any{
				"trade_id": tradeID, "order_id": reportedOrder, "trade_type": "usdt.bep20", "fiat": currency,
				"status": status, "money": "20.15", "actual_amount": "2.925",
			}
		case "/api/v1/order/cancel-transaction":
			if payload["trade_id"] != tradeID || payload["signature"] != provider.sign(payload) {
				t.Errorf("unexpected cancel payload: %v", payload)
			}
			result = map[string]any{"trade_id": tradeID}
		default:
			t.Errorf("unexpected API request: %s", req.URL.Path)
			w.WriteHeader(http.StatusNotFound)
			return
		}
		_ = json.NewEncoder(w).Encode(map[string]any{"status_code": 200, "data": result})
	}))
	defer server.Close()

	var err error
	provider, err = NewBEpusdt(map[string]string{
		"apiBase": server.URL, "checkoutOrigin": "https://checkout.example",
		"notifyUrl": "http://sub2api:8080/api/v1/payment/webhook/bepusdt",
		"returnUrl": "https://app.example/payment/result", "apiToken": "fixture-token",
	})
	require.NoError(t, err)
	ctx := context.Background()
	response, err := provider.CreatePayment(ctx, payment.CreatePaymentRequest{
		OrderID: orderID, PaymentType: payment.TypeUSDTBSC, Amount: "20.15", Currency: currency,
		ExpiresAt: time.Now().Add(30 * time.Minute),
	})
	require.NoError(t, err)
	require.Equal(t, "https://checkout.example/pay/checkout/"+tradeID, response.PayURL)
	require.Equal(t, tradeID, response.TradeNo)
	require.Equal(t, currency, response.Currency)
	require.Equal(t, "2.925", response.CryptoAmount)
	require.WithinDuration(t, time.Now().Add(30*time.Minute), response.ExpiresAt, 2*time.Second)

	status = bepusdtConfirming
	query, err := provider.QueryOrder(ctx, tradeID)
	require.NoError(t, err)
	require.Equal(t, payment.ProviderStatusConfirming, query.Status)
	require.Equal(t, orderID, query.Metadata["order_id"])
	require.Equal(t, currency, query.Metadata["currency"])
	require.Equal(t, "2.925", query.Metadata["crypto_amount"])

	fields := map[string]any{
		"trade_id": tradeID, "order_id": orderID, "status": 2.0, "amount": 20.15, "actual_amount": "2.925",
	}
	fields["signature"] = provider.sign(fields)
	raw, err := json.Marshal(fields)
	require.NoError(t, err)
	notification, err := provider.VerifyNotification(ctx, string(raw), nil)
	require.NoError(t, err)
	require.Equal(t, 20.15, notification.Amount)
	require.ErrorContains(t, provider.ConfirmNotification(ctx, notification), "does not match")
	status = bepusdtPaid
	require.NoError(t, provider.ConfirmNotification(ctx, notification))
	require.Equal(t, currency, notification.Metadata["currency"])
	notification.Metadata["crypto_amount"] = "2.926"
	require.ErrorContains(t, provider.ConfirmNotification(ctx, notification), "does not match")
	notification.Metadata["crypto_amount"] = "2.925"
	reportedOrder = "different-order"
	require.ErrorContains(t, provider.ConfirmNotification(ctx, notification), "does not match")
	_, err = provider.VerifyNotification(ctx, strings.Replace(string(raw), "20.15", "200.15", 1), nil)
	require.ErrorContains(t, err, "invalid webhook signature")
	require.NoError(t, provider.CancelPayment(ctx, tradeID))
}

func TestBEpusdtRejectsUnsafeURLs(t *testing.T) {
	for _, raw := range []string{"//evil.example", "http://checkout.example", "https://secret@checkout.example", "https://checkout.example/path"} {
		_, err := bepusdtURL(raw, true, true)
		require.Error(t, err, raw)
	}
}
