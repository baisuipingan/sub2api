package service

import (
	"context"
	"errors"
	"net/http"
	"slices"
	"testing"

	"github.com/stretchr/testify/require"
)

type desktopModelAccounts struct {
	AccountRepository
	accounts     []Account
	err          error
	queriedGroup int64
}

func (r *desktopModelAccounts) ListModelAvailabilityCandidates(_ context.Context, group *int64, platforms []string, includeGrouped bool) ([]Account, error) {
	if group != nil {
		r.queriedGroup = *group
	}
	if includeGrouped {
		return nil, errors.New("catalog must remain group scoped")
	}
	return r.accounts, r.err
}
func desktopModelAccount(platform string, mapping map[string]any) Account {
	return Account{Platform: platform, Type: AccountTypeAPIKey, Status: StatusActive, Schedulable: true, Credentials: map[string]any{"model_mapping": mapping}}
}

type desktopModelRoutesFailure struct{ CompositeModelRouteRepository }

func (desktopModelRoutesFailure) ListByGroup(context.Context, int64, bool) ([]CompositeModelRoute, error) {
	return nil, errors.New("routes unavailable")
}
func TestDesktopAgentModelCatalogPublicIDsRoutingAndAllowlist(t *testing.T) {
	ctx := context.Background()
	group := &Group{ID: 12, Platform: PlatformComposite, Status: StatusActive, SubscriptionType: SubscriptionTypeStandard}
	accounts := &desktopModelAccounts{accounts: []Account{
		desktopModelAccount(PlatformOpenAI, map[string]any{"public-alpha": "private-upstream", "target": "target", "image-alias": "gpt-image-2", "ambiguous": "shared"}),
		desktopModelAccount(PlatformAnthropic, map[string]any{"claude-public": "claude-upstream", "ambiguous": "shared"}),
	}}
	routes := compositeRouteRepoStub{routes: []CompositeModelRoute{
		{ID: 1, GroupID: 12, Enabled: true, PublicModel: "route-alias", MatchType: CompositeRouteMatchExact, Endpoint: CompositeRouteEndpointResponses, TargetPlatform: PlatformOpenAI, UpstreamModel: "target"},
		{ID: 2, GroupID: 12, Enabled: true, PublicModel: "chat-only", MatchType: CompositeRouteMatchExact, Endpoint: CompositeRouteEndpointChatCompletions, TargetPlatform: PlatformOpenAI, UpstreamModel: "target"},
		{ID: 3, GroupID: 12, Enabled: true, PublicModel: "system-only", MatchType: CompositeRouteMatchExact, TargetPlatform: PlatformTypeSafe, UpstreamModel: "target"},
		{ID: 4, GroupID: 12, Enabled: false, PublicModel: "disabled-route", MatchType: CompositeRouteMatchExact, TargetPlatform: PlatformOpenAI, UpstreamModel: "target"},
		{ID: 5, GroupID: 12, Enabled: true, PublicModel: "no-account", MatchType: CompositeRouteMatchExact, TargetPlatform: PlatformGemini, UpstreamModel: "target"},
	}}
	catalog := NewDesktopAgentModelCatalog(accounts, routes)
	got, err := catalog.ModelsForGroup(ctx, group)
	require.NoError(t, err)
	require.Equal(t, []string{"claude-public", "public-alpha", "route-alias", "target"}, got)
	require.Equal(t, int64(12), accounts.queriedGroup)
	group.ModelAllowlist = GroupModelAllowlist{Enabled: true, Models: []string{"route-alias", "public-alpha"}}
	got, err = catalog.ModelsForGroup(ctx, group)
	require.NoError(t, err)
	require.Equal(t, []string{"route-alias", "public-alpha"}, got)
}

func TestDesktopAgentSharedSelectionWithNativeOnlyModels(t *testing.T) {
	ctx := context.Background()
	const desktop, gemini, claude = "gpt-6.1-sol", "carbon-office", "claude-office"
	svc, settings, _, groups, keys := desktopFixture(t)
	groups.group.ModelAllowlist = GroupModelAllowlist{Enabled: true, Models: []string{desktop, gemini, claude}}
	accounts := &desktopModelAccounts{accounts: []Account{
		desktopModelAccount(PlatformOpenAI, map[string]any{desktop: desktop}),
		desktopModelAccount(PlatformGemini, map[string]any{"gemini-4-carbon": "gemini-4-carbon"}),
		desktopModelAccount(PlatformAnthropic, map[string]any{"claude-opus-4-6": "claude-opus-4-6"}),
	}}
	routes := compositeRouteRepoStub{routes: []CompositeModelRoute{
		{ID: 1, GroupID: 12, Enabled: true, MatchType: CompositeRouteMatchExact, PublicModel: gemini, Endpoint: CompositeRouteEndpointGemini, TargetPlatform: PlatformGemini, UpstreamModel: "gemini-4-carbon"},
		{ID: 2, GroupID: 12, Enabled: true, MatchType: CompositeRouteMatchExact, PublicModel: claude, Endpoint: CompositeRouteEndpointMessages, TargetPlatform: PlatformAnthropic, UpstreamModel: "claude-opus-4-6"},
	}}
	svc.catalog = NewDesktopAgentModelCatalog(accounts, routes)
	candidates, err := svc.GetModels(ctx, 12)
	require.NoError(t, err)
	require.Equal(t, []string{gemini, claude, desktop}, candidates.Models)
	require.Equal(t, []string{gemini, claude}, candidates.WebOnly)

	cfg := DesktopAgentConfig{
		Enabled: true, GroupID: 12, BaseURL: "https://gateway.example.test/v1", DefaultModelID: gemini,
		ModelProfiles: map[string]DesktopAgentModelProfileBinding{
			gemini: {ProfileID: "gemini-4-carbon"},
			claude: {ProfileID: "claude-opus-4-6"},
		},
	}
	for _, id := range []string{gemini, desktop, claude} {
		profileID := id
		if binding, exists := cfg.ModelProfiles[id]; exists {
			profileID = binding.ProfileID
		}
		profiles := desktopAgentBuiltinProfiles()
		index := slices.IndexFunc(profiles, func(profile DesktopAgentProfile) bool { return profile.ID == profileID })
		require.NotEqual(t, -1, index, profileID)
		model := profiles[index].Model
		model.ID = id
		cfg.Models = append(cfg.Models, model)
	}
	_, err = svc.UpdateConfig(ctx, cfg)
	require.NoError(t, err)
	for _, protocol := range []int{1, 2, 3, 4} {
		bootstrap, err := svc.BootstrapForProtocol(ctx, 7, protocol)
		require.NoError(t, err)
		require.Equal(t, "ready", bootstrap.ServiceStatus)
		require.Equal(t, "openai-responses", bootstrap.Provider.APIType)
		require.Len(t, bootstrap.Models, 1)
		require.Equal(t, desktop, bootstrap.Models[0].ID)
		require.Empty(t, bootstrap.UnavailableModels)
		if protocol < 3 {
			require.Equal(t, desktop, bootstrap.SelectionPolicy.ModelID)
		} else {
			require.Equal(t, desktop, bootstrap.SelectionPolicyV3.Default.ModelID)
		}
	}
	saved, err := svc.GetConfig(ctx)
	require.NoError(t, err)
	require.Equal(t, gemini, saved.DefaultModelID)

	settings.values["workbench_chat_enabled"] = "true"
	workbench, _ := officeBridgeFixture(http.NotFoundHandler())
	workbench.settings = svc.settings
	workbench.groups = NewGroupService(&workbenchGroupStub{group: groups.group}, nil)
	workbench.catalog = NewWorkbenchChatModelCatalog(accounts, routes, nil)
	models, err := workbench.ChatModels(ctx)
	require.NoError(t, err)
	require.Equal(t, gemini, models.DefaultModel)
	require.Len(t, models.Models, 3)
	require.Equal(t, gemini, models.Models[0].ID)
	require.Equal(t, workbenchProtocolGemini, models.Models[0].Protocol)
	require.Equal(t, desktop, models.Models[1].ID)
	require.Equal(t, workbenchProtocolResponses, models.Models[1].Protocol)
	require.Equal(t, claude, models.Models[2].ID)
	require.Equal(t, workbenchProtocolAnthropic, models.Models[2].Protocol)

	cfg.Models = []DesktopAgentModel{cfg.Models[0], cfg.Models[2]}
	_, err = svc.UpdateConfig(ctx, cfg)
	require.NoError(t, err)
	keyReads := keys.calls
	for _, protocol := range []int{1, 2, 3, 4} {
		bootstrap, err := svc.BootstrapForProtocol(ctx, 7, protocol)
		require.NoError(t, err)
		require.Equal(t, "not_configured", bootstrap.ServiceStatus)
		require.Nil(t, bootstrap.Credential)
	}
	require.Equal(t, keyReads, keys.calls)
}
func TestDesktopAgentModelCatalogDoesNotInventDefaultsOrTreatPatternsAsIDs(t *testing.T) {
	group := &Group{ID: 12, Platform: PlatformOpenAI}
	repo := &desktopModelAccounts{}
	catalog := NewDesktopAgentModelCatalog(repo, nil)
	got, err := catalog.ModelsForGroup(context.Background(), group)
	require.NoError(t, err)
	require.Empty(t, got)
	require.NotNil(t, got)
	repo.accounts = []Account{desktopModelAccount(PlatformOpenAI, nil)}
	got, err = catalog.ModelsForGroup(context.Background(), group)
	require.NoError(t, err)
	require.Empty(t, got)
	repo.accounts = []Account{desktopModelAccount(PlatformOpenAI, map[string]any{"wild-*": "upstream", "gpt-image-2": "gpt-image-2", "codex-auto-test": "upstream"})}
	got, err = catalog.ModelsForGroup(context.Background(), group)
	require.NoError(t, err)
	require.Empty(t, got)
	group.ModelAllowlist = GroupModelAllowlist{Enabled: true, Models: []string{"wild-concrete", "codex-auto-test"}}
	got, err = catalog.ModelsForGroup(context.Background(), group)
	require.NoError(t, err)
	require.Equal(t, []string{"wild-concrete", "codex-auto-test"}, got)
}
func TestDesktopAgentModelCatalogReadFailuresFailClosed(t *testing.T) {
	group := &Group{ID: 12, Platform: PlatformComposite}
	for _, catalog := range []DesktopAgentModelCatalog{
		NewDesktopAgentModelCatalog(&desktopModelAccounts{err: errors.New("accounts unavailable")}, compositeRouteRepoStub{}),
		NewDesktopAgentModelCatalog(&desktopModelAccounts{}, desktopModelRoutesFailure{}),
		NewDesktopAgentModelCatalog(&desktopModelAccounts{}, nil),
	} {
		got, err := catalog.ModelsForGroup(context.Background(), group)
		require.Error(t, err)
		require.Nil(t, got)
	}
}

func TestDesktopAgentModelCatalogNeverEnumeratesRuntimeInjectedDefaults(t *testing.T) {
	group := &Group{ID: 12, Platform: PlatformComposite}
	account := desktopModelAccount(PlatformAntigravity, nil)
	catalog := NewDesktopAgentModelCatalog(&desktopModelAccounts{accounts: []Account{account}}, compositeRouteRepoStub{})
	got, err := catalog.ModelsForGroup(context.Background(), group)
	require.NoError(t, err)
	require.Empty(t, got)
}

func TestDesktopAgentModelCatalogExplicitAllowlistStillRequiresRoutableAccount(t *testing.T) {
	group := &Group{ID: 12, Platform: PlatformOpenAI,
		ModelAllowlist: GroupModelAllowlist{Enabled: true, Models: []string{"configured-public", "gpt-image-2", "pattern-*"}},
	}
	repo := &desktopModelAccounts{accounts: []Account{desktopModelAccount(PlatformOpenAI, nil)}}
	catalog := NewDesktopAgentModelCatalog(repo, nil)
	got, err := catalog.ModelsForGroup(context.Background(), group)
	require.NoError(t, err)
	require.Equal(t, []string{"configured-public"}, got)

	repo.accounts = nil
	got, err = catalog.ModelsForGroup(context.Background(), group)
	require.NoError(t, err)
	require.Empty(t, got)

	repo.accounts = []Account{desktopModelAccount(PlatformOpenAI, nil)}
	group.ModelAllowlist.Enabled = false
	got, err = catalog.ModelsForGroup(context.Background(), group)
	require.NoError(t, err)
	require.Empty(t, got)
}

func TestDesktopAgentModelCatalogUsesStoredMetadataWithoutExposingPrivateTargets(t *testing.T) {
	group := &Group{ID: 12, Platform: PlatformOpenAI}
	account := desktopModelAccount(PlatformOpenAI, map[string]any{
		"known-*": "upstream", "public-alias": "private-target",
	})
	account.SetUpstreamModelMetadataSnapshot(UpstreamModelMetadataSnapshot{Models: map[string]UpstreamModelMetadata{
		"known-concrete": {ID: "known-concrete"},
		"private-target": {ID: "private-target"},
		"unknown":        {ID: "unknown"},
		"gpt-image-2":    {ID: "gpt-image-2"},
		"pattern-*":      {ID: "pattern-*"},
	}})
	catalog := NewDesktopAgentModelCatalog(&desktopModelAccounts{accounts: []Account{account}}, nil)
	got, err := catalog.ModelsForGroup(context.Background(), group)
	require.NoError(t, err)
	require.Equal(t, []string{"known-concrete", "public-alias"}, got)
}

func TestDesktopAgentModelCatalogStoredMetadataDoesNotAssignCompositeOwnership(t *testing.T) {
	account := desktopModelAccount(PlatformOpenAI, nil)
	account.SetUpstreamModelMetadataSnapshot(UpstreamModelMetadataSnapshot{Models: map[string]UpstreamModelMetadata{
		"gpt-5.1":                {ID: "gpt-5.1"},
		"unknown-persisted-name": {ID: "unknown-persisted-name"},
	}})
	repo := &desktopModelAccounts{accounts: []Account{account}}
	catalog := NewDesktopAgentModelCatalog(repo, compositeRouteRepoStub{})
	got, err := catalog.ModelsForGroup(context.Background(), &Group{ID: 12, Platform: PlatformOpenAI})
	require.NoError(t, err)
	require.Equal(t, []string{"gpt-5.1", "unknown-persisted-name"}, got)

	got, err = catalog.ModelsForGroup(context.Background(), &Group{ID: 12, Platform: PlatformComposite})
	require.NoError(t, err)
	require.Equal(t, []string{"gpt-5.1"}, got)
}

func TestDesktopAgentModelCatalogPreservesRoutablePublicIDCasing(t *testing.T) {
	group := &Group{ID: 12, Platform: PlatformComposite,
		ModelAllowlist: GroupModelAllowlist{Enabled: true, Models: []string{"custom-id"}},
	}
	catalog := NewDesktopAgentModelCatalog(&desktopModelAccounts{accounts: []Account{
		desktopModelAccount(PlatformOpenAI, map[string]any{"Custom-ID": "private-target"}),
	}}, compositeRouteRepoStub{})
	got, err := catalog.ModelsForGroup(context.Background(), group)
	require.NoError(t, err)
	require.Equal(t, []string{"Custom-ID"}, got)
}

type desktopModelChannelReader struct {
	channel *Channel
	err     error
}

func (r desktopModelChannelReader) GetChannelForGroup(context.Context, int64) (*Channel, error) {
	return r.channel, r.err
}

func desktopModelChannel(groupID int64, platform string, mapping map[string]string) *Channel {
	return &Channel{ID: 1, Status: StatusActive, GroupIDs: []int64{groupID}, ModelMapping: map[string]map[string]string{platform: mapping}}
}

func TestDesktopAgentModelCatalogResolvesChannelAliasesBeforeAccountSupport(t *testing.T) {
	group := &Group{ID: 12, Platform: PlatformOpenAI}
	account := desktopModelAccount(PlatformOpenAI, map[string]any{"gpt-5": "gpt-5", "gpt-stale": "gpt-stale"})
	channel := desktopModelChannel(group.ID, group.Platform, map[string]string{
		"public-agent": "gpt-5", "gpt-stale": "missing", "public-image": "gpt-image-2", "pattern-*": "gpt-5",
	})
	catalog := NewDesktopAgentModelCatalog(&desktopModelAccounts{accounts: []Account{account}}, nil, desktopModelChannelReader{channel: channel})
	got, err := catalog.ModelsForGroup(context.Background(), group)
	require.NoError(t, err)
	require.Equal(t, []string{"gpt-5", "public-agent"}, got)
	group.ModelAllowlist = GroupModelAllowlist{Enabled: true, Models: []string{"public-agent"}}
	got, err = catalog.ModelsForGroup(context.Background(), group)
	require.NoError(t, err)
	require.Equal(t, []string{"public-agent"}, got)
}

func TestDesktopAgentModelCatalogUsesResolvedCompositePlatformForChannel(t *testing.T) {
	group := &Group{ID: 12, Platform: PlatformComposite}
	accounts := &desktopModelAccounts{accounts: []Account{
		desktopModelAccount(PlatformOpenAI, map[string]any{"gpt-target": "gpt-target"}),
		desktopModelAccount(PlatformGemini, map[string]any{"gemini-target": "gemini-target"}),
	}}
	channel := desktopModelChannel(group.ID, PlatformOpenAI, map[string]string{"gpt-public": "gpt-target"})
	channel.ModelMapping[PlatformGemini] = map[string]string{"gpt-public": "gemini-target"}
	routes := compositeRouteRepoStub{routes: []CompositeModelRoute{
		{ID: 1, GroupID: group.ID, Enabled: true, PublicModel: "route-agent", MatchType: CompositeRouteMatchExact, Endpoint: CompositeRouteEndpointResponses, TargetPlatform: PlatformOpenAI, UpstreamModel: "gpt-public"},
	}}
	catalog := NewDesktopAgentModelCatalog(accounts, routes, desktopModelChannelReader{channel: channel})
	got, err := catalog.ModelsForGroup(context.Background(), group)
	require.NoError(t, err)
	require.Contains(t, got, "gpt-public")
	require.Contains(t, got, "route-agent")
	accounts.accounts = accounts.accounts[1:]
	got, err = catalog.ModelsForGroup(context.Background(), group)
	require.NoError(t, err)
	require.NotContains(t, got, "gpt-public", "a Gemini channel mapping must not serve an OpenAI-resolved request")
	require.NotContains(t, got, "route-agent")
}

func TestDesktopAgentModelCatalogChannelReadFailsClosed(t *testing.T) {
	group := &Group{ID: 12, Platform: PlatformOpenAI}
	catalog := NewDesktopAgentModelCatalog(&desktopModelAccounts{accounts: []Account{desktopModelAccount(PlatformOpenAI, map[string]any{"gpt-5": "gpt-5"})}}, nil,
		desktopModelChannelReader{err: errors.New("channel unavailable")})
	got, err := catalog.ModelsForGroup(context.Background(), group)
	require.Error(t, err)
	require.Nil(t, got)
}

func TestDesktopAgentModelCatalogHonorsPrivacyWithoutChangingOwnership(t *testing.T) {
	group := &Group{ID: 12, Platform: PlatformOpenAI, RequirePrivacySet: true}
	account := desktopModelAccount(PlatformOpenAI, map[string]any{"gpt-5": "gpt-5"})
	repo := &desktopModelAccounts{accounts: []Account{account}}
	catalog := NewDesktopAgentModelCatalog(repo, nil)
	got, err := catalog.ModelsForGroup(context.Background(), group)
	require.NoError(t, err)
	require.Empty(t, got)
	repo.accounts[0].Extra = map[string]any{"privacy_mode": PrivacyModeTrainingOff}
	got, err = catalog.ModelsForGroup(context.Background(), group)
	require.NoError(t, err)
	require.Equal(t, []string{"gpt-5"}, got)

	group.Platform = PlatformComposite
	repo.accounts = []Account{
		desktopModelAccount(PlatformOpenAI, map[string]any{"shared-public": "gpt-5"}),
		desktopModelAccount(PlatformDeepseek, map[string]any{"shared-public": "deepseek-v4-pro"}),
	}
	catalog = NewDesktopAgentModelCatalog(repo, compositeRouteRepoStub{})
	got, err = catalog.ModelsForGroup(context.Background(), group)
	require.NoError(t, err)
	require.NotContains(t, got, "shared-public", "privacy-ineligible claims must still preserve gateway ownership ambiguity")
}

func TestDesktopAgentModelCatalogAcceptsMixedAntigravityForExplicitNativeRoutes(t *testing.T) {
	group := &Group{ID: 12, Platform: PlatformComposite}
	account := desktopModelAccount(PlatformAntigravity, map[string]any{"ag-private": "claude-sonnet-4-5"})
	account.Extra = map[string]any{"mixed_scheduling": true}
	repo := &desktopModelAccounts{accounts: []Account{account}}
	routes := compositeRouteRepoStub{routes: []CompositeModelRoute{
		{ID: 1, GroupID: group.ID, Enabled: true, PublicModel: "claude-public", MatchType: CompositeRouteMatchExact, Endpoint: CompositeRouteEndpointResponses, TargetPlatform: PlatformAnthropic, UpstreamModel: "ag-private"},
	}}
	catalog := NewDesktopAgentModelCatalog(repo, routes)
	got, err := catalog.ModelsForGroup(context.Background(), group)
	require.NoError(t, err)
	require.Contains(t, got, "claude-public")
	repo.accounts[0].Extra = map[string]any{"mixed_scheduling": false}
	got, err = catalog.ModelsForGroup(context.Background(), group)
	require.NoError(t, err)
	require.NotContains(t, got, "claude-public")
}

func TestDesktopAgentModelCatalogAppliesChannelPricingRestrictionAtItsBillingStage(t *testing.T) {
	for _, source := range []string{BillingModelSourceRequested, BillingModelSourceChannelMapped, BillingModelSourceUpstream} {
		t.Run(source, func(t *testing.T) {
			group := &Group{ID: 12, Platform: PlatformOpenAI}
			account := desktopModelAccount(PlatformOpenAI, map[string]any{"account-model": "upstream-model"})
			channel := desktopModelChannel(group.ID, group.Platform, map[string]string{"public-agent": "account-model"})
			channel.BillingModelSource, channel.RestrictModels = source, true
			pricingModel := map[string]string{
				BillingModelSourceRequested: "account-model", BillingModelSourceChannelMapped: "account-model", BillingModelSourceUpstream: "upstream-model",
			}[source]
			channel.ModelPricing = []ChannelModelPricing{{Platform: PlatformOpenAI, Models: []string{pricingModel}}}
			catalog := NewDesktopAgentModelCatalog(&desktopModelAccounts{accounts: []Account{account}}, nil, desktopModelChannelReader{channel: channel})
			got, err := catalog.ModelsForGroup(context.Background(), group)
			require.NoError(t, err)
			require.Contains(t, got, "public-agent")
			channel.ModelPricing = []ChannelModelPricing{{Platform: PlatformOpenAI, Models: []string{"unrelated"}}}
			got, err = catalog.ModelsForGroup(context.Background(), group)
			require.NoError(t, err)
			require.NotContains(t, got, "public-agent")
		})
	}
}

func TestDesktopAgentModelCatalogNativeResponsesSelectsBeforeChannelMapping(t *testing.T) {
	group := &Group{ID: 12, Platform: PlatformComposite}
	account := desktopModelAccount(PlatformAnthropic, map[string]any{"selection-model": "selection-upstream", "forward-model": "forward-upstream"})
	repo := &desktopModelAccounts{accounts: []Account{account}}
	routes := compositeRouteRepoStub{routes: []CompositeModelRoute{
		{ID: 1, GroupID: group.ID, Enabled: true, PublicModel: "claude-agent", MatchType: CompositeRouteMatchExact, Endpoint: CompositeRouteEndpointResponses, TargetPlatform: PlatformAnthropic, UpstreamModel: "selection-model"},
	}}
	channel := desktopModelChannel(group.ID, PlatformAnthropic, map[string]string{"selection-model": "forward-model"})
	channel.RestrictModels, channel.BillingModelSource = true, BillingModelSourceUpstream
	channel.ModelPricing = []ChannelModelPricing{{Platform: PlatformAnthropic, Models: []string{"selection-upstream"}}}
	catalog := NewDesktopAgentModelCatalog(repo, routes, desktopModelChannelReader{channel: channel})
	got, err := catalog.ModelsForGroup(context.Background(), group)
	require.NoError(t, err)
	require.Contains(t, got, "claude-agent")

	channel.ModelPricing = []ChannelModelPricing{{Platform: PlatformAnthropic, Models: []string{"forward-upstream"}}}
	got, err = catalog.ModelsForGroup(context.Background(), group)
	require.NoError(t, err)
	require.NotContains(t, got, "claude-agent", "native scheduler restricts the pre-channel account model")

	channel.RestrictModels = false
	repo.accounts = []Account{desktopModelAccount(PlatformAnthropic, map[string]any{"forward-model": "forward-upstream"})}
	got, err = catalog.ModelsForGroup(context.Background(), group)
	require.NoError(t, err)
	require.NotContains(t, got, "claude-agent", "channel target support alone cannot pass native account selection")
}
