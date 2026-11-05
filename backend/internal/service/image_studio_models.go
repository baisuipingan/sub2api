package service

import (
	"context"
	"fmt"
	"sort"
	"strings"
)

// ImageStudioModel exposes the public request ID, client protocol and capability profiles.
// Account IDs, credentials and private upstream mapping targets stay server-side.
type ImageStudioModel struct {
	ID       string                    `json:"id"`
	API      string                    `json:"api"`
	Profiles []ImageStudioModelProfile `json:"profiles"`
}

// ListImageStudioModels builds the studio catalog from persistent configuration.
// Unlike the general discovery endpoint, a composite route alone cannot publish
// a studio option: an enabled account must actually admit its resolved model.
// Temporary throttling, concurrency, balance and cooldowns do not change this list.
func (s *GatewayService) ListImageStudioModels(ctx context.Context, group *Group) ([]ImageStudioModel, error) {
	models := make([]ImageStudioModel, 0)
	if group == nil || group.ID <= 0 || group.Status != StatusActive || !GroupAllowsImageGeneration(group) {
		return models, nil
	}
	if s == nil || s.accountRepo == nil {
		return nil, fmt.Errorf("image studio model discovery is not configured")
	}
	platforms := []string{PlatformOpenAI, PlatformGemini, PlatformAntigravity, PlatformGrok}
	accounts, err := s.accountRepo.ListModelAvailabilityCandidates(ctx, &group.ID, platforms, false)
	if err != nil {
		return nil, fmt.Errorf("load image studio accounts: %w", err)
	}
	eligible := make([]Account, 0, len(accounts))
	candidates := make(map[string]struct{})
	add := func(model string) {
		model = strings.TrimSpace(model)
		if model != "" && !strings.ContainsAny(model, "*?") {
			candidates[model] = struct{}{}
		}
	}
	for _, account := range accounts {
		if !account.IsActive() || !account.Schedulable || !imageStudioGroupAcceptsAccount(group, &account) {
			continue
		}
		eligible = append(eligible, account)
		mapping := account.GetModelMapping()
		for public, target := range mapping {
			if imageStudioImageModel(public) || imageStudioImageModel(target) {
				add(public)
			}
		}
	}
	if group.Platform == PlatformComposite {
		for _, endpoint := range []string{CompositeRouteEndpointImages, CompositeRouteEndpointGemini} {
			routes, err := s.GetCompositeRouteModels(ctx, &group.ID, endpoint, false)
			if err != nil {
				return nil, fmt.Errorf("load image studio routes: %w", err)
			}
			for _, model := range routes {
				add(model)
			}
		}
	}
	if group.ModelAllowlistEnabled() {
		for _, model := range group.ModelAllowlist.Models {
			add(model)
		}
	}
	ids := make([]string, 0, len(candidates))
	for model := range candidates {
		ids = append(ids, model)
	}
	sort.Strings(ids)
	if group.ModelAllowlistEnabled() {
		ids = group.ModelAllowlist.FilterForListing(ids)
	}
	for _, model := range ids {
		metadata, err := s.imageStudioModelMetadata(ctx, group, eligible, model)
		if err != nil {
			return nil, err
		}
		if metadata.API != "" {
			models = append(models, metadata)
		}
	}
	return models, nil
}

func imageStudioGroupAcceptsAccount(group *Group, account *Account) bool {
	if group.RequirePrivacySet && !account.IsPrivacySet() {
		return false
	}
	if group.Platform == PlatformComposite {
		return true
	}
	return account.Platform == group.Platform ||
		(group.Platform == PlatformGemini && account.Platform == PlatformAntigravity && account.IsMixedSchedulingEnabled())
}

func imageStudioImageModel(model string) bool {
	return isOpenAIImageGenerationModel(model) || imageStudioGoogleImageModel(model)
}

func imageStudioGoogleImageModel(model string) bool {
	return isGeminiCompatibleImageModel(model) || imageStudioProfileForModel(model).Provider == "google"
}

func (s *GatewayService) imageStudioModelMetadata(ctx context.Context, group *Group, accounts []Account, public string) (ImageStudioModel, error) {
	for _, api := range []struct{ name, endpoint string }{
		{"openai", CompositeRouteEndpointImages},
		{"gemini", CompositeRouteEndpointGemini},
	} {
		platform, requestModel := group.Platform, public
		requestCtx := ctx
		if group.Platform == PlatformComposite {
			resolver := s.compositeResolver
			if resolver == nil {
				return ImageStudioModel{}, fmt.Errorf("image studio composite routing is not configured")
			}
			decision, err := resolver.Resolve(ctx, group.ID, public, api.endpoint)
			if err != nil {
				return ImageStudioModel{}, fmt.Errorf("resolve image studio model: %w", err)
			}
			if !decision.Matched {
				continue
			}
			platform, requestModel = decision.TargetPlatform, decision.UpstreamModel
			requestCtx = WithCompositeRouteDecision(ctx, decision)
		}
		if api.name == "openai" && platform != PlatformOpenAI && platform != PlatformGrok {
			continue
		}
		if api.name == "gemini" && platform != PlatformGemini && (group.Platform != PlatformComposite || platform != PlatformAntigravity) {
			continue
		}
		if platform == PlatformGrok {
			requestModel = NormalizeGrokMediaModelForEndpoint(GrokMediaEndpointImagesGenerations, requestModel, false)
		}
		channelModel := requestModel
		if platform != PlatformGrok {
			mapping, _ := s.ResolveChannelMappingAndRestrict(ctx, &group.ID, requestModel)
			channelModel = mapping.MappedModel
		}
		selectionModel := requestModel
		if api.name == "gemini" {
			selectionModel = channelModel
		}
		if s.checkChannelPricingRestriction(requestCtx, &group.ID, selectionModel) {
			continue
		}
		checkUpstreamRestriction := s.needsUpstreamChannelRestrictionCheck(requestCtx, &group.ID)
		profiles := make(map[ImageStudioModelProfile]struct{})
		for i := range accounts {
			account := &accounts[i]
			mixed := platform == PlatformGemini && account.Platform == PlatformAntigravity && account.IsMixedSchedulingEnabled()
			if account.Platform != platform && !mixed {
				continue
			}
			if !s.isModelSupportedByAccountWithContext(requestCtx, account, selectionModel) {
				continue
			}
			if checkUpstreamRestriction && s.isUpstreamModelRestrictedByChannel(requestCtx, group.ID, account, selectionModel) {
				continue
			}
			upstream := account.GetMappedModel(channelModel)
			switch platform {
			case PlatformOpenAI:
				if validateCompatibleImagesModel(requestModel) != nil || validateCompatibleImagesModel(channelModel) != nil {
					continue
				}
				if !account.SupportsOpenAIImageCapability(OpenAIImagesCapabilityNative) {
					continue
				}
				if account.Type == AccountTypeAPIKey && validateCompatibleImagesModel(upstream) == nil ||
					account.IsOpenAIOAuthLike() && validateOpenAIImagesModel(requestModel) == nil &&
						validateOpenAIImagesModel(channelModel) == nil && validateOpenAIImagesModel(upstream) == nil {
					profiles[imageStudioAccountProfile(account, upstream)] = struct{}{}
				}
			case PlatformGrok:
				if account.SupportsOpenAIEndpointCapability(OpenAIEndpointCapabilityGrokMediaGeneration) && isGrokImageGenerationModel(requestModel) {
					profiles[imageStudioAccountProfile(account, upstream)] = struct{}{}
				}
			case PlatformGemini, PlatformAntigravity:
				if account.Platform == PlatformAntigravity {
					upstream = mapAntigravityModel(account, channelModel)
				} else if account.Type == AccountTypeOAuth {
					upstream = channelModel // Gemini OAuth native forwarding does not apply account mappings.
				}
				if (account.Type == AccountTypeAPIKey || account.Type == AccountTypeServiceAccount || account.Type == AccountTypeOAuth) &&
					imageStudioGoogleImageModel(upstream) {
					profiles[imageStudioAccountProfile(account, upstream)] = struct{}{}
				}
			}
		}
		if len(profiles) > 0 {
			return ImageStudioModel{ID: public, API: api.name, Profiles: sortedImageStudioProfiles(profiles)}, nil
		}
	}
	return ImageStudioModel{}, nil
}
