package repository

import (
	"context"
	"fmt"
	"sort"
	"strings"
	"time"

	dbent "github.com/Wei-Shaw/sub2api/ent"
	"github.com/Wei-Shaw/sub2api/ent/event"
	"github.com/Wei-Shaw/sub2api/ent/eventcategory"
	"github.com/Wei-Shaw/sub2api/ent/eventoccurrence"
	"github.com/Wei-Shaw/sub2api/ent/eventsource"
	dbpredicate "github.com/Wei-Shaw/sub2api/ent/predicate"
	"github.com/Wei-Shaw/sub2api/internal/pkg/pagination"
	"github.com/Wei-Shaw/sub2api/internal/service"

	entsql "entgo.io/ent/dialect/sql"
)

type eventRepository struct {
	client *dbent.Client
}

func NewEventRepository(client *dbent.Client) service.EventRepository {
	return &eventRepository{client: client}
}

func (r *eventRepository) Create(ctx context.Context, value *service.Event) error {
	if value == nil {
		return service.ErrEventInvalid
	}
	return r.withTx(ctx, func(txCtx context.Context, client *dbent.Client) error {
		created, err := applyEventCreateFields(client.Event.Create(), value).Save(txCtx)
		if err != nil {
			return translatePersistenceError(err, nil, service.ErrEventConflict)
		}
		value.ID = created.ID
		value.CreatedAt = created.CreatedAt
		value.UpdatedAt = created.UpdatedAt
		if err := createEventOccurrences(txCtx, client, value.ID, value.Occurrences); err != nil {
			return err
		}
		for i := range value.Occurrences {
			value.Occurrences[i].EventID = value.ID
		}
		return nil
	})
}

func (r *eventRepository) Update(ctx context.Context, value *service.Event) error {
	if value == nil || value.ID <= 0 {
		return service.ErrEventInvalid
	}
	return r.withTx(ctx, func(txCtx context.Context, client *dbent.Client) error {
		updated, err := applyEventUpdateFields(client.Event.UpdateOneID(value.ID), value).Save(txCtx)
		if err != nil {
			return translatePersistenceError(err, service.ErrEventNotFound, service.ErrEventConflict)
		}
		if _, err := client.EventOccurrence.Delete().Where(eventoccurrence.EventIDEQ(value.ID)).Exec(txCtx); err != nil {
			return fmt.Errorf("delete event occurrences: %w", err)
		}
		if err := createEventOccurrences(txCtx, client, value.ID, value.Occurrences); err != nil {
			return err
		}
		value.UpdatedAt = updated.UpdatedAt
		for i := range value.Occurrences {
			value.Occurrences[i].EventID = value.ID
		}
		return nil
	})
}

func (r *eventRepository) Delete(ctx context.Context, id int64) error {
	client := clientFromContext(ctx, r.client)
	err := client.Event.DeleteOneID(id).Exec(ctx)
	return translatePersistenceError(err, service.ErrEventNotFound, nil)
}

func (r *eventRepository) GetByID(ctx context.Context, id int64) (*service.Event, error) {
	entity, err := clientFromContext(ctx, r.client).Event.Query().
		Where(event.IDEQ(id)).
		WithCategory().
		WithOccurrences(func(q *dbent.EventOccurrenceQuery) {
			q.Order(dbent.Asc(eventoccurrence.FieldStartsAt), dbent.Asc(eventoccurrence.FieldID))
		}).
		Only(ctx)
	if err != nil {
		return nil, translatePersistenceError(err, service.ErrEventNotFound, nil)
	}
	return eventEntityToService(entity), nil
}

func (r *eventRepository) ListAdmin(
	ctx context.Context,
	params pagination.PaginationParams,
	filters service.EventListFilters,
) ([]service.Event, *pagination.PaginationResult, error) {
	query := applyEventListFilters(r.client.Event.Query(), filters)
	total, err := query.Count(ctx)
	if err != nil {
		return nil, nil, err
	}
	itemsQuery := query.
		WithCategory().
		WithOccurrences(func(q *dbent.EventOccurrenceQuery) {
			q.Order(dbent.Asc(eventoccurrence.FieldStartsAt), dbent.Asc(eventoccurrence.FieldID))
		}).
		Offset(params.Offset()).
		Limit(params.Limit())
	for _, order := range eventListOrders(params) {
		itemsQuery = itemsQuery.Order(order)
	}
	items, err := itemsQuery.All(ctx)
	if err != nil {
		return nil, nil, err
	}
	return eventEntitiesToService(items), paginationResultFromTotal(int64(total), params), nil
}

func (r *eventRepository) ListPublishedForUser(
	ctx context.Context,
	params pagination.PaginationParams,
	filters service.EventListFilters,
	audienceGroupIDs []int64,
) ([]service.Event, *pagination.PaginationResult, error) {
	query := publishedEventQuery(r.client.Event.Query(), filters, audienceGroupIDs)
	total, err := query.Count(ctx)
	if err != nil {
		return nil, nil, err
	}
	items, err := query.
		WithCategory().
		WithOccurrences(func(q *dbent.EventOccurrenceQuery) {
			q.Where(eventOccurrenceFilterPredicates(filters, nil)...)
			q.Order(dbent.Asc(eventoccurrence.FieldStartsAt), dbent.Asc(eventoccurrence.FieldID))
		}).
		Offset(params.Offset()).
		Limit(params.Limit()).
		Order(eventOccurrenceStartOrder(filters), dbent.Asc(event.FieldID)).
		All(ctx)
	if err != nil {
		return nil, nil, err
	}
	return eventEntitiesToService(items), paginationResultFromTotal(int64(total), params), nil
}

func (r *eventRepository) ListPublishedMapForUser(ctx context.Context, filters service.EventMapFilters, audienceGroupIDs []int64) ([]service.Event, error) {
	now := time.Now()
	query := applyEventListFilters(r.client.Event.Query(), filters.EventListFilters).
		Where(
			event.StatusIn(service.EventStatusPublished, service.EventStatusCancelled),
			event.Or(event.VisibleFromIsNil(), event.VisibleFromLTE(now)),
			event.Or(event.VisibleUntilIsNil(), event.VisibleUntilGT(now)),
		)
	query = applyEventAudienceFilter(query, audienceGroupIDs)
	mapOccurrencePredicates := eventMapOccurrenceFilterPredicates(filters.EventListFilters, filters.Bounds)
	query = query.Where(event.HasOccurrencesWith(mapOccurrencePredicates...))
	limit := filters.Limit
	if limit <= 0 || limit > 5000 {
		limit = 5000
	}
	items, err := query.
		WithCategory().
		WithOccurrences(func(q *dbent.EventOccurrenceQuery) {
			q.Where(mapOccurrencePredicates...)
			q.Order(dbent.Asc(eventoccurrence.FieldStartsAt), dbent.Asc(eventoccurrence.FieldID))
		}).
		Limit(limit+1).
		Order(dbent.Desc(event.FieldPublishedAt), dbent.Desc(event.FieldID)).
		All(ctx)
	if err != nil {
		return nil, err
	}
	out := eventEntitiesToService(items)
	sort.SliceStable(out, func(i, j int) bool {
		if len(out[i].Occurrences) == 0 {
			return false
		}
		if len(out[j].Occurrences) == 0 {
			return true
		}
		return out[i].Occurrences[0].StartsAt.Before(out[j].Occurrences[0].StartsAt)
	})
	return out, nil
}

func publishedEventQuery(query *dbent.EventQuery, filters service.EventListFilters, audienceGroupIDs []int64) *dbent.EventQuery {
	now := time.Now()
	query = applyEventListFilters(query, filters).Where(
		event.StatusIn(service.EventStatusPublished, service.EventStatusCancelled),
		event.Or(event.VisibleFromIsNil(), event.VisibleFromLTE(now)),
		event.Or(event.VisibleUntilIsNil(), event.VisibleUntilGT(now)),
	)
	return applyEventAudienceFilter(query, audienceGroupIDs)
}

func applyEventAudienceFilter(query *dbent.EventQuery, audienceGroupIDs []int64) *dbent.EventQuery {
	return query.Where(func(selector *entsql.Selector) {
		visibilityColumn := selector.C(event.FieldVisibility)
		audienceColumn := selector.C(event.FieldAudience)
		predicates := []*entsql.Predicate{entsql.EQ(visibilityColumn, service.EventVisibilityAuthenticated)}
		for _, groupID := range audienceGroupIDs {
			if groupID <= 0 {
				continue
			}
			payload := fmt.Sprintf(`{"subscription_group_ids":[%d]}`, groupID)
			containsGroup := entsql.P(func(builder *entsql.Builder) {
				builder.WriteString(audienceColumn).WriteString(" @> ").Arg(payload).WriteString("::jsonb")
			})
			predicates = append(predicates, entsql.And(
				entsql.EQ(visibilityColumn, service.EventVisibilityTargeted),
				containsGroup,
			))
		}
		selector.Where(entsql.Or(predicates...))
	})
}

func applyEventListFilters(query *dbent.EventQuery, filters service.EventListFilters) *dbent.EventQuery {
	if filters.Status != "" {
		query = query.Where(event.StatusEQ(filters.Status))
	}
	if filters.CategoryCode != "" {
		query = query.Where(event.HasCategoryWith(eventcategory.CodeEQ(filters.CategoryCode)))
	}
	if filters.Search != "" {
		query = query.Where(event.Or(
			event.TitleContainsFold(filters.Search),
			event.SummaryContainsFold(filters.Search),
			event.DescriptionMarkdownContainsFold(filters.Search),
			event.OrganizerNameContainsFold(filters.Search),
		))
	}
	if filters.FeeType != "" {
		query = query.Where(event.FeeTypeEQ(filters.FeeType))
	}
	occurrencePredicates := eventOccurrenceFilterPredicates(filters, nil)
	if len(occurrencePredicates) > 0 {
		query = query.Where(event.HasOccurrencesWith(occurrencePredicates...))
	}
	return query
}

func eventOccurrenceFilterPredicates(filters service.EventListFilters, bounds *service.EventMapBounds) []dbpredicate.EventOccurrence {
	predicates := make([]dbpredicate.EventOccurrence, 0, 10)
	if filters.City != "" {
		predicates = append(predicates, eventoccurrence.CityContainsFold(filters.City))
	}
	if filters.District != "" {
		predicates = append(predicates, eventoccurrence.DistrictContainsFold(filters.District))
	}
	if filters.From != nil {
		predicates = append(predicates, eventoccurrence.Or(
			eventoccurrence.EndsAtIsNil(),
			eventoccurrence.EndsAtGTE(*filters.From),
		))
	}
	if filters.To != nil {
		predicates = append(predicates, eventoccurrence.StartsAtLTE(*filters.To))
	}
	if bounds != nil {
		predicates = append(predicates,
			eventoccurrence.LatitudeNotNil(),
			eventoccurrence.LongitudeNotNil(),
			eventoccurrence.LatitudeGTE(bounds.MinLatitude),
			eventoccurrence.LatitudeLTE(bounds.MaxLatitude),
			eventoccurrence.LongitudeGTE(bounds.MinLongitude),
			eventoccurrence.LongitudeLTE(bounds.MaxLongitude),
		)
	}
	return predicates
}

func eventMapOccurrenceFilterPredicates(filters service.EventListFilters, bounds *service.EventMapBounds) []dbpredicate.EventOccurrence {
	predicates := eventOccurrenceFilterPredicates(filters, bounds)
	if bounds == nil {
		predicates = append(predicates, eventoccurrence.LatitudeNotNil(), eventoccurrence.LongitudeNotNil())
	}
	return predicates
}

func eventOccurrenceStartOrder(filters service.EventListFilters) func(*entsql.Selector) {
	return func(selector *entsql.Selector) {
		occurrences := entsql.Table(eventoccurrence.Table)
		predicates := []*entsql.Predicate{
			entsql.ColumnsEQ(occurrences.C(eventoccurrence.FieldEventID), selector.C(event.FieldID)),
		}
		if filters.City != "" {
			predicates = append(predicates, entsql.ContainsFold(occurrences.C(eventoccurrence.FieldCity), filters.City))
		}
		if filters.District != "" {
			predicates = append(predicates, entsql.ContainsFold(occurrences.C(eventoccurrence.FieldDistrict), filters.District))
		}
		if filters.From != nil {
			predicates = append(predicates, entsql.Or(
				entsql.IsNull(occurrences.C(eventoccurrence.FieldEndsAt)),
				entsql.GTE(occurrences.C(eventoccurrence.FieldEndsAt), *filters.From),
			))
		}
		if filters.To != nil {
			predicates = append(predicates, entsql.LTE(occurrences.C(eventoccurrence.FieldStartsAt), *filters.To))
		}
		subquery := entsql.Select(occurrences.C(eventoccurrence.FieldStartsAt)).
			From(occurrences).
			Where(entsql.And(predicates...)).
			OrderBy(entsql.Asc(occurrences.C(eventoccurrence.FieldStartsAt))).
			Limit(1)
		selector.OrderExpr(entsql.ExprFunc(func(builder *entsql.Builder) {
			builder.WriteByte('(').Join(subquery).WriteString(") ASC NULLS LAST")
		}))
	}
}

func eventListOrders(params pagination.PaginationParams) []func(*entsql.Selector) {
	field := event.FieldCreatedAt
	switch strings.ToLower(strings.TrimSpace(params.SortBy)) {
	case "id":
		field = event.FieldID
	case "title":
		field = event.FieldTitle
	case "status":
		field = event.FieldStatus
	case "published_at":
		field = event.FieldPublishedAt
	case "updated_at":
		field = event.FieldUpdatedAt
	}
	if params.NormalizedSortOrder(pagination.SortOrderDesc) == pagination.SortOrderAsc {
		if field == event.FieldID {
			return []func(*entsql.Selector){dbent.Asc(field)}
		}
		return []func(*entsql.Selector){dbent.Asc(field), dbent.Asc(event.FieldID)}
	}
	if field == event.FieldID {
		return []func(*entsql.Selector){dbent.Desc(field)}
	}
	return []func(*entsql.Selector){dbent.Desc(field), dbent.Desc(event.FieldID)}
}

func applyEventCreateFields(builder *dbent.EventCreate, value *service.Event) *dbent.EventCreate {
	builder.SetTitle(value.Title).
		SetSummary(value.Summary).
		SetDescriptionMarkdown(value.DescriptionMarkdown).
		SetTags(value.Tags).
		SetOrganizerName(value.OrganizerName).
		SetOrganizerURL(value.OrganizerURL).
		SetFeeType(value.FeeType).
		SetCurrency(value.Currency).
		SetRegistrationURL(value.RegistrationURL).
		SetCoverURL(value.CoverURL).
		SetStatus(value.Status).
		SetVisibility(value.Visibility).
		SetAudience(value.Audience).
		SetCancelledReason(value.CancelledReason).
		SetManualOverrideFields(value.ManualOverrideFields)
	if value.CategoryID != nil {
		builder.SetCategoryID(*value.CategoryID)
	}
	if value.PriceMin != nil {
		builder.SetPriceMin(*value.PriceMin)
	}
	if value.PriceMax != nil {
		builder.SetPriceMax(*value.PriceMax)
	}
	if value.RegistrationDeadline != nil {
		builder.SetRegistrationDeadline(*value.RegistrationDeadline)
	}
	if value.VisibleFrom != nil {
		builder.SetVisibleFrom(*value.VisibleFrom)
	}
	if value.VisibleUntil != nil {
		builder.SetVisibleUntil(*value.VisibleUntil)
	}
	if value.PublishedAt != nil {
		builder.SetPublishedAt(*value.PublishedAt)
	}
	if value.CreatedBy != nil {
		builder.SetCreatedBy(*value.CreatedBy)
	}
	if value.UpdatedBy != nil {
		builder.SetUpdatedBy(*value.UpdatedBy)
	}
	return builder
}

func applyEventUpdateFields(builder *dbent.EventUpdateOne, value *service.Event) *dbent.EventUpdateOne {
	builder.SetTitle(value.Title).
		SetSummary(value.Summary).
		SetDescriptionMarkdown(value.DescriptionMarkdown).
		SetTags(value.Tags).
		SetOrganizerName(value.OrganizerName).
		SetOrganizerURL(value.OrganizerURL).
		SetFeeType(value.FeeType).
		SetCurrency(value.Currency).
		SetRegistrationURL(value.RegistrationURL).
		SetCoverURL(value.CoverURL).
		SetStatus(value.Status).
		SetVisibility(value.Visibility).
		SetAudience(value.Audience).
		SetCancelledReason(value.CancelledReason).
		SetManualOverrideFields(value.ManualOverrideFields)
	if value.CategoryID != nil {
		builder.SetCategoryID(*value.CategoryID)
	} else {
		builder.ClearCategoryID()
	}
	if value.PriceMin != nil {
		builder.SetPriceMin(*value.PriceMin)
	} else {
		builder.ClearPriceMin()
	}
	if value.PriceMax != nil {
		builder.SetPriceMax(*value.PriceMax)
	} else {
		builder.ClearPriceMax()
	}
	if value.RegistrationDeadline != nil {
		builder.SetRegistrationDeadline(*value.RegistrationDeadline)
	} else {
		builder.ClearRegistrationDeadline()
	}
	if value.VisibleFrom != nil {
		builder.SetVisibleFrom(*value.VisibleFrom)
	} else {
		builder.ClearVisibleFrom()
	}
	if value.VisibleUntil != nil {
		builder.SetVisibleUntil(*value.VisibleUntil)
	} else {
		builder.ClearVisibleUntil()
	}
	if value.PublishedAt != nil {
		builder.SetPublishedAt(*value.PublishedAt)
	} else {
		builder.ClearPublishedAt()
	}
	if value.CreatedBy != nil {
		builder.SetCreatedBy(*value.CreatedBy)
	} else {
		builder.ClearCreatedBy()
	}
	if value.UpdatedBy != nil {
		builder.SetUpdatedBy(*value.UpdatedBy)
	} else {
		builder.ClearUpdatedBy()
	}
	return builder
}

func createEventOccurrences(ctx context.Context, client *dbent.Client, eventID int64, values []service.EventOccurrence) error {
	builders := make([]*dbent.EventOccurrenceCreate, 0, len(values))
	for i := range values {
		value := values[i]
		builder := client.EventOccurrence.Create().
			SetEventID(eventID).
			SetStartsAt(value.StartsAt).
			SetTimezone(value.Timezone).
			SetAllDay(value.AllDay).
			SetLocationMode(value.LocationMode).
			SetOnlineURL(value.OnlineURL).
			SetVenueName(value.VenueName).
			SetAddress(value.Address).
			SetCountry(value.Country).
			SetProvince(value.Province).
			SetCity(value.City).
			SetDistrict(value.District).
			SetCoordinateSource(value.CoordinateSource).
			SetGeocodeStatus(value.GeocodeStatus).
			SetGeocodePrecision(value.GeocodePrecision).
			SetProviderPlaceID(value.ProviderPlaceID)
		if value.EndsAt != nil {
			builder.SetEndsAt(*value.EndsAt)
		}
		if value.Latitude != nil {
			builder.SetLatitude(*value.Latitude)
			builder.SetLongitude(*value.Longitude)
		}
		builders = append(builders, builder)
	}
	if len(builders) == 0 {
		return service.ErrEventInvalid
	}
	if _, err := client.EventOccurrence.CreateBulk(builders...).Save(ctx); err != nil {
		return fmt.Errorf("create event occurrences: %w", err)
	}
	return nil
}

func (r *eventRepository) ListCategories(ctx context.Context, includeDisabled bool) ([]service.EventCategory, error) {
	query := r.client.EventCategory.Query()
	if !includeDisabled {
		query = query.Where(eventcategory.EnabledEQ(true))
	}
	entities, err := query.Order(dbent.Asc(eventcategory.FieldSortOrder), dbent.Asc(eventcategory.FieldID)).All(ctx)
	if err != nil {
		return nil, err
	}
	out := make([]service.EventCategory, 0, len(entities))
	for _, entity := range entities {
		out = append(out, eventCategoryEntityToService(entity))
	}
	return out, nil
}

func (r *eventRepository) CreateCategory(ctx context.Context, value *service.EventCategory) error {
	entity, err := clientFromContext(ctx, r.client).EventCategory.Create().
		SetCode(value.Code).SetName(value.Name).SetColor(value.Color).SetIcon(value.Icon).
		SetSortOrder(value.SortOrder).SetEnabled(value.Enabled).Save(ctx)
	if err != nil {
		return translatePersistenceError(err, nil, service.ErrEventConflict)
	}
	*value = eventCategoryEntityToService(entity)
	return nil
}

func (r *eventRepository) UpdateCategory(ctx context.Context, value *service.EventCategory) error {
	entity, err := clientFromContext(ctx, r.client).EventCategory.UpdateOneID(value.ID).
		SetCode(value.Code).SetName(value.Name).SetColor(value.Color).SetIcon(value.Icon).
		SetSortOrder(value.SortOrder).SetEnabled(value.Enabled).Save(ctx)
	if err != nil {
		return translatePersistenceError(err, service.ErrEventCategoryNotFound, service.ErrEventConflict)
	}
	*value = eventCategoryEntityToService(entity)
	return nil
}

func (r *eventRepository) DeleteCategory(ctx context.Context, id int64) error {
	err := clientFromContext(ctx, r.client).EventCategory.DeleteOneID(id).Exec(ctx)
	return translatePersistenceError(err, service.ErrEventCategoryNotFound, nil)
}

func (r *eventRepository) GetCategoryByID(ctx context.Context, id int64) (*service.EventCategory, error) {
	entity, err := r.client.EventCategory.Get(ctx, id)
	if err != nil {
		return nil, translatePersistenceError(err, service.ErrEventCategoryNotFound, nil)
	}
	value := eventCategoryEntityToService(entity)
	return &value, nil
}

func (r *eventRepository) ListSources(ctx context.Context, includeDisabled bool) ([]service.EventSource, error) {
	query := r.client.EventSource.Query()
	if !includeDisabled {
		query = query.Where(eventsource.EnabledEQ(true))
	}
	entities, err := query.Order(dbent.Asc(eventsource.FieldName), dbent.Asc(eventsource.FieldID)).All(ctx)
	if err != nil {
		return nil, err
	}
	out := make([]service.EventSource, 0, len(entities))
	for _, entity := range entities {
		out = append(out, eventSourceEntityToService(entity))
	}
	return out, nil
}

func (r *eventRepository) CreateSource(ctx context.Context, value *service.EventSource) error {
	entity, err := clientFromContext(ctx, r.client).EventSource.Create().
		SetCode(value.Code).SetName(value.Name).SetKind(value.Kind).SetEnabled(value.Enabled).SetConfig(value.Config).
		Save(ctx)
	if err != nil {
		return translatePersistenceError(err, nil, service.ErrEventConflict)
	}
	*value = eventSourceEntityToService(entity)
	return nil
}

func (r *eventRepository) UpdateSource(ctx context.Context, value *service.EventSource) error {
	builder := clientFromContext(ctx, r.client).EventSource.UpdateOneID(value.ID).
		SetCode(value.Code).SetName(value.Name).SetKind(value.Kind).SetEnabled(value.Enabled).SetConfig(value.Config)
	if value.LastSyncAt != nil {
		builder.SetLastSyncAt(*value.LastSyncAt)
	} else {
		builder.ClearLastSyncAt()
	}
	entity, err := builder.Save(ctx)
	if err != nil {
		return translatePersistenceError(err, service.ErrEventSourceNotFound, service.ErrEventConflict)
	}
	*value = eventSourceEntityToService(entity)
	return nil
}

func (r *eventRepository) DeleteSource(ctx context.Context, id int64) error {
	err := clientFromContext(ctx, r.client).EventSource.DeleteOneID(id).Exec(ctx)
	if dbent.IsConstraintError(err) {
		return service.ErrEventConflict.WithCause(err)
	}
	return translatePersistenceError(err, service.ErrEventSourceNotFound, nil)
}

func (r *eventRepository) GetSourceByID(ctx context.Context, id int64) (*service.EventSource, error) {
	entity, err := r.client.EventSource.Get(ctx, id)
	if err != nil {
		return nil, translatePersistenceError(err, service.ErrEventSourceNotFound, nil)
	}
	value := eventSourceEntityToService(entity)
	return &value, nil
}

func (r *eventRepository) GetSourceByCode(ctx context.Context, code string) (*service.EventSource, error) {
	entity, err := r.client.EventSource.Query().Where(eventsource.CodeEQ(code)).Only(ctx)
	if err != nil {
		return nil, translatePersistenceError(err, service.ErrEventSourceNotFound, nil)
	}
	value := eventSourceEntityToService(entity)
	return &value, nil
}

func (r *eventRepository) withTx(ctx context.Context, fn func(context.Context, *dbent.Client) error) error {
	if tx := dbent.TxFromContext(ctx); tx != nil {
		return fn(ctx, tx.Client())
	}
	tx, err := r.client.Tx(ctx)
	if err != nil {
		return fmt.Errorf("begin event transaction: %w", err)
	}
	defer func() { _ = tx.Rollback() }()
	txCtx := dbent.NewTxContext(ctx, tx)
	if err := fn(txCtx, tx.Client()); err != nil {
		return err
	}
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("commit event transaction: %w", err)
	}
	return nil
}

func eventEntityToService(entity *dbent.Event) *service.Event {
	if entity == nil {
		return nil
	}
	value := &service.Event{
		ID:                   entity.ID,
		CategoryID:           entity.CategoryID,
		Title:                entity.Title,
		Summary:              entity.Summary,
		DescriptionMarkdown:  entity.DescriptionMarkdown,
		Tags:                 append([]string(nil), entity.Tags...),
		OrganizerName:        entity.OrganizerName,
		OrganizerURL:         entity.OrganizerURL,
		FeeType:              entity.FeeType,
		PriceMin:             entity.PriceMin,
		PriceMax:             entity.PriceMax,
		Currency:             entity.Currency,
		RegistrationURL:      entity.RegistrationURL,
		RegistrationDeadline: entity.RegistrationDeadline,
		CoverURL:             entity.CoverURL,
		Status:               entity.Status,
		Visibility:           entity.Visibility,
		Audience:             entity.Audience,
		VisibleFrom:          entity.VisibleFrom,
		VisibleUntil:         entity.VisibleUntil,
		PublishedAt:          entity.PublishedAt,
		CancelledReason:      entity.CancelledReason,
		ManualOverrideFields: append([]string(nil), entity.ManualOverrideFields...),
		CreatedBy:            entity.CreatedBy,
		UpdatedBy:            entity.UpdatedBy,
		CreatedAt:            entity.CreatedAt,
		UpdatedAt:            entity.UpdatedAt,
		DeletedAt:            entity.DeletedAt,
	}
	if entity.Edges.Category != nil {
		category := eventCategoryEntityToService(entity.Edges.Category)
		value.Category = &category
	}
	value.Occurrences = make([]service.EventOccurrence, 0, len(entity.Edges.Occurrences))
	for _, occurrence := range entity.Edges.Occurrences {
		value.Occurrences = append(value.Occurrences, eventOccurrenceEntityToService(occurrence))
	}
	return value
}

func eventEntitiesToService(entities []*dbent.Event) []service.Event {
	out := make([]service.Event, 0, len(entities))
	for _, entity := range entities {
		if value := eventEntityToService(entity); value != nil {
			out = append(out, *value)
		}
	}
	return out
}

func eventOccurrenceEntityToService(entity *dbent.EventOccurrence) service.EventOccurrence {
	return service.EventOccurrence{
		ID:               entity.ID,
		EventID:          entity.EventID,
		StartsAt:         entity.StartsAt,
		EndsAt:           entity.EndsAt,
		Timezone:         entity.Timezone,
		AllDay:           entity.AllDay,
		LocationMode:     entity.LocationMode,
		OnlineURL:        entity.OnlineURL,
		VenueName:        entity.VenueName,
		Address:          entity.Address,
		Country:          entity.Country,
		Province:         entity.Province,
		City:             entity.City,
		District:         entity.District,
		Latitude:         entity.Latitude,
		Longitude:        entity.Longitude,
		CoordinateSource: entity.CoordinateSource,
		GeocodeStatus:    entity.GeocodeStatus,
		GeocodePrecision: entity.GeocodePrecision,
		ProviderPlaceID:  entity.ProviderPlaceID,
		CreatedAt:        entity.CreatedAt,
		UpdatedAt:        entity.UpdatedAt,
	}
}

func eventCategoryEntityToService(entity *dbent.EventCategory) service.EventCategory {
	return service.EventCategory{
		ID:        entity.ID,
		Code:      entity.Code,
		Name:      entity.Name,
		Color:     entity.Color,
		Icon:      entity.Icon,
		SortOrder: entity.SortOrder,
		Enabled:   entity.Enabled,
		CreatedAt: entity.CreatedAt,
		UpdatedAt: entity.UpdatedAt,
	}
}

func eventSourceEntityToService(entity *dbent.EventSource) service.EventSource {
	return service.EventSource{
		ID:         entity.ID,
		Code:       entity.Code,
		Name:       entity.Name,
		Kind:       entity.Kind,
		Enabled:    entity.Enabled,
		Config:     entity.Config,
		LastSyncAt: entity.LastSyncAt,
		CreatedAt:  entity.CreatedAt,
		UpdatedAt:  entity.UpdatedAt,
	}
}
