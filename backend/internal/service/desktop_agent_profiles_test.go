//go:build unit

package service

import (
	"context"
	"encoding/json"
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
	require.Len(t, profiles, 7)
	for _, profile := range profiles {
		require.Equal(t, "official_openai", profile.Source)
		require.Contains(t, profile.SourceURL, "https://developers.openai.com/api/docs/models/")
		require.Equal(t, 1050000, profile.OfficialContextWindow)
		require.Equal(t, 922000, profile.MaxInputTokens)
		require.Equal(t, 922000, profile.Model.ContextWindow)
		require.Equal(t, 128000, profile.Model.MaxOutputTokens)
		require.NotContains(t, profile.Model.ReasoningLevels, "none")
		require.False(t, *profile.Model.SupportsNativeWebSearch)
		require.NoError(t, validateDesktopAgentModel(&profile.Model))
	}
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
