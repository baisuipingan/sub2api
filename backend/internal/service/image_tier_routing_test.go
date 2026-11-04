package service

import "testing"

// fork: image-tier-routing
//
// 覆盖三件事：
//  1. 请求模型名按 size 改写为带档位的形式
//  2. 账号映射查找：精确 → 回落基础名 → 不支持
//  3. 非生图模型 / 无法判档的 size 完全不受影响

func TestApplyImageTierToModel(t *testing.T) {
	tests := []struct {
		name  string
		model string
		size  string
		want  string
	}{
		{"4K 方图", "gpt-image-2", "4096x4096", "gpt-image-2:4k"},
		{"2K 方图", "gpt-image-2", "2048x2048", "gpt-image-2:2k"},
		{"1K 方图", "gpt-image-2", "1024x1024", "gpt-image-2:1k"},
		{"4K 横幅", "gpt-image-2", "3840x2160", "gpt-image-2:4k"},
		{"2K 横幅", "gpt-image-2", "2048x1152", "gpt-image-2:2k"},
		{"字面量 4k", "gpt-image-2", "4k", "gpt-image-2:4k"},
		{"带版本号模型", "gpt-image-2.5", "4096x4096", "gpt-image-2.5:4k"},

		// 以下都应原样返回（空字符串 = 不改写）
		{"auto 不判档", "gpt-image-2", "auto", ""},
		{"空 size", "gpt-image-2", "", ""},
		{"无法解析的 size", "gpt-image-2", "huge", ""},
		{"文本模型不受影响", "gpt-4o", "4096x4096", ""},
		{"Claude 不受影响", "claude-sonnet-4", "4096x4096", ""},
		{"空模型名", "", "4096x4096", ""},
		{"已带档位保持幂等", "gpt-image-2:4k", "4096x4096", "gpt-image-2:4k"},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if got := ApplyImageTierToModel(tc.model, tc.size); got != tc.want {
				t.Fatalf("ApplyImageTierToModel(%q, %q) = %q, want %q", tc.model, tc.size, got, tc.want)
			}
		})
	}
}

func TestSplitImageTierModel(t *testing.T) {
	tests := []struct {
		model    string
		wantBase string
		wantTier string
		wantOK   bool
	}{
		{"gpt-image-2:4k", "gpt-image-2", "4k", true},
		{"gpt-image-2:2k", "gpt-image-2", "2k", true},
		{"gpt-image-2:1k", "gpt-image-2", "1k", true},
		{"gpt-image-2.5:4k", "gpt-image-2.5", "4k", true},
		// 大小写归一
		{"gpt-image-2:4K", "gpt-image-2", "4k", true},

		// 非档位形式
		{"gpt-image-2", "", "", false},
		{"gpt-4o:4k", "", "", false},        // 不是生图模型族
		{"gpt-image-2:8k", "", "", false},   // 未知档位
		{"gpt-image-2:", "", "", false},     // 空档位
		{":4k", "", "", false},              // 空基础名
		{"", "", "", false},
	}

	for _, tc := range tests {
		t.Run(tc.model, func(t *testing.T) {
			base, tier, ok := splitImageTierModel(tc.model)
			if ok != tc.wantOK || base != tc.wantBase || tier != tc.wantTier {
				t.Fatalf("splitImageTierModel(%q) = (%q, %q, %v), want (%q, %q, %v)",
					tc.model, base, tier, ok, tc.wantBase, tc.wantTier, tc.wantOK)
			}
		})
	}
}

func TestResolveImageTierMapping(t *testing.T) {
	t.Run("精确档位优先于基础名", func(t *testing.T) {
		mapping := map[string]string{
			"gpt-image-2":    "gpt-image-2",
			"gpt-image-2:4k": "gpt-image-2-4k",
		}
		mapped, ok := ResolveImageTierMapping(mapping, "gpt-image-2:4k")
		if !ok || mapped != "gpt-image-2-4k" {
			t.Fatalf("got (%q, %v), want (gpt-image-2-4k, true)", mapped, ok)
		}
	})

	t.Run("没配档位时回落到基础名", func(t *testing.T) {
		mapping := map[string]string{"gpt-image-2": "gpt-image-2"}
		mapped, ok := ResolveImageTierMapping(mapping, "gpt-image-2:4k")
		if !ok || mapped != "gpt-image-2" {
			t.Fatalf("got (%q, %v), want (gpt-image-2, true)", mapped, ok)
		}
	})

	t.Run("2K 配了 4K 没配", func(t *testing.T) {
		mapping := map[string]string{
			"gpt-image-2":    "gpt-image-2",
			"gpt-image-2:2k": "gpt-image-2-2k",
		}
		if mapped, ok := ResolveImageTierMapping(mapping, "gpt-image-2:2k"); !ok || mapped != "gpt-image-2-2k" {
			t.Fatalf("2k 应命中专用名, got (%q, %v)", mapped, ok)
		}
		// 4K 没配专用名 → 回落基础名
		if mapped, ok := ResolveImageTierMapping(mapping, "gpt-image-2:4k"); !ok || mapped != "gpt-image-2" {
			t.Fatalf("4k 应回落基础名, got (%q, %v)", mapped, ok)
		}
	})

	t.Run("基础名和档位都没配则不支", func(t *testing.T) {
		mapping := map[string]string{"gpt-4o": "gpt-4o"}
		if _, ok := ResolveImageTierMapping(mapping, "gpt-image-2:4k"); ok {
			t.Fatal("应判定为不支持")
		}
	})

	t.Run("空映射不支持", func(t *testing.T) {
		if _, ok := ResolveImageTierMapping(nil, "gpt-image-2:4k"); ok {
			t.Fatal("空映射应判定为不支持")
		}
	})

	t.Run("通配符不参与档位匹配", func(t *testing.T) {
		// 关键行为：一个 gpt-image-2* 通配符不能让所有档位都"支持"，
		// 否则无法区分哪家真的能做 4K。
		mapping := map[string]string{"gpt-image-2*": "gpt-image-2"}
		if _, ok := ResolveImageTierMapping(mapping, "gpt-image-2:4k"); ok {
			t.Fatal("通配符不应让档位请求命中")
		}
	})

	t.Run("非档位请求不做特殊处理", func(t *testing.T) {
		mapping := map[string]string{"gpt-image-2": "gpt-image-2"}
		if _, ok := ResolveImageTierMapping(mapping, "gpt-image-2"); ok {
			t.Fatal("不带档位的模型名不该走本函数")
		}
	})
}

func TestSupportsImageTierModel(t *testing.T) {
	mapping := map[string]string{"gpt-image-2": "gpt-image-2"}
	if !SupportsImageTierModel(mapping, "gpt-image-2:4k") {
		t.Fatal("配了基础名应视为支持 4K")
	}
	if SupportsImageTierModel(mapping, "gpt-image-9:4k") {
		t.Fatal("没配的模型应不支持")
	}
}

// TestApplyImageTierToModel_OtherFamilies 验证不止 gpt-image-2 能走档位路由。
//
// NanoBanana 对应 Gemini 的 image 模型（gemini-3-pro-image 等），
// 这类模型同样可能有 1K/2K/4K 拆分售卖的情况，必须支持。
func TestApplyImageTierToModel_OtherFamilies(t *testing.T) {
	tests := []struct {
		name  string
		model string
		size  string
		want  string
	}{
		// NanoBanana（Gemini image 系列）
		{"NanoBanana 4K", "gemini-3-pro-image", "4096x4096", "gemini-3-pro-image:4k"},
		{"NanoBanana 2K", "gemini-2.5-flash-image", "2048x2048", "gemini-2.5-flash-image:2k"},
		{"Gemini 带 models 前缀", "models/gemini-3-pro-image", "4096x4096", "models/gemini-3-pro-image:4k"},
		{"Gemini 新版", "gemini-3.1-flash-image", "4096x4096", "gemini-3.1-flash-image:4k"},

		// Grok 系
		{"Grok image", "grok-imagine-image", "2048x2048", "grok-imagine-image:2k"},

		// 非生图模型仍然不受影响
		{"Gemini 文本模型", "gemini-2.5-pro", "4096x4096", ""},
		{"Gemini Flash 文本", "gemini-2.5-flash", "4096x4096", ""},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if got := ApplyImageTierToModel(tc.model, tc.size); got != tc.want {
				t.Fatalf("ApplyImageTierToModel(%q, %q) = %q, want %q", tc.model, tc.size, got, tc.want)
			}
		})
	}
}

func TestIsImageGenerationModelFamily(t *testing.T) {
	imageModels := []string{
		"gpt-image-2", "gpt-image-2.5",
		"grok-imagine-image",
		"gemini-3-pro-image", "models/gemini-3-pro-image",
		"gemini-2.5-flash-image", "gemini-3.1-flash-image",
	}
	for _, m := range imageModels {
		if !IsImageGenerationModelFamily(m) {
			t.Fatalf("%q 应被识别为生图模型", m)
		}
	}

	textModels := []string{
		"gpt-4o", "claude-sonnet-4", "gemini-2.5-pro", "gemini-2.5-flash", "deepseek-chat", "",
	}
	for _, m := range textModels {
		if IsImageGenerationModelFamily(m) {
			t.Fatalf("%q 不应被识别为生图模型", m)
		}
	}
}
