package service

import (
	"context"
	"encoding/json"
	"strconv"
)

func (s *SettingService) workbenchDeploymentLimits() WorkbenchTaskDeploymentLimits {
	limits := WorkbenchTaskDeploymentLimits{ChatConcurrentRuns: 8, OfficeConcurrentRuns: 2,
		MaxModelCalls: 8, MaxToolCalls: 16, MaxOutputTokens: 8192, MaxRunSeconds: 600}
	if s != nil && s.cfg != nil {
		cfg := s.cfg.OfficeAgent
		if cfg.MaxModelCalls > 0 {
			limits.MaxModelCalls = cfg.MaxModelCalls
		}
		if cfg.MaxToolCalls > 0 {
			limits.MaxToolCalls = cfg.MaxToolCalls
		}
		if cfg.MaxOutputTokens > 0 {
			limits.MaxOutputTokens = cfg.MaxOutputTokens
		}
		if cfg.MaxRunSeconds > 0 {
			limits.MaxRunSeconds = cfg.MaxRunSeconds
		}
	}
	return limits
}

func (s *SettingService) workbenchTaskDefaults(cfg WorkbenchConfig) WorkbenchConfig {
	cfg.DeploymentLimits = s.workbenchDeploymentLimits()
	if cfg.ChatConcurrentRuns == 0 {
		cfg.ChatConcurrentRuns = 3
	}
	if cfg.OfficeConcurrentRuns == 0 {
		cfg.OfficeConcurrentRuns = 1
	}
	if cfg.MaxModelCalls == 0 {
		cfg.MaxModelCalls = cfg.DeploymentLimits.MaxModelCalls
	}
	if cfg.MaxToolCalls == 0 {
		cfg.MaxToolCalls = cfg.DeploymentLimits.MaxToolCalls
	}
	if cfg.MaxOutputTokens == 0 {
		cfg.MaxOutputTokens = cfg.DeploymentLimits.MaxOutputTokens
	}
	if cfg.MaxRunSeconds == 0 {
		cfg.MaxRunSeconds = cfg.DeploymentLimits.MaxRunSeconds
	}
	if cfg.ModelProfiles == nil {
		cfg.ModelProfiles = map[string]DesktopAgentModelProfileBinding{}
	}
	return cfg
}

func validWorkbenchTaskBudgets(c WorkbenchConfig) bool {
	l := c.DeploymentLimits
	return c.ChatConcurrentRuns >= 1 && c.ChatConcurrentRuns <= l.ChatConcurrentRuns && c.OfficeConcurrentRuns >= 1 && c.OfficeConcurrentRuns <= l.OfficeConcurrentRuns &&
		c.MaxModelCalls >= 1 && c.MaxModelCalls <= l.MaxModelCalls && c.MaxToolCalls >= 1 && c.MaxToolCalls <= l.MaxToolCalls &&
		c.MaxOutputTokens >= 128 && c.MaxOutputTokens <= l.MaxOutputTokens && c.MaxRunSeconds >= 30 && c.MaxRunSeconds <= l.MaxRunSeconds &&
		c.MaxRunCredits >= 0 && c.MaxRunCredits <= 9_007_199_254_740_991
}

func (s *SettingService) GetWorkbenchConfig(ctx context.Context) (*WorkbenchConfig, error) {
	values, err := s.settingRepo.GetMultiple(ctx, []string{SettingKeyImageStudioEnabled, SettingKeyImageStudioGroupID, "workbench_chat_enabled", "workbench_office_enabled", "workbench_search_enabled", "workbench_files_enabled",
		"workbench_chat_concurrent_runs", "workbench_office_concurrent_runs", "workbench_max_model_calls", "workbench_max_tool_calls", "workbench_max_output_tokens", "workbench_max_run_seconds", "workbench_max_run_credits", "workbench_model_profiles_v1"})
	if err != nil {
		return nil, err
	}
	cfg := s.workbenchTaskDefaults(WorkbenchConfig{ChatEnabled: values["workbench_chat_enabled"] == "true", OfficeEnabled: values["workbench_office_enabled"] == "true", SearchEnabled: values["workbench_search_enabled"] == "true", FilesEnabled: values["workbench_files_enabled"] == "true", ImageStudioEnabled: values[SettingKeyImageStudioEnabled] != "false", ImageStudioGroupID: parseInt64Setting(values[SettingKeyImageStudioGroupID])})
	for key, target := range map[string]*int{"workbench_chat_concurrent_runs": &cfg.ChatConcurrentRuns, "workbench_office_concurrent_runs": &cfg.OfficeConcurrentRuns, "workbench_max_model_calls": &cfg.MaxModelCalls, "workbench_max_tool_calls": &cfg.MaxToolCalls, "workbench_max_output_tokens": &cfg.MaxOutputTokens, "workbench_max_run_seconds": &cfg.MaxRunSeconds} {
		if raw := values[key]; raw != "" {
			if value, e := strconv.Atoi(raw); e == nil && value > 0 {
				*target = value
			}
		}
	}
	// Lowering a deployment cap takes effect immediately for future runs; it
	// does not change the immutable budget snapshot on already admitted runs.
	cfg.ChatConcurrentRuns = min(cfg.ChatConcurrentRuns, cfg.DeploymentLimits.ChatConcurrentRuns)
	cfg.OfficeConcurrentRuns = min(cfg.OfficeConcurrentRuns, cfg.DeploymentLimits.OfficeConcurrentRuns)
	cfg.MaxModelCalls = min(cfg.MaxModelCalls, cfg.DeploymentLimits.MaxModelCalls)
	cfg.MaxToolCalls = min(cfg.MaxToolCalls, cfg.DeploymentLimits.MaxToolCalls)
	cfg.MaxOutputTokens = min(cfg.MaxOutputTokens, cfg.DeploymentLimits.MaxOutputTokens)
	cfg.MaxRunSeconds = min(cfg.MaxRunSeconds, cfg.DeploymentLimits.MaxRunSeconds)
	if value, e := strconv.ParseInt(values["workbench_max_run_credits"], 10, 64); e == nil && value >= 0 {
		cfg.MaxRunCredits = min(value, int64(9_007_199_254_740_991))
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

// UpdateWorkbenchConfig writes only the workbench keys. It must not load
// and resave the full system settings document, which could overwrite a
// concurrent administrator's unrelated settings with a stale snapshot.
func (s *SettingService) UpdateWorkbenchConfig(ctx context.Context, cfg WorkbenchConfig) error {
	cfg = s.workbenchTaskDefaults(cfg)
	if !validWorkbenchTaskBudgets(cfg) {
		return ErrWorkbenchInvalid
	}
	bindings, err := json.Marshal(cfg.ModelProfiles)
	if err != nil || len(bindings) > 32<<10 || len(cfg.ModelProfiles) > 128 {
		return ErrWorkbenchInvalid
	}
	if err := s.settingRepo.SetMultiple(ctx, map[string]string{
		SettingKeyImageStudioEnabled:       strconv.FormatBool(cfg.ImageStudioEnabled),
		"workbench_chat_enabled":           strconv.FormatBool(cfg.ChatEnabled),
		"workbench_office_enabled":         strconv.FormatBool(cfg.OfficeEnabled),
		"workbench_search_enabled":         strconv.FormatBool(cfg.SearchEnabled),
		"workbench_files_enabled":          strconv.FormatBool(cfg.FilesEnabled),
		SettingKeyImageStudioGroupID:       strconv.FormatInt(cfg.ImageStudioGroupID, 10),
		"workbench_chat_concurrent_runs":   strconv.Itoa(cfg.ChatConcurrentRuns),
		"workbench_office_concurrent_runs": strconv.Itoa(cfg.OfficeConcurrentRuns),
		"workbench_max_model_calls":        strconv.Itoa(cfg.MaxModelCalls),
		"workbench_max_tool_calls":         strconv.Itoa(cfg.MaxToolCalls),
		"workbench_max_output_tokens":      strconv.Itoa(cfg.MaxOutputTokens),
		"workbench_max_run_seconds":        strconv.Itoa(cfg.MaxRunSeconds),
		"workbench_max_run_credits":        strconv.FormatInt(cfg.MaxRunCredits, 10),
		"workbench_model_profiles_v1":      string(bindings),
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
