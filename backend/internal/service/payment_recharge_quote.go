package service

import (
	"context"
	"fmt"
	"math"
	"strings"

	"github.com/Wei-Shaw/sub2api/internal/payment"
	infraerrors "github.com/Wei-Shaw/sub2api/internal/pkg/errors"
	"github.com/shopspring/decimal"
)

type RechargeAmountRange struct {
	MinAmount float64 `json:"min_amount"`
	MaxAmount float64 `json:"max_amount"`
}

type RechargeQuote struct {
	RechargeAmountRange
	Amount          float64 `json:"amount"`
	Currency        string  `json:"currency"`
	PayAmount       float64 `json:"pay_amount"`
	FeeAmount       float64 `json:"fee_amount"`
	FeeRate         float64 `json:"fee_rate"`
	CreditedAmount  float64 `json:"credited_amount"`
	CreditedCredits int64   `json:"credited_credits"`
	BonusAmount     float64 `json:"bonus_amount"`

	bonusCredits int64
	payBase      float64
	payAmountStr string
	payAmountCNY float64
	billing      BillingCurrencyConfig
}

// RechargeQuote returns the same fiat and credit amounts used at order creation.
// BEpusdt assigns the final USDT amount when the gateway order is created.
func (s *PaymentService) RechargeQuote(ctx context.Context, req CreateOrderRequest) (*RechargeQuote, error) {
	if NormalizeVisibleMethod(req.PaymentType) != payment.TypeUSDTBSC {
		return nil, infraerrors.BadRequest("INVALID_PAYMENT_TYPE", "recharge quotes are available for USDT (BEP20)")
	}
	cfg, err := s.configService.GetPaymentConfig(ctx)
	if err != nil {
		return nil, fmt.Errorf("get payment config: %w", err)
	}
	if !cfg.Enabled {
		return nil, infraerrors.Forbidden("PAYMENT_DISABLED", "payment system is disabled")
	}
	req.PaymentType = payment.TypeUSDTBSC
	req.OrderType = payment.OrderTypeBalance
	if _, err := s.validateOrderInput(ctx, req, cfg); err != nil {
		return nil, err
	}
	billing := cfg.rechargeBillingConfig(ctx)
	if strings.TrimSpace(req.Currency) == "" {
		req.Currency = billing.DisplayCurrency
	}
	quote, err := buildUSDTRechargeQuote(cfg, billing, req.Amount, req.Currency)
	if err != nil {
		return nil, err
	}
	limits, err := s.configService.GetAvailableMethodLimits(ctx)
	if err != nil {
		return nil, err
	}
	method, ok := limits.Methods[req.PaymentType]
	if !ok {
		return nil, infraerrors.ServiceUnavailable("PAYMENT_GATEWAY_ERROR", "method_not_configured")
	}
	if (method.SingleMin > 0 && quote.payAmountCNY < method.SingleMin) ||
		(method.SingleMax > 0 && quote.payAmountCNY > method.SingleMax) {
		return nil, infraerrors.BadRequest("INVALID_AMOUNT", "payment amount is outside the USDT provider limits")
	}
	return quote, nil
}

func (cfg *PaymentConfig) rechargeBillingConfig(ctx context.Context) BillingCurrencyConfig {
	if cfg.billingCurrency != nil {
		return *cfg.billingCurrency
	}
	return GetBillingCurrencySettings(ctx)
}

func (cfg *PaymentConfig) RechargeQuoteLimits(ctx context.Context) map[string]RechargeAmountRange {
	billing := cfg.rechargeBillingConfig(ctx)
	return map[string]RechargeAmountRange{
		BillingDisplayCNY: usdtRechargeRange(cfg, billing, BillingDisplayCNY),
		BillingDisplayUSD: usdtRechargeRange(cfg, billing, BillingDisplayUSD),
	}
}

func usdtRechargeRange(cfg *PaymentConfig, billing BillingCurrencyConfig, currency string) RechargeAmountRange {
	if currency == BillingDisplayCNY {
		return RechargeAmountRange{MinAmount: cfg.MinAmount, MaxAmount: cfg.MaxAmount}
	}
	// Existing payment limits are in CNY. Round inward to USD cents so the
	// advertised edge amounts also satisfy the configured CNY limits.
	fx := decimal.NewFromFloat(billing.USDToCNY)
	min, minRemainder := decimal.NewFromFloat(cfg.MinAmount).QuoRem(fx, 2)
	if minRemainder.IsPositive() {
		min = min.Add(decimal.New(1, -2))
	}
	max, _ := decimal.NewFromFloat(cfg.MaxAmount).QuoRem(fx, 2)
	return RechargeAmountRange{MinAmount: min.InexactFloat64(), MaxAmount: max.InexactFloat64()}
}

func buildUSDTRechargeQuote(cfg *PaymentConfig, billing BillingCurrencyConfig, amount float64, requestedCurrency string) (*RechargeQuote, error) {
	currency := strings.ToUpper(strings.TrimSpace(requestedCurrency))
	if currency == "" {
		currency = BillingDisplayCNY // Legacy checkout requests omit the currency.
	}
	if currency != BillingDisplayCNY && currency != BillingDisplayUSD {
		return nil, infraerrors.BadRequest("INVALID_CURRENCY", "USDT (BEP20) accepts quotes in USD or CNY")
	}
	if !isValidProviderAmount(amount) {
		return nil, infraerrors.BadRequest("INVALID_AMOUNT", "amount must be a positive number")
	}
	if err := validateCreateOrderAmountCurrency(amount, currency); err != nil {
		return nil, err
	}
	fiat := decimal.NewFromFloat(amount)
	fx := decimal.NewFromInt(1)
	if currency == BillingDisplayUSD {
		fx = decimal.NewFromFloat(billing.USDToCNY)
	}
	cnyAmount := fiat.Mul(fx)
	bounds := usdtRechargeRange(cfg, billing, currency)
	if (cfg.MinAmount > 0 && cnyAmount.LessThan(decimal.NewFromFloat(cfg.MinAmount))) ||
		(cfg.MaxAmount > 0 && cnyAmount.GreaterThan(decimal.NewFromFloat(cfg.MaxAmount))) {
		return nil, infraerrors.BadRequest("INVALID_AMOUNT", "amount out of range").WithMetadata(map[string]string{
			"currency": currency, "min": fmt.Sprintf("%.2f", bounds.MinAmount), "max": fmt.Sprintf("%.2f", bounds.MaxAmount),
		})
	}

	rate := decimal.NewFromFloat(billing.RechargeRate(currency))
	creditNumerator := fiat.Mul(decimal.NewFromInt(CreditsPerUSD))
	credits := creditNumerator.DivRound(rate, 0)
	bonus := decimal.Zero
	payBase := fiat
	if tier, ok := matchRechargeBonusTier(cfg.RechargeBonusTiers, cnyAmount.InexactFloat64()); ok {
		percent := decimal.NewFromFloat(tier.BonusPercent).Shift(-2)
		mode, _ := NormalizeRechargeBonusMode(cfg.RechargeBonusMode)
		if mode == RechargeBonusModeDiscount {
			discounted := fiat.Mul(decimal.NewFromInt(1).Sub(percent)).Round(2)
			if discounted.IsPositive() && discounted.LessThan(fiat) {
				payBase = discounted
				bonus = credits.Sub(discounted.Mul(decimal.NewFromInt(CreditsPerUSD)).DivRound(rate, 0))
			}
		} else {
			bonus = creditNumerator.Mul(percent).DivRound(rate, 0)
			credits = credits.Add(bonus)
		}
	}
	if !credits.IsPositive() || credits.GreaterThan(decimal.NewFromInt(math.MaxInt64)) {
		return nil, infraerrors.BadRequest("INVALID_AMOUNT", "recharge amount cannot be represented in credits")
	}
	payAmountStr, payAmount, err := calculateCreateOrderPayAmount(payBase.InexactFloat64(), cfg.RechargeFeeRate, currency)
	if err != nil {
		return nil, err
	}
	pay := decimal.NewFromFloat(payAmount)
	return &RechargeQuote{
		RechargeAmountRange: bounds,
		Amount:              amount, Currency: currency, PayAmount: payAmount,
		FeeAmount: pay.Sub(payBase).InexactFloat64(), FeeRate: cfg.RechargeFeeRate,
		CreditedAmount: CreditsToUSD(credits.IntPart()), CreditedCredits: credits.IntPart(),
		BonusAmount: CreditsToUSD(bonus.IntPart()), bonusCredits: bonus.IntPart(),
		payBase: payBase.InexactFloat64(), payAmountStr: payAmountStr,
		payAmountCNY: pay.Mul(fx).InexactFloat64(), billing: billing,
	}, nil
}
