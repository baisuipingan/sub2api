package service

import (
	"context"
	"encoding/json"
	"fmt"
	"regexp"
	"sort"
	"strings"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/domain"
	"github.com/Wei-Shaw/sub2api/internal/pkg/pagination"
	"github.com/Wei-Shaw/sub2api/internal/util/urlvalidator"
)

const (
	maxEventOccurrences       = 50
	maxEventTags              = 20
	maxEventSourceConfigBytes = 64 * 1024
)

var eventCodePattern = regexp.MustCompile(`^[a-z0-9][a-z0-9_-]{0,63}$`)

type EventService struct {
	repo        EventRepository
	userSubRepo UserSubscriptionRepository
	settings    *SettingService
}

func NewEventService(repo EventRepository, userSubRepo UserSubscriptionRepository, settings *SettingService) *EventService {
	return &EventService{repo: repo, userSubRepo: userSubRepo, settings: settings}
}

func (s *EventService) Create(ctx context.Context, input *CreateEventInput) (*Event, error) {
	event, err := normalizeEventInput(input)
	if err != nil {
		return nil, err
	}
	if input.ActorID > 0 {
		event.CreatedBy = &input.ActorID
		event.UpdatedBy = &input.ActorID
	}
	if event.Status == EventStatusPublished {
		now := time.Now()
		event.PublishedAt = &now
	}
	if err := s.repo.Create(ctx, event); err != nil {
		return nil, fmt.Errorf("create event: %w", err)
	}
	return event, nil
}

func (s *EventService) Update(ctx context.Context, id int64, input *UpdateEventInput) (*Event, error) {
	if input == nil {
		return nil, ErrEventInvalid
	}
	existing, err := s.repo.GetByID(ctx, id)
	if err != nil {
		return nil, err
	}
	normalized, err := normalizeEventInput(&input.CreateEventInput)
	if err != nil {
		return nil, err
	}
	normalized.ID = existing.ID
	normalized.CreatedAt = existing.CreatedAt
	normalized.CreatedBy = existing.CreatedBy
	normalized.PublishedAt = existing.PublishedAt
	normalized.ManualOverrideFields = append([]string(nil), existing.ManualOverrideFields...)
	if input.ActorID > 0 {
		normalized.UpdatedBy = &input.ActorID
	}
	if existing.Status != EventStatusPublished && normalized.Status == EventStatusPublished {
		now := time.Now()
		normalized.PublishedAt = &now
	}
	if input.ManualEdit {
		normalized.ManualOverrideFields = allEventImportManagedFields()
	}
	if err := s.repo.Update(ctx, normalized); err != nil {
		return nil, fmt.Errorf("update event: %w", err)
	}
	return normalized, nil
}

func (s *EventService) Delete(ctx context.Context, id int64) error {
	if err := s.repo.Delete(ctx, id); err != nil {
		return fmt.Errorf("delete event: %w", err)
	}
	return nil
}

func (s *EventService) SetStatus(ctx context.Context, id int64, status, reason string, actorID int64) (*Event, error) {
	event, err := s.repo.GetByID(ctx, id)
	if err != nil {
		return nil, err
	}
	status = strings.TrimSpace(status)
	if !validEventStatus(status) || status == EventStatusDraft && event.Status == EventStatusCancelled {
		return nil, ErrEventInvalid
	}
	event.Status = status
	event.CancelledReason = ""
	if status == EventStatusCancelled {
		event.CancelledReason = strings.TrimSpace(reason)
		if event.CancelledReason == "" {
			return nil, ErrEventInvalid
		}
	}
	if status == EventStatusPublished && event.PublishedAt == nil {
		now := time.Now()
		event.PublishedAt = &now
	}
	if actorID > 0 {
		event.UpdatedBy = &actorID
	}
	if err := s.repo.Update(ctx, event); err != nil {
		return nil, fmt.Errorf("update event status: %w", err)
	}
	return event, nil
}

func (s *EventService) ListCategories(ctx context.Context, includeDisabled bool) ([]EventCategory, error) {
	return s.repo.ListCategories(ctx, includeDisabled)
}

func (s *EventService) CreateCategory(ctx context.Context, input EventCategory) (*EventCategory, error) {
	value, err := normalizeEventCategory(input)
	if err != nil {
		return nil, err
	}
	if err := s.repo.CreateCategory(ctx, value); err != nil {
		return nil, fmt.Errorf("create event category: %w", err)
	}
	return value, nil
}

func (s *EventService) UpdateCategory(ctx context.Context, id int64, input EventCategory) (*EventCategory, error) {
	value, err := normalizeEventCategory(input)
	if err != nil {
		return nil, err
	}
	value.ID = id
	if err := s.repo.UpdateCategory(ctx, value); err != nil {
		return nil, fmt.Errorf("update event category: %w", err)
	}
	return value, nil
}

func (s *EventService) DeleteCategory(ctx context.Context, id int64) error {
	if err := s.repo.DeleteCategory(ctx, id); err != nil {
		return fmt.Errorf("delete event category: %w", err)
	}
	return nil
}

func (s *EventService) ListSources(ctx context.Context, includeDisabled bool) ([]EventSource, error) {
	return s.repo.ListSources(ctx, includeDisabled)
}

func (s *EventService) CreateSource(ctx context.Context, input EventSource) (*EventSource, error) {
	value, err := normalizeEventSource(input)
	if err != nil {
		return nil, err
	}
	if err := s.repo.CreateSource(ctx, value); err != nil {
		return nil, fmt.Errorf("create event source: %w", err)
	}
	return value, nil
}

func (s *EventService) UpdateSource(ctx context.Context, id int64, input EventSource) (*EventSource, error) {
	value, err := normalizeEventSource(input)
	if err != nil {
		return nil, err
	}
	value.ID = id
	if err := s.repo.UpdateSource(ctx, value); err != nil {
		return nil, fmt.Errorf("update event source: %w", err)
	}
	return value, nil
}

func (s *EventService) DeleteSource(ctx context.Context, id int64) error {
	if err := s.repo.DeleteSource(ctx, id); err != nil {
		return fmt.Errorf("delete event source: %w", err)
	}
	return nil
}

func (s *EventService) GetAdmin(ctx context.Context, id int64) (*Event, error) {
	return s.repo.GetByID(ctx, id)
}

func (s *EventService) ListAdmin(ctx context.Context, params pagination.PaginationParams, filters EventListFilters) ([]Event, *pagination.PaginationResult, error) {
	return s.repo.ListAdmin(ctx, params, normalizeEventFilters(filters))
}

func (s *EventService) GetForUser(ctx context.Context, userID, eventID int64) (*Event, error) {
	if !s.settings.IsEventCenterEnabled(ctx) {
		return nil, ErrEventNotFound
	}
	event, err := s.repo.GetByID(ctx, eventID)
	if err != nil {
		return nil, err
	}
	groups, err := s.activeSubscriptionGroups(ctx, userID)
	if err != nil {
		return nil, fmt.Errorf("load event audience: %w", err)
	}
	if !eventVisibleToUser(event, time.Now(), groups) {
		return nil, ErrEventNotFound
	}
	return event, nil
}

func (s *EventService) ListForUser(ctx context.Context, userID int64, params pagination.PaginationParams, filters EventListFilters) ([]Event, *pagination.PaginationResult, error) {
	if !s.settings.IsEventCenterEnabled(ctx) {
		return nil, nil, ErrEventNotFound
	}
	groups, err := s.activeSubscriptionGroups(ctx, userID)
	if err != nil {
		return nil, nil, fmt.Errorf("load event audience: %w", err)
	}
	return s.repo.ListPublishedForUser(ctx, params, normalizeEventFilters(filters), eventAudienceGroupIDs(groups))
}

func (s *EventService) MapForUser(ctx context.Context, userID int64, filters EventMapFilters) ([]Event, bool, error) {
	if !s.settings.IsEventCenterEnabled(ctx) {
		return nil, false, ErrEventNotFound
	}
	filters.EventListFilters = normalizeEventFilters(filters.EventListFilters)
	if filters.Limit <= 0 || filters.Limit > 2000 {
		filters.Limit = 2000
	}
	groups, err := s.activeSubscriptionGroups(ctx, userID)
	if err != nil {
		return nil, false, fmt.Errorf("load event audience: %w", err)
	}
	visible, err := s.repo.ListPublishedMapForUser(ctx, filters, eventAudienceGroupIDs(groups))
	if err != nil {
		return nil, false, err
	}
	limited, truncated := limitEventMapOccurrences(visible, filters.Limit)
	return limited, truncated, nil
}

func (s *EventService) ListCategoriesForUser(ctx context.Context) ([]EventCategory, error) {
	if !s.settings.IsEventCenterEnabled(ctx) {
		return nil, ErrEventNotFound
	}
	return s.repo.ListCategories(ctx, false)
}

func (s *EventService) activeSubscriptionGroups(ctx context.Context, userID int64) (map[int64]struct{}, error) {
	subs, err := s.userSubRepo.ListActiveByUserID(ctx, userID)
	if err != nil {
		return nil, err
	}
	groups := make(map[int64]struct{}, len(subs))
	for i := range subs {
		groups[subs[i].GroupID] = struct{}{}
	}
	return groups, nil
}

func eventAudienceGroupIDs(groups map[int64]struct{}) []int64 {
	ids := make([]int64, 0, len(groups))
	for id := range groups {
		if id > 0 {
			ids = append(ids, id)
		}
	}
	sort.Slice(ids, func(i, j int) bool { return ids[i] < ids[j] })
	return ids
}

func limitEventMapOccurrences(events []Event, limit int) ([]Event, bool) {
	if limit <= 0 {
		return nil, len(events) > 0
	}
	out := make([]Event, 0, len(events))
	remaining := limit
	for i := range events {
		if remaining == 0 {
			return out, true
		}
		value := events[i]
		if len(value.Occurrences) > remaining {
			value.Occurrences = append([]EventOccurrence(nil), value.Occurrences[:remaining]...)
			out = append(out, value)
			return out, true
		}
		out = append(out, value)
		remaining -= len(value.Occurrences)
	}
	return out, false
}

func eventVisibleToUser(event *Event, now time.Time, groups map[int64]struct{}) bool {
	if event == nil || (event.Status != EventStatusPublished && event.Status != EventStatusCancelled) {
		return false
	}
	if event.VisibleFrom != nil && now.Before(*event.VisibleFrom) {
		return false
	}
	if event.VisibleUntil != nil && !now.Before(*event.VisibleUntil) {
		return false
	}
	if event.Visibility != EventVisibilityTargeted {
		return true
	}
	if len(event.Audience.SubscriptionGroupIDs) == 0 {
		return false
	}
	for _, id := range event.Audience.SubscriptionGroupIDs {
		if _, ok := groups[id]; ok {
			return true
		}
	}
	return false
}

func normalizeEventInput(input *CreateEventInput) (*Event, error) {
	if input == nil {
		return nil, ErrEventInvalid
	}
	title := strings.TrimSpace(input.Title)
	if title == "" || len([]rune(title)) > 200 {
		return nil, ErrEventInvalid
	}
	if len(input.Occurrences) == 0 || len(input.Occurrences) > maxEventOccurrences {
		return nil, ErrEventInvalid
	}
	status := strings.TrimSpace(input.Status)
	if status == "" {
		status = EventStatusDraft
	}
	if !validEventStatus(status) {
		return nil, ErrEventInvalid
	}
	cancelledReason := strings.TrimSpace(input.CancelledReason)
	if status == EventStatusCancelled && cancelledReason == "" {
		return nil, ErrEventInvalid
	}
	if status != EventStatusCancelled {
		cancelledReason = ""
	}
	visibility := strings.TrimSpace(input.Visibility)
	if visibility == "" {
		visibility = EventVisibilityAuthenticated
	}
	if visibility != EventVisibilityAuthenticated && visibility != EventVisibilityTargeted {
		return nil, ErrEventInvalid
	}
	audience, err := normalizeEventAudience(input.Audience)
	if err != nil || (visibility == EventVisibilityTargeted && len(audience.SubscriptionGroupIDs) == 0) {
		return nil, ErrEventInvalid
	}
	if input.VisibleFrom != nil && input.VisibleUntil != nil && !input.VisibleFrom.Before(*input.VisibleUntil) {
		return nil, ErrEventInvalid
	}
	feeType := strings.TrimSpace(input.FeeType)
	if feeType == "" {
		feeType = domain.EventFeeUnknown
	}
	if feeType != domain.EventFeeFree && feeType != domain.EventFeePaid && feeType != domain.EventFeeUnknown {
		return nil, ErrEventInvalid
	}
	if !validPriceRange(input.PriceMin, input.PriceMax) {
		return nil, ErrEventInvalid
	}
	currency := strings.ToUpper(strings.TrimSpace(input.Currency))
	if currency == "" {
		currency = "CNY"
	}
	if len(currency) > 8 {
		return nil, ErrEventInvalid
	}
	organizerURL, err := normalizeOptionalEventURL(input.OrganizerURL)
	if err != nil {
		return nil, ErrEventInvalid
	}
	registrationURL, err := normalizeOptionalEventURL(input.RegistrationURL)
	if err != nil {
		return nil, ErrEventInvalid
	}
	coverURL, err := normalizeOptionalEventURL(input.CoverURL)
	if err != nil {
		return nil, ErrEventInvalid
	}
	occurrences := make([]EventOccurrence, 0, len(input.Occurrences))
	for i := range input.Occurrences {
		occurrence, err := normalizeOccurrenceInput(input.Occurrences[i])
		if err != nil {
			return nil, err
		}
		occurrences = append(occurrences, occurrence)
	}
	sort.Slice(occurrences, func(i, j int) bool { return occurrences[i].StartsAt.Before(occurrences[j].StartsAt) })
	return &Event{
		CategoryID:           input.CategoryID,
		Title:                title,
		Summary:              strings.TrimSpace(input.Summary),
		DescriptionMarkdown:  strings.TrimSpace(input.DescriptionMarkdown),
		Tags:                 normalizeEventTags(input.Tags),
		OrganizerName:        strings.TrimSpace(input.OrganizerName),
		OrganizerURL:         organizerURL,
		FeeType:              feeType,
		PriceMin:             input.PriceMin,
		PriceMax:             input.PriceMax,
		Currency:             currency,
		RegistrationURL:      registrationURL,
		RegistrationDeadline: input.RegistrationDeadline,
		CoverURL:             coverURL,
		Status:               status,
		Visibility:           visibility,
		Audience:             audience,
		VisibleFrom:          input.VisibleFrom,
		VisibleUntil:         input.VisibleUntil,
		CancelledReason:      cancelledReason,
		Occurrences:          occurrences,
	}, nil
}

func normalizeOccurrenceInput(input EventOccurrenceInput) (EventOccurrence, error) {
	if input.StartsAt.IsZero() || (input.EndsAt != nil && !input.StartsAt.Before(*input.EndsAt)) {
		return EventOccurrence{}, ErrEventInvalid
	}
	timezone := strings.TrimSpace(input.Timezone)
	if timezone == "" {
		timezone = "Asia/Shanghai"
	}
	if _, err := time.LoadLocation(timezone); err != nil {
		return EventOccurrence{}, ErrEventInvalid
	}
	locationMode := strings.TrimSpace(input.LocationMode)
	if locationMode == "" {
		locationMode = domain.EventLocationOffline
	}
	if locationMode != domain.EventLocationOffline && locationMode != domain.EventLocationOnline && locationMode != domain.EventLocationHybrid {
		return EventOccurrence{}, ErrEventInvalid
	}
	onlineURL, err := normalizeOptionalEventURL(input.OnlineURL)
	if err != nil {
		return EventOccurrence{}, ErrEventInvalid
	}
	if (input.Latitude == nil) != (input.Longitude == nil) {
		return EventOccurrence{}, ErrEventInvalid
	}
	coordinateSource := strings.ToLower(strings.TrimSpace(input.CoordinateSource))
	if coordinateSource == "" {
		coordinateSource = domain.EventCoordinateWGS84
	}
	if coordinateSource != domain.EventCoordinateWGS84 && coordinateSource != domain.EventCoordinateGCJ02 {
		return EventOccurrence{}, ErrEventInvalid
	}
	latitude, longitude := input.Latitude, input.Longitude
	if latitude != nil {
		if *latitude < -90 || *latitude > 90 || *longitude < -180 || *longitude > 180 {
			return EventOccurrence{}, ErrEventInvalid
		}
		if coordinateSource == domain.EventCoordinateGCJ02 {
			lat, lng := domain.GCJ02ToWGS84(*latitude, *longitude)
			latitude, longitude = &lat, &lng
		}
	}
	return EventOccurrence{
		StartsAt:         input.StartsAt,
		EndsAt:           input.EndsAt,
		Timezone:         timezone,
		AllDay:           input.AllDay,
		LocationMode:     locationMode,
		OnlineURL:        onlineURL,
		VenueName:        strings.TrimSpace(input.VenueName),
		Address:          strings.TrimSpace(input.Address),
		Country:          strings.TrimSpace(input.Country),
		Province:         strings.TrimSpace(input.Province),
		City:             strings.TrimSpace(input.City),
		District:         strings.TrimSpace(input.District),
		Latitude:         latitude,
		Longitude:        longitude,
		CoordinateSource: domain.EventCoordinateWGS84,
	}, nil
}

func normalizeOptionalEventURL(raw string) (string, error) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return "", nil
	}
	return urlvalidator.ValidateURLFormat(raw, true)
}

func normalizeEventAudience(audience EventAudience) (EventAudience, error) {
	seen := make(map[int64]struct{}, len(audience.SubscriptionGroupIDs))
	out := EventAudience{SubscriptionGroupIDs: make([]int64, 0, len(audience.SubscriptionGroupIDs))}
	for _, id := range audience.SubscriptionGroupIDs {
		if id <= 0 {
			return EventAudience{}, ErrEventInvalid
		}
		if _, ok := seen[id]; ok {
			continue
		}
		seen[id] = struct{}{}
		out.SubscriptionGroupIDs = append(out.SubscriptionGroupIDs, id)
	}
	sort.Slice(out.SubscriptionGroupIDs, func(i, j int) bool { return out.SubscriptionGroupIDs[i] < out.SubscriptionGroupIDs[j] })
	return out, nil
}

func normalizeEventTags(tags []string) []string {
	seen := make(map[string]struct{}, len(tags))
	out := make([]string, 0, len(tags))
	for _, tag := range tags {
		tag = strings.TrimSpace(tag)
		if tag == "" || len([]rune(tag)) > 40 {
			continue
		}
		key := strings.ToLower(tag)
		if _, ok := seen[key]; ok {
			continue
		}
		seen[key] = struct{}{}
		out = append(out, tag)
		if len(out) == maxEventTags {
			break
		}
	}
	return out
}

func normalizeEventFilters(filters EventListFilters) EventListFilters {
	filters.Status = strings.TrimSpace(filters.Status)
	filters.CategoryCode = strings.TrimSpace(filters.CategoryCode)
	filters.Search = strings.TrimSpace(filters.Search)
	filters.City = strings.TrimSpace(filters.City)
	filters.District = strings.TrimSpace(filters.District)
	filters.FeeType = strings.TrimSpace(filters.FeeType)
	return filters
}

func validEventStatus(status string) bool {
	switch status {
	case EventStatusDraft, EventStatusPublished, EventStatusCancelled, EventStatusArchived:
		return true
	default:
		return false
	}
}

func validPriceRange(minPrice, maxPrice *float64) bool {
	if minPrice != nil && *minPrice < 0 || maxPrice != nil && *maxPrice < 0 {
		return false
	}
	return minPrice == nil || maxPrice == nil || *maxPrice >= *minPrice
}

func allEventImportManagedFields() []string {
	return []string{"category", "title", "summary", "description_markdown", "tags", "organizer", "fee", "registration", "cover_url", "occurrences"}
}

func normalizeEventCategory(input EventCategory) (*EventCategory, error) {
	code := strings.ToLower(strings.TrimSpace(input.Code))
	name := strings.TrimSpace(input.Name)
	if !eventCodePattern.MatchString(code) || name == "" || len([]rune(name)) > 100 {
		return nil, ErrEventInvalid
	}
	color := strings.TrimSpace(input.Color)
	if color == "" {
		color = "#2563EB"
	}
	icon := strings.TrimSpace(input.Icon)
	if icon == "" {
		icon = "calendar"
	}
	if len(color) > 20 || len(icon) > 64 {
		return nil, ErrEventInvalid
	}
	return &EventCategory{Code: code, Name: name, Color: color, Icon: icon, SortOrder: input.SortOrder, Enabled: input.Enabled}, nil
}

func normalizeEventSource(input EventSource) (*EventSource, error) {
	code := strings.ToLower(strings.TrimSpace(input.Code))
	name := strings.TrimSpace(input.Name)
	kind := strings.ToLower(strings.TrimSpace(input.Kind))
	if !eventCodePattern.MatchString(code) || name == "" || len([]rune(name)) > 100 {
		return nil, ErrEventInvalid
	}
	if kind != domain.EventSourceManual && kind != domain.EventSourceJSON && kind != domain.EventSourceCrawler {
		return nil, ErrEventInvalid
	}
	config := input.Config
	if config == nil {
		config = map[string]any{}
	}
	encoded, err := json.Marshal(config)
	if err != nil || len(encoded) > maxEventSourceConfigBytes {
		return nil, ErrEventInvalid
	}
	return &EventSource{Code: code, Name: name, Kind: kind, Enabled: input.Enabled, Config: config, LastSyncAt: input.LastSyncAt}, nil
}
