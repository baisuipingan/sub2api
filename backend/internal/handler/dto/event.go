package dto

import (
	"sort"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/service"
)

type EventCategory struct {
	ID        int64  `json:"id"`
	Code      string `json:"code"`
	Name      string `json:"name"`
	Color     string `json:"color"`
	Icon      string `json:"icon"`
	SortOrder int    `json:"sort_order"`
	Enabled   bool   `json:"enabled"`
}

type EventSource struct {
	ID         int64          `json:"id"`
	Code       string         `json:"code"`
	Name       string         `json:"name"`
	Kind       string         `json:"kind"`
	Enabled    bool           `json:"enabled"`
	Config     map[string]any `json:"config"`
	LastSyncAt *time.Time     `json:"last_sync_at,omitempty"`
}

type EventOccurrence struct {
	ID               int64      `json:"id"`
	StartsAt         time.Time  `json:"starts_at"`
	EndsAt           *time.Time `json:"ends_at,omitempty"`
	Timezone         string     `json:"timezone"`
	AllDay           bool       `json:"all_day"`
	LocationMode     string     `json:"location_mode"`
	OnlineURL        string     `json:"online_url,omitempty"`
	VenueName        string     `json:"venue_name,omitempty"`
	Address          string     `json:"address,omitempty"`
	Country          string     `json:"country,omitempty"`
	Province         string     `json:"province,omitempty"`
	City             string     `json:"city,omitempty"`
	District         string     `json:"district,omitempty"`
	Latitude         *float64   `json:"latitude,omitempty"`
	Longitude        *float64   `json:"longitude,omitempty"`
	CoordinateSource string     `json:"coordinate_source"`
	GeocodeStatus    string     `json:"geocode_status,omitempty"`
	GeocodePrecision string     `json:"geocode_precision,omitempty"`
	ProviderPlaceID  string     `json:"provider_place_id,omitempty"`
}

type Event struct {
	ID                   int64                 `json:"id"`
	CategoryID           *int64                `json:"category_id,omitempty"`
	Category             *EventCategory        `json:"category,omitempty"`
	Title                string                `json:"title"`
	Summary              string                `json:"summary"`
	DescriptionMarkdown  string                `json:"description_markdown"`
	Tags                 []string              `json:"tags"`
	OrganizerName        string                `json:"organizer_name"`
	OrganizerURL         string                `json:"organizer_url,omitempty"`
	FeeType              string                `json:"fee_type"`
	PriceMin             *float64              `json:"price_min,omitempty"`
	PriceMax             *float64              `json:"price_max,omitempty"`
	Currency             string                `json:"currency"`
	RegistrationURL      string                `json:"registration_url,omitempty"`
	RegistrationDeadline *time.Time            `json:"registration_deadline,omitempty"`
	CoverURL             string                `json:"cover_url,omitempty"`
	Status               string                `json:"status"`
	Phase                string                `json:"phase"`
	Visibility           string                `json:"visibility"`
	Audience             service.EventAudience `json:"audience"`
	VisibleFrom          *time.Time            `json:"visible_from,omitempty"`
	VisibleUntil         *time.Time            `json:"visible_until,omitempty"`
	PublishedAt          *time.Time            `json:"published_at,omitempty"`
	CancelledReason      string                `json:"cancelled_reason,omitempty"`
	ManualOverrideFields []string              `json:"manual_override_fields,omitempty"`
	Occurrences          []EventOccurrence     `json:"occurrences"`
	CreatedAt            time.Time             `json:"created_at"`
	UpdatedAt            time.Time             `json:"updated_at"`
}

// UserEvent intentionally excludes audience rules and import-maintenance
// metadata. Those fields are only useful to administrators and would expose
// internal subscription-group IDs to the user-facing API.
type UserEvent struct {
	ID                   int64             `json:"id"`
	CategoryID           *int64            `json:"category_id,omitempty"`
	Category             *EventCategory    `json:"category,omitempty"`
	Title                string            `json:"title"`
	Summary              string            `json:"summary"`
	DescriptionMarkdown  string            `json:"description_markdown"`
	Tags                 []string          `json:"tags"`
	OrganizerName        string            `json:"organizer_name"`
	OrganizerURL         string            `json:"organizer_url,omitempty"`
	FeeType              string            `json:"fee_type"`
	PriceMin             *float64          `json:"price_min,omitempty"`
	PriceMax             *float64          `json:"price_max,omitempty"`
	Currency             string            `json:"currency"`
	RegistrationURL      string            `json:"registration_url,omitempty"`
	RegistrationDeadline *time.Time        `json:"registration_deadline,omitempty"`
	CoverURL             string            `json:"cover_url,omitempty"`
	Status               string            `json:"status"`
	Phase                string            `json:"phase"`
	CancelledReason      string            `json:"cancelled_reason,omitempty"`
	Occurrences          []EventOccurrence `json:"occurrences"`
	PublishedAt          *time.Time        `json:"published_at,omitempty"`
}

type EventMapMarker struct {
	EventID      int64          `json:"event_id"`
	OccurrenceID int64          `json:"occurrence_id"`
	Title        string         `json:"title"`
	Summary      string         `json:"summary"`
	Status       string         `json:"status"`
	Phase        string         `json:"phase"`
	Category     *EventCategory `json:"category,omitempty"`
	FeeType      string         `json:"fee_type"`
	StartsAt     time.Time      `json:"starts_at"`
	EndsAt       *time.Time     `json:"ends_at,omitempty"`
	VenueName    string         `json:"venue_name,omitempty"`
	Address      string         `json:"address,omitempty"`
	City         string         `json:"city,omitempty"`
	District     string         `json:"district,omitempty"`
	Latitude     float64        `json:"latitude"`
	Longitude    float64        `json:"longitude"`
}

type EventImportItem struct {
	ID          int64  `json:"id"`
	ItemIndex   int    `json:"item_index"`
	ExternalID  string `json:"external_id,omitempty"`
	Action      string `json:"action"`
	Status      string `json:"status"`
	EventID     *int64 `json:"event_id,omitempty"`
	ErrorCode   string `json:"error_code,omitempty"`
	ErrorDetail string `json:"error_detail,omitempty"`
}

type EventImportBatch struct {
	ID             int64             `json:"id"`
	SourceID       int64             `json:"source_id"`
	FileName       string            `json:"file_name"`
	SchemaVersion  int               `json:"schema_version"`
	Mode           string            `json:"mode"`
	Status         string            `json:"status"`
	TotalCount     int               `json:"total_count"`
	CreateCount    int               `json:"create_count"`
	UpdateCount    int               `json:"update_count"`
	UnchangedCount int               `json:"unchanged_count"`
	ConflictCount  int               `json:"conflict_count"`
	ErrorCount     int               `json:"error_count"`
	CommittedAt    *time.Time        `json:"committed_at,omitempty"`
	CreatedAt      time.Time         `json:"created_at"`
	Items          []EventImportItem `json:"items"`
}

func EventFromService(value *service.Event) *Event {
	if value == nil {
		return nil
	}
	out := &Event{
		ID: value.ID, CategoryID: value.CategoryID, Title: value.Title, Summary: value.Summary,
		DescriptionMarkdown: value.DescriptionMarkdown, Tags: append([]string(nil), value.Tags...),
		OrganizerName: value.OrganizerName, OrganizerURL: value.OrganizerURL, FeeType: value.FeeType,
		PriceMin: value.PriceMin, PriceMax: value.PriceMax, Currency: value.Currency,
		RegistrationURL: value.RegistrationURL, RegistrationDeadline: value.RegistrationDeadline,
		CoverURL: value.CoverURL, Status: value.Status, Phase: eventPhase(value, time.Now()),
		Visibility: value.Visibility, Audience: value.Audience, VisibleFrom: value.VisibleFrom,
		VisibleUntil: value.VisibleUntil, PublishedAt: value.PublishedAt, CancelledReason: value.CancelledReason,
		ManualOverrideFields: append([]string(nil), value.ManualOverrideFields...),
		Occurrences:          make([]EventOccurrence, 0, len(value.Occurrences)), CreatedAt: value.CreatedAt, UpdatedAt: value.UpdatedAt,
	}
	if value.Category != nil {
		category := EventCategoryFromService(*value.Category)
		out.Category = &category
	}
	for i := range value.Occurrences {
		out.Occurrences = append(out.Occurrences, EventOccurrenceFromService(value.Occurrences[i]))
	}
	return out
}

func EventsFromService(values []service.Event) []Event {
	out := make([]Event, 0, len(values))
	for i := range values {
		out = append(out, *EventFromService(&values[i]))
	}
	return out
}

func UserEventFromService(value *service.Event) *UserEvent {
	adminEvent := EventFromService(value)
	if adminEvent == nil {
		return nil
	}
	return &UserEvent{
		ID: adminEvent.ID, CategoryID: adminEvent.CategoryID, Category: adminEvent.Category,
		Title: adminEvent.Title, Summary: adminEvent.Summary, DescriptionMarkdown: adminEvent.DescriptionMarkdown,
		Tags: adminEvent.Tags, OrganizerName: adminEvent.OrganizerName, OrganizerURL: adminEvent.OrganizerURL,
		FeeType: adminEvent.FeeType, PriceMin: adminEvent.PriceMin, PriceMax: adminEvent.PriceMax,
		Currency: adminEvent.Currency, RegistrationURL: adminEvent.RegistrationURL,
		RegistrationDeadline: adminEvent.RegistrationDeadline, CoverURL: adminEvent.CoverURL,
		Status: adminEvent.Status, Phase: adminEvent.Phase, CancelledReason: adminEvent.CancelledReason,
		Occurrences: adminEvent.Occurrences, PublishedAt: adminEvent.PublishedAt,
	}
}

func UserEventsFromService(values []service.Event) []UserEvent {
	out := make([]UserEvent, 0, len(values))
	for i := range values {
		out = append(out, *UserEventFromService(&values[i]))
	}
	return out
}

func EventOccurrenceFromService(value service.EventOccurrence) EventOccurrence {
	return EventOccurrence{
		ID: value.ID, StartsAt: value.StartsAt, EndsAt: value.EndsAt, Timezone: value.Timezone,
		AllDay: value.AllDay, LocationMode: value.LocationMode, OnlineURL: value.OnlineURL,
		VenueName: value.VenueName, Address: value.Address, Country: value.Country,
		Province: value.Province, City: value.City, District: value.District,
		Latitude: value.Latitude, Longitude: value.Longitude, CoordinateSource: value.CoordinateSource,
		GeocodeStatus: value.GeocodeStatus, GeocodePrecision: value.GeocodePrecision,
		ProviderPlaceID: value.ProviderPlaceID,
	}
}

func EventCategoryFromService(value service.EventCategory) EventCategory {
	return EventCategory{ID: value.ID, Code: value.Code, Name: value.Name, Color: value.Color, Icon: value.Icon, SortOrder: value.SortOrder, Enabled: value.Enabled}
}

func EventCategoriesFromService(values []service.EventCategory) []EventCategory {
	out := make([]EventCategory, 0, len(values))
	for i := range values {
		out = append(out, EventCategoryFromService(values[i]))
	}
	return out
}

func EventSourceFromService(value service.EventSource) EventSource {
	return EventSource{ID: value.ID, Code: value.Code, Name: value.Name, Kind: value.Kind, Enabled: value.Enabled, Config: value.Config, LastSyncAt: value.LastSyncAt}
}

func EventSourcesFromService(values []service.EventSource) []EventSource {
	out := make([]EventSource, 0, len(values))
	for i := range values {
		out = append(out, EventSourceFromService(values[i]))
	}
	return out
}

func EventMapMarkersFromService(values []service.Event) []EventMapMarker {
	markers := make([]EventMapMarker, 0)
	for i := range values {
		value := &values[i]
		phase := eventPhase(value, time.Now())
		var category *EventCategory
		if value.Category != nil {
			mapped := EventCategoryFromService(*value.Category)
			category = &mapped
		}
		for j := range value.Occurrences {
			occurrence := value.Occurrences[j]
			if occurrence.Latitude == nil || occurrence.Longitude == nil {
				continue
			}
			markers = append(markers, EventMapMarker{
				EventID: value.ID, OccurrenceID: occurrence.ID, Title: value.Title, Summary: value.Summary,
				Status: value.Status, Phase: phase, Category: category, FeeType: value.FeeType,
				StartsAt: occurrence.StartsAt, EndsAt: occurrence.EndsAt, VenueName: occurrence.VenueName,
				Address: occurrence.Address, City: occurrence.City, District: occurrence.District,
				Latitude: *occurrence.Latitude, Longitude: *occurrence.Longitude,
			})
		}
	}
	sort.Slice(markers, func(i, j int) bool { return markers[i].StartsAt.Before(markers[j].StartsAt) })
	return markers
}

func EventImportBatchFromService(value *service.EventImportBatch) *EventImportBatch {
	if value == nil {
		return nil
	}
	out := &EventImportBatch{
		ID: value.ID, SourceID: value.SourceID, FileName: value.FileName, SchemaVersion: value.SchemaVersion,
		Mode: value.Mode, Status: value.Status, TotalCount: value.TotalCount, CreateCount: value.CreateCount,
		UpdateCount: value.UpdateCount, UnchangedCount: value.UnchangedCount, ConflictCount: value.ConflictCount,
		ErrorCount: value.ErrorCount, CommittedAt: value.CommittedAt, CreatedAt: value.CreatedAt,
		Items: make([]EventImportItem, 0, len(value.Items)),
	}
	for i := range value.Items {
		item := value.Items[i]
		out.Items = append(out.Items, EventImportItem{
			ID: item.ID, ItemIndex: item.ItemIndex, ExternalID: item.ExternalID, Action: item.Action,
			Status: item.Status, EventID: item.EventID, ErrorCode: item.ErrorCode, ErrorDetail: item.ErrorDetail,
		})
	}
	return out
}

func eventPhase(value *service.Event, now time.Time) string {
	if value.Status == service.EventStatusCancelled {
		return "cancelled"
	}
	hasFuture := false
	for i := range value.Occurrences {
		occurrence := value.Occurrences[i]
		if now.Before(occurrence.StartsAt) {
			hasFuture = true
			continue
		}
		if occurrence.EndsAt == nil || now.Before(*occurrence.EndsAt) {
			return "ongoing"
		}
	}
	if hasFuture {
		return "upcoming"
	}
	return "ended"
}
