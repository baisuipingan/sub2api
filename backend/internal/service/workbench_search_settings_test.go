package service

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"io"
	"net/http"
	"strings"
	"testing"

	"github.com/Wei-Shaw/sub2api/internal/config"
	"github.com/Wei-Shaw/sub2api/internal/pkg/websearch"
	"github.com/stretchr/testify/require"
)

type workbenchSearchSettingsRepo struct {
	workbenchSettingsStub
	writes []string
}

func (r *workbenchSearchSettingsRepo) Set(_ context.Context, key, value string) error {
	r.writes = append(r.writes, key)
	r.values[key] = value
	return nil
}

func (r *workbenchSearchSettingsRepo) Delete(_ context.Context, key string) error {
	r.writes = append(r.writes, key)
	delete(r.values, key)
	return nil
}

// Use a reversible stub to exercise encryption wiring without using deploy keys.
type workbenchSearchTestEncryptor struct{}

func (workbenchSearchTestEncryptor) Encrypt(plaintext string) (string, error) {
	return base64.StdEncoding.EncodeToString([]byte(plaintext)), nil
}

func (workbenchSearchTestEncryptor) Decrypt(ciphertext string) (string, error) {
	raw, err := base64.StdEncoding.DecodeString(ciphertext)
	return string(raw), err
}

func workbenchSearchFixture(fixedEncryptionKey bool) (*WorkbenchAdminService, *workbenchSearchSettingsRepo) {
	const legacy = `{"enabled":true,"providers":[{"type":"brave","api_key":"gateway-only"}]}`
	repo := &workbenchSearchSettingsRepo{workbenchSettingsStub: workbenchSettingsStub{values: map[string]string{
		SettingKeyWebSearchEmulationConfig: legacy,
		"workbench_search_enabled":         "true",
	}}}
	settings := NewSettingService(repo, &config.Config{Totp: config.TotpConfig{EncryptionKeyConfigured: fixedEncryptionKey}})
	settings.workbenchSearchEncryptor = workbenchSearchTestEncryptor{}
	return NewWorkbenchAdminService(nil, settings, nil), repo
}

func TestWorkbenchSearchConfigKeepsSeparateEncryptedWriteOnlyCredentials(t *testing.T) {
	ctx := context.Background()
	svc, repo := workbenchSearchFixture(true)
	legacy := repo.values[SettingKeyWebSearchEmulationConfig]
	config, err := svc.GetConfig(ctx)
	require.NoError(t, err)
	require.True(t, config.SearchEnabled)
	require.Equal(t, workbenchSearchUnconfigured, config.SearchProviderStatus)

	_, err = svc.UpdateSearchConfig(ctx, websearch.ProviderTypeBrave, "", nil)
	require.ErrorIs(t, err, ErrWorkbenchSearchKeyRequired)
	updated, err := svc.UpdateSearchConfig(ctx, websearch.ProviderTypeBrave, "workbench-only-secret", nil)
	require.NoError(t, err)
	require.Equal(t, WorkbenchSearchConfig{Provider: websearch.ProviderTypeBrave, APIKeyConfigured: true}, updated)
	masked, err := svc.GetSearchConfig(ctx)
	require.NoError(t, err)
	raw, err := json.Marshal(masked)
	require.NoError(t, err)
	require.JSONEq(t, `{"provider":"brave","api_key_configured":true}`, string(raw))
	require.NotContains(t, repo.values[workbenchSearchSettingKey], "workbench-only-secret")

	config, err = svc.GetConfig(ctx)
	require.NoError(t, err)
	require.Equal(t, workbenchSearchConfigured, config.SearchProviderStatus)
	_, err = svc.UpdateSearchConfig(ctx, websearch.ProviderTypeBrave, "", nil)
	require.NoError(t, err)
	require.Len(t, repo.writes, 1, "keeping a key cannot overwrite a concurrent rotation")
	_, err = svc.UpdateSearchConfig(ctx, websearch.ProviderTypeTavily, "", nil)
	require.ErrorIs(t, err, ErrWorkbenchSearchKeyRequired)

	// A DB field accidentally paired with another provider's ciphertext cannot
	// send the original credential to that provider.
	valid := repo.values[workbenchSearchSettingKey]
	repo.values[workbenchSearchSettingKey] = strings.Replace(valid, `"brave"`, `"tavily"`, 1)
	config, err = svc.GetConfig(ctx)
	require.NoError(t, err)
	require.Equal(t, workbenchSearchUnavailable, config.SearchProviderStatus)
	_, err = svc.UpdateSearchConfig(ctx, "", "", nil)
	require.NoError(t, err)
	require.NotContains(t, repo.values, workbenchSearchSettingKey)
	require.Equal(t, legacy, repo.values[SettingKeyWebSearchEmulationConfig])
	require.Equal(t, []string{workbenchSearchSettingKey, workbenchSearchSettingKey}, repo.writes)
}

func TestWorkbenchSearchConfigRejectsEphemeralEncryption(t *testing.T) {
	svc, repo := workbenchSearchFixture(false)
	_, err := svc.UpdateSearchConfig(context.Background(), websearch.ProviderTypeDeepSeek, "deepseek-secret", nil)
	require.ErrorIs(t, err, ErrWorkbenchSearchEncryption)
	require.Empty(t, repo.writes)
}

func TestWorkbenchSearchPriceCanChangeWithoutReplacingTheKey(t *testing.T) {
	ctx := context.Background()
	svc, repo := workbenchSearchFixture(true)
	price := 1.5
	updated, err := svc.UpdateSearchConfig(ctx, websearch.ProviderTypeDeepSeek, "test-key", &price)
	require.NoError(t, err)
	require.Equal(t, &price, updated.PricePer1000USD)
	credential := repo.values[workbenchSearchSettingKey]
	updated, err = svc.UpdateSearchConfig(ctx, websearch.ProviderTypeDeepSeek, "", nil)
	require.NoError(t, err)
	require.Equal(t, &price, updated.PricePer1000USD)
	zero := 0.0
	updated, err = svc.UpdateSearchConfig(ctx, websearch.ProviderTypeDeepSeek, "", &zero)
	require.NoError(t, err)
	require.Equal(t, &zero, updated.PricePer1000USD)
	require.Equal(t, credential, repo.values[workbenchSearchSettingKey])
	_, err = svc.UpdateSearchConfig(ctx, websearch.ProviderTypeBrave, "brave-key", &price)
	require.ErrorIs(t, err, ErrWorkbenchInvalid)
}

type workbenchSearchMockTransport func(*http.Request) (*http.Response, error)

func (f workbenchSearchMockTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	return f(req)
}

func TestWorkbenchSearchUsesSelectedAdapterAndSanitizesItsResponse(t *testing.T) {
	ctx := context.Background()
	for _, tc := range []struct {
		provider, host, path, method, response string
	}{
		{websearch.ProviderTypeBrave, "api.search.brave.com", "/res/v1/web/search", http.MethodGet, `{"web":{"results":[{"url":"https://example.com/source","title":"Result"}]}}`},
		{websearch.ProviderTypeTavily, "api.tavily.com", "/search", http.MethodPost, `{"results":[{"url":"https://example.com/source","title":"Result"}]}`},
		{websearch.ProviderTypeDeepSeek, "api.deepseek.com", "/v1/tools/search", http.MethodPost, `{"results":[{"url":"https://example.com/source","title":"Result"},{"url":"javascript:alert(1)","title":"Invalid"}]}`},
	} {
		t.Run(tc.provider, func(t *testing.T) {
			svc, _ := workbenchSearchFixture(true)
			_, err := svc.UpdateSearchConfig(ctx, tc.provider, "workbench-only-secret", nil)
			require.NoError(t, err)
			client := &http.Client{Transport: workbenchSearchMockTransport(func(req *http.Request) (*http.Response, error) {
				require.Equal(t, tc.host, req.URL.Host)
				require.Equal(t, tc.path, req.URL.Path)
				require.Equal(t, "https", req.URL.Scheme)
				require.Equal(t, tc.method, req.Method)
				switch tc.provider {
				case websearch.ProviderTypeBrave:
					require.Equal(t, "workbench-only-secret", req.Header.Get("X-Subscription-Token"))
				case websearch.ProviderTypeTavily:
					require.Equal(t, "Bearer workbench-only-secret", req.Header.Get("Authorization"))
					payload, err := io.ReadAll(req.Body)
					require.NoError(t, err)
					require.JSONEq(t, `{"query":"中文查询","max_results":3,"search_depth":"basic"}`, string(payload))
				case websearch.ProviderTypeDeepSeek:
					require.Equal(t, "Bearer workbench-only-secret", req.Header.Get("Authorization"))
				}
				return &http.Response{StatusCode: http.StatusOK, Header: make(http.Header), Body: io.NopCloser(strings.NewReader(tc.response))}, nil
			})}
			result, err := svc.settings.searchWorkbench(ctx, "中文查询", 3, client)
			require.NoError(t, err)
			require.Equal(t, tc.provider, result.Provider)
			require.Equal(t, "中文查询", result.Query)
			require.Equal(t, []websearch.SearchResult{{URL: "https://example.com/source", Title: "Result"}}, result.Results)
		})
	}
}

func TestWorkbenchSearchReplaysExistingToolResultWithNoConfiguredProvider(t *testing.T) {
	admin, _ := workbenchSearchFixture(true)
	svc, repo := officeBridgeFixture(nil)
	svc.settings = admin.settings
	repo.lease.Run.SearchEnabled = true
	const id = "existing-search"
	input := json.RawMessage(`{"max_results":8,"query":" search query ","tool":"web_search"}`)
	output := json.RawMessage(`{"provider":"brave","query":" search query ","results":[]}`)
	repo.ops[id] = OfficeOperation{ID: id, Kind: OfficeOperationTool, InputHash: officeDigest(input), Input: input,
		State: OfficeOperationCompleted, Result: &OfficeOperationResult{Output: output}}
	got, err := svc.Search(context.Background(), repo.lease, id, " search query ", 8)
	require.NoError(t, err)
	require.Equal(t, output, got)
}

func TestWorkbenchSearchReportsSafeProviderFailureCategories(t *testing.T) {
	const privateResponse = "private response with key and URL https://example.com/?token=private"
	ctx := context.Background()
	for _, tc := range []struct {
		provider string
		status   int
		code     string
	}{
		{websearch.ProviderTypeBrave, http.StatusUnauthorized, "SEARCH_PROVIDER_AUTH_FAILED"},
		{websearch.ProviderTypeTavily, http.StatusForbidden, "SEARCH_PROVIDER_AUTH_FAILED"},
		{websearch.ProviderTypeDeepSeek, http.StatusPaymentRequired, "SEARCH_PROVIDER_QUOTA_EXCEEDED"},
		{websearch.ProviderTypeTavily, http.StatusTooManyRequests, "SEARCH_PROVIDER_RATE_LIMITED"},
		{websearch.ProviderTypeBrave, http.StatusServiceUnavailable, "SEARCH_PROVIDER_UNAVAILABLE"},
		{websearch.ProviderTypeDeepSeek, http.StatusBadRequest, "SEARCH_FAILED"},
	} {
		t.Run(tc.provider+"/"+tc.code, func(t *testing.T) {
			svc, _ := workbenchSearchFixture(true)
			_, err := svc.UpdateSearchConfig(ctx, tc.provider, "workbench-only-secret", nil)
			require.NoError(t, err)
			client := &http.Client{Transport: workbenchSearchMockTransport(func(*http.Request) (*http.Response, error) {
				return &http.Response{StatusCode: tc.status, Body: io.NopCloser(strings.NewReader(privateResponse))}, nil
			})}
			result, err := svc.settings.searchWorkbench(ctx, "test query", 3, client)
			require.ErrorIs(t, err, ErrWorkbenchSearchUpstream)
			require.NotContains(t, err.Error(), privateResponse)
			require.Equal(t, tc.provider, result.Provider)
			receipt, encodeErr := officeNetworkReceipt("web_search", result, err, nil)
			require.NoError(t, encodeErr)
			require.Equal(t, tc.code, receipt.ErrorCode)
			require.Equal(t, "failed", receipt.ToolStatus)
			require.NotEmpty(t, receipt.ErrorMessage)
			require.NotContains(t, string(receipt.Output), "private")
			require.NotContains(t, string(receipt.Output), "workbench-only-secret")
		})
	}
}
