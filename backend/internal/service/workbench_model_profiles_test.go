package service

import (
	"context"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestWorkbenchPublicModelAliasUsesOnlyExplicitOfficialProfile(t *testing.T) {
	profiles := desktopAgentBuiltinProfiles()
	alias := "公司办公助理"
	require.Nil(t, workbenchModelProfile(alias, nil, profiles), "unknown public IDs must not guess a supplier family")
	bindings := map[string]DesktopAgentModelProfileBinding{alias: {ProfileID: "gpt-6.1-sol"}}
	require.True(t, validWorkbenchModelProfiles(bindings, profiles))
	profile := workbenchModelProfile(alias, bindings, profiles)
	require.NotNil(t, profile)
	require.Equal(t, "gpt-6.1-sol", profile.ID)
	require.EqualValues(t, 1050000, *profile.OfficialContextWindow)
	require.EqualValues(t, 128000, *profile.OfficialMaxOutputTokens)
	for _, invalid := range []map[string]DesktopAgentModelProfileBinding{
		{alias: {ProfileID: "not-a-profile"}},
		{alias: {ProfileID: "gpt-6.1-sol", Override: true}},
		{"bad\nmodel": {ProfileID: "gpt-6.1-sol"}},
		{"gpt-6.1-sol": {ProfileID: "gpt-6-luna"}},
		{alias: {ProfileID: ""}},
	} {
		require.False(t, validWorkbenchModelProfiles(invalid, profiles))
	}
	require.Equal(t, "gpt-6.1-sol", workbenchModelProfile("gpt-6.1-sol", nil, profiles).ID)
	require.Nil(t, workbenchModelProfile("gpt-6.1-sol-provider-suffix", nil, profiles), "public IDs use exact catalog declarations, never a prefix guess")
}

func TestWorkbenchModelBindingsPersistSeparatelyFromDesktopSettings(t *testing.T) {
	ctx := context.Background()
	repo := &workbenchSettingsStub{values: map[string]string{SettingKeyDesktopAgentProfileCatalog: "unchanged-catalog", "desktop_agent_config_v1": "unchanged-desktop"}}
	settings := NewSettingService(repo, nil)
	svc := NewWorkbenchAdminService(nil, settings, nil)
	cfg := WorkbenchConfig{ModelProfiles: map[string]DesktopAgentModelProfileBinding{"my-office-model": {ProfileID: "gpt-6.1-sol"}}}
	_, err := svc.UpdateConfig(ctx, cfg)
	require.NoError(t, err)
	loaded, err := settings.GetWorkbenchConfig(ctx)
	require.NoError(t, err)
	require.Equal(t, cfg.ModelProfiles, loaded.ModelProfiles)
	require.Equal(t, "unchanged-catalog", repo.values[SettingKeyDesktopAgentProfileCatalog])
	require.Equal(t, "unchanged-desktop", repo.values["desktop_agent_config_v1"])
	require.Contains(t, repo.written, "workbench_model_profiles_v1")
	require.NotContains(t, repo.written, SettingKeyDesktopAgentProfileCatalog)
	bad := *loaded
	bad.ModelProfiles = map[string]DesktopAgentModelProfileBinding{"gpt-6.1-sol": {ProfileID: "gpt-6-luna"}}
	_, err = svc.UpdateConfig(ctx, bad)
	require.ErrorIs(t, err, ErrWorkbenchInvalid)
	loadedAgain, err := settings.GetWorkbenchConfig(ctx)
	require.NoError(t, err)
	require.Equal(t, cfg.ModelProfiles, loadedAgain.ModelProfiles, "an invalid draft must not overwrite the last valid mapping")
}
