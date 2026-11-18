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

// The pilot has no user-facing tool execution or resource API. A trusted worker
// gets a scoped model capability, never the user's existing Studio API key.
type OfficeAgentService struct {
	cfg        config.OfficeAgentConfig
	repo       OfficeAgentRepository
	mu         sync.RWMutex
	router     http.Handler
	modelSlots chan struct{}
	resolve    func(context.Context, int64, string) (int64, int64, error)
	authorize  func(context.Context, OfficeRun) (string, error)
}

func NewOfficeAgentService(repo OfficeAgentRepository, cfg *config.Config, settings *SettingService,
	groups *GroupService, keys *APIKeyService, accounts AccountRepository, routes CompositeModelRouteRepository, channels *ChannelService,
) *OfficeAgentService {
	catalog := NewWorkbenchChatModelCatalog(accounts, routes, channels)
	var options config.OfficeAgentConfig
	if cfg != nil {
		options = cfg.OfficeAgent
	}
	s := &OfficeAgentService{repo: repo, cfg: options, modelSlots: make(chan struct{}, max(1, min(options.MaxConcurrentCalls, 4)))}
	currentGroup := func(ctx context.Context, model string) (*Group, error) {
		if settings == nil || groups == nil || catalog == nil || keys == nil {
			return nil, ErrOfficeDisabled
		}
		workbench, err := settings.GetWorkbenchConfig(ctx)
		if err != nil {
			return nil, err
		}
		// ImageStudioEnabled deliberately does not gate the new module.
		if workbench.ImageStudioGroupID <= 0 {
			return nil, ErrOfficeRouteChanged
		}
		group, err := groups.GetByID(ctx, workbench.ImageStudioGroupID)
		if err != nil || group == nil || group.Status != StatusActive {
			return nil, ErrOfficeRouteChanged
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
		key, err := keys.GetOrCreateImageStudioKey(ctx, userID, &group.ID)
		if err != nil {
			return 0, 0, err
		}
		// Newly created keys are not hydrated with User. Re-read through the
		// ordinary service, without constructing another system key identity.
		key, err = keys.GetByID(ctx, key.ID)
		if err != nil {
			return 0, 0, err
		}
		if !officeKeyMatches(key, userID, group.ID) {
			return 0, 0, ErrOfficeRouteChanged
		}
		return key.ID, group.ID, nil
	}
	s.authorize = func(ctx context.Context, run OfficeRun) (string, error) {
		group, err := currentGroup(ctx, run.Model)
		if err != nil || group.ID != run.GroupID {
			return "", ErrOfficeRouteChanged
		}
		key, err := keys.GetByID(ctx, run.APIKeyID)
		if err != nil || !officeKeyMatches(key, run.UserID, run.GroupID) {
			return "", ErrOfficeRouteChanged
		}
		return key.Key, nil
	}
	return s
}

func officeKeyMatches(key *APIKey, userID, groupID int64) bool {
	return key != nil && key.UserID == userID && key.KeyType == APIKeyTypeSystem && key.Name == ImageStudioKeyName &&
		key.Key != "" && key.IsActive() && !key.IsExpired() && key.GroupID != nil && *key.GroupID == groupID &&
		key.User != nil && key.User.IsActive()
}

func (s *OfficeAgentService) BindGatewayRouter(router http.Handler) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.router = router
}

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
		if !((ch >= 'a' && ch <= 'z') || (ch >= 'A' && ch <= 'Z') || (ch >= '0' && ch <= '9') || strings.ContainsRune("._:-", ch)) {
			return false
		}
	}
	first := value[0]
	return (first >= 'a' && first <= 'z') || (first >= 'A' && first <= 'Z') || (first >= '0' && first <= '9')
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
	return s.Enabled() && len(token) == len(s.cfg.WorkerToken) &&
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
		until = minOfficeTime(until, lease.Run.StartedAt.Add(time.Duration(lease.Run.Limits.MaxRunSeconds)*time.Second))
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

func (s *OfficeAgentService) authenticateLeaseIdentity(ctx context.Context, runID, epoch, token string) (*OfficeRunLease, error) {
	if !s.Enabled() || len(token) > 2048 || uuid.Validate(runID) != nil {
		return nil, ErrOfficeUnauthorized
	}
	parts := strings.Split(token, ".")
	if len(parts) != 2 || !hmac.Equal([]byte(parts[1]), []byte(s.credentialSignature(parts[0]))) {
		return nil, ErrOfficeUnauthorized
	}
	data, err := base64.RawURLEncoding.DecodeString(parts[0])
	var credential officeLeaseCredential
	if err != nil || json.Unmarshal(data, &credential) != nil || credential.RunID != runID ||
		credential.Expiry <= time.Now().Unix() || strconv.FormatInt(credential.Epoch, 10) != epoch {
		return nil, ErrOfficeUnauthorized
	}
	run, err := s.repo.GetRun(ctx, credential.UserID, runID)
	if err != nil {
		return nil, ErrOfficeLeaseLost
	}
	lease := OfficeRunLease{Run: *run, Owner: credential.Owner, Token: credential.Token, Epoch: credential.Epoch}
	return &lease, nil
}

type OfficeWorkerMessage struct {
	Role    string `json:"role"`
	Content string `json:"content"`
}

type OfficeWorkerManifest struct {
	RunID          string                  `json:"run_id"`
	ConversationID string                  `json:"conversation_id"`
	ThreadID       string                  `json:"thread_id"`
	LeaseEpoch     int64                   `json:"lease_epoch"`
	LeaseToken     string                  `json:"lease_token"`
	LeaseUntil     time.Time               `json:"lease_until"`
	Model          string                  `json:"model"`
	BridgePath     string                  `json:"bridge_path"`
	Limits         OfficeRunLimits         `json:"limits"`
	Messages       []OfficeWorkerMessage   `json:"messages"`
	DurableResume  bool                    `json:"durable_resume"`
	InputResources []OfficeResourceVersion `json:"input_resources"`
}

func (s *OfficeAgentService) Claim(ctx context.Context, owner string) (*OfficeWorkerManifest, error) {
	if !s.Enabled() {
		return nil, ErrOfficeDisabled
	}
	if !officeIdentifier(owner, 80) {
		return nil, ErrOfficeInvalid
	}
	lease, err := s.repo.ClaimRun(ctx, owner, officeLeaseDuration)
	if err != nil || lease == nil {
		return nil, err
	}
	// Until the persistent LangGraph adapter passes fencing tests, the pilot
	// never claims it can resume a graph from a partial tool/model sequence.
	rows, err := s.repo.ListMessages(ctx, lease.Run.UserID, lease.Run.ConversationID, 0, 101)
	if err != nil {
		return nil, err
	}
	if len(rows) > 100 {
		_, _ = s.repo.FinishRun(ctx, *lease, OfficeRunFailed, nil, "CONTEXT_LIMIT", "试验会话过长，请新建会话；尚未调用模型")
		return nil, ErrOfficeBudgetExceeded
	}
	manifest := &OfficeWorkerManifest{RunID: lease.Run.ID, ConversationID: lease.Run.ConversationID,
		ThreadID: lease.Run.ConversationID, LeaseEpoch: lease.Epoch, LeaseToken: s.leaseCredential(*lease), LeaseUntil: lease.Until,
		Model: lease.Run.Model, BridgePath: "/internal/office-agent/v1/runs/" + lease.Run.ID + "/model/chat/completions",
		Limits: lease.Run.Limits, DurableResume: false, InputResources: lease.Run.InputResources}
	var total int
	for _, row := range rows {
		var content string
		if json.Unmarshal(row.Content, &content) != nil || (row.Role != "user" && row.Role != "assistant") {
			return nil, ErrOfficeInvalid
		}
		total += len(content)
		if total > 96<<10 {
			_, _ = s.repo.FinishRun(ctx, *lease, OfficeRunFailed, nil, "CONTEXT_LIMIT", "试验会话超过输入预算，请新建会话；尚未调用模型")
			return nil, ErrOfficeBudgetExceeded
		}
		manifest.Messages = append(manifest.Messages, OfficeWorkerMessage{Role: row.Role, Content: content})
	}
	return manifest, nil
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
// A pilot operator cannot read another user's conversation by guessing a run ID.
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
	if status != OfficeRunCompleted && status != OfficeRunFailed {
		return nil, ErrOfficeInvalid
	}
	if len(content) > 96<<10 || !utf8.ValidString(content) || len(code) > 64 || len(message) > 1000 {
		return nil, ErrOfficeInvalid
	}
	var encoded json.RawMessage
	if strings.TrimSpace(content) != "" {
		encoded, _ = json.Marshal(content)
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
