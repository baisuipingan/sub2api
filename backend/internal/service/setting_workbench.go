package service

import (
	"context"
	"encoding/json"
	"strconv"
)

func (s *SettingService) GetWorkbenchConfig(ctx context.Context) (*WorkbenchConfig, error) {
	values, err := s.settingRepo.GetMultiple(ctx, []string{SettingKeyImageStudioEnabled, SettingKeyImageStudioGroupID, "workbench_chat_enabled", "workbench_office_enabled", "workbench_search_enabled", "workbench_files_enabled",
		"workbench_model_profiles_v1", SettingKeyDesktopAgentConfig})
	if err != nil {
		return nil, err
	}
	agent := workbenchAgentSelection(values[SettingKeyDesktopAgentConfig])
	cfg := WorkbenchConfig{
		ChatEnabled:        values["workbench_chat_enabled"] == "true",
		OfficeEnabled:      values["workbench_office_enabled"] == "true",
		SearchEnabled:      values["workbench_search_enabled"] == "true",
		FilesEnabled:       values["workbench_files_enabled"] == "true",
		ImageStudioEnabled: values[SettingKeyImageStudioEnabled] != "false",
		ImageStudioGroupID: parseInt64Setting(values[SettingKeyImageStudioGroupID]),
		AgentGroupID:       agent.GroupID,
		ModelProfiles:      map[string]DesktopAgentModelProfileBinding{},
		agentSelection:     agent,
	}
	if raw := values["workbench_model_profiles_v1"]; raw != "" {
		if len(raw) > 32<<10 || json.Unmarshal([]byte(raw), &cfg.ModelProfiles) != nil || len(cfg.ModelProfiles) > 128 || cfg.ModelProfiles == nil {
			cfg.ModelProfiles = map[string]DesktopAgentModelProfileBinding{}
		}
	}
	return &cfg, nil
}

type workbenchAgentModel struct {
	ID                     string `json:"id"`
	DisplayName            string `json:"display_name"`
	DefaultReasoning       string `json:"default_reasoning"`
	DefaultMaxOutputTokens int    `json:"default_max_output_tokens"`
}

type workbenchAgentConfig struct {
	GroupID        int64
	DefaultModelID string
	Models         []workbenchAgentModel
	ModelProfiles  map[string]DesktopAgentModelProfileBinding
	Revision       string
}

func workbenchAgentSelection(raw string) workbenchAgentConfig {
	var selection struct {
		Enabled        bool            `json:"enabled"`
		GroupID        int64           `json:"group_id"`
		DefaultModelID json.RawMessage `json:"default_model_id"`
		Models         json.RawMessage `json:"models"`
		ModelProfiles  json.RawMessage `json:"model_profiles"`
		LegacyModel    json.RawMessage `json:"model"`
	}
	if err := json.Unmarshal([]byte(raw), &selection); err != nil {
		return workbenchAgentConfig{}
	}
	if !selection.Enabled || selection.GroupID <= 0 {
		return workbenchAgentConfig{}
	}
	result := workbenchAgentConfig{GroupID: selection.GroupID, Revision: officeDigest([]byte(raw))}
	if len(selection.Models) != 0 {
		if json.Unmarshal(selection.Models, &result.Models) != nil {
			result.Models = nil
			return result
		}
	} else if len(selection.LegacyModel) != 0 {
		var model workbenchAgentModel
		if json.Unmarshal(selection.LegacyModel, &model) == nil && model.ID != "" {
			result.Models = []workbenchAgentModel{model}
			result.DefaultModelID = model.ID
		}
	}
	if len(selection.DefaultModelID) != 0 {
		_ = json.Unmarshal(selection.DefaultModelID, &result.DefaultModelID)
	}
	if len(selection.ModelProfiles) != 0 && json.Unmarshal(selection.ModelProfiles, &result.ModelProfiles) != nil {
		result.ModelProfiles = nil
	}
	return result
}

// UpdateWorkbenchConfig writes only the workbench keys. It must not load
// and resave the full system settings document, which could overwrite a
// concurrent administrator's unrelated settings with a stale snapshot.
func (s *SettingService) UpdateWorkbenchConfig(ctx context.Context, cfg WorkbenchConfig) error {
	if err := s.settingRepo.SetMultiple(ctx, map[string]string{
		SettingKeyImageStudioEnabled: strconv.FormatBool(cfg.ImageStudioEnabled),
		"workbench_chat_enabled":     strconv.FormatBool(cfg.ChatEnabled),
		"workbench_office_enabled":   strconv.FormatBool(cfg.OfficeEnabled),
		"workbench_search_enabled":   strconv.FormatBool(cfg.SearchEnabled),
		"workbench_files_enabled":    strconv.FormatBool(cfg.FilesEnabled),
		SettingKeyImageStudioGroupID: strconv.FormatInt(cfg.ImageStudioGroupID, 10),
	}); err != nil {
		return err
	}
	// These values have no independent runtime caches; invalidate the public
	// settings/HTML response through the existing settings-update callback.
	if s.onUpdate != nil {
		s.onUpdate()
	}
	return nil
}
