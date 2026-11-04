package service

import (
	"context"
	"testing"
)

// fork: image-size-routing
//
// 覆盖三条硬约束：
//  1. 不配能力表的账号行为完全不变（存量零影响）
//  2. 配了表但没该档位 → 明确不支持（不降级）
//  3. model_mapping 绝不出现档位标记（模型列表不会被污染）

func newSizeAccount(t *testing.T, mapping map[string]string, extra map[string]any) *Account {
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
		Extra:       extra,
	}
}

// sizeCapabilitiesExtra 构造能力表配置
func sizeCapabilitiesExtra(caps map[string]any) map[string]any {
	return map[string]any{ImageSizeCapabilitiesExtraKey: caps}
}

func TestImageSizeRouting_NoCapabilityTable(t *testing.T) {
	// 存量账号：只配 model_mapping，没有任何尺寸能力表
	acc := newSizeAccount(t, map[string]string{"gpt-image-2": "gpt-image-2"}, nil)

	for _, size := range []string{"1024x1024", "2048x2048", "4096x4096", "2880x2880", "auto"} {
		upstream, supported := ResolveImageSizeUpstream(acc, "gpt-image-2", size)
		if !supported {
			t.Fatalf("size=%s: 未配能力表应始终支持", size)
		}
		if upstream != "" {
			t.Fatalf("size=%s: 未配能力表不应改写上游名, got %q", size, upstream)
		}
	}
}

func TestImageSizeRouting_TierMapping(t *testing.T) {
	// 拆名型供应商：4K 和 2K 各有独立上游名
	acc := newSizeAccount(t,
		map[string]string{"gpt-image-2": "gpt-image-2"},
		sizeCapabilitiesExtra(map[string]any{
			"gpt-image-2": map[string]any{
				"4k": "gpt-image-2-4K",
				"2k": "gpt-image-2-2K",
				"1k": "gpt-image-2",
			},
		}),
	)

	cases := []struct{ size, want string }{
		{"4096x4096", "gpt-image-2-4K"},
		{"2880x2880", "gpt-image-2-4K"}, // 前端 4K 档位实际发的是 2880
		{"2048x2048", "gpt-image-2-2K"},
		{"1024x1024", "gpt-image-2"},
	}
	for _, c := range cases {
		upstream, supported := ResolveImageSizeUpstream(acc, "gpt-image-2", c.size)
		if !supported {
			t.Fatalf("size=%s 应支持", c.size)
		}
		if upstream != c.want {
			t.Fatalf("size=%s: got %q, want %q", c.size, upstream, c.want)
		}
	}

	// size=auto 无法判档 → 不介入
	upstream, supported := ResolveImageSizeUpstream(acc, "gpt-image-2", "auto")
	if !supported || upstream != "" {
		t.Fatalf("auto 应不介入, got (%q, %v)", upstream, supported)
	}
}

func TestImageSizeRouting_OnlyOneK(t *testing.T) {
	// 只支持 1K 的供应商：配了表但只有 1k
	acc := newSizeAccount(t,
		map[string]string{"gpt-image-2": "gpt-image-2"},
		sizeCapabilitiesExtra(map[string]any{
			"gpt-image-2": map[string]any{"1k": "gpt-image-2"},
		}),
	)

	if _, supported := ResolveImageSizeUpstream(acc, "gpt-image-2", "4096x4096"); supported {
		t.Fatal("只配 1k 的账号不应接受 4K 请求")
	}
	if _, supported := ResolveImageSizeUpstream(acc, "gpt-image-2", "2048x2048"); supported {
		t.Fatal("只配 1k 的账号不应接受 2K 请求")
	}
	upstream, supported := ResolveImageSizeUpstream(acc, "gpt-image-2", "1024x1024")
	if !supported || upstream != "gpt-image-2" {
		t.Fatalf("1K 应支持, got (%q, %v)", upstream, supported)
	}
}

func TestImageSizeRouting_OtherModelInTable(t *testing.T) {
	// 能力表里只提了别的模型 → 对 gpt-image-2 不介入
	acc := newSizeAccount(t,
		map[string]string{"gpt-image-2": "gpt-image-2"},
		sizeCapabilitiesExtra(map[string]any{
			"some-other-image": map[string]any{"4k": "x"},
		}),
	)

	upstream, supported := ResolveImageSizeUpstream(acc, "gpt-image-2", "4096x4096")
	if !supported || upstream != "" {
		t.Fatalf("表里没提这个模型时应不介入, got (%q, %v)", upstream, supported)
	}
}

func TestImageSizeRouting_ArrayForm(t *testing.T) {
	// 数组写法（前端表单更容易生成）
	acc := newSizeAccount(t,
		map[string]string{"gpt-image-2": "gpt-image-2"},
		sizeCapabilitiesExtra(map[string]any{
			"gpt-image-2": []any{
				map[string]any{"tier": "4k", "upstream": "gpt-image-2-4K"},
			},
		}),
	)

	upstream, supported := ResolveImageSizeUpstream(acc, "gpt-image-2", "4096x4096")
	if !supported || upstream != "gpt-image-2-4K" {
		t.Fatalf("数组写法应生效, got (%q, %v)", upstream, supported)
	}
}

func TestImageSizeRouting_DoesNotTouchModelMapping(t *testing.T) {
	// 核心不变量：model_mapping 的 key 里绝不能出现档位标记。
	// 一旦出现，会被全站多处代码当作可售模型遍历，前端就会显示伪模型。
	acc := newSizeAccount(t,
		map[string]string{"gpt-image-2": "gpt-image-2"},
		sizeCapabilitiesExtra(map[string]any{
			"gpt-image-2": map[string]any{"4k": "gpt-image-2-4K"},
		}),
	)

	for key := range acc.GetModelMapping() {
		if key != "gpt-image-2" {
			t.Fatalf("model_mapping 被污染: %q", key)
		}
	}

	// 且能力表不影响模型清单的可售模型
	models := batchImageModelsFromAccountMapping(acc)
	for _, m := range models {
		if m != "gpt-image-2" {
			t.Fatalf("模型清单出现非预期模型: %q", m)
		}
	}
}

func TestImageSizeRouting_ContextRoundTrip(t *testing.T) {
	ctx := WithImageRequestSize(context.Background(), "4096x4096")
	got, ok := ImageRequestSizeFromContext(ctx)
	if !ok || got != "4096x4096" {
		t.Fatalf("context 传递失败, got (%q, %v)", got, ok)
	}

	if _, ok := ImageRequestSizeFromContext(context.Background()); ok {
		t.Fatal("空 context 应返回 false")
	}
	if _, ok := ImageRequestSizeFromContext(nil); ok {
		t.Fatal("nil context 应返回 false")
	}
}

func TestImageSizeTierFor(t *testing.T) {
	cases := []struct {
		size string
		want string
		ok   bool
	}{
		{"1024x1024", "1k", true},
		{"2048x2048", "2k", true},
		{"4096x4096", "4k", true},
		{"2880x2880", "4k", true},    // 前端 4K 档位的实际值
		{"3840x2160", "4k", true},
		{"auto", "", false},
		{"", "", false},
		{"invalid", "", false},
	}
	for _, c := range cases {
		got, ok := ImageSizeTierFor(c.size)
		if ok != c.ok || got != c.want {
			t.Fatalf("size=%q: got (%q, %v), want (%q, %v)", c.size, got, ok, c.want, c.ok)
		}
	}
}
