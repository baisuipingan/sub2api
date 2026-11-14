//go:build unit

package service

import (
	"context"
	"encoding/json"
	"net/url"
	"slices"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

func saveDesktopProfileFixture(t *testing.T, repo *desktopAgentSettingsStub, cfg DesktopAgentConfig) {
	t.Helper()
	raw, err := json.Marshal(cfg)
	require.NoError(t, err)
	repo.values[SettingKeyDesktopAgentConfig] = string(raw)
}

func TestDesktopAgentBuiltinProfilesNeverTrustSettingsOrShareMutableState(t *testing.T) {
	svc, repo, _, _, keys := desktopFixture(t)
	repo.values[SettingKeyDesktopAgentConfig] = `broken settings`
	profiles, err := svc.GetProfiles(context.Background())
	require.NoError(t, err)
	openAICount := 0
	for _, profile := range profiles {
		if profile.Source != "official_openai" {
			continue
		}
		openAICount++
		require.Contains(t, profile.SourceURL, "https://developers.openai.com/api/docs/models/")
		require.Equal(t, 1050000, profile.OfficialContextWindow)
		require.Equal(t, 922000, profile.MaxInputTokens)
		require.Equal(t, 922000, profile.Model.ContextWindow)
		require.Equal(t, 128000, profile.Model.MaxOutputTokens)
		require.NotContains(t, profile.Model.ReasoningLevels, "none")
		require.False(t, *profile.Model.SupportsNativeWebSearch)
		require.NoError(t, validateDesktopAgentModel(&profile.Model))
	}
	require.Equal(t, 7, openAICount)
	profiles[0].Model.ContextWindow = 1
	*profiles[0].Model.Input.Image = false
	profiles[0].Model.ReasoningLevels[0] = "invented"
	profiles, err = svc.GetProfiles(context.Background())
	require.NoError(t, err)
	require.Equal(t, 922000, profiles[0].Model.ContextWindow)
	require.True(t, *profiles[0].Model.Input.Image)
	require.Equal(t, "low", profiles[0].Model.ReasoningLevels[0])
	require.Zero(t, keys.calls)
}

func desktopProfileByIDFixture(t *testing.T, id string) DesktopAgentProfile {
	t.Helper()
	for _, profile := range desktopAgentBuiltinProfiles() {
		if profile.ID == id {
			return profile
		}
	}
	t.Fatalf("missing reviewed desktop profile %s", id)
	return DesktopAgentProfile{}
}

func TestDesktopAgentVendorRegistryHasUniqueExactIdentitiesAndOfficialSources(t *testing.T) {
	hosts := map[string][]string{
		"official_openai":    {"developers.openai.com"},
		"official_deepseek":  {"api-docs.deepseek.com"},
		"official_anthropic": {"platform.claude.com", "docs.claude.com"},
		"official_xai":       {"docs.x.ai"},
		"official_google":    {"ai.google.dev", "aistudio.google.com"},
	}
	aliases := map[string]string{}
	ids := map[string]bool{}
	for _, profile := range desktopAgentBuiltinProfiles() {
		t.Run(profile.ID, func(t *testing.T) {
			require.False(t, ids[profile.ID], "duplicate canonical profile")
			ids[profile.ID] = true
			u, err := url.Parse(profile.SourceURL)
			require.NoError(t, err)
			require.Equal(t, "https", u.Scheme)
			require.Contains(t, hosts[profile.Source], u.Hostname())
			require.Nil(t, u.User)
			require.Empty(t, u.Port())
			require.Regexp(t, `^\d{4}-\d{2}-\d{2}$`, profile.ReviewedAt)
			require.NotEmpty(t, profile.Revision)
			require.Equal(t, profile.ID, profile.Model.ID)
			require.Positive(t, profile.MaxInputTokens)
			require.LessOrEqual(t, profile.MaxInputTokens, profile.OfficialContextWindow)
			require.LessOrEqual(t, profile.Model.ContextWindow, profile.MaxInputTokens)
			require.NoError(t, validateDesktopAgentModel(&profile.Model))
			require.False(t, *profile.Model.SupportsNativeWebSearch)
			require.False(t, *profile.Model.Input.Audio)
			require.False(t, *profile.Model.Input.Video)
			require.Contains(t, profile.Aliases, profile.ID)
			for _, alias := range profile.Aliases {
				require.NotContains(t, aliases, alias, "alias must identify exactly one reviewed specification")
				aliases[alias] = profile.ID
				require.True(t, desktopAgentString(alias, 128))
				binding, resolved := desktopAgentProfileBinding(DesktopAgentConfig{}, alias)
				require.NotNil(t, resolved)
				require.Equal(t, profile.ID, resolved.ID)
				require.Equal(t, profile.ID, binding.ProfileID)
				for _, invented := range []string{alias + "-unreviewed", "custom/" + alias, strings.ToUpper(alias), alias + " "} {
					_, unknown := desktopAgentProfileBinding(DesktopAgentConfig{}, invented)
					require.Nil(t, unknown, "unknown suffixes/namespaces/casing cannot inherit capabilities: %s", invented)
				}
			}
		})
	}
	for _, id := range []string{"deepseek-flash", "deepseek-v4-pro", "claude-opus-5-5", "claude-haiku-4-5-20251001", "grok-4.7", "grok-build-0.1", "gemini-4-carbon", "gemini-2.5-flash"} {
		require.True(t, ids[id], id)
	}
	for _, unsupported := range []string{"deepseek-chat", "deepseek-reasoner", "claude-opus-future", "grok-imagine-image", "gemini-3.1-flash-image-preview"} {
		_, resolved := desktopAgentProfileBinding(DesktopAgentConfig{}, unsupported)
		require.Nil(t, resolved, unsupported)
	}
}

func TestDesktopAgentVendorBudgetsAndTransportCapabilitiesStayModelSpecific(t *testing.T) {
	flash := desktopProfileByIDFixture(t, "deepseek-flash")
	pro := desktopProfileByIDFixture(t, "deepseek-v4-pro")
	require.Equal(t, 1048576, flash.OfficialContextWindow)
	require.Equal(t, 393216, flash.Model.MaxOutputTokens)
	require.Equal(t, flash.OfficialContextWindow, flash.Model.ContextWindow+flash.Model.MaxOutputTokens)
	require.True(t, *flash.Model.Input.Image)
	require.True(t, *flash.Model.SupportsMidConversationSystem)
	require.False(t, *pro.Model.Input.Image, "legacy Pro must not inherit Flash vision")
	require.False(t, *pro.Model.SupportsMidConversationSystem)
	require.False(t, *flash.Model.Input.PDF, "inline PDF is not the documented DeepSeek file_id flow")
	require.Equal(t, []string{"low", "high", "max"}, flash.Model.ReasoningLevels)
	require.Contains(t, flash.OfficialReasoningLevels, "none")
	require.NotContains(t, flash.Model.ReasoningLevels, "none", "client omits none and cannot explicitly disable reasoning")

	grok := desktopProfileByIDFixture(t, "grok-4.7")
	olderGrok := desktopProfileByIDFixture(t, "grok-4.5")
	build := desktopProfileByIDFixture(t, "grok-build-0.1")
	require.Equal(t, 500000, grok.OfficialContextWindow)
	require.Equal(t, 1000000, desktopProfileByIDFixture(t, "grok-4.3").OfficialContextWindow)
	require.Contains(t, grok.Model.ReasoningLevels, "xhigh")
	require.Contains(t, olderGrok.OfficialReasoningLevels, "xhigh")
	require.NotContains(t, olderGrok.Model.ReasoningLevels, "xhigh", "official Grok 4.5 guide aliases xhigh to high")
	require.Equal(t, 256000, build.OfficialContextWindow)
	require.Equal(t, 64000, build.Model.MaxOutputTokens)
	require.Equal(t, build.OfficialContextWindow, build.Model.ContextWindow+build.Model.MaxOutputTokens)
	require.Contains(t, build.Notes, "output_budget_compatibility")
	require.False(t, *grok.Model.Input.PDF)

	for _, id := range []string{"claude-opus-5-5", "claude-opus-4-6", "gemini-4-carbon", "gemini-3.8-flash", "gemini-2.5-pro"} {
		profile := desktopProfileByIDFixture(t, id)
		require.False(t, *profile.Model.SupportsJSONSchemaOutput, id+": bridge does not preserve text.format")
		require.False(t, *profile.Model.SupportsMidConversationSystem, id+": bridge hoists system messages")
		if strings.HasPrefix(id, "gemini-") {
			require.False(t, *profile.Model.Input.PDF)
			require.Equal(t, []string{"none"}, profile.Model.ReasoningLevels)
			require.False(t, *profile.Model.SupportsReasoning)
			require.Contains(t, profile.Notes, "reasoning_auto_only")
		} else {
			require.True(t, *profile.Model.Input.PDF)
		}
	}
	adaptive := desktopProfileByIDFixture(t, "claude-opus-5-5")
	require.Equal(t, []string{"low", "medium", "high", "xhigh", "max"}, adaptive.Model.ReasoningLevels)
	legacy := desktopProfileByIDFixture(t, "claude-opus-4-6")
	require.False(t, *legacy.Model.SupportsReasoning, "legacy fixed budgets are not official adaptive effort levels")
	require.Equal(t, []string{"none"}, legacy.Model.ReasoningLevels)
	require.Contains(t, legacy.OfficialReasoningLevels, "max")
	carbon := desktopProfileByIDFixture(t, "gemini-4-carbon")
	require.Equal(t, carbon.OfficialContextWindow, carbon.Model.ContextWindow+carbon.Model.MaxOutputTokens)
	flash38 := desktopProfileByIDFixture(t, "gemini-3.8-flash")
	require.Equal(t, flash38.MaxInputTokens, flash38.Model.ContextWindow)
	require.Equal(t, flash38.OfficialContextWindow, flash38.MaxInputTokens, "separate input limit must not be reduced like a shared context")
}

func TestDesktopAgentVendorProfilesHaveNoSharedMutableStorage(t *testing.T) {
	spec := desktopAgentVendorSpecification{
		id: "fixture-profile", name: "Fixture", source: "official_deepseek", sourceURL: "https://api-docs.deepseek.com/api/list-models/",
		context: 100000, input: 80000, output: 20000, aliases: []string{"fixture-alias"},
		officialLevels: []string{"low", "high"}, levels: []string{"low", "high"}, defaultReasoning: "high", notes: []string{"fixture-note"},
	}
	first, second := desktopAgentVendorProfile(spec), desktopAgentVendorProfile(spec)
	before, err := json.Marshal(second)
	require.NoError(t, err)
	first.Aliases[1], first.OfficialReasoningLevels[0], first.Model.ReasoningLevels[0], first.Notes[0] = "changed", "changed", "changed", "changed"
	*first.Model.Input.Text, *first.Model.Output.Text, *first.Model.SupportsToolCall = false, false, false
	spec.aliases[0], spec.officialLevels[0], spec.levels[0], spec.notes[0] = "mutated", "mutated", "mutated", "mutated"
	after, err := json.Marshal(second)
	require.NoError(t, err)
	require.JSONEq(t, string(before), string(after), "helper must not retain caller-owned slices or another profile's capability pointers")

	profiles := desktopAgentBuiltinProfiles()
	pristine, err := json.Marshal(profiles)
	require.NoError(t, err)
	for i := range profiles {
		profile := &profiles[i]
		profile.Model.ContextWindow = 1
		profile.Aliases[0] = "changed"
		profile.Model.ReasoningLevels[0] = "changed"
		if len(profile.OfficialReasoningLevels) > 0 {
			profile.OfficialReasoningLevels[0] = "changed"
		}
		if len(profile.Notes) > 0 {
			profile.Notes[0] = "changed"
		}
		for _, capability := range []*bool{profile.Model.SupportsReasoning, profile.Model.SupportsStreaming, profile.Model.RequiresMFJSToolSchema,
			profile.Model.Input.Text, profile.Model.Input.Image, profile.Model.Input.Video, profile.Model.Input.Audio, profile.Model.Input.PDF,
			profile.Model.Output.Text, profile.Model.SupportsToolCall, profile.Model.SupportsJSONSchemaOutput, profile.Model.SupportsNativeWebSearch, profile.Model.SupportsMidConversationSystem} {
			*capability = !*capability
		}
	}
	fresh, err := json.Marshal(desktopAgentBuiltinProfiles())
	require.NoError(t, err)
	require.JSONEq(t, string(pristine), string(fresh), "no draft may mutate future reviewed profiles")
}

func TestDesktopAgentVendorBootstrapPreservesPublicIdentityAndLegacyWireShapes(t *testing.T) {
	for _, profileID := range []string{"deepseek-flash", "deepseek-v4-pro", "claude-opus-5-5", "claude-opus-4-6", "grok-4.7", "grok-build-0.1", "gemini-4-carbon"} {
		for _, customAlias := range []bool{false, true} {
			t.Run(profileID+map[bool]string{false: "/official", true: "/custom"}[customAlias], func(t *testing.T) {
				svc, repo, _, _, _ := desktopFixture(t)
				cfg, err := normalizeDesktopAgentConfig(validDesktopConfig())
				require.NoError(t, err)
				profile := desktopProfileByIDFixture(t, profileID)
				model := profile.Model
				model.DisplayName = "Operator display name"
				cfg.ModelProfiles = nil
				if customAlias {
					model.ID = "private-model-alias"
					cfg.ModelProfiles = map[string]DesktopAgentModelProfileBinding{model.ID: {ProfileID: profileID}}
				}
				model.DefaultMaxOutputTokens = 8192
				cfg.Models, cfg.DefaultModelID = []DesktopAgentModel{model}, model.ID
				svc.catalog.(*desktopCatalogStub).models = []string{model.ID}
				saveDesktopProfileFixture(t, repo, cfg)
				for _, protocol := range []int{1, 2, 3} {
					result, err := svc.BootstrapForProtocol(context.Background(), 7, protocol)
					require.NoError(t, err)
					require.Equal(t, "ready", result.ServiceStatus)
					require.Equal(t, "openai-responses", result.Provider.APIType)
					require.Equal(t, []DesktopAgentModel{model}, result.Models)
					raw, err := json.Marshal(result)
					require.NoError(t, err)
					for _, metadata := range []string{"model_profiles", "profile_id", "source_url", "reviewed_at", "official_context_window", "max_input_tokens", "official_reasoning_levels"} {
						require.NotContains(t, string(raw), metadata)
					}
					if protocol == 3 {
						require.Equal(t, model.ID, result.SelectionPolicyV3.Default.ModelID)
						require.Equal(t, model.DefaultReasoning, result.SelectionPolicyV3.Default.ReasoningLevel)
						require.Len(t, result.Credential.AuthorizationScope, 64)
					} else {
						require.Equal(t, model.ID, result.SelectionPolicy.ModelID)
						require.Equal(t, model.DefaultReasoning, result.SelectionPolicy.ReasoningLevel)
					}
				}
			})
		}
	}
}

func TestDesktopAgentVendorProjectionRepairsLegacyValuesWithoutCrossModelBudgetLeak(t *testing.T) {
	cfg, err := normalizeDesktopAgentConfig(validDesktopConfig())
	require.NoError(t, err)
	first, second := cfg.Models[0], cfg.Models[0]
	first.ID, first.DisplayName, first.DefaultReasoning, first.DefaultMaxOutputTokens = "deepseek-flash", "Flash alias label", "medium", 19000
	second.ID, second.DisplayName, second.DefaultReasoning, second.DefaultMaxOutputTokens = "gemini-4-carbon", "Gemini alias label", "medium", 17000
	cfg.Models, cfg.DefaultModelID, cfg.ModelProfiles = []DesktopAgentModel{first, second}, second.ID, nil
	repaired, err := applyDesktopAgentCapabilityProfiles(cfg, true)
	require.NoError(t, err)
	require.Equal(t, second.ID, repaired.DefaultModelID)
	require.Equal(t, []string{first.ID, second.ID}, []string{repaired.Models[0].ID, repaired.Models[1].ID})
	require.Equal(t, []int{19000, 17000}, []int{repaired.Models[0].DefaultMaxOutputTokens, repaired.Models[1].DefaultMaxOutputTokens})
	require.Equal(t, []string{"high", "none"}, []string{repaired.Models[0].DefaultReasoning, repaired.Models[1].DefaultReasoning})
	require.Equal(t, 393216, repaired.Models[0].MaxOutputTokens)
	require.Equal(t, 65536, repaired.Models[1].MaxOutputTokens)
	_, err = applyDesktopAgentCapabilityProfiles(cfg, false)
	require.ErrorIs(t, err, ErrDesktopAgentInvalid, "writes must not silently change an explicitly chosen unsupported effort")

	cfg.Models[0].DefaultReasoning = "high"
	cfg.Models[1].DefaultReasoning = "none"
	cfg.Models[1].DefaultMaxOutputTokens = 65537
	_, err = applyDesktopAgentCapabilityProfiles(cfg, false)
	require.ErrorIs(t, err, ErrDesktopAgentInvalid, "one model's large output allowance must not authorize another's")
	cfg.Models[1].DefaultMaxOutputTokens = 65536
	validated, err := applyDesktopAgentCapabilityProfiles(cfg, false)
	require.NoError(t, err)
	require.Equal(t, 65536, validated.Models[1].DefaultMaxOutputTokens)
	require.True(t, slices.Contains(validated.Models[0].ReasoningLevels, "max"))
}

func TestDesktopAgentLegacyOfficialCapabilitiesAreReplacedWithoutWritingSettings(t *testing.T) {
	svc, repo, _, _, _ := desktopFixture(t)
	cfg, err := normalizeDesktopAgentConfig(validDesktopConfig())
	require.NoError(t, err)
	cfg.Models[0].ID, cfg.DefaultModelID = "gpt-6.1-sol", "gpt-6.1-sol"
	cfg.Models[0].ReasoningLevels, cfg.Models[0].DefaultReasoning = []string{"none"}, "none"
	cfg.Models[0].SupportsReasoning = desktopBool(false)
	cfg.Models[0].SupportsNativeWebSearch = desktopBool(true)
	cfg.Models[0].RequiresMFJSToolSchema = desktopBool(true)
	cfg.Models[0].Input.Video = desktopBool(true)
	cfg.ModelProfiles = nil
	svc.catalog.(*desktopCatalogStub).models = []string{"gpt-6.1-sol"}
	saveDesktopProfileFixture(t, repo, cfg)
	before := repo.values[SettingKeyDesktopAgentConfig]
	resolved, err := svc.GetConfig(context.Background())
	require.NoError(t, err)
	model := resolved.Models[0]
	require.Equal(t, "gpt-6.1-sol", model.ID)
	require.Equal(t, cfg.Models[0].DisplayName, model.DisplayName)
	require.Equal(t, 922000, model.ContextWindow)
	require.Equal(t, 128000, model.MaxOutputTokens)
	require.Equal(t, cfg.Models[0].DefaultMaxOutputTokens, model.DefaultMaxOutputTokens)
	require.Equal(t, "medium", model.DefaultReasoning)
	require.False(t, *model.Input.Video)
	require.False(t, *model.RequiresMFJSToolSchema)
	require.False(t, *model.SupportsNativeWebSearch)
	require.Equal(t, DesktopAgentModelProfileBinding{ProfileID: "gpt-6.1-sol"}, resolved.ModelProfiles[model.ID])
	require.Equal(t, before, repo.values[SettingKeyDesktopAgentConfig])
	for _, protocol := range []int{1, 2, 3} {
		result, err := svc.BootstrapForProtocol(context.Background(), 7, protocol)
		require.NoError(t, err)
		require.Equal(t, "ready", result.ServiceStatus)
		require.Equal(t, 922000, result.Models[0].ContextWindow)
		raw, err := json.Marshal(result)
		require.NoError(t, err)
		for _, metadata := range []string{"model_profiles", "profile_id", "source_url", "official_context_window"} {
			require.NotContains(t, string(raw), metadata)
		}
	}
}

func TestDesktopAgentUnknownLegacyAliasCannotPublishGuessedCapabilities(t *testing.T) {
	svc, repo, _, _, keys := desktopFixture(t)
	cfg, err := normalizeDesktopAgentConfig(validDesktopConfig())
	require.NoError(t, err)
	cfg.ModelProfiles = nil
	saveDesktopProfileFixture(t, repo, cfg)
	for _, protocol := range []int{1, 2, 3} {
		result, err := svc.BootstrapForProtocol(context.Background(), 7, protocol)
		require.NoError(t, err)
		if protocol == 3 {
			require.Empty(t, result.Models)
			require.Nil(t, result.SelectionPolicyV3.Default)
			require.Equal(t, "model_unavailable", result.UnavailableModels[0].ReasonCode)
		} else {
			require.Equal(t, "not_configured", result.ServiceStatus)
			require.Nil(t, result.Credential)
		}
	}
	_, err = svc.UpdateConfig(context.Background(), cfg)
	require.ErrorIs(t, err, ErrDesktopAgentInvalid)
	// Explicitly associate an alias, retaining the public identity and valid
	// operator choices but replacing every untrusted capability declaration.
	cfg.ModelProfiles = map[string]DesktopAgentModelProfileBinding{cfg.Models[0].ID: {ProfileID: "gpt-6.1-sol"}}
	updated, err := svc.UpdateConfig(context.Background(), cfg)
	require.NoError(t, err)
	require.Equal(t, cfg.Models[0].ID, updated.Models[0].ID)
	require.Equal(t, 922000, updated.Models[0].ContextWindow)
	require.NotZero(t, keys.calls) // v3 ready+empty retains its established contract.
}

func TestDesktopAgentProfileMatchingIsExactAndOverridesAreExplicit(t *testing.T) {
	for _, id := range []string{"gpt-6.1-sol-pretend", "openai/gpt-6.1-sol", "gpt-6.1-sol ", "gpt-image-2"} {
		_, profile := desktopAgentProfileBinding(DesktopAgentConfig{}, id)
		require.Nil(t, profile, id)
	}
	_, profile := desktopAgentProfileBinding(DesktopAgentConfig{}, "gpt-5.6")
	require.NotNil(t, profile)
	require.Equal(t, "gpt-5.6-sol", profile.ID)
	_, profile = desktopAgentProfileBinding(DesktopAgentConfig{ModelProfiles: map[string]DesktopAgentModelProfileBinding{
		"gpt-6.1-sol": {ProfileID: "gpt-5.6-luna", Override: true},
	}}, "gpt-6.1-sol")
	require.Nil(t, profile) // an official public ID keeps its declared semantics
	svc, _, _, _, _ := desktopFixture(t)
	cfg, err := normalizeDesktopAgentConfig(validDesktopConfig())
	require.NoError(t, err)
	updated, err := svc.UpdateConfig(context.Background(), cfg)
	require.NoError(t, err)
	require.Equal(t, cfg.Models[0], updated.Models[0]) // explicit advanced override
	cfg.ModelProfiles[cfg.Models[0].ID] = DesktopAgentModelProfileBinding{ProfileID: "gpt-6.1-sol"}
	updated, err = svc.UpdateConfig(context.Background(), cfg)
	require.NoError(t, err)
	require.Equal(t, 922000, updated.Models[0].ContextWindow)
	require.NotEqual(t, cfg.Models[0].ContextWindow, updated.Models[0].ContextWindow)
	for _, bindings := range []map[string]DesktopAgentModelProfileBinding{
		{"other": {ProfileID: "gpt-6.1-sol"}},
		{cfg.Models[0].ID: {ProfileID: "saved_configuration"}},
		{cfg.Models[0].ID: {Override: true}},
	} {
		cfg.ModelProfiles = bindings
		_, err = svc.UpdateConfig(context.Background(), cfg)
		require.ErrorIs(t, err, ErrDesktopAgentInvalid)
	}
}

func TestDesktopAgentProfileBindingsRejectAmbiguousNestedJSON(t *testing.T) {
	cfg, err := normalizeDesktopAgentConfig(validDesktopConfig())
	require.NoError(t, err)
	raw, err := json.Marshal(cfg)
	require.NoError(t, err)
	validBinding := `{"profile_id":"gpt-6.1-sol","override":true}`
	require.Contains(t, string(raw), validBinding)
	_, err = ParseDesktopAgentAdminConfig(raw)
	require.NoError(t, err)
	for _, binding := range []string{
		`{"profile_id":"gpt-6.1-sol","override":false,"Override":true}`,
		`{"profile_id":"gpt-6.1-sol","Override":true}`,
		`{"Profile_ID":"gpt-6.1-sol","override":true}`,
		`{"profile_id":"gpt-6.1-sol","profile_id":"gpt-5.6-sol","override":true}`,
		`{"profile_id":"gpt-6.1-sol","override":null}`,
	} {
		_, err := ParseDesktopAgentAdminConfig([]byte(strings.Replace(string(raw), validBinding, binding, 1)))
		require.ErrorIs(t, err, ErrDesktopAgentInvalid, binding)
	}
}

func TestDesktopAgentDisableReturnsTheSameReviewedProjectionAsRead(t *testing.T) {
	svc, repo, _, _, _ := desktopFixture(t)
	cfg, err := normalizeDesktopAgentConfig(validDesktopConfig())
	require.NoError(t, err)
	cfg.Models[0].ID, cfg.DefaultModelID = "gpt-6.1-sol", "gpt-6.1-sol"
	cfg.ModelProfiles = nil
	saveDesktopProfileFixture(t, repo, cfg)
	disabled, err := svc.DisableConfig(context.Background())
	require.NoError(t, err)
	read, err := svc.GetConfig(context.Background())
	require.NoError(t, err)
	require.Equal(t, read, disabled)
	require.False(t, disabled.Enabled)
	require.Equal(t, 922000, disabled.Models[0].ContextWindow)
	require.Contains(t, repo.values[SettingKeyDesktopAgentConfig], `"context_window":100000`)
}
