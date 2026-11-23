package service

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"entgo.io/ent/dialect"
	dbent "github.com/Wei-Shaw/sub2api/ent"
	"github.com/Wei-Shaw/sub2api/ent/paymentorder"
	"github.com/Wei-Shaw/sub2api/ent/setting"
	"github.com/Wei-Shaw/sub2api/ent/user"
	"github.com/Wei-Shaw/sub2api/internal/payment"
	infraerrors "github.com/Wei-Shaw/sub2api/internal/pkg/errors"
)

const rechargeCheckoutSnapshotKey = "recharge_checkout_response"

var errRechargeQuoteUsed = errors.New("recharge quote already created an order")

// lockRechargeOrderCreation serializes pending/daily limit checks for a user
// and fences the currency version until the new order commits.
func (s *PaymentService) lockRechargeOrderCreation(ctx context.Context, tx *dbent.Tx, req CreateOrderRequest) error {
	if paymentAuditDialect(tx.Client()) == dialect.Postgres {
		_, err := tx.User.Query().Where(user.IDEQ(req.UserID)).Select(user.FieldID).ForUpdate().Only(ctx)
		if err != nil {
			return fmt.Errorf("lock recharge user: %w", err)
		}
	}
	exists, err := tx.PaymentOrder.Query().Where(paymentorder.OutTradeNo(req.rechargeOrderNo), paymentorder.UserIDEQ(req.UserID)).Exist(ctx)
	if err != nil {
		return fmt.Errorf("look up recharge quote order: %w", err)
	}
	if exists {
		return errRechargeQuoteUsed
	}
	if time.Now().After(req.rechargeQuote.ExpiresAt) {
		return rechargeQuoteExpired()
	}
	if paymentAuditDialect(tx.Client()) != dialect.Postgres {
		return nil
	}
	row, err := tx.Setting.Query().Where(setting.KeyEQ(SettingKeyBillingCurrencyConfig)).ForShare().Only(ctx)
	if dbent.IsNotFound(err) {
		return nil
	}
	if err != nil {
		return fmt.Errorf("lock platform currency for recharge: %w", err)
	}
	billing, err := parseBillingCurrencyConfig(row.Value)
	if err != nil || billing.Version != req.rechargeQuote.BillingVersion ||
		billing.DisplayCurrency != req.rechargeQuote.Currency || billing.USDToCNY != req.rechargeQuote.billing.USDToCNY {
		return rechargeQuoteChanged()
	}
	return nil
}

// Store the action response alongside the order once the provider session has
// initialized. A retried HTTP request can recover it without recreating a
// provider order. Client secrets and JSAPI payloads are encrypted at rest.
func (s *PaymentService) storeRechargeCheckoutResponse(order *dbent.PaymentOrder, resp *CreateOrderResponse) error {
	raw, err := json.Marshal(resp)
	if err != nil {
		return fmt.Errorf("encode recharge checkout response: %w", err)
	}
	key := rechargeCheckoutKey(s.paymentResume().signingKey)
	ciphertext, err := payment.Encrypt(string(raw), key[:])
	if err != nil {
		return fmt.Errorf("protect recharge checkout response: %w", err)
	}
	order.ProviderSnapshot[rechargeCheckoutSnapshotKey] = ciphertext
	return nil
}

func (s *PaymentService) replayRechargeOrder(ctx context.Context, client *dbent.Client, userID int64, orderNo string) (*CreateOrderResponse, error) {
	order, err := client.PaymentOrder.Query().Where(paymentorder.OutTradeNo(orderNo), paymentorder.UserIDEQ(userID)).Only(ctx)
	if dbent.IsNotFound(err) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("look up recharge quote order: %w", err)
	}
	if order.Status == OrderStatusFailed {
		return nil, infraerrors.Conflict("RECHARGE_ORDER_FAILED", "the order created from this quote failed; check the order before requesting a new quote")
	}
	if order.Status != OrderStatusPending || !time.Now().Before(order.ExpiresAt) {
		return nil, infraerrors.Conflict("RECHARGE_ORDER_CLOSED", "the order created from this quote is no longer payable").
			WithMetadata(map[string]string{"order_id": fmt.Sprint(order.ID), "status": order.Status})
	}
	ciphertext := psSnapshotStringValue(order.ProviderSnapshot[rechargeCheckoutSnapshotKey])
	if ciphertext == "" {
		return nil, infraerrors.Conflict("RECHARGE_ORDER_PROCESSING", "the recharge order is initializing; retry the same quote")
	}
	for _, signingKey := range s.paymentResume().verifyKeys {
		key := rechargeCheckoutKey(signingKey)
		raw, err := payment.Decrypt(ciphertext, key[:])
		if err != nil {
			continue
		}
		var response CreateOrderResponse
		if err := json.Unmarshal([]byte(raw), &response); err != nil {
			return nil, fmt.Errorf("restore recharge checkout response: %w", err)
		}
		response.Status = order.Status
		return &response, nil
	}
	return nil, infraerrors.Conflict("RECHARGE_QUOTE_CHANGED", "the checkout details must be recovered from the existing order")
}

func rechargeCheckoutKey(signingKey []byte) [32]byte {
	return sha256.Sum256(append([]byte("milorouter:recharge-checkout-response:v1:"), signingKey...))
}
