package service

import (
	"context"
	"encoding/json"
	"errors"
	"math"
	"net"
	"net/http"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"

	infraerrors "github.com/Wei-Shaw/sub2api/internal/pkg/errors"
	"github.com/Wei-Shaw/sub2api/internal/pkg/websearch"
)

const workbenchSearchSettingKey = "workbench_search_provider_v1"
const workbenchSearchPriceKey = "workbench_search_deepseek_price_per_1000_usd"

var (
	ErrWorkbenchSearchKeyRequired = infraerrors.BadRequest("WORKBENCH_SEARCH_KEY_REQUIRED", "请输入所选搜索供应商的 API Key")
	ErrWorkbenchSearchEncryption  = infraerrors.BadRequest("SECRET_ENCRYPTION_KEY_NOT_CONFIGURED", "服务端需要配置固定的 TOTP_ENCRYPTION_KEY 才能保存搜索供应商密钥")
	ErrWorkbenchSearchQuery       = infraerrors.BadRequest("WORKBENCH_SEARCH_QUERY_INVALID", "请填写有效的搜索关键词")
	ErrWorkbenchSearchUnavailable = infraerrors.Conflict("WORKBENCH_SEARCH_UNAVAILABLE", "请先保存可用的工作台搜索供应商配置")
	ErrWorkbenchSearchUpstream    = infraerrors.New(http.StatusBadGateway, "WORKBENCH_SEARCH_TEST_FAILED", "搜索请求失败，请检查供应商 API Key、额度和连通性")
)

// The independent workbench search client uses only the adapters' fixed HTTPS
// destinations. Redirects could move supplier credentials to a different host.
var workbenchSearchClient = &http.Client{
	Timeout: 25 * time.Second,
	CheckRedirect: func(*http.Request, []*http.Request) error {
		return http.ErrUseLastResponse
	},
}

type WorkbenchSearchConfig struct {
	Provider         string   `json:"provider"`
	APIKeyConfigured bool     `json:"api_key_configured"`
	PricePer1000USD  *float64 `json:"price_per_1000_usd,omitempty"`
}

type workbenchSearchStoredConfig struct {
	Provider        string `json:"provider"`
	EncryptedAPIKey string `json:"encrypted_api_key"`
	pricePer1000USD float64
}

type WorkbenchSearchResult struct {
	Provider            string                   `json:"provider"`
	Results             []websearch.SearchResult `json:"results"`
	Query               string                   `json:"query"`
	supplierResultCount int
}

func validWorkbenchSearchPrice(value float64) bool {
	return !math.IsNaN(value) && value >= 0 && value <= 1000000 &&
		(value == 0 || value >= 1000.0/float64(CreditsPerUSD))
}

func (c workbenchSearchStoredConfig) publicConfig() WorkbenchSearchConfig {
	result := WorkbenchSearchConfig{Provider: c.Provider, APIKeyConfigured: c.EncryptedAPIKey != ""}
	if c.Provider == websearch.ProviderTypeDeepSeek {
		result.PricePer1000USD = &c.pricePer1000USD
	}
	return result
}

func validWorkbenchSearchProvider(provider string) bool {
	switch provider {
	case websearch.ProviderTypeBrave, websearch.ProviderTypeTavily, websearch.ProviderTypeDeepSeek:
		return true
	default:
		return false
	}
}

func (s *SettingService) loadWorkbenchSearchConfig(ctx context.Context) (workbenchSearchStoredConfig, error) {
	var cfg workbenchSearchStoredConfig
	values, err := s.settingRepo.GetMultiple(ctx, []string{workbenchSearchSettingKey, workbenchSearchPriceKey})
	if err != nil {
		return cfg, err
	}
	raw := values[workbenchSearchSettingKey]
	if raw == "" {
		return cfg, nil
	}
	if len(raw) > 8<<10 || json.Unmarshal([]byte(raw), &cfg) != nil || (cfg.Provider == "") != (cfg.EncryptedAPIKey == "") || cfg.Provider != "" && !validWorkbenchSearchProvider(cfg.Provider) {
		return workbenchSearchStoredConfig{}, ErrWorkbenchSearchUnavailable
	}
	if price := values[workbenchSearchPriceKey]; cfg.Provider == websearch.ProviderTypeDeepSeek && price != "" {
		cfg.pricePer1000USD, err = strconv.ParseFloat(price, 64)
		if err != nil || !validWorkbenchSearchPrice(cfg.pricePer1000USD) {
			return workbenchSearchStoredConfig{}, ErrWorkbenchSearchUnavailable
		}
	}
	return cfg, nil
}

func (s *SettingService) workbenchSearchEncryptionReady() bool {
	return s.cfg != nil && s.cfg.Totp.EncryptionKeyConfigured && s.workbenchSearchEncryptor != nil
}

func workbenchSearchSecretPrefix(provider string) string {
	return workbenchSearchSettingKey + ":" + provider + ":"
}

func (s *SettingService) workbenchSearchAPIKey(cfg workbenchSearchStoredConfig) (string, error) {
	if cfg.Provider == "" || !s.workbenchSearchEncryptionReady() {
		return "", ErrWorkbenchSearchUnavailable
	}
	plaintext, err := s.workbenchSearchEncryptor.Decrypt(cfg.EncryptedAPIKey)
	apiKey, matched := strings.CutPrefix(plaintext, workbenchSearchSecretPrefix(cfg.Provider))
	if err != nil || !matched || apiKey == "" {
		return "", ErrWorkbenchSearchUnavailable
	}
	return apiKey, nil
}

func (s *WorkbenchAdminService) GetSearchConfig(ctx context.Context) (WorkbenchSearchConfig, error) {
	cfg, err := s.settings.loadWorkbenchSearchConfig(ctx)
	return cfg.publicConfig(), err
}

func (s *WorkbenchAdminService) UpdateSearchConfig(ctx context.Context, provider, apiKey string, price *float64) (WorkbenchSearchConfig, error) {
	if price != nil && (provider != websearch.ProviderTypeDeepSeek || !validWorkbenchSearchPrice(*price)) {
		return WorkbenchSearchConfig{}, ErrWorkbenchInvalid
	}
	apiKey = strings.TrimSpace(apiKey)
	if provider == "" {
		if apiKey != "" {
			return WorkbenchSearchConfig{}, ErrWorkbenchInvalid
		}
		return WorkbenchSearchConfig{}, s.settings.settingRepo.Delete(ctx, workbenchSearchSettingKey)
	}
	if !validWorkbenchSearchProvider(provider) || len(apiKey) > 4096 || strings.IndexFunc(apiKey, func(r rune) bool { return r < '!' || r > '~' }) >= 0 {
		return WorkbenchSearchConfig{}, ErrWorkbenchInvalid
	}
	if !s.settings.workbenchSearchEncryptionReady() {
		return WorkbenchSearchConfig{}, ErrWorkbenchSearchEncryption
	}
	if apiKey == "" {
		stored, err := s.settings.loadWorkbenchSearchConfig(ctx)
		if err != nil {
			return WorkbenchSearchConfig{}, err
		}
		if stored.Provider != provider {
			return WorkbenchSearchConfig{}, ErrWorkbenchSearchKeyRequired
		}
		if _, err := s.settings.workbenchSearchAPIKey(stored); err != nil {
			return WorkbenchSearchConfig{}, err
		}
		// Price updates leave the separately persisted credential untouched.
		if price != nil {
			if err := s.settings.settingRepo.Set(ctx, workbenchSearchPriceKey, strconv.FormatFloat(*price, 'f', -1, 64)); err != nil {
				return WorkbenchSearchConfig{}, err
			}
		}
		return s.GetSearchConfig(ctx)
	}
	encrypted, err := s.settings.workbenchSearchEncryptor.Encrypt(workbenchSearchSecretPrefix(provider) + apiKey)
	if err != nil {
		return WorkbenchSearchConfig{}, errors.New("encrypt workbench search API key")
	}
	encoded, err := json.Marshal(workbenchSearchStoredConfig{Provider: provider, EncryptedAPIKey: encrypted})
	if err != nil {
		return WorkbenchSearchConfig{}, err
	}
	if price == nil {
		err = s.settings.settingRepo.Set(ctx, workbenchSearchSettingKey, string(encoded))
	} else {
		err = s.settings.settingRepo.SetMultiple(ctx, map[string]string{
			workbenchSearchSettingKey: string(encoded),
			workbenchSearchPriceKey:   strconv.FormatFloat(*price, 'f', -1, 64),
		})
	}
	if err != nil {
		return WorkbenchSearchConfig{}, err
	}
	return s.GetSearchConfig(ctx)
}

func (s *WorkbenchAdminService) TestSearch(ctx context.Context, query string) (*WorkbenchSearchResult, error) {
	return s.settings.searchWorkbench(ctx, query, 3, workbenchSearchClient)
}

func (s *SettingService) searchWorkbench(ctx context.Context, query string, limit int, client *http.Client) (*WorkbenchSearchResult, error) {
	cfg, err := s.loadWorkbenchSearchConfig(ctx)
	if err != nil {
		return nil, ErrWorkbenchSearchUnavailable
	}
	return s.searchWorkbenchWithConfig(ctx, cfg, query, limit, client)
}

type workbenchSearchProviderStatus int

func (workbenchSearchProviderStatus) Error() string {
	return "search provider returned an error status"
}

func (s *SettingService) searchWorkbenchWithConfig(ctx context.Context, cfg workbenchSearchStoredConfig, query string, limit int, client *http.Client) (*WorkbenchSearchResult, error) {
	query = strings.TrimSpace(query)
	if query == "" || len(query) > 2000 || !utf8.ValidString(query) {
		return nil, ErrWorkbenchSearchQuery
	}
	key, err := s.workbenchSearchAPIKey(cfg)
	if err != nil {
		return nil, err
	}
	var provider websearch.Provider
	switch cfg.Provider {
	case websearch.ProviderTypeBrave:
		provider = websearch.NewBraveProvider(key, client)
	case websearch.ProviderTypeTavily:
		provider = websearch.NewTavilyBearerProvider(key, client)
	case websearch.ProviderTypeDeepSeek:
		if utf8.RuneCountInString(query) > 128 {
			return nil, ErrWorkbenchSearchQuery
		}
		provider = websearch.NewDeepSeekProvider(key, client)
	default:
		return nil, ErrWorkbenchSearchUnavailable
	}
	upstream, err := provider.Search(ctx, websearch.SearchRequest{Query: query, MaxResults: limit})
	if err != nil || upstream == nil {
		// Retain the selected provider and transport category without exposing
		// adapter errors that can contain supplier bodies or credentials.
		failure := &WorkbenchSearchResult{Provider: provider.Name()}
		var timeout net.Error
		if errors.Is(err, context.DeadlineExceeded) || errors.As(err, &timeout) && timeout.Timeout() {
			return failure, ErrWorkbenchSearchUpstream.WithCause(context.DeadlineExceeded)
		}
		if errors.Is(err, context.Canceled) {
			return failure, ErrWorkbenchSearchUpstream.WithCause(context.Canceled)
		}
		var statusErr *websearch.HTTPStatusError
		if errors.As(err, &statusErr) {
			return failure, ErrWorkbenchSearchUpstream.WithCause(workbenchSearchProviderStatus(statusErr.StatusCode))
		}
		return failure, ErrWorkbenchSearchUpstream
	}
	return &WorkbenchSearchResult{Provider: provider.Name(), Query: query,
		Results: workbenchSearchResults(upstream.Results, limit), supplierResultCount: len(upstream.Results)}, nil
}

func workbenchSearchResults(results []websearch.SearchResult, limit int) []websearch.SearchResult {
	safe := make([]websearch.SearchResult, 0, min(len(results), limit))
	seen := make(map[string]bool, cap(safe))
	for _, result := range results {
		if len(safe) == limit {
			break
		}
		url, ok := officeCitationURL(result.URL)
		if !ok || seen[url] {
			continue
		}
		seen[url] = true
		safe = append(safe, websearch.SearchResult{
			URL: url, Title: officeCitationText(result.Title, 200),
			Snippet: officeCitationText(result.Snippet, 1200), PageAge: officeCitationText(result.PageAge, 80),
		})
	}
	return safe
}
