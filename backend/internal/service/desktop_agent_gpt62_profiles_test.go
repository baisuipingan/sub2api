//go:build unit

package service

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestDesktopAgentGPT62ProfilesReachBootstrapWithOfficialLimits(t *testing.T) {
	for _, profileID := range []string{"gpt-6.2-sol", "gpt-6.2-astra"} {
		for _, publicID := range []string{profileID, "enterprise-" + profileID} {
			t.Run(publicID, func(t *testing.T) {
				svc, repo, _, _, _ := desktopFixture(t)
				cfg, err := normalizeDesktopAgentConfig(validDesktopConfig())
				require.NoError(t, err)
				cfg.Models[0].ID, cfg.DefaultModelID = publicID, publicID
				cfg.Models[0].ContextWindow = 64000
				cfg.Models[0].MaxOutputTokens = 16384
				cfg.Models[0].ReasoningLevels = []string{"high", "max"}
				cfg.Models[0].DefaultReasoning = "max"
				cfg.ModelProfiles = map[string]DesktopAgentModelProfileBinding{publicID: {ProfileID: profileID}}
				svc.catalog.(*desktopCatalogStub).models = []string{publicID}
				saveDesktopProfileFixture(t, repo, cfg)
				before := repo.values[SettingKeyDesktopAgentConfig]

				for _, protocol := range []int{1, 2, 3} {
					result, err := svc.BootstrapForProtocol(context.Background(), 7, protocol)
					require.NoError(t, err)
					require.Equal(t, "ready", result.ServiceStatus)
					require.Len(t, result.Models, 1)
					model := result.Models[0]
					require.Equal(t, publicID, model.ID)
					require.Equal(t, 1050000, model.ContextWindow)
					require.Equal(t, 128000, model.MaxOutputTokens)
					require.Equal(t, "max", model.DefaultReasoning)
					require.Equal(t, []string{"low", "medium", "high", "xhigh", "max"}, model.ReasoningLevels)
					require.True(t, *model.Input.Image)
					require.False(t, *model.Input.Audio)
					raw, err := json.Marshal(result)
					require.NoError(t, err)
					require.NotContains(t, string(raw), "official_context_window")
				}
				require.Equal(t, before, repo.values[SettingKeyDesktopAgentConfig])
			})
		}
	}
}

func TestDesktopAgentGPT62BuiltinProfilesSurviveOlderRemoteCatalog(t *testing.T) {
	svc, repo, body := newDesktopCatalogFixture(t)
	var previous []DesktopAgentProfile
	for _, profile := range desktopAgentBuiltinProfiles() {
		if profile.ID != "gpt-6.2-sol" && profile.ID != "gpt-6.2-astra" {
			previous = append(previous, profile)
		}
	}
	require.Len(t, previous, 37)
	*body = catalogDocumentFixture(t, previous)
	require.NoError(t, svc.Refresh(context.Background()))
	require.Len(t, svc.Snapshot(), 39)

	reloaded := NewDesktopAgentProfileCatalogService(nil, NewSettingService(repo, nil))
	t.Cleanup(reloaded.Stop)
	require.NoError(t, reloaded.Initialize(context.Background()))
	for _, profiles := range [][]DesktopAgentProfile{svc.Snapshot(), reloaded.Snapshot()} {
		for _, id := range []string{"gpt-6.2-sol", "gpt-6.2-astra"} {
			_, profile := desktopAgentProfileBinding(DesktopAgentConfig{}, id, profiles)
			require.NotNil(t, profile)
			require.Equal(t, desktopProfileTokenLimit(1050000), profile.OfficialContextWindow)
			require.Equal(t, desktopProfileTokenLimit(922000), profile.MaxInputTokens)
			require.Equal(t, desktopProfileTokenLimit(128000), profile.OfficialMaxOutputTokens)
			require.Equal(t, "2026-11-18.1", profile.Revision)
		}
	}
}
