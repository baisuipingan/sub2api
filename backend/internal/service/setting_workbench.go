package service

import (
	"context"
	"strconv"
)

func (s *SettingService) GetWorkbenchConfig(ctx context.Context) (*WorkbenchConfig, error) {
	values, err := s.settingRepo.GetMultiple(ctx, []string{SettingKeyImageStudioEnabled, SettingKeyImageStudioGroupID})
	if err != nil {
		return nil, err
	}
	return &WorkbenchConfig{ImageStudioEnabled: values[SettingKeyImageStudioEnabled] != "false", ImageStudioGroupID: parseInt64Setting(values[SettingKeyImageStudioGroupID])}, nil
}

// UpdateWorkbenchConfig writes only the two workbench keys. It must not load
// and resave the full system settings document, which could overwrite a
// concurrent administrator's unrelated settings with a stale snapshot.
func (s *SettingService) UpdateWorkbenchConfig(ctx context.Context, cfg WorkbenchConfig) error {
	if err := s.settingRepo.SetMultiple(ctx, map[string]string{
		SettingKeyImageStudioEnabled: strconv.FormatBool(cfg.ImageStudioEnabled),
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
