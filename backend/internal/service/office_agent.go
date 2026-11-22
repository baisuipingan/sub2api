package service

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"slices"
	"strconv"
	"strings"
	"sync"
	"time"
	"unicode"
	"unicode/utf8"

	"github.com/Wei-Shaw/sub2api/internal/config"
	"github.com/google/uuid"
)

var (
	ErrOfficeDisabled     = errors.New("office agent pilot is disabled")
	ErrOfficeUnauthorized = errors.New("office agent credential is invalid or expired")
	ErrOfficeRouteChanged = errors.New("workbench key, group or public model changed")
	ErrOfficeCapacity     = errors.New("office agent model capacity is exhausted")
)

const officeLeaseDuration = 90 * time.Second

// The service owns authenticated workbench runs, leases and the ordinary model
// gateway bridge. A trusted worker receives a scoped capability, never the
// user's Agent API key. Files use the separate resources service.
type OfficeAgentService struct {
	cfg                   config.OfficeAgentConfig
	repo                  OfficeAgentRepository
	settings              *SettingService
	billingCache          *BillingCacheService
	groups                *GroupService
	catalog               DesktopAgentModelCatalog
	profiles              *DesktopAgentProfileCatalogService
	resources             *OfficeResourcesService
	pinInputs             func(context.Context, int64, string) error
	mu                    sync.RWMutex
	router                http.Handler
	modelRequestBodyLimit int64
	modelSlots            chan struct{}
	officeNodeEncryptor   SecretEncryptor
	resolve               func(context.Context, int64, string) (int64, int64, error)
	authorize             func(context.Context, OfficeRun) (string, error)
}

func NewOfficeAgentService(repo OfficeAgentRepository, cfg *config.Config, settings *SettingService,
	groups *GroupService, keys *APIKeyService, accounts AccountRepository, routes CompositeModelRouteRepository, channels *ChannelService,
) *OfficeAgentService {
	catalog := NewWorkbenchChatModelCatalog(accounts, routes, channels)
	var options config.OfficeAgentConfig
	if cfg != nil {
		options = cfg.OfficeAgent
	}
	s := &OfficeAgentService{repo: repo, settings: settings, groups: groups, catalog: catalog, cfg: options, modelSlots: make(chan struct{}, max(1, min(options.MaxConcurrentCalls, 4)))}
	if cfg != nil {
		s.modelRequestBodyLimit = cfg.Gateway.MaxBodySize
		if globalLimit := cfg.Server.MaxRequestBodySize; globalLimit > 0 && (s.modelRequestBodyLimit <= 0 || globalLimit < s.modelRequestBodyLimit) {
			s.modelRequestBodyLimit = globalLimit
		}
	}
	currentGroup := func(ctx context.Context, model string) (*Group, error) {
		if settings == nil || groups == nil || catalog == nil || keys == nil {
			return nil, ErrOfficeDisabled
		}
		_, group, err := s.loadWorkbenchAgentGroup(ctx)
		if err != nil {
			return nil, err
		}
		models, err := catalog.ModelsForGroup(ctx, group)
		if err != nil {
			return nil, err
		}
		if !slices.Contains(models, model) {
			return nil, ErrOfficeRouteChanged
		}
		return group, nil
	}
	s.resolve = func(ctx context.Context, userID int64, model string) (int64, int64, error) {
		group, err := currentGroup(ctx, model)
		if err != nil {
			return 0, 0, err
		}
		key, _, err := keys.GetDesktopAgentCredential(ctx, userID, group.ID)
		if errors.Is(err, ErrDesktopAgentPolicyChanged) {
			return 0, 0, ErrOfficeRouteChanged
		}
		if err != nil {
			return 0, 0, err
		}
		key, err = keys.GetByID(ctx, key.ID)
		if err != nil {
			return 0, 0, err
		}
		if !officeKeyMatches(key, userID, group) {
			return 0, 0, ErrOfficeRouteChanged
		}
		return key.ID, group.ID, nil
	}
	s.authorize = func(ctx context.Context, run OfficeRun) (string, error) {
		var group *Group
		var err error
		if run.BrowserDriven {
			_, group, err = s.loadWorkbenchAgentGroup(ctx)
		} else {
			group, err = currentGroup(ctx, run.Model)
		}
		if err != nil || group.ID != run.GroupID {
			return "", ErrOfficeRouteChanged
		}
		key, err := keys.GetByID(ctx, run.APIKeyID)
		if err != nil || !officeKeyMatches(key, run.UserID, group) {
			return "", ErrOfficeRouteChanged
		}
		if group.IsExclusive || key.User.RestrictPublicGroups {
			// The ID lookup does not hydrate the user's allowed group list.
			user, err := keys.userRepo.GetByID(ctx, run.UserID)
			if err != nil || user == nil || !user.IsActive() || !user.CanBindGroup(group.ID, group.IsExclusive) {
				return "", ErrGroupNotAllowed
			}
		}
		return key.Key, nil
	}
	return s
}

func officeKeyMatches(key *APIKey, userID int64, group *Group) bool {
	return key != nil && key.UserID == userID && key.KeyType == APIKeyTypeSystem && key.Purpose == APIKeyPurposeDesktopAgent &&
		key.Key != "" && key.IsActive() && !key.IsExpired() && !key.IsQuotaExhausted() && key.GroupID != nil && *key.GroupID == group.ID &&
		key.User != nil && key.User.IsActive()
}

func (s *OfficeAgentService) BindGatewayRouter(router http.Handler) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.router = router
}

// ModelRequestBodyLimit is the effective ingress limit of /v1/chat/completions.
func (s *OfficeAgentService) ModelRequestBodyLimit() int64 { return s.modelRequestBodyLimit }

func (s *OfficeAgentService) Enabled() bool {
	if s == nil || !s.cfg.Enabled || s.cfg.Validate() != nil || s.repo == nil {
		return false
	}
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.router != nil
}

type OfficePilotSubmit struct {
	ConversationID string `json:"conversation_id,omitempty"`
	RunID          string `json:"run_id"`
	Model          string `json:"model"`
	Content        string `json:"content"`
}

func (s *OfficeAgentService) SubmitPilot(ctx context.Context, userID int64, in OfficePilotSubmit) (*OfficeRun, error) {
	if !s.Enabled() {
		return nil, ErrOfficeDisabled
	}
	if userID <= 0 || uuid.Validate(in.RunID) != nil ||
		(in.ConversationID != "" && uuid.Validate(in.ConversationID) != nil) || !officeModelID(in.Model) ||
		strings.TrimSpace(in.Content) == "" || len(in.Content) > 32<<10 || !utf8.ValidString(in.Content) {
		return nil, ErrOfficeInvalid
	}
	if in.ConversationID == "" {
		// Stable on replay even when the browser did not supply a conversation.
		in.ConversationID = uuid.NewSHA1(uuid.NameSpaceOID, []byte(fmt.Sprintf("office:%d:%s", userID, in.RunID))).String()
	}
	keyID, groupID, err := s.resolve(ctx, userID, in.Model)
	if err != nil {
		return nil, err
	}
	_, err = s.repo.GetConversation(ctx, userID, in.ConversationID)
	if errors.Is(err, ErrOfficeNotFound) {
		title := []rune(strings.TrimSpace(in.Content))
		if len(title) > 60 {
			title = title[:60]
		}
		_, err = s.repo.CreateConversation(ctx, userID, in.ConversationID, string(title))
		if errors.Is(err, ErrOfficeConflict) {
			_, err = s.repo.GetConversation(ctx, userID, in.ConversationID)
		}
	}
	if err != nil {
		return nil, err
	}
	content, _ := json.Marshal(in.Content)
	encoded, _ := json.Marshal(in)
	run, _, err := s.repo.AdmitRun(ctx, OfficeRunAdmission{
		UserID: userID, ConversationID: in.ConversationID, RunID: in.RunID,
		IdempotencyKey: in.RunID, RequestHash: officeDigest(encoded), APIKeyID: keyID,
		GroupID: groupID, Model: in.Model, UserContent: content,
		Limits: OfficeRunLimits{MaxModelCalls: s.cfg.MaxModelCalls, MaxToolCalls: s.cfg.MaxToolCalls,
			MaxOutputTokens: s.cfg.MaxOutputTokens, MaxRunSeconds: s.cfg.MaxRunSeconds},
	})
	return run, err
}

func officeIdentifier(value string, maxBytes int) bool {
	if value == "" || len(value) > maxBytes {
		return false
	}
	for _, ch := range value {
		if officeIdentifierAlphanumeric(ch) || strings.ContainsRune("._:-", ch) {
			continue
		}
		return false
	}
	return officeIdentifierAlphanumeric(rune(value[0]))
}

func officeIdentifierAlphanumeric(ch rune) bool {
	return (ch >= 'a' && ch <= 'z') || (ch >= 'A' && ch <= 'Z') || (ch >= '0' && ch <= '9')
}

func officeModelID(value string) bool {
	if value == "" || len(value) > 128 || !utf8.ValidString(value) {
		return false
	}
	for _, ch := range value {
		if unicode.IsControl(ch) {
			return false
		}
	}
	return true
}

func officeDigest(data []byte) string {
	hash := sha256.Sum256(data)
	return hex.EncodeToString(hash[:])
}

func (s *OfficeAgentService) BootstrapAuthorized(token string) bool {
	return s.Enabled() && s.cfg.WorkerToken != "" && len(token) == len(s.cfg.WorkerToken) &&
		subtle.ConstantTimeCompare([]byte(token), []byte(s.cfg.WorkerToken)) == 1
}

type officeLeaseCredential struct {
	RunID  string `json:"r"`
	UserID int64  `json:"u"`
	Owner  string `json:"o"`
	Token  string `json:"t"`
	Epoch  int64  `json:"e"`
	Expiry int64  `json:"x"`
}

func (s *OfficeAgentService) leaseCredential(lease OfficeRunLease) string {
	until := time.Now().Add(10 * time.Minute)
	if lease.Run.StartedAt != nil {
		until = minOfficeTime(until, lease.Run.StartedAt.Add(time.Duration(int64(lease.Run.Limits.MaxRunSeconds)+lease.Run.PausedSeconds)*time.Second))
	}
	value, _ := json.Marshal(officeLeaseCredential{lease.Run.ID, lease.Run.UserID, lease.Owner, lease.Token, lease.Epoch, until.Unix()})
	encoded := base64.RawURLEncoding.EncodeToString(value)
	return encoded + "." + s.credentialSignature(encoded)
}

func (s *OfficeAgentService) credentialSignature(encoded string) string {
	h := hmac.New(sha256.New, []byte(s.cfg.WorkerToken))
	_, _ = h.Write([]byte("sub2api-office-lease-v1\n" + encoded))
	return base64.RawURLEncoding.EncodeToString(h.Sum(nil))
}

func minOfficeTime(a, b time.Time) time.Time {
	if a.Before(b) {
		return a
	}
	return b
}

func (s *OfficeAgentService) AuthenticateLease(ctx context.Context, runID, epoch, token string) (*OfficeRunLease, error) {
	lease, err := s.authenticateLeaseIdentity(ctx, runID, epoch, token)
	if err != nil {
		return nil, err
	}
	if err := s.repo.CheckLease(ctx, *lease); err != nil {
		return nil, err
	}
	return lease, nil
}

// AuthenticateResultLease authorizes only a deposit into an operation's inbox.
// The repository still verifies its original dispatch identity; it grants no
// permission to renew, issue another call, or complete a cancelled run.
func (s *OfficeAgentService) AuthenticateResultLease(ctx context.Context, runID, epoch, token string) (*OfficeRunLease, error) {
	return s.authenticateLeaseIdentity(ctx, runID, epoch, token)
}

// Expired credentials may only report a stopped execution for a short grace
// period. The repository still requires the exact current dispatch identity;
// this grants no model, tool, resource-read or renewal capability.
func (s *OfficeAgentService) AuthenticateSuspensionLease(ctx context.Context, runID, epoch, token string) (*OfficeRunLease, error) {
	return s.authenticateLeaseIdentityGrace(ctx, runID, epoch, token, 5*time.Minute)
}

func (s *OfficeAgentService) authenticateLeaseIdentity(ctx context.Context, runID, epoch, token string) (*OfficeRunLease, error) {
	return s.authenticateLeaseIdentityGrace(ctx, runID, epoch, token, 0)
}
func (s *OfficeAgentService) authenticateLeaseIdentityGrace(ctx context.Context, runID, epoch, token string, grace time.Duration) (*OfficeRunLease, error) {
	if !s.Enabled() || s.cfg.WorkerToken == "" || len(token) > 2048 || uuid.Validate(runID) != nil {
		return nil, ErrOfficeUnauthorized
	}
	parts := strings.Split(token, ".")
	if len(parts) != 2 || !hmac.Equal([]byte(parts[1]), []byte(s.credentialSignature(parts[0]))) {
		return nil, ErrOfficeUnauthorized
	}
	data, err := base64.RawURLEncoding.DecodeString(parts[0])
	var credential officeLeaseCredential
	if err != nil || json.Unmarshal(data, &credential) != nil || credential.RunID != runID ||
		credential.Expiry <= time.Now().Add(-grace).Unix() || strconv.FormatInt(credential.Epoch, 10) != epoch {
		return nil, ErrOfficeUnauthorized
	}
	run, err := s.repo.GetRun(ctx, credential.UserID, runID)
	if err != nil || run.BrowserDriven {
		return nil, ErrOfficeLeaseLost
	}
	lease := OfficeRunLease{Run: *run, Owner: credential.Owner, Token: credential.Token, Epoch: credential.Epoch}
	return &lease, nil
}

type OfficeWorkerMessage struct {
	ID      string `json:"id"`
	Role    string `json:"role"`
	Content string `json:"content"`
}

type OfficeWorkerManifest struct {
	SupportsVision      bool                    `json:"supports_vision"`
	ReplayToolReasoning bool                    `json:"replay_tool_reasoning,omitempty"`
	ContextWindow       int                     `json:"context_window"`
	ContextSummary      *OfficeContextSummary   `json:"context_summary,omitempty"`
	CurrentQuestionID   string                  `json:"current_question_id"`
	OutputParent        json.RawMessage         `json:"output_parent,omitempty"`
	Mode                string                  `json:"mode"`
	SearchEnabled       bool                    `json:"search_enabled"`
	Parameters          json.RawMessage         `json:"parameters"`
	Resume              bool                    `json:"resume"`
	ResumeValue         json.RawMessage         `json:"resume_value,omitempty"`
	CheckpointVersion   int64                   `json:"checkpoint_version"`
	RunID               string                  `json:"run_id"`
	ConversationID      string                  `json:"conversation_id"`
	ThreadID            string                  `json:"thread_id"`
	LeaseEpoch          int64                   `json:"lease_epoch"`
	LeaseToken          string                  `json:"lease_token"`
	LeaseUntil          time.Time               `json:"lease_until"`
	Model               string                  `json:"model"`
	BridgePath          string                  `json:"bridge_path"`
	Limits              OfficeRunLimits         `json:"limits"`
	Messages            []OfficeWorkerMessage   `json:"messages"`
	DurableResume       bool                    `json:"durable_resume"`
	InputResources      []OfficeResourceVersion `json:"input_resources"`
}

func (s *OfficeAgentService) Claim(ctx context.Context, owner string) (*OfficeWorkerManifest, error) {
	return s.ClaimPreset(ctx, owner, "office")
}
func (s *OfficeAgentService) ClaimPreset(ctx context.Context, owner, preset string) (*OfficeWorkerManifest, error) {
	if !s.Enabled() {
		return nil, ErrOfficeDisabled
	}
	if !officeIdentifier(owner, 80) {
		return nil, ErrOfficeInvalid
	}
	var lease *OfficeRunLease
	var err error
	if repo, ok := s.repo.(OfficeChatRepository); ok {
		lease, err = repo.ClaimPreset(ctx, owner, preset, officeLeaseDuration)
	} else {
		lease, err = s.repo.ClaimRun(ctx, owner, officeLeaseDuration)
	}
	if err != nil || lease == nil {
		return nil, err
	}
	// Only selected conversation messages enter the worker context. An office
	// graph restores its separately fenced checkpoint and workspace below;
	// uncertain dispatches remain blocked by repository claim policy.
	var rows []OfficeMessage
	if repo, ok := s.repo.(OfficeChatRepository); ok {
		rows, err = repo.ContextMessages(ctx, lease.Run)
	} else {
		rows, err = s.repo.ListMessages(ctx, lease.Run.UserID, lease.Run.ConversationID, 0, 200)
	}
	if err == nil && s.pinInputs != nil && len(lease.Run.InputResources) > 0 {
		err = s.pinInputs(ctx, lease.Run.UserID, lease.Run.ID)
	}

	if err != nil {
		return nil, err
	}
	if len(rows) > 2000 {
		_, _ = s.repo.FinishRun(ctx, *lease, OfficeRunFailed, nil, "CONTEXT_LIMIT", "会话超过安全处理上限，请新建会话；尚未调用模型")
		return nil, ErrOfficeBudgetExceeded
	}
	manifest := &OfficeWorkerManifest{Mode: lease.Run.Mode, SearchEnabled: lease.Run.SearchEnabled, Parameters: json.RawMessage(`{}`), ContextSummary: &OfficeContextSummary{}, Messages: []OfficeWorkerMessage{}, Resume: len(lease.Run.ResumeValue) > 0 && string(lease.Run.ResumeValue) != "null", ResumeValue: lease.Run.ResumeValue, RunID: lease.Run.ID, ConversationID: lease.Run.ConversationID,
		ThreadID: lease.Run.ConversationID, LeaseEpoch: lease.Epoch, LeaseToken: s.leaseCredential(*lease), LeaseUntil: lease.Until,
		Model: lease.Run.Model, BridgePath: "/internal/office-agent/v1/runs/" + lease.Run.ID + "/model/chat/completions",
		Limits: lease.Run.Limits, DurableResume: false, InputResources: lease.Run.InputResources}
	if manifest.Mode == "" {
		manifest.Mode = "office"
	}
	if manifest.InputResources == nil {
		manifest.InputResources = []OfficeResourceVersion{}
	}
	if repo, ok := s.repo.(OfficeChatRepository); ok && lease.Run.Mode == "office" {
		_, version, e := repo.Checkpoint(ctx, *lease)
		if e != nil {
			return nil, e
		}
		manifest.CheckpointVersion = version
		manifest.DurableResume = true
	}
	var params map[string]json.RawMessage
	if json.Unmarshal(lease.Run.Parameters, &params) == nil {
		manifest.OutputParent = params["output_parent"]
		_ = json.Unmarshal(params["supports_vision"], &manifest.SupportsVision)
		_ = json.Unmarshal(params["replay_tool_reasoning"], &manifest.ReplayToolReasoning)
		_ = json.Unmarshal(params["context_window"], &manifest.ContextWindow)
		modelParams := map[string]json.RawMessage{}
		if len(params["reasoning_effort"]) > 0 {
			modelParams["reasoning_effort"] = params["reasoning_effort"]
		}
		manifest.Parameters, _ = json.Marshal(modelParams)
	}
	if repo, ok := s.repo.(OfficeChatRepository); ok {
		manifest.ContextSummary, err = repo.ContextSummary(ctx, *lease)
		if err != nil {
			return nil, err
		}
		if manifest.ContextSummary == nil {
			manifest.ContextSummary = &OfficeContextSummary{}
		}
	}
	var total int
	for _, row := range rows {
		content, e := officeMessageText(row.Content)
		if e != nil || (row.Role != "user" && row.Role != "assistant") {
			return nil, ErrOfficeInvalid
		}
		total += len(content)
		if total > 2<<20 {
			_, _ = s.repo.FinishRun(ctx, *lease, OfficeRunFailed, nil, "CONTEXT_LIMIT", "会话超过安全处理上限，请新建会话；尚未调用模型")
			return nil, ErrOfficeBudgetExceeded
		}
		if row.RunID == lease.Run.ID && row.Role == "user" {
			manifest.CurrentQuestionID = row.ID
		}
		manifest.Messages = append(manifest.Messages, OfficeWorkerMessage{ID: row.ID, Role: row.Role, Content: content})
	}
	return manifest, nil
}

// Suspension accepts a finite reason code, never a worker-supplied public error
// body. It fences future work while keeping original paid inbox deposits valid.
func (s *OfficeAgentService) Suspend(ctx context.Context, lease OfficeRunLease, code string) (*OfficeRun, error) {
	messages := map[string]string{
		"BILLING_PENDING":              "上一轮模型已完成，计费记录仍在入账；本步骤尚未调用，任务已暂停",
		"RUN_TIMEOUT":                  "任务达到时间上限，已停止后续操作；已发出的调用不会自动重发",
		"MODEL_STREAM_INCOMPLETE":      "模型响应中断，结果尚未确认；已停止后续操作，不会自动重发",
		"ARTIFACT_PUBLICATION_UNKNOWN": "成果归档结果尚未确认；已停止后续操作，请检查文件记录",
		"TRANSPORT_UNKNOWN":            "模型或工具连接中断，结果尚未确认；已停止后续操作，不会自动重发",
		"WORKSPACE_SAVE_UNKNOWN":       "任务文件保存结果尚未确认；已停止后续操作",
		"CHECKPOINT_WRITE_UNKNOWN":     "任务运行状态保存结果尚未确认；已停止自动恢复",
		"CONTEXT_SUMMARY_SAVE_UNKNOWN": "上下文摘要保存结果尚未确认；已停止后续模型调用",
	}
	message, ok := messages[code]
	if !ok {
		code, message = "RUN_RESULT_UNCONFIRMED", "本次任务的执行或保存结果尚未确认；已停止后续操作，不会自动重发"
	}
	return s.repo.SuspendRun(ctx, lease, code, message)
}

func (s *OfficeAgentService) Heartbeat(ctx context.Context, lease OfficeRunLease) (string, error) {
	renewed, err := s.repo.RenewLease(ctx, lease, officeLeaseDuration)
	if err != nil {
		return "", err
	}
	return s.leaseCredential(*renewed), nil
}

func (s *OfficeAgentService) GetRun(ctx context.Context, userID int64, runID string) (*OfficeRun, error) {
	if !s.Enabled() {
		return nil, ErrOfficeDisabled
	}
	return s.repo.GetRun(ctx, userID, runID)
}

// Messages always establishes run ownership before looking up its conversation.
// A caller cannot read another user's conversation by guessing a run ID.
func (s *OfficeAgentService) Messages(ctx context.Context, userID int64, runID string, after int64) ([]OfficeMessage, error) {
	if after < 0 {
		return nil, ErrOfficeInvalid
	}
	run, err := s.GetRun(ctx, userID, runID)
	if err != nil {
		return nil, err
	}
	return s.repo.ListMessages(ctx, userID, run.ConversationID, after, 100)
}

func (s *OfficeAgentService) Cancel(ctx context.Context, userID int64, runID string) (*OfficeRun, error) {
	if !s.Enabled() {
		return nil, ErrOfficeDisabled
	}
	return s.repo.CancelRun(ctx, userID, runID)
}

func (s *OfficeAgentService) Events(ctx context.Context, userID int64, runID string, after int64) ([]OfficeRunEvent, error) {
	if !s.Enabled() {
		return nil, ErrOfficeDisabled
	}
	return s.repo.ListEvents(ctx, userID, runID, after, 100)
}

func (s *OfficeAgentService) Finish(ctx context.Context, lease OfficeRunLease, status, content, code, message string) (*OfficeRun, error) {
	if status != OfficeRunCompleted && status != OfficeRunPartiallyCompleted && status != OfficeRunFailed {
		return nil, ErrOfficeInvalid
	}
	if len(content) > 96<<10 || !utf8.ValidString(content) || len(code) > 64 || len(message) > 1000 {
		return nil, ErrOfficeInvalid
	}
	var encoded json.RawMessage
	if strings.TrimSpace(content) != "" {
		encoded, _ = json.Marshal(content)
		if lease.Run.SearchEnabled && status != OfficeRunFailed {
			if repo, ok := s.repo.(OfficeChatRepository); ok {
				operations, err := repo.ListOperations(ctx, lease.Run.UserID, lease.Run.ID)
				if err != nil {
					return nil, err
				}
				if citations := officeCitations(operations); len(citations) > 0 {
					encoded, _ = json.Marshal(struct {
						Text      string           `json:"text"`
						Citations []OfficeCitation `json:"citations"`
					}{content, citations})
				}
			}
		}
	} else if status == OfficeRunCompleted {
		return nil, ErrOfficeInvalid
	}
	return s.repo.FinishRun(ctx, lease, status, encoded, code, message)
}

func (s *OfficeAgentService) Progress(ctx context.Context, lease OfficeRunLease, eventID, message string) (*OfficeRunEvent, error) {
	if !officeIdentifier(eventID, 128) || len(message) > 2000 || !utf8.ValidString(message) {
		return nil, ErrOfficeInvalid
	}
	payload, _ := json.Marshal(map[string]string{"message": message})
	return s.repo.AppendEvent(ctx, lease, eventID, "progress", payload)
}
