package repository

import (
	"context"
	"fmt"
	"time"

	dbent "github.com/Wei-Shaw/sub2api/ent"
	"github.com/Wei-Shaw/sub2api/ent/eventimportbatch"
	"github.com/Wei-Shaw/sub2api/ent/eventimportitem"
	"github.com/Wei-Shaw/sub2api/ent/eventsourcerecord"
	"github.com/Wei-Shaw/sub2api/internal/domain"
	"github.com/Wei-Shaw/sub2api/internal/service"
)

func (r *eventRepository) GetSourceRecordByExternalID(ctx context.Context, sourceID int64, externalID string) (*service.EventSourceRecord, error) {
	if externalID == "" {
		return nil, nil
	}
	entity, err := clientFromContext(ctx, r.client).EventSourceRecord.Query().
		Where(eventsourcerecord.SourceIDEQ(sourceID), eventsourcerecord.ExternalIDEQ(externalID)).
		Only(ctx)
	if dbent.IsNotFound(err) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	value := eventSourceRecordEntityToService(entity)
	return &value, nil
}

func (r *eventRepository) GetSourceRecordBySourceAndFingerprint(ctx context.Context, sourceID int64, fingerprint string) (*service.EventSourceRecord, error) {
	if sourceID <= 0 || fingerprint == "" {
		return nil, nil
	}
	entity, err := clientFromContext(ctx, r.client).EventSourceRecord.Query().
		Where(eventsourcerecord.SourceIDEQ(sourceID), eventsourcerecord.FingerprintEQ(fingerprint)).
		Order(dbent.Desc(eventsourcerecord.FieldLastSeenAt), dbent.Desc(eventsourcerecord.FieldID)).
		First(ctx)
	if dbent.IsNotFound(err) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	value := eventSourceRecordEntityToService(entity)
	return &value, nil
}

func (r *eventRepository) GetSourceRecordByFingerprint(ctx context.Context, fingerprint string) (*service.EventSourceRecord, error) {
	if fingerprint == "" {
		return nil, nil
	}
	entity, err := clientFromContext(ctx, r.client).EventSourceRecord.Query().
		Where(eventsourcerecord.FingerprintEQ(fingerprint)).
		Order(dbent.Desc(eventsourcerecord.FieldLastSeenAt), dbent.Desc(eventsourcerecord.FieldID)).
		First(ctx)
	if dbent.IsNotFound(err) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	value := eventSourceRecordEntityToService(entity)
	return &value, nil
}

func (r *eventRepository) SaveImportedEvent(ctx context.Context, value *service.Event, record *service.EventSourceRecord) error {
	if value == nil || record == nil || record.SourceID <= 0 {
		return service.ErrEventInvalid
	}
	return r.withTx(ctx, func(txCtx context.Context, client *dbent.Client) error {
		existing, err := findEventSourceRecord(txCtx, client, record.SourceID, record.ExternalID, record.Fingerprint)
		if err != nil {
			return err
		}
		if value.ID <= 0 && existing != nil && existing.EventID != nil {
			value.ID = *existing.EventID
		}
		if value.ID > 0 {
			if err := r.Update(txCtx, value); err != nil {
				return err
			}
		} else if err := r.Create(txCtx, value); err != nil {
			return err
		}
		record.EventID = &value.ID
		now := time.Now()
		if existing == nil {
			builder := client.EventSourceRecord.Create().
				SetSourceID(record.SourceID).
				SetEventID(value.ID).
				SetExternalID(record.ExternalID).
				SetSourceURL(record.SourceURL).
				SetFingerprint(record.Fingerprint).
				SetContentHash(record.ContentHash).
				SetState(record.State).
				SetRawPayload(record.RawPayload).
				SetNormalizedPayload(record.NormalizedPayload).
				SetFirstSeenAt(now).
				SetLastSeenAt(now)
			if record.SourceUpdatedAt != nil {
				builder.SetSourceUpdatedAt(*record.SourceUpdatedAt)
			}
			created, err := builder.Save(txCtx)
			if err != nil {
				return translatePersistenceError(err, nil, service.ErrEventConflict)
			}
			record.ID = created.ID
			record.FirstSeenAt = created.FirstSeenAt
			record.LastSeenAt = created.LastSeenAt
			return nil
		}
		builder := client.EventSourceRecord.UpdateOneID(existing.ID).
			SetEventID(value.ID).
			SetExternalID(record.ExternalID).
			SetSourceURL(record.SourceURL).
			SetFingerprint(record.Fingerprint).
			SetContentHash(record.ContentHash).
			SetState(record.State).
			SetRawPayload(record.RawPayload).
			SetNormalizedPayload(record.NormalizedPayload).
			SetLastSeenAt(now)
		if record.SourceUpdatedAt != nil {
			builder.SetSourceUpdatedAt(*record.SourceUpdatedAt)
		} else {
			builder.ClearSourceUpdatedAt()
		}
		updated, err := builder.Save(txCtx)
		if err != nil {
			return err
		}
		mapped := eventSourceRecordEntityToService(updated)
		*record = mapped
		return nil
	})
}

func findEventSourceRecord(ctx context.Context, client *dbent.Client, sourceID int64, externalID, fingerprint string) (*dbent.EventSourceRecord, error) {
	if externalID != "" {
		entity, err := client.EventSourceRecord.Query().
			Where(eventsourcerecord.SourceIDEQ(sourceID), eventsourcerecord.ExternalIDEQ(externalID)).
			First(ctx)
		if err == nil {
			return entity, nil
		}
		if !dbent.IsNotFound(err) {
			return nil, err
		}
	}
	if fingerprint == "" {
		return nil, nil
	}
	entity, err := client.EventSourceRecord.Query().
		Where(eventsourcerecord.SourceIDEQ(sourceID), eventsourcerecord.FingerprintEQ(fingerprint)).
		First(ctx)
	if dbent.IsNotFound(err) {
		return nil, nil
	}
	return entity, err
}

func (r *eventRepository) CreateImportBatch(ctx context.Context, batch *service.EventImportBatch) error {
	if batch == nil || batch.SourceID <= 0 || len(batch.Items) == 0 {
		return service.ErrEventInvalid
	}
	return r.withTx(ctx, func(txCtx context.Context, client *dbent.Client) error {
		entity, err := client.EventImportBatch.Create().
			SetSourceID(batch.SourceID).
			SetFileName(batch.FileName).
			SetFileHash(batch.FileHash).
			SetSchemaVersion(batch.SchemaVersion).
			SetMode(batch.Mode).
			SetStatus(batch.Status).
			SetTotalCount(batch.TotalCount).
			SetCreateCount(batch.CreateCount).
			SetUpdateCount(batch.UpdateCount).
			SetUnchangedCount(batch.UnchangedCount).
			SetConflictCount(batch.ConflictCount).
			SetErrorCount(batch.ErrorCount).
			SetCreatedBy(batch.CreatedBy).
			Save(txCtx)
		if err != nil {
			return err
		}
		batch.ID = entity.ID
		batch.CreatedAt = entity.CreatedAt
		batch.UpdatedAt = entity.UpdatedAt
		builders := make([]*dbent.EventImportItemCreate, 0, len(batch.Items))
		for i := range batch.Items {
			item := batch.Items[i]
			builder := client.EventImportItem.Create().
				SetBatchID(batch.ID).
				SetItemIndex(item.ItemIndex).
				SetExternalID(item.ExternalID).
				SetFingerprint(item.Fingerprint).
				SetContentHash(item.ContentHash).
				SetAction(item.Action).
				SetStatus(item.Status).
				SetErrorCode(item.ErrorCode).
				SetErrorDetail(item.ErrorDetail).
				SetNormalizedPayload(item.NormalizedPayload)
			if item.EventID != nil {
				builder.SetEventID(*item.EventID)
			}
			builders = append(builders, builder)
		}
		createdItems, err := client.EventImportItem.CreateBulk(builders...).Save(txCtx)
		if err != nil {
			return fmt.Errorf("create event import items: %w", err)
		}
		for i := range createdItems {
			batch.Items[i] = eventImportItemEntityToService(createdItems[i])
		}
		return nil
	})
}

func (r *eventRepository) GetImportBatch(ctx context.Context, id int64) (*service.EventImportBatch, error) {
	entity, err := clientFromContext(ctx, r.client).EventImportBatch.Query().
		Where(eventimportbatch.IDEQ(id)).
		WithItems(func(q *dbent.EventImportItemQuery) {
			q.Order(dbent.Asc(eventimportitem.FieldItemIndex), dbent.Asc(eventimportitem.FieldID))
		}).
		Only(ctx)
	if err != nil {
		return nil, translatePersistenceError(err, service.ErrEventImportNotFound, nil)
	}
	return eventImportBatchEntityToService(entity), nil
}

func (r *eventRepository) ClaimImportBatch(ctx context.Context, id int64) (bool, error) {
	affected, err := clientFromContext(ctx, r.client).EventImportBatch.Update().
		Where(eventimportbatch.IDEQ(id), eventimportbatch.StatusEQ(domain.EventImportStatusPreviewed)).
		SetStatus(domain.EventImportStatusCommitting).
		Save(ctx)
	return affected == 1, err
}

func (r *eventRepository) UpdateImportBatch(ctx context.Context, batch *service.EventImportBatch) error {
	builder := clientFromContext(ctx, r.client).EventImportBatch.UpdateOneID(batch.ID).
		SetMode(batch.Mode).
		SetStatus(batch.Status).
		SetTotalCount(batch.TotalCount).
		SetCreateCount(batch.CreateCount).
		SetUpdateCount(batch.UpdateCount).
		SetUnchangedCount(batch.UnchangedCount).
		SetConflictCount(batch.ConflictCount).
		SetErrorCount(batch.ErrorCount)
	if batch.CommittedAt != nil {
		builder.SetCommittedAt(*batch.CommittedAt)
	} else {
		builder.ClearCommittedAt()
	}
	entity, err := builder.Save(ctx)
	if err != nil {
		return translatePersistenceError(err, service.ErrEventImportNotFound, nil)
	}
	batch.UpdatedAt = entity.UpdatedAt
	return nil
}

func (r *eventRepository) UpdateImportItem(ctx context.Context, item *service.EventImportItem) error {
	builder := clientFromContext(ctx, r.client).EventImportItem.UpdateOneID(item.ID).
		SetAction(item.Action).
		SetStatus(item.Status).
		SetErrorCode(item.ErrorCode).
		SetErrorDetail(item.ErrorDetail)
	if item.EventID != nil {
		builder.SetEventID(*item.EventID)
	} else {
		builder.ClearEventID()
	}
	entity, err := builder.Save(ctx)
	if err != nil {
		return err
	}
	item.UpdatedAt = entity.UpdatedAt
	return nil
}

func eventSourceRecordEntityToService(entity *dbent.EventSourceRecord) service.EventSourceRecord {
	return service.EventSourceRecord{
		ID: entity.ID, SourceID: entity.SourceID, EventID: entity.EventID, ExternalID: entity.ExternalID,
		SourceURL: entity.SourceURL, Fingerprint: entity.Fingerprint, ContentHash: entity.ContentHash,
		State: entity.State, RawPayload: entity.RawPayload, NormalizedPayload: entity.NormalizedPayload,
		SourceUpdatedAt: entity.SourceUpdatedAt, FirstSeenAt: entity.FirstSeenAt, LastSeenAt: entity.LastSeenAt,
		CreatedAt: entity.CreatedAt, UpdatedAt: entity.UpdatedAt,
	}
}

func eventImportBatchEntityToService(entity *dbent.EventImportBatch) *service.EventImportBatch {
	if entity == nil {
		return nil
	}
	batch := &service.EventImportBatch{
		ID: entity.ID, SourceID: entity.SourceID, FileName: entity.FileName, FileHash: entity.FileHash,
		SchemaVersion: entity.SchemaVersion, Mode: entity.Mode, Status: entity.Status,
		TotalCount: entity.TotalCount, CreateCount: entity.CreateCount, UpdateCount: entity.UpdateCount,
		UnchangedCount: entity.UnchangedCount, ConflictCount: entity.ConflictCount, ErrorCount: entity.ErrorCount,
		CreatedBy: entity.CreatedBy, CommittedAt: entity.CommittedAt, CreatedAt: entity.CreatedAt, UpdatedAt: entity.UpdatedAt,
		Items: make([]service.EventImportItem, 0, len(entity.Edges.Items)),
	}
	for _, item := range entity.Edges.Items {
		batch.Items = append(batch.Items, eventImportItemEntityToService(item))
	}
	return batch
}

func eventImportItemEntityToService(entity *dbent.EventImportItem) service.EventImportItem {
	return service.EventImportItem{
		ID: entity.ID, BatchID: entity.BatchID, ItemIndex: entity.ItemIndex, ExternalID: entity.ExternalID,
		Fingerprint: entity.Fingerprint, ContentHash: entity.ContentHash, Action: entity.Action, Status: entity.Status,
		EventID: entity.EventID, ErrorCode: entity.ErrorCode, ErrorDetail: entity.ErrorDetail,
		NormalizedPayload: entity.NormalizedPayload, CreatedAt: entity.CreatedAt, UpdatedAt: entity.UpdatedAt,
	}
}
