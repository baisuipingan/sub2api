package service

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

type desktopAgentSettingsStub struct {
	SettingRepository
	values  map[string]string
	written map[string]string
	err     error
}

func (r *desktopAgentSettingsStub) GetMultiple(_ context.Context, keys []string) (map[string]string, error) {
	return r.values, r.err
}
func (r *desktopAgentSettingsStub) SetMultiple(_ context.Context, values map[string]string) error {
	r.written = values
	if r.err != nil {
		return r.err
	}
	for k, v := range values {
		r.values[k] = v
	}
	return nil
}

type desktopAgentUserStub struct {
	user  *User
	err   error
	calls int
}

func (r *desktopAgentUserStub) GetByID(context.Context, int64) (*User, error) {
	r.calls++
	return r.user, r.err
}

type desktopAgentGroupStub struct {
	group *Group
	err   error
	calls int
}

func (r *desktopAgentGroupStub) GetByID(context.Context, int64) (*Group, error) {
	r.calls++
	return r.group, r.err
}

type desktopAgentKeyStub struct {
	onEnsure func()
	key      *APIKey
	err      error
	calls    int
}

func (r *desktopAgentKeyStub) GetDesktopAgentCredential(_ context.Context, userID, groupID int64) (*APIKey, string, error) {
	r.calls++
	if r.onEnsure != nil {
		r.onEnsure()
	}
	return r.key, APIKeyPurposeDesktopAgent, r.err
}
func desktopBool(v bool) *bool { return &v }
func desktopTestProfileBindings(ids ...string) map[string]DesktopAgentModelProfileBinding {
	bindings := make(map[string]DesktopAgentModelProfileBinding, len(ids))
	for _, id := range ids {
		// These fixtures exercise bespoke group policies; their non-official
		// capabilities are intentionally and explicitly administrator-overridden.
		bindings[id] = DesktopAgentModelProfileBinding{ProfileID: "gpt-6.1-sol", Override: true}
	}
	return bindings
}
func validDesktopConfig() DesktopAgentConfig {
	return DesktopAgentConfig{Enabled: true, GroupID: 12, BaseURL: "https://gateway.example.test/v1", ModelProfiles: desktopTestProfileBindings("verified-responses-model"), Model: &DesktopAgentModel{
		ID: "verified-responses-model", DisplayName: "Verified model", ContextWindow: 100000, MaxOutputTokens: 20000, DefaultMaxOutputTokens: 10000,
		ReasoningLevels: []string{"low", "medium", "high"}, DefaultReasoning: "medium", SupportsReasoning: desktopBool(true), SupportsStreaming: desktopBool(true), RequiresMFJSToolSchema: desktopBool(false),
		Input: &DesktopAgentInput{Text: desktopBool(true), Image: desktopBool(true), Video: desktopBool(false), Audio: desktopBool(false), PDF: desktopBool(false)}, Output: &DesktopAgentOutput{Text: desktopBool(true)},
		SupportsToolCall: desktopBool(true), SupportsJSONSchemaOutput: desktopBool(false), SupportsNativeWebSearch: desktopBool(false), SupportsMidConversationSystem: desktopBool(true),
	}}
}
func desktopFixture(t *testing.T) (*DesktopAgentService, *desktopAgentSettingsStub, *desktopAgentUserStub, *desktopAgentGroupStub, *desktopAgentKeyStub) {
	t.Helper()
	cfg := validDesktopConfig()
	raw, err := json.Marshal(cfg)
	require.NoError(t, err)
	repo := &desktopAgentSettingsStub{values: map[string]string{SettingKeyDesktopAgentConfig: string(raw), "unrelated": "keep"}}
	user := &desktopAgentUserStub{user: &User{ID: 7, Status: StatusActive}}
	groups := &desktopAgentGroupStub{group: &Group{ID: 12, Platform: PlatformComposite, Status: StatusActive, SubscriptionType: SubscriptionTypeStandard}}
	id := int64(12)
	keys := &desktopAgentKeyStub{key: &APIKey{ID: 9007199254740993, UserID: 7, Key: "private-fixture-only", GroupID: &id, Status: StatusActive, KeyType: APIKeyTypeSystem, Purpose: APIKeyPurposeDesktopAgent}}
	return NewDesktopAgentService(NewSettingService(repo, nil), user, groups, keys, &desktopCatalogStub{models: []string{"verified-responses-model", "second-model"}}), repo, user, groups, keys
}
func TestDesktopAgentDefaultsDisabledAndNeverTouchesCredentials(t *testing.T) {
	svc, repo, users, groups, keys := desktopFixture(t)
	delete(repo.values, SettingKeyDesktopAgentConfig)
	result, err := svc.Bootstrap(context.Background(), 7)
	require.NoError(t, err)
	require.Equal(t, "disabled", result.ServiceStatus)
	require.Nil(t, result.Credential)
	require.Zero(t, keys.calls)
	require.Zero(t, users.calls)
	require.Zero(t, groups.calls)
	again, err := svc.Bootstrap(context.Background(), 7)
	require.NoError(t, err)
	require.Equal(t, result.ConfigRevision, again.ConfigRevision)
}
func TestDesktopAgentConfigRequiresCompleteExplicitSafePolicy(t *testing.T) {
	cases := map[string]func(*DesktopAgentConfig){
		"missing model":          func(c *DesktopAgentConfig) { c.Model = nil },
		"http":                   func(c *DesktopAgentConfig) { c.BaseURL = "http://gateway.example.test/v1" },
		"query":                  func(c *DesktopAgentConfig) { c.BaseURL += "?token=secret" },
		"userinfo":               func(c *DesktopAgentConfig) { c.BaseURL = "https://secret@gateway.example.test/v1" },
		"responses path":         func(c *DesktopAgentConfig) { c.BaseURL += "/responses" },
		"no group":               func(c *DesktopAgentConfig) { c.GroupID = 0 },
		"missing bool":           func(c *DesktopAgentConfig) { c.Model.SupportsJSONSchemaOutput = nil },
		"no tools":               func(c *DesktopAgentConfig) { c.Model.SupportsToolCall = desktopBool(false) },
		"no streaming":           func(c *DesktopAgentConfig) { c.Model.SupportsStreaming = desktopBool(false) },
		"default outside budget": func(c *DesktopAgentConfig) { c.Model.DefaultMaxOutputTokens = c.Model.MaxOutputTokens + 1 },
		"limit exceeds context":  func(c *DesktopAgentConfig) { c.Model.MaxOutputTokens = c.Model.ContextWindow + 1 },
		"reasoning duplicate":    func(c *DesktopAgentConfig) { c.Model.ReasoningLevels = []string{"low", "low"} },
		"reasoning out of order": func(c *DesktopAgentConfig) { c.Model.ReasoningLevels = []string{"high", "low"} },
		"reasoning unknown":      func(c *DesktopAgentConfig) { c.Model.ReasoningLevels = []string{"guessed"} },
		"default absent":         func(c *DesktopAgentConfig) { c.Model.DefaultReasoning = "max" },
		"unsupported reasoning":  func(c *DesktopAgentConfig) { c.Model.SupportsReasoning = desktopBool(false) },
		"model wildcard":         func(c *DesktopAgentConfig) { c.Model.ID = "model-*" },
	}
	for name, mutate := range cases {
		t.Run(name, func(t *testing.T) {
			cfg := validDesktopConfig()
			mutate(&cfg)
			require.ErrorIs(t, validateDesktopAgentConfig(cfg), ErrDesktopAgentInvalid)
		})
	}
	cfg := validDesktopConfig()
	cfg.Model.SupportsReasoning = desktopBool(false)
	cfg.Model.ReasoningLevels = []string{"none"}
	cfg.Model.DefaultReasoning = "none"
	require.NoError(t, validateDesktopAgentConfig(cfg))
	// 模型 ID 是网关标识而不是路径；保留合法命名空间 alias。
	cfg.Model.ID = "openai/verified-responses-model"
	cfg.ModelProfiles = desktopTestProfileBindings(cfg.Model.ID)
	require.NoError(t, validateDesktopAgentConfig(cfg))
}
func TestDesktopAgentStrictRequestRejectsAmbiguityAndAuthorityFields(t *testing.T) {
	valid := `{"client_version":"0.1.0","protocol_version":1,"platform":"darwin"}`
	for _, raw := range []string{`{}`, `null`, `[]`, valid + ` {}`,
		strings.Replace(valid, `"platform":"darwin"`, `"platform":"darwin","Platform":"win32"`, 1),
		strings.Replace(valid, `"platform":"darwin"`, `"platform":"darwin","installation_id":""`, 1), strings.Replace(valid, `"platform":"darwin"`, `"platform":"linux"`, 1), strings.Replace(valid, `"protocol_version":1`, `"protocol_version":5`, 1), strings.Replace(valid, `"platform":"darwin"`, `"platform":"darwin","platform":"win32"`, 1), strings.Replace(valid, `"platform":"darwin"`, `"platform":null`, 1), strings.Replace(valid, `"platform":"darwin"`, `"platform":"darwin","group_id":12`, 1), strings.Replace(valid, `"platform":"darwin"`, `"platform":"darwin","installation_id":null`, 1)} {
		_, err := ParseDesktopAgentRequest([]byte(raw))
		require.ErrorIs(t, err, ErrDesktopAgentInvalid, raw)
	}
	req, err := ParseDesktopAgentRequest([]byte(valid))
	require.NoError(t, err)
	require.Equal(t, "darwin", req.Platform)
	for _, raw := range []string{`{}`, `{"enabled":false,"enabled":true}`, `{"enabled":null}`, `{"enabled":false,"key":"no"}`} {
		_, err := ParseDesktopAgentConfig([]byte(raw))
		require.ErrorIs(t, err, ErrDesktopAgentInvalid)
	}
}
func TestDesktopAgentBootstrapFailuresNeverReturnSecretsOrCreateKeys(t *testing.T) {
	for _, name := range []string{"invalid config", "user inactive", "wrong user", "deleted user", "user missing", "group inactive", "subscription", "wrong platform", "exclusive", "restricted public", "model forbidden", "group missing", "reasoning capped", "reasoning denied"} {
		t.Run(name, func(t *testing.T) {
			svc, repo, users, groups, keys := desktopFixture(t)
			switch name {
			case "invalid config":
				repo.values[SettingKeyDesktopAgentConfig] = `{"enabled":true}`
			case "user inactive":
				users.user.Status = "disabled"
			case "wrong user":
				users.user.ID = 8
			case "deleted user":
				now := time.Now()
				users.user.DeletedAt = &now
			case "user missing":
				users.err = ErrUserNotFound
			case "group inactive":
				groups.group.Status = "disabled"
			case "subscription":
				groups.group.SubscriptionType = SubscriptionTypeSubscription
			case "wrong platform":
				groups.group.Platform = PlatformGemini
			case "exclusive":
				groups.group.IsExclusive = true
			case "restricted public":
				users.user.RestrictPublicGroups = true
			case "model forbidden":
				groups.group.ModelAllowlist = GroupModelAllowlist{Enabled: true, Models: []string{"another-model"}}
			case "group missing":
				groups.err = ErrGroupNotFound
			case "reasoning capped":
				groups.group.MaxReasoningEffort = "low"
			case "reasoning denied":
				groups.group.MaxReasoningEffort = "low"
				groups.group.MaxReasoningEffortOverLimit = "deny"
			}
			result, err := svc.Bootstrap(context.Background(), 7)
			require.NoError(t, err)
			require.NotEqual(t, "ready", result.ServiceStatus)
			raw, err := json.Marshal(result)
			require.NoError(t, err)
			for _, field := range []string{"credential", "models", "provider", "selection_policy", "private-fixture"} {
				require.NotContains(t, string(raw), field)
			}
			require.Zero(t, keys.calls)
		})
	}
}
func TestDesktopAgentBootstrapReadyAndRevokedCredentialNotReactivated(t *testing.T) {
	svc, _, _, _, keys := desktopFixture(t)
	ready, err := svc.Bootstrap(context.Background(), 7)
	require.NoError(t, err)
	require.Equal(t, "ready", ready.ServiceStatus)
	require.Equal(t, "9007199254740993", ready.Credential.ID)
	require.Nil(t, ready.Credential.ExpiresAt)
	require.Equal(t, 10000, ready.SelectionPolicy.MaxOutputTokens)
	require.Equal(t, "inherit-main", ready.SelectionPolicy.Auxiliary)
	require.Len(t, ready.Models, 1)
	for _, reason := range []string{"disabled", "expired", "quota_exhausted"} {
		t.Run(reason, func(t *testing.T) {
			svc, _, _, _, keys := desktopFixture(t)
			switch reason {
			case "disabled":
				keys.key.Status = StatusAPIKeyDisabled
			case "expired":
				past := time.Now().Add(-time.Second)
				keys.key.ExpiresAt = &past
			case "quota_exhausted":
				keys.key.Quota = 1
				keys.key.QuotaUsed = 1
			}
			result, err := svc.Bootstrap(context.Background(), 7)
			require.NoError(t, err)
			require.Equal(t, "credential_blocked", result.ServiceStatus)
			require.Equal(t, "credential_"+reason, result.ReasonCode)
			require.Nil(t, result.Credential)
			require.Equal(t, 1, keys.calls)
		})
	}
	keys.err = errors.New("secret infrastructure detail")
	result, err := svc.Bootstrap(context.Background(), 7)
	require.Error(t, err)
	require.Nil(t, result)
}

func TestDesktopAgentReasoningValidationMatchesClientRequestShape(t *testing.T) {
	for _, tc := range []struct {
		name string
		from string
		to   string
		deny bool
	}{
		{name: "omitted none ignores explicit none mapping", from: "none", to: "low"},
		{name: "omitted none ignores explicit none deny", from: "none", to: ReasoningEffortMappingDeny},
		{name: "explicit low mapping still rejected", from: "low", to: "medium", deny: true},
		{name: "explicit low deny still rejected", from: "low", to: ReasoningEffortMappingDeny, deny: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			svc, repo, _, groups, keys := desktopFixture(t)
			cfg := validDesktopConfig()
			cfg.Model.ReasoningLevels = []string{"none", "low"}
			cfg.Model.DefaultReasoning = "none"
			groups.group.ReasoningEffortMappings = []ReasoningEffortMapping{{From: tc.from, To: tc.to}}
			// 同时覆盖保存配置和既有配置的 bootstrap 校验，避免只修其中一条链路。
			raw, err := json.Marshal(cfg)
			require.NoError(t, err)
			repo.values[SettingKeyDesktopAgentConfig] = string(raw)
			_, err = svc.UpdateConfig(context.Background(), cfg)
			if tc.deny {
				require.ErrorIs(t, err, ErrDesktopAgentInvalid)
			} else {
				require.NoError(t, err)
			}
			result, err := svc.Bootstrap(context.Background(), 7)
			require.NoError(t, err)
			if tc.deny {
				require.Equal(t, "configuration_invalid", result.ReasonCode)
				require.Nil(t, result.Credential)
				require.Zero(t, keys.calls)
			} else {
				require.Equal(t, "ready", result.ServiceStatus)
				require.Equal(t, "none", result.SelectionPolicy.ReasoningLevel)
				require.NotNil(t, result.Credential)
				require.Equal(t, 1, keys.calls)
			}
		})
	}
}
func TestDesktopAgentUpdateOnlyOwnSettingAndValidatesGroup(t *testing.T) {
	svc, repo, _, groups, _ := desktopFixture(t)
	cfg := validDesktopConfig()
	_, err := svc.UpdateConfig(context.Background(), cfg)
	require.NoError(t, err)
	require.Len(t, repo.written, 1)
	require.Equal(t, "keep", repo.values["unrelated"])
	first, err := svc.Bootstrap(context.Background(), 7)
	require.NoError(t, err)
	cfg.Model.DefaultMaxOutputTokens--
	_, err = svc.UpdateConfig(context.Background(), cfg)
	require.NoError(t, err)
	second, err := svc.Bootstrap(context.Background(), 7)
	require.NoError(t, err)
	require.NotEqual(t, first.ConfigRevision, second.ConfigRevision)
	groups.group.Status = "disabled"
	_, err = svc.UpdateConfig(context.Background(), cfg)
	require.ErrorIs(t, err, ErrDesktopAgentInvalid)
	cfg.Enabled = false
	_, err = svc.UpdateConfig(context.Background(), cfg)
	require.ErrorIs(t, err, ErrDesktopAgentInvalid)
	disabled, err := svc.DisableConfig(context.Background())
	require.NoError(t, err)
	require.False(t, disabled.Enabled)
	require.Equal(t, cfg.Model.ID, disabled.DefaultModelID)
}

func TestDesktopAgentDoesNotReturnReadyAfterConfigurationChangesDuringEnsure(t *testing.T) {
	svc, repo, _, _, keys := desktopFixture(t)
	keys.onEnsure = func() {
		cfg := validDesktopConfig()
		cfg.Enabled = false
		raw, _ := json.Marshal(cfg)
		repo.values[SettingKeyDesktopAgentConfig] = string(raw)
	}
	result, err := svc.Bootstrap(context.Background(), 7)
	require.NoError(t, err)
	require.Equal(t, "configuration_changed", result.ReasonCode)
	require.Nil(t, result.Credential)
	require.Nil(t, result.Provider)
}

func TestDesktopAgentDoesNotReturnReadyAfterAuthorizationChangesDuringEnsure(t *testing.T) {
	for _, kind := range []string{"user", "group permission", "model permission"} {
		t.Run(kind, func(t *testing.T) {
			svc, _, users, groups, keys := desktopFixture(t)
			keys.onEnsure = func() {
				switch kind {
				case "user":
					users.user.Status = StatusDisabled
				case "group permission":
					users.user.RestrictPublicGroups = true
				case "model permission":
					groups.group.Status = StatusDisabled
				}
			}
			result, err := svc.Bootstrap(context.Background(), 7)
			require.NoError(t, err)
			require.NotEqual(t, "ready", result.ServiceStatus)
			require.Nil(t, result.Credential)
		})
	}
}

type desktopCatalogStub struct {
	models []string
	err    error
	calls  int
}

func (c *desktopCatalogStub) ModelsForGroup(context.Context, *Group) ([]string, error) {
	c.calls++
	return c.models, c.err
}
func (r *desktopAgentSettingsStub) CompareAndSwapDesktopAgentConfig(_ context.Context, before, after string) (bool, error) {
	if r.err != nil {
		return false, r.err
	}
	if r.values[SettingKeyDesktopAgentConfig] != before {
		return false, nil
	}
	r.values[SettingKeyDesktopAgentConfig] = after
	return true, nil
}

func TestDesktopAgentMultipleModelsProtocolAndLegacyRead(t *testing.T) {
	svc, repo, _, _, _ := desktopFixture(t)
	legacy := validDesktopConfig()
	// Existing persisted model is accepted on read, but all responses and writes are canonical.
	rawModel, err := json.Marshal(legacy.Model)
	require.NoError(t, err)
	repo.values[SettingKeyDesktopAgentConfig] = `{"enabled":true,"group_id":12,"base_url":"https://gateway.example.test/v1","model":` + string(rawModel) + `}`
	cfg, err := svc.GetConfig(context.Background())
	require.NoError(t, err)
	require.Nil(t, cfg.Model)
	require.Len(t, cfg.Models, 1)
	require.Equal(t, legacy.Model.ID, cfg.DefaultModelID)
	second := *legacy.Model
	second.ID = "second-model"
	second.DefaultMaxOutputTokens = 1234
	second.ReasoningLevels = []string{"none"}
	second.DefaultReasoning = "none"
	second.SupportsReasoning = desktopBool(false)
	cfg.Models = append(cfg.Models, second)
	cfg.DefaultModelID = second.ID
	cfg.ModelProfiles = desktopTestProfileBindings(cfg.Models[0].ID, second.ID)
	_, err = svc.UpdateConfig(context.Background(), *cfg)
	require.NoError(t, err)
	require.NotContains(t, repo.values[SettingKeyDesktopAgentConfig], `"model":`)
	for _, protocol := range []int{1, 2} {
		result, err := svc.BootstrapForProtocol(context.Background(), 7, protocol)
		require.NoError(t, err)
		require.Equal(t, protocol, result.SchemaVersion)
		require.Equal(t, "ready", result.ServiceStatus)
		require.Len(t, result.Models, protocol)
		require.Equal(t, second.ID, result.SelectionPolicy.ModelID)
		require.Equal(t, 1234, result.SelectionPolicy.MaxOutputTokens)
		if protocol == 1 {
			require.Equal(t, second.ID, result.Models[0].ID)
		} else {
			require.Equal(t, legacy.Model.DefaultMaxOutputTokens, result.Models[0].DefaultMaxOutputTokens)
		}
	}
}
func TestDesktopAgentMultipleModelsRejectInvalidMembershipAndPolicy(t *testing.T) {
	for _, kind := range []string{"duplicate", "default missing", "default unknown", "over limit", "second model forbidden", "second model missing capability", "second model reasoning", "catalog failure"} {
		t.Run(kind, func(t *testing.T) {
			svc, repo, _, groups, keys := desktopFixture(t)
			catalog, ok := svc.catalog.(*desktopCatalogStub)
			require.True(t, ok)
			cfg, err := normalizeDesktopAgentConfig(validDesktopConfig())
			require.NoError(t, err)
			second := cfg.Models[0]
			second.ID = "second-model"
			cfg.Models = append(cfg.Models, second)
			cfg.ModelProfiles[second.ID] = DesktopAgentModelProfileBinding{ProfileID: "gpt-6.1-sol", Override: true}
			switch kind {
			case "duplicate":
				cfg.Models[1].ID = cfg.Models[0].ID
			case "default missing":
				cfg.DefaultModelID = ""
			case "default unknown":
				cfg.DefaultModelID = "unknown"
			case "over limit":
				cfg.Models = make([]DesktopAgentModel, 33)
			case "second model forbidden":
				catalog.models = []string{cfg.Models[0].ID}
			case "second model missing capability":
				cfg.Models[1].SupportsToolCall = nil
			case "second model reasoning":
				cfg.Models[1].ReasoningLevels = []string{"max"}
				cfg.Models[1].DefaultReasoning = "max"
				groups.group.MaxReasoningEffort = "high"
			case "catalog failure":
				catalog.err = errors.New("catalog private detail")
			}
			_, err = svc.UpdateConfig(context.Background(), cfg)
			require.Error(t, err)
			raw, err := json.Marshal(cfg)
			require.NoError(t, err)
			repo.values[SettingKeyDesktopAgentConfig] = string(raw)
			result, err := svc.BootstrapForProtocol(context.Background(), 7, 2)
			if kind == "catalog failure" {
				require.Error(t, err)
				require.Nil(t, result)
			} else {
				require.NoError(t, err)
				require.NotEqual(t, "ready", result.ServiceStatus)
				require.Nil(t, result.Credential)
			}
			require.Zero(t, keys.calls)
		})
	}
}
func TestDesktopAgentAdminCanonicalOnlyAndMixedShapeRejected(t *testing.T) {
	cfg := validDesktopConfig()
	raw, err := json.Marshal(cfg)
	require.NoError(t, err)
	_, err = ParseDesktopAgentAdminConfig(raw)
	require.NoError(t, err)
	for _, body := range []string{`{"enabled":false}`, `{"enabled":false,"model":{},"models":[],"default_model_id":""}`, `{"enabled":false,"group_id":12,"base_url":"","models":[],"default_model_id":""}`} {
		_, err = ParseDesktopAgentAdminConfig([]byte(body))
		require.Error(t, err, body)
	}
	_, err = ParseDesktopAgentRequest([]byte(`{"client_version":"fixture","protocol_version":2,"platform":"win32"}`))
	require.NoError(t, err)
}
func TestDesktopAgentDisableRetainsSavedFieldsAndNeverCreatesCredential(t *testing.T) {
	svc, repo, _, groups, keys := desktopFixture(t)
	before := repo.values[SettingKeyDesktopAgentConfig]
	groups.group.Status = StatusDisabled
	cfg, err := svc.DisableConfig(context.Background())
	require.NoError(t, err)
	require.False(t, cfg.Enabled)
	var old, updated map[string]any
	require.NoError(t, json.Unmarshal([]byte(before), &old))
	require.NoError(t, json.Unmarshal([]byte(repo.values[SettingKeyDesktopAgentConfig]), &updated))
	old["enabled"] = false
	require.Equal(t, old, updated)
	require.Zero(t, keys.calls)
	result, err := svc.BootstrapForProtocol(context.Background(), 7, 2)
	require.NoError(t, err)
	require.Equal(t, "disabled", result.ServiceStatus)
	require.Equal(t, 2, result.SchemaVersion)
	delete(repo.values, SettingKeyDesktopAgentConfig)
	cfg, err = svc.DisableConfig(context.Background())
	require.NoError(t, err)
	require.False(t, cfg.Enabled)
	_, exists := repo.values[SettingKeyDesktopAgentConfig]
	require.False(t, exists)
}

type desktopDisableRaceSettings struct {
	desktopAgentSettingsStub
	concurrent string
	attempts   int
}

func (r *desktopDisableRaceSettings) CompareAndSwapDesktopAgentConfig(ctx context.Context, before, after string) (bool, error) {
	r.attempts++
	if r.attempts == 1 {
		r.values[SettingKeyDesktopAgentConfig] = r.concurrent
		return false, nil
	}
	return r.desktopAgentSettingsStub.CompareAndSwapDesktopAgentConfig(ctx, before, after)
}
func TestDesktopAgentDisableRetriesAgainstLatestSavedConfiguration(t *testing.T) {
	svc, repo, _, _, _ := desktopFixture(t)
	newer := validDesktopConfig()
	newer.Model.DisplayName = "Concurrent saved model"
	newer.Model.DefaultMaxOutputTokens = 1234
	raw, err := json.Marshal(newer)
	require.NoError(t, err)
	race := &desktopDisableRaceSettings{desktopAgentSettingsStub: *repo, concurrent: string(raw)}
	svc.settings.settingRepo = race
	result, err := svc.DisableConfig(context.Background())
	require.NoError(t, err)
	require.Equal(t, 2, race.attempts)
	require.False(t, result.Enabled)
	require.Equal(t, "Concurrent saved model", result.Models[0].DisplayName)
	require.Equal(t, 1234, result.Models[0].DefaultMaxOutputTokens)
}

// 目录变化和授权变化分离：运行期下架单模型不能让所有其它模型不可用。
func TestDesktopAgentProtocolThreeIsolatesUnavailableModels(t *testing.T) {
	for _, kind := range []string{"second removed", "default removed", "all removed", "reasoning changed", "allowlist changed"} {
		t.Run(kind, func(t *testing.T) {
			svc, _, _, groups, _ := desktopFixture(t)
			cfg, err := normalizeDesktopAgentConfig(validDesktopConfig())
			require.NoError(t, err)
			second := cfg.Models[0]
			second.ID, second.DisplayName = "second-model", "Second model"
			second.SupportsReasoning, second.ReasoningLevels, second.DefaultReasoning = desktopBool(false), []string{"none"}, "none"
			cfg.Models = append(cfg.Models, second)
			cfg.ModelProfiles[second.ID] = DesktopAgentModelProfileBinding{ProfileID: "gpt-6.1-sol", Override: true}
			_, err = svc.UpdateConfig(context.Background(), cfg)
			require.NoError(t, err)
			before, err := svc.BootstrapForProtocol(context.Background(), 7, 3)
			require.NoError(t, err)
			require.Equal(t, "ready", before.ServiceStatus)
			require.Len(t, before.Models, 2)
			catalog, ok := svc.catalog.(*desktopCatalogStub)
			require.True(t, ok)
			wantUnavailable, wantDefault, wantReason := "second-model", true, "model_unavailable"
			switch kind {
			case "second removed":
				catalog.models = []string{cfg.Models[0].ID}
			case "default removed":
				catalog.models = []string{"second-model"}
				wantUnavailable, wantDefault = cfg.DefaultModelID, false
			case "all removed":
				catalog.models = nil
				wantUnavailable, wantDefault = cfg.DefaultModelID, false
			case "reasoning changed":
				groups.group.MaxReasoningEffort = "low"
				wantUnavailable, wantDefault, wantReason = cfg.DefaultModelID, false, "reasoning_policy_changed"
			case "allowlist changed":
				groups.group.ModelAllowlist = GroupModelAllowlist{Enabled: true, Models: []string{"second-model"}}
				wantUnavailable, wantDefault = cfg.DefaultModelID, false
			}
			after, err := svc.BootstrapForProtocol(context.Background(), 7, 3)
			require.NoError(t, err)
			require.Equal(t, "ready", after.ServiceStatus)
			require.Equal(t, before.ConfigRevision, after.ConfigRevision)
			require.Equal(t, before.Credential.AuthorizationScope, after.Credential.AuthorizationScope)
			require.Equal(t, wantUnavailable, after.UnavailableModels[0].ModelID)
			require.Equal(t, wantReason, after.UnavailableModels[0].ReasonCode)
			require.Equal(t, wantDefault, after.SelectionPolicyV3.Default != nil)
			wantCount := 1
			if kind == "all removed" {
				wantCount = 0
				require.Len(t, after.UnavailableModels, 2)
			}
			require.Len(t, after.Models, wantCount)
			raw, err := json.Marshal(after)
			require.NoError(t, err)
			var wire map[string]any
			require.NoError(t, json.Unmarshal(raw, &wire))
			require.IsType(t, []any{}, wire["models"])
			require.IsType(t, []any{}, wire["unavailable_models"])
			policy, ok := wire["selection_policy"].(map[string]any)
			require.True(t, ok)
			require.Equal(t, "inherit-main", policy["auxiliary"])
			if !wantDefault {
				require.Nil(t, policy["default"])
			}
			// 管理保存仍严格拒绝失效项，旧客户端仍维持整体拒绝合同。
			_, err = svc.UpdateConfig(context.Background(), cfg)
			require.ErrorIs(t, err, ErrDesktopAgentInvalid)
			for _, legacy := range []int{1, 2} {
				result, err := svc.BootstrapForProtocol(context.Background(), 7, legacy)
				require.NoError(t, err)
				require.Equal(t, "not_configured", result.ServiceStatus)
				require.Nil(t, result.Credential)
			}
		})
	}
}

func TestDesktopAgentProtocolThreeAuthorizationScopeOnlyTracksCredentialRoute(t *testing.T) {
	svc, repo, _, groups, keys := desktopFixture(t)
	first, err := svc.BootstrapForProtocol(context.Background(), 7, 3)
	require.NoError(t, err)
	require.Len(t, first.Credential.AuthorizationScope, 64)
	cfg, err := normalizeDesktopAgentConfig(validDesktopConfig())
	require.NoError(t, err)
	second := cfg.Models[0]
	second.ID = "second-model"
	cfg.Models = append(cfg.Models, second)
	cfg.ModelProfiles[second.ID] = DesktopAgentModelProfileBinding{ProfileID: "gpt-6.1-sol", Override: true}
	cfg.DefaultModelID = second.ID
	cfg.Models[0].DisplayName = "Changed label"
	cfg.Models[0].DefaultMaxOutputTokens--
	_, err = svc.UpdateConfig(context.Background(), cfg)
	require.NoError(t, err)
	changed, err := svc.BootstrapForProtocol(context.Background(), 7, 3)
	require.NoError(t, err)
	require.NotEqual(t, first.ConfigRevision, changed.ConfigRevision)
	require.Equal(t, first.Credential.AuthorizationScope, changed.Credential.AuthorizationScope)
	priorScope := changed.Credential.AuthorizationScope
	for _, kind := range []string{"gateway", "key identity", "group"} {
		switch kind {
		case "gateway":
			cfg.BaseURL = "https://second.example.test/v1"
		case "key identity":
			keys.key.ID++
		case "group":
			cfg.GroupID++
			groups.group.ID = cfg.GroupID
			keys.key.GroupID = &cfg.GroupID
		}
		raw, err := json.Marshal(cfg)
		require.NoError(t, err)
		repo.values[SettingKeyDesktopAgentConfig] = string(raw)
		result, err := svc.BootstrapForProtocol(context.Background(), 7, 3)
		require.NoError(t, err)
		require.Equal(t, "ready", result.ServiceStatus)
		require.NotEqual(t, priorScope, result.Credential.AuthorizationScope, kind)
		priorScope = result.Credential.AuthorizationScope
	}
	for _, protocol := range []int{1, 2} {
		result, err := svc.BootstrapForProtocol(context.Background(), 7, protocol)
		require.NoError(t, err)
		raw, err := json.Marshal(result)
		require.NoError(t, err)
		require.NotContains(t, string(raw), "authorization_scope")
		require.NotContains(t, string(raw), "unavailable_models")
		require.NotNil(t, result.SelectionPolicy)
	}
}

func TestDesktopAgentProtocolThreeUnavailableStateAndReadFailures(t *testing.T) {
	for _, kind := range []string{"unconfigured", "disabled", "user", "group", "key", "catalog error", "group error", "second catalog error"} {
		t.Run(kind, func(t *testing.T) {
			svc, repo, users, groups, keys := desktopFixture(t)
			catalog, ok := svc.catalog.(*desktopCatalogStub)
			require.True(t, ok)
			switch kind {
			case "unconfigured":
				delete(repo.values, SettingKeyDesktopAgentConfig)
			case "disabled":
				cfg := validDesktopConfig()
				cfg.Enabled = false
				raw, err := json.Marshal(cfg)
				require.NoError(t, err)
				repo.values[SettingKeyDesktopAgentConfig] = string(raw)
			case "user":
				users.user.Status = "disabled"
			case "group":
				groups.group.Status = "disabled"
			case "key":
				keys.key.Status = "disabled"
			case "catalog error":
				catalog.err = errors.New("private catalog failure")
			case "group error":
				groups.err = errors.New("private database failure")
			case "second catalog error":
				keys.onEnsure = func() { catalog.err = errors.New("private late catalog failure") }
			}
			result, err := svc.BootstrapForProtocol(context.Background(), 7, 3)
			if strings.Contains(kind, "error") {
				require.Error(t, err)
				require.Nil(t, result)
				return
			}
			require.NoError(t, err)
			require.NotEqual(t, "ready", result.ServiceStatus)
			if kind == "unconfigured" {
				require.Equal(t, "not_configured", result.ServiceStatus)
				require.Equal(t, "configuration_missing", result.ReasonCode)
			}
			if kind == "disabled" {
				require.Equal(t, "disabled", result.ServiceStatus)
			}
			raw, err := json.Marshal(result)
			require.NoError(t, err)
			for _, absent := range []string{"provider", "models", "selection_policy", "credential"} {
				require.NotContains(t, string(raw), `"`+absent+`"`)
			}
		})
	}
}
