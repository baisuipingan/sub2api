package repository

import (
	"context"
	"database/sql"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/service"
)

type imageStudioGalleryRepository struct{ db *sql.DB }

const (
	galleryMaxUserBytes  int64 = 1 << 30
	galleryMaxUserAssets       = 500
	galleryMaxUserTasks        = 1000
	// Reserve a maximum original plus bounded preview/thumbnail overhead.
	galleryReservedBytesPerJobAsset int64 = service.GalleryMaxImageBytes + (16 << 20)
	galleryLockNamespace            int64 = 8_000_000_000_000_000
	galleryTargetLock               int64 = 7_000_000_000_000_000
)

func (r *imageStudioGalleryRepository) LockTarget(ctx context.Context) (func(), error) {
	return r.lockSession(ctx, galleryTargetLock)
}

func (r *imageStudioGalleryRepository) LockCleanup(ctx context.Context, userID int64) (func(), error) {
	return r.lockSession(ctx, galleryLockNamespace+userID)
}

func (r *imageStudioGalleryRepository) lockSession(ctx context.Context, key int64) (func(), error) {
	if r.db.Stats().MaxOpenConnections == 1 {
		return nil, fmt.Errorf("%w: gallery target locking requires at least two PostgreSQL connections", service.ErrGalleryUnavailable)
	}
	ticker := time.NewTicker(100 * time.Millisecond)
	defer ticker.Stop()
	for {
		conn, err := r.db.Conn(ctx)
		if err != nil {
			return nil, err
		}
		var acquired bool
		err = conn.QueryRowContext(ctx, "SELECT pg_try_advisory_lock($1)", key).Scan(&acquired)
		if err != nil {
			_ = conn.Close()
			return nil, err
		}
		if acquired {
			return func() {
				_, _ = conn.ExecContext(context.Background(), "SELECT pg_advisory_unlock($1)", key)
				_ = conn.Close()
			}, nil
		}
		// Never hold a pooled connection while waiting; the lock owner needs another
		// connection to finish the protected database work.
		_ = conn.Close()
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		case <-ticker.C:
		}
	}
}

func galleryLockUser(ctx context.Context, tx *sql.Tx, userID int64) error {
	_, err := tx.ExecContext(ctx, `SELECT pg_advisory_xact_lock($1)`, galleryLockNamespace+userID)
	return err
}

func galleryRequireActiveUser(ctx context.Context, tx *sql.Tx, userID int64) error {
	var deletedAt sql.NullTime
	err := tx.QueryRowContext(ctx, `SELECT deleted_at FROM users WHERE id=$1 FOR SHARE`, userID).Scan(&deletedAt)
	if errors.Is(err, sql.ErrNoRows) || deletedAt.Valid {
		return service.ErrGalleryGone
	}
	return err
}

func NewImageStudioGalleryRepository(db *sql.DB) service.GalleryRepository {
	return &imageStudioGalleryRepository{db: db}
}

func (r *imageStudioGalleryRepository) Stats(ctx context.Context, userID int64) (service.GalleryStats, error) {
	stats := service.GalleryStats{
		MaxTasks: galleryMaxUserTasks, MaxAssets: galleryMaxUserAssets, MaxBytes: galleryMaxUserBytes,
	}
	err := r.db.QueryRowContext(ctx, `SELECT
        (SELECT COUNT(*) FROM image_studio_gallery_tasks
          WHERE user_id=$1 AND deleted_at IS NULL),
        (SELECT COUNT(*) FROM image_studio_gallery_tasks
          WHERE user_id=$1 AND deleted_at IS NULL AND task_json->>'status'='done'),
        (SELECT COUNT(*) FROM image_studio_gallery_tasks
          WHERE user_id=$1 AND deleted_at IS NULL AND task_json->>'status'='error'),
        (SELECT COUNT(*) FROM image_studio_gallery_task_assets l
          JOIN image_studio_gallery_tasks t ON t.user_id=l.user_id AND t.task_id=l.task_id
          WHERE l.user_id=$1 AND l.role='output' AND t.deleted_at IS NULL),
        (SELECT COUNT(*) FROM image_studio_gallery_assets
          WHERE user_id=$1),
        (SELECT COALESCE(SUM(byte_size+preview_byte_size+thumbnail_byte_size),0)
          FROM image_studio_gallery_assets WHERE user_id=$1)`, userID).
		Scan(&stats.Tasks, &stats.DoneTasks, &stats.FailedTasks, &stats.OutputImages, &stats.Assets, &stats.UsedBytes)
	return stats, err
}

func (r *imageStudioGalleryRepository) HasAssets(ctx context.Context) (bool, error) {
	var exists bool
	err := r.db.QueryRowContext(ctx, `SELECT EXISTS(SELECT 1 FROM image_studio_gallery_assets)`).Scan(&exists)
	return exists, err
}

// Ent soft-deletes users, so database FK cascades never run. Remove their
// gallery links and durably mark objects for the retryable S3 cleanup pass.
func (r *imageStudioGalleryRepository) MarkDeletedUsersForCleanup(ctx context.Context, limit int) error {
	if limit <= 0 || limit > 100 {
		limit = 20
	}
	rows, err := r.db.QueryContext(ctx, `SELECT u.id FROM users u WHERE u.deleted_at IS NOT NULL
        AND (EXISTS (SELECT 1 FROM image_studio_gallery_tasks t WHERE t.user_id=u.id)
          OR EXISTS (SELECT 1 FROM image_studio_gallery_assets a
            WHERE a.user_id=u.id AND a.state<>'deleting')
          OR EXISTS (SELECT 1 FROM image_studio_jobs j
            WHERE j.user_id=u.id AND j.deleted_at IS NULL)) LIMIT $1`, limit)
	if err != nil {
		return err
	}
	var userIDs []int64
	for rows.Next() {
		var id int64
		if err := rows.Scan(&id); err != nil {
			_ = rows.Close()
			return err
		}
		userIDs = append(userIDs, id)
	}
	if err := rows.Err(); err != nil {
		_ = rows.Close()
		return err
	}
	_ = rows.Close()
	for _, userID := range userIDs {
		tx, err := r.db.BeginTx(ctx, nil)
		if err != nil {
			return err
		}
		if err := galleryLockUser(ctx, tx, userID); err != nil {
			_ = tx.Rollback()
			return err
		}
		// Stop accepted Studio work before releasing job-input FK references.
		// A late gateway result cannot be archived after the tombstone is set.
		if _, err := tx.ExecContext(ctx, `UPDATE image_studio_jobs SET status='cancelled',
            deleted_at=COALESCE(deleted_at,NOW()),updated_at=NOW()
            WHERE user_id=$1 AND deleted_at IS NULL`, userID); err != nil {
			_ = tx.Rollback()
			return err
		}
		if _, err := tx.ExecContext(ctx, `UPDATE image_studio_job_calls SET status='cancelled',
            billing_state=CASE WHEN sent_at IS NULL THEN billing_state ELSE 'unknown' END,
            lease_token=NULL,lease_until=NULL,updated_at=NOW()
            WHERE user_id=$1 AND status IN ('queued','generating','saving')`, userID); err != nil {
			_ = tx.Rollback()
			return err
		}
		if _, err := tx.ExecContext(ctx, `DELETE FROM image_studio_job_inputs WHERE user_id=$1`, userID); err != nil {
			_ = tx.Rollback()
			return err
		}
		if _, err := tx.ExecContext(ctx, `DELETE FROM image_studio_gallery_tasks WHERE user_id=$1`, userID); err != nil {
			_ = tx.Rollback()
			return err
		}
		if _, err := tx.ExecContext(ctx, `UPDATE image_studio_gallery_assets SET state='deleting',updated_at=NOW() WHERE user_id=$1`, userID); err != nil {
			_ = tx.Rollback()
			return err
		}
		if err := tx.Commit(); err != nil {
			return err
		}
	}
	return nil
}

const galleryAssetColumns = `user_id, image_id, sha256, mime_type, byte_size, width, height,
    preview_byte_size, preview_width, preview_height, thumbnail_byte_size, thumbnail_width,
    thumbnail_height, storage_endpoint, storage_region, storage_bucket, original_key,
    preview_key, thumbnail_key, state`

const galleryAssetUpdatedColumns = `a.user_id, a.image_id, a.sha256, a.mime_type, a.byte_size, a.width, a.height,
    a.preview_byte_size, a.preview_width, a.preview_height, a.thumbnail_byte_size, a.thumbnail_width,
    a.thumbnail_height, a.storage_endpoint, a.storage_region, a.storage_bucket, a.original_key,
    a.preview_key, a.thumbnail_key, a.state`

type galleryScanner interface{ Scan(dest ...any) error }

func scanGalleryAsset(row galleryScanner) (*service.GalleryAsset, error) {
	var a service.GalleryAsset
	err := row.Scan(&a.UserID, &a.ImageID, &a.SHA256, &a.MIME, &a.Bytes, &a.Width, &a.Height,
		&a.PreviewBytes, &a.PreviewWidth, &a.PreviewHeight, &a.ThumbnailBytes, &a.ThumbnailWidth,
		&a.ThumbnailHeight, &a.Endpoint, &a.Region, &a.Bucket, &a.OriginalKey,
		&a.PreviewKey, &a.ThumbnailKey, &a.State)
	return &a, err
}

func (r *imageStudioGalleryRepository) GetAsset(ctx context.Context, userID int64, imageID string) (*service.GalleryAsset, error) {
	a, err := scanGalleryAsset(r.db.QueryRowContext(ctx, `SELECT `+galleryAssetColumns+` FROM image_studio_gallery_assets WHERE user_id=$1 AND image_id=$2`, userID, imageID))
	if errors.Is(err, sql.ErrNoRows) {
		return nil, service.ErrGalleryNotFound
	}
	return a, err
}

func (r *imageStudioGalleryRepository) ReserveAsset(ctx context.Context, a service.GalleryAsset) (*service.GalleryAsset, error) {
	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return nil, err
	}
	defer func() { _ = tx.Rollback() }()
	if err := galleryLockUser(ctx, tx, a.UserID); err != nil {
		return nil, err
	}
	if err := galleryRequireActiveUser(ctx, tx, a.UserID); err != nil {
		return nil, err
	}
	previous, err := scanGalleryAsset(tx.QueryRowContext(ctx, `SELECT `+galleryAssetColumns+` FROM image_studio_gallery_assets WHERE user_id=$1 AND image_id=$2`, a.UserID, a.ImageID))
	if err == nil {
		return previous, tx.Commit()
	}
	if !errors.Is(err, sql.ErrNoRows) {
		return nil, err
	}
	var count int64
	var total int64
	err = tx.QueryRowContext(ctx, `SELECT COUNT(*),COALESCE(SUM(byte_size+preview_byte_size+thumbnail_byte_size),0)
        FROM image_studio_gallery_assets WHERE user_id=$1`, a.UserID).Scan(&count, &total)
	if err != nil {
		return nil, err
	}
	var reservedAssets int64
	if err := tx.QueryRowContext(ctx, `SELECT COALESCE(SUM((request_json->>'image_count')::bigint *
        CASE WHEN request_json->>'postprocess'='keyed' THEN 2 ELSE 1 END),0)
        FROM image_studio_jobs WHERE user_id=$1 AND deleted_at IS NULL
          AND status IN ('queued','generating','saving')`, a.UserID).Scan(&reservedAssets); err != nil {
		return nil, err
	}
	if a.AcceptedJobTaskID != "" {
		var ownAssets int64
		var endpoint, region, bucket string
		err := tx.QueryRowContext(ctx, `SELECT (request_json->>'image_count')::bigint *
            CASE WHEN request_json->>'postprocess'='keyed' THEN 2 ELSE 1 END,
            storage_endpoint,storage_region,storage_bucket FROM image_studio_jobs
            WHERE user_id=$1 AND task_id=$2 AND deleted_at IS NULL
              AND status IN ('queued','generating','saving')`, a.UserID, a.AcceptedJobTaskID).
			Scan(&ownAssets, &endpoint, &region, &bucket)
		if errors.Is(err, sql.ErrNoRows) {
			return nil, service.ErrGalleryGone
		}
		if err != nil {
			return nil, err
		}
		if endpoint != a.Endpoint || region != a.Region || bucket != a.Bucket {
			return nil, service.ErrGalleryUnavailable
		}
		reservedAssets -= ownAssets
	}
	if reservedAssets < 0 {
		reservedAssets = 0
	}
	if count+reservedAssets+1 > galleryMaxUserAssets ||
		total+a.Bytes+a.PreviewBytes+a.ThumbnailBytes+reservedAssets*galleryReservedBytesPerJobAsset > galleryMaxUserBytes {
		return nil, service.ErrGalleryQuota
	}
	_, err = tx.ExecContext(ctx, `INSERT INTO image_studio_gallery_assets (
        user_id,image_id,sha256,mime_type,byte_size,width,height,
        preview_byte_size,preview_width,preview_height,thumbnail_byte_size,thumbnail_width,thumbnail_height,
        storage_endpoint,storage_region,storage_bucket,original_key,preview_key,thumbnail_key,state)
        VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13,$14,$15,$16,$17,$18,$19,'uploading')
        ON CONFLICT (user_id,image_id) DO NOTHING`,
		a.UserID, a.ImageID, a.SHA256, a.MIME, a.Bytes, a.Width, a.Height,
		a.PreviewBytes, a.PreviewWidth, a.PreviewHeight, a.ThumbnailBytes, a.ThumbnailWidth, a.ThumbnailHeight,
		a.Endpoint, a.Region, a.Bucket, a.OriginalKey, a.PreviewKey, a.ThumbnailKey)
	if err != nil {
		return nil, fmt.Errorf("reserve gallery asset: %w", err)
	}
	if err := tx.Commit(); err != nil {
		return nil, err
	}
	return r.GetAsset(ctx, a.UserID, a.ImageID)
}

func (r *imageStudioGalleryRepository) MarkAssetReady(ctx context.Context, userID int64, imageID string) error {
	result, err := r.db.ExecContext(ctx, `UPDATE image_studio_gallery_assets SET state='ready',updated_at=NOW()
        WHERE user_id=$1 AND image_id=$2 AND state IN ('uploading','ready')`, userID, imageID)
	if err != nil {
		return err
	}
	count, err := result.RowsAffected()
	if err != nil {
		return err
	}
	if count == 0 {
		return service.ErrGalleryNotFound
	}
	return nil
}

func (r *imageStudioGalleryRepository) SaveTask(ctx context.Context, userID int64, taskID, digest string, task json.RawMessage, links []service.GalleryTaskLink) (json.RawMessage, error) {
	return r.saveTask(ctx, userID, taskID, digest, task, links, false)
}

func (r *imageStudioGalleryRepository) SaveAcceptedTask(ctx context.Context, userID int64, taskID, digest string, task json.RawMessage, links []service.GalleryTaskLink) (json.RawMessage, error) {
	return r.saveTask(ctx, userID, taskID, digest, task, links, true)
}

func (r *imageStudioGalleryRepository) saveTask(ctx context.Context, userID int64, taskID, digest string, task json.RawMessage, links []service.GalleryTaskLink, accepted bool) (json.RawMessage, error) {
	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return nil, err
	}
	defer func() { _ = tx.Rollback() }()
	if err := galleryLockUser(ctx, tx, userID); err != nil {
		return nil, err
	}
	if err := galleryRequireActiveUser(ctx, tx, userID); err != nil {
		return nil, err
	}
	var stored []byte
	var previous string
	var deletedAt sql.NullTime
	err = tx.QueryRowContext(ctx, `SELECT task_json,sha256,deleted_at FROM image_studio_gallery_tasks
        WHERE user_id=$1 AND task_id=$2 FOR UPDATE`, userID, taskID).Scan(&stored, &previous, &deletedAt)
	if err == nil {
		if deletedAt.Valid {
			return nil, service.ErrGalleryGone
		}
		if previous != digest {
			return nil, service.ErrGalleryConflict
		}
		return json.RawMessage(stored), tx.Commit()
	}
	if !errors.Is(err, sql.ErrNoRows) {
		return nil, err
	}
	var count int64
	if err := tx.QueryRowContext(ctx, `SELECT COUNT(*) FROM image_studio_gallery_tasks WHERE user_id=$1 AND deleted_at IS NULL`, userID).Scan(&count); err != nil {
		return nil, err
	}
	var activeJobs int64
	if err := tx.QueryRowContext(ctx, `SELECT COUNT(*) FROM image_studio_jobs
        WHERE user_id=$1 AND deleted_at IS NULL AND status IN ('queued','generating','saving')`, userID).Scan(&activeJobs); err != nil {
		return nil, err
	}
	if accepted {
		var current bool
		if err := tx.QueryRowContext(ctx, `SELECT EXISTS(SELECT 1 FROM image_studio_jobs
            WHERE user_id=$1 AND task_id=$2 AND deleted_at IS NULL
              AND status IN ('queued','generating','saving'))`, userID, taskID).Scan(&current); err != nil {
			return nil, err
		}
		if !current {
			return nil, service.ErrGalleryGone
		}
		activeJobs--
	}
	if count+activeJobs >= galleryMaxUserTasks {
		return nil, service.ErrGalleryQuota
	}
	_, err = tx.ExecContext(ctx, `INSERT INTO image_studio_gallery_tasks (user_id,task_id,task_json,sha256)
        VALUES ($1,$2,$3::jsonb,$4)`, userID, taskID, string(task), digest)
	if err != nil {
		return nil, err
	}
	for _, link := range links {
		var state string
		err = tx.QueryRowContext(ctx, `SELECT state FROM image_studio_gallery_assets
            WHERE user_id=$1 AND image_id=$2 FOR SHARE`, userID, link.ImageID).Scan(&state)
		if errors.Is(err, sql.ErrNoRows) {
			return nil, service.ErrGalleryNotFound
		}
		if err != nil {
			return nil, err
		}
		if state != "ready" {
			return nil, service.ErrGalleryNotFound
		}
		_, err = tx.ExecContext(ctx, `INSERT INTO image_studio_gallery_task_assets
            (user_id,task_id,image_id,role,ordinal) VALUES ($1,$2,$3,$4,$5)`,
			userID, taskID, link.ImageID, link.Role, link.Ordinal)
		if err != nil {
			return nil, err
		}
	}
	return task, tx.Commit()
}

func (r *imageStudioGalleryRepository) GetTask(ctx context.Context, userID int64, taskID string) (json.RawMessage, error) {
	var raw []byte
	err := r.db.QueryRowContext(ctx, `SELECT task_json FROM image_studio_gallery_tasks
        WHERE user_id=$1 AND task_id=$2 AND deleted_at IS NULL`, userID, taskID).Scan(&raw)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, service.ErrGalleryNotFound
	}
	return json.RawMessage(raw), err
}

type galleryCursor struct {
	CreatedAt time.Time `json:"created_at"`
	TaskID    string    `json:"task_id"`
}

func (r *imageStudioGalleryRepository) ListTasks(ctx context.Context, userID int64, cursor string, limit int) (service.GalleryTaskPage, error) {
	if limit <= 0 {
		limit = 20
	}
	if limit > 100 {
		limit = 100
	}
	var at any
	taskID := ""
	if cursor != "" {
		decoded, err := base64.RawURLEncoding.DecodeString(cursor)
		if err != nil || len(decoded) > 512 {
			return service.GalleryTaskPage{}, service.ErrGalleryInvalid
		}
		var position galleryCursor
		if json.Unmarshal(decoded, &position) != nil || position.CreatedAt.IsZero() || position.TaskID == "" {
			return service.GalleryTaskPage{}, service.ErrGalleryInvalid
		}
		at, taskID = position.CreatedAt, position.TaskID
	}
	rows, err := r.db.QueryContext(ctx, `SELECT task_json,created_at,task_id FROM image_studio_gallery_tasks
        WHERE user_id=$1 AND deleted_at IS NULL AND
          ($2::timestamptz IS NULL OR (created_at,task_id) < ($2::timestamptz,$3))
        ORDER BY created_at DESC,task_id DESC LIMIT $4`, userID, at, taskID, limit+1)
	if err != nil {
		return service.GalleryTaskPage{}, err
	}
	defer func() { _ = rows.Close() }()
	page := service.GalleryTaskPage{Items: make([]json.RawMessage, 0, limit)}
	var last galleryCursor
	hasMore := false
	for rows.Next() {
		var raw []byte
		var position galleryCursor
		if err := rows.Scan(&raw, &position.CreatedAt, &position.TaskID); err != nil {
			return service.GalleryTaskPage{}, err
		}
		if len(page.Items) == limit {
			hasMore = true
			break
		}
		page.Items = append(page.Items, json.RawMessage(raw))
		last = position
	}
	if err := rows.Err(); err != nil {
		return service.GalleryTaskPage{}, err
	}
	// Determine whether a row beyond the returned limit was observed without
	// issuing a second query. Query order is stable under the compound cursor.
	if hasMore {
		encoded, _ := json.Marshal(last)
		next := base64.RawURLEncoding.EncodeToString(encoded)
		page.NextCursor = &next
	}
	return page, nil
}

func (r *imageStudioGalleryRepository) DeleteTask(ctx context.Context, userID int64, taskID string) ([]service.GalleryAsset, error) {
	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return nil, err
	}
	defer func() { _ = tx.Rollback() }()
	if err := galleryLockUser(ctx, tx, userID); err != nil {
		return nil, err
	}
	var exists string
	var deletedAt sql.NullTime
	err = tx.QueryRowContext(ctx, `SELECT task_id,deleted_at FROM image_studio_gallery_tasks
        WHERE user_id=$1 AND task_id=$2 FOR UPDATE`, userID, taskID).Scan(&exists, &deletedAt)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	if deletedAt.Valid {
		return nil, tx.Commit()
	}
	rows, err := tx.QueryContext(ctx, `SELECT DISTINCT image_id FROM image_studio_gallery_task_assets
        WHERE user_id=$1 AND task_id=$2`, userID, taskID)
	if err != nil {
		return nil, err
	}
	var ids []string
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			_ = rows.Close()
			return nil, err
		}
		ids = append(ids, id)
	}
	if err := rows.Err(); err != nil {
		_ = rows.Close()
		return nil, err
	}
	_ = rows.Close()
	if _, err := tx.ExecContext(ctx, `DELETE FROM image_studio_gallery_task_assets WHERE user_id=$1 AND task_id=$2`, userID, taskID); err != nil {
		return nil, err
	}
	if _, err := tx.ExecContext(ctx, `UPDATE image_studio_gallery_tasks SET deleted_at=NOW(),task_json='{}'::jsonb
        WHERE user_id=$1 AND task_id=$2`, userID, taskID); err != nil {
		return nil, err
	}
	var assets []service.GalleryAsset
	for _, id := range ids {
		a, err := scanGalleryAsset(tx.QueryRowContext(ctx, `UPDATE image_studio_gallery_assets a
            SET state='deleting', updated_at=NOW() WHERE user_id=$1 AND image_id=$2
            AND NOT EXISTS (SELECT 1 FROM image_studio_gallery_task_assets l
                WHERE l.user_id=a.user_id AND l.image_id=a.image_id)
            AND NOT EXISTS (SELECT 1 FROM image_studio_job_inputs j
                WHERE j.user_id=a.user_id AND j.image_id=a.image_id)
            RETURNING `+galleryAssetUpdatedColumns, userID, id))
		if errors.Is(err, sql.ErrNoRows) {
			continue
		}
		if err != nil {
			return nil, err
		}
		assets = append(assets, *a)
	}
	return assets, tx.Commit()
}

func (r *imageStudioGalleryRepository) ListCleanupAssets(ctx context.Context, limit int) ([]service.GalleryAsset, error) {
	if limit <= 0 || limit > 100 {
		limit = 100
	}
	rows, err := r.db.QueryContext(ctx, `SELECT a.user_id,a.image_id FROM image_studio_gallery_assets a
        WHERE (a.state='deleting' OR a.created_at < NOW()-INTERVAL '24 hours')
          AND NOT EXISTS (SELECT 1 FROM image_studio_gallery_task_assets l
            WHERE l.user_id=a.user_id AND l.image_id=a.image_id)
          AND NOT EXISTS (SELECT 1 FROM image_studio_job_inputs j
            WHERE j.user_id=a.user_id AND j.image_id=a.image_id)
        ORDER BY a.created_at LIMIT $1`, limit)
	if err != nil {
		return nil, err
	}
	type candidate struct {
		userID  int64
		imageID string
	}
	var candidates []candidate
	for rows.Next() {
		var next candidate
		if err := rows.Scan(&next.userID, &next.imageID); err != nil {
			_ = rows.Close()
			return nil, err
		}
		candidates = append(candidates, next)
	}
	if err := rows.Err(); err != nil {
		_ = rows.Close()
		return nil, err
	}
	_ = rows.Close()
	var assets []service.GalleryAsset
	for _, next := range candidates {
		tx, err := r.db.BeginTx(ctx, nil)
		if err != nil {
			return nil, err
		}
		if err := galleryLockUser(ctx, tx, next.userID); err != nil {
			_ = tx.Rollback()
			return nil, err
		}
		asset, err := scanGalleryAsset(tx.QueryRowContext(ctx, `UPDATE image_studio_gallery_assets a
            SET state='deleting',updated_at=NOW() WHERE a.user_id=$1 AND a.image_id=$2
            AND (a.state='deleting' OR a.created_at < NOW()-INTERVAL '24 hours')
            AND NOT EXISTS (SELECT 1 FROM image_studio_gallery_task_assets l
              WHERE l.user_id=a.user_id AND l.image_id=a.image_id)
            AND NOT EXISTS (SELECT 1 FROM image_studio_job_inputs j
              WHERE j.user_id=a.user_id AND j.image_id=a.image_id)
            RETURNING `+galleryAssetUpdatedColumns, next.userID, next.imageID))
		if errors.Is(err, sql.ErrNoRows) {
			_ = tx.Rollback()
			continue
		}
		if err != nil {
			_ = tx.Rollback()
			return nil, err
		}
		if err := tx.Commit(); err != nil {
			return nil, err
		}
		assets = append(assets, *asset)
	}
	return assets, nil
}

func (r *imageStudioGalleryRepository) FinalizeAssetDelete(ctx context.Context, userID int64, imageID string) error {
	_, err := r.db.ExecContext(ctx, `DELETE FROM image_studio_gallery_assets a
        WHERE a.user_id=$1 AND a.image_id=$2 AND a.state='deleting'
        AND NOT EXISTS (SELECT 1 FROM image_studio_gallery_task_assets l
          WHERE l.user_id=a.user_id AND l.image_id=a.image_id)
        AND NOT EXISTS (SELECT 1 FROM image_studio_job_inputs j
          WHERE j.user_id=a.user_id AND j.image_id=a.image_id)`, userID, imageID)
	return err
}
