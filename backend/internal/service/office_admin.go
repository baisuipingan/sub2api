package service

import (
	"context"
	"time"
)

type OfficeAdminFilter struct {
	Page, PageSize, Days int
	UserID               int64
	Execution            string
	Mode, Status, Model  string
}
type OfficeAdminOverview struct {
	Since               time.Time           `json:"since"`
	Until               time.Time           `json:"until"`
	TotalRuns           int64               `json:"total_runs"`
	Completed           int64               `json:"completed"`
	PartiallyCompleted  int64               `json:"partially_completed"`
	Failed              int64               `json:"failed"`
	Cancelled           int64               `json:"cancelled"`
	ModelCalls          int64               `json:"model_calls"`
	ModelChargeCredits  int64               `json:"model_charge_credits"`
	SearchCalls         int64               `json:"search_calls"`
	SearchChargeCredits int64               `json:"search_charge_credits"`
	InputTokens         int64               `json:"input_tokens"`
	OutputTokens        int64               `json:"output_tokens"`
	ChargeCredits       int64               `json:"charge_credits"`
	UsedBytes           int64               `json:"used_bytes"`
	ResourceCount       int64               `json:"resource_count"`
	OfficeTools         string              `json:"office_tools"`
	OfficeNodes         *OfficeNodeOverview `json:"office_nodes,omitempty"`
}
type OfficeAdminRun struct {
	OfficeRun
	UserEmail     string `json:"user_email"`
	UserName      string `json:"user_name"`
	InputTokens   int64  `json:"input_tokens"`
	OutputTokens  int64  `json:"output_tokens"`
	ChargeCredits int64  `json:"charge_credits"`
}
type OfficeAdminRunPage struct {
	Items    []OfficeAdminRun `json:"items"`
	Total    int64            `json:"total"`
	Page     int              `json:"page"`
	PageSize int              `json:"page_size"`
	Pages    int64            `json:"pages"`
}
type OfficeCallUsage struct {
	Settled         bool                    `json:"settled"`
	InputTokens     int64                   `json:"input_tokens"`
	OutputTokens    int64                   `json:"output_tokens"`
	CacheReadTokens int64                   `json:"cache_read_tokens"`
	ActualCost      float64                 `json:"actual_cost"`
	ChargeCredits   int64                   `json:"charge_credits"`
	DurationMS      *int64                  `json:"duration_ms"`
	Upstreams       []WorkbenchTaskUpstream `json:"upstreams,omitempty"`
}
type OfficeCallDetail struct {
	OfficeOperation
	Usage OfficeCallUsage       `json:"usage"`
	Tool  *WorkbenchToolSummary `json:"tool,omitempty"`
}
type OfficeAdminRunDetail struct {
	Run        OfficeRun          `json:"run"`
	UserEmail  string             `json:"user_email"`
	UserName   string             `json:"user_name"`
	Operations []OfficeCallDetail `json:"operations"`
}
type OfficeAdminRunMessages struct {
	ConversationTitle   string          `json:"conversation_title"`
	Messages            []OfficeMessage `json:"messages"`
	Runs                []OfficeRun     `json:"runs"`
	HasMore             bool            `json:"has_more"`
	NextBefore          int64           `json:"next_before"`
	SelectedUserMessage *OfficeMessage  `json:"selected_user_message,omitempty"`
}
type OfficeAdminResource struct {
	OfficeResource
	OwnerID   int64  `json:"user_id"`
	UserEmail string `json:"user_email"`
	UserName  string `json:"user_name"`
}
type OfficeAdminResourcePage struct {
	Items    []OfficeAdminResource `json:"items"`
	Total    int64                 `json:"total"`
	Page     int                   `json:"page"`
	PageSize int                   `json:"page_size"`
	Pages    int64                 `json:"pages"`
}
type OfficeAdminRepository interface {
	AdminOverview(context.Context, int) (*OfficeAdminOverview, error)
	AdminRuns(context.Context, OfficeAdminFilter) (*OfficeAdminRunPage, error)
	AdminRun(context.Context, string) (*OfficeAdminRunDetail, error)
	AdminRunMessages(context.Context, string, int64) (*OfficeAdminRunMessages, error)
	RunUsage(context.Context, int64, string, bool) ([]OfficeCallDetail, error)
	AdminResources(context.Context, OfficeAdminFilter) (*OfficeAdminResourcePage, error)
	AdminResourceOwner(context.Context, string) (int64, error)
	WorkerSeen(context.Context, string, string, *bool) error
}

func (s *OfficeAgentService) AdminRepository() (OfficeAdminRepository, error) {
	r, ok := s.repo.(OfficeAdminRepository)
	if !ok {
		return nil, ErrOfficeDisabled
	}
	return r, nil
}

func (s *OfficeAgentService) AdminOverview(ctx context.Context, days int) (*OfficeAdminOverview, error) {
	repo, err := s.AdminRepository()
	if err != nil {
		return nil, err
	}
	result, err := repo.AdminOverview(ctx, days)
	if err != nil {
		return nil, err
	}
	result.OfficeTools = s.officeToolsReadiness(ctx)
	if nodes, err := s.AdminOfficeNodes(ctx); err == nil && len(nodes) > 0 {
		result.OfficeNodes = summarizeOfficeNodes(nodes)
		if nodeRepo, err := s.officeNodeRepository(); err == nil {
			result.OfficeNodes.Waiting, _ = nodeRepo.CountWaitingOfficeNodes(ctx)
		}
	}
	return result, nil
}

func (s *OfficeAgentService) WorkerSeen(ctx context.Context, id, preset string, ready *bool) error {
	if !officeIdentifier(id, 80) || (preset != "chat" && preset != "office") {
		return ErrOfficeInvalid
	}
	if preset == "chat" {
		// A lightweight worker does not inspect or depend on an executor.
		ready = nil
	}
	r, e := s.AdminRepository()
	if e != nil {
		return e
	}
	return r.WorkerSeen(ctx, id, preset, ready)
}
func (s *OfficeAgentService) AdminResourceAccess(ctx context.Context, version, variant string) (*OfficeResourceAccess, error) {
	r, e := s.AdminRepository()
	if e != nil {
		return nil, e
	}
	owner, e := r.AdminResourceOwner(ctx, version)
	if e != nil {
		return nil, e
	}
	if s.resources == nil {
		return nil, ErrOfficeDisabled
	}
	return s.resources.Access(ctx, owner, version)
}
