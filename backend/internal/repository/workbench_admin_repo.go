package repository

import (
	"context"
	"database/sql"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/service"
)

type workbenchAdminRepository struct{ db *sql.DB }

func NewWorkbenchAdminRepository(db *sql.DB) service.WorkbenchAdminRepository {
	return &workbenchAdminRepository{db: db}
}

func (r *workbenchAdminRepository) Overview(ctx context.Context, since, until time.Time) (*service.WorkbenchOverview, error) {
	tx, err := r.db.BeginTx(ctx, &sql.TxOptions{ReadOnly: true, Isolation: sql.LevelRepeatableRead})
	if err != nil {
		return nil, err
	}
	defer func() { _ = tx.Rollback() }()
	result := &service.WorkbenchOverview{}
	err = tx.QueryRowContext(ctx, `SELECT COUNT(*),
        COUNT(*) FILTER (WHERE j.status='queued'), COUNT(*) FILTER (WHERE j.status='generating'),
        COUNT(*) FILTER (WHERE j.status='saving'), COUNT(*) FILTER (WHERE j.status='completed'),
        COUNT(*) FILTER (WHERE j.status='partial'), COUNT(*) FILTER (WHERE j.status='failed'),
        COUNT(*) FILTER (WHERE j.status='unknown'), COUNT(*) FILTER (WHERE j.status='cancelled')
        FROM image_studio_jobs j JOIN users u ON u.id=j.user_id
        WHERE j.deleted_at IS NULL AND u.deleted_at IS NULL AND j.created_at >= $1 AND j.created_at < $2`, since, until).
		Scan(&result.Tasks.Total, &result.Tasks.Queued, &result.Tasks.Generating, &result.Tasks.Saving, &result.Tasks.Completed, &result.Tasks.Partial, &result.Tasks.Failed, &result.Tasks.Unknown, &result.Tasks.Cancelled)
	if err != nil {
		return nil, err
	}
	err = tx.QueryRowContext(ctx, `SELECT COUNT(*),
        COUNT(*) FILTER (WHERE c.status='completed'), COUNT(*) FILTER (WHERE c.status='failed'),
        COUNT(*) FILTER (WHERE c.status='unknown'), COUNT(*) FILTER (WHERE c.status='cancelled'),
        COUNT(*) FILTER (WHERE c.billing_state='pending'), COUNT(*) FILTER (WHERE c.billing_state='unknown'),
        COALESCE(SUM(jsonb_array_length(c.output_image_ids)),0)
        FROM image_studio_job_calls c JOIN image_studio_jobs j ON j.user_id=c.user_id AND j.task_id=c.task_id
        JOIN users u ON u.id=j.user_id
        WHERE j.deleted_at IS NULL AND u.deleted_at IS NULL AND j.created_at >= $1 AND j.created_at < $2`, since, until).
		Scan(&result.Calls.Total, &result.Calls.Completed, &result.Calls.Failed, &result.Calls.Unknown, &result.Calls.Cancelled, &result.Calls.BillingPending, &result.Calls.BillingUnknown, &result.OutputImages)
	if err != nil {
		return nil, err
	}
	// Queue and unresolved results are current snapshots, not windowed totals.
	err = tx.QueryRowContext(ctx, `SELECT COUNT(*) FILTER (WHERE j.status='queued'),
        COUNT(*) FILTER (WHERE j.status='generating'), COUNT(*) FILTER (WHERE j.status='saving'),
        COUNT(*) FILTER (WHERE j.status='unknown')
        FROM image_studio_jobs j JOIN users u ON u.id=j.user_id
        WHERE j.deleted_at IS NULL AND u.deleted_at IS NULL`).Scan(&result.Queue.Queued, &result.Queue.Generating, &result.Queue.Saving, &result.Queue.Unknown)
	if err != nil {
		return nil, err
	}
	// Match the existing quota's asset-byte policy: uploading/deleting assets
	// occupy capacity until cleanup has actually removed their database row.
	err = tx.QueryRowContext(ctx, `SELECT
        (SELECT COUNT(*) FROM image_studio_gallery_tasks t JOIN users u ON u.id=t.user_id
          WHERE t.deleted_at IS NULL AND u.deleted_at IS NULL),
        (SELECT COUNT(*) FROM image_studio_gallery_task_assets l
          JOIN image_studio_gallery_tasks t ON t.user_id=l.user_id AND t.task_id=l.task_id
          JOIN users u ON u.id=t.user_id
          WHERE l.role='output' AND t.deleted_at IS NULL AND u.deleted_at IS NULL),
        (SELECT COUNT(*) FROM image_studio_gallery_assets a JOIN users u ON u.id=a.user_id WHERE u.deleted_at IS NULL),
        (SELECT COALESCE(SUM(a.byte_size+a.preview_byte_size+a.thumbnail_byte_size),0)
          FROM image_studio_gallery_assets a JOIN users u ON u.id=a.user_id WHERE u.deleted_at IS NULL)`).
		Scan(&result.Gallery.Tasks, &result.Gallery.OutputImages, &result.Gallery.Assets, &result.Gallery.UsedBytes)
	if err != nil {
		return nil, err
	}
	return result, tx.Commit()
}

func (r *workbenchAdminRepository) ListTasks(ctx context.Context, f service.WorkbenchTaskFilter) (*service.WorkbenchTaskPage, error) {
	tx, err := r.db.BeginTx(ctx, &sql.TxOptions{ReadOnly: true, Isolation: sql.LevelRepeatableRead})
	if err != nil {
		return nil, err
	}
	defer func() { _ = tx.Rollback() }()
	var since any
	if f.Days > 0 {
		since = time.Now().UTC().AddDate(0, 0, -f.Days)
	}
	const where = ` FROM image_studio_jobs j JOIN users u ON u.id=j.user_id
        WHERE j.deleted_at IS NULL AND u.deleted_at IS NULL
          AND ($1::text='' OR j.status=$1) AND ($2::bigint=0 OR j.user_id=$2)
          AND ($3::text='' OR j.model=$3) AND ($4::timestamptz IS NULL OR j.created_at>=$4)`
	result := &service.WorkbenchTaskPage{Items: []service.WorkbenchTaskSummary{}, Page: f.Page, PageSize: f.PageSize}
	if err := tx.QueryRowContext(ctx, `SELECT COUNT(*)`+where, f.Status, f.UserID, f.Model, since).Scan(&result.Total); err != nil {
		return nil, err
	}
	result.Pages = (result.Total + int64(f.PageSize) - 1) / int64(f.PageSize)
	rows, err := tx.QueryContext(ctx, `WITH selected AS (
        SELECT j.user_id,j.task_id,u.email,j.model,j.protocol,j.status,
          COALESCE((j.request_json->>'image_count')::integer,0) AS image_count,
          j.created_at,j.updated_at,j.finished_at`+where+`
        ORDER BY j.created_at DESC,j.task_id DESC,j.user_id DESC LIMIT $5 OFFSET $6)
        SELECT j.task_id,j.user_id,j.email,j.model,j.protocol,j.status,j.image_count,
          COALESCE(c.outputs,0),COALESCE(c.calls,0),COALESCE(c.billing_pending,0),COALESCE(c.billing_unknown,0),
          j.created_at,j.updated_at,j.finished_at,
          CASE WHEN j.finished_at IS NULL THEN NULL ELSE GREATEST(0,(EXTRACT(EPOCH FROM (j.finished_at-j.created_at))*1000)::bigint) END,
          COALESCE(c.error_code,'')
        FROM selected j LEFT JOIN LATERAL (
          SELECT COUNT(*) AS calls,COALESCE(SUM(jsonb_array_length(output_image_ids)),0) AS outputs,
            COUNT(*) FILTER (WHERE billing_state='pending') AS billing_pending,
            COUNT(*) FILTER (WHERE billing_state='unknown') AS billing_unknown,
            (ARRAY_AGG(error_code ORDER BY ordinal) FILTER (WHERE error_code IS NOT NULL AND error_code<>''))[1] AS error_code
          FROM image_studio_job_calls WHERE user_id=j.user_id AND task_id=j.task_id
        ) c ON TRUE ORDER BY j.created_at DESC,j.task_id DESC,j.user_id DESC`, f.Status, f.UserID, f.Model, since, f.PageSize, (f.Page-1)*f.PageSize)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()
	for rows.Next() {
		var item service.WorkbenchTaskSummary
		if err := rows.Scan(&item.TaskID, &item.UserID, &item.UserEmail, &item.Model, &item.Protocol, &item.Status, &item.ImageCount,
			&item.OutputCount, &item.CallCount, &item.BillingPending, &item.BillingUnknown, &item.CreatedAt, &item.UpdatedAt, &item.FinishedAt, &item.ElapsedMS, &item.ErrorCode); err != nil {
			return nil, err
		}
		result.Items = append(result.Items, item)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	if err := rows.Close(); err != nil {
		return nil, err
	}
	return result, tx.Commit()
}
