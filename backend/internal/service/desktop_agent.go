package service

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"net/url"
	"reflect"
	"slices"
	"strconv"
	"strings"
	"time"
	"unicode"
	"unicode/utf8"

	infraerrors "github.com/Wei-Shaw/sub2api/internal/pkg/errors"
)

const SettingKeyDesktopAgentConfig = "desktop_agent_config_v1"
const DesktopAgentProviderID = "milorouter-managed"

var ErrDesktopAgentInvalid = infraerrors.BadRequest("DESKTOP_AGENT_INVALID", "invalid desktop agent request or configuration")

type DesktopAgentRequest struct {
	ClientVersion   string `json:"client_version"`
	ProtocolVersion int    `json:"protocol_version"`
	Platform        string `json:"platform"`
	InstallationID  string `json:"installation_id,omitempty"`
}

type DesktopAgentInput struct {
	Text  *bool `json:"text"`
	Image *bool `json:"image"`
	Video *bool `json:"video"`
	Audio *bool `json:"audio"`
	PDF   *bool `json:"pdf"`
}
type DesktopAgentOutput struct {
	Text *bool `json:"text"`
}

// Pointer booleans distinguish an explicit false capability from an omitted one.
// Only fully validated policy is returned; no server-supplied executable maps.
type DesktopAgentModel struct {
	ID                            string              `json:"id"`
	DisplayName                   string              `json:"display_name"`
	ContextWindow                 int                 `json:"context_window"`
	MaxOutputTokens               int                 `json:"max_output_tokens"`
	DefaultMaxOutputTokens        int                 `json:"default_max_output_tokens"`
	ReasoningLevels               []string            `json:"reasoning_levels"`
	DefaultReasoning              string              `json:"default_reasoning"`
	SupportsReasoning             *bool               `json:"supports_reasoning"`
	SupportsStreaming             *bool               `json:"supports_streaming"`
	RequiresMFJSToolSchema        *bool               `json:"requires_mfjs_tool_schema"`
	Input                         *DesktopAgentInput  `json:"input"`
	Output                        *DesktopAgentOutput `json:"output"`
	SupportsToolCall              *bool               `json:"supports_tool_call"`
	SupportsJSONSchemaOutput      *bool               `json:"supports_json_schema_output"`
	SupportsNativeWebSearch       *bool               `json:"supports_native_web_search"`
	SupportsMidConversationSystem *bool               `json:"supports_mid_conversation_system"`
}

type DesktopAgentConfig struct {
	Enabled        bool                `json:"enabled"`
	GroupID        int64               `json:"group_id"`
	BaseURL        string              `json:"base_url"`
	Models         []DesktopAgentModel `json:"models"`
	DefaultModelID string              `json:"default_model_id"`
	// Model only accepts historical persisted records and existing Go fixtures.
	// Administrative writes and every serialized response use the canonical list.
	Model *DesktopAgentModel `json:"model,omitempty"`
}

func normalizeDesktopAgentConfig(cfg DesktopAgentConfig) (DesktopAgentConfig, error) {
	if cfg.Model != nil {
		if cfg.Models != nil || cfg.DefaultModelID != "" {
			return cfg, ErrDesktopAgentInvalid
		}
		cfg.Models = []DesktopAgentModel{*cfg.Model}
		cfg.DefaultModelID = cfg.Model.ID
		cfg.Model = nil
	}
	if cfg.Models == nil {
		cfg.Models = []DesktopAgentModel{}
	}
	return cfg, nil
}

func (cfg DesktopAgentConfig) MarshalJSON() ([]byte, error) {
	cfg, err := normalizeDesktopAgentConfig(cfg)
	if err != nil {
		return nil, err
	}
	return json.Marshal(struct {
		Enabled        bool                `json:"enabled"`
		GroupID        int64               `json:"group_id"`
		BaseURL        string              `json:"base_url"`
		Models         []DesktopAgentModel `json:"models"`
		DefaultModelID string              `json:"default_model_id"`
	}{cfg.Enabled, cfg.GroupID, cfg.BaseURL, cfg.Models, cfg.DefaultModelID})
}

type DesktopAgentProvider struct {
	ID      string `json:"id"`
	APIType string `json:"api_type"`
	BaseURL string `json:"base_url"`
}
type DesktopAgentSelection struct {
	ModelID         string `json:"model_id"`
	ReasoningLevel  string `json:"reasoning_level"`
	MaxOutputTokens int    `json:"max_output_tokens"`
	Auxiliary       string `json:"auxiliary"`
}
type DesktopAgentCredential struct {
	ID        string     `json:"id"`
	Key       string     `json:"key"`
	Scope     string     `json:"scope"`
	ExpiresAt *time.Time `json:"expires_at"`
}
type DesktopAgentBootstrap struct {
	SchemaVersion   int                     `json:"schema_version"`
	ConfigRevision  string                  `json:"config_revision"`
	ServiceStatus   string                  `json:"service_status"`
	ReasonCode      string                  `json:"reason_code"`
	Provider        *DesktopAgentProvider   `json:"provider,omitempty"`
	Models          []DesktopAgentModel     `json:"models,omitempty"`
	SelectionPolicy *DesktopAgentSelection  `json:"selection_policy,omitempty"`
	Credential      *DesktopAgentCredential `json:"credential,omitempty"`
}

type desktopAgentUserReader interface {
	GetByID(context.Context, int64) (*User, error)
}
type desktopAgentGroupReader interface {
	GetByID(context.Context, int64) (*Group, error)
}
type DesktopAgentCredentialProvider interface {
	GetDesktopAgentCredential(context.Context, int64, int64) (*APIKey, string, error)
}

type DesktopAgentService struct {
	settings    *SettingService
	users       desktopAgentUserReader
	groups      desktopAgentGroupReader
	credentials DesktopAgentCredentialProvider
	catalog     DesktopAgentModelCatalog
}

func NewDesktopAgentService(settings *SettingService, users desktopAgentUserReader, groups desktopAgentGroupReader, credentials DesktopAgentCredentialProvider, catalogs ...DesktopAgentModelCatalog) *DesktopAgentService {
	s := &DesktopAgentService{settings: settings, users: users, groups: groups, credentials: credentials}
	if len(catalogs) > 0 {
		s.catalog = catalogs[0]
	}
	return s
}

func desktopAgentString(value string, max int) bool {
	if value == "" || len(value) > max || value != strings.TrimSpace(value) || !utf8.ValidString(value) {
		return false
	}
	for _, ch := range value {
		if unicode.IsControl(ch) {
			return false
		}
	}
	return true
}

// decodeDesktopJSON rejects ambiguous JSON (duplicate/null fields) before strict
// DTO decoding. 凭据与权限合同不能依赖最后一个重复字段或 null 的隐式零值。
func decodeDesktopJSON(data []byte, target any) error {
	trimmed := bytes.TrimSpace(data)
	if len(trimmed) == 0 || trimmed[0] != '{' {
		return ErrDesktopAgentInvalid
	}
	d := json.NewDecoder(bytes.NewReader(trimmed))
	var visit func(int) error
	visit = func(depth int) error {
		if depth > 8 {
			return ErrDesktopAgentInvalid
		}
		token, err := d.Token()
		if err != nil || token == nil {
			return ErrDesktopAgentInvalid
		}
		delim, ok := token.(json.Delim)
		if !ok {
			return nil
		}
		switch delim {
		case '{':
			seen := map[string]bool{}
			for d.More() {
				key, err := d.Token()
				if err != nil {
					return ErrDesktopAgentInvalid
				}
				name, ok := key.(string)
				if !ok || seen[name] {
					return ErrDesktopAgentInvalid
				}
				seen[name] = true
				if err := visit(depth + 1); err != nil {
					return err
				}
			}
		case '[':
			for d.More() {
				if err := visit(depth + 1); err != nil {
					return err
				}
			}
		default:
			return ErrDesktopAgentInvalid
		}
		end, err := d.Token()
		if err != nil || (delim == '{' && end != json.Delim('}')) || (delim == '[' && end != json.Delim(']')) {
			return ErrDesktopAgentInvalid
		}
		return nil
	}
	if err := visit(0); err != nil {
		return err
	}
	if _, err := d.Token(); err != io.EOF {
		return ErrDesktopAgentInvalid
	}
	d = json.NewDecoder(bytes.NewReader(trimmed))
	d.DisallowUnknownFields()
	if d.Decode(target) != nil {
		return ErrDesktopAgentInvalid
	}
	var shape any
	if json.Unmarshal(trimmed, &shape) != nil || validateDesktopJSONShape(shape, reflect.TypeOf(target)) != nil {
		return ErrDesktopAgentInvalid
	}
	return nil
}

func validateDesktopJSONShape(value any, typ reflect.Type) error {
	for typ.Kind() == reflect.Pointer {
		typ = typ.Elem()
	}
	if typ.Kind() == reflect.Struct {
		object, ok := value.(map[string]any)
		if !ok {
			return ErrDesktopAgentInvalid
		}
		fields := make(map[string]reflect.Type, typ.NumField())
		for i := 0; i < typ.NumField(); i++ {
			field := typ.Field(i)
			fields[strings.Split(field.Tag.Get("json"), ",")[0]] = field.Type
		}
		for name, child := range object {
			fieldType, exists := fields[name]
			// encoding/json 默许大小写别名；权限配置要求精确字段，避免
			// platform/Platform 一起出现时绕过重复字段校验。
			if !exists || validateDesktopJSONShape(child, fieldType) != nil {
				return ErrDesktopAgentInvalid
			}
		}
	} else if typ.Kind() == reflect.Slice {
		items, ok := value.([]any)
		if !ok {
			return ErrDesktopAgentInvalid
		}
		for _, child := range items {
			if validateDesktopJSONShape(child, typ.Elem()) != nil {
				return ErrDesktopAgentInvalid
			}
		}
	}
	return nil
}

func ParseDesktopAgentRequest(data []byte) (DesktopAgentRequest, error) {
	var req DesktopAgentRequest
	if len(data) > 2048 {
		return req, ErrDesktopAgentInvalid
	}
	if err := decodeDesktopJSON(data, &req); err != nil {
		return req, err
	}
	if !desktopAgentString(req.ClientVersion, 128) || (req.ProtocolVersion != 1 && req.ProtocolVersion != 2) || (req.Platform != "win32" && req.Platform != "darwin") || (req.InstallationID != "" && !desktopAgentString(req.InstallationID, 128)) {
		return req, ErrDesktopAgentInvalid
	}
	var fields map[string]json.RawMessage
	_ = json.Unmarshal(data, &fields)
	if _, exists := fields["installation_id"]; exists && req.InstallationID == "" {
		return req, ErrDesktopAgentInvalid
	}
	return req, nil
}
func ParseDesktopAgentConfig(data []byte) (DesktopAgentConfig, error) {
	var cfg DesktopAgentConfig
	if len(data) > 131072 {
		return cfg, ErrDesktopAgentInvalid
	}
	if err := decodeDesktopJSON(data, &cfg); err != nil {
		return cfg, err
	}
	// enabled must be explicit; an omitted switch is not an administrative disable.
	var fields map[string]json.RawMessage
	_ = json.Unmarshal(data, &fields)
	if _, ok := fields["enabled"]; !ok {
		return cfg, ErrDesktopAgentInvalid
	}
	if _, legacy := fields["model"]; legacy {
		if _, mixed := fields["models"]; mixed {
			return cfg, ErrDesktopAgentInvalid
		}
		if _, mixed := fields["default_model_id"]; mixed {
			return cfg, ErrDesktopAgentInvalid
		}
	}
	cfg, err := normalizeDesktopAgentConfig(cfg)
	if err != nil {
		return cfg, err
	}
	if err := validateDesktopAgentConfig(cfg); err != nil {
		return cfg, err
	}
	return cfg, nil
}

// ParseDesktopAgentAdminConfig rejects old UI payloads rather than collapsing a
// multi-model policy into the historical single-model shape.
func ParseDesktopAgentAdminConfig(data []byte) (DesktopAgentConfig, error) {
	cfg, err := ParseDesktopAgentConfig(data)
	if err != nil {
		return cfg, err
	}
	var fields map[string]json.RawMessage
	_ = json.Unmarshal(data, &fields)
	if _, legacy := fields["model"]; legacy {
		return cfg, ErrDesktopAgentInvalid
	}
	for _, name := range []string{"enabled", "group_id", "base_url", "models", "default_model_id"} {
		if _, ok := fields[name]; !ok {
			return cfg, ErrDesktopAgentInvalid
		}
	}
	return cfg, nil
}
func validateDesktopAgentConfig(cfg DesktopAgentConfig) error {
	var err error
	cfg, err = normalizeDesktopAgentConfig(cfg)
	if err != nil {
		return err
	}
	if cfg.GroupID < 0 {
		return ErrDesktopAgentInvalid
	}
	if !cfg.Enabled && cfg.GroupID == 0 && cfg.BaseURL == "" && len(cfg.Models) == 0 && cfg.DefaultModelID == "" {
		return nil
	}
	u, err := url.Parse(cfg.BaseURL)
	if err != nil || u.Scheme != "https" || u.Hostname() == "" || u.User != nil || u.RawQuery != "" || u.Fragment != "" || u.RawPath != "" || u.Path != "/v1" || u.ForceQuery || cfg.GroupID <= 0 {
		return ErrDesktopAgentInvalid
	}
	if len(cfg.Models) == 0 || len(cfg.Models) > 32 {
		return ErrDesktopAgentInvalid
	}
	seen := make(map[string]bool, len(cfg.Models))
	for i := range cfg.Models {
		m := &cfg.Models[i]
		if seen[m.ID] {
			return ErrDesktopAgentInvalid
		}
		seen[m.ID] = true
		if err := validateDesktopAgentModel(m); err != nil {
			return err
		}
	}
	if !seen[cfg.DefaultModelID] {
		return ErrDesktopAgentInvalid
	}
	return nil
}

func validateDesktopAgentModel(m *DesktopAgentModel) error {
	if m == nil || !desktopAgentString(m.ID, 128) || strings.ContainsAny(m.ID, " *?\\") || !desktopAgentString(m.DisplayName, 128) || m.ContextWindow <= 0 || m.ContextWindow > 100000000 || m.MaxOutputTokens <= 0 || m.MaxOutputTokens >= m.ContextWindow || m.DefaultMaxOutputTokens <= 0 || m.DefaultMaxOutputTokens > m.MaxOutputTokens {
		return ErrDesktopAgentInvalid
	}
	booleans := []*bool{m.SupportsReasoning, m.SupportsStreaming, m.RequiresMFJSToolSchema, m.SupportsToolCall, m.SupportsJSONSchemaOutput, m.SupportsNativeWebSearch, m.SupportsMidConversationSystem}
	if m.Input == nil || m.Output == nil {
		return ErrDesktopAgentInvalid
	}
	booleans = append(booleans, m.Input.Text, m.Input.Image, m.Input.Video, m.Input.Audio, m.Input.PDF, m.Output.Text)
	for _, value := range booleans {
		if value == nil {
			return ErrDesktopAgentInvalid
		}
	}
	if !*m.Input.Text || !*m.Output.Text || !*m.SupportsToolCall || !*m.SupportsStreaming {
		return ErrDesktopAgentInvalid
	}
	order := []string{"none", "minimal", "low", "medium", "high", "xhigh", "max"}
	previous := -1
	if len(m.ReasoningLevels) == 0 || len(m.ReasoningLevels) > len(order) {
		return ErrDesktopAgentInvalid
	}
	for _, level := range m.ReasoningLevels {
		index := slices.Index(order, level)
		if index <= previous {
			return ErrDesktopAgentInvalid
		}
		previous = index
	}
	if !slices.Contains(m.ReasoningLevels, m.DefaultReasoning) {
		return ErrDesktopAgentInvalid
	}
	if !*m.SupportsReasoning && (len(m.ReasoningLevels) != 1 || m.ReasoningLevels[0] != "none") {
		return ErrDesktopAgentInvalid
	}
	return nil
}
func (s *DesktopAgentService) GetConfig(ctx context.Context) (*DesktopAgentConfig, error) {
	values, err := s.settings.settingRepo.GetMultiple(ctx, []string{SettingKeyDesktopAgentConfig})
	if err != nil {
		return nil, err
	}
	raw := values[SettingKeyDesktopAgentConfig]
	if raw == "" {
		return &DesktopAgentConfig{}, nil
	}
	cfg, err := ParseDesktopAgentConfig([]byte(raw))
	if err != nil {
		return nil, err
	}
	return &cfg, nil
}
func (s *DesktopAgentService) validateGroup(ctx context.Context, cfg DesktopAgentConfig) (*Group, string, error) {
	group, err := s.groups.GetByID(ctx, cfg.GroupID)
	if err != nil {
		if errors.Is(err, ErrGroupNotFound) {
			return nil, "group_unavailable", nil
		}
		return nil, "", err
	}
	if group == nil || group.ID != cfg.GroupID || !group.IsActive() || group.SubscriptionType != SubscriptionTypeStandard || (group.Platform != PlatformOpenAI && group.Platform != PlatformComposite) {
		return nil, "group_unavailable", nil
	}
	if s.catalog == nil {
		return nil, "", errors.New("desktop model catalog unavailable")
	}
	models, err := s.catalog.ModelsForGroup(ctx, group)
	if err != nil {
		return nil, "", err
	}
	for i := range cfg.Models {
		model := &cfg.Models[i]
		if !group.ModelAllowlist.Allows(model.ID) || !slices.Contains(models, model.ID) {
			return nil, "model_forbidden", nil
		}
		if *model.SupportsReasoning {
			for _, effort := range model.ReasoningLevels {
				request := map[string]any{"model": model.ID}
				// 客户端 none 表示沿用上游默认，实际不会发送 effort；按相同
				// 请求形状校验，避免误命中分组对显式 none 设置的映射或拒绝。
				if effort != "none" {
					request["reasoning"] = map[string]string{"effort": effort}
				}
				body, _ := json.Marshal(request)
				_, changed, err := ApplyReasoningEffortPolicy(body, group.MaxReasoningEffort, group.ReasoningEffortMappings, group.MaxReasoningEffortOverLimit)
				if err != nil || changed {
					// 下发档位必须和网关实际执行一致，不把静默降档当作已支持。
					return nil, "configuration_invalid", nil
				}
			}
		}
	}
	return group, "", nil
}
func (s *DesktopAgentService) GetModels(ctx context.Context, groupID int64) ([]string, error) {
	if groupID <= 0 {
		return nil, ErrDesktopAgentInvalid
	}
	group, err := s.groups.GetByID(ctx, groupID)
	if err != nil {
		return nil, err
	}
	if group == nil || group.ID != groupID || !group.IsActive() || group.SubscriptionType != SubscriptionTypeStandard || (group.Platform != PlatformOpenAI && group.Platform != PlatformComposite) {
		return nil, ErrDesktopAgentInvalid
	}
	if s.catalog == nil {
		return nil, errors.New("desktop model catalog unavailable")
	}
	return s.catalog.ModelsForGroup(ctx, group)
}
func (s *DesktopAgentService) UpdateConfig(ctx context.Context, cfg DesktopAgentConfig) (*DesktopAgentConfig, error) {
	var err error
	cfg, err = normalizeDesktopAgentConfig(cfg)
	if err != nil {
		return nil, err
	}
	if err := validateDesktopAgentConfig(cfg); err != nil {
		return nil, err
	}
	if cfg.Enabled || len(cfg.Models) > 0 {
		_, reason, err := s.validateGroup(ctx, cfg)
		if err != nil {
			return nil, err
		}
		if reason != "" {
			return nil, ErrDesktopAgentInvalid
		}
	}
	data, err := json.Marshal(cfg)
	if err != nil {
		return nil, err
	}
	if err = s.settings.settingRepo.SetMultiple(ctx, map[string]string{SettingKeyDesktopAgentConfig: string(data)}); err != nil {
		return nil, err
	}
	return &cfg, nil
}
func (s *DesktopAgentService) Bootstrap(ctx context.Context, userID int64) (*DesktopAgentBootstrap, error) {
	return s.BootstrapForProtocol(ctx, userID, 1)
}
func (s *DesktopAgentService) BootstrapForProtocol(ctx context.Context, userID int64, protocol int) (*DesktopAgentBootstrap, error) {
	if protocol != 1 && protocol != 2 {
		return nil, ErrDesktopAgentInvalid
	}
	cfg, err := s.GetConfig(ctx)
	if err != nil {
		if errors.Is(err, ErrDesktopAgentInvalid) {
			return &DesktopAgentBootstrap{SchemaVersion: protocol, ConfigRevision: "invalid", ServiceStatus: "not_configured", ReasonCode: "configuration_invalid"}, nil
		}
		return nil, err
	}
	raw, _ := json.Marshal(cfg)
	hash := sha256.Sum256(raw)
	result := &DesktopAgentBootstrap{SchemaVersion: protocol, ConfigRevision: hex.EncodeToString(hash[:]), ServiceStatus: "disabled", ReasonCode: "disabled"}
	if !cfg.Enabled {
		return result, nil
	}
	unavailable := func(status, reason string) (*DesktopAgentBootstrap, error) {
		result.ServiceStatus = status
		result.ReasonCode = reason
		return result, nil
	}
	if len(cfg.Models) == 0 || cfg.GroupID <= 0 {
		return unavailable("not_configured", "configuration_missing")
	}
	user, err := s.users.GetByID(ctx, userID)
	if err != nil {
		if errors.Is(err, ErrUserNotFound) {
			return unavailable("forbidden", "user_forbidden")
		}
		return nil, err
	}
	if user == nil || user.ID != userID || !user.IsActive() || user.DeletedAt != nil {
		return unavailable("forbidden", "user_forbidden")
	}
	group, reason, err := s.validateGroup(ctx, *cfg)
	if err != nil {
		return nil, err
	}
	if reason != "" {
		return unavailable("not_configured", reason)
	}
	if !user.CanBindGroup(group.ID, group.IsExclusive) {
		return unavailable("forbidden", "group_forbidden")
	}
	if s.credentials == nil {
		return unavailable("not_configured", "configuration_missing")
	}
	key, scope, err := s.credentials.GetDesktopAgentCredential(ctx, userID, group.ID)
	if errors.Is(err, ErrDesktopAgentPolicyChanged) {
		return unavailable("not_configured", "configuration_changed")
	}
	if errors.Is(err, ErrGroupNotAllowed) || errors.Is(err, ErrInsufficientPerms) {
		return unavailable("forbidden", "group_forbidden")
	}
	if err != nil {
		return nil, err
	}
	// 领取可能等待数据库锁；不能将已经更新/关闭的旧策略作为 ready 返回。
	current, err := s.GetConfig(ctx)
	if err != nil {
		return nil, err
	}
	if !reflect.DeepEqual(cfg, current) {
		return unavailable("not_configured", "configuration_changed")
	}
	// 同样复查可能在领取期间变更的用户/模型组权限；失败不返回凭据。
	user, err = s.users.GetByID(ctx, userID)
	if err != nil {
		return nil, err
	}
	if user == nil || user.ID != userID || !user.IsActive() || user.DeletedAt != nil {
		return unavailable("forbidden", "user_forbidden")
	}
	group, reason, err = s.validateGroup(ctx, *cfg)
	if err != nil {
		return nil, err
	}
	if reason != "" {
		return unavailable("not_configured", reason)
	}
	if !user.CanBindGroup(group.ID, group.IsExclusive) {
		return unavailable("forbidden", "group_forbidden")
	}
	if key == nil || key.UserID != userID || key.ID <= 0 || key.Key == "" || scope != APIKeyPurposeDesktopAgent || key.Purpose != APIKeyPurposeDesktopAgent || key.KeyType != APIKeyTypeSystem {
		return unavailable("not_configured", "configuration_invalid")
	}
	if key.Status == StatusAPIKeyExpired {
		return unavailable("credential_blocked", "credential_expired")
	}
	if key.Status == StatusAPIKeyQuotaExhausted {
		return unavailable("credential_blocked", "credential_quota_exhausted")
	}
	if !key.IsActive() {
		return unavailable("credential_blocked", "credential_disabled")
	}
	if key.IsExpired() {
		return unavailable("credential_blocked", "credential_expired")
	}
	if key.IsQuotaExhausted() {
		return unavailable("credential_blocked", "credential_quota_exhausted")
	}
	if key.GroupID == nil || *key.GroupID != group.ID {
		return unavailable("not_configured", "configuration_invalid")
	}
	result.ServiceStatus = "ready"
	result.ReasonCode = "ready"
	result.Provider = &DesktopAgentProvider{ID: DesktopAgentProviderID, APIType: "openai-responses", BaseURL: cfg.BaseURL}
	var selected DesktopAgentModel
	for _, model := range cfg.Models {
		if model.ID == cfg.DefaultModelID {
			selected = model
			break
		}
	}
	result.Models = cfg.Models
	if protocol == 1 {
		result.Models = []DesktopAgentModel{selected}
	}
	result.SelectionPolicy = &DesktopAgentSelection{ModelID: selected.ID, ReasoningLevel: selected.DefaultReasoning, MaxOutputTokens: selected.DefaultMaxOutputTokens, Auxiliary: "inherit-main"}
	result.Credential = &DesktopAgentCredential{ID: strconv.FormatInt(key.ID, 10), Key: key.Key, Scope: scope, ExpiresAt: key.ExpiresAt}
	return result, nil
}

// DisableConfig changes only the latest persisted switch. CAS retries preserve
// a concurrent save; unsaved browser fields never participate in this operation.
func (s *DesktopAgentService) DisableConfig(ctx context.Context) (*DesktopAgentConfig, error) {
	repo, ok := s.settings.settingRepo.(interface {
		CompareAndSwapDesktopAgentConfig(context.Context, string, string) (bool, error)
	})
	if !ok {
		return nil, errors.New("desktop configuration atomic update unavailable")
	}
	for attempt := 0; attempt < 4; attempt++ {
		values, err := s.settings.settingRepo.GetMultiple(ctx, []string{SettingKeyDesktopAgentConfig})
		if err != nil {
			return nil, err
		}
		raw := values[SettingKeyDesktopAgentConfig]
		if raw == "" {
			return &DesktopAgentConfig{Models: []DesktopAgentModel{}}, nil
		}
		var fields map[string]json.RawMessage
		if json.Unmarshal([]byte(raw), &fields) != nil || fields == nil {
			return nil, ErrDesktopAgentInvalid
		}
		fields["enabled"] = json.RawMessage("false")
		data, err := json.Marshal(fields)
		if err != nil {
			return nil, err
		}
		cfg, err := ParseDesktopAgentConfig(data)
		if err != nil {
			return nil, err
		}
		// CAS防止读取后有新模型配置保存时，旧快照关闭覆盖新集合。
		changed, err := repo.CompareAndSwapDesktopAgentConfig(ctx, raw, string(data))
		if err != nil {
			return nil, err
		}
		if changed {
			return &cfg, nil
		}
	}
	return nil, errors.New("desktop configuration changed concurrently")
}
