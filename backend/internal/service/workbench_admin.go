package service

import (
	"context"
	"strings"
	"time"
	"unicode"
	"unicode/utf8"

	infraerrors "github.com/Wei-Shaw/sub2api/internal/pkg/errors"
)

var ErrWorkbenchInvalid = infraerrors.BadRequest("WORKBENCH_INVALID", "invalid workbench query or configuration")

type WorkbenchTaskCounts struct {
	Total      int64 `json:"total"`
	Queued     int64 `json:"queued"`
	Generating int64 `json:"generating"`
	Saving     int64 `json:"saving"`
	Completed  int64 `json:"completed"`
	Partial    int64 `json:"partial"`
	Failed     int64 `json:"failed"`
	Unknown    int64 `json:"unknown"`
	Cancelled  int64 `json:"cancelled"`
}

type WorkbenchCallCounts struct {
	// Total is planned child calls, including queued/unsent records. It is not
	// the number of paid provider requests or settled billing events.
	Total          int64 `json:"total"`
	Completed      int64 `json:"completed"`
	Failed         int64 `json:"failed"`
	Unknown        int64 `json:"unknown"`
	Cancelled      int64 `json:"cancelled"`
	BillingPending int64 `json:"billing_pending"`
	BillingUnknown int64 `json:"billing_unknown"`
}

type WorkbenchQueueCounts struct {
	Queued     int64 `json:"queued"`
	Generating int64 `json:"generating"`
	Saving     int64 `json:"saving"`
	Unknown    int64 `json:"unknown"`
}

type WorkbenchGalleryCounts struct {
	Tasks        int64 `json:"tasks"`
	OutputImages int64 `json:"output_images"`
	Assets       int64 `json:"assets"`
	UsedBytes    int64 `json:"used_bytes"`
}

type WorkbenchOverview struct {
	// Reports visible jobs of active users. A task deletion/cancellation
	// tombstone is excluded; Cancelled is not lifetime cancellation volume.
	Scope        string                 `json:"scope"`
	Days         int                    `json:"days"`
	Since        time.Time              `json:"since"`
	Until        time.Time              `json:"until"`
	Tasks        WorkbenchTaskCounts    `json:"tasks"`
	Calls        WorkbenchCallCounts    `json:"calls"`
	OutputImages int64                  `json:"output_images"`
	SuccessRate  *float64               `json:"success_rate"`
	Queue        WorkbenchQueueCounts   `json:"queue"`
	Gallery      WorkbenchGalleryCounts `json:"gallery"`
}

// WorkbenchTaskSummary deliberately excludes prompts, asset IDs, URLs,
// storage targets, spool paths, raw upstream errors and API-key credentials.
type WorkbenchTaskSummary struct {
	TaskID         string                   `json:"task_id"`
	UserID         int64                    `json:"user_id"`
	UserEmail      string                   `json:"user_email"`
	Model          string                   `json:"model"`
	Protocol       string                   `json:"protocol"`
	Status         string                   `json:"status"`
	ImageCount     int                      `json:"image_count"`
	OutputCount    int64                    `json:"output_count"`
	CallCount      int64                    `json:"call_count"`
	BillingPending int64                    `json:"billing_pending"`
	BillingUnknown int64                    `json:"billing_unknown"`
	CreatedAt      time.Time                `json:"created_at"`
	UpdatedAt      time.Time                `json:"updated_at"`
	FinishedAt     *time.Time               `json:"finished_at"`
	ElapsedMS      *int64                   `json:"elapsed_ms"`
	ErrorCode      string                   `json:"error_code"`
	ErrorMessage   string                   `json:"error_message"`
	RouteCalls     []WorkbenchTaskRouteCall `json:"route_calls"`
}

// WorkbenchTaskRouteCall contains administrator-only observed route metadata.
// It never resolves today's account mappings to reconstruct a historical route.
type WorkbenchTaskRouteCall struct {
	Ordinal   int                     `json:"ordinal"`
	State     string                  `json:"state"` // recorded | not_dispatched | pending | unknown
	Upstreams []WorkbenchTaskUpstream `json:"upstreams"`
}

type WorkbenchTaskUpstream struct {
	AccountID     int64     `json:"account_id"`
	AccountName   string    `json:"account_name"`
	UpstreamModel *string   `json:"upstream_model"`
	Outcome       string    `json:"outcome"`
	StatusCode    *int      `json:"status_code,omitempty"`
	RecordedAt    time.Time `json:"recorded_at"`
}

// StudioJobCallRequestID is the existing deterministic internal gateway request
// identifier. It is safe to use for exact admin log correlation, together with
// user_id and api_key_id; it is not exposed by user-facing task APIs.
func StudioJobCallRequestID(userID int64, taskID string, ordinal int) string {
	return studioClientRequestID(StudioJobClaim{StudioJob: StudioJob{UserID: userID, TaskID: taskID}, Call: StudioJobCall{Ordinal: ordinal}})
}

type WorkbenchTaskFilter struct {
	Page     int
	PageSize int
	Status   string
	UserID   int64
	Model    string
	Days     int
}

type WorkbenchTaskPage struct {
	Items    []WorkbenchTaskSummary `json:"items"`
	Total    int64                  `json:"total"`
	Page     int                    `json:"page"`
	PageSize int                    `json:"page_size"`
	Pages    int64                  `json:"pages"`
}

type WorkbenchConfig struct {
	ImageStudioEnabled bool  `json:"image_studio_enabled"`
	ImageStudioGroupID int64 `json:"image_studio_group_id"`
}

type WorkbenchAdminRepository interface {
	Overview(ctx context.Context, since, until time.Time) (*WorkbenchOverview, error)
	ListTasks(ctx context.Context, filter WorkbenchTaskFilter) (*WorkbenchTaskPage, error)
}

type WorkbenchAdminService struct {
	repo     WorkbenchAdminRepository
	settings *SettingService
	groups   GroupRepository
	gallery  *ImageStudioGalleryService
}

func NewWorkbenchAdminService(repo WorkbenchAdminRepository, settings *SettingService, groups GroupRepository) *WorkbenchAdminService {
	return &WorkbenchAdminService{repo: repo, settings: settings, groups: groups}
}

func workbenchDaysValid(days int) bool { return days == 7 || days == 30 || days == 90 }

func (s *WorkbenchAdminService) Overview(ctx context.Context, days int) (*WorkbenchOverview, error) {
	if !workbenchDaysValid(days) {
		return nil, ErrWorkbenchInvalid
	}
	ctx, cancel := context.WithTimeout(ctx, 8*time.Second)
	defer cancel()
	until := time.Now().UTC()
	since := until.AddDate(0, 0, -days)
	result, err := s.repo.Overview(ctx, since, until)
	if err != nil {
		return nil, err
	}
	result.Scope, result.Days, result.Since, result.Until = "studio_cloud_jobs", days, since, until
	denominator := result.Tasks.Completed + result.Tasks.Partial + result.Tasks.Failed
	if denominator > 0 {
		rate := float64(result.Tasks.Completed) / float64(denominator)
		result.SuccessRate = &rate
	}
	return result, nil
}

func (s *WorkbenchAdminService) ListTasks(ctx context.Context, filter WorkbenchTaskFilter) (*WorkbenchTaskPage, error) {
	if filter.Page < 1 || filter.Page > 10000 || filter.PageSize < 1 || filter.PageSize > 100 || filter.UserID < 0 || filter.Days != 0 && !workbenchDaysValid(filter.Days) {
		return nil, ErrWorkbenchInvalid
	}
	switch filter.Status {
	case "", StudioJobQueued, StudioJobGenerating, StudioJobSaving, StudioJobCompleted, StudioJobPartial, StudioJobFailed, StudioJobUnknown, StudioJobCancelled:
	default:
		return nil, ErrWorkbenchInvalid
	}
	filter.Model = strings.TrimSpace(filter.Model)
	if len(filter.Model) > 128 || !utf8.ValidString(filter.Model) {
		return nil, ErrWorkbenchInvalid
	}
	for _, ch := range filter.Model {
		if unicode.IsControl(ch) {
			return nil, ErrWorkbenchInvalid
		}
	}
	ctx, cancel := context.WithTimeout(ctx, 8*time.Second)
	defer cancel()
	page, err := s.repo.ListTasks(ctx, filter)
	if err != nil || page == nil {
		return page, err
	}
	for i := range page.Items {
		page.Items[i].ErrorMessage = SanitizeStudioErrorMessage(page.Items[i].ErrorMessage)
	}
	return page, nil
}

func (s *WorkbenchAdminService) GetConfig(ctx context.Context) (*WorkbenchConfig, error) {
	return s.settings.GetWorkbenchConfig(ctx)
}

func (s *WorkbenchAdminService) UpdateConfig(ctx context.Context, cfg WorkbenchConfig) (*WorkbenchConfig, error) {
	if cfg.ImageStudioGroupID < 0 || cfg.ImageStudioEnabled && cfg.ImageStudioGroupID == 0 {
		return nil, ErrWorkbenchInvalid
	}
	if cfg.ImageStudioEnabled && cfg.ImageStudioGroupID > 0 {
		group, err := s.groups.GetByID(ctx, cfg.ImageStudioGroupID)
		if err != nil || group == nil || group.Status != StatusActive || !GroupAllowsImageGeneration(group) {
			return nil, ErrWorkbenchInvalid
		}
	}
	if err := s.settings.UpdateWorkbenchConfig(ctx, cfg); err != nil {
		return nil, err
	}
	return &cfg, nil
}
