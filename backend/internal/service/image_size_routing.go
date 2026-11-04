package service

import (
	"context"
	"strings"
)

// fork: image-size-routing
//
// ── 生图尺寸能力路由 ────────────────────────────────────────────────────
//
// 要解决的问题：对外只暴露一个模型名（gpt-image-2），客户只传 size；
// 但各供应商把 2K / 4K 拆成不同产品名单独售卖，需要按尺寸选对上游。
//
// 为什么不复用 model_mapping：
// model_mapping 的 key 会被全站至少 4 处代码当作「可售模型清单」遍历
// （admin_group / gateway_service / batch_image_public / gemini_messages_compat）。
// 往 key 里编码任何标记（如 gpt-image-2:4k）都会污染前端模型列表，
// 上游模型名也可能被当模型名回传，再被二次加工成非法名（双后缀 400）。
//
// 因此尺寸能力放在 account.Extra.image_size_capabilities，与 model_mapping 物理隔离：
//
//	{
//	  "gpt-image-2": {
//	    "4k": "gpt-image-2-4K",
//	    "2k": "gpt-image-2-2K"
//	  }
//	}
//
// 语义：
//   - 有该档位 key   → 该账号支持这个尺寸，转发时用其 value 作为上游模型名
//   - 没有该档位 key → 该账号不支持这个尺寸，调度阶段直接跳过
//   - 整个 capability 为空 → 完全不介入，行为和没装这个功能一样
//
// 这样「不支持 4K」是配置的自然结果，不需要任何特殊标记值。

// ImageSizeCapabilitiesExtraKey 是 account.Extra 中存放尺寸能力表的键名。
const ImageSizeCapabilitiesExtraKey = "image_size_capabilities"

// ImageSizeCapability 描述一个模型在某档尺寸下的上游映射。
type ImageSizeCapability map[string]string

// imageSizeCapabilityTable 解析账号的尺寸能力表。
//
// 返回 nil 表示该账号没有配置任何尺寸能力，调用方应完全走原有逻辑。
func imageSizeCapabilityTable(account *Account) map[string]ImageSizeCapability {
	if account == nil || account.Extra == nil {
		return nil
	}
	raw, ok := account.Extra[ImageSizeCapabilitiesExtraKey].(map[string]any)
	if !ok || len(raw) == 0 {
		return nil
	}

	table := make(map[string]ImageSizeCapability, len(raw))
	for model, value := range raw {
		modelName := strings.TrimSpace(model)
		if modelName == "" {
			continue
		}
		capability, ok := normalizeImageSizeCapability(value)
		if !ok || len(capability) == 0 {
			continue
		}
		table[modelName] = capability
	}
	if len(table) == 0 {
		return nil
	}
	return table
}

// normalizeImageSizeCapability 把配置值规整为「档位 → 上游模型名」。
//
// 兼容两种写法：
//
//	{"4k": "gpt-image-2-4K"}                       ← 直接映射上游名
//	[{"tier": "4k", "upstream": "gpt-image-2-4K"}] ← 前端表单更易生成的数组
func normalizeImageSizeCapability(value any) (ImageSizeCapability, bool) {
	switch typed := value.(type) {
	case map[string]any:
		out := make(ImageSizeCapability, len(typed))
		for tier, upstream := range typed {
			normalizedTier, ok := normalizeImageSizeTier(tier)
			if !ok {
				continue
			}
			name, ok := upstream.(string)
			if !ok {
				continue
			}
			if trimmed := strings.TrimSpace(name); trimmed != "" {
				out[normalizedTier] = trimmed
			}
		}
		return out, len(out) > 0

	case []any:
		out := make(ImageSizeCapability, len(typed))
		for _, item := range typed {
			entry, ok := item.(map[string]any)
			if !ok {
				continue
			}
			tier, _ := entry["tier"].(string)
			upstream, _ := entry["upstream"].(string)
			normalizedTier, ok := normalizeImageSizeTier(tier)
			if !ok {
				continue
			}
			if trimmed := strings.TrimSpace(upstream); trimmed != "" {
				out[normalizedTier] = trimmed
			}
		}
		return out, len(out) > 0

	default:
		return nil, false
	}
}

// normalizeImageSizeTier 把各种档位写法归一为 1k / 2k / 4k。
func normalizeImageSizeTier(tier string) (string, bool) {
	switch strings.ToLower(strings.TrimSpace(tier)) {
	case "1k":
		return "1k", true
	case "2k":
		return "2k", true
	case "4k":
		return "4k", true
	default:
		return "", false
	}
}

// ImageSizeTierFor 判断请求尺寸属于哪一档。
//
// 复用 ClassifyImageBillingTier，保证与计费口径完全一致：
// 不会出现「路由按 4K 选账号、计费按 2K 收钱」的错位。
// 无法判档（auto / 空 / 非法）时返回 false，调用方应保持原有行为。
func ImageSizeTierFor(size string) (string, bool) {
	tier, ok := ClassifyImageBillingTier(size)
	if !ok {
		return "", false
	}
	return strings.ToLower(tier), true
}

// ResolveImageSizeUpstream 计算某账号在该尺寸下的上游模型名。
//
// 返回值语义：
//   - supported=false → 该账号不能接这个尺寸，调度应跳过
//   - supported=true  → 使用 upstream（若为空则沿用 model_mapping 的结果）
//
// 注意：账号没配能力表时返回 supported=true、upstream 为空，
// 表示「不介入」，由调用方按原逻辑处理。
func ResolveImageSizeUpstream(account *Account, requestedModel, size string) (upstream string, supported bool) {
	table := imageSizeCapabilityTable(account)
	if table == nil {
		return "", true
	}

	capability, ok := table[strings.TrimSpace(requestedModel)]
	if !ok {
		// 账号配了能力表，但没提这个模型 → 不介入该模型，交给原逻辑
		return "", true
	}

	tier, ok := ImageSizeTierFor(size)
	if !ok {
		// 尺寸无法判档（如 auto），不介入
		return "", true
	}

	upstream, exists := capability[tier]
	if !exists || strings.TrimSpace(upstream) == "" {
		// 配了表但没有这一档 = 明确不支持该尺寸
		return "", false
	}

	return strings.TrimSpace(upstream), true
}

// ── context 传递 ───────────────────────────────────────────────────────
//
// 调度阶段需要请求的 size 才能判断档位，而选账号的接口签名被多处调用，
// 直接加参数会扩散改动面。这里沿用项目已有的 ctxkey 传递方式。
//
// 注意与旧方案的区别：这里只传 size（一个纯数据），不改模型名，
// 因此不可能污染 model_mapping，也不会产生双后缀。

type imageSizeContextKey struct{}

// WithImageRequestSize 把请求的 size 写入 context，供调度阶段做档位判断。
func WithImageRequestSize(ctx context.Context, size string) context.Context {
	if ctx == nil {
		return ctx
	}
	trimmed := strings.TrimSpace(size)
	if trimmed == "" {
		return ctx
	}
	return context.WithValue(ctx, imageSizeContextKey{}, trimmed)
}

// ImageRequestSizeFromContext 读取请求的 size。
func ImageRequestSizeFromContext(ctx context.Context) (string, bool) {
	if ctx == nil {
		return "", false
	}
	size, ok := ctx.Value(imageSizeContextKey{}).(string)
	if !ok || size == "" {
		return "", false
	}
	return size, true
}

// imageSizeUpstreamContextKey 用于把调度阶段算出的上游模型名传给转发层。
type imageSizeUpstreamContextKey struct{}

// WithImageSizeUpstream 记录某账号按尺寸路由得到的上游模型名。
func WithImageSizeUpstream(ctx context.Context, upstream string) context.Context {
	if ctx == nil || strings.TrimSpace(upstream) == "" {
		return ctx
	}
	return context.WithValue(ctx, imageSizeUpstreamContextKey{}, strings.TrimSpace(upstream))
}

// ImageSizeUpstreamFromContext 读取尺寸路由得到的上游模型名。
func ImageSizeUpstreamFromContext(ctx context.Context) (string, bool) {
	if ctx == nil {
		return "", false
	}
	upstream, ok := ctx.Value(imageSizeUpstreamContextKey{}).(string)
	if !ok || upstream == "" {
		return "", false
	}
	return upstream, true
}
