package service

import (
	"strings"
	"time"

	dbent "github.com/Wei-Shaw/sub2api/ent"
	"github.com/Wei-Shaw/sub2api/internal/payment"
	"github.com/shopspring/decimal"
)

func paymentProviderConfigCurrency(providerKey string, cfg map[string]string) string {
	switch strings.TrimSpace(providerKey) {
	case payment.TypeStripe, payment.TypeAirwallex:
		currency, err := payment.NormalizePaymentCurrency(cfg["currency"])
		if err == nil {
			return currency
		}
	}
	return payment.DefaultPaymentCurrency
}

func PaymentOrderCurrency(order *dbent.PaymentOrder) string {
	if snapshot := psOrderProviderSnapshot(order); snapshot != nil {
		if currency, err := payment.NormalizePaymentCurrency(snapshot.Currency); err == nil {
			return currency
		}
	}
	return payment.DefaultPaymentCurrency
}

func createOrderCurrency(req CreateOrderRequest, sel *payment.InstanceSelection) string {
	if req.rechargeQuote != nil {
		return req.rechargeQuote.Currency
	}
	if sel != nil {
		return paymentProviderConfigCurrency(sel.ProviderKey, sel.Config)
	}
	return payment.DefaultPaymentCurrency
}

type PaymentCryptoDetails struct {
	CryptoAmount   string `json:"crypto_amount,omitempty"`
	CryptoCurrency string `json:"crypto_currency,omitempty"`
	CryptoNetwork  string `json:"crypto_network,omitempty"`
}

func PaymentOrderCryptoDetails(order *dbent.PaymentOrder) PaymentCryptoDetails {
	if order == nil || order.PaymentType != payment.TypeUSDTBSC {
		return PaymentCryptoDetails{}
	}
	return PaymentCryptoDetails{
		CryptoAmount:   psSnapshotStringValue(order.ProviderSnapshot["crypto_amount"]),
		CryptoCurrency: "USDT",
		CryptoNetwork:  "BEP20",
	}
}

// PaymentOrderAccountingDetails describes the original value of a wallet credit.
// CreditedAt is set only once the order has finished posting to the wallet.
type PaymentOrderAccountingDetails struct {
	CreditedDisplayAmount   string     `json:"credited_display_amount,omitempty"`
	CreditedDisplayCurrency string     `json:"credited_display_currency,omitempty"`
	BonusDisplayAmount      string     `json:"bonus_display_amount,omitempty"`
	CreditedAt              *time.Time `json:"credited_at,omitempty"`
	RefundBlocked           bool       `json:"refund_blocked,omitempty"`
}

func PaymentOrderAccounting(order *dbent.PaymentOrder) PaymentOrderAccountingDetails {
	details := PaymentOrderAccountingDetails{RefundBlocked: paymentOrderRefundBlocked(order)}
	if order == nil || order.OrderType != payment.OrderTypeBalance {
		return details
	}

	currency := BillingDisplayUSD
	conversion := decimal.NewFromInt(1)
	if paymentOrderUsesUnifiedRechargePricing(order) &&
		psSnapshotStringValue(order.ProviderSnapshot["platform_currency"]) == BillingDisplayCNY &&
		isValidProviderAmount(order.UsdToCnySnapshot) {
		currency = BillingDisplayCNY
		conversion = decimal.NewFromFloat(order.UsdToCnySnapshot)
	}
	creditScale := decimal.NewFromInt(CreditsPerUSD)
	amount := decimal.NewFromFloat(order.Amount)
	if order.CreditedCredits > 0 {
		amount = decimal.NewFromInt(order.CreditedCredits).Div(creditScale)
	}
	bonus := decimal.NewFromFloat(order.BonusAmount)
	if order.BonusCredits > 0 {
		bonus = decimal.NewFromInt(order.BonusCredits).Div(creditScale)
	}
	details.CreditedDisplayAmount = amount.Mul(conversion).StringFixed(2)
	details.CreditedDisplayCurrency = currency
	if bonus.IsPositive() {
		details.BonusDisplayAmount = bonus.Mul(conversion).StringFixed(2)
	}
	details.CreditedAt = order.CompletedAt
	return details
}

func paymentOrderUsesUnifiedRechargePricing(order *dbent.PaymentOrder) bool {
	return order != nil && order.OrderType == payment.OrderTypeBalance &&
		psSnapshotIntValue(order.ProviderSnapshot["recharge_pricing_version"]) > 0
}

func paymentOrderRefundBlocked(order *dbent.PaymentOrder) bool {
	return order != nil && (NormalizeVisibleMethod(order.PaymentType) == payment.TypeUSDTBSC ||
		strings.TrimSpace(psStringValue(order.ProviderKey)) == payment.TypeBEpusdt ||
		psSnapshotStringValue(order.ProviderSnapshot["provider_key"]) == payment.TypeBEpusdt ||
		paymentOrderUsesUnifiedRechargePricing(order))
}

// The legacy amount columns store cents. Present the exact value for orders
// created with a credit snapshot so display and the ledger use the same amount.
func PaymentOrderCreditedAmount(order *dbent.PaymentOrder) float64 {
	if (order.PaymentType == payment.TypeUSDTBSC || paymentOrderUsesUnifiedRechargePricing(order)) && order.CreditedCredits > 0 {
		return CreditsToUSD(order.CreditedCredits)
	}
	return order.Amount
}

func PaymentOrderBonusAmount(order *dbent.PaymentOrder) float64 {
	if (order.PaymentType == payment.TypeUSDTBSC || paymentOrderUsesUnifiedRechargePricing(order)) && order.BonusCredits > 0 {
		return CreditsToUSD(order.BonusCredits)
	}
	return order.BonusAmount
}
