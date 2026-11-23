package service

import (
	"strings"

	dbent "github.com/Wei-Shaw/sub2api/ent"
	"github.com/Wei-Shaw/sub2api/internal/payment"
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

// The legacy amount columns store cents. Present the exact value for orders
// created with a credit snapshot so display and the ledger use the same amount.
func PaymentOrderCreditedAmount(order *dbent.PaymentOrder) float64 {
	if order.PaymentType == payment.TypeUSDTBSC && order.CreditedCredits > 0 {
		return CreditsToUSD(order.CreditedCredits)
	}
	return order.Amount
}

func PaymentOrderBonusAmount(order *dbent.PaymentOrder) float64 {
	if order.PaymentType == payment.TypeUSDTBSC && order.BonusCredits > 0 {
		return CreditsToUSD(order.BonusCredits)
	}
	return order.BonusAmount
}
