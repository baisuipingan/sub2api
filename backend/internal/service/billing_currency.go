package service

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"strings"
	"sync/atomic"
	"time"

	infraerrors "github.com/Wei-Shaw/sub2api/internal/pkg/errors"
	"github.com/shopspring/decimal"
)

const (
	// SettingKeyBillingCurrencyConfig stores the versioned display-currency and
	// recharge-price configuration. It is separate from payment-provider config.
	SettingKeyBillingCurrencyConfig = "billing_currency_config"

	// CreditsPerUSD is intentionally immutable. Existing NUMERIC(20,8) USD
	// amounts can be represented exactly by eight decimal places of a credit.
	CreditsPerUSD int64 = 100_000_000

	BillingEngineLegacy  = "legacy"
	BillingEngineShadow  = "shadow"
	BillingEngineCredits = "credits"

	BillingDisplayUSD = "USD"
	BillingDisplayCNY = "CNY"

	defaultBillingCurrencyCacheTTL = 30 * time.Second
	defaultDisplayUSDToCNY         = 7.2
)

var (
	ErrInvalidBillingCurrencyConfig = errors.New("invalid billing currency configuration")
	errBillingCurrencySettingMiss   = errors.New("billing currency setting not found")

	// billingCurrencyRuntime is a process-local read-through snapshot. The
	// settings table remains the source of truth; an asynchronous refresh keeps
	// every instance current without blocking the billing hot path.
	billingCurrencyRuntime       atomic.Pointer[cachedBillingCurrencyRuntime]
	billingCurrencyLoader        atomic.Value // func(context.Context) BillingCurrencyConfig
	billingCurrencyRefreshActive atomic.Bool
)

// BillingCurrencyConfig controls display conversion and the billing-engine
// rollout. The internal ledger always uses fixed integer credits.
type BillingCurrencyConfig struct {
	Enabled         bool               `json:"enabled"`
	Engine          string             `json:"engine"`
	CreditsPerUSD   int64              `json:"credits_per_usd"`
	DisplayCurrency string             `json:"display_currency"`
	USDToCNY        float64            `json:"usd_to_cny"`
	RechargeRates   map[string]float64 `json:"recharge_rates,omitempty"`
	UpdatedAt       time.Time          `json:"updated_at"`
	Version         int64              `json:"version"`
}

type cachedBillingCurrencyConfig struct {
	config    BillingCurrencyConfig
	expiresAt time.Time
}

type cachedBillingCurrencyRuntime struct {
	config    BillingCurrencyConfig
	expiresAt time.Time
}

// DefaultBillingCurrencyConfig mirrors the migration defaults: CNY display and
// shadow billing. Administrators switch to the credits engine after reconciliation.
func DefaultBillingCurrencyConfig() BillingCurrencyConfig {
	return BillingCurrencyConfig{
		Enabled:         true,
		Engine:          BillingEngineShadow,
		CreditsPerUSD:   CreditsPerUSD,
		DisplayCurrency: BillingDisplayCNY,
		USDToCNY:        defaultDisplayUSDToCNY,
		RechargeRates: map[string]float64{
			BillingDisplayCNY: defaultDisplayUSDToCNY,
			BillingDisplayUSD: 1,
		},
		UpdatedAt: time.Now().UTC(),
		Version:   1,
	}
}

// EffectiveEngine disables the credit engine when the feature switch is off.
func (c BillingCurrencyConfig) EffectiveEngine() string {
	if !c.Enabled {
		return BillingEngineLegacy
	}
	switch strings.ToLower(strings.TrimSpace(c.Engine)) {
	case BillingEngineShadow:
		return BillingEngineShadow
	case BillingEngineCredits:
		return BillingEngineCredits
	default:
		return BillingEngineLegacy
	}
}

// USDToCredits converts a USD-equivalent amount into the immutable integer
// ledger unit with half-away-from-zero rounding.
func (c BillingCurrencyConfig) USDToCredits(amount float64) int64 {
	return USDToCredits(amount)
}

// RechargeRate returns how many units of paid currency buy one USD-equivalent
// credit bundle. Display FX and recharge pricing deliberately remain separate.
func (c BillingCurrencyConfig) RechargeRate(currency string) float64 {
	code := strings.ToUpper(strings.TrimSpace(currency))
	if code == "" {
		code = BillingDisplayUSD
	}
	if rate := c.RechargeRates[code]; rate > 0 && !math.IsNaN(rate) && !math.IsInf(rate, 0) {
		return rate
	}
	if code == BillingDisplayCNY {
		if c.USDToCNY > 0 {
			return c.USDToCNY
		}
		return defaultDisplayUSDToCNY
	}
	return 1
}

func normalizeBillingCurrencyConfig(cfg BillingCurrencyConfig) (BillingCurrencyConfig, error) {
	if cfg.CreditsPerUSD == 0 {
		cfg.CreditsPerUSD = CreditsPerUSD
	}
	if cfg.CreditsPerUSD != CreditsPerUSD {
		return BillingCurrencyConfig{}, fmt.Errorf(
			"%w: credits_per_usd is immutable and must be %d",
			ErrInvalidBillingCurrencyConfig,
			CreditsPerUSD,
		)
	}

	engine := strings.ToLower(strings.TrimSpace(cfg.Engine))
	if engine == "" {
		engine = BillingEngineLegacy
	}
	switch engine {
	case BillingEngineLegacy, BillingEngineShadow, BillingEngineCredits:
	default:
		return BillingCurrencyConfig{}, fmt.Errorf("%w: unsupported billing engine %q", ErrInvalidBillingCurrencyConfig, cfg.Engine)
	}
	cfg.Engine = engine

	display := strings.ToUpper(strings.TrimSpace(cfg.DisplayCurrency))
	if display == "" {
		display = BillingDisplayUSD
	}
	if display != BillingDisplayUSD && display != BillingDisplayCNY {
		return BillingCurrencyConfig{}, fmt.Errorf("%w: display currency must be USD or CNY", ErrInvalidBillingCurrencyConfig)
	}
	cfg.DisplayCurrency = display

	if cfg.USDToCNY == 0 {
		cfg.USDToCNY = defaultDisplayUSDToCNY
	}
	if math.IsNaN(cfg.USDToCNY) || math.IsInf(cfg.USDToCNY, 0) || cfg.USDToCNY <= 0 {
		return BillingCurrencyConfig{}, fmt.Errorf("%w: usd_to_cny must be a positive number", ErrInvalidBillingCurrencyConfig)
	}

	rates := make(map[string]float64, 2)
	for currency, rate := range cfg.RechargeRates {
		code := strings.ToUpper(strings.TrimSpace(currency))
		if code == "" {
			continue
		}
		if math.IsNaN(rate) || math.IsInf(rate, 0) || rate <= 0 {
			return BillingCurrencyConfig{}, fmt.Errorf("%w: recharge rate for %s must be positive", ErrInvalidBillingCurrencyConfig, code)
		}
		rates[code] = rate
	}
	if rates[BillingDisplayCNY] <= 0 {
		rates[BillingDisplayCNY] = cfg.USDToCNY
	}
	if rates[BillingDisplayUSD] <= 0 {
		rates[BillingDisplayUSD] = 1
	}
	cfg.RechargeRates = rates

	if cfg.Version <= 0 {
		cfg.Version = 1
	}
	if cfg.UpdatedAt.IsZero() {
		cfg.UpdatedAt = time.Now().UTC()
	} else {
		cfg.UpdatedAt = cfg.UpdatedAt.UTC()
	}
	return cfg, nil
}

func parseBillingCurrencyConfig(raw string) (BillingCurrencyConfig, error) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return BillingCurrencyConfig{}, errBillingCurrencySettingMiss
	}
	var cfg BillingCurrencyConfig
	if err := json.Unmarshal([]byte(raw), &cfg); err != nil {
		return BillingCurrencyConfig{}, fmt.Errorf("decode billing currency config: %w", err)
	}
	return normalizeBillingCurrencyConfig(cfg)
}

func publishBillingCurrencyConfig(cfg BillingCurrencyConfig) BillingCurrencyConfig {
	normalized, err := normalizeBillingCurrencyConfig(cfg)
	if err != nil {
		return cfg
	}
	billingCurrencyRuntime.Store(&cachedBillingCurrencyRuntime{
		config:    normalized,
		expiresAt: time.Now().Add(defaultBillingCurrencyCacheTTL),
	})
	return normalized
}

func setBillingCurrencyConfigLoader(loader func(context.Context) BillingCurrencyConfig) {
	if loader == nil {
		return
	}
	billingCurrencyLoader.Store(loader)
}

func refreshBillingCurrencyConfigAsync(ctx context.Context) {
	if !billingCurrencyRefreshActive.CompareAndSwap(false, true) {
		return
	}
	loader, _ := billingCurrencyLoader.Load().(func(context.Context) BillingCurrencyConfig)
	if loader == nil {
		billingCurrencyRefreshActive.Store(false)
		return
	}
	go func() {
		defer billingCurrencyRefreshActive.Store(false)
		refreshCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 5*time.Second)
		defer cancel()
		_ = loader(refreshCtx)
	}()
}

// GetBillingCurrencySettings returns the last validated process snapshot. It
// performs no synchronous DB access and is safe to call on the billing hot path.
func GetBillingCurrencySettings(ctx context.Context) BillingCurrencyConfig {
	if cached := billingCurrencyRuntime.Load(); cached != nil {
		if time.Now().After(cached.expiresAt) {
			refreshBillingCurrencyConfigAsync(ctx)
		}
		return cached.config
	}
	return publishBillingCurrencyConfig(DefaultBillingCurrencyConfig())
}

func (s *SettingService) GetBillingCurrencyConfig(ctx context.Context) BillingCurrencyConfig {
	if s == nil || s.settingRepo == nil {
		return GetBillingCurrencySettings(ctx)
	}
	now := time.Now()
	if cached := s.billingCurrencyCache.Load(); cached != nil && now.Before(cached.expiresAt) {
		return publishBillingCurrencyConfig(cached.config)
	}

	raw, err := s.settingRepo.GetValue(ctx, SettingKeyBillingCurrencyConfig)
	if err == nil {
		parsed, parseErr := parseBillingCurrencyConfig(raw)
		if parseErr != nil {
			return GetBillingCurrencySettings(ctx)
		}
		cfg := publishBillingCurrencyConfig(parsed)
		s.billingCurrencyCache.Store(&cachedBillingCurrencyConfig{
			config:    cfg,
			expiresAt: now.Add(defaultBillingCurrencyCacheTTL),
		})
		return cfg
	}

	// A missing setting is a normal installation state. Transient database
	// failures keep using the last valid in-process snapshot without caching the
	// failure as if it were a configured default.
	if errors.Is(err, ErrSettingNotFound) || errors.Is(err, errBillingCurrencySettingMiss) {
		cfg := publishBillingCurrencyConfig(DefaultBillingCurrencyConfig())
		s.billingCurrencyCache.Store(&cachedBillingCurrencyConfig{
			config:    cfg,
			expiresAt: now.Add(defaultBillingCurrencyCacheTTL),
		})
		return cfg
	}
	return GetBillingCurrencySettings(ctx)
}

func (s *SettingService) UpdateBillingCurrencyConfig(ctx context.Context, cfg BillingCurrencyConfig) (BillingCurrencyConfig, error) {
	if s == nil || s.settingRepo == nil {
		return BillingCurrencyConfig{}, ErrSettingNotFound
	}
	current := s.GetBillingCurrencyConfig(ctx)
	if cfg.Version > 0 && cfg.Version != current.Version {
		return BillingCurrencyConfig{}, infraerrors.Conflict(
			"BILLING_CURRENCY_VERSION_CONFLICT",
			"billing currency settings changed; reload before saving",
		)
	}
	cfg.Version = current.Version + 1
	cfg.UpdatedAt = time.Now().UTC()
	normalized, err := normalizeBillingCurrencyConfig(cfg)
	if err != nil {
		return BillingCurrencyConfig{}, infraerrors.BadRequest("INVALID_BILLING_CURRENCY_CONFIG", err.Error())
	}
	payload, err := json.Marshal(normalized)
	if err != nil {
		return BillingCurrencyConfig{}, fmt.Errorf("marshal billing currency config: %w", err)
	}
	if err := s.settingRepo.Set(ctx, SettingKeyBillingCurrencyConfig, string(payload)); err != nil {
		return BillingCurrencyConfig{}, fmt.Errorf("save billing currency config: %w", err)
	}
	normalized = publishBillingCurrencyConfig(normalized)
	s.billingCurrencyCache.Store(&cachedBillingCurrencyConfig{
		config:    normalized,
		expiresAt: time.Now().Add(defaultBillingCurrencyCacheTTL),
	})
	// 必须失效首屏 HTML 缓存：index.html 会把 billing_* 一起注入到
	// window.__APP_CONFIG__，若缓存不失效，改完币种后：
	//   · /api/v1/settings/public 返回新值（动态读）
	//   · 但 index.html 注入的仍是旧值（缓存），且带 ETag
	// 浏览器 revalidate 命中 304 后会继续用旧 HTML，
	// 表现为"配置已改但界面仍显示旧币种符号"。
	if s.onUpdate != nil {
		s.onUpdate()
	}
	return normalized, nil
}

// PaidToCredits converts a completed payment into the same fixed credit ledger.
// Recharge pricing is captured separately from display FX, so promotions or
// payment-channel fees never rewrite the value of existing balances.
func (c BillingCurrencyConfig) PaidToCredits(amount float64, currency string) (int64, float64, error) {
	if math.IsNaN(amount) || math.IsInf(amount, 0) || amount < 0 {
		return 0, 0, fmt.Errorf("%w: paid amount must be nonnegative", ErrInvalidBillingCurrencyConfig)
	}
	rate := c.RechargeRate(currency)
	usdEquivalent := decimal.NewFromFloat(amount).Div(decimal.NewFromFloat(rate))
	credits := usdEquivalent.Mul(decimal.NewFromInt(c.CreditsPerUSD)).Round(0).IntPart()
	return credits, rate, nil
}

// StartBillingCurrencyRefresh keeps every process converged to the database
// setting without putting a database query on the request billing path.
func (s *SettingService) StartBillingCurrencyRefresh(ctx context.Context) {
	if s == nil || s.settingRepo == nil {
		return
	}
	go func() {
		ticker := time.NewTicker(defaultBillingCurrencyCacheTTL)
		defer ticker.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				_ = s.GetBillingCurrencyConfig(context.Background())
			}
		}
	}()
}

// PublicBillingCurrencyConfig removes administrator-only recharge pricing
// before a value is included in an unauthenticated settings response.
func PublicBillingCurrencyConfig(cfg BillingCurrencyConfig) BillingCurrencyConfig {
	cfg.RechargeRates = nil
	return cfg
}

// USDToDisplayAmount converts an existing USD-equivalent value to the selected
// display currency. It is intentionally separate from the fixed ledger scale.
func USDToDisplayAmount(amountUSD float64, cfg BillingCurrencyConfig) float64 {
	if math.IsNaN(amountUSD) || math.IsInf(amountUSD, 0) {
		return 0
	}
	value := decimal.NewFromFloat(amountUSD)
	if strings.EqualFold(cfg.DisplayCurrency, BillingDisplayCNY) {
		rate := cfg.USDToCNY
		if rate <= 0 {
			rate = defaultDisplayUSDToCNY
		}
		value = value.Mul(decimal.NewFromFloat(rate))
	}
	return value.InexactFloat64()
}

// USDToCredits converts a USD-equivalent amount to integer credits using
// half-away-from-zero rounding, matching PostgreSQL NUMERIC rounding.
func USDToCredits(amount float64) int64 {
	if amount == 0 || math.IsNaN(amount) || math.IsInf(amount, 0) {
		return 0
	}
	return decimal.NewFromFloat(amount).
		Mul(decimal.NewFromInt(CreditsPerUSD)).
		Round(0).
		IntPart()
}

// CreditsToUSD is used only at compatibility boundaries such as cache updates
// and legacy float columns. Accounting reads should use integer credits.
func CreditsToUSD(credits int64) float64 {
	return decimal.NewFromInt(credits).Div(decimal.NewFromInt(CreditsPerUSD)).InexactFloat64()
}

// CreditsToDisplayAmount converts credits to the configured display currency.
// It never changes the ledger; changing display currency only changes this UI
// projection.
func CreditsToDisplayAmount(credits int64, cfg BillingCurrencyConfig) float64 {
	amount := decimal.NewFromInt(credits).Div(decimal.NewFromInt(CreditsPerUSD))
	if strings.EqualFold(cfg.DisplayCurrency, BillingDisplayCNY) {
		rate := cfg.USDToCNY
		if rate <= 0 {
			rate = defaultDisplayUSDToCNY
		}
		amount = amount.Mul(decimal.NewFromFloat(rate))
	}
	return amount.InexactFloat64()
}

// FormatBillingDisplayAmount formats a USD-equivalent amount for server-rendered
// emails and notifications using the active display currency.
func FormatBillingDisplayAmount(amountUSD float64) string {
	cfg := GetBillingCurrencySettings(context.Background())
	symbol := "$"
	if strings.EqualFold(cfg.DisplayCurrency, BillingDisplayCNY) {
		symbol = "¥"
	}
	return fmt.Sprintf("%s%.2f", symbol, USDToDisplayAmount(amountUSD, cfg))
}
