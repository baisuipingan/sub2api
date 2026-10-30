package service

import (
	"context"
	"strings"
)

// ImageStudioPricingService 为生图工作台提供「每张实际成本」预估。
//
// 为什么要单独做：工作台前端原来用一张写死的常量表（0.0268 / 0.0402 / 0.0536），
// 那组数字只是某一套配置下 默认单价 × 尺寸系数 × 0.2 倍率 凑出来的巧合值。
// 一旦管理员改分组倍率、配自定义单价，或换用计费规则不同的模型，页面显示就会
// 与真实扣费脱节，而且不会报错 —— 用户会以为被多扣钱。
//
// 这里复用 BillingService 的计价函数与网关同一套倍率解析规则，保证「显示 = 实扣」。
type ImageStudioPricingService struct {
	billing           *BillingService
	resolver          *ModelPricingResolver
	userGroupRateRepo UserGroupRateRepository
}

// NewImageStudioPricingService 构造生图工作台定价服务。
func NewImageStudioPricingService(
	billing *BillingService,
	resolver *ModelPricingResolver,
	userGroupRateRepo UserGroupRateRepository,
) *ImageStudioPricingService {
	return &ImageStudioPricingService{
		billing:           billing,
		resolver:          resolver,
		userGroupRateRepo: userGroupRateRepo,
	}
}

// ImageStudioPriceTable 是 map[计费模型][尺寸档位]每张实际成本（已含倍率）。
type ImageStudioPriceTable map[string]map[string]float64

// EstimateUnitPrices 计算指定模型在各尺寸档位下的单张实际成本。
//
// 只返回「每张、已乘倍率」的价格，前端乘以张数即可，与网关扣费口径一致。
func (s *ImageStudioPricingService) EstimateUnitPrices(
	ctx context.Context,
	userID int64,
	group *Group,
	models []string,
) ImageStudioPriceTable {
	if s == nil || s.billing == nil || group == nil || len(models) == 0 {
		return ImageStudioPriceTable{}
	}

	cfg := &ImagePriceConfig{
		Price1K: group.ImagePrice1K,
		Price2K: group.ImagePrice2K,
		Price4K: group.ImagePrice4K,
	}
	multiplier := s.resolveImageRateMultiplier(ctx, userID, group)

	table := make(ImageStudioPriceTable, len(models))
	for _, model := range models {
		model = strings.TrimSpace(model)
		if model == "" {
			continue
		}

		// 定价解析只依赖模型，与尺寸档位无关 —— 每个模型解析一次即可，
		// 否则同一个模型要重复查三次渠道/分组定价。
		resolved := s.resolvePricing(ctx, model, group)

		tiers := make(map[string]float64, 3)
		for _, tier := range []string{ImageBillingSize1K, ImageBillingSize2K, ImageBillingSize4K} {
			// 算不出就让该档位缺省，前端会回退到内置估算；
			// 写 0 会让界面显示「约 0.0000」，比不显示更误导。
			if cost, ok := s.unitCost(ctx, model, tier, group, cfg, multiplier, resolved); ok {
				tiers[tier] = cost
			}
		}
		if len(tiers) > 0 {
			table[model] = tiers
		}
	}
	return table
}

// resolveImageRateMultiplier 复现网关的生图倍率规则：
//
//	基础倍率 = 用户在该分组的专属倍率（无则用分组默认）
//	若分组「生图倍率独立」→ 改用分组的 image_rate_multiplier
//
// 与 resolveImageRateMultiplier(apiKey, base) 保持同一语义，只是换了入参来源。
func (s *ImageStudioPricingService) resolveImageRateMultiplier(
	ctx context.Context,
	userID int64,
	group *Group,
) float64 {
	base := group.RateMultiplier

	if s.userGroupRateRepo != nil && userID > 0 && group.ID > 0 {
		if override, err := s.userGroupRateRepo.GetByUserAndGroup(ctx, userID, group.ID); err == nil && override != nil {
			base = *override
		}
	}

	if group.ImageRateIndependent {
		// 负值按 0 处理，与保存时的约定一致，避免按 1x 误算
		if group.ImageRateMultiplier < 0 {
			return 0
		}
		return group.ImageRateMultiplier
	}

	if base < 0 {
		return 0
	}
	return base
}

// resolvePricing 解析模型在分组下的定价来源（分组模型级 / 渠道 / 无）。
func (s *ImageStudioPricingService) resolvePricing(
	ctx context.Context,
	model string,
	group *Group,
) *ResolvedPricing {
	if s.resolver == nil || group == nil {
		return nil
	}
	groupID := group.ID
	return s.resolver.Resolve(ctx, PricingInput{
		Model:   model,
		GroupID: &groupID,
		Group:   group,
	})
}

// imagePriceSource 表示单张价格的取值来源，顺序即优先级。
type imagePriceSource int

const (
	imagePriceFromModelDefault imagePriceSource = iota // ④ 模型默认价
	imagePriceFromGroupTier                            // ② 分组图片档位价
	imagePriceFromChannel                              // ③ 渠道定价
	imagePriceFromGroupModel                           // ① 分组模型级固定价
)

// chooseImagePriceSource 是「取哪一档价格」的唯一决策点。
//
// 顺序必须与网关 calculateOpenAIImageCost 一致，抽成纯函数是为了能用测试锁住：
// 一旦有人调换顺序，测试立刻失败，而不是等用户发现页面价和账单不一致。
//
// 关键点：分组档位价（②）优先于渠道价（③）—— 分组同时配了两者时，
// 网关会用分组档位价扣费。
func chooseImagePriceSource(resolved *ResolvedPricing, hasGroupTierPrice bool) imagePriceSource {
	imageMode := resolved != nil &&
		(resolved.Mode == BillingModeImage || resolved.Mode == BillingModePerRequest)

	if imageMode && resolved.Source == PricingSourceGroup {
		return imagePriceFromGroupModel
	}
	if hasGroupTierPrice {
		return imagePriceFromGroupTier
	}
	if imageMode && resolved.Source == PricingSourceChannel {
		return imagePriceFromChannel
	}
	return imagePriceFromModelDefault
}

// unitCost 计算单张实际成本。
//
// 这里的取值顺序**必须与网关 calculateOpenAIImageCost 逐条对齐**，
// 否则「页面预估」和「实际扣费」会分叉：
//
//	① 分组模型级固定价（Source=Group，按次/按图片）
//	② 分组图片档位价（image_price_1k/2k/4k）
//	③ 渠道定价（Source=Channel，按次/按图片）
//	④ 模型默认价
//
// 特别注意 ② 必须在 ③ 之前：分组同时配了档位价和渠道价时，
// 网关优先用分组档位价，顺序反了就会“显示渠道价、扣分组价”。
//
// 返回 ok=false 表示取不到可信价格，调用方应省略该档位而不是填 0。
func (s *ImageStudioPricingService) unitCost(
	ctx context.Context,
	model string,
	tier string,
	group *Group,
	cfg *ImagePriceConfig,
	multiplier float64,
	resolved *ResolvedPricing,
) (float64, bool) {
	if s.billing == nil || group == nil {
		return 0, false
	}

	switch chooseImagePriceSource(resolved, group.GetImagePrice(tier) != nil) {
	// ① 分组模型级固定价
	case imagePriceFromGroupModel, imagePriceFromChannel:
		if cost, ok := s.unifiedUnitCost(ctx, model, tier, group, multiplier, resolved); ok {
			return cost, true
		}
		// 统一入口失败时，仍退回默认价（与网关 err 后兜底的行为一致）
	}

	// ② 分组档位价与 ④ 默认价都走 CalculateImageCost
	if breakdown := s.billing.CalculateImageCost(model, tier, 1, cfg, multiplier); breakdown != nil {
		return breakdown.ActualCost, true
	}
	return 0, false
}

// unifiedUnitCost 走与网关完全相同的统一计费入口（按图片/按次模式）。
func (s *ImageStudioPricingService) unifiedUnitCost(
	ctx context.Context,
	model string,
	tier string,
	group *Group,
	multiplier float64,
	resolved *ResolvedPricing,
) (float64, bool) {
	groupID := group.ID
	breakdown, err := s.billing.CalculateCostUnified(CostInput{
		Ctx:            ctx,
		Model:          model,
		GroupID:        &groupID,
		Group:          group,
		RequestCount:   1,
		SizeTier:       tier,
		RateMultiplier: multiplier,
		Resolver:       s.resolver,
		Resolved:       resolved,
	})
	if err != nil || breakdown == nil {
		return 0, false
	}
	return breakdown.ActualCost, true
}
