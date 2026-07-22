package service

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/domain"
)

const (
	EventImportSchemaVersion = 1
	maxEventImportItems      = 1000
)

type EventImportCandidate struct {
	ExternalID      string
	CategoryCode    string
	SourceURL       string
	SourceUpdatedAt *time.Time
	RawPayload      map[string]any
	Event           CreateEventInput
}

type PreviewEventImportInput struct {
	SourceCode    string
	FileName      string
	FileHash      string
	SchemaVersion int
	Mode          string
	ActorID       int64
	Items         []EventImportCandidate
}

type CommitEventImportInput struct {
	BatchID int64
	Publish bool
	ActorID int64
}

type storedEventImport struct {
	ExternalID      string                   `json:"external_id,omitempty"`
	SourceURL       string                   `json:"source_url,omitempty"`
	SourceUpdatedAt *time.Time               `json:"source_updated_at,omitempty"`
	RawPayload      map[string]any           `json:"raw_payload,omitempty"`
	Event           storedEventImportContent `json:"event"`
}

type storedEventImportContent struct {
	CategoryID           *int64                  `json:"category_id,omitempty"`
	Title                string                  `json:"title"`
	Summary              string                  `json:"summary,omitempty"`
	DescriptionMarkdown  string                  `json:"description_markdown,omitempty"`
	Tags                 []string                `json:"tags,omitempty"`
	OrganizerName        string                  `json:"organizer_name,omitempty"`
	OrganizerURL         string                  `json:"organizer_url,omitempty"`
	FeeType              string                  `json:"fee_type"`
	PriceMin             *float64                `json:"price_min,omitempty"`
	PriceMax             *float64                `json:"price_max,omitempty"`
	Currency             string                  `json:"currency"`
	RegistrationURL      string                  `json:"registration_url,omitempty"`
	RegistrationDeadline *time.Time              `json:"registration_deadline,omitempty"`
	CoverURL             string                  `json:"cover_url,omitempty"`
	Visibility           string                  `json:"visibility"`
	Audience             EventAudience           `json:"audience,omitempty"`
	VisibleFrom          *time.Time              `json:"visible_from,omitempty"`
	VisibleUntil         *time.Time              `json:"visible_until,omitempty"`
	Occurrences          []storedEventOccurrence `json:"occurrences"`
}

type storedEventOccurrence struct {
	StartsAt         time.Time  `json:"starts_at"`
	EndsAt           *time.Time `json:"ends_at,omitempty"`
	Timezone         string     `json:"timezone"`
	AllDay           bool       `json:"all_day,omitempty"`
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
}

func (s *EventService) PreviewImport(ctx context.Context, input *PreviewEventImportInput) (*EventImportBatch, error) {
	if input == nil || input.SchemaVersion != EventImportSchemaVersion || len(input.Items) == 0 || len(input.Items) > maxEventImportItems || input.ActorID <= 0 {
		return nil, ErrEventInvalid
	}
	mode := strings.TrimSpace(input.Mode)
	if mode == "" {
		mode = "upsert"
	}
	if mode != "create_only" && mode != "upsert" {
		return nil, ErrEventInvalid
	}
	source, err := s.repo.GetSourceByCode(ctx, strings.TrimSpace(input.SourceCode))
	if err != nil {
		return nil, err
	}
	if !source.Enabled || source.Kind == domain.EventSourceManual {
		return nil, ErrEventInvalid
	}
	fileHash := strings.TrimSpace(input.FileHash)
	if fileHash == "" {
		encoded, _ := json.Marshal(input.Items)
		fileHash = sha256Hex(encoded)
	}
	batch := &EventImportBatch{
		SourceID:      source.ID,
		FileName:      strings.TrimSpace(input.FileName),
		FileHash:      fileHash,
		SchemaVersion: input.SchemaVersion,
		Mode:          mode,
		Status:        domain.EventImportStatusPreviewed,
		TotalCount:    len(input.Items),
		CreatedBy:     input.ActorID,
		Items:         make([]EventImportItem, 0, len(input.Items)),
	}
	categories, err := s.repo.ListCategories(ctx, true)
	if err != nil {
		return nil, fmt.Errorf("list event categories: %w", err)
	}
	categoryIDs := make(map[string]int64, len(categories))
	for i := range categories {
		categoryIDs[categories[i].Code] = categories[i].ID
	}
	seenExternalIDs := make(map[string]int, len(input.Items))
	seenFingerprints := make(map[string]int, len(input.Items))
	for index := range input.Items {
		candidate := input.Items[index]
		if candidate.Event.CategoryID == nil && strings.TrimSpace(candidate.CategoryCode) != "" {
			categoryID, ok := categoryIDs[strings.ToLower(strings.TrimSpace(candidate.CategoryCode))]
			if !ok {
				batch.Items = append(batch.Items, EventImportItem{
					ItemIndex: index, ExternalID: strings.TrimSpace(candidate.ExternalID), Action: domain.EventImportActionError,
					Status: "invalid", ErrorCode: "EVENT_CATEGORY_NOT_FOUND", ErrorDetail: "活动分类不存在",
				})
				batch.ErrorCount++
				continue
			}
			candidate.Event.CategoryID = &categoryID
		}
		item := s.previewImportItem(ctx, source.ID, index, candidate, mode)
		if firstIndex, duplicate := seenExternalIDs[item.ExternalID]; item.ExternalID != "" && duplicate {
			markImportItemDuplicate(&item, "EVENT_DUPLICATE_EXTERNAL_ID", fmt.Sprintf("与导入文件第 %d 条的 external_id 重复", firstIndex+1))
		} else if item.ExternalID != "" {
			seenExternalIDs[item.ExternalID] = index
		}
		if item.Action != domain.EventImportActionError && item.Action != domain.EventImportActionConflict {
			if firstIndex, duplicate := seenFingerprints[item.Fingerprint]; item.Fingerprint != "" && duplicate {
				markImportItemDuplicate(&item, "EVENT_DUPLICATE_FINGERPRINT", fmt.Sprintf("与导入文件第 %d 条的标题、首场时间和地点重复", firstIndex+1))
			} else if item.Fingerprint != "" {
				seenFingerprints[item.Fingerprint] = index
			}
		}
		batch.Items = append(batch.Items, item)
		switch item.Action {
		case domain.EventImportActionCreate:
			batch.CreateCount++
		case domain.EventImportActionUpdate:
			batch.UpdateCount++
		case domain.EventImportActionUnchanged:
			batch.UnchangedCount++
		case domain.EventImportActionConflict:
			batch.ConflictCount++
		case domain.EventImportActionError:
			batch.ErrorCount++
		}
	}
	if err := s.repo.CreateImportBatch(ctx, batch); err != nil {
		return nil, fmt.Errorf("create event import preview: %w", err)
	}
	return batch, nil
}

func (s *EventService) previewImportItem(ctx context.Context, sourceID int64, index int, candidate EventImportCandidate, mode string) EventImportItem {
	item := EventImportItem{ItemIndex: index, ExternalID: strings.TrimSpace(candidate.ExternalID), Status: "pending"}
	candidate.Event.Status = EventStatusDraft
	candidate.Event.ActorID = 0
	normalized, err := normalizeEventInput(&candidate.Event)
	if err != nil {
		item.Action = domain.EventImportActionError
		item.Status = "invalid"
		item.ErrorCode = "EVENT_INVALID"
		item.ErrorDetail = "活动字段、时间、链接或坐标不符合要求"
		return item
	}
	stored := storedEventFromDomain(candidate, normalized)
	encoded, err := json.Marshal(stored)
	if err != nil {
		item.Action = domain.EventImportActionError
		item.Status = "invalid"
		item.ErrorCode = "EVENT_ENCODE_FAILED"
		item.ErrorDetail = "活动标准化失败"
		return item
	}
	if err := json.Unmarshal(encoded, &item.NormalizedPayload); err != nil {
		item.Action = domain.EventImportActionError
		item.Status = "invalid"
		item.ErrorCode = "EVENT_ENCODE_FAILED"
		item.ErrorDetail = "活动标准化失败"
		return item
	}
	item.ContentHash = sha256Hex(encoded)
	item.Fingerprint = eventFingerprint(normalized)
	if item.ExternalID != "" {
		record, lookupErr := s.repo.GetSourceRecordByExternalID(ctx, sourceID, item.ExternalID)
		if lookupErr != nil {
			item.Action = domain.EventImportActionError
			item.Status = "invalid"
			item.ErrorCode = "EVENT_LOOKUP_FAILED"
			item.ErrorDetail = "检查已有来源记录失败"
			return item
		}
		if record != nil {
			if record.ContentHash == item.ContentHash {
				item.Action = domain.EventImportActionUnchanged
				item.Status = "skipped"
				item.EventID = record.EventID
				return item
			}
			if mode == "create_only" {
				item.Action = domain.EventImportActionConflict
				item.Status = "conflict"
				item.EventID = record.EventID
				item.ErrorCode = "EVENT_ALREADY_EXISTS"
				item.ErrorDetail = "来源 external_id 已存在，create_only 模式不会更新"
				return item
			}
			item.Action = domain.EventImportActionUpdate
			item.EventID = record.EventID
			return item
		}
	}
	record, lookupErr := s.repo.GetSourceRecordBySourceAndFingerprint(ctx, sourceID, item.Fingerprint)
	if lookupErr != nil {
		item.Action = domain.EventImportActionError
		item.Status = "invalid"
		item.ErrorCode = "EVENT_LOOKUP_FAILED"
		item.ErrorDetail = "检查重复活动失败"
		return item
	}
	if record != nil {
		item.EventID = record.EventID
		if item.ExternalID != "" && record.ExternalID != "" && item.ExternalID != record.ExternalID {
			item.Action = domain.EventImportActionConflict
			item.Status = "conflict"
			item.ErrorCode = "EVENT_FINGERPRINT_CONFLICT"
			item.ErrorDetail = "同一来源中存在不同 external_id 的相同活动，需要人工确认"
			return item
		}
		if record.ContentHash == item.ContentHash {
			item.Action = domain.EventImportActionUnchanged
			item.Status = "skipped"
			return item
		}
		if mode == "create_only" {
			item.Action = domain.EventImportActionConflict
			item.Status = "conflict"
			item.ErrorCode = "EVENT_ALREADY_EXISTS"
			item.ErrorDetail = "来源中已存在相同活动，create_only 模式不会更新"
			return item
		}
		item.Action = domain.EventImportActionUpdate
		return item
	}
	record, lookupErr = s.repo.GetSourceRecordByFingerprint(ctx, item.Fingerprint)
	if lookupErr != nil {
		item.Action = domain.EventImportActionError
		item.Status = "invalid"
		item.ErrorCode = "EVENT_LOOKUP_FAILED"
		item.ErrorDetail = "检查重复活动失败"
		return item
	}
	if record != nil {
		item.Action = domain.EventImportActionConflict
		item.Status = "conflict"
		item.EventID = record.EventID
		item.ErrorCode = "EVENT_FINGERPRINT_CONFLICT"
		item.ErrorDetail = "标题、首场时间和地点与已有活动相同，需要人工确认"
		return item
	}
	item.Action = domain.EventImportActionCreate
	return item
}

func markImportItemDuplicate(item *EventImportItem, code, detail string) {
	item.Action = domain.EventImportActionConflict
	item.Status = "conflict"
	item.EventID = nil
	item.ErrorCode = code
	item.ErrorDetail = detail
}

func (s *EventService) GetImportBatch(ctx context.Context, id int64) (*EventImportBatch, error) {
	return s.repo.GetImportBatch(ctx, id)
}

func (s *EventService) CommitImport(ctx context.Context, input *CommitEventImportInput) (*EventImportBatch, error) {
	if input == nil || input.BatchID <= 0 || input.ActorID <= 0 {
		return nil, ErrEventInvalid
	}
	claimed, err := s.repo.ClaimImportBatch(ctx, input.BatchID)
	if err != nil {
		return nil, fmt.Errorf("claim event import: %w", err)
	}
	if !claimed {
		return nil, ErrEventImportState
	}
	batch, err := s.repo.GetImportBatch(ctx, input.BatchID)
	if err != nil {
		return nil, err
	}
	failed := 0
	for i := range batch.Items {
		item := &batch.Items[i]
		if item.Action != domain.EventImportActionCreate && item.Action != domain.EventImportActionUpdate {
			continue
		}
		if err := s.commitImportItem(ctx, batch.SourceID, item, input); err != nil {
			failed++
			item.Status = "failed"
			item.ErrorCode = "EVENT_COMMIT_FAILED"
			item.ErrorDetail = "写入活动失败"
		} else {
			item.Status = "committed"
		}
		if err := s.repo.UpdateImportItem(ctx, item); err != nil {
			failed++
		}
	}
	now := time.Now()
	batch.CommittedAt = &now
	if failed > 0 {
		batch.Status = domain.EventImportStatusPartial
		batch.ErrorCount += failed
	} else {
		batch.Status = domain.EventImportStatusCompleted
	}
	if err := s.repo.UpdateImportBatch(ctx, batch); err != nil {
		return nil, fmt.Errorf("finish event import: %w", err)
	}
	return s.repo.GetImportBatch(ctx, batch.ID)
}

func (s *EventService) commitImportItem(ctx context.Context, sourceID int64, item *EventImportItem, input *CommitEventImportInput) error {
	encoded, err := json.Marshal(item.NormalizedPayload)
	if err != nil {
		return err
	}
	var stored storedEventImport
	if err := json.Unmarshal(encoded, &stored); err != nil {
		return err
	}
	eventInput := stored.toCreateEventInput()
	eventInput.ActorID = input.ActorID
	if input.Publish {
		eventInput.Status = EventStatusPublished
	}
	normalized, err := normalizeEventInput(&eventInput)
	if err != nil {
		return err
	}
	if input.Publish {
		now := time.Now()
		normalized.PublishedAt = &now
	}
	normalized.CreatedBy = &input.ActorID
	normalized.UpdatedBy = &input.ActorID
	if item.EventID != nil {
		existing, err := s.repo.GetByID(ctx, *item.EventID)
		if err != nil {
			return err
		}
		if len(existing.ManualOverrideFields) > 0 {
			normalized = existing
		} else {
			normalized.ID = existing.ID
			normalized.CreatedAt = existing.CreatedAt
			normalized.CreatedBy = existing.CreatedBy
			normalized.ManualOverrideFields = existing.ManualOverrideFields
		}
	}
	record := &EventSourceRecord{
		SourceID:          sourceID,
		EventID:           item.EventID,
		ExternalID:        item.ExternalID,
		SourceURL:         stored.SourceURL,
		Fingerprint:       item.Fingerprint,
		ContentHash:       item.ContentHash,
		State:             "active",
		RawPayload:        stored.RawPayload,
		NormalizedPayload: item.NormalizedPayload,
		SourceUpdatedAt:   stored.SourceUpdatedAt,
		LastSeenAt:        time.Now(),
	}
	if err := s.repo.SaveImportedEvent(ctx, normalized, record); err != nil {
		return err
	}
	item.EventID = &normalized.ID
	return nil
}

func storedEventFromDomain(candidate EventImportCandidate, event *Event) storedEventImport {
	occurrences := make([]storedEventOccurrence, 0, len(event.Occurrences))
	for i := range event.Occurrences {
		value := event.Occurrences[i]
		occurrences = append(occurrences, storedEventOccurrence{
			StartsAt: value.StartsAt, EndsAt: value.EndsAt, Timezone: value.Timezone, AllDay: value.AllDay,
			LocationMode: value.LocationMode, OnlineURL: value.OnlineURL, VenueName: value.VenueName,
			Address: value.Address, Country: value.Country, Province: value.Province, City: value.City,
			District: value.District, Latitude: value.Latitude, Longitude: value.Longitude,
			CoordinateSource: domain.EventCoordinateWGS84,
		})
	}
	return storedEventImport{
		ExternalID: strings.TrimSpace(candidate.ExternalID), SourceURL: strings.TrimSpace(candidate.SourceURL),
		SourceUpdatedAt: candidate.SourceUpdatedAt, RawPayload: candidate.RawPayload,
		Event: storedEventImportContent{
			CategoryID: event.CategoryID, Title: event.Title, Summary: event.Summary,
			DescriptionMarkdown: event.DescriptionMarkdown, Tags: event.Tags, OrganizerName: event.OrganizerName,
			OrganizerURL: event.OrganizerURL, FeeType: event.FeeType, PriceMin: event.PriceMin,
			PriceMax: event.PriceMax, Currency: event.Currency, RegistrationURL: event.RegistrationURL,
			RegistrationDeadline: event.RegistrationDeadline, CoverURL: event.CoverURL,
			Visibility: event.Visibility, Audience: event.Audience, VisibleFrom: event.VisibleFrom,
			VisibleUntil: event.VisibleUntil, Occurrences: occurrences,
		},
	}
}

func (stored storedEventImport) toCreateEventInput() CreateEventInput {
	occurrences := make([]EventOccurrenceInput, 0, len(stored.Event.Occurrences))
	for i := range stored.Event.Occurrences {
		value := stored.Event.Occurrences[i]
		occurrences = append(occurrences, EventOccurrenceInput{
			StartsAt: value.StartsAt, EndsAt: value.EndsAt, Timezone: value.Timezone, AllDay: value.AllDay,
			LocationMode: value.LocationMode, OnlineURL: value.OnlineURL, VenueName: value.VenueName,
			Address: value.Address, Country: value.Country, Province: value.Province, City: value.City,
			District: value.District, Latitude: value.Latitude, Longitude: value.Longitude,
			CoordinateSource: domain.EventCoordinateWGS84,
		})
	}
	return CreateEventInput{
		CategoryID: stored.Event.CategoryID, Title: stored.Event.Title, Summary: stored.Event.Summary,
		DescriptionMarkdown: stored.Event.DescriptionMarkdown, Tags: stored.Event.Tags,
		OrganizerName: stored.Event.OrganizerName, OrganizerURL: stored.Event.OrganizerURL,
		FeeType: stored.Event.FeeType, PriceMin: stored.Event.PriceMin, PriceMax: stored.Event.PriceMax,
		Currency: stored.Event.Currency, RegistrationURL: stored.Event.RegistrationURL,
		RegistrationDeadline: stored.Event.RegistrationDeadline, CoverURL: stored.Event.CoverURL,
		Status: EventStatusDraft, Visibility: stored.Event.Visibility, Audience: stored.Event.Audience,
		VisibleFrom: stored.Event.VisibleFrom, VisibleUntil: stored.Event.VisibleUntil, Occurrences: occurrences,
	}
}

func eventFingerprint(event *Event) string {
	first := event.Occurrences[0]
	value := strings.Join([]string{
		strings.ToLower(strings.Join(strings.Fields(event.Title), " ")),
		first.StartsAt.UTC().Format(time.RFC3339),
		strings.ToLower(strings.Join(strings.Fields(first.City), " ")),
		strings.ToLower(strings.Join(strings.Fields(first.Address), " ")),
	}, "|")
	return sha256Hex([]byte(value))
}

func sha256Hex(value []byte) string {
	sum := sha256.Sum256(value)
	return hex.EncodeToString(sum[:])
}
