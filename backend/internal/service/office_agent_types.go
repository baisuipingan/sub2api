package service

import (
	"context"
	"encoding/json"
	"errors"
	"time"
)

// Office control-plane records never contain a user JWT, API key, object-store
// credential, or file bytes. Resource IDs must be authorized by the service.
const (
	OfficeRunQueued               = "queued"
	OfficeRunRunning              = "running"
	OfficeRunWaitingUser          = "waiting_user"
	OfficeRunRecovering           = "recovering"
	OfficeRunNeedsConfirmation    = "needs_confirmation"
	OfficeRunCompleted            = "completed"
	OfficeRunPartiallyCompleted   = "partially_completed"
	OfficeRunFailed               = "failed"
	OfficeRunCancelled            = "cancelled"
	OfficeOperationPrepared       = "prepared"
	OfficeOperationDispatched     = "dispatched"
	OfficeOperationCompleted      = "completed"
	OfficeOperationUnknown        = "unknown"
	OfficeOperationModel          = "model"
	OfficeOperationTool           = "tool"
	OfficeMaxOperationInputBytes  = 256 << 10
	OfficeMaxCompactionInputBytes = 1 << 20
	OfficeMaxOperationResultBytes = 2 << 20
	OfficeMaxEventBytes           = 64 << 10
)

var (
	ErrOfficeInvalid             = errors.New("invalid office agent record")
	ErrOfficeNotFound            = errors.New("office agent record not found")
	ErrOfficeConflict            = errors.New("office agent request conflicts with an existing record")
	ErrOfficeActiveRun           = errors.New("an office agent run is already active")
	ErrOfficeLeaseLost           = errors.New("office agent lease is no longer valid")
	ErrOfficeOperationDispatched = errors.New("office agent operation was already dispatched; replay its result instead")
	ErrOfficeBudgetExceeded      = errors.New("office agent operation budget exceeded")
	ErrOfficeSettlementPending   = errors.New("previous office model usage is not settled yet")
	ErrOfficeResultUnknown       = errors.New("office agent operation result needs confirmation")
)

type OfficeResourceVersion struct {
	ResourceID string `json:"resource_id"`
	VersionID  string `json:"version_id"`
}

type OfficeRunLimits struct {
	MaxModelCalls    int   `json:"max_model_calls"`
	MaxToolCalls     int   `json:"max_tool_calls"`
	MaxOutputTokens  int   `json:"max_output_tokens"`
	MaxRunSeconds    int   `json:"max_run_seconds"`
	MaxChargeCredits int64 `json:"max_charge_credits,omitempty"`
}

type OfficeConversation struct {
	ID        string    `json:"id"`
	UserID    int64     `json:"user_id"`
	Title     string    `json:"title"`
	CreatedAt time.Time `json:"created_at"`
	UpdatedAt time.Time `json:"updated_at"`
}

type OfficeMessage struct {
	ID             string          `json:"id"`
	ConversationID string          `json:"conversation_id"`
	RunID          string          `json:"run_id"`
	Sequence       int64           `json:"sequence"`
	Role           string          `json:"role"`
	Content        json.RawMessage `json:"content"`
	CreatedAt      time.Time       `json:"created_at"`
}

type OfficeRunAdmission struct {
	Mode            string
	SearchEnabled   bool
	Parameters      json.RawMessage
	ContextBoundary int64
	RegenerationOf  string
	MaxActiveRuns   int
	UserID          int64
	ConversationID  string
	RunID           string
	IdempotencyKey  string
	RequestHash     string
	APIKeyID        int64
	GroupID         int64
	Model           string
	UserContent     json.RawMessage
	InputResources  []OfficeResourceVersion
	Limits          OfficeRunLimits
}

type OfficeRun struct {
	PausedSeconds   int64                   `json:"-"`
	Mode            string                  `json:"mode"`
	SearchEnabled   bool                    `json:"search_enabled"`
	Parameters      json.RawMessage         `json:"parameters"`
	ContextBoundary int64                   `json:"context_boundary"`
	RegenerationOf  string                  `json:"regeneration_of,omitempty"`
	Selected        bool                    `json:"selected"`
	ResumeValue     json.RawMessage         `json:"resume_value,omitempty"`
	InputRequestID  string                  `json:"input_request_id,omitempty"`
	ID              string                  `json:"id"`
	UserID          int64                   `json:"user_id"`
	ConversationID  string                  `json:"conversation_id"`
	APIKeyID        int64                   `json:"-"`
	GroupID         int64                   `json:"group_id"`
	Model           string                  `json:"model"`
	Status          string                  `json:"status"`
	Limits          OfficeRunLimits         `json:"limits"`
	InputResources  []OfficeResourceVersion `json:"input_resources"`
	// History projects durable file metadata separately from assistant text and
	// event replay. A nil projection is omitted by ordinary/internal run reads;
	// history explicitly includes an empty array when files were deleted.
	Artifacts    *[]OfficeResource `json:"artifacts,omitempty"`
	ModelCalls   int               `json:"model_calls"`
	ToolCalls    int               `json:"tool_calls"`
	ErrorCode    string            `json:"error_code,omitempty"`
	ErrorMessage string            `json:"error_message,omitempty"`
	CreatedAt    time.Time         `json:"created_at"`
	UpdatedAt    time.Time         `json:"updated_at"`
	StartedAt    *time.Time        `json:"started_at,omitempty"`
	FinishedAt   *time.Time        `json:"finished_at,omitempty"`
}

type OfficeRunLease struct {
	Run   OfficeRun `json:"run"`
	Owner string    `json:"-"`
	Token string    `json:"-"`
	Epoch int64     `json:"epoch"`
	Until time.Time `json:"until"`
}

type OfficeOperationPrepare struct {
	ID        string
	Kind      string
	InputHash string
	Input     json.RawMessage
	RequestID string
}

type OfficeOperationResult struct {
	Output       json.RawMessage `json:"output"`
	HTTPStatus   int             `json:"http_status,omitempty"`
	ErrorCode    string          `json:"error_code,omitempty"`
	ErrorMessage string          `json:"error_message,omitempty"`
}

type OfficeOperation struct {
	ID            string                 `json:"id"`
	RunID         string                 `json:"run_id"`
	Kind          string                 `json:"kind"`
	State         string                 `json:"state"`
	InputHash     string                 `json:"input_hash"`
	Input         json.RawMessage        `json:"input"`
	RequestID     string                 `json:"request_id"`
	Result        *OfficeOperationResult `json:"result,omitempty"`
	DispatchEpoch int64                  `json:"-"`
	DispatchToken string                 `json:"-"`
	CreatedAt     time.Time              `json:"created_at"`
	DispatchedAt  *time.Time             `json:"dispatched_at,omitempty"`
	CompletedAt   *time.Time             `json:"completed_at,omitempty"`
}

type OfficeRunEvent struct {
	ID        string          `json:"id"`
	RunID     string          `json:"run_id"`
	Sequence  int64           `json:"sequence"`
	Type      string          `json:"type"`
	Payload   json.RawMessage `json:"payload"`
	CreatedAt time.Time       `json:"created_at"`
}

// A lease can progress only its current, unexpired running run. Result inbox
// writes are the deliberate exception: the original dispatch identity may
// deposit a late result, but cannot renew a lease or change the run's state.
type OfficeAgentRepository interface {
	CreateConversation(context.Context, int64, string, string) (*OfficeConversation, error)
	GetConversation(context.Context, int64, string) (*OfficeConversation, error)
	ListConversations(context.Context, int64, int) ([]OfficeConversation, error)
	ListMessages(context.Context, int64, string, int64, int) ([]OfficeMessage, error)
	AdmitRun(context.Context, OfficeRunAdmission) (*OfficeRun, bool, error)
	GetRun(context.Context, int64, string) (*OfficeRun, error)
	ListRuns(context.Context, int64, string, int) ([]OfficeRun, error)
	ClaimRun(context.Context, string, time.Duration) (*OfficeRunLease, error)
	RenewLease(context.Context, OfficeRunLease, time.Duration) (*OfficeRunLease, error)
	CheckLease(context.Context, OfficeRunLease) error
	CancelRun(context.Context, int64, string) (*OfficeRun, error)
	FinishRun(context.Context, OfficeRunLease, string, json.RawMessage, string, string) (*OfficeRun, error)
	SuspendRun(context.Context, OfficeRunLease, string, string) (*OfficeRun, error)
	PrepareOperation(context.Context, OfficeRunLease, OfficeOperationPrepare) (*OfficeOperation, bool, error)
	DispatchOperation(context.Context, OfficeRunLease, string) (*OfficeOperation, error)
	RecordOperationResult(context.Context, OfficeRunLease, string, OfficeOperationResult) (*OfficeOperation, error)
	GetOperation(context.Context, int64, string, string) (*OfficeOperation, error)
	MarkOperationUnknown(context.Context, OfficeRunLease, string, string) error
	AppendEvent(context.Context, OfficeRunLease, string, string, json.RawMessage) (*OfficeRunEvent, error)
	ListEvents(context.Context, int64, string, int64, int) ([]OfficeRunEvent, error)
}

// OfficeChatRepository groups conversation/recovery capabilities separately from
// the original run/operation journal and its focused test fixtures.
type OfficeChatRepository interface {
	MessagePage(context.Context, int64, string, int64) ([]OfficeMessage, []OfficeRun, bool, int64, error)
	ListConversationPage(context.Context, int64, string, int) ([]OfficeConversation, string, error)
	RenameConversation(context.Context, int64, string, string) (*OfficeConversation, error)
	DeleteConversation(context.Context, int64, string) error
	ClaimPreset(context.Context, string, string, time.Duration) (*OfficeRunLease, error)
	ContextMessages(context.Context, OfficeRun) ([]OfficeMessage, error)
	RegenerationSource(context.Context, int64, string, string) (*OfficeRun, *OfficeMessage, error)
	ContextSummary(context.Context, OfficeRunLease) (*OfficeContextSummary, error)
	SaveContextSummary(context.Context, OfficeRunLease, OfficeContextSummary, int64, string) (int64, error)
	ListOperations(context.Context, int64, string) ([]OfficeOperation, error)
	Checkpoint(context.Context, OfficeRunLease) (json.RawMessage, int64, error)
	SaveCheckpoint(context.Context, OfficeRunLease, json.RawMessage, int64, string) (int64, error)
	WaitForInput(context.Context, OfficeRunLease, string, json.RawMessage) (*OfficeRun, error)
	ResumeInput(context.Context, int64, string, string, json.RawMessage) (*OfficeRun, error)
	SelectAnswer(context.Context, int64, string) (*OfficeRun, error)
}

type OfficeContextSummary struct {
	Version          int64  `json:"version"`
	Content          string `json:"content"`
	ThroughMessageID string `json:"through_message_id"`
	SourceHash       string `json:"source_hash"`
}
