package service

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"math"
	"strings"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/payment"
	infraerrors "github.com/Wei-Shaw/sub2api/internal/pkg/errors"
	"github.com/shopspring/decimal"
)

const (
	rechargeQuoteTTL       = 10 * time.Minute
	rechargeQuoteTokenType = "recharge_quote"
)

type RechargeAmountRange struct {
	MinAmount float64 `json:"min_amount"`
	MaxAmount float64 `json:"max_amount"`
}

type RechargeQuote struct {
	RechargeAmountRange
	Amount                float64   `json:"amount"`
	Currency              string    `json:"currency"`
	PayAmount             float64   `json:"pay_amount"`
	FeeAmount             float64   `json:"fee_amount"`
	FeeRate               float64   `json:"fee_rate"`
	CreditedAmount        float64   `json:"credited_amount"`
	CreditedCredits       int64     `json:"credited_credits,string"`
	CreditedDisplayAmount float64   `json:"credited_display_amount"`
	BonusAmount           float64   `json:"bonus_amount"`
	BonusDisplayAmount    float64   `json:"bonus_display_amount"`
	QuoteID               string    `json:"quote_id"`
	ExpiresAt             time.Time `json:"expires_at"`
	BillingVersion        int64     `json:"billing_version"`

	amountMinor     int64
	feeMinor        int64
	bonusCredits    int64
	rechargeRate    float64
	payBase         float64
	payAmountStr    string
	payAmountCNY    float64
	selectionAmount float64
	billing         BillingCurrencyConfig
}

type rechargeQuoteClaims struct {
	TokenType      string  `json:"tk"`
	OrderNo        string  `json:"o"`
	UserID         int64   `json:"uid"`
	PaymentType    string  `json:"pt"`
	Amount         float64 `json:"a"`
	Currency       string  `json:"cur"`
	BillingVersion int64   `json:"bv"`
	Fingerprint    string  `json:"f"`
	ExpiresAt      int64   `json:"exp"`
}

// RechargeQuote issues a short-lived, signed quote for the site's current checkout currency.
func (s *PaymentService) RechargeQuote(ctx context.Context, req CreateOrderRequest) (*RechargeQuote, error) {
	cfg, err := s.configService.GetPaymentConfig(ctx)
	if err != nil {
		return nil, fmt.Errorf("get payment config: %w", err)
	}
	if !cfg.Enabled {
		return nil, infraerrors.Forbidden("PAYMENT_DISABLED", "payment system is disabled")
	}
	req.PaymentType = NormalizeVisibleMethod(req.PaymentType)
	req.OrderType = payment.OrderTypeBalance
	if _, err := s.validateOrderInput(ctx, req, cfg); err != nil {
		return nil, err
	}
	billing := cfg.rechargeBillingConfig(ctx)
	if req.Currency != billing.DisplayCurrency || req.BillingVersion != billing.Version {
		return nil, rechargeQuoteChanged()
	}
	quote, err := s.buildSiteRechargeQuote(ctx, req, cfg)
	if err != nil {
		return nil, err
	}
	resume := s.paymentResume()
	if err := resume.ensureSigningKey(); err != nil {
		return nil, err
	}
	var nonce [12]byte
	if _, err := rand.Read(nonce[:]); err != nil {
		return nil, fmt.Errorf("generate recharge quote ID: %w", err)
	}
	claims := rechargeQuoteClaims{
		TokenType:      rechargeQuoteTokenType,
		OrderNo:        "sub2q_" + hex.EncodeToString(nonce[:]),
		UserID:         req.UserID,
		PaymentType:    req.PaymentType,
		Amount:         req.Amount,
		Currency:       quote.Currency,
		BillingVersion: quote.BillingVersion,
		Fingerprint:    quote.fingerprint(),
		ExpiresAt:      time.Now().Add(rechargeQuoteTTL).Unix(),
	}
	quote.QuoteID, err = resume.createSignedToken(claims)
	if err != nil {
		return nil, err
	}
	quote.ExpiresAt = time.Unix(claims.ExpiresAt, 0).UTC()
	return quote, nil
}

func (s *PaymentService) buildSiteRechargeQuote(ctx context.Context, req CreateOrderRequest, cfg *PaymentConfig) (*RechargeQuote, error) {
	billing := cfg.rechargeBillingConfig(ctx)
	if (req.Currency != "" && !strings.EqualFold(strings.TrimSpace(req.Currency), billing.DisplayCurrency)) ||
		(req.BillingVersion > 0 && req.BillingVersion != billing.Version) {
		return nil, rechargeQuoteChanged()
	}
	limits, err := s.configService.GetCheckoutMethodLimits(ctx, cfg)
	if err != nil {
		return nil, err
	}
	method, ok := limits.RechargeMethods(billing.DisplayCurrency)[req.PaymentType]
	if !ok {
		return nil, infraerrors.BadRequest("PAYMENT_METHOD_CURRENCY_MISMATCH", "payment method is unavailable for the current site currency")
	}
	quote, err := buildSiteRechargePrice(cfg, billing, req.Amount)
	if err != nil {
		return nil, err
	}
	selectionAmount := quote.PayAmount
	if req.PaymentType == payment.TypeUSDTBSC {
		selectionAmount = quote.payAmountCNY
	}
	if (method.SingleMin > 0 && selectionAmount < method.SingleMin) ||
		(method.SingleMax > 0 && selectionAmount > method.SingleMax) {
		return nil, infraerrors.BadRequest("INVALID_AMOUNT", "payment amount is outside the provider limits")
	}
	quote.selectionAmount = selectionAmount
	return quote, nil
}

func rechargeProviderSupportsCurrency(sel *payment.InstanceSelection, method, currency string) bool {
	if method == payment.TypeUSDTBSC {
		return sel.ProviderKey == payment.TypeBEpusdt && (currency == BillingDisplayUSD || currency == BillingDisplayCNY)
	}
	return paymentProviderConfigCurrency(sel.ProviderKey, sel.Config) == currency
}

func rechargeQuoteExpired() error {
	return infraerrors.Conflict("RECHARGE_QUOTE_EXPIRED", "recharge quote has expired")
}

func rechargeQuoteChanged() error {
	return infraerrors.Conflict("RECHARGE_QUOTE_CHANGED", "recharge terms changed; request a new quote")
}

func (s *PaymentService) parseRechargeQuote(req CreateOrderRequest) (*rechargeQuoteClaims, error) {
	token := strings.TrimSpace(req.QuoteID)
	if token == "" {
		return nil, infraerrors.BadRequest("RECHARGE_QUOTE_REQUIRED", "a recharge quote is required")
	}
	if err := s.paymentResume().ensureSigningKey(); err != nil {
		return nil, err
	}
	var claims rechargeQuoteClaims
	if err := s.paymentResume().parseSignedToken(token, &claims); err != nil ||
		claims.TokenType != rechargeQuoteTokenType || claims.UserID != req.UserID ||
		claims.PaymentType != req.PaymentType || claims.Amount != req.Amount || claims.OrderNo == "" ||
		claims.ExpiresAt <= 0 || claims.Fingerprint == "" {
		return nil, rechargeQuoteChanged()
	}
	return &claims, nil
}

func (q *RechargeQuote) fingerprint() string {
	data, _ := json.Marshal(struct {
		Amount         float64
		Currency       string
		PayAmount      string
		FeeRate        float64
		Credits        int64
		BonusCredits   int64
		Rate           float64
		BillingVersion int64
	}{q.Amount, q.Currency, q.payAmountStr, q.FeeRate, q.CreditedCredits, q.bonusCredits, q.billing.USDToCNY, q.BillingVersion})
	return fmt.Sprintf("%x", sha256.Sum256(data))
}

func (cfg *PaymentConfig) rechargeBillingConfig(ctx context.Context) BillingCurrencyConfig {
	if cfg.billingCurrency != nil {
		return *cfg.billingCurrency
	}
	return GetBillingCurrencySettings(ctx)
}

func (cfg *PaymentConfig) RechargeCheckoutCurrency(ctx context.Context) (BillingCurrencyConfig, RechargeAmountRange) {
	billing := cfg.rechargeBillingConfig(ctx)
	return billing, rechargeAmountRange(cfg, billing, billing.DisplayCurrency)
}

func rechargeAmountRange(cfg *PaymentConfig, billing BillingCurrencyConfig, currency string) RechargeAmountRange {
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

func buildSiteRechargePrice(cfg *PaymentConfig, billing BillingCurrencyConfig, amount float64) (*RechargeQuote, error) {
	currency := billing.DisplayCurrency
	if !isValidProviderAmount(amount) {
		return nil, infraerrors.BadRequest("INVALID_AMOUNT", "amount must be a positive number")
	}
	fiat := decimal.NewFromFloat(amount)
	amountMinor, err := payment.AmountToMinorUnit(fiat.String(), currency)
	if err != nil {
		return nil, infraerrors.BadRequest("INVALID_AMOUNT", err.Error()).WithMetadata(map[string]string{"currency": currency})
	}
	fx := decimal.NewFromInt(1)
	if currency == BillingDisplayUSD {
		fx = decimal.NewFromFloat(billing.USDToCNY)
	}
	cnyAmount := fiat.Mul(fx)
	bounds := rechargeAmountRange(cfg, billing, currency)
	if (cfg.MinAmount > 0 && cnyAmount.LessThan(decimal.NewFromFloat(cfg.MinAmount))) ||
		(cfg.MaxAmount > 0 && cnyAmount.GreaterThan(decimal.NewFromFloat(cfg.MaxAmount))) {
		return nil, infraerrors.BadRequest("INVALID_AMOUNT", "amount out of range").WithMetadata(map[string]string{
			"currency": currency, "min": fmt.Sprintf("%.2f", bounds.MinAmount), "max": fmt.Sprintf("%.2f", bounds.MaxAmount),
		})
	}

	rate := decimal.NewFromInt(1)
	if currency == BillingDisplayCNY {
		rate = decimal.NewFromFloat(billing.USDToCNY)
	}
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
	pay, err := decimal.NewFromString(payAmountStr)
	if err != nil {
		return nil, fmt.Errorf("parse recharge payment amount: %w", err)
	}
	fee := pay.Sub(payBase)
	return &RechargeQuote{
		RechargeAmountRange: bounds,
		Amount:              amount, Currency: currency, PayAmount: payAmount,
		FeeAmount: fee.InexactFloat64(), FeeRate: cfg.RechargeFeeRate,
		amountMinor: amountMinor, feeMinor: fee.Shift(int32(payment.CurrencyMinorUnit(currency))).IntPart(),
		CreditedAmount: CreditsToUSD(credits.IntPart()), CreditedCredits: credits.IntPart(),
		CreditedDisplayAmount: CreditsToDisplayAmount(credits.IntPart(), billing),
		BonusAmount:           CreditsToUSD(bonus.IntPart()), BonusDisplayAmount: CreditsToDisplayAmount(bonus.IntPart(), billing),
		bonusCredits: bonus.IntPart(), BillingVersion: billing.Version, rechargeRate: rate.InexactFloat64(),
		payBase: payBase.InexactFloat64(), payAmountStr: payAmountStr,
		payAmountCNY: pay.Mul(fx).InexactFloat64(), billing: billing,
	}, nil
}
