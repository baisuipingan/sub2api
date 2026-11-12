package service

import (
	"context"
	"errors"
	"sort"
	"strings"

	"github.com/Wei-Shaw/sub2api/internal/domain"
)

// DesktopAgentModelCatalog reads configured public IDs only. It never discovers
// upstream models, creates credentials, or infers executable model capabilities.
type DesktopAgentModelCatalog interface {
	ModelsForGroup(context.Context, *Group) ([]string, error)
}

type desktopAgentChannelReader interface {
	GetChannelForGroup(context.Context, int64) (*Channel, error)
}

type desktopAgentModelCatalog struct {
	accounts AccountRepository
	routes   CompositeModelRouteRepository
	channels desktopAgentChannelReader
}

func NewDesktopAgentModelCatalog(accounts AccountRepository, routes CompositeModelRouteRepository, channels ...desktopAgentChannelReader) DesktopAgentModelCatalog {
	catalog := &desktopAgentModelCatalog{accounts: accounts, routes: routes}
	if len(channels) > 0 {
		catalog.channels = channels[0]
	}
	return catalog
}

func (s *desktopAgentModelCatalog) ModelsForGroup(ctx context.Context, group *Group) ([]string, error) {
	if s == nil || s.accounts == nil || group == nil || group.ID <= 0 {
		return nil, errors.New("desktop model catalog unavailable")
	}
	platforms := []string{PlatformOpenAI}
	if group.Platform == PlatformComposite {
		platforms = domain.ConcretePlatformIDs()
	}
	// 配置目录不能随限流、过载等临时健康状态变动，也不能查询失败后补全平台默认列表。
	accounts, err := s.accounts.ListModelAvailabilityCandidates(ctx, &group.ID, platforms, false)
	if err != nil {
		return nil, err
	}
	var routes []CompositeModelRoute
	if group.Platform == PlatformComposite {
		if s.routes == nil {
			return nil, errors.New("desktop composite model catalog unavailable")
		}
		routes, err = s.routes.ListByGroup(ctx, group.ID, false)
		if err != nil {
			return nil, err
		}
		enabled := make([]CompositeModelRoute, 0, len(routes))
		for _, route := range routes {
			if route.Enabled && route.GroupID == group.ID {
				enabled = append(enabled, route)
			}
		}
		routes = enabled
	}
	// Read channel configuration once and propagate reported read errors instead
	// of using mapping helpers that intentionally fail open. Existing channel
	// cache semantics still apply to this configuration snapshot.
	var channel *Channel
	var channelSnapshot *channelCache
	if s.channels != nil {
		channel, err = s.channels.GetChannelForGroup(ctx, group.ID)
		if err != nil {
			return nil, err
		}
		if channel != nil {
			channelSnapshot = populateChannelCache([]Channel{*channel}, map[int64]string{group.ID: group.Platform})
			channel = channelSnapshot.channelByGroupID[group.ID]
		}
	}
	candidates := make(map[string]struct{})
	add := func(model string) {
		if desktopAgentString(model, 128) && !strings.ContainsAny(model, " *?\\") {
			candidates[model] = struct{}{}
		}
	}
	if channel != nil {
		for _, platform := range matchingPlatforms(group.Platform) {
			for public, target := range channel.ModelMapping[platform] {
				if strings.TrimSpace(target) != "" {
					add(public)
				}
			}
		}
	}
	for i := range accounts {
		account := &accounts[i]
		if group.Platform != PlatformComposite && account.Platform != group.Platform {
			continue
		}
		for public, target := range stringMappingFromRaw(account.Credentials["model_mapping"]) {
			if strings.TrimSpace(target) != "" {
				add(public)
			}
		}
		// 已持久快照只提供有限候选；其上游映射目标仍须通过下面的公开请求路由校验。
		if snapshot := account.GetUpstreamModelMetadataSnapshot(); snapshot != nil {
			for model := range snapshot.Models {
				add(model)
			}
		}
	}
	for _, route := range routes {
		if route.Enabled && normalizeCompositeRouteMatchType(route.MatchType) == CompositeRouteMatchExact &&
			(normalizeCompositeRouteEndpoint(route.Endpoint) == CompositeRouteEndpointResponses || normalizeCompositeRouteEndpoint(route.Endpoint) == CompositeRouteEndpointAny) {
			add(route.PublicModel)
		}
	}
	// 通配规则不是具体 ID；显式白名单可提供具体名称，但不能绕过实际账号/路由校验。
	if group.ModelAllowlistEnabled() {
		for _, model := range group.ModelAllowlist.Models {
			add(model)
		}
	}
	models := make([]string, 0, len(candidates))
	// These scheduler predicates inspect only persistent account/model policy;
	// using them does not select an account or issue an upstream request.
	modelSupport := &GatewayService{}
	for model := range candidates {
		platform, target := group.Platform, model
		requestCtx := ctx
		if group.Platform == PlatformComposite {
			decision := desktopAgentCompositeModelTarget(model, accounts, routes)
			if !decision.Matched || decision.TargetPlatform == PlatformTypeSafe {
				continue
			}
			platform, target = decision.TargetPlatform, decision.UpstreamModel
			requestCtx = WithCompositeRouteDecision(ctx, decision)
		}
		if isCodexDedicatedMediaModel(target) {
			continue
		}
		forwardTarget := target
		var channelPolicy *channelLookup
		if channel != nil {
			channelPolicy = &channelLookup{cache: channelSnapshot, channel: channel, platform: channelLookupPlatform(requestCtx, group.Platform)}
			mapping := resolveMapping(channelPolicy, group.ID, target)
			forwardTarget = mapping.MappedModel
		}
		if strings.TrimSpace(forwardTarget) == "" || isCodexDedicatedMediaModel(forwardTarget) {
			continue
		}
		// OpenAI-compatible Responses handlers select against the channel-mapped
		// model. Native handlers select first, then apply the channel to the body.
		usesOpenAIGateway := domain.UsesOpenAIGateway(platform)
		if usesOpenAIGateway {
			target = forwardTarget
		}
		if channelPolicy != nil {
			// Match the actual scheduler's precheck input. OpenAI handlers have
			// already rewritten the request before entering that scheduler.
			mapping := resolveMapping(channelPolicy, group.ID, target)
			billingModel := billingModelForRestriction(mapping.BillingModelSource, target, mapping.MappedModel)
			if billingModel != "" && checkRestricted(channelPolicy, group.ID, billingModel) {
				continue
			}
		}
		useMixed := platform == PlatformAnthropic || platform == PlatformGemini
		for i := range accounts {
			account := &accounts[i]
			if !modelSupport.isAccountAllowedForPlatform(account, platform, useMixed) ||
				(group.RequirePrivacySet && !account.IsPrivacySet()) ||
				!modelSupport.isModelSupportedByAccountWithContext(requestCtx, account, target) {
				continue
			}
			mapped := resolveAccountUpstreamModel(account, forwardTarget)
			pricingTarget := resolveAccountUpstreamModel(account, target)
			if usesOpenAIGateway {
				mapped = resolveOpenAIAccountUpstreamModelForRequest(account, forwardTarget, false)
				pricingTarget = mapped
			}
			if strings.TrimSpace(mapped) == "" || isCodexDedicatedMediaModel(mapped) {
				continue
			}
			if channelPolicy != nil && channel.BillingModelSource == BillingModelSourceUpstream && checkRestricted(channelPolicy, group.ID, pricingTarget) {
				continue
			}
			models = append(models, model)
			break
		}
	}
	sort.Strings(models)
	validIDs := make(map[string]struct{}, len(models))
	for _, model := range models {
		validIDs[model] = struct{}{}
	}
	filtered := FilterCodexModelIDsForGroup(models, group)
	result := make([]string, 0, len(filtered))
	for _, listed := range filtered {
		// 通用白名单输出沿用条目的大小写，但映射和聚合路由区分大小写。
		// 目录只返回已经验证可路由的原始 ID，避免将可调用别名改成另一个请求。
		if _, ok := validIDs[listed]; ok {
			result = append(result, listed)
			continue
		}
		for _, model := range models {
			if strings.EqualFold(listed, model) {
				result = append(result, model)
				break
			}
		}
	}
	return result, nil
}

// 与CompositeRouteResolver一致：Responses显式路由优先，其次持久化精确映射
// 的唯一平台归属，最后才允许内置检测器；不将运行时补齐的默认映射当成归属声明。
func desktopAgentCompositeModelTarget(model string, accounts []Account, routes []CompositeModelRoute) CompositeRouteDecision {
	decision := CompositeRouteDecision{PublicModel: model, UpstreamModel: model, Endpoint: CompositeRouteEndpointResponses}
	if route, matched := matchCompositeRoute(routes, model, CompositeRouteEndpointResponses); matched {
		decision.UpstreamModel = strings.TrimSpace(route.UpstreamModel)
		if decision.UpstreamModel == "" {
			decision.UpstreamModel = model
		}
		decision.TargetPlatform, decision.Source, decision.Matched = route.TargetPlatform, CompositeRouteSourceExplicit, true
		return decision
	}
	claims := make(map[string]struct{})
	for _, account := range accounts {
		if isConcreteRequestPlatform(account.Platform) && explicitModelMappingClaims(account, model) {
			claims[account.Platform] = struct{}{}
		}
	}
	if len(claims) > 1 {
		return decision
	}
	for platform := range claims {
		decision.TargetPlatform, decision.Source, decision.Matched = platform, CompositeRouteSourceAccount, true
		return decision
	}
	decision.TargetPlatform, decision.Matched = DetectModelPlatform(model)
	decision.Source = CompositeRouteSourceDetector
	return decision
}
