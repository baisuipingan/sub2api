package service

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestImageStudioProfilesRecognizeDocumentedFamilies(t *testing.T) {
	for _, tc := range []struct{ model, provider, profile string }{
		{"gpt-image-2.6-flare", "openai", "gpt-image-2.6"},
		{"gpt-image-2.6-sunburst-2026-11-01", "openai", "gpt-image-2.6"},
		{"gpt-image-2.5", "openai", "gpt-image-2.5"},
		{"gpt-image-2.5-flare-2026-09-08", "openai", "gpt-image-2.5"},
		{"gpt-image-2.5-sunburst", "openai", "gpt-image-2.5"},
		{"gpt-image-2", "openai", "gpt-image-2"},
		{"gpt-image-2-2026-04-21", "openai", "gpt-image-2"},
		{" gpt-image-2-2K ", "openai", "gpt-image-2"},
		{"gpt-image-2-4K", "openai", "gpt-image-2"},
		{"gpt-image-1", "openai", "gpt-image-1"},
		{"gpt-image-1.5", "openai", "gpt-image-1.5"},
		{"gpt-image-1-mini", "openai", "gpt-image-1-mini"},
		{"gemini-2.5-flash-image-preview", "google", "gemini-2.5-flash-image"},
		{"models/gemini-3-pro-image", "google", "gemini-3-pro-image"},
		{"gemini-3.1-flash-image", "google", "gemini-3.1-flash-image"},
		{"gemini-nano-banana-2.1", "google", "gemini-nano-banana-2.1"},
		{"gemini-3.1-flash-lite-image", "google", "gemini-3.1-flash-lite-image"},
		{"grok-imagine-image-2.0", "xai", "grok-imagine-image-2.0"},
		{"grok-imagine-image-pro", "xai", "grok-imagine-image-pro"},
		{"grok-imagine-image", "xai", "grok-imagine-image"},
	} {
		t.Run(tc.model, func(t *testing.T) {
			require.Equal(t, ImageStudioModelProfile{Provider: tc.provider, Profile: tc.profile}, imageStudioProfileForModel(tc.model))
		})
	}
}

func TestImageStudioProfilesDoNotGuessPrivateOrAdjacentFamilies(t *testing.T) {
	for _, model := range []string{
		"gpt-image-2.60-flare", "gpt-image-2.6-flare-private", "gpt-image-2.5-private", "gpt-image-20",
		"gpt-image-1.50", "gpt-image-1-mini-private", "gemini-3.10-flash-image", "gemini-3-pro-image-private",
		"gemini-nano-banana-2.10", "grok-imagine-image-2.00", "grok-imagine-image-2.0-2026-11-05", "vendor-product-id", "private-alias",
	} {
		t.Run(model, func(t *testing.T) {
			require.Equal(t, unknownImageStudioProfile, imageStudioProfileForModel(model))
		})
	}
}

func TestImageStudioProfilesDescribeEverySupplierBehindPublicAlias(t *testing.T) {
	svc := studioModelsGateway(&imageStudioModelsAccountRepo{accounts: []Account{
		studioModelsAccount(1, PlatformOpenAI, map[string]any{"gpt-image-2": "gpt-image-2.6-flare"}),
		studioModelsAccount(2, PlatformOpenAI, map[string]any{"gpt-image-2": "gemini-3.1-flash-image"}),
		studioModelsAccount(3, PlatformOpenAI, map[string]any{"gpt-image-2": "gpt-image-2.6-sunburst"}),
		studioModelsAccount(4, PlatformOpenAI, map[string]any{"gpt-image-2": "gpt-image-private-product"}),
	}}, studioModelRoute("public-painter", "gpt-image-2", PlatformOpenAI, CompositeRouteEndpointImages))
	group := &Group{ID: 14, Platform: PlatformComposite, Status: StatusActive, AllowImageGeneration: true,
		ModelAllowlist: GroupModelAllowlist{Enabled: true, Models: []string{"public-painter"}}}
	models, err := svc.ListImageStudioModels(context.Background(), group)
	require.NoError(t, err)
	require.Equal(t, []ImageStudioModel{{ID: "public-painter", API: "openai", Profiles: []ImageStudioModelProfile{
		{Provider: "google", Profile: "gemini-3.1-flash-image"},
		{Provider: "openai", Profile: "gpt-image-2.6"},
		unknownImageStudioProfile,
	}}}, models)
	encoded, err := json.Marshal(models)
	require.NoError(t, err)
	require.NotContains(t, string(encoded), "gpt-image-private-product")
	require.NotContains(t, string(encoded), "flare")
	require.NotContains(t, string(encoded), "sunburst")
}

func TestImageStudioProfilesDoNotInferTargetFromKnownPublicName(t *testing.T) {
	svc := studioModelsGateway(&imageStudioModelsAccountRepo{accounts: []Account{
		studioModelsAccount(1, PlatformOpenAI, map[string]any{"gpt-image-2.6-flare": "gpt-image-private-product"}),
	}})
	models, err := svc.ListImageStudioModels(context.Background(), &Group{ID: 14, Platform: PlatformOpenAI, Status: StatusActive, AllowImageGeneration: true})
	require.NoError(t, err)
	require.Equal(t, []ImageStudioModel{studioModelWithProfile("gpt-image-2.6-flare", "openai", "unknown", "unknown")}, models)
}

func TestImageStudioProfilesMergeSizeProductsByCanonicalFamily(t *testing.T) {
	twoK := studioModelsAccount(1, PlatformOpenAI, map[string]any{"gpt-image-2": "gpt-image-2-2K"})
	fourK := studioModelsAccount(2, PlatformOpenAI, map[string]any{"gpt-image-2": "gpt-image-2-4K"})
	twoK.Extra = map[string]any{AccountExtraOpenAIImageSizeTiers: []any{"2K"}}
	fourK.Extra = map[string]any{AccountExtraOpenAIImageSizeTiers: []any{"4K"}}
	svc := studioModelsGateway(&imageStudioModelsAccountRepo{accounts: []Account{twoK, fourK}})
	models, err := svc.ListImageStudioModels(context.Background(), &Group{ID: 14, Platform: PlatformOpenAI, Status: StatusActive, AllowImageGeneration: true})
	require.NoError(t, err)
	require.Equal(t, []ImageStudioModel{studioModelWithProfile("gpt-image-2", "openai", "openai", "gpt-image-2")}, models)
}

func TestImageStudioProfilesPreserveBridgeConstraintInMixedPool(t *testing.T) {
	apiKey := studioModelsAccount(1, PlatformOpenAI, map[string]any{"gpt-image-2.6-flare": "gpt-image-2.6-flare"})
	apiKey.Extra = map[string]any{"openai_passthrough": true}
	oauth := studioModelsAccount(2, PlatformOpenAI, map[string]any{"gpt-image-2.6-flare": "gpt-image-2.6-sunburst"})
	oauth.Type = AccountTypeOAuth
	setupToken := studioModelsAccount(3, PlatformOpenAI, map[string]any{"gpt-image-2.6-flare": "gpt-image-2.6-flare"})
	setupToken.Type = AccountTypeSetupToken
	svc := studioModelsGateway(&imageStudioModelsAccountRepo{accounts: []Account{apiKey, oauth, setupToken}})
	models, err := svc.ListImageStudioModels(context.Background(), &Group{ID: 14, Platform: PlatformOpenAI, Status: StatusActive, AllowImageGeneration: true})
	require.NoError(t, err)
	require.Equal(t, []ImageStudioModel{{ID: "gpt-image-2.6-flare", API: "openai", Profiles: []ImageStudioModelProfile{
		{Provider: "openai", Profile: "gpt-image-2.6"},
		{Provider: "openai", Profile: "gpt-image-2.6", Transport: "bridge"},
	}}}, models)
}

func TestImageStudioProfilesKeepGeminiNativeOAuthFamily(t *testing.T) {
	account := studioModelsAccount(1, PlatformGemini, map[string]any{"gemini-3.1-flash-image": "gemini-2.5-flash-image"})
	account.Type = AccountTypeOAuth
	account.Credentials["project_id"] = "private-project"
	svc := studioModelsGateway(&imageStudioModelsAccountRepo{accounts: []Account{account}})
	models, err := svc.ListImageStudioModels(context.Background(), &Group{ID: 14, Platform: PlatformGemini, Status: StatusActive, AllowImageGeneration: true})
	require.NoError(t, err)
	require.Equal(t, []ImageStudioModel{studioModelWithProfile("gemini-3.1-flash-image", "gemini", "google", "gemini-3.1-flash-image")}, models,
		"the native OAuth path forwards the channel model without account mapping")
}

func TestImageStudioProfilesImagesAPIKeyPassthroughStillUsesAccountMapping(t *testing.T) {
	account := studioModelsAccount(1, PlatformOpenAI, map[string]any{"gpt-image-2": "gemini-3-pro-image"})
	account.Extra = map[string]any{"openai_passthrough": true}
	svc := studioModelsGateway(&imageStudioModelsAccountRepo{accounts: []Account{account}})
	models, err := svc.ListImageStudioModels(context.Background(), &Group{ID: 14, Platform: PlatformOpenAI, Status: StatusActive, AllowImageGeneration: true})
	require.NoError(t, err)
	require.Equal(t, []ImageStudioModel{studioModelWithProfile("gpt-image-2", "openai", "google", "gemini-3-pro-image")}, models)
}

func TestImageStudioProfilesNanoBananaUsesNativeAdmissionOnly(t *testing.T) {
	for _, platform := range []string{PlatformGemini, PlatformOpenAI} {
		t.Run(platform, func(t *testing.T) {
			svc := studioModelsGateway(&imageStudioModelsAccountRepo{accounts: []Account{
				studioModelsAccount(1, platform, map[string]any{"gemini-nano-banana-2.1": "gemini-nano-banana-2.1"}),
			}}, studioModelRoute("public-banana", "gemini-nano-banana-2.1", platform, CompositeRouteEndpointAny))
			models, err := svc.ListImageStudioModels(context.Background(), &Group{
				ID: 14, Platform: PlatformComposite, Status: StatusActive, AllowImageGeneration: true,
				ModelAllowlist: GroupModelAllowlist{Enabled: true, Models: []string{"public-banana"}},
			})
			require.NoError(t, err)
			if platform == PlatformGemini {
				require.Equal(t, []ImageStudioModel{studioModelWithProfile("public-banana", "gemini", "google", "gemini-nano-banana-2.1")}, models)
			} else {
				require.Empty(t, models, "metadata must not bypass the existing Images API model validation")
			}
		})
	}
}
