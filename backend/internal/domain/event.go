package domain

import "time"

const (
	EventStatusDraft     = "draft"
	EventStatusPublished = "published"
	EventStatusCancelled = "cancelled"
	EventStatusArchived  = "archived"
)

const (
	EventVisibilityAuthenticated = "authenticated"
	EventVisibilityTargeted      = "targeted"
)

const (
	EventLocationOffline = "offline"
	EventLocationOnline  = "online"
	EventLocationHybrid  = "hybrid"
)

const (
	EventFeeFree    = "free"
	EventFeePaid    = "paid"
	EventFeeUnknown = "unknown"
)

const (
	EventCoordinateWGS84 = "wgs84"
	EventCoordinateGCJ02 = "gcj02"
)

const (
	EventSourceManual  = "manual"
	EventSourceJSON    = "json"
	EventSourceCrawler = "crawler"
)

const (
	EventImportStatusPreviewed  = "previewed"
	EventImportStatusCommitting = "committing"
	EventImportStatusCompleted  = "completed"
	EventImportStatusPartial    = "partial"
	EventImportStatusFailed     = "failed"
)

const (
	EventImportActionCreate    = "create"
	EventImportActionUpdate    = "update"
	EventImportActionUnchanged = "unchanged"
	EventImportActionConflict  = "conflict"
	EventImportActionError     = "error"
)

// EventAudience keeps the initial event visibility contract deliberately small.
// Empty rules mean all authenticated users. The JSON shape can evolve without a
// table migration when additional audience conditions are introduced later.
type EventAudience struct {
	SubscriptionGroupIDs []int64 `json:"subscription_group_ids,omitempty"`
}

type Event struct {
	ID                   int64
	CategoryID           *int64
	Title                string
	Summary              string
	DescriptionMarkdown  string
	Tags                 []string
	OrganizerName        string
	OrganizerURL         string
	FeeType              string
	PriceMin             *float64
	PriceMax             *float64
	Currency             string
	RegistrationURL      string
	RegistrationDeadline *time.Time
	CoverURL             string
	Status               string
	Visibility           string
	Audience             EventAudience
	VisibleFrom          *time.Time
	VisibleUntil         *time.Time
	PublishedAt          *time.Time
	CancelledReason      string
	ManualOverrideFields []string
	CreatedBy            *int64
	UpdatedBy            *int64
	CreatedAt            time.Time
	UpdatedAt            time.Time
	DeletedAt            *time.Time
	Category             *EventCategory
	Occurrences          []EventOccurrence
}

type EventCategory struct {
	ID        int64
	Code      string
	Name      string
	Color     string
	Icon      string
	SortOrder int
	Enabled   bool
	CreatedAt time.Time
	UpdatedAt time.Time
}

type EventOccurrence struct {
	ID               int64
	EventID          int64
	StartsAt         time.Time
	EndsAt           *time.Time
	Timezone         string
	AllDay           bool
	LocationMode     string
	OnlineURL        string
	VenueName        string
	Address          string
	Country          string
	Province         string
	City             string
	District         string
	Latitude         *float64
	Longitude        *float64
	CoordinateSource string
	GeocodeStatus    string
	GeocodePrecision string
	ProviderPlaceID  string
	CreatedAt        time.Time
	UpdatedAt        time.Time
}

type EventSource struct {
	ID         int64
	Code       string
	Name       string
	Kind       string
	Enabled    bool
	Config     map[string]any
	LastSyncAt *time.Time
	CreatedAt  time.Time
	UpdatedAt  time.Time
}

type EventSourceRecord struct {
	ID                int64
	SourceID          int64
	EventID           *int64
	ExternalID        string
	SourceURL         string
	Fingerprint       string
	ContentHash       string
	State             string
	RawPayload        map[string]any
	NormalizedPayload map[string]any
	SourceUpdatedAt   *time.Time
	FirstSeenAt       time.Time
	LastSeenAt        time.Time
	CreatedAt         time.Time
	UpdatedAt         time.Time
}

type EventImportBatch struct {
	ID             int64
	SourceID       int64
	FileName       string
	FileHash       string
	SchemaVersion  int
	Mode           string
	Status         string
	TotalCount     int
	CreateCount    int
	UpdateCount    int
	UnchangedCount int
	ConflictCount  int
	ErrorCount     int
	CreatedBy      int64
	CommittedAt    *time.Time
	CreatedAt      time.Time
	UpdatedAt      time.Time
	Items          []EventImportItem
}

type EventImportItem struct {
	ID                int64
	BatchID           int64
	ItemIndex         int
	ExternalID        string
	Fingerprint       string
	ContentHash       string
	Action            string
	Status            string
	EventID           *int64
	ErrorCode         string
	ErrorDetail       string
	NormalizedPayload map[string]any
	CreatedAt         time.Time
	UpdatedAt         time.Time
}
