package service

import (
	"context"
	"encoding/json"
	"errors"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

type imageStudioModelsAccountRepo struct {
	AccountRepository
	accounts []Account
	err      error
	groupID  int64
}

func (r *imageStudioModelsAccountRepo) ListModelAvailabilityCandidates(_ context.Context, groupID *int64, _ []string, includeGrouped bool) ([]Account, error) {
	if groupID == nil || includeGrouped {
		return nil, errors.New("studio discovery must stay scoped to its configured group")
	}
	r.groupID = *groupID
	var accounts []Account
	for _, account := range r.accounts {
		if account.IsActive() && account.Schedulable {
			accounts = append(accounts, account)
		}
	}
	return accounts, r.err
}

func studioModelsAccount(id int64, platform string, mapping map[string]any) Account {
	return Account{
		ID: id, Platform: platform, Type: AccountTypeAPIKey, Status: StatusActive, Schedulable: true,
		Credentials: map[string]any{"model_mapping": mapping},
	}
}

func studioModelsGateway(repo *imageStudioModelsAccountRepo, routes ...CompositeModelRoute) *GatewayService {
	svc := &GatewayService{accountRepo: repo}
	svc.compositeResolver = NewCompositeRouteResolver(compositeRouteRepoStub{routes: routes})
	svc.compositeResolver.SetModelOwnershipResolver(svc.resolveCompositeModelOwnership)
	return svc
}

func studioModelRoute(public, target, platform, endpoint string) CompositeModelRoute {
	return CompositeModelRoute{GroupID: 14, PublicModel: public, UpstreamModel: target,
		TargetPlatform: platform, Endpoint: endpoint, MatchType: CompositeRouteMatchExact, Enabled: true}
}

func TestImageStudioModelsIgnoreRoutesWithoutConfiguredSupplier(t *testing.T) {
	disabled := studioModelsAccount(68, PlatformOpenAI, map[string]any{"gpt-image-2.5-flare": "gpt-image-2.5-flare"})
	disabled.Schedulable = false
	repo := &imageStudioModelsAccountRepo{accounts: []Account{
		disabled,
		studioModelsAccount(72, PlatformOpenAI, map[string]any{"gpt-image-2": "gpt-image-2-private", "gpt-image-2.5": "gpt-image-2.5-private"}),
	}}
	svc := studioModelsGateway(repo,
		studioModelRoute("gpt-image-2", "gpt-image-2", PlatformOpenAI, CompositeRouteEndpointImages),
		studioModelRoute("gpt-image-2.5", "gpt-image-2.5", PlatformOpenAI, CompositeRouteEndpointImages),
		studioModelRoute("gpt-image-2-4K", "gpt-image-2-4K", PlatformOpenAI, CompositeRouteEndpointImages),
		studioModelRoute("gpt-image-2.5-flare", "gpt-image-2.5-flare", PlatformOpenAI, CompositeRouteEndpointImages),
		studioModelRoute("gpt-image-2.5-sunburst", "gpt-image-2.5-sunburst", PlatformOpenAI, CompositeRouteEndpointImages),
	)
	models, err := svc.ListImageStudioModels(context.Background(), &Group{ID: 14, Platform: PlatformComposite, Status: StatusActive, AllowImageGeneration: true})
	require.NoError(t, err)
	require.Equal(t, []ImageStudioModel{{ID: "gpt-image-2", API: "openai"}, {ID: "gpt-image-2.5", API: "openai"}}, models)
	require.Equal(t, int64(14), repo.groupID)
	encoded, err := json.Marshal(models)
	require.NoError(t, err)
	require.NotContains(t, string(encoded), "private")
}

func TestImageStudioModelsKeepTemporaryUnavailableAccountsAndRespectPublicAllowlist(t *testing.T) {
	account := studioModelsAccount(1, PlatformOpenAI, map[string]any{"gpt-image-2": "gpt-image-2", "gpt-image-2.5": "gpt-image-2.5"})
	future := time.Now().Add(time.Hour)
	account.RateLimitResetAt, account.OverloadUntil, account.TempUnschedulableUntil = &future, &future, &future
	svc := studioModelsGateway(&imageStudioModelsAccountRepo{accounts: []Account{account}})
	models, err := svc.ListImageStudioModels(context.Background(), &Group{
		ID: 14, Platform: PlatformOpenAI, Status: StatusActive, AllowImageGeneration: true,
		ModelAllowlist: GroupModelAllowlist{Enabled: true, Models: []string{"gpt-image-2.5"}},
	})
	require.NoError(t, err)
	require.Equal(t, []ImageStudioModel{{ID: "gpt-image-2.5", API: "openai"}}, models)
}

func TestImageStudioModelsResolveAliasesToCorrectClientProtocol(t *testing.T) {
	svc := studioModelsGateway(&imageStudioModelsAccountRepo{accounts: []Account{
		studioModelsAccount(1, PlatformOpenAI, map[string]any{"gpt-image-2": "gpt-image-2-provider"}),
		studioModelsAccount(2, PlatformGemini, map[string]any{"gemini-3.1-flash-image": "gemini-3.1-flash-image"}),
	}},
		studioModelRoute("public-painter", "gpt-image-2", PlatformOpenAI, CompositeRouteEndpointImages),
		studioModelRoute("public-banana", "gemini-3.1-flash-image", PlatformGemini, CompositeRouteEndpointGemini),
		studioModelRoute("fake-image", "gpt-5.4", PlatformOpenAI, CompositeRouteEndpointImages),
	)
	models, err := svc.ListImageStudioModels(context.Background(), &Group{
		ID: 14, Platform: PlatformComposite, Status: StatusActive, AllowImageGeneration: true,
		ModelAllowlist: GroupModelAllowlist{Enabled: true, Models: []string{"public-painter", "public-banana", "fake-image"}},
	})
	require.NoError(t, err)
	require.Equal(t, []ImageStudioModel{{ID: "public-painter", API: "openai"}, {ID: "public-banana", API: "gemini"}}, models)
}

func TestImageStudioModelsCompatibleGeminiUsesImagesProtocol(t *testing.T) {
	svc := studioModelsGateway(&imageStudioModelsAccountRepo{accounts: []Account{
		studioModelsAccount(1, PlatformOpenAI, map[string]any{"gpt-image-2": "gemini-3.1-flash-image"}),
	}})
	models, err := svc.ListImageStudioModels(context.Background(), &Group{ID: 14, Platform: PlatformOpenAI, Status: StatusActive, AllowImageGeneration: true})
	require.NoError(t, err)
	require.Equal(t, []ImageStudioModel{{ID: "gpt-image-2", API: "openai"}}, models)
}

func TestImageStudioModelsOAuthCannotServeCompatibleGeminiAlias(t *testing.T) {
	account := studioModelsAccount(1, PlatformOpenAI, map[string]any{"gemini-3.1-flash-image": "gpt-image-2"})
	account.Type = AccountTypeOAuth
	svc := studioModelsGateway(&imageStudioModelsAccountRepo{accounts: []Account{account}})
	models, err := svc.ListImageStudioModels(context.Background(), &Group{ID: 14, Platform: PlatformOpenAI, Status: StatusActive, AllowImageGeneration: true})
	require.NoError(t, err)
	require.Empty(t, models)
}

func TestImageStudioModelsGrokUsesNormalizedRequestAndAllowsProviderTarget(t *testing.T) {
	svc := studioModelsGateway(&imageStudioModelsAccountRepo{accounts: []Account{
		studioModelsAccount(1, PlatformGrok, map[string]any{"grok-imagine-image-quality": "vendor-product-id"}),
	}}, studioModelRoute("public-grok", "grok-imagine", PlatformGrok, CompositeRouteEndpointImages))
	models, err := svc.ListImageStudioModels(context.Background(), &Group{
		ID: 14, Platform: PlatformComposite, Status: StatusActive, AllowImageGeneration: true,
		ModelAllowlist: GroupModelAllowlist{Enabled: true, Models: []string{"public-grok"}},
	})
	require.NoError(t, err)
	require.Equal(t, []ImageStudioModel{{ID: "public-grok", API: "openai"}}, models)
}

func TestImageStudioModelsEmptyPoolDoesNotResurrectDefaults(t *testing.T) {
	svc := studioModelsGateway(&imageStudioModelsAccountRepo{})
	models, err := svc.ListImageStudioModels(context.Background(), &Group{ID: 14, Platform: PlatformOpenAI, Status: StatusActive, AllowImageGeneration: true})
	require.NoError(t, err)
	require.NotNil(t, models)
	require.Empty(t, models)
}

func TestImageStudioModelsUnmappedAccountNeedsExplicitCatalog(t *testing.T) {
	svc := studioModelsGateway(&imageStudioModelsAccountRepo{accounts: []Account{
		studioModelsAccount(1, PlatformOpenAI, nil),
	}}, studioModelRoute("configured-painter", "gpt-image-2", PlatformOpenAI, CompositeRouteEndpointImages))
	group := &Group{ID: 14, Platform: PlatformOpenAI, Status: StatusActive, AllowImageGeneration: true}
	models, err := svc.ListImageStudioModels(context.Background(), group)
	require.NoError(t, err)
	require.Empty(t, models, "an unmapped account must not publish built-in model guesses")

	group.Platform = PlatformComposite
	models, err = svc.ListImageStudioModels(context.Background(), group)
	require.NoError(t, err)
	require.Equal(t, []ImageStudioModel{{ID: "configured-painter", API: "openai"}}, models,
		"an explicit public route backed by an unrestricted account remains available")
}

func TestImageStudioModelsDiscoveryFailureIsExplicit(t *testing.T) {
	svc := studioModelsGateway(&imageStudioModelsAccountRepo{err: errors.New("database unavailable")})
	_, err := svc.ListImageStudioModels(context.Background(), &Group{ID: 14, Platform: PlatformComposite, Status: StatusActive, AllowImageGeneration: true})
	require.Error(t, err)
}
