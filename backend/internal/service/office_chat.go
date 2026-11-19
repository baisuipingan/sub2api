package service

import (
	"context"
	"encoding/json"
	"errors"
	"path"
	"slices"
	"strings"
	"unicode/utf8"

	"github.com/Wei-Shaw/sub2api/internal/config"
	"github.com/google/uuid"
)

type WorkbenchChatStatus struct {
	Enabled        bool  `json:"enabled"`
	OfficeEnabled  bool  `json:"office_enabled"`
	SearchEnabled  bool  `json:"search_enabled"`
	FilesEnabled   bool  `json:"files_enabled"`
	UploadsEnabled bool  `json:"uploads_enabled"`
	MaxFileBytes   int64 `json:"max_file_bytes"`
	MaxFiles       int   `json:"max_files"`
}
type WorkbenchChatModel struct {
	ID               string   `json:"id"`
	DisplayName      string   `json:"display_name"`
	SupportsTools    bool     `json:"supports_tools"`
	SupportsVision   bool     `json:"supports_vision"`
	ReasoningEfforts []string `json:"reasoning_efforts"`
	ContextWindow    *int     `json:"context_window,omitempty"`
	MaxOutputTokens  *int     `json:"max_output_tokens,omitempty"`
}

func (s *OfficeAgentService) BindWorkbenchProfiles(profiles *DesktopAgentProfileCatalogService) {
	s.profiles = profiles
}
func (s *OfficeAgentService) BindResourcePin(pin func(context.Context, int64, string) error) {
	s.pinInputs = pin
}
func (s *OfficeAgentService) ChatStatus(ctx context.Context) (WorkbenchChatStatus, error) {
	result := WorkbenchChatStatus{MaxFileBytes: 24 << 20, MaxFiles: 8}
	if s.settings == nil {
		return result, nil
	}
	cfg, e := s.settings.GetWorkbenchConfig(ctx)
	if e != nil {
		return result, e
	}
	result.Enabled = s.Enabled() && cfg.ChatEnabled
	result.OfficeEnabled = result.Enabled && cfg.OfficeEnabled
	result.SearchEnabled = result.Enabled && cfg.SearchEnabled && s.settings.IsWebSearchEmulationEnabled(ctx) && getWebSearchManager().HasAvailableProvider()
	result.FilesEnabled = cfg.FilesEnabled
	result.UploadsEnabled = cfg.FilesEnabled
	return result, nil
}
func (s *OfficeAgentService) ChatModels(ctx context.Context) ([]WorkbenchChatModel, error) {
	status, e := s.ChatStatus(ctx)
	if e != nil {
		return nil, e
	}
	if !status.Enabled {
		return nil, ErrOfficeDisabled
	}
	cfg, e := s.settings.GetWorkbenchConfig(ctx)
	if e != nil {
		return nil, e
	}
	g, e := s.groups.GetByID(ctx, cfg.ImageStudioGroupID)
	if e != nil {
		return nil, e
	}
	ids, e := s.catalog.ModelsForGroup(ctx, g)
	if e != nil {
		return nil, e
	}
	profiles := desktopAgentBuiltinProfiles()
	if s.profiles != nil {
		profiles = s.profiles.Snapshot()
	}
	out := make([]WorkbenchChatModel, 0, len(ids))
	for _, id := range ids {
		// Use the advertised ID's public contract. Private supplier mappings
		// must not turn a text alias into an image option (or the reverse).
		if imageStudioImageModel(id) {
			continue
		}
		m := WorkbenchChatModel{ID: id, DisplayName: id, ReasoningEfforts: []string{}}
		if p := workbenchModelProfile(id, cfg.ModelProfiles, profiles); p != nil {
			m.DisplayName = p.DisplayName
			m.ContextWindow = p.OfficialContextWindow
			m.MaxOutputTokens = p.OfficialMaxOutputTokens
			m.ReasoningEfforts = slices.Clone(p.OfficialReasoningLevels)
			m.SupportsTools = p.Model.SupportsToolCall != nil && *p.Model.SupportsToolCall
			m.SupportsVision = p.Model.Input != nil && p.Model.Input.Image != nil && *p.Model.Input.Image
		}
		out = append(out, m)
	}
	return out, nil
}

type WorkbenchChatSubmit struct {
	TargetResourceVersionID string   `json:"target_resource_version_id,omitempty"`
	RunID                   string   `json:"run_id"`
	Model                   string   `json:"model"`
	Content                 string   `json:"content"`
	TaskMode                bool     `json:"task_mode"`
	WebSearch               bool     `json:"web_search"`
	ReasoningEffort         string   `json:"reasoning_effort,omitempty"`
	ResourceVersionIDs      []string `json:"resource_version_ids,omitempty"`
	RegenerateRunID         string   `json:"regenerate_run_id,omitempty"`
}

func (s *OfficeAgentService) SubmitChat(ctx context.Context, user int64, conversation string, in WorkbenchChatSubmit) (*OfficeRun, error) {
	status, e := s.ChatStatus(ctx)
	if e != nil {
		return nil, e
	}
	if !status.Enabled || in.TaskMode && !status.OfficeEnabled || in.WebSearch && !status.SearchEnabled {
		return nil, ErrOfficeDisabled
	}
	if user <= 0 || uuid.Validate(conversation) != nil || uuid.Validate(in.RunID) != nil || !officeModelID(in.Model) || strings.TrimSpace(in.Content) == "" || len(in.Content) > 32<<10 || !utf8.ValidString(in.Content) || len(in.ResourceVersionIDs) > 8 {
		return nil, ErrOfficeInvalid
	}
	if len(in.ResourceVersionIDs) > 0 && (!status.FilesEnabled || !in.TaskMode) {
		return nil, ErrOfficeDisabled
	}
	if _, e = s.repo.GetConversation(ctx, user, conversation); e != nil {
		return nil, e
	}
	models, e := s.ChatModels(ctx)
	if e != nil {
		return nil, e
	}
	var model *WorkbenchChatModel
	for i := range models {
		if models[i].ID == in.Model {
			model = &models[i]
			break
		}
	}
	if model == nil {
		return nil, ErrOfficeRouteChanged
	}
	if (in.TaskMode || in.WebSearch) && !model.SupportsTools {
		return nil, ErrOfficeInvalid
	}
	if in.ReasoningEffort != "" && !slices.Contains(model.ReasoningEfforts, in.ReasoningEffort) {
		return nil, ErrOfficeInvalid
	}
	key, group, e := s.resolve(ctx, user, in.Model)
	if e != nil {
		return nil, e
	}
	var boundary int64
	if in.RegenerateRunID != "" {
		repo, e := s.ChatRepository()
		if e != nil {
			return nil, e
		}
		old, question, e := repo.RegenerationSource(ctx, user, conversation, in.RegenerateRunID)
		if e != nil {
			return nil, e
		}
		in.RegenerateRunID = old.ID
		boundary = old.ContextBoundary
		var oldParams struct {
			OutputParent struct {
				VersionID string `json:"version_id"`
			} `json:"output_parent"`
		}
		if json.Unmarshal(old.Parameters, &oldParams) == nil && oldParams.OutputParent.VersionID != "" {
			in.TargetResourceVersionID = oldParams.OutputParent.VersionID
		}
		in.Content, e = officeMessageText(question.Content)
		if e != nil {
			return nil, e
		}
		in.ResourceVersionIDs = nil
		for _, ref := range old.InputResources {
			in.ResourceVersionIDs = append(in.ResourceVersionIDs, ref.VersionID)
		}
	}
	budgets, e := s.settings.GetWorkbenchConfig(ctx)
	if e != nil {
		return nil, e
	}
	mode := "chat"
	calls, tools, active := min(5, budgets.MaxModelCalls), 0, budgets.ChatConcurrentRuns
	if in.WebSearch {
		calls, tools = min(10, budgets.MaxModelCalls), min(8, budgets.MaxToolCalls)
	}
	if in.TaskMode {
		mode = "office"
		calls, tools, active = budgets.MaxModelCalls, budgets.MaxToolCalls, budgets.OfficeConcurrentRuns
	}
	parameters := map[string]any{"supports_vision": model.SupportsVision}
	if in.ReasoningEffort != "" {
		parameters["reasoning_effort"] = in.ReasoningEffort
	}
	if model.ContextWindow != nil {
		parameters["context_window"] = *model.ContextWindow
	}
	if in.TargetResourceVersionID != "" {
		if !in.TaskMode || s.resources == nil {
			return nil, ErrOfficeInvalid
		}
		parent, e := s.resources.repo.GetResourceVersion(ctx, user, in.TargetResourceVersionID, true)
		if e != nil {
			return nil, e
		}
		if !slices.Contains([]string{".docx", ".xlsx", ".pptx"}, strings.ToLower(path.Ext(parent.Name))) {
			return nil, ErrOfficeInvalid
		}
		parameters["output_parent"] = map[string]string{"resource_id": parent.ID, "version_id": parent.VersionID, "path": "output/" + parent.Name}
		if !slices.Contains(in.ResourceVersionIDs, parent.VersionID) {
			in.ResourceVersionIDs = append(in.ResourceVersionIDs, parent.VersionID)
		}
	}
	params, _ := json.Marshal(parameters)
	encoded, _ := json.Marshal(in)
	refs := []OfficeResourceVersion{}
	if len(in.ResourceVersionIDs) > 0 {
		if s.resources == nil {
			return nil, ErrOfficeDisabled
		}
		refs, e = s.resources.ResolveVersions(ctx, user, in.ResourceVersionIDs)
		if e != nil {
			return nil, e
		}
	}
	artifacts := make([]map[string]string, 0, len(refs))
	for _, ref := range refs {
		resource, e := s.resources.repo.GetResourceVersion(ctx, user, ref.VersionID, true)
		if e != nil {
			return nil, e
		}
		artifacts = append(artifacts, map[string]string{"resource_id": ref.ResourceID, "version_id": ref.VersionID, "id": ref.ResourceID, "name": resource.Name, "mime": resource.MIME})
	}
	content, _ := json.Marshal(map[string]any{"text": in.Content, "artifacts": artifacts})
	maxOutput := budgets.MaxOutputTokens
	if model.MaxOutputTokens != nil {
		maxOutput = min(maxOutput, *model.MaxOutputTokens)
	}
	run, _, e := s.repo.AdmitRun(ctx, OfficeRunAdmission{UserID: user, ConversationID: conversation, RunID: in.RunID, IdempotencyKey: in.RunID, RequestHash: officeDigest(encoded), APIKeyID: key, GroupID: group, Model: in.Model, UserContent: content, InputResources: refs, Mode: mode, SearchEnabled: in.WebSearch, Parameters: params, ContextBoundary: boundary, RegenerationOf: in.RegenerateRunID, MaxActiveRuns: active, Limits: OfficeRunLimits{MaxModelCalls: calls, MaxToolCalls: tools, MaxOutputTokens: maxOutput, MaxRunSeconds: budgets.MaxRunSeconds, MaxChargeCredits: budgets.MaxRunCredits}})
	return run, e
}
func (s *OfficeAgentService) ChatRepository() (OfficeChatRepository, error) {
	r, ok := s.repo.(OfficeChatRepository)
	if !ok {
		return nil, errors.New("workbench repository unavailable")
	}
	return r, nil
}
func (s *OfficeAgentService) Conversations(ctx context.Context, user int64, cursor string) ([]OfficeConversation, string, error) {
	r, e := s.ChatRepository()
	if e != nil {
		return nil, "", e
	}
	return r.ListConversationPage(ctx, user, cursor, 30)
}
func (s *OfficeAgentService) CreateConversation(ctx context.Context, user int64, id, title string) (*OfficeConversation, error) {
	status, e := s.ChatStatus(ctx)
	if e != nil {
		return nil, e
	}
	if !status.Enabled {
		return nil, ErrOfficeDisabled
	}
	if id == "" {
		id = uuid.NewString()
	}
	if title == "" {
		title = "新对话"
	}
	return s.repo.CreateConversation(ctx, user, id, title)
}
func (s *OfficeAgentService) ConversationMessages(ctx context.Context, user int64, id string, after int64) ([]OfficeMessage, []OfficeRun, error) {
	rows, e := s.repo.ListMessages(ctx, user, id, after, 100)
	if e != nil {
		return nil, nil, e
	}
	runs, e := s.repo.ListRuns(ctx, user, id, 100)
	return rows, runs, e
}

// History stays readable when new-run admission is disabled.
func (s *OfficeAgentService) PublicRun(ctx context.Context, user int64, id string) (*OfficeRun, error) {
	return s.repo.GetRun(ctx, user, id)
}
func (s *OfficeAgentService) PublicCancel(ctx context.Context, user int64, id string) (*OfficeRun, error) {
	return s.repo.CancelRun(ctx, user, id)
}
func (s *OfficeAgentService) PublicEvents(ctx context.Context, user int64, id string, after int64) ([]OfficeRunEvent, error) {
	return s.repo.ListEvents(ctx, user, id, after, 100)
}

func (s *OfficeAgentService) StructuredProgress(ctx context.Context, lease OfficeRunLease, id, kind string, payload json.RawMessage) (*OfficeRunEvent, error) {
	if !officeIdentifier(id, 128) || len(payload) > 32<<10 || !json.Valid(payload) {
		return nil, ErrOfficeInvalid
	}
	// Decode into typed projections: internal callbacks and arbitrary tool logs
	// cannot become public events simply by choosing a plausible event name.
	var safe any
	switch kind {
	case "progress":
		var v struct {
			Message string `json:"message"`
		}
		if json.Unmarshal(payload, &v) != nil || len(v.Message) > 2000 {
			return nil, ErrOfficeInvalid
		}
		safe = v
	case "tool.started", "tool.completed":
		var v struct {
			OperationID string `json:"operation_id"`
			ToolName    string `json:"tool_name"`
			Title       string `json:"title"`
			Status      string `json:"status,omitempty"`
			Message     string `json:"message,omitempty"`
		}
		if json.Unmarshal(payload, &v) != nil || !officeIdentifier(v.OperationID, 128) || !officeApprovedTool(v.ToolName) || len(v.Title) > 300 || len(v.Message) > 2000 {
			return nil, ErrOfficeInvalid
		}
		safe = v
	case "task.updated":
		var v struct {
			Tasks []struct {
				ID     string `json:"id"`
				Title  string `json:"title"`
				Status string `json:"status"`
			} `json:"tasks"`
		}
		if json.Unmarshal(payload, &v) != nil || len(v.Tasks) > 64 {
			return nil, ErrOfficeInvalid
		}
		for _, t := range v.Tasks {
			if len(t.Title) > 500 || len(t.ID) > 128 || !slices.Contains([]string{"pending", "in_progress", "completed", "failed", "cancelled"}, t.Status) {
				return nil, ErrOfficeInvalid
			}
		}
		safe = v
	case "artifact.published": // Resource ownership is checked by publication, not supplied IDs in progress.
		return nil, ErrOfficeInvalid
	case "clarification.requested": // Waiting state must be committed together with its event.
		return nil, ErrOfficeInvalid
	default:
		return nil, ErrOfficeInvalid
	}
	encoded, e := json.Marshal(safe)
	if e != nil {
		return nil, e
	}
	return s.repo.AppendEvent(ctx, lease, id, kind, encoded)
}

func ProvideOfficeAgentService(repo OfficeAgentRepository, cfg *config.Config, settings *SettingService, groups *GroupService, keys *APIKeyService, accounts AccountRepository, routes CompositeModelRouteRepository, channels *ChannelService, profiles *DesktopAgentProfileCatalogService, resources *OfficeResourcesService) *OfficeAgentService {
	s := NewOfficeAgentService(repo, cfg, settings, groups, keys, accounts, routes, channels)
	s.BindWorkbenchProfiles(profiles)
	s.resources = resources
	if resources != nil {
		s.BindResourcePin(func(ctx context.Context, user int64, id string) error {
			run, e := repo.GetRun(ctx, user, id)
			if e != nil {
				return e
			}
			return resources.PinRunInputs(ctx, *run)
		})
	}
	return s
}

// officeMessageText accepts historic string messages and the resource-aware
// message projection. File bytes and signed URLs never enter history.
func officeMessageText(raw json.RawMessage) (string, error) {
	var text string
	if json.Unmarshal(raw, &text) == nil {
		return text, nil
	}
	var value struct {
		Text string `json:"text"`
	}
	if json.Unmarshal(raw, &value) != nil || value.Text == "" {
		return "", ErrOfficeInvalid
	}
	return value.Text, nil
}
