package provider

import (
	"bytes"
	"context"
	"crypto/subtle"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"regexp"
	"strings"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/payment"
	"github.com/shopspring/decimal"
)

const (
	bepusdtTradeType    = "usdt.bep20"
	bepusdtCheckoutPath = "/pay/checkout/"
	bepusdtWaiting      = 1
	bepusdtPaid         = 2
	bepusdtExpired      = 3
	bepusdtCancelled    = 4
	bepusdtConfirming   = 5
	bepusdtFailed       = 6
)

var bepusdtTradeID = regexp.MustCompile("^[A-Za-z0-9]{18}$")

type BEpusdt struct {
	apiBase        string
	checkoutOrigin string
	notifyURL      string
	returnURL      string
	token          string
	client         *http.Client
}

func NewBEpusdt(config map[string]string) (*BEpusdt, error) {
	token := strings.TrimSpace(config["apiToken"])
	if token == "" {
		return nil, fmt.Errorf("bepusdt config missing apiToken")
	}
	apiBase, err := bepusdtURL(config["apiBase"], false, true)
	if err != nil {
		return nil, fmt.Errorf("bepusdt apiBase: %w", err)
	}
	checkoutOrigin, err := bepusdtURL(config["checkoutOrigin"], true, true)
	if err != nil {
		return nil, fmt.Errorf("bepusdt checkoutOrigin: %w", err)
	}
	notifyURL, err := bepusdtURL(config["notifyUrl"], false, false)
	if err != nil {
		return nil, fmt.Errorf("bepusdt notifyUrl: %w", err)
	}
	returnURL, err := bepusdtURL(config["returnUrl"], true, false)
	if err != nil {
		return nil, fmt.Errorf("bepusdt returnUrl: %w", err)
	}
	return &BEpusdt{
		apiBase: apiBase, checkoutOrigin: checkoutOrigin, notifyURL: notifyURL, returnURL: returnURL, token: token,
		client: &http.Client{
			Timeout: 10 * time.Second,
			CheckRedirect: func(_ *http.Request, _ []*http.Request) error {
				return http.ErrUseLastResponse
			},
		},
	}, nil
}

func bepusdtURL(raw string, httpsOnly, originOnly bool) (string, error) {
	u, err := url.Parse(strings.TrimSpace(raw))
	if err != nil || u.Hostname() == "" || u.User != nil || u.Fragment != "" ||
		(u.Scheme != "https" && (httpsOnly || u.Scheme != "http")) {
		return "", fmt.Errorf("invalid HTTP(S) URL")
	}
	if originOnly {
		if (u.Path != "" && u.Path != "/") || u.RawQuery != "" || u.ForceQuery {
			return "", fmt.Errorf("expected a URL origin")
		}
		return u.Scheme + "://" + u.Host, nil
	}
	return u.String(), nil
}

func (b *BEpusdt) Name() string        { return "BEpusdt" }
func (b *BEpusdt) ProviderKey() string { return payment.TypeBEpusdt }
func (b *BEpusdt) SupportedTypes() []payment.PaymentType {
	return []payment.PaymentType{payment.TypeUSDTBSC}
}

type bepusdtOrder struct {
	TradeID        string           `json:"trade_id"`
	OrderID        string           `json:"order_id"`
	TradeType      string           `json:"trade_type"`
	Fiat           string           `json:"fiat"`
	Money          *decimal.Decimal `json:"money"`
	Amount         *decimal.Decimal `json:"amount"`
	Status         int              `json:"status"`
	ExpirationTime int64            `json:"expiration_time"`
}

func (b *BEpusdt) CreatePayment(ctx context.Context, req payment.CreatePaymentRequest) (*payment.CreatePaymentResponse, error) {
	if req.PaymentType != payment.TypeUSDTBSC {
		return nil, fmt.Errorf("bepusdt supports only USDT on BSC")
	}
	minorAmount, err := payment.AmountToMinorUnit(req.Amount, payment.DefaultPaymentCurrency)
	if err != nil || minorAmount <= 0 {
		return nil, fmt.Errorf("bepusdt invalid CNY amount")
	}
	returnURL := strings.TrimSpace(req.ReturnURL)
	if returnURL == "" {
		returnURL = b.returnURL
	}
	returnURL, err = bepusdtURL(returnURL, true, false)
	if err != nil {
		return nil, fmt.Errorf("bepusdt return URL: %w", err)
	}
	amount := decimal.NewFromInt(minorAmount).Shift(-2)
	payload := map[string]any{
		"order_id": req.OrderID, "amount": amount.InexactFloat64(), "name": req.Subject,
		"fiat": payment.DefaultPaymentCurrency, "trade_type": bepusdtTradeType,
		"notify_url": b.notifyURL, "redirect_url": returnURL,
	}
	if !req.ExpiresAt.IsZero() {
		payload["timeout"] = float64(max(180, min(3600, int(time.Until(req.ExpiresAt).Seconds()))))
	}
	var order bepusdtOrder
	if err := b.post(ctx, "/api/v1/order/create-transaction", payload, true, &order); err != nil {
		return nil, err
	}
	if !bepusdtTradeID.MatchString(order.TradeID) || order.OrderID != req.OrderID ||
		order.TradeType != bepusdtTradeType || order.Fiat != payment.DefaultPaymentCurrency ||
		order.Amount == nil || !order.Amount.Equal(amount) || order.Status != bepusdtWaiting ||
		order.ExpirationTime <= 0 || order.ExpirationTime > 3600 {
		return nil, fmt.Errorf("bepusdt create order response did not match the request")
	}
	return &payment.CreatePaymentResponse{
		TradeNo: order.TradeID, PayURL: b.checkoutOrigin + bepusdtCheckoutPath + order.TradeID,
		Currency: order.Fiat, ExpiresAt: time.Now().Add(time.Duration(order.ExpirationTime) * time.Second),
	}, nil
}

func (b *BEpusdt) queryOrder(ctx context.Context, tradeID string) (*bepusdtOrder, error) {
	if !bepusdtTradeID.MatchString(tradeID) {
		return nil, fmt.Errorf("bepusdt invalid trade ID")
	}
	var order bepusdtOrder
	if err := b.post(ctx, "/api/v1/pay/info", map[string]any{"trade_id": tradeID}, false, &order); err != nil {
		return nil, err
	}
	if order.TradeID != tradeID || order.OrderID == "" || order.TradeType != bepusdtTradeType ||
		order.Fiat != payment.DefaultPaymentCurrency || order.Money == nil || !order.Money.IsPositive() {
		return nil, fmt.Errorf("bepusdt order query response did not match the trade")
	}
	return &order, nil
}

func (b *BEpusdt) QueryOrder(ctx context.Context, tradeID string) (*payment.QueryOrderResponse, error) {
	order, err := b.queryOrder(ctx, tradeID)
	if err != nil {
		return nil, err
	}
	var status string
	switch order.Status {
	case bepusdtWaiting:
		status = payment.ProviderStatusPending
	case bepusdtConfirming:
		status = payment.ProviderStatusConfirming
	case bepusdtPaid:
		status = payment.ProviderStatusPaid
	case bepusdtExpired, bepusdtCancelled, bepusdtFailed:
		status = payment.ProviderStatusFailed
	default:
		return nil, fmt.Errorf("bepusdt unknown payment status")
	}
	return &payment.QueryOrderResponse{
		TradeNo: order.TradeID, Status: status, Amount: order.Money.InexactFloat64(),
		Metadata: map[string]string{"order_id": order.OrderID},
	}, nil
}

func (b *BEpusdt) VerifyNotification(_ context.Context, rawBody string, _ map[string]string) (*payment.PaymentNotification, error) {
	var fields map[string]any
	if err := json.Unmarshal([]byte(rawBody), &fields); err != nil {
		return nil, fmt.Errorf("bepusdt invalid webhook JSON")
	}
	signature, ok := fields["signature"].(string)
	if !ok || subtle.ConstantTimeCompare([]byte(signature), []byte(b.sign(fields))) != 1 {
		return nil, fmt.Errorf("bepusdt invalid webhook signature")
	}
	var notice bepusdtOrder
	if err := json.Unmarshal([]byte(rawBody), &notice); err != nil {
		return nil, fmt.Errorf("bepusdt invalid webhook fields")
	}
	switch notice.Status {
	case bepusdtPaid:
	case bepusdtWaiting, bepusdtExpired, bepusdtCancelled, bepusdtConfirming, bepusdtFailed:
		return nil, nil
	default:
		return nil, fmt.Errorf("bepusdt unknown webhook status")
	}
	if !bepusdtTradeID.MatchString(notice.TradeID) || notice.OrderID == "" ||
		notice.Amount == nil || !notice.Amount.IsPositive() {
		return nil, fmt.Errorf("bepusdt incomplete paid webhook")
	}
	return &payment.PaymentNotification{
		TradeNo: notice.TradeID, OrderID: notice.OrderID, Amount: notice.Amount.InexactFloat64(),
		Status: payment.NotificationStatusSuccess, Metadata: map[string]string{"order_id": notice.OrderID},
	}, nil
}

func (b *BEpusdt) ConfirmNotification(ctx context.Context, n *payment.PaymentNotification) error {
	order, err := b.queryOrder(ctx, n.TradeNo)
	if err != nil {
		return err
	}
	if order.Status != bepusdtPaid || order.OrderID != n.OrderID ||
		!order.Money.Equal(decimal.NewFromFloat(n.Amount)) {
		return fmt.Errorf("bepusdt paid webhook does not match the upstream order")
	}
	return nil
}

func (b *BEpusdt) CancelPayment(ctx context.Context, tradeID string) error {
	if !bepusdtTradeID.MatchString(tradeID) {
		return fmt.Errorf("bepusdt invalid trade ID")
	}
	return b.post(ctx, "/api/v1/order/cancel-transaction", map[string]any{"trade_id": tradeID}, true, nil)
}

func (b *BEpusdt) Refund(context.Context, payment.RefundRequest) (*payment.RefundResponse, error) {
	return nil, fmt.Errorf("bepusdt does not support automatic refunds")
}

func (b *BEpusdt) sign(fields map[string]any) string {
	params := make(map[string]string, len(fields))
	for key, val := range fields {
		if key != "signature" && val != nil {
			params[key] = fmt.Sprint(val)
		}
	}
	return easyPaySign(params, b.token)
}

func (b *BEpusdt) post(ctx context.Context, path string, payload map[string]any, signed bool, target any) error {
	if signed {
		payload["signature"] = b.sign(payload)
	}
	body, err := json.Marshal(payload)
	if err != nil {
		return fmt.Errorf("bepusdt encode request: %w", err)
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, b.apiBase+path, bytes.NewReader(body))
	if err != nil {
		return fmt.Errorf("bepusdt build request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err := b.client.Do(req)
	if err != nil {
		return fmt.Errorf("bepusdt request: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("bepusdt returned HTTP %d", resp.StatusCode)
	}
	var envelope struct {
		StatusCode int             `json:"status_code"`
		Data       json.RawMessage `json:"data"`
	}
	if err := json.NewDecoder(io.LimitReader(resp.Body, 64<<10)).Decode(&envelope); err != nil {
		return fmt.Errorf("bepusdt decode response: %w", err)
	}
	if envelope.StatusCode != http.StatusOK {
		return fmt.Errorf("bepusdt rejected the request (status %d)", envelope.StatusCode)
	}
	if target != nil {
		if err := json.Unmarshal(envelope.Data, target); err != nil {
			return fmt.Errorf("bepusdt decode response data: %w", err)
		}
	}
	return nil
}
