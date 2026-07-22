package handler

import (
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

type EventHandler struct {
	service *service.EventService
}

func NewEventHandler(eventService *service.EventService) *EventHandler {
	return &EventHandler{service: eventService}
}

func (h *EventHandler) List(c *gin.Context) {
	subject, ok := middleware2.GetAuthSubjectFromContext(c)
	if !ok {
		response.Unauthorized(c, "User not found in context")
		return
	}
	page, pageSize := response.ParsePagination(c)
	if page > 10000 {
		page = 10000
	}
	if pageSize > 100 {
		pageSize = 100
	}
	filters, ok := parseUserEventFilters(c, true)
	if !ok {
		return
	}
	items, result, err := h.service.ListForUser(c.Request.Context(), subject.UserID, pagination.PaginationParams{
		Page: page, PageSize: pageSize, SortBy: "starts_at", SortOrder: "asc",
	}, filters)
	if err != nil {
		response.ErrorFrom(c, err)
		return
	}
	response.Paginated(c, dto.UserEventsFromService(items), result.Total, page, pageSize)
}

func (h *EventHandler) Map(c *gin.Context) {
	subject, ok := middleware2.GetAuthSubjectFromContext(c)
	if !ok {
		response.Unauthorized(c, "User not found in context")
		return
	}
	filters, ok := parseUserEventFilters(c, true)
	if !ok {
		return
	}
	bounds, ok := parseEventMapBounds(c.Query("bbox"))
	if !ok {
		response.ErrorFrom(c, infraerrors.BadRequest("EVENT_BBOX_INVALID", "invalid map bounds"))
		return
	}
	items, truncated, err := h.service.MapForUser(c.Request.Context(), subject.UserID, service.EventMapFilters{
		EventListFilters: filters,
		Bounds:           bounds,
		Limit:            2000,
	})
	if err != nil {
		response.ErrorFrom(c, err)
		return
	}
	response.Success(c, gin.H{"markers": dto.EventMapMarkersFromService(items), "truncated": truncated})
}

func (h *EventHandler) Get(c *gin.Context) {
	subject, ok := middleware2.GetAuthSubjectFromContext(c)
	if !ok {
		response.Unauthorized(c, "User not found in context")
		return
	}
	id, err := strconv.ParseInt(c.Param("id"), 10, 64)
	if err != nil || id <= 0 {
		response.ErrorFrom(c, infraerrors.BadRequest("EVENT_ID_INVALID", "invalid event id"))
		return
	}
	item, err := h.service.GetForUser(c.Request.Context(), subject.UserID, id)
	if err != nil {
		response.ErrorFrom(c, err)
		return
	}
	response.Success(c, dto.UserEventFromService(item))
}

func (h *EventHandler) Categories(c *gin.Context) {
	items, err := h.service.ListCategoriesForUser(c.Request.Context())
	if err != nil {
		response.ErrorFrom(c, err)
		return
	}
	response.Success(c, dto.EventCategoriesFromService(items))
}

func parseUserEventFilters(c *gin.Context, defaultUpcoming bool) (service.EventListFilters, bool) {
	filters := service.EventListFilters{
		CategoryCode: c.Query("category"), Search: c.Query("search"), City: c.Query("city"),
		District: c.Query("district"), FeeType: c.Query("fee_type"),
	}
	var err error
	if raw := strings.TrimSpace(c.Query("from")); raw != "" {
		filters.From, err = parseUserEventTime(raw)
		if err != nil {
			response.ErrorFrom(c, infraerrors.BadRequest("EVENT_TIME_FILTER_INVALID", "invalid from time"))
			return service.EventListFilters{}, false
		}
	} else if defaultUpcoming {
		now := time.Now()
		filters.From = &now
	}
	if raw := strings.TrimSpace(c.Query("to")); raw != "" {
		filters.To, err = parseUserEventTime(raw)
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

func parseUserEventTime(value string) (*time.Time, error) {
	parsed, err := time.Parse(time.RFC3339, value)
	if err != nil {
		return nil, err
	}
	return &parsed, nil
}

func parseEventMapBounds(value string) (*service.EventMapBounds, bool) {
	value = strings.TrimSpace(value)
	if value == "" {
		return nil, true
	}
	parts := strings.Split(value, ",")
	if len(parts) != 4 {
		return nil, false
	}
	values := make([]float64, 4)
	for i := range parts {
		parsed, err := strconv.ParseFloat(strings.TrimSpace(parts[i]), 64)
		if err != nil {
			return nil, false
		}
		values[i] = parsed
	}
	bounds := &service.EventMapBounds{MinLongitude: values[0], MinLatitude: values[1], MaxLongitude: values[2], MaxLatitude: values[3]}
	if bounds.MinLongitude < -180 || bounds.MaxLongitude > 180 || bounds.MinLatitude < -90 || bounds.MaxLatitude > 90 ||
		bounds.MinLongitude >= bounds.MaxLongitude || bounds.MinLatitude >= bounds.MaxLatitude {
		return nil, false
	}
	return bounds, true
}
