package service

import "slices"

// Official specifications and the current Responses execution path are separate
// facts. In particular, none means omitting effort, not disabling model thinking.
type desktopAgentVendorSpecification struct {
	id, name, source, sourceURL                   string
	aliases                                       []string
	context, input, output                        int
	outputLimitKind                               string
	officialLevels, levels                        []string
	defaultReasoning                              string
	image, pdf, jsonSchema, midConversationSystem bool
	notes                                         []string
}

func desktopAgentVendorProfile(spec desktopAgentVendorSpecification) DesktopAgentProfile {
	contextWindow := spec.context
	if contextWindow == 0 {
		// Some vendors publish an independent input limit without publishing
		// a shared total. Use the input limit without relabeling it as a total.
		contextWindow = spec.input
	}
	return DesktopAgentProfile{
		ID: spec.id, DisplayName: spec.name, Source: spec.source, SourceURL: spec.sourceURL,
		ReviewedAt: "2026-11-14", Revision: "2026-11-14.2",
		Aliases:               append([]string{spec.id}, spec.aliases...),
		OfficialContextWindow: desktopProfileTokenLimit(spec.context), MaxInputTokens: desktopProfileTokenLimit(spec.input),
		OfficialMaxOutputTokens: desktopProfileTokenLimit(spec.output),
		OfficialReasoningLevels: append([]string{}, spec.officialLevels...), Notes: slices.Clone(spec.notes),
		Model: DesktopAgentModel{
			ID: spec.id, DisplayName: spec.name,
			ContextWindow: contextWindow, MaxOutputTokens: spec.output, OutputLimitKind: spec.outputLimitKind, DefaultMaxOutputTokens: 16384,
			ReasoningLevels: slices.Clone(spec.levels), DefaultReasoning: spec.defaultReasoning,
			SupportsReasoning: desktopProfileBool(len(spec.levels) > 1 || spec.levels[0] != "none"),
			SupportsStreaming: desktopProfileBool(true), RequiresMFJSToolSchema: desktopProfileBool(false),
			Input: &DesktopAgentInput{
				Text: desktopProfileBool(true), Image: desktopProfileBool(spec.image),
				Video: desktopProfileBool(false), Audio: desktopProfileBool(false), PDF: desktopProfileBool(spec.pdf),
			},
			Output:           &DesktopAgentOutput{Text: desktopProfileBool(true)},
			SupportsToolCall: desktopProfileBool(true), SupportsJSONSchemaOutput: desktopProfileBool(spec.jsonSchema),
			SupportsNativeWebSearch: desktopProfileBool(false), SupportsMidConversationSystem: desktopProfileBool(spec.midConversationSystem),
		},
	}
}

func desktopAgentDeepSeekProfiles() []DesktopAgentProfile {
	profiles := make([]DesktopAgentProfile, 0, 2)
	for _, id := range []string{"deepseek-flash", "deepseek-v4-pro"} {
		spec := desktopAgentVendorSpecification{
			id: id, name: "DeepSeek V4 Pro", source: "official_deepseek",
			sourceURL: "https://api-docs.deepseek.com/api/list-models/",
			// /models documents a shared context, not an independent input cap.
			context: 1048576, output: 393216,
			officialLevels: []string{"none", "low", "high", "max"}, levels: []string{"low", "high", "max"}, defaultReasoning: "high",
			jsonSchema: true,
			notes:      []string{"official_shared_context", "reasoning_none_unavailable", "native_web_search_unavailable"},
		}
		if id == "deepseek-flash" {
			spec.name = "DeepSeek Flash"
			spec.aliases = []string{"deepseek-v4-flash", "deepseek-v4-flash-vision-exp"}
			spec.image, spec.midConversationSystem = true, true
		}
		// Responses PDF input requires an uploaded file_id. The client sends
		// inline file_data, so native document support is not exposed here.
		profiles = append(profiles, desktopAgentVendorProfile(spec))
	}
	return profiles
}

func desktopAgentClaudeProfiles() []DesktopAgentProfile {
	type claudeSpecification struct {
		id, name, slug, defaultReasoning string
		aliases                          []string
		context, output                  int
		adaptive                         bool
		officialLevels                   []string
	}
	adaptiveLevels := []string{"low", "medium", "high", "xhigh", "max"}
	specs := []claudeSpecification{
		{"claude-fable-5-2", "Claude Fable 5.2", "fable-5-2", "high", nil, 1000000, 128000, true, adaptiveLevels},
		{"claude-opus-5-5", "Claude Opus 5.5", "opus-5-5", "medium", nil, 1000000, 128000, true, adaptiveLevels},
		{"claude-sonnet-5-5", "Claude Sonnet 5.5", "sonnet-5-5", "high", nil, 1000000, 128000, true, adaptiveLevels},
		{"claude-haiku-5-5", "Claude Haiku 5.5", "haiku-5-5", "medium", nil, 1000000, 128000, true, adaptiveLevels},
		{"claude-opus-4-6", "Claude Opus 4.6", "opus-4-6", "none", nil, 1000000, 128000, false, []string{"low", "medium", "high", "max"}},
		{"claude-sonnet-4-6", "Claude Sonnet 4.6", "sonnet-4-6", "none", nil, 1000000, 128000, false, []string{"low", "medium", "high", "max"}},
		{"claude-opus-4-5-20251101", "Claude Opus 4.5", "opus-4-5", "none", []string{"claude-opus-4-5"}, 200000, 64000, false, []string{"low", "medium", "high"}},
		{"claude-sonnet-4-5-20250929", "Claude Sonnet 4.5", "sonnet-4-5", "none", []string{"claude-sonnet-4-5"}, 200000, 64000, false, nil},
		{"claude-haiku-4-5-20251001", "Claude Haiku 4.5", "haiku-4-5", "none", []string{"claude-haiku-4-5"}, 200000, 64000, false, nil},
	}
	profiles := make([]DesktopAgentProfile, 0, len(specs))
	for _, model := range specs {
		spec := desktopAgentVendorSpecification{
			id: model.id, name: model.name, source: "official_anthropic",
			sourceURL: "https://platform.claude.com/docs/en/models/" + model.slug + "/overview",
			aliases:   model.aliases, context: model.context, output: model.output,
			officialLevels: model.officialLevels, levels: []string{"none"}, defaultReasoning: model.defaultReasoning,
			image: true, pdf: true,
			notes: []string{"official_shared_context", "structured_output_compatibility", "native_web_search_unavailable", "responses_compatibility_required"},
		}
		// 已有 Responses→Messages 桥仅对这些新家族保真映射 adaptive
		// effort。旧家族走固定 thinking budget，不能冒充等价官方档位，
		// 也避免其 32K thinking 超出当前默认 16K 单次输出预算。
		if model.adaptive {
			spec.levels = adaptiveLevels
		} else {
			spec.notes = append(spec.notes, "reasoning_auto_only")
		}
		// The bridge preserves inline images/PDFs, but hoists system messages
		// and does not translate Responses text.format into structured output.
		profiles = append(profiles, desktopAgentVendorProfile(spec))
	}
	return profiles
}

func desktopAgentGrokProfiles() []DesktopAgentProfile {
	type grokSpecification struct {
		id, name, defaultReasoning             string
		context                                int
		outputLimitKind                        string
		aliases, officialLevels, levels, notes []string
	}
	levels := []string{"low", "medium", "high", "xhigh"}
	specs := []grokSpecification{
		{"grok-4.7", "Grok 4.7", "high", 500000, "unlimited", nil, levels, levels, nil},
		{"grok-4.6", "Grok 4.6", "high", 500000, "unlimited", nil, levels, levels, nil},
		{"grok-4.5", "Grok 4.5", "high", 500000, "not_published", []string{"grok-4.5-latest", "grok-build-latest"}, levels, []string{"low", "medium", "high"}, []string{"grok_xhigh_compatibility"}},
		{"grok-4.3", "Grok 4.3", "low", 1000000, "not_published", []string{"grok-4.3-latest"}, []string{"none", "low", "medium", "high", "xhigh"}, levels, []string{"reasoning_none_unavailable"}},
		{"grok-build-0.1", "Grok Build 0.1", "none", 256000, "not_published", []string{"grok-code-fast-1", "grok-code-fast", "grok-code-fast-1-0825"}, nil, []string{"none"}, []string{"reasoning_auto_only"}},
		{"grok-4.20-0309-reasoning", "Grok 4.20 Reasoning", "none", 1000000, "not_published",
			[]string{"grok-4.20-reasoning-latest", "grok-4.20", "grok-4.20-reasoning", "grok-4.20-0309", "grok-4.20-beta-0309-reasoning", "grok-4.20-beta", "grok-4.20-beta-0309", "grok-4.20-beta-latest", "grok-4.20-beta-latest-reasoning", "grok-4.20-beta-reasoning", "grok-4.20-experimental-beta-0304-reasoning", "grok-4.20-experimental-beta-0304", "grok-4.20-experimental-beta-reasoning-latest", "grok-4.20-experimental-beta-latest", "grok-4.20-reasoning-gv2"},
			nil, []string{"none"}, []string{"reasoning_auto_only"}},
		{"grok-4.20-0309-non-reasoning", "Grok 4.20 Non-Reasoning", "none", 1000000, "not_published",
			[]string{"grok-4.20-non-reasoning", "grok-4.20-non-reasoning-latest", "grok-4.20-beta-non-reasoning", "grok-4.20-beta-latest-non-reasoning", "grok-4.20-experimental-beta-0304-non-reasoning", "grok-4.20-experimental-beta-non-reasoning-latest", "grok-4.20-beta-0309-non-reasoning", "grok-4.20-non-reasoning-gv2"},
			nil, []string{"none"}, nil},
	}
	profiles := make([]DesktopAgentProfile, 0, len(specs))
	for _, model := range specs {
		outputLimitNote := "output_limit_unpublished"
		if model.outputLimitKind == "unlimited" {
			outputLimitNote = "output_limit_unlimited"
		}
		spec := desktopAgentVendorSpecification{
			id: model.id, name: model.name, source: "official_xai",
			sourceURL: "https://docs.x.ai/developers/models/" + model.id + ".md",
			aliases:   model.aliases, context: model.context, outputLimitKind: model.outputLimitKind,
			officialLevels: model.officialLevels, levels: model.levels, defaultReasoning: model.defaultReasoning,
			image: true, jsonSchema: true, midConversationSystem: true,
			notes: append([]string{"official_shared_context", outputLimitNote, "native_web_search_unavailable"}, model.notes...),
		}
		// A Responses API default is not a model maximum. Keep unlimited and
		// unpublished limits explicit instead of inventing a numeric cap.
		profiles = append(profiles, desktopAgentVendorProfile(spec))
	}
	return profiles
}

func desktopAgentGeminiProfiles() []DesktopAgentProfile {
	type geminiSpecification struct {
		id, name       string
		sharedContext  bool
		officialLevels []string
	}
	levels := []string{"low", "medium", "high"}
	flashLevels := []string{"minimal", "low", "medium", "high"}
	specs := []geminiSpecification{
		{"gemini-4-carbon", "Gemini 4 Carbon", true, levels},
		{"gemini-4-argon", "Gemini 4 Argon", true, levels},
		{"gemini-4-hydrogen", "Gemini 4 Hydrogen", true, []string{"low", "medium", "high", "xhigh"}},
		{"gemini-3.8-flash", "Gemini 3.8 Flash", false, levels},
		{"gemini-3.6-flash", "Gemini 3.6 Flash", false, flashLevels},
		{"gemini-3.5-flash-lite", "Gemini 3.5 Flash-Lite", false, flashLevels},
		{"gemini-3.1-flash-lite", "Gemini 3.1 Flash-Lite", false, flashLevels},
		{"gemini-3.1-pro-preview", "Gemini 3.1 Pro Preview", false, levels},
		{"gemini-3-flash-preview", "Gemini 3 Flash Preview", false, flashLevels},
		// Current Interactions documentation exposes these official levels.
		// The executor below still omits effort until that transport is adapted.
		{"gemini-2.5-pro", "Gemini 2.5 Pro", false, levels},
		{"gemini-2.5-flash", "Gemini 2.5 Flash", false, levels},
		{"gemini-2.5-flash-lite", "Gemini 2.5 Flash-Lite", false, levels},
	}
	profiles := make([]DesktopAgentProfile, 0, len(specs))
	for _, model := range specs {
		spec := desktopAgentVendorSpecification{
			id: model.id, name: model.name, source: "official_google",
			sourceURL: "https://aistudio.google.com/docs/models/" + model.id,
			input:     1048576, output: 65536,
			officialLevels: model.officialLevels, levels: []string{"none"}, defaultReasoning: "none", image: true,
			notes: []string{"reasoning_auto_only", "responses_compatibility_required", "structured_output_compatibility", "native_web_search_unavailable"},
		}
		if model.sharedContext {
			spec.context, spec.input, spec.output = 1048576, 0, 1048576
			spec.notes = append(spec.notes, "official_shared_context", "long_decode_required")
		} else {
			spec.notes = append(spec.notes, "official_input_limit", "context_limit_unpublished")
		}
		// Gemini-native /generateContent is not this client's wire protocol.
		// Only image input is portable through the current Responses paths;
		// audio/video/files and native reasoning need a dedicated adaptation.
		profiles = append(profiles, desktopAgentVendorProfile(spec))
	}
	return profiles
}
