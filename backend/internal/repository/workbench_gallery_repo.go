package repository

import (
	"context"
	"database/sql"
	"encoding/json"

	"github.com/Wei-Shaw/sub2api/internal/service"
)

func (r *workbenchAdminRepository) ListGalleryTasks(ctx context.Context, f service.WorkbenchGalleryFilter) (*service.WorkbenchGalleryPage, error) {
	tx, err := r.db.BeginTx(ctx, &sql.TxOptions{ReadOnly: true, Isolation: sql.LevelRepeatableRead})
	if err != nil {
		return nil, err
	}
	defer func() { _ = tx.Rollback() }()
	const where = ` FROM image_studio_gallery_tasks t JOIN users u ON u.id=t.user_id
        WHERE t.deleted_at IS NULL AND u.deleted_at IS NULL
          AND ($1::bigint=0 OR t.user_id=$1) AND ($2::text='' OR t.task_id=$2)
          AND EXISTS (SELECT 1 FROM image_studio_gallery_task_assets l
            JOIN image_studio_gallery_assets a ON a.user_id=l.user_id AND a.image_id=l.image_id
            WHERE l.user_id=t.user_id AND l.task_id=t.task_id AND l.role='output' AND a.state='ready')`
	page := &service.WorkbenchGalleryPage{Items: []service.WorkbenchGalleryTask{}, Page: f.Page, PageSize: f.PageSize}
	if err := tx.QueryRowContext(ctx, `SELECT COUNT(*)`+where, f.UserID, f.TaskID).Scan(&page.Total); err != nil {
		return nil, err
	}
	page.Pages = (page.Total + int64(f.PageSize) - 1) / int64(f.PageSize)
	rows, err := tx.QueryContext(ctx, `SELECT t.user_id,u.email,t.task_id,
          LEFT(COALESCE(NULLIF(t.task_json->>'apiModel',''),t.task_json->>'modelId',''),128),t.created_at,
          (SELECT jsonb_agg(l.image_id ORDER BY l.ordinal) FROM image_studio_gallery_task_assets l
            JOIN image_studio_gallery_assets a ON a.user_id=l.user_id AND a.image_id=l.image_id
            WHERE l.user_id=t.user_id AND l.task_id=t.task_id AND l.role='output' AND a.state='ready')`+where+`
        ORDER BY t.created_at DESC,t.task_id DESC,t.user_id DESC LIMIT $3 OFFSET $4`, f.UserID, f.TaskID, f.PageSize, (f.Page-1)*f.PageSize)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()
	for rows.Next() {
		var item service.WorkbenchGalleryTask
		var ids []byte
		if err := rows.Scan(&item.UserID, &item.UserEmail, &item.TaskID, &item.Model, &item.CreatedAt, &ids); err != nil {
			return nil, err
		}
		if err := json.Unmarshal(ids, &item.ImageIDs); err != nil {
			return nil, err
		}
		item.OutputCount = int64(len(item.ImageIDs))
		page.Items = append(page.Items, item)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	if err := rows.Close(); err != nil {
		return nil, err
	}
	return page, tx.Commit()
}

func (r *workbenchAdminRepository) GalleryOutputExists(ctx context.Context, userID int64, taskID, imageID string) (bool, error) {
	var exists bool
	err := r.db.QueryRowContext(ctx, `SELECT EXISTS(SELECT 1 FROM image_studio_gallery_task_assets l
        JOIN image_studio_gallery_tasks t ON t.user_id=l.user_id AND t.task_id=l.task_id
        JOIN image_studio_gallery_assets a ON a.user_id=l.user_id AND a.image_id=l.image_id
        JOIN users u ON u.id=t.user_id WHERE l.user_id=$1 AND l.task_id=$2 AND l.image_id=$3
        AND l.role='output' AND t.deleted_at IS NULL AND u.deleted_at IS NULL AND a.state='ready')`, userID, taskID, imageID).Scan(&exists)
	return exists, err
}
