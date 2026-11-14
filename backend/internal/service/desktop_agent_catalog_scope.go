package service

import "context"

func validDesktopAgentOutputLimit(m *DesktopAgentModel) bool {
	switch m.OutputLimitKind {
	case "", "fixed":
		return m.MaxOutputTokens > 0 && m.MaxOutputTokens <= m.ContextWindow && m.DefaultMaxOutputTokens <= m.MaxOutputTokens
	case "unlimited", "not_published":
		return m.MaxOutputTokens == 0 && m.DefaultMaxOutputTokens <= m.ContextWindow
	default:
		return false
	}
}
func desktopAgentLegacyModel(m DesktopAgentModel) bool {
	return (m.OutputLimitKind == "" || m.OutputLimitKind == "fixed") && m.MaxOutputTokens > 0 && m.MaxOutputTokens < m.ContextWindow
}
func (s *DesktopAgentService) profileRequestScope() *DesktopAgentService {
	if s.profiles != nil {
		return s
	}
	scoped := *s
	if s.profileCatalog != nil {
		scoped.profiles, scoped.profileRecordRaw = s.profileCatalog.requestSnapshot()
	} else {
		scoped.profiles = desktopAgentBuiltinProfiles()
	}
	return &scoped
}
func (s *DesktopAgentService) WithProfileCatalog(catalog *DesktopAgentProfileCatalogService) *DesktopAgentService {
	s.profileCatalog = catalog
	if catalog != nil {
		catalog.validate = s.preflightProfileCatalog
		catalog.Start()
	}
	return s
}

// This raw entry point keeps admin parsing and the save on exactly one catalog
// revision and serializes publication against compatibility preflight.
func (s *DesktopAgentService) UpdateConfigJSON(ctx context.Context, raw []byte) (*DesktopAgentConfig, error) {
	if s.profileCatalog != nil {
		s.profileCatalog.updateMu.Lock()
		defer s.profileCatalog.updateMu.Unlock()
	}
	scoped := s.profileRequestScope()
	cfg, err := parseDesktopAgentAdminConfig(raw, scoped.profiles)
	if err != nil {
		return nil, err
	}
	return scoped.UpdateConfig(ctx, cfg)
}
func (s *DesktopAgentService) preflightProfileCatalog(ctx context.Context, profiles []DesktopAgentProfile) (string, error) {
	before := s.profileRequestScope()
	values, err := s.settings.settingRepo.GetMultiple(ctx, []string{SettingKeyDesktopAgentConfig})
	if err != nil {
		return "", err
	}
	raw := values[SettingKeyDesktopAgentConfig]
	if raw == "" {
		return raw, nil
	}
	cfg, err := parseDesktopAgentConfig([]byte(raw), profiles)
	if err != nil {
		return raw, err
	}
	cfg, err = applyDesktopAgentCapabilityProfiles(cfg, false, profiles)
	if err != nil {
		return raw, err
	}
	if err = validateDesktopAgentConfig(cfg, profiles); err != nil {
		return raw, err
	}
	if !cfg.Enabled {
		return raw, nil
	}
	previous, err := parseDesktopAgentConfig([]byte(raw), before.profiles)
	if err != nil {
		return raw, err
	}
	previous, err = applyDesktopAgentCapabilityProfiles(previous, true, before.profiles)
	if err != nil {
		return raw, err
	}
	candidate := *s
	candidate.profiles = profiles
	// Existing unavailability must not prohibit repairing the catalog. A catalog
	// refresh may not introduce a new failure for a previously available model.
	group, models, reason, err := candidate.readGroupCatalog(ctx, cfg)
	if err != nil {
		return raw, err
	}
	if reason != "" {
		return raw, nil
	}
	for i := range cfg.Models {
		var old *DesktopAgentModel
		for j := range previous.Models {
			if previous.Models[j].ID == cfg.Models[i].ID {
				old = &previous.Models[j]
				break
			}
		}
		if old != nil && desktopAgentModelPolicyReason(group, models, old) == "" && desktopAgentModelPolicyReason(group, models, &cfg.Models[i]) != "" {
			return raw, ErrDesktopAgentInvalid
		}
	}
	return raw, nil
}
func (s *DesktopAgentService) ProfileCatalogStatus() DesktopAgentProfileCatalogStatus {
	if s.profileCatalog == nil {
		return DesktopAgentProfileCatalogStatus{Revision: "builtin", ProfileCount: len(desktopAgentBuiltinProfiles()), UsingFallback: true}
	}
	return s.profileCatalog.Status()
}
func (s *DesktopAgentService) RefreshProfileCatalog(ctx context.Context) error {
	if s.profileCatalog == nil {
		return ErrDesktopAgentInvalid
	}
	return s.profileCatalog.Refresh(ctx)
}
func (s *DesktopAgentService) SetProfileCatalogSource(ctx context.Context, source string) error {
	if s.profileCatalog == nil {
		return ErrDesktopAgentInvalid
	}
	return s.profileCatalog.SetSource(ctx, source)
}

func ValidDesktopAgentProfileSourceBody(raw []byte) bool {
	var body struct {
		SourceURL string `json:"source_url"`
	}
	return decodeDesktopJSON(raw, &body) == nil
}
