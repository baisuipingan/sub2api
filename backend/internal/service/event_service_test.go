package service

import (
	"context"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/domain"
	"github.com/Wei-Shaw/sub2api/internal/pkg/pagination"
)

func validEventInput() *CreateEventInput {
	startsAt := time.Date(2026, time.July, 20, 10, 0, 0, 0, time.UTC)
	endsAt := startsAt.Add(2 * time.Hour)
	latitude := 31.228457
	longitude := 121.478223
	return &CreateEventInput{
		Title:      " Shanghai AI Meetup ",
		Status:     EventStatusDraft,
		Visibility: EventVisibilityAuthenticated,
		FeeType:    domain.EventFeeFree,
		Occurrences: []EventOccurrenceInput{{
			StartsAt: startsAt, EndsAt: &endsAt, Timezone: "Asia/Shanghai",
			LocationMode: domain.EventLocationOffline, Latitude: &latitude, Longitude: &longitude,
			CoordinateSource: domain.EventCoordinateGCJ02, VenueName: "People's Square",
		}},
	}
}

func TestNormalizeEventInput(t *testing.T) {
	t.Run("normalizes GCJ-02 coordinates and defaults", func(t *testing.T) {
		event, err := normalizeEventInput(validEventInput())
		if err != nil {
			t.Fatalf("normalize valid event: %v", err)
		}
		occurrence := event.Occurrences[0]
		if event.Title != "Shanghai AI Meetup" || event.Currency != "CNY" {
			t.Fatalf("unexpected normalized event: %#v", event)
		}
		if occurrence.CoordinateSource != domain.EventCoordinateWGS84 {
			t.Fatalf("coordinate source not normalized: %q", occurrence.CoordinateSource)
		}
		if occurrence.Latitude == nil || occurrence.Longitude == nil ||
			*occurrence.Latitude < 31.2302 || *occurrence.Latitude > 31.2306 ||
			*occurrence.Longitude < 121.4735 || *occurrence.Longitude > 121.4739 {
			t.Fatalf("unexpected converted coordinate: (%v, %v)", occurrence.Latitude, occurrence.Longitude)
		}
	})

	tests := []struct {
		name   string
		mutate func(*CreateEventInput)
	}{
		{name: "empty title", mutate: func(input *CreateEventInput) { input.Title = " " }},
		{name: "invalid timezone", mutate: func(input *CreateEventInput) { input.Occurrences[0].Timezone = "Mars/Olympus" }},
		{name: "inverted time range", mutate: func(input *CreateEventInput) {
			endsAt := input.Occurrences[0].StartsAt.Add(-time.Minute)
			input.Occurrences[0].EndsAt = &endsAt
		}},
		{name: "coordinate pair", mutate: func(input *CreateEventInput) { input.Occurrences[0].Longitude = nil }},
		{name: "invalid URL", mutate: func(input *CreateEventInput) { input.RegistrationURL = "javascript:alert(1)" }},
		{name: "targeted without audience", mutate: func(input *CreateEventInput) { input.Visibility = EventVisibilityTargeted }},
		{name: "cancelled without reason", mutate: func(input *CreateEventInput) { input.Status = EventStatusCancelled }},
		{name: "invalid visible range", mutate: func(input *CreateEventInput) {
			from := time.Now().Add(time.Hour)
			until := from.Add(-time.Minute)
			input.VisibleFrom, input.VisibleUntil = &from, &until
		}},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			input := validEventInput()
			test.mutate(input)
			if _, err := normalizeEventInput(input); err == nil {
				t.Fatal("expected validation error")
			}
		})
	}
}

func TestEventVisibleToUser(t *testing.T) {
	now := time.Now()
	earlier := now.Add(-time.Hour)
	later := now.Add(time.Hour)
	groups := map[int64]struct{}{7: {}}

	tests := []struct {
		name    string
		event   *Event
		visible bool
	}{
		{name: "published authenticated", event: &Event{Status: EventStatusPublished, Visibility: EventVisibilityAuthenticated}, visible: true},
		{name: "cancelled remains visible", event: &Event{Status: EventStatusCancelled, Visibility: EventVisibilityAuthenticated}, visible: true},
		{name: "draft", event: &Event{Status: EventStatusDraft, Visibility: EventVisibilityAuthenticated}},
		{name: "before window", event: &Event{Status: EventStatusPublished, Visibility: EventVisibilityAuthenticated, VisibleFrom: &later}},
		{name: "after window", event: &Event{Status: EventStatusPublished, Visibility: EventVisibilityAuthenticated, VisibleUntil: &earlier}},
		{name: "matching audience", event: &Event{Status: EventStatusPublished, Visibility: EventVisibilityTargeted, Audience: EventAudience{SubscriptionGroupIDs: []int64{7}}}, visible: true},
		{name: "other audience", event: &Event{Status: EventStatusPublished, Visibility: EventVisibilityTargeted, Audience: EventAudience{SubscriptionGroupIDs: []int64{8}}}},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if got := eventVisibleToUser(test.event, now, groups); got != test.visible {
				t.Fatalf("visible = %v, want %v", got, test.visible)
			}
		})
	}
}

type eventAudienceRepoStub struct {
	EventRepository
	listAudience []int64
	mapAudience  []int64
}

func (r *eventAudienceRepoStub) ListPublishedForUser(_ context.Context, params pagination.PaginationParams, _ EventListFilters, audienceGroupIDs []int64) ([]Event, *pagination.PaginationResult, error) {
	r.listAudience = append([]int64(nil), audienceGroupIDs...)
	return nil, &pagination.PaginationResult{Page: params.Page, PageSize: params.PageSize}, nil
}

func (r *eventAudienceRepoStub) ListPublishedMapForUser(_ context.Context, _ EventMapFilters, audienceGroupIDs []int64) ([]Event, error) {
	r.mapAudience = append([]int64(nil), audienceGroupIDs...)
	return nil, nil
}

type eventUserSubscriptionRepoStub struct {
	UserSubscriptionRepository
	subscriptions []UserSubscription
}

func (r *eventUserSubscriptionRepoStub) ListActiveByUserID(context.Context, int64) ([]UserSubscription, error) {
	return r.subscriptions, nil
}

func TestUserEventQueriesPushAudienceGroupsIntoRepository(t *testing.T) {
	repo := &eventAudienceRepoStub{}
	service := &EventService{
		repo: repo,
		userSubRepo: &eventUserSubscriptionRepoStub{subscriptions: []UserSubscription{
			{GroupID: 7}, {GroupID: 3}, {GroupID: 7},
		}},
	}
	if _, _, err := service.ListForUser(context.Background(), 9, pagination.PaginationParams{Page: 1, PageSize: 20}, EventListFilters{}); err != nil {
		t.Fatalf("list user events: %v", err)
	}
	if _, _, err := service.MapForUser(context.Background(), 9, EventMapFilters{Limit: 100}); err != nil {
		t.Fatalf("map user events: %v", err)
	}
	want := []int64{3, 7}
	if !reflect.DeepEqual(repo.listAudience, want) || !reflect.DeepEqual(repo.mapAudience, want) {
		t.Fatalf("audience groups list=%v map=%v, want %v", repo.listAudience, repo.mapAudience, want)
	}
}

func TestLimitEventMapOccurrencesCapsMarkers(t *testing.T) {
	events := []Event{
		{ID: 1, Occurrences: []EventOccurrence{{ID: 1}, {ID: 2}}},
		{ID: 2, Occurrences: []EventOccurrence{{ID: 3}, {ID: 4}}},
	}
	limited, truncated := limitEventMapOccurrences(events, 3)
	if !truncated || len(limited) != 2 || len(limited[0].Occurrences) != 2 || len(limited[1].Occurrences) != 1 {
		t.Fatalf("unexpected map limit result: events=%#v truncated=%v", limited, truncated)
	}
	if events[1].Occurrences[1].ID != 4 {
		t.Fatal("map limiting mutated repository results")
	}
}

type eventImportRepoStub struct {
	EventRepository
	externalRecord          *EventSourceRecord
	sourceFingerprintRecord *EventSourceRecord
	fingerprintRecord       *EventSourceRecord
	existingEvent           *Event
	savedEvent              *Event
	source                  *EventSource
	createdBatch            *EventImportBatch
}

func (r *eventImportRepoStub) GetSourceRecordByExternalID(context.Context, int64, string) (*EventSourceRecord, error) {
	return r.externalRecord, nil
}

func (r *eventImportRepoStub) GetSourceRecordByFingerprint(context.Context, string) (*EventSourceRecord, error) {
	return r.fingerprintRecord, nil
}

func (r *eventImportRepoStub) GetSourceRecordBySourceAndFingerprint(context.Context, int64, string) (*EventSourceRecord, error) {
	return r.sourceFingerprintRecord, nil
}

func (r *eventImportRepoStub) GetSourceByCode(context.Context, string) (*EventSource, error) {
	if r.source != nil {
		return r.source, nil
	}
	return &EventSource{ID: 1, Code: "json", Kind: domain.EventSourceJSON, Enabled: true}, nil
}

func (r *eventImportRepoStub) ListCategories(context.Context, bool) ([]EventCategory, error) {
	return nil, nil
}

func (r *eventImportRepoStub) CreateImportBatch(_ context.Context, batch *EventImportBatch) error {
	batch.ID = 1
	for i := range batch.Items {
		batch.Items[i].ID = int64(i + 1)
	}
	r.createdBatch = batch
	return nil
}

func (r *eventImportRepoStub) GetByID(context.Context, int64) (*Event, error) {
	return r.existingEvent, nil
}

func (r *eventImportRepoStub) SaveImportedEvent(_ context.Context, event *Event, record *EventSourceRecord) error {
	copy := *event
	if copy.ID == 0 {
		copy.ID = 99
		event.ID = copy.ID
	}
	r.savedEvent = &copy
	record.EventID = &copy.ID
	return nil
}

func TestPreviewImportItemActions(t *testing.T) {
	ctx := context.Background()
	candidate := EventImportCandidate{ExternalID: "evt-1", Event: *validEventInput()}
	repo := &eventImportRepoStub{}
	service := &EventService{repo: repo}

	created := service.previewImportItem(ctx, 1, 0, candidate, "upsert")
	if created.Action != domain.EventImportActionCreate {
		t.Fatalf("action = %q, want create", created.Action)
	}

	eventID := int64(12)
	repo.externalRecord = &EventSourceRecord{EventID: &eventID, ContentHash: created.ContentHash}
	unchanged := service.previewImportItem(ctx, 1, 0, candidate, "upsert")
	if unchanged.Action != domain.EventImportActionUnchanged {
		t.Fatalf("action = %q, want unchanged", unchanged.Action)
	}

	changedCandidate := candidate
	changedCandidate.Event.Summary = "updated"
	repo.externalRecord.ContentHash = "different"
	updated := service.previewImportItem(ctx, 1, 0, changedCandidate, "upsert")
	if updated.Action != domain.EventImportActionUpdate {
		t.Fatalf("action = %q, want update", updated.Action)
	}
	conflicted := service.previewImportItem(ctx, 1, 0, changedCandidate, "create_only")
	if conflicted.Action != domain.EventImportActionConflict {
		t.Fatalf("action = %q, want conflict", conflicted.Action)
	}

	repo.externalRecord = nil
	repo.fingerprintRecord = &EventSourceRecord{SourceID: 2, EventID: &eventID}
	fingerprintConflict := service.previewImportItem(ctx, 1, 0, candidate, "upsert")
	if fingerprintConflict.Action != domain.EventImportActionConflict {
		t.Fatalf("action = %q, want fingerprint conflict", fingerprintConflict.Action)
	}

	repo.fingerprintRecord = nil
	withoutExternalID := candidate
	withoutExternalID.ExternalID = ""
	withoutExternalIDPreview := service.previewImportItem(ctx, 1, 0, withoutExternalID, "upsert")
	repo.sourceFingerprintRecord = &EventSourceRecord{SourceID: 1, EventID: &eventID, ContentHash: withoutExternalIDPreview.ContentHash}
	unchangedByFingerprint := service.previewImportItem(ctx, 1, 0, withoutExternalID, "upsert")
	if unchangedByFingerprint.Action != domain.EventImportActionUnchanged {
		t.Fatalf("action = %q, want unchanged for source fingerprint", unchangedByFingerprint.Action)
	}
	repo.sourceFingerprintRecord.ContentHash = "different"
	updatedByFingerprint := service.previewImportItem(ctx, 1, 0, withoutExternalID, "upsert")
	if updatedByFingerprint.Action != domain.EventImportActionUpdate {
		t.Fatalf("action = %q, want update for changed source fingerprint", updatedByFingerprint.Action)
	}
}

func TestPreviewImportRejectsDuplicatesWithinFile(t *testing.T) {
	repo := &eventImportRepoStub{}
	service := &EventService{repo: repo}
	input := &PreviewEventImportInput{
		SourceCode: "json", SchemaVersion: EventImportSchemaVersion, ActorID: 3,
		Items: []EventImportCandidate{
			{Event: *validEventInput()},
			{Event: *validEventInput()},
		},
	}
	batch, err := service.PreviewImport(context.Background(), input)
	if err != nil {
		t.Fatalf("preview import: %v", err)
	}
	if batch.CreateCount != 1 || batch.ConflictCount != 1 {
		t.Fatalf("unexpected counts: create=%d conflict=%d", batch.CreateCount, batch.ConflictCount)
	}
	if batch.Items[1].ErrorCode != "EVENT_DUPLICATE_FINGERPRINT" {
		t.Fatalf("error code = %q, want duplicate fingerprint", batch.Items[1].ErrorCode)
	}
}

func TestPreviewImportRejectsManualSource(t *testing.T) {
	repo := &eventImportRepoStub{source: &EventSource{ID: 1, Code: "manual", Kind: domain.EventSourceManual, Enabled: true}}
	service := &EventService{repo: repo}
	_, err := service.PreviewImport(context.Background(), &PreviewEventImportInput{
		SourceCode: "manual", SchemaVersion: EventImportSchemaVersion, ActorID: 3,
		Items: []EventImportCandidate{{Event: *validEventInput()}},
	})
	if err == nil {
		t.Fatal("expected manual source import to be rejected")
	}
}

func TestNormalizeEventSourceLimitsConfigSize(t *testing.T) {
	value := EventSource{Code: "crawler", Name: "Public events", Kind: domain.EventSourceCrawler, Enabled: true, Config: map[string]any{"selector": strings.Repeat("x", maxEventSourceConfigBytes)}}
	if _, err := normalizeEventSource(value); err == nil {
		t.Fatal("expected oversized source config to be rejected")
	}
}

func TestCommitImportPreservesManualOverride(t *testing.T) {
	ctx := context.Background()
	repo := &eventImportRepoStub{}
	service := &EventService{repo: repo}
	candidate := EventImportCandidate{ExternalID: "evt-1", Event: *validEventInput()}
	item := service.previewImportItem(ctx, 1, 0, candidate, "upsert")
	eventID := int64(12)
	item.EventID = &eventID
	item.Action = domain.EventImportActionUpdate
	repo.existingEvent = &Event{
		ID: eventID, Title: "Manually edited title", Status: EventStatusPublished,
		ManualOverrideFields: []string{"title"}, Occurrences: []EventOccurrence{{StartsAt: time.Now()}},
	}

	if err := service.commitImportItem(ctx, 1, &item, &CommitEventImportInput{ActorID: 3}); err != nil {
		t.Fatalf("commit import item: %v", err)
	}
	if repo.savedEvent == nil || repo.savedEvent.Title != "Manually edited title" {
		t.Fatalf("manual edit overwritten: %#v", repo.savedEvent)
	}
}
