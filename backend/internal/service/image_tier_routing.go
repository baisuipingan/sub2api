package service

import "strings"

// fork: image-tier-routing
//
// ── 生图模型尺寸档位路由 ────────────────────────────────────────────────
//
// 背景：外部只暴露一个模型名（如 gpt-image-2），但不同上游供应商对同一模型
// 的 1K / 2K / 4K 可能拆成不同产品名单独售卖（gpt-image-2-4k 等）。
// 客户不应感知这些差异，只传 size 参数即可。
//
// 做法：请求进入时把「模型名 + size」在内部翻译成带档位的形式
//
//	gpt-image-2 + size=4096x4096  →  gpt-image-2:4k
//
// 之后的账号筛选与模型映射全部沿用既有机制：
//   - 账号配了 "gpt-image-2:4k" → 精确命中，按配置的上游名转发
//   - 账号没配             → 回落到基础名 "gpt-image-2"，默认全档位可用
//
// 为什么用「查询期回落」而不是「往映射表里补默认项」：
// model_mapping 是所有平台共用的，改它会影响 Anthropic / Gemini / Grok 等
// 全部链路。而档位展开只该在生图请求上发生，属于查找期行为。

// imageTierSeparator 是档位段与模型名之间的分隔符。
//
// 选冒号的原因：已确认 model_mapping 的 key 从解析到查找全链路都不做字符
// 清洗，冒号可安全使用；且它不会出现在现有任何模型名里，无歧义。
const imageTierSeparator = ":"

// ApplyImageTierToModel 按请求尺寸把生图模型名改写为带档位的形式。
//
// 返回值语义：
//   - 返回非空字符串 → 调用方应使用它作为 routingModel
//   - 返回空字符串   → 不是需要做档位路由的请求，调用方保持原模型名不变
//
// 只有同时满足以下条件才会改写，任何一条不满足都原样返回：
//  1. size 能解析出档位（auto / 空 / 无法解析 → 不改写）
//  2. 模型名属于 GPT 生图模型族（文本模型完全不受影响）
//
// size 到档位的判定复用 ClassifyImageBillingTier，与计费口径保持完全一致，
// 避免出现「路由按 4K 选账号、计费按 2K 收钱」的错位。
func ApplyImageTierToModel(model, size string) string {
	base := strings.TrimSpace(model)
	if base == "" {
		return ""
	}

	// 已经是带档位的形式就不用再处理（幂等）
	if _, _, ok := splitImageTierModel(base); ok {
		return base
	}

	if !IsImageGenerationModelFamily(base) {
		return ""
	}

	tier, ok := ClassifyImageBillingTier(size)
	if !ok {
		return ""
	}

	return base + imageTierSeparator + strings.ToLower(tier)
}

// IsImageGenerationModelFamily 判断模型是否属于「生图模型」。
//
// 覆盖所有已知的生图模型族，而不是只认 gpt-image-：
//   - GPT 系（gpt-image-2 / gpt-image-2.5 ...）
//   - Grok 系（grok-imagine-image ...）
//   - Gemini 系（NanoBanana 对应 gemini-3-pro-image / gemini-2.5-flash-image 等）
//
// 新增生图模型时只要它的命名落在既有族里，无需改这里。
// 如果将来接入命名完全不同的族（如某家叫 banana-v2），在这里补一个前缀判断即可，
// 档位路由的其余逻辑（改写、映射解析）都无需改动。
func IsImageGenerationModelFamily(model string) bool {
	trimmed := strings.ToLower(strings.TrimSpace(model))
	if trimmed == "" {
		return false
	}
	// 去掉 Gemini 的 models/ 前缀
	trimmed = strings.TrimPrefix(trimmed, "models/")
	return IsGPTImageGenerationModel(trimmed) ||
		isGrokImageGenerationModel(trimmed) ||
		isImageGenerationModel(trimmed)
}

// splitImageTierModel 把 "gpt-image-2:4k" 拆成 ("gpt-image-2", "4k", true)。
//
// 不是带档位的形式时返回 ok=false，调用方应完全走原有逻辑。
// 只识别 GPT 生图模型族，避免误伤其它带冒号的模型名。
func splitImageTierModel(model string) (base string, tier string, ok bool) {
	trimmed := strings.TrimSpace(model)
	if trimmed == "" {
		return "", "", false
	}

	idx := strings.LastIndex(trimmed, imageTierSeparator)
	if idx <= 0 || idx == len(trimmed)-1 {
		return "", "", false
	}

	base = trimmed[:idx]
	tier = strings.ToLower(trimmed[idx+1:])

	if !IsImageGenerationModelFamily(base) {
		return "", "", false
	}

	// 只接受真实存在的档位，防止把 "gpt-image-2:anything" 误当成档位
	switch tier {
	case strings.ToLower(ImageBillingSize1K),
		strings.ToLower(ImageBillingSize2K),
		strings.ToLower(ImageBillingSize4K):
		return base, tier, true
	default:
		return "", "", false
	}
}

// ResolveImageTierMapping 在账号的模型映射里查找请求模型应使用的上游名。
//
// 规则（只对带档位的生图模型生效）：
//  1. 精确命中 "gpt-image-2:4k" → 用配置的上游名
//  2. 没有精确项              → 回落到基础名 "gpt-image-2"
//  3. 基础名也没有            → 不支持，返回 false
//
// 注意：带档位时**不参与通配符匹配**。否则一个 "gpt-image-2*" 的通配符会让
// 所有账号都「支持」任意档位，失去区分意义。
func ResolveImageTierMapping(mapping map[string]string, requestedModel string) (mapped string, matched bool) {
	base, _, ok := splitImageTierModel(requestedModel)
	if !ok {
		return "", false
	}
	if len(mapping) == 0 {
		return "", false
	}

	// 1. 精确命中档位 key
	if value, exists := mapping[requestedModel]; exists {
		if trimmed := strings.TrimSpace(value); trimmed != "" {
			return trimmed, true
		}
	}

	// 2. 回落到基础名
	if value, exists := mapping[base]; exists {
		if trimmed := strings.TrimSpace(value); trimmed != "" {
			return trimmed, true
		}
	}

	return "", false
}

// SupportsImageTierModel 判断账号是否接受带档位的生图请求。
//
// 与 ResolveImageTierMapping 同源：能解析出上游名就算支持。
func SupportsImageTierModel(mapping map[string]string, requestedModel string) bool {
	_, ok := ResolveImageTierMapping(mapping, requestedModel)
	return ok
}
