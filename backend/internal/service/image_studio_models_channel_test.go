//go:build unit

package service

import (
	"context"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestImageStudioModelsRespectPersistentChannelRestrictions(t *testing.T) {
	for _, source := range []string{BillingModelSourceRequested, BillingModelSourceUpstream, BillingModelSourceChannelMapped} {
		t.Run(source, func(t *testing.T) {
			account := studioModelsAccount(1, PlatformOpenAI, map[string]any{
				"gpt-image-2": "gpt-image-2-provider", "gpt-image-2.5": "gpt-image-2.5-provider",
			})
			svc := studioModelsGateway(&imageStudioModelsAccountRepo{accounts: []Account{account}})
			allowed := "gpt-image-2"
			if source == BillingModelSourceUpstream {
				allowed = "gpt-image-2-provider"
			}
			channel := Channel{
				ID: 1, Status: StatusActive, GroupIDs: []int64{14}, RestrictModels: true, BillingModelSource: source,
				ModelPricing: []ChannelModelPricing{{Platform: PlatformOpenAI, Models: []string{allowed}}},
			}
			svc.channelService = newTestChannelService(makeStandardRepo(channel, map[int64]string{14: PlatformOpenAI}))
			models, err := svc.ListImageStudioModels(context.Background(), &Group{ID: 14, Platform: PlatformOpenAI, Status: StatusActive, AllowImageGeneration: true})
			require.NoError(t, err)
			require.Equal(t, []ImageStudioModel{studioOpenAIModelWithProfile("gpt-image-2", "openai", "gpt-image-2")}, models)
		})
	}
}

func TestImageStudioProfilesDoNotBorrowPrivateMappingContract(t *testing.T) {
	svc := studioModelsGateway(&imageStudioModelsAccountRepo{accounts: []Account{
		studioModelsAccount(1, PlatformOpenAI, map[string]any{
			"gpt-image-2": "gpt-image-1", "gpt-image-2.5": "gpt-image-2.6-sunburst-2026-11-01",
		}),
	}}, studioModelRoute("public-painter", "gpt-image-2", PlatformOpenAI, CompositeRouteEndpointImages))
	channel := Channel{
		ID: 1, Status: StatusActive, GroupIDs: []int64{14},
		ModelMapping: map[string]map[string]string{PlatformOpenAI: {"gpt-image-2": "gpt-image-2.5"}},
	}
	svc.channelService = newTestChannelService(makeStandardRepo(channel, map[int64]string{14: PlatformOpenAI}))
	models, err := svc.ListImageStudioModels(context.Background(), &Group{
		ID: 14, Platform: PlatformComposite, Status: StatusActive, AllowImageGeneration: true,
		ModelAllowlist: GroupModelAllowlist{Enabled: true, Models: []string{"public-painter"}},
	})
	require.NoError(t, err)
	require.Equal(t, []ImageStudioModel{studioOpenAIModelWithProfile("public-painter", "unknown", "unknown")}, models)
}

func TestImageStudioSizeTiersExcludeChannelRejectedSupplier(t *testing.T) {
	const model = "gpt-image-2.6-flare"
	svc := studioModelsGateway(&imageStudioModelsAccountRepo{accounts: []Account{
		studioTierAccount(1, model, "gpt-image-2.6-flare-1K", "1K", "2K"),
		studioTierAccount(2, model, "gpt-image-2.6-flare-4K", "4K"),
	}})
	channel := Channel{
		ID: 1, Status: StatusActive, GroupIDs: []int64{14}, RestrictModels: true, BillingModelSource: BillingModelSourceUpstream,
		ModelPricing: []ChannelModelPricing{{Platform: PlatformOpenAI, Models: []string{"gpt-image-2.6-flare-1K"}}},
	}
	svc.channelService = newTestChannelService(makeStandardRepo(channel, map[int64]string{14: PlatformOpenAI}))
	models, err := svc.ListImageStudioModels(context.Background(), &Group{ID: 14, Platform: PlatformOpenAI, Status: StatusActive, AllowImageGeneration: true})
	require.NoError(t, err)
	require.Len(t, models, 1)
	require.Equal(t, []string{"1K", "2K"}, models[0].AvailableSizeTiers)
}
