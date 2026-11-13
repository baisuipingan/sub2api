package service

import (
	"context"
	"slices"
)

func ParseDesktopAgentCredentialAction(raw []byte) (int64, error) {
	var body struct {
		KeyID int64 `json:"key_id"`
	}
	if err := decodeDesktopJSON(raw, &body); err != nil || body.KeyID <= 0 {
		return 0, ErrDesktopAgentInvalid
	}
	return body.KeyID, nil
}

// Profiles contain reviewed official specifications and a conservative projection
// for the current Responses executor. Saved administrator declarations are never
// a source of built-in capabilities. Keep metadata out of bootstrap model JSON.
type DesktopAgentProfile struct {
	ID                      string            `json:"id"`
	DisplayName             string            `json:"display_name"`
	Source                  string            `json:"source"`
	SourceURL               string            `json:"source_url"`
	ReviewedAt              string            `json:"reviewed_at"`
	Revision                string            `json:"revision"`
	Aliases                 []string          `json:"aliases"`
	OfficialContextWindow   int               `json:"official_context_window"`
	MaxInputTokens          int               `json:"max_input_tokens"`
	OfficialReasoningLevels []string          `json:"official_reasoning_levels"`
	Notes                   []string          `json:"notes"`
	Model                   DesktopAgentModel `json:"model"`
}

type DesktopAgentModelProfileBinding struct {
	ProfileID string `json:"profile_id"`
	Override  bool   `json:"override"`
}

// Return fresh structures: neither a caller nor a settings draft may mutate the
// process-wide source of truth. Only exact official IDs/declared aliases match.
func desktopAgentBuiltinProfiles() []DesktopAgentProfile {
	type specification struct {
		id, name string
		aliases  []string
		none     bool
	}
	specs := []specification{
		{"gpt-6.1-sol", "GPT-6.1 Sol", nil, false},
		{"gpt-6-astra", "GPT-6 Astra", nil, false},
		{"gpt-6-sol", "GPT-6 Sol", nil, true},
		{"gpt-6-luna", "GPT-6 Luna", nil, true},
		{"gpt-5.6-sol", "GPT-5.6 Sol", []string{"gpt-5.6"}, true},
		{"gpt-5.6-terra", "GPT-5.6 Terra", nil, true},
		{"gpt-5.6-luna", "GPT-5.6 Luna", nil, true},
	}
	profiles := make([]DesktopAgentProfile, 0, len(specs))
	for _, spec := range specs {
		levels := []string{"low", "medium", "high", "xhigh", "max"}
		officialLevels := slices.Clone(levels)
		notes := []string{"input_budget_compatibility", "native_web_search_unavailable"}
		if spec.none {
			officialLevels = append([]string{"none"}, officialLevels...)
			notes = append(notes, "reasoning_none_unavailable")
		}
		aliases := append([]string{spec.id}, spec.aliases...)
		profiles = append(profiles, DesktopAgentProfile{
			ID: spec.id, DisplayName: spec.name, Source: "official_openai",
			SourceURL:  "https://developers.openai.com/api/docs/models/" + spec.id,
			ReviewedAt: "2026-11-13", Revision: "2026-11-13.1", Aliases: aliases,
			OfficialContextWindow: 1050000, MaxInputTokens: 922000,
			OfficialReasoningLevels: officialLevels, Notes: notes,
			Model: DesktopAgentModel{
				ID: spec.id, DisplayName: spec.name,
				// The executor has no separate maximum-input field and does not
				// reserve the physical 128K output limit during compaction. Bound
				// its working context by the documented input limit, not 1.05M.
				ContextWindow: 922000, MaxOutputTokens: 128000, DefaultMaxOutputTokens: 16384,
				ReasoningLevels: levels, DefaultReasoning: "medium",
				SupportsReasoning: desktopProfileBool(true), SupportsStreaming: desktopProfileBool(true),
				RequiresMFJSToolSchema: desktopProfileBool(false),
				Input:                  &DesktopAgentInput{Text: desktopProfileBool(true), Image: desktopProfileBool(true), Video: desktopProfileBool(false), Audio: desktopProfileBool(false), PDF: desktopProfileBool(true)},
				Output:                 &DesktopAgentOutput{Text: desktopProfileBool(true)},
				SupportsToolCall:       desktopProfileBool(true), SupportsJSONSchemaOutput: desktopProfileBool(true),
				// ZCode only encodes provider-native WebSearch for Anthropic.
				SupportsNativeWebSearch: desktopProfileBool(false), SupportsMidConversationSystem: desktopProfileBool(true),
			},
		})
	}
	return profiles
}

func desktopProfileBool(value bool) *bool { return &value }

func (s *DesktopAgentService) GetProfiles(_ context.Context) ([]DesktopAgentProfile, error) {
	profiles := desktopAgentBuiltinProfiles()
	return profiles, nil
}

func desktopAgentProfileBinding(cfg DesktopAgentConfig, modelID string) (DesktopAgentModelProfileBinding, *DesktopAgentProfile) {
	binding, explicit := cfg.ModelProfiles[modelID]
	profiles := desktopAgentBuiltinProfiles()
	for _, profile := range profiles {
		if slices.Contains(profile.Aliases, modelID) {
			if explicit && binding.ProfileID != profile.ID {
				return binding, nil
			}
			if !explicit {
				binding = DesktopAgentModelProfileBinding{ProfileID: profile.ID}
			}
			return binding, &profile
		}
	}
	if explicit {
		for _, profile := range profiles {
			if binding.ProfileID == profile.ID {
				return binding, &profile
			}
		}
	}
	return binding, nil
}

func validateDesktopAgentProfileBindings(cfg DesktopAgentConfig) error {
	if len(cfg.ModelProfiles) > len(cfg.Models) {
		return ErrDesktopAgentInvalid
	}
	for id, binding := range cfg.ModelProfiles {
		if !slices.ContainsFunc(cfg.Models, func(model DesktopAgentModel) bool { return model.ID == id }) {
			return ErrDesktopAgentInvalid
		}
		_, profile := desktopAgentProfileBinding(cfg, id)
		if profile == nil || binding.ProfileID == "" {
			return ErrDesktopAgentInvalid
		}
	}
	return nil
}

// Resolve official capabilities on both reads and writes. The only legacy values
// retained by default are presentation and valid operational output/reasoning
// choices. An explicit saved override is required to retain custom capabilities.
// Unknown legacy aliases stay visible to administrators but are not executable.
func applyDesktopAgentCapabilityProfiles(cfg DesktopAgentConfig, repairLegacyPolicy bool) (DesktopAgentConfig, error) {
	bindings := make(map[string]DesktopAgentModelProfileBinding, len(cfg.Models))
	models := make([]DesktopAgentModel, 0, len(cfg.Models))
	for _, old := range cfg.Models {
		binding, profile := desktopAgentProfileBinding(cfg, old.ID)
		if profile == nil {
			if !repairLegacyPolicy {
				return cfg, ErrDesktopAgentInvalid
			}
			models = append(models, old)
			continue
		}
		bindings[old.ID] = binding
		if binding.Override {
			models = append(models, old)
			continue
		}
		model := profile.Model
		model.ID, model.DisplayName = old.ID, old.DisplayName
		if slices.Contains(model.ReasoningLevels, old.DefaultReasoning) {
			model.DefaultReasoning = old.DefaultReasoning
		} else if !repairLegacyPolicy {
			return cfg, ErrDesktopAgentInvalid
		}
		if old.DefaultMaxOutputTokens > 0 && old.DefaultMaxOutputTokens <= model.MaxOutputTokens {
			model.DefaultMaxOutputTokens = old.DefaultMaxOutputTokens
		} else if !repairLegacyPolicy {
			return cfg, ErrDesktopAgentInvalid
		}
		models = append(models, model)
	}
	if len(bindings) == 0 {
		bindings = nil
	}
	cfg.Models, cfg.ModelProfiles = models, bindings
	return cfg, nil
}

func (s *DesktopAgentService) ManageCredential(ctx context.Context, caller DesktopAgentCredentialCaller, userID, keyID int64, reset bool) (*DesktopAgentCredentialMutation, error) {
	manager, ok := s.credentials.(interface {
		ManageDesktopAgentCredential(context.Context, DesktopAgentCredentialCaller, int64, int64, bool) (*DesktopAgentCredentialMutation, error)
	})
	if !ok {
		return nil, ErrDesktopAgentInvalid
	}
	return manager.ManageDesktopAgentCredential(ctx, caller, userID, keyID, reset)
}
