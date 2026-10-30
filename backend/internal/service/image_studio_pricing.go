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

		tiers := make(map[string]float64, 3)
		for _, tier := range []string{ImageBillingSize1K, ImageBillingSize2K, ImageBillingSize4K} {
			tiers[tier] = s.unitCost(ctx, model, tier, group, cfg, multiplier)
		}
		table[model] = tiers
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

// unitCost 计算单张实际成本，取值优先级与网关扣费完全一致：
//
//	① 分组模型级固定价（group.model_pricing，Source=Group，按次/按图片模式）
//	② 渠道定价覆盖（Source=Channel，按次/按图片模式）
//	③ 分组图片档位价 → 模型默认价，再乘倍率
//
// 前两级必须走 CalculateCostUnified：分组模型级定价（例如给
// gpt-image-2.5 单独配 0.198/张）只在那里生效。
// 早先这里直接调 CalculateImageCost，会跳过它，导致页面显示默认兜底价
// （0.0268）而实际按 0.0396 扣费。
func (s *ImageStudioPricingService) unitCost(
	ctx context.Context,
	model string,
	tier string,
	group *Group,
	cfg *ImagePriceConfig,
	multiplier float64,
) float64 {
	if s.billing == nil {
		return 0
	}

	if s.resolver != nil {
		groupID := group.ID
		resolved := s.resolver.Resolve(ctx, PricingInput{
			Model:   model,
			GroupID: &groupID,
			Group:   group,
		})

		imageMode := resolved != nil &&
			(resolved.Mode == BillingModeImage || resolved.Mode == BillingModePerRequest) &&
			(resolved.Source == PricingSourceGroup || resolved.Source == PricingSourceChannel)

		if imageMode {
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
			if err == nil && breakdown != nil {
				return breakdown.ActualCost
			}
		}
	}

	breakdown := s.billing.CalculateImageCost(model, tier, 1, cfg, multiplier)
	if breakdown == nil {
		return 0
	}
	return breakdown.ActualCost
}
