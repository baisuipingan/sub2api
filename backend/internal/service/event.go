package service

import (
	"context"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/domain"
	infraerrors "github.com/Wei-Shaw/sub2api/internal/pkg/errors"
	"github.com/Wei-Shaw/sub2api/internal/pkg/pagination"
)

const (
	EventStatusDraft     = domain.EventStatusDraft
	EventStatusPublished = domain.EventStatusPublished
	EventStatusCancelled = domain.EventStatusCancelled
	EventStatusArchived  = domain.EventStatusArchived
)

const (
	EventVisibilityAuthenticated = domain.EventVisibilityAuthenticated
	EventVisibilityTargeted      = domain.EventVisibilityTargeted
)

var (
	ErrEventNotFound         = infraerrors.NotFound("EVENT_NOT_FOUND", "event not found")
	ErrEventCategoryNotFound = infraerrors.NotFound("EVENT_CATEGORY_NOT_FOUND", "event category not found")
	ErrEventSourceNotFound   = infraerrors.NotFound("EVENT_SOURCE_NOT_FOUND", "event source not found")
	ErrEventImportNotFound   = infraerrors.NotFound("EVENT_IMPORT_NOT_FOUND", "event import batch not found")
	ErrEventInvalid          = infraerrors.BadRequest("EVENT_INVALID", "event data is invalid")
	ErrEventConflict         = infraerrors.Conflict("EVENT_CONFLICT", "event conflicts with existing data")
	ErrEventImportState      = infraerrors.Conflict("EVENT_IMPORT_STATE_INVALID", "event import batch cannot be committed")
)

type Event = domain.Event
type EventCategory = domain.EventCategory
type EventOccurrence = domain.EventOccurrence
type EventSource = domain.EventSource
type EventSourceRecord = domain.EventSourceRecord
type EventImportBatch = domain.EventImportBatch
type EventImportItem = domain.EventImportItem
type EventAudience = domain.EventAudience

type EventListFilters struct {
	Status       string
	CategoryCode string
	Search       string
	City         string
	District     string
	FeeType      string
	From         *time.Time
	To           *time.Time
}

type EventMapBounds struct {
	MinLongitude float64
	MinLatitude  float64
	MaxLongitude float64
	MaxLatitude  float64
}

type EventMapFilters struct {
	EventListFilters
	Bounds *EventMapBounds
	Limit  int
}

type EventOccurrenceInput struct {
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
}

type CreateEventInput struct {
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
	CancelledReason      string
	Occurrences          []EventOccurrenceInput
	ActorID              int64
}

type UpdateEventInput struct {
	CreateEventInput
	ManualEdit bool
}

type EventRepository interface {
	Create(ctx context.Context, event *Event) error
	Update(ctx context.Context, event *Event) error
	Delete(ctx context.Context, id int64) error
	GetByID(ctx context.Context, id int64) (*Event, error)
	ListAdmin(ctx context.Context, params pagination.PaginationParams, filters EventListFilters) ([]Event, *pagination.PaginationResult, error)
	ListPublishedForUser(ctx context.Context, params pagination.PaginationParams, filters EventListFilters, audienceGroupIDs []int64) ([]Event, *pagination.PaginationResult, error)
	ListPublishedMapForUser(ctx context.Context, filters EventMapFilters, audienceGroupIDs []int64) ([]Event, error)

	ListCategories(ctx context.Context, includeDisabled bool) ([]EventCategory, error)
	CreateCategory(ctx context.Context, category *EventCategory) error
	UpdateCategory(ctx context.Context, category *EventCategory) error
	DeleteCategory(ctx context.Context, id int64) error
	GetCategoryByID(ctx context.Context, id int64) (*EventCategory, error)

	ListSources(ctx context.Context, includeDisabled bool) ([]EventSource, error)
	CreateSource(ctx context.Context, source *EventSource) error
	UpdateSource(ctx context.Context, source *EventSource) error
	DeleteSource(ctx context.Context, id int64) error
	GetSourceByID(ctx context.Context, id int64) (*EventSource, error)
	GetSourceByCode(ctx context.Context, code string) (*EventSource, error)

	GetSourceRecordByExternalID(ctx context.Context, sourceID int64, externalID string) (*EventSourceRecord, error)
	GetSourceRecordBySourceAndFingerprint(ctx context.Context, sourceID int64, fingerprint string) (*EventSourceRecord, error)
	GetSourceRecordByFingerprint(ctx context.Context, fingerprint string) (*EventSourceRecord, error)
	SaveImportedEvent(ctx context.Context, event *Event, record *EventSourceRecord) error
	CreateImportBatch(ctx context.Context, batch *EventImportBatch) error
	GetImportBatch(ctx context.Context, id int64) (*EventImportBatch, error)
	ClaimImportBatch(ctx context.Context, id int64) (bool, error)
	UpdateImportBatch(ctx context.Context, batch *EventImportBatch) error
	UpdateImportItem(ctx context.Context, item *EventImportItem) error
}
