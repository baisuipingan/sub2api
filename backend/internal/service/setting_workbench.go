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
	agentGroupID, agentDefaultModelID := workbenchAgentSelection(values[SettingKeyDesktopAgentConfig])
	cfg := WorkbenchConfig{
		ChatEnabled:         values["workbench_chat_enabled"] == "true",
		OfficeEnabled:       values["workbench_office_enabled"] == "true",
		SearchEnabled:       values["workbench_search_enabled"] == "true",
		FilesEnabled:        values["workbench_files_enabled"] == "true",
		ImageStudioEnabled:  values[SettingKeyImageStudioEnabled] != "false",
		ImageStudioGroupID:  parseInt64Setting(values[SettingKeyImageStudioGroupID]),
		AgentGroupID:        agentGroupID,
		ModelProfiles:       map[string]DesktopAgentModelProfileBinding{},
		agentDefaultModelID: agentDefaultModelID,
	}
	if raw := values["workbench_model_profiles_v1"]; raw != "" {
		if len(raw) > 32<<10 || json.Unmarshal([]byte(raw), &cfg.ModelProfiles) != nil || len(cfg.ModelProfiles) > 128 {
			return nil, ErrWorkbenchInvalid
		}
		if cfg.ModelProfiles == nil {
			cfg.ModelProfiles = map[string]DesktopAgentModelProfileBinding{}
		}
	}
	return &cfg, nil
}

func workbenchAgentSelection(raw string) (int64, string) {
	var cfg struct {
		Enabled        bool            `json:"enabled"`
		GroupID        int64           `json:"group_id"`
		DefaultModelID json.RawMessage `json:"default_model_id"`
		LegacyModel    json.RawMessage `json:"model"`
	}
	if json.Unmarshal([]byte(raw), &cfg) != nil || !cfg.Enabled || cfg.GroupID <= 0 {
		return 0, ""
	}
	var modelID string
	if len(cfg.DefaultModelID) != 0 {
		_ = json.Unmarshal(cfg.DefaultModelID, &modelID)
	} else if len(cfg.LegacyModel) != 0 {
		var legacy struct {
			ID string `json:"id"`
		}
		if json.Unmarshal(cfg.LegacyModel, &legacy) == nil {
			modelID = legacy.ID
		}
	}
	return cfg.GroupID, modelID
}

// UpdateWorkbenchConfig writes only the workbench keys. It must not load
// and resave the full system settings document, which could overwrite a
// concurrent administrator's unrelated settings with a stale snapshot.
func (s *SettingService) UpdateWorkbenchConfig(ctx context.Context, cfg WorkbenchConfig) error {
	if cfg.ModelProfiles == nil {
		cfg.ModelProfiles = map[string]DesktopAgentModelProfileBinding{}
	}
	bindings, err := json.Marshal(cfg.ModelProfiles)
	if err != nil || len(bindings) > 32<<10 || len(cfg.ModelProfiles) > 128 {
		return ErrWorkbenchInvalid
	}
	if err := s.settingRepo.SetMultiple(ctx, map[string]string{
		SettingKeyImageStudioEnabled:  strconv.FormatBool(cfg.ImageStudioEnabled),
		"workbench_chat_enabled":      strconv.FormatBool(cfg.ChatEnabled),
		"workbench_office_enabled":    strconv.FormatBool(cfg.OfficeEnabled),
		"workbench_search_enabled":    strconv.FormatBool(cfg.SearchEnabled),
		"workbench_files_enabled":     strconv.FormatBool(cfg.FilesEnabled),
		SettingKeyImageStudioGroupID:  strconv.FormatInt(cfg.ImageStudioGroupID, 10),
		"workbench_model_profiles_v1": string(bindings),
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
