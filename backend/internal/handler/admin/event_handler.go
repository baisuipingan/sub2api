package admin

import (
	"encoding/json"
	"net/http"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/handler/dto"
	infraerrors "github.com/Wei-Shaw/sub2api/internal/pkg/errors"
	"github.com/Wei-Shaw/sub2api/internal/pkg/pagination"
	"github.com/Wei-Shaw/sub2api/internal/pkg/response"
	middleware2 "github.com/Wei-Shaw/sub2api/internal/server/middleware"
	"github.com/Wei-Shaw/sub2api/internal/service"

	"github.com/gin-gonic/gin"
)

const maxEventImportBodyBytes int64 = 5 * 1024 * 1024

type EventHandler struct {
	service        *service.EventService
	settingService *service.SettingService
}

func NewEventHandler(eventService *service.EventService, settingService *service.SettingService) *EventHandler {
	return &EventHandler{service: eventService, settingService: settingService}
}

type eventOccurrenceRequest struct {
	StartsAt         time.Time  `json:"starts_at" binding:"required"`
	EndsAt           *time.Time `json:"ends_at"`
	Timezone         string     `json:"timezone" binding:"omitempty,max=64"`
	AllDay           bool       `json:"all_day"`
	LocationMode     string     `json:"location_mode" binding:"omitempty,oneof=offline online hybrid"`
	OnlineURL        string     `json:"online_url" binding:"omitempty,max=2048"`
	VenueName        string     `json:"venue_name" binding:"omitempty,max=300"`
	Address          string     `json:"address" binding:"omitempty,max=1000"`
	Country          string     `json:"country" binding:"omitempty,max=100"`
	Province         string     `json:"province" binding:"omitempty,max=100"`
	City             string     `json:"city" binding:"omitempty,max=100"`
	District         string     `json:"district" binding:"omitempty,max=100"`
	Latitude         *float64   `json:"latitude"`
	Longitude        *float64   `json:"longitude"`
	CoordinateSource string     `json:"coordinate_source" binding:"omitempty,oneof=wgs84 gcj02"`
}

type eventWriteRequest struct {
	CategoryID           *int64                   `json:"category_id"`
	Title                string                   `json:"title" binding:"required,max=200"`
	Summary              string                   `json:"summary" binding:"max=1000"`
	DescriptionMarkdown  string                   `json:"description_markdown" binding:"max=50000"`
	Tags                 []string                 `json:"tags" binding:"max=20"`
	OrganizerName        string                   `json:"organizer_name" binding:"max=200"`
	OrganizerURL         string                   `json:"organizer_url" binding:"max=2048"`
	FeeType              string                   `json:"fee_type" binding:"omitempty,oneof=free paid unknown"`
	PriceMin             *float64                 `json:"price_min"`
	PriceMax             *float64                 `json:"price_max"`
	Currency             string                   `json:"currency" binding:"omitempty,max=8"`
	RegistrationURL      string                   `json:"registration_url" binding:"max=2048"`
	RegistrationDeadline *time.Time               `json:"registration_deadline"`
	CoverURL             string                   `json:"cover_url" binding:"max=2048"`
	Status               string                   `json:"status" binding:"omitempty,oneof=draft published cancelled archived"`
	Visibility           string                   `json:"visibility" binding:"omitempty,oneof=authenticated targeted"`
	Audience             service.EventAudience    `json:"audience"`
	VisibleFrom          *time.Time               `json:"visible_from"`
	VisibleUntil         *time.Time               `json:"visible_until"`
	CancelledReason      string                   `json:"cancelled_reason" binding:"max=1000"`
	Occurrences          []eventOccurrenceRequest `json:"occurrences" binding:"required,min=1,max=50,dive"`
}

type eventStatusRequest struct {
	Reason string `json:"reason" binding:"max=1000"`
}

type eventCategoryRequest struct {
	Code      string `json:"code" binding:"required,max=64"`
	Name      string `json:"name" binding:"required,max=100"`
	Color     string `json:"color" binding:"max=20"`
	Icon      string `json:"icon" binding:"max=64"`
	SortOrder int    `json:"sort_order"`
	Enabled   *bool  `json:"enabled"`
}

type eventSourceRequest struct {
	Code    string         `json:"code" binding:"required,max=64"`
	Name    string         `json:"name" binding:"required,max=100"`
	Kind    string         `json:"kind" binding:"required,oneof=manual json crawler"`
	Enabled *bool          `json:"enabled"`
	Config  map[string]any `json:"config"`
}

func (h *EventHandler) GetMapSettings(c *gin.Context) {
	settings, err := h.settingService.GetEventMapSettings(c.Request.Context())
	if err != nil {
		response.ErrorFrom(c, err)
		return
	}
	response.Success(c, settings)
}

func (h *EventHandler) UpdateMapSettings(c *gin.Context) {
	var req service.EventMapSettings
	if err := c.ShouldBindJSON(&req); err != nil {
		response.ErrorFrom(c, infraerrors.BadRequest("EVENT_MAP_SETTINGS_INVALID", "invalid event map settings"))
		return
	}
	if err := h.settingService.SetEventMapSettings(c.Request.Context(), req); err != nil {
		response.ErrorFrom(c, err)
		return
	}
	response.Success(c, req)
}

func (h *EventHandler) List(c *gin.Context) {
	page, pageSize := response.ParsePagination(c)
	filters, ok := parseEventListFilters(c, false)
	if !ok {
		return
	}
	items, result, err := h.service.ListAdmin(c.Request.Context(), pagination.PaginationParams{
		Page: page, PageSize: pageSize, SortBy: c.DefaultQuery("sort_by", "created_at"), SortOrder: c.DefaultQuery("sort_order", "desc"),
	}, filters)
	if err != nil {
		response.ErrorFrom(c, err)
		return
	}
	response.Paginated(c, dto.EventsFromService(items), result.Total, page, pageSize)
}

func (h *EventHandler) Get(c *gin.Context) {
	id, ok := parseEventResourceID(c)
	if !ok {
		return
	}
	item, err := h.service.GetAdmin(c.Request.Context(), id)
	if err != nil {
		response.ErrorFrom(c, err)
		return
	}
	response.Success(c, dto.EventFromService(item))
}

func (h *EventHandler) Create(c *gin.Context) {
	var req eventWriteRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		response.ErrorFrom(c, infraerrors.BadRequest("EVENT_REQUEST_INVALID", "invalid event request"))
		return
	}
	subject, ok := middleware2.GetAuthSubjectFromContext(c)
	if !ok {
		response.Unauthorized(c, "User not found in context")
		return
	}
	item, err := h.service.Create(c.Request.Context(), eventWriteRequestToService(req, subject.UserID))
	if err != nil {
		response.ErrorFrom(c, err)
		return
	}
	response.Created(c, dto.EventFromService(item))
}

func (h *EventHandler) Update(c *gin.Context) {
	id, ok := parseEventResourceID(c)
	if !ok {
		return
	}
	var req eventWriteRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		response.ErrorFrom(c, infraerrors.BadRequest("EVENT_REQUEST_INVALID", "invalid event request"))
		return
	}
	subject, ok := middleware2.GetAuthSubjectFromContext(c)
	if !ok {
		response.Unauthorized(c, "User not found in context")
		return
	}
	item, err := h.service.Update(c.Request.Context(), id, &service.UpdateEventInput{CreateEventInput: *eventWriteRequestToService(req, subject.UserID), ManualEdit: true})
	if err != nil {
		response.ErrorFrom(c, err)
		return
	}
	response.Success(c, dto.EventFromService(item))
}

func (h *EventHandler) Delete(c *gin.Context) {
	id, ok := parseEventResourceID(c)
	if !ok {
		return
	}
	if err := h.service.Delete(c.Request.Context(), id); err != nil {
		response.ErrorFrom(c, err)
		return
	}
	response.Success(c, gin.H{"message": "event deleted"})
}

func (h *EventHandler) Publish(c *gin.Context) { h.setStatus(c, service.EventStatusPublished) }
func (h *EventHandler) Cancel(c *gin.Context)  { h.setStatus(c, service.EventStatusCancelled) }
func (h *EventHandler) Archive(c *gin.Context) { h.setStatus(c, service.EventStatusArchived) }

func (h *EventHandler) setStatus(c *gin.Context, status string) {
	id, ok := parseEventResourceID(c)
	if !ok {
		return
	}
	var req eventStatusRequest
	if c.Request.ContentLength != 0 {
		if err := c.ShouldBindJSON(&req); err != nil {
			response.ErrorFrom(c, infraerrors.BadRequest("EVENT_STATUS_REQUEST_INVALID", "invalid event status request"))
			return
		}
	}
	subject, ok := middleware2.GetAuthSubjectFromContext(c)
	if !ok {
		response.Unauthorized(c, "User not found in context")
		return
	}
	item, err := h.service.SetStatus(c.Request.Context(), id, status, req.Reason, subject.UserID)
	if err != nil {
		response.ErrorFrom(c, err)
		return
	}
	response.Success(c, dto.EventFromService(item))
}

func (h *EventHandler) ListCategories(c *gin.Context) {
	items, err := h.service.ListCategories(c.Request.Context(), true)
	if err != nil {
		response.ErrorFrom(c, err)
		return
	}
	response.Success(c, dto.EventCategoriesFromService(items))
}

func (h *EventHandler) CreateCategory(c *gin.Context) {
	var req eventCategoryRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		response.ErrorFrom(c, infraerrors.BadRequest("EVENT_CATEGORY_REQUEST_INVALID", "invalid event category request"))
		return
	}
	item, err := h.service.CreateCategory(c.Request.Context(), eventCategoryRequestToService(req))
	if err != nil {
		response.ErrorFrom(c, err)
		return
	}
	response.Created(c, dto.EventCategoryFromService(*item))
}

func (h *EventHandler) UpdateCategory(c *gin.Context) {
	id, ok := parseEventResourceID(c)
	if !ok {
		return
	}
	var req eventCategoryRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		response.ErrorFrom(c, infraerrors.BadRequest("EVENT_CATEGORY_REQUEST_INVALID", "invalid event category request"))
		return
	}
	item, err := h.service.UpdateCategory(c.Request.Context(), id, eventCategoryRequestToService(req))
	if err != nil {
		response.ErrorFrom(c, err)
		return
	}
	response.Success(c, dto.EventCategoryFromService(*item))
}

func (h *EventHandler) DeleteCategory(c *gin.Context) {
	id, ok := parseEventResourceID(c)
	if !ok {
		return
	}
	if err := h.service.DeleteCategory(c.Request.Context(), id); err != nil {
		response.ErrorFrom(c, err)
		return
	}
	response.Success(c, gin.H{"message": "event category deleted"})
}

func (h *EventHandler) ListSources(c *gin.Context) {
	items, err := h.service.ListSources(c.Request.Context(), true)
	if err != nil {
		response.ErrorFrom(c, err)
		return
	}
	response.Success(c, dto.EventSourcesFromService(items))
}

func (h *EventHandler) CreateSource(c *gin.Context) {
	var req eventSourceRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		response.ErrorFrom(c, infraerrors.BadRequest("EVENT_SOURCE_REQUEST_INVALID", "invalid event source request"))
		return
	}
	item, err := h.service.CreateSource(c.Request.Context(), eventSourceRequestToService(req))
	if err != nil {
		response.ErrorFrom(c, err)
		return
	}
	response.Created(c, dto.EventSourceFromService(*item))
}

func (h *EventHandler) UpdateSource(c *gin.Context) {
	id, ok := parseEventResourceID(c)
	if !ok {
		return
	}
	var req eventSourceRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		response.ErrorFrom(c, infraerrors.BadRequest("EVENT_SOURCE_REQUEST_INVALID", "invalid event source request"))
		return
	}
	item, err := h.service.UpdateSource(c.Request.Context(), id, eventSourceRequestToService(req))
	if err != nil {
		response.ErrorFrom(c, err)
		return
	}
	response.Success(c, dto.EventSourceFromService(*item))
}

func (h *EventHandler) DeleteSource(c *gin.Context) {
	id, ok := parseEventResourceID(c)
	if !ok {
		return
	}
	if err := h.service.DeleteSource(c.Request.Context(), id); err != nil {
		response.ErrorFrom(c, err)
		return
	}
	response.Success(c, gin.H{"message": "event source deleted"})
}

type eventImportDefaultsRequest struct {
	Timezone         string `json:"timezone"`
	CoordinateSystem string `json:"coordinate_system"`
	Country          string `json:"country"`
	Province         string `json:"province"`
	City             string `json:"city"`
}

type eventImportOrganizerRequest struct {
	Name string `json:"name"`
	URL  string `json:"url"`
}

type eventImportFeeRequest struct {
	Type     string   `json:"type"`
	PriceMin *float64 `json:"price_min"`
	PriceMax *float64 `json:"price_max"`
	Currency string   `json:"currency"`
}

type eventImportEventRequest struct {
	ExternalID           string                      `json:"external_id"`
	SourceURL            string                      `json:"source_url"`
	SourceUpdatedAt      *time.Time                  `json:"source_updated_at"`
	Category             string                      `json:"category"`
	Title                string                      `json:"title"`
	Summary              string                      `json:"summary"`
	DescriptionMarkdown  string                      `json:"description_markdown"`
	Tags                 []string                    `json:"tags"`
	Organizer            eventImportOrganizerRequest `json:"organizer"`
	Fee                  eventImportFeeRequest       `json:"fee"`
	RegistrationURL      string                      `json:"registration_url"`
	RegistrationDeadline *time.Time                  `json:"registration_deadline"`
	CoverURL             string                      `json:"cover_url"`
	Visibility           string                      `json:"visibility"`
	Audience             service.EventAudience       `json:"audience"`
	VisibleFrom          *time.Time                  `json:"visible_from"`
	VisibleUntil         *time.Time                  `json:"visible_until"`
	Occurrences          []eventOccurrenceRequest    `json:"occurrences"`
}

type eventImportPreviewRequest struct {
	Type     string                     `json:"type" binding:"required"`
	Version  int                        `json:"version" binding:"required"`
	Source   string                     `json:"source" binding:"required,max=64"`
	FileName string                     `json:"file_name" binding:"max=255"`
	Mode     string                     `json:"mode" binding:"omitempty,oneof=create_only upsert"`
	Defaults eventImportDefaultsRequest `json:"defaults"`
	Events   []eventImportEventRequest  `json:"events" binding:"required,min=1,max=1000"`
}

type eventImportCommitRequest struct {
	Publish bool `json:"publish"`
}

func (h *EventHandler) PreviewImport(c *gin.Context) {
	c.Request.Body = http.MaxBytesReader(c.Writer, c.Request.Body, maxEventImportBodyBytes)
	var req eventImportPreviewRequest
	if err := c.ShouldBindJSON(&req); err != nil || req.Type != "sub2api-events" || req.Version != service.EventImportSchemaVersion {
		response.ErrorFrom(c, infraerrors.BadRequest("EVENT_IMPORT_FORMAT_INVALID", "invalid or unsupported event import format"))
		return
	}
	subject, ok := middleware2.GetAuthSubjectFromContext(c)
	if !ok {
		response.Unauthorized(c, "User not found in context")
		return
	}
	items := make([]service.EventImportCandidate, 0, len(req.Events))
	for i := range req.Events {
		item := req.Events[i]
		raw := map[string]any{}
		if encoded, err := json.Marshal(item); err == nil {
			_ = json.Unmarshal(encoded, &raw)
		}
		occurrences := make([]service.EventOccurrenceInput, 0, len(item.Occurrences))
		for j := range item.Occurrences {
			occurrence := item.Occurrences[j]
			if occurrence.Timezone == "" {
				occurrence.Timezone = req.Defaults.Timezone
			}
			if occurrence.CoordinateSource == "" {
				occurrence.CoordinateSource = req.Defaults.CoordinateSystem
			}
			if occurrence.Country == "" {
				occurrence.Country = req.Defaults.Country
			}
			if occurrence.Province == "" {
				occurrence.Province = req.Defaults.Province
			}
			if occurrence.City == "" {
				occurrence.City = req.Defaults.City
			}
			occurrences = append(occurrences, occurrenceRequestToService(occurrence))
		}
		items = append(items, service.EventImportCandidate{
			ExternalID: item.ExternalID, CategoryCode: item.Category, SourceURL: item.SourceURL,
			SourceUpdatedAt: item.SourceUpdatedAt, RawPayload: raw,
			Event: service.CreateEventInput{
				Title: item.Title, Summary: item.Summary, DescriptionMarkdown: item.DescriptionMarkdown,
				Tags: item.Tags, OrganizerName: item.Organizer.Name, OrganizerURL: item.Organizer.URL,
				FeeType: item.Fee.Type, PriceMin: item.Fee.PriceMin, PriceMax: item.Fee.PriceMax,
				Currency: item.Fee.Currency, RegistrationURL: item.RegistrationURL,
				RegistrationDeadline: item.RegistrationDeadline, CoverURL: item.CoverURL,
				Status: service.EventStatusDraft, Visibility: item.Visibility, Audience: item.Audience,
				VisibleFrom: item.VisibleFrom, VisibleUntil: item.VisibleUntil, Occurrences: occurrences,
			},
		})
	}
	fileName := ""
	if strings.TrimSpace(req.FileName) != "" {
		fileName = filepath.Base(req.FileName)
	}
	batch, err := h.service.PreviewImport(c.Request.Context(), &service.PreviewEventImportInput{
		SourceCode: req.Source, FileName: fileName, SchemaVersion: req.Version,
		Mode: req.Mode, ActorID: subject.UserID, Items: items,
	})
	if err != nil {
		response.ErrorFrom(c, err)
		return
	}
	response.Created(c, dto.EventImportBatchFromService(batch))
}

func (h *EventHandler) GetImport(c *gin.Context) {
	id, ok := parseEventResourceID(c)
	if !ok {
		return
	}
	batch, err := h.service.GetImportBatch(c.Request.Context(), id)
	if err != nil {
		response.ErrorFrom(c, err)
		return
	}
	response.Success(c, dto.EventImportBatchFromService(batch))
}

func (h *EventHandler) CommitImport(c *gin.Context) {
	id, ok := parseEventResourceID(c)
	if !ok {
		return
	}
	var req eventImportCommitRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		response.ErrorFrom(c, infraerrors.BadRequest("EVENT_IMPORT_COMMIT_INVALID", "invalid event import commit request"))
		return
	}
	subject, ok := middleware2.GetAuthSubjectFromContext(c)
	if !ok {
		response.Unauthorized(c, "User not found in context")
		return
	}
	batch, err := h.service.CommitImport(c.Request.Context(), &service.CommitEventImportInput{BatchID: id, Publish: req.Publish, ActorID: subject.UserID})
	if err != nil {
		response.ErrorFrom(c, err)
		return
	}
	response.Success(c, dto.EventImportBatchFromService(batch))
}

func eventWriteRequestToService(req eventWriteRequest, actorID int64) *service.CreateEventInput {
	occurrences := make([]service.EventOccurrenceInput, 0, len(req.Occurrences))
	for i := range req.Occurrences {
		occurrences = append(occurrences, occurrenceRequestToService(req.Occurrences[i]))
	}
	return &service.CreateEventInput{
		CategoryID: req.CategoryID, Title: req.Title, Summary: req.Summary, DescriptionMarkdown: req.DescriptionMarkdown,
		Tags: req.Tags, OrganizerName: req.OrganizerName, OrganizerURL: req.OrganizerURL,
		FeeType: req.FeeType, PriceMin: req.PriceMin, PriceMax: req.PriceMax, Currency: req.Currency,
		RegistrationURL: req.RegistrationURL, RegistrationDeadline: req.RegistrationDeadline, CoverURL: req.CoverURL,
		Status: req.Status, Visibility: req.Visibility, Audience: req.Audience, VisibleFrom: req.VisibleFrom,
		VisibleUntil: req.VisibleUntil, CancelledReason: req.CancelledReason, Occurrences: occurrences, ActorID: actorID,
	}
}

func occurrenceRequestToService(req eventOccurrenceRequest) service.EventOccurrenceInput {
	return service.EventOccurrenceInput{
		StartsAt: req.StartsAt, EndsAt: req.EndsAt, Timezone: req.Timezone, AllDay: req.AllDay,
		LocationMode: req.LocationMode, OnlineURL: req.OnlineURL, VenueName: req.VenueName,
		Address: req.Address, Country: req.Country, Province: req.Province, City: req.City,
		District: req.District, Latitude: req.Latitude, Longitude: req.Longitude, CoordinateSource: req.CoordinateSource,
	}
}

func eventCategoryRequestToService(req eventCategoryRequest) service.EventCategory {
	enabled := true
	if req.Enabled != nil {
		enabled = *req.Enabled
	}
	return service.EventCategory{Code: req.Code, Name: req.Name, Color: req.Color, Icon: req.Icon, SortOrder: req.SortOrder, Enabled: enabled}
}

func eventSourceRequestToService(req eventSourceRequest) service.EventSource {
	enabled := true
	if req.Enabled != nil {
		enabled = *req.Enabled
	}
	return service.EventSource{Code: req.Code, Name: req.Name, Kind: req.Kind, Enabled: enabled, Config: req.Config}
}

func parseEventResourceID(c *gin.Context) (int64, bool) {
	id, err := strconv.ParseInt(c.Param("id"), 10, 64)
	if err != nil || id <= 0 {
		response.ErrorFrom(c, infraerrors.BadRequest("EVENT_ID_INVALID", "invalid event resource id"))
		return 0, false
	}
	return id, true
}

func parseEventListFilters(c *gin.Context, defaultUpcoming bool) (service.EventListFilters, bool) {
	filters := service.EventListFilters{
		Status: c.Query("status"), CategoryCode: c.Query("category"), Search: c.Query("search"),
		City: c.Query("city"), District: c.Query("district"), FeeType: c.Query("fee_type"),
	}
	var err error
	if raw := strings.TrimSpace(c.Query("from")); raw != "" {
		filters.From, err = parseEventQueryTime(raw)
		if err != nil {
			response.ErrorFrom(c, infraerrors.BadRequest("EVENT_TIME_FILTER_INVALID", "invalid from time"))
			return service.EventListFilters{}, false
		}
	} else if defaultUpcoming {
		now := time.Now()
		filters.From = &now
	}
	if raw := strings.TrimSpace(c.Query("to")); raw != "" {
		filters.To, err = parseEventQueryTime(raw)
		if err != nil {
			response.ErrorFrom(c, infraerrors.BadRequest("EVENT_TIME_FILTER_INVALID", "invalid to time"))
			return service.EventListFilters{}, false
		}
	}
	if filters.From != nil && filters.To != nil && !filters.From.Before(*filters.To) {
		response.ErrorFrom(c, infraerrors.BadRequest("EVENT_TIME_FILTER_INVALID", "from must be before to"))
		return service.EventListFilters{}, false
	}
	return filters, true
}

func parseEventQueryTime(value string) (*time.Time, error) {
	parsed, err := time.Parse(time.RFC3339, value)
	if err != nil {
		return nil, err
	}
	return &parsed, nil
}
