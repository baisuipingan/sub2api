package service

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func studioTierAccount(id int64, public, upstream string, tiers ...string) Account {
	account := studioModelsAccount(id, PlatformOpenAI, map[string]any{public: upstream})
	account.Extra = map[string]any{AccountExtraOpenAIImageSizeTiers: tiers}
	return account
}

func TestImageStudioSizeTiersUnionAcrossAdmittedSuppliers(t *testing.T) {
	const model = "gpt-image-2.6-flare"
	for _, tc := range []struct {
		name     string
		accounts []Account
		want     []string
	}{
		{
			name:     "one supplier limited to 1K and 2K",
			accounts: []Account{studioTierAccount(1, model, model, "1K", "2K")},
			want:     []string{"1K", "2K"},
		},
		{
			name: "2K and 4K suppliers form a union",
			accounts: []Account{
				studioTierAccount(1, model, model, "2K"),
				studioTierAccount(2, model, model, "4K"),
			},
			want: []string{"2K", "4K"},
		},
		{
			name: "unrestricted supplier admits all sizes",
			accounts: []Account{
				studioTierAccount(1, model, model, "2K"),
				studioTierAccount(2, model, model),
			},
			want: []string{"1K", "2K", "4K"},
		},
		{
			name:     "explicit all tiers is unrestricted",
			accounts: []Account{studioTierAccount(1, model, model, "4K", "2K", "1K")},
			want:     []string{"1K", "2K", "4K"},
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			svc := studioModelsGateway(&imageStudioModelsAccountRepo{accounts: tc.accounts})
			models, err := svc.ListImageStudioModels(context.Background(), &Group{ID: 14, Platform: PlatformOpenAI, Status: StatusActive, AllowImageGeneration: true})
			require.NoError(t, err)
			require.Len(t, models, 1)
			require.Equal(t, tc.want, models[0].AvailableSizeTiers)
			require.Equal(t, []ImageStudioModelProfile{{Provider: "openai", Profile: "gpt-image-2.6"}}, models[0].Profiles)
		})
	}
}

func TestImageStudioSizeTiersExcludeDisabledAndUnrelatedModelsButKeepCooldown(t *testing.T) {
	const model = "gpt-image-2.6-flare"
	active := studioTierAccount(1, model, model, "1K", "2K")
	future := time.Now().Add(time.Hour)
	active.RateLimitResetAt, active.TempUnschedulableUntil, active.OverloadUntil = &future, &future, &future
	disabled := studioTierAccount(2, model, model, "4K")
	disabled.Schedulable = false
	inactive := studioTierAccount(3, model, model, "4K")
	inactive.Status = StatusDisabled
	otherModel := studioTierAccount(4, "gpt-image-2", "gpt-image-2", "4K")
	svc := studioModelsGateway(&imageStudioModelsAccountRepo{accounts: []Account{active, disabled, inactive, otherModel}},
		studioModelRoute("public-painter", model, PlatformOpenAI, CompositeRouteEndpointImages))
	models, err := svc.ListImageStudioModels(context.Background(), &Group{
		ID: 14, Platform: PlatformComposite, Status: StatusActive, AllowImageGeneration: true,
		ModelAllowlist: GroupModelAllowlist{Enabled: true, Models: []string{"public-painter"}},
	})
	require.NoError(t, err)
	require.Len(t, models, 1)
	require.Equal(t, "public-painter", models[0].ID)
	require.Equal(t, []string{"1K", "2K"}, models[0].AvailableSizeTiers)
}

func TestImageStudioSizeTiersDoNotApplyToNativeGeminiOrGrok(t *testing.T) {
	for _, tc := range []struct{ platform, model, api string }{
		{PlatformGemini, "gemini-3.1-flash-image", "gemini"},
		{PlatformGrok, "grok-imagine-image-2.0", "openai"},
	} {
		t.Run(tc.platform, func(t *testing.T) {
			account := studioTierAccount(1, tc.model, tc.model, "1K")
			account.Platform = tc.platform
			svc := studioModelsGateway(&imageStudioModelsAccountRepo{accounts: []Account{account}})
			models, err := svc.ListImageStudioModels(context.Background(), &Group{ID: 14, Platform: tc.platform, Status: StatusActive, AllowImageGeneration: true})
			require.NoError(t, err)
			require.Len(t, models, 1)
			require.Equal(t, tc.api, models[0].API)
			require.Nil(t, models[0].AvailableSizeTiers)
			encoded, err := json.Marshal(models)
			require.NoError(t, err)
			require.NotContains(t, string(encoded), "available_size_tiers")
		})
	}
}

func TestImageStudioSizeTiersApplyToGeminiUsingOpenAIImages(t *testing.T) {
	account := studioTierAccount(1, "gpt-image-2", "gemini-3.1-flash-image", "1K", "2K")
	svc := studioModelsGateway(&imageStudioModelsAccountRepo{accounts: []Account{account}})
	models, err := svc.ListImageStudioModels(context.Background(), &Group{ID: 14, Platform: PlatformOpenAI, Status: StatusActive, AllowImageGeneration: true})
	require.NoError(t, err)
	require.Len(t, models, 1)
	require.Equal(t, "openai", models[0].API)
	require.Equal(t, "google", models[0].Profiles[0].Provider)
	require.Equal(t, []string{"1K", "2K"}, models[0].AvailableSizeTiers)
}
