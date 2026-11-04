package service

import (
	"strings"
	"testing"
)

// fork: image-tier-routing
//
// 集成测试：验证「请求改写 → 账号筛选 → 上游名解析」这条链路的端到端行为。
//
// 场景来自真实运营需求：
//   - 供应商 A 融合型：4K 也叫 gpt-image-2
//   - 供应商 B 拆名型：4K 叫 gpt-image-2-4k
//   - 供应商 C 只映射了别的模型，做不了 gpt-image-2
//
// 期望：A、B 都能接 4K 单（各自用自己的上游名），C 被排除。

func newTierTestAccount(t *testing.T, mapping map[string]string) *Account {
	t.Helper()
	credentials := map[string]any{}
	if len(mapping) > 0 {
		raw := make(map[string]any, len(mapping))
		for k, v := range mapping {
			raw[k] = v
		}
		credentials["model_mapping"] = raw
	}
	return &Account{
		Platform:    PlatformOpenAI,
		Credentials: credentials,
	}
}

func TestImageTierRouting_EndToEnd(t *testing.T) {
	// 客户请求：gpt-image-2 + 4K
	const clientModel = "gpt-image-2"
	const clientSize = "4096x4096"

	routingModel := ApplyImageTierToModel(clientModel, clientSize)
	if routingModel != "gpt-image-2:4k" {
		t.Fatalf("改写失败: got %q", routingModel)
	}

	// 供应商 A：融合型，4K 也是 gpt-image-2
	accountA := newTierTestAccount(t, map[string]string{
		"gpt-image-2": "gpt-image-2",
	})
	if !accountA.IsModelSupported(routingModel) {
		t.Fatal("供应商 A（融合型）应支持 4K")
	}
	upstreamA, matchedA := accountA.ResolveMappedModel(routingModel)
	if !matchedA || upstreamA != "gpt-image-2" {
		t.Fatalf("供应商 A 上游名应为 gpt-image-2, got (%q, %v)", upstreamA, matchedA)
	}

	// 供应商 B：拆名型，4K 单独售卖
	accountB := newTierTestAccount(t, map[string]string{
		"gpt-image-2":    "gpt-image-2",
		"gpt-image-2:4k": "gpt-image-2-4k",
	})
	if !accountB.IsModelSupported(routingModel) {
		t.Fatal("供应商 B（拆名型）应支持 4K")
	}
	upstreamB, matchedB := accountB.ResolveMappedModel(routingModel)
	if !matchedB || upstreamB != "gpt-image-2-4k" {
		t.Fatalf("供应商 B 上游名应为 gpt-image-2-4k, got (%q, %v)", upstreamB, matchedB)
	}

	// 供应商 C：没有 gpt-image-2 映射 → 不接这单
	accountC := newTierTestAccount(t, map[string]string{
		"gpt-4o": "gpt-4o",
	})
	if accountC.IsModelSupported(routingModel) {
		t.Fatal("供应商 C 不应支持 gpt-image-2 的 4K 请求")
	}

	// 关键验证：两个可用供应商拿到的上游名不同，这就是本需求的核心价值
	if upstreamA == upstreamB {
		t.Fatal("A/B 两个供应商的上游名应不同，否则没有区分意义")
	}
}

func TestImageTierRouting_OnlyOneTierConfigured(t *testing.T) {
	// 供应商只有 2K 是拆名的，4K 沿用基础名
	account := newTierTestAccount(t, map[string]string{
		"gpt-image-2":    "gpt-image-2-base",
		"gpt-image-2:2k": "gpt-image-2-2k",
	})

	cases := []struct {
		size     string
		wantUp   string
		wantOK   bool
	}{
		{"2048x2048", "gpt-image-2-2k", true},   // 命中专用名
		{"4096x4096", "gpt-image-2-base", true},  // 回落基础名
		{"1024x1024", "gpt-image-2-base", true},  // 回落基础名
	}

	for _, tc := range cases {
		routingModel := ApplyImageTierToModel("gpt-image-2", tc.size)
		if routingModel == "" {
			t.Fatalf("size %s 应能判档", tc.size)
		}
		upstream, ok := account.ResolveMappedModel(routingModel)
		if ok != tc.wantOK || upstream != tc.wantUp {
			t.Fatalf("size=%s: got (%q, %v), want (%q, %v)", tc.size, upstream, ok, tc.wantUp, tc.wantOK)
		}
	}
}

func TestImageTierRouting_TextModelsUnaffected(t *testing.T) {
	// 文本模型：即使 size 看起来像图片尺寸，也不该被改写
	for _, model := range []string{"gpt-4o", "claude-sonnet-4", "gemini-2.5-pro"} {
		if got := ApplyImageTierToModel(model, "4096x4096"); got != "" {
			t.Fatalf("文本模型 %s 不应被改写, got %q", model, got)
		}
	}

	// 原模型名保持不变
	account := newTierTestAccount(t, map[string]string{"gpt-4o": "gpt-4o-custom"})
	upstream, matched := account.ResolveMappedModel("gpt-4o")
	if !matched || upstream != "gpt-4o-custom" {
		t.Fatalf("文本模型映射行为被破坏: got (%q, %v)", upstream, matched)
	}
}

func TestImageTierRouting_AutoSizeNoRewrite(t *testing.T) {
	// size=auto 时不做改写，走原有单模型流程
	if got := ApplyImageTierToModel("gpt-image-2", "auto"); got != "" {
		t.Fatalf("auto 尺寸不应改写, got %q", got)
	}
	account := newTierTestAccount(t, map[string]string{"gpt-image-2": "gpt-image-2"})
	upstream, matched := account.ResolveMappedModel("gpt-image-2")
	if !matched || upstream != "gpt-image-2" {
		t.Fatalf("auto 尺寸应走原路径, got (%q, %v)", upstream, matched)
	}
}

// TestImageTierRouting_EmptyMappingStripsTier 锁住一个曾经遗漏的行为：
//
// 空 mapping 表示「透传全部模型」，此时档位后缀必须被剥掉，
// 否则 "gpt-image-2:4k" 会被原样发给上游——冒号不是合法模型名，上游必然 400。
// 剥成 "gpt-image-2" 正是方案约定的「没配档位就回落到基础名」。
func TestImageTierRouting_EmptyMappingStripsTier(t *testing.T) {
	account := newTierTestAccount(t, nil)

	upstream, _ := account.ResolveMappedModel("gpt-image-2:4k")
	if upstream != "gpt-image-2" {
		t.Fatalf("空映射应剥掉档位后缀, got %q, want gpt-image-2", upstream)
	}

	// 剥完后必须仍是合法图片模型名，能通过转发前校验
	if err := validateCompatibleImagesModel(upstream); err != nil {
		t.Fatalf("剥离后的模型名应通过校验: %v", err)
	}

	// 非档位请求不受影响
	if got, _ := account.ResolveMappedModel("gpt-4o"); got != "gpt-4o" {
		t.Fatalf("普通模型不应被改动, got %q", got)
	}
}

// TestImageTierRouting_MappedAccountNeverLeaksTier 确认配了映射的账号也不会泄漏档位后缀。
func TestImageTierRouting_MappedAccountNeverLeaksTier(t *testing.T) {
	account := newTierTestAccount(t, map[string]string{
		"gpt-image-2": "gpt-image-2",
	})
	upstream, _ := account.ResolveMappedModel("gpt-image-2:4k")
	if strings.Contains(upstream, ":") {
		t.Fatalf("上游模型名不应包含档位后缀, got %q", upstream)
	}
}

// TestImageTierRouting_UpstreamResolvedViaTierModel 验证转发的最后一段：
//
// 转发层拿到的模型名来自 channelMappedModel。handler 在解析渠道映射时
// 传入的正是档位模型名（gpt-image-2:4k），无渠道配置时它会被原样返回，
// 因此转发层能用同一个名字查到 ":4k" 那条映射。
//
// 关键：上游最终收到的是映射后的真实模型名（gpt-image-2-4k），
// 档位后缀不会泄漏给上游。
func TestImageTierRouting_UpstreamResolvedViaTierModel(t *testing.T) {
	account := newTierTestAccount(t, map[string]string{
		"gpt-image-2":    "gpt-image-2",
		"gpt-image-2:4k": "gpt-image-2-4k",
	})

	// handler 改写得到的档位模型名
	routingModel := ApplyImageTierToModel("gpt-image-2", "4096x4096")
	if routingModel != "gpt-image-2:4k" {
		t.Fatalf("改写失败: %q", routingModel)
	}

	// 无渠道映射时，ResolveChannelMapping 原样返回传入值 → 转发层收到档位名
	channelMappedModel := routingModel

	upstream := account.GetMappedModel(channelMappedModel)
	if upstream != "gpt-image-2-4k" {
		t.Fatalf("上游模型名应为 gpt-image-2-4k, got %q", upstream)
	}
	if strings.Contains(upstream, ":") {
		t.Fatalf("上游模型名不应含档位后缀, got %q", upstream)
	}
}
