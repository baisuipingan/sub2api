package service

import "testing"

// 本测试锁住「页面预估」与「网关实扣」的取值顺序一致。
//
// 网关 calculateOpenAIImageCost 的顺序是：
//	cost := func() {
//		if resolved.Source == Group && (按次|按图片) { return Unified }
//		if 分组配了该档位图价 { return CalculateImageCost }
//		if resolved.Source == Channel && (按次|按图片) { return Unified }
//		return CalculateImageCost
//	}
//
// 顺序一旦被调换（最常见：把渠道价提到分组档位价之前），
// 就会出现「页面显示渠道价、实际扣分组价」——用户以为便宜，账单更贵。
// 这类问题不会报错，只能靠测试拦。

func resolvedPricing(source string, mode BillingMode) *ResolvedPricing {
	return &ResolvedPricing{Source: source, Mode: mode}
}

func TestChooseImagePriceSource_GroupModelPricingWins(t *testing.T) {
	got := chooseImagePriceSource(
		resolvedPricing(PricingSourceGroup, BillingModePerRequest),
		true, // 即使分组也配了档位价
	)
	if got != imagePriceFromGroupModel {
		t.Fatalf("分组模型级固定价应最优先，实际=%v", got)
	}
}

func TestChooseImagePriceSource_GroupTierBeatsChannel(t *testing.T) {
	// 这是最容易写错的一条：分组档位价必须优先于渠道价
	for _, mode := range []BillingMode{BillingModeImage, BillingModePerRequest} {
		got := chooseImagePriceSource(
			resolvedPricing(PricingSourceChannel, mode),
			true,
		)
		if got != imagePriceFromGroupTier {
			t.Fatalf("mode=%s: 分组档位价应优先于渠道价，实际=%v", mode, got)
		}
	}
}

func TestChooseImagePriceSource_ChannelWhenNoGroupTier(t *testing.T) {
	for _, mode := range []BillingMode{BillingModeImage, BillingModePerRequest} {
		got := chooseImagePriceSource(
			resolvedPricing(PricingSourceChannel, mode),
			false,
		)
		if got != imagePriceFromChannel {
			t.Fatalf("mode=%s: 无分组档位价时应走渠道价，实际=%v", mode, got)
		}
	}
}

func TestChooseImagePriceSource_DefaultsWhenNothingConfigured(t *testing.T) {
	if got := chooseImagePriceSource(nil, false); got != imagePriceFromModelDefault {
		t.Fatalf("全无配置应走模型默认价，实际=%v", got)
	}
}

func TestChooseImagePriceSource_TokenModeFallsBackToDefault(t *testing.T) {
	// 渠道配的是 token 计价时，不能按张算价，应落到默认价兜底
	got := chooseImagePriceSource(
		resolvedPricing(PricingSourceChannel, BillingModeToken),
		false,
	)
	if got != imagePriceFromModelDefault {
		t.Fatalf("token 模式不应按图片价取值，实际=%v", got)
	}

	// 但分组配了档位价时，仍按档位价（与网关 apiKeyHasConfiguredImagePrice 一致）
	got = chooseImagePriceSource(
		resolvedPricing(PricingSourceChannel, BillingModeToken),
		true,
	)
	if got != imagePriceFromGroupTier {
		t.Fatalf("token 模式下分组档位价仍应生效，实际=%v", got)
	}
}

func TestChooseImagePriceSource_NilResolvedWithGroupTier(t *testing.T) {
	if got := chooseImagePriceSource(nil, true); got != imagePriceFromGroupTier {
		t.Fatalf("无定价解析结果但配置了档位价时应取档位价，实际=%v", got)
	}
}
