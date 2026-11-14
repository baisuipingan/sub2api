package service

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"slices"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/config"
	"github.com/Wei-Shaw/sub2api/internal/pkg/httpclient"
	"github.com/Wei-Shaw/sub2api/internal/util/urlvalidator"
)

const (
	SettingKeyDesktopAgentProfileCatalog = "desktop_agent_profile_catalog_v1"
	DefaultDesktopAgentProfileCatalogURL = "https://raw.githubusercontent.com/baisuipingan/sub2api/model-profiles/desktop-agent-profiles.json"
	desktopAgentCatalogMaxBytes          = 2 << 20
)

// The remote catalog is data only. Credentials, routes and executable maps never
// appear in this document. Unknown schemas are rejected without replacing LKG.
type DesktopAgentProfileCatalogDocument struct {
	SchemaVersion int                   `json:"schema_version"`
	Revision      string                `json:"revision"`
	Profiles      []DesktopAgentProfile `json:"profiles"`
}
type desktopAgentProfileCatalogRecord struct {
	SchemaVersion int                   `json:"schema_version"`
	SourceURL     string                `json:"source_url"`
	Revision      string                `json:"revision"`
	Profiles      []DesktopAgentProfile `json:"profiles"`
	LastSuccessAt string                `json:"last_success_at"`
}
type DesktopAgentProfileCatalogStatus struct {
	SourceURL     string `json:"source_url"`
	Revision      string `json:"revision"`
	ProfileCount  int    `json:"profile_count"`
	LastSuccessAt string `json:"last_success_at"`
	LastCheckedAt string `json:"last_checked_at"`
	LastError     string `json:"last_error"`
	UsingFallback bool   `json:"using_fallback"`
}
type DesktopAgentProfileCatalogService struct {
	cfg          *config.Config
	repo         SettingRepository
	client       *http.Client
	mu           sync.RWMutex
	updateMu     sync.Mutex
	profiles     []DesktopAgentProfile
	persistedRaw string
	status       DesktopAgentProfileCatalogStatus
	validate     func(context.Context, []DesktopAgentProfile) (string, error)
	ctx          context.Context
	cancel       context.CancelFunc
	once         sync.Once
	stopOnce     sync.Once
	wg           sync.WaitGroup
}

func NewDesktopAgentProfileCatalogService(cfg *config.Config, settings *SettingService) *DesktopAgentProfileCatalogService {
	ctx, cancel := context.WithCancel(context.Background())
	s := &DesktopAgentProfileCatalogService{cfg: cfg, profiles: desktopAgentBuiltinProfiles(), ctx: ctx, cancel: cancel}
	if settings != nil {
		s.repo = settings.settingRepo
	}
	s.status = DesktopAgentProfileCatalogStatus{SourceURL: DefaultDesktopAgentProfileCatalogURL, Revision: "builtin", ProfileCount: len(s.profiles), UsingFallback: true}
	opts := httpclient.Options{Timeout: 20 * time.Second, ResponseHeaderTimeout: 10 * time.Second, ValidateResolvedIP: true}
	if cfg != nil {
		opts.ProxyURL = cfg.Update.ProxyURL
	}
	shared, err := httpclient.GetClient(opts)
	if err == nil {
		copied := *shared
		copied.CheckRedirect = func(*http.Request, []*http.Request) error { return errors.New("catalog redirects are disabled") }
		s.client = &copied
	}
	return s
}

func (s *DesktopAgentProfileCatalogService) Initialize(ctx context.Context) error {
	if s.repo == nil {
		return nil
	}
	values, err := s.repo.GetMultiple(ctx, []string{SettingKeyDesktopAgentProfileCatalog})
	if err != nil {
		return err
	}
	raw := values[SettingKeyDesktopAgentProfileCatalog]
	if raw == "" {
		return nil
	}
	var record desktopAgentProfileCatalogRecord
	if len(raw) > desktopAgentCatalogMaxBytes || decodeDesktopJSON([]byte(raw), &record) != nil || record.SchemaVersion != 1 || !desktopAgentString(record.Revision, 128) {
		return ErrDesktopAgentInvalid
	}
	if record.SourceURL != "" {
		// A retained cache is safe to read after the outbound allowlist changes.
		// The destination is revalidated against current policy before any fetch.
		formatOnly := &DesktopAgentProfileCatalogService{}
		if _, err = formatOnly.validateSource(record.SourceURL); err != nil {
			return err
		}
	}
	if err = validateDesktopAgentCatalogProfiles(record.Profiles); err != nil {
		return err
	}
	merged, err := mergeDesktopAgentCatalog(desktopAgentBuiltinProfiles(), record.Profiles)
	if err != nil {
		return err
	}
	s.mu.Lock()
	s.profiles = merged
	s.persistedRaw = raw
	s.status = DesktopAgentProfileCatalogStatus{SourceURL: record.SourceURL, Revision: record.Revision, ProfileCount: len(merged), LastSuccessAt: record.LastSuccessAt, UsingFallback: record.LastSuccessAt == ""}
	s.mu.Unlock()
	return nil
}

func (s *DesktopAgentProfileCatalogService) Start() {
	s.once.Do(func() {
		s.wg.Add(1)
		go func() {
			defer s.wg.Done()
			_ = s.Refresh(s.ctx)
			ticker := time.NewTicker(10 * time.Minute)
			defer ticker.Stop()
			for {
				select {
				case <-s.ctx.Done():
					return
				case <-ticker.C:
					_ = s.Refresh(s.ctx)
				}
			}
		}()
	})
}
func (s *DesktopAgentProfileCatalogService) Stop() { s.stopOnce.Do(func() { s.cancel() }); s.wg.Wait() }
func (s *DesktopAgentProfileCatalogService) Snapshot() []DesktopAgentProfile {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return cloneDesktopAgentProfiles(s.profiles)
}
func (s *DesktopAgentProfileCatalogService) Status() DesktopAgentProfileCatalogStatus {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.status
}
func (s *DesktopAgentProfileCatalogService) recordError(code string) error {
	s.mu.Lock()
	s.status.LastError = code
	s.mu.Unlock()
	return fmt.Errorf("desktop profile catalog: %s", code)
}

func (s *DesktopAgentProfileCatalogService) validateSource(raw string) (string, error) {
	u, err := url.Parse(raw)
	if err != nil || u.Scheme != "https" || u.Hostname() == "" || u.User != nil || u.RawQuery != "" || u.ForceQuery || u.Fragment != "" || u.RawPath != "" || raw != strings.TrimSpace(raw) {
		return "", ErrDesktopAgentInvalid
	}
	opts := urlvalidator.ValidationOptions{AllowPrivate: false}
	if s.cfg != nil && s.cfg.Security.URLAllowlist.Enabled {
		opts.AllowedHosts = s.cfg.Security.URLAllowlist.PricingHosts
		opts.RequireAllowlist = true
	}
	return urlvalidator.ValidateHTTPSURL(raw, opts)
}

func (s *DesktopAgentProfileCatalogService) Refresh(ctx context.Context) error {
	s.updateMu.Lock()
	defer s.updateMu.Unlock()
	// Pick up another instance's accepted catalog/source before publishing.
	if err := s.Initialize(ctx); err != nil {
		return s.recordError("cache_unavailable_or_invalid")
	}
	s.mu.RLock()
	status := s.status
	previousProfiles := cloneDesktopAgentProfiles(s.profiles)
	previousRaw := s.persistedRaw
	s.mu.RUnlock()
	if status.SourceURL == "" {
		return nil
	}
	s.mu.Lock()
	s.status.LastCheckedAt = time.Now().UTC().Format(time.RFC3339)
	s.mu.Unlock()
	source, err := s.validateSource(status.SourceURL)
	if err != nil {
		return s.recordError("source_invalid")
	}
	if s.client == nil {
		return s.recordError("client_unavailable")
	}
	requestCtx, cancel := context.WithTimeout(ctx, 20*time.Second)
	defer cancel()
	stop := context.AfterFunc(s.ctx, cancel)
	defer stop()
	req, err := http.NewRequestWithContext(requestCtx, http.MethodGet, source, nil)
	if err != nil {
		return s.recordError("source_invalid")
	}
	req.Header.Set("Accept", "application/json")
	response, err := s.client.Do(req)
	if err != nil {
		return s.recordError("fetch_failed")
	}
	defer func() { _ = response.Body.Close() }()
	if response.StatusCode != http.StatusOK {
		return s.recordError("fetch_failed")
	}
	body, err := io.ReadAll(io.LimitReader(response.Body, desktopAgentCatalogMaxBytes+1))
	if err != nil || len(body) > desktopAgentCatalogMaxBytes {
		return s.recordError("response_too_large_or_unreadable")
	}
	var document DesktopAgentProfileCatalogDocument
	if decodeDesktopJSON(body, &document) != nil || document.SchemaVersion != 1 || !desktopAgentString(document.Revision, 128) || validateDesktopAgentCatalogProfiles(document.Profiles) != nil {
		return s.recordError("catalog_invalid")
	}
	merged, err := mergeDesktopAgentCatalog(previousProfiles, document.Profiles)
	if err != nil {
		return s.recordError("catalog_conflict")
	}
	var expectedConfig *string
	if s.validate != nil {
		raw, validationErr := s.validate(requestCtx, merged)
		if validationErr != nil {
			return s.recordError("configuration_conflict")
		}
		expectedConfig = &raw
	}
	record := desktopAgentProfileCatalogRecord{SchemaVersion: 1, SourceURL: status.SourceURL, Revision: document.Revision, Profiles: merged, LastSuccessAt: time.Now().UTC().Format(time.RFC3339)}
	persistedRaw, err := s.persist(requestCtx, previousRaw, record, expectedConfig)
	if err != nil {
		return s.recordError("storage_failed")
	}
	s.publish(record, persistedRaw)
	return nil
}

// Persistence never changes the observable snapshot. The caller publishes the
// accepted profiles, CAS guard and status together after the database commits.
func (s *DesktopAgentProfileCatalogService) persist(ctx context.Context, previousRaw string, record desktopAgentProfileCatalogRecord, expectedConfig *string) (string, error) {
	if s.repo == nil {
		return "", errors.New("catalog persistence unavailable")
	}
	body, err := json.Marshal(record)
	if err != nil {
		return "", err
	}
	if len(body) > desktopAgentCatalogMaxBytes {
		return "", ErrDesktopAgentInvalid
	}
	if repo, ok := s.repo.(interface {
		CompareAndSwapDesktopAgentProfileCatalog(context.Context, string, string, ...string) (bool, error)
	}); ok {
		var configGuard []string
		if expectedConfig != nil {
			configGuard = []string{*expectedConfig}
		}
		changed, err := repo.CompareAndSwapDesktopAgentProfileCatalog(ctx, previousRaw, string(body), configGuard...)
		if err != nil {
			return "", err
		}
		if !changed {
			return "", errors.New("catalog changed concurrently")
		}
	} else {
		if err := s.repo.Set(ctx, SettingKeyDesktopAgentProfileCatalog, string(body)); err != nil {
			return "", err
		}
	}
	return string(body), nil
}

func (s *DesktopAgentProfileCatalogService) publish(record desktopAgentProfileCatalogRecord, raw string) {
	s.mu.Lock()
	s.profiles = record.Profiles
	s.persistedRaw = raw
	s.status.SourceURL = record.SourceURL
	s.status.Revision = record.Revision
	s.status.ProfileCount = len(record.Profiles)
	s.status.LastSuccessAt = record.LastSuccessAt
	s.status.LastError = ""
	s.status.UsingFallback = record.LastSuccessAt == ""
	s.mu.Unlock()
}
func (s *DesktopAgentProfileCatalogService) SetSource(ctx context.Context, source string) error {
	if source != "" {
		if _, err := s.validateSource(source); err != nil {
			return ErrDesktopAgentInvalid
		}
	}
	s.updateMu.Lock()
	defer s.updateMu.Unlock()
	if err := s.Initialize(ctx); err != nil {
		return s.recordError("cache_unavailable_or_invalid")
	}
	s.mu.RLock()
	current := s.status
	profiles := cloneDesktopAgentProfiles(s.profiles)
	previousRaw := s.persistedRaw
	s.mu.RUnlock()
	record := desktopAgentProfileCatalogRecord{SchemaVersion: 1, SourceURL: source, Revision: current.Revision, Profiles: profiles, LastSuccessAt: current.LastSuccessAt}
	persistedRaw, err := s.persist(ctx, previousRaw, record, nil)
	if err != nil {
		return s.recordError("storage_failed")
	}
	s.publish(record, persistedRaw)
	return nil
}

func cloneDesktopAgentProfiles(profiles []DesktopAgentProfile) []DesktopAgentProfile {
	body, _ := json.Marshal(profiles)
	var cloned []DesktopAgentProfile
	_ = json.Unmarshal(body, &cloned)
	return cloned
}
func desktopAgentProfileList(catalog ...[]DesktopAgentProfile) []DesktopAgentProfile {
	if len(catalog) > 0 && catalog[0] != nil {
		return catalog[0]
	}
	return desktopAgentBuiltinProfiles()
}

func validateDesktopAgentCatalogProfiles(profiles []DesktopAgentProfile) error {
	if len(profiles) == 0 || len(profiles) > 1024 {
		return ErrDesktopAgentInvalid
	}
	ids := map[string]bool{}
	aliases := map[string]bool{}
	hosts := map[string][]string{"official_openai": {"developers.openai.com", "platform.openai.com"}, "official_deepseek": {"api-docs.deepseek.com"}, "official_anthropic": {"platform.claude.com", "docs.anthropic.com"}, "official_xai": {"docs.x.ai"}, "official_google": {"ai.google.dev", "aistudio.google.com"}}
	for _, p := range profiles {
		if !desktopAgentString(p.ID, 128) || !desktopAgentString(p.DisplayName, 128) || ids[p.ID] || p.Model.ID != p.ID || validateDesktopAgentModel(&p.Model) != nil || len(p.Aliases) == 0 || len(p.Aliases) > 32 || !slices.Contains(p.Aliases, p.ID) || !desktopAgentString(p.Revision, 128) {
			return ErrDesktopAgentInvalid
		}
		ids[p.ID] = true
		if _, err := time.Parse("2006-01-02", p.ReviewedAt); err != nil {
			return ErrDesktopAgentInvalid
		}
		u, err := url.Parse(p.SourceURL)
		if err != nil || u.Scheme != "https" || u.User != nil || u.Port() != "" || u.RawQuery != "" || u.Fragment != "" || !slices.Contains(hosts[p.Source], u.Hostname()) {
			return ErrDesktopAgentInvalid
		}
		for _, v := range []*int{p.OfficialContextWindow, p.MaxInputTokens, p.OfficialMaxOutputTokens} {
			if v != nil && (*v <= 0 || *v > 100000000) {
				return ErrDesktopAgentInvalid
			}
		}
		if p.OfficialContextWindow != nil && p.MaxInputTokens != nil && *p.MaxInputTokens > *p.OfficialContextWindow {
			return ErrDesktopAgentInvalid
		}
		if p.OfficialContextWindow != nil && p.Model.ContextWindow != *p.OfficialContextWindow {
			return ErrDesktopAgentInvalid
		}
		if p.OfficialContextWindow == nil && (p.MaxInputTokens == nil || p.Model.ContextWindow != *p.MaxInputTokens) {
			return ErrDesktopAgentInvalid
		}
		if p.OfficialMaxOutputTokens != nil && p.Model.MaxOutputTokens != *p.OfficialMaxOutputTokens {
			return ErrDesktopAgentInvalid
		}
		if p.OfficialMaxOutputTokens == nil && p.Model.MaxOutputTokens != 0 {
			return ErrDesktopAgentInvalid
		}
		for _, alias := range p.Aliases {
			if !desktopAgentString(alias, 128) || strings.ContainsAny(alias, " *?\\") || aliases[alias] {
				return ErrDesktopAgentInvalid
			}
			aliases[alias] = true
		}
		if len(p.Notes) > 32 || len(p.OfficialReasoningLevels) > 7 {
			return ErrDesktopAgentInvalid
		}
		for _, note := range p.Notes {
			if !desktopAgentString(note, 128) {
				return ErrDesktopAgentInvalid
			}
		}
		for _, level := range p.OfficialReasoningLevels {
			if !slices.Contains([]string{"none", "minimal", "low", "medium", "high", "xhigh", "max"}, level) {
				return ErrDesktopAgentInvalid
			}
		}
	}
	return nil
}

// Missing entries/aliases are retained; remote omissions can never silently
// delete an existing profile or transfer a public identity to another profile.
func mergeDesktopAgentCatalog(previous, incoming []DesktopAgentProfile) ([]DesktopAgentProfile, error) {
	byID := map[string]DesktopAgentProfile{}
	owners := map[string]string{}
	for _, p := range previous {
		byID[p.ID] = p
		for _, alias := range p.Aliases {
			owners[alias] = p.ID
		}
	}
	for _, p := range incoming {
		if old, ok := byID[p.ID]; ok {
			if old.Source != p.Source {
				return nil, ErrDesktopAgentInvalid
			}
			for _, alias := range old.Aliases {
				if !slices.Contains(p.Aliases, alias) {
					p.Aliases = append(p.Aliases, alias)
				}
			}
		}
		for _, alias := range p.Aliases {
			if owner, ok := owners[alias]; ok && owner != p.ID {
				return nil, ErrDesktopAgentInvalid
			}
			owners[alias] = p.ID
		}
		byID[p.ID] = p
	}
	merged := make([]DesktopAgentProfile, 0, len(byID))
	for _, p := range byID {
		merged = append(merged, p)
	}
	sort.Slice(merged, func(i, j int) bool { return merged[i].ID < merged[j].ID })
	if err := validateDesktopAgentCatalogProfiles(merged); err != nil {
		return nil, err
	}
	return cloneDesktopAgentProfiles(merged), nil
}

func (s *DesktopAgentProfileCatalogService) requestSnapshot() ([]DesktopAgentProfile, string) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return cloneDesktopAgentProfiles(s.profiles), s.persistedRaw
}
