package repository

import (
	"context"
	"database/sql"

	"github.com/Wei-Shaw/sub2api/internal/service"
)

// Cleanup is a durable two-phase outbox: mark under the same owner lock used
// by admission, delete S3 objects, then remove metadata. Failed S3 deletes keep
// their targets/bytes reserved and are retried on the next pass.
func (r *officeResourcesRepository) ListCleanup(ctx context.Context, limit int) ([]service.OfficeResourceCleanup, error) {
	limit = officeLimit(limit)
	// Releasing expired conversation references must make progress even when
	// their resources remain visible in My Files and never enter the deletion
	// candidate set. Deleted conversations/users cannot admit new references.
	_, err := r.db.ExecContext(ctx, `DELETE FROM office_resource_refs f USING (
 SELECT f.user_id,f.version_id,f.conversation_id FROM office_resource_refs f
 JOIN office_agent_conversations c ON c.id=f.conversation_id JOIN users u ON u.id=f.user_id
 WHERE c.deleted_at IS NOT NULL OR u.deleted_at IS NOT NULL
 ORDER BY f.created_at,f.user_id,f.version_id,f.conversation_id LIMIT $1
 ) expired WHERE (f.user_id,f.version_id,f.conversation_id)=(expired.user_id,expired.version_id,expired.conversation_id)`, limit*4)
	if err != nil {
		return nil, err
	}
	rows, err := r.db.QueryContext(ctx, `SELECT r.user_id,r.id::text FROM office_resources r JOIN users u ON u.id=r.user_id WHERE
 (u.deleted_at IS NOT NULL OR r.deleted_at IS NOT NULL OR (r.latest_version_id IS NULL AND r.created_at<NOW()-INTERVAL '1 hour'))
 AND NOT EXISTS(SELECT 1 FROM office_resource_refs f JOIN office_resource_versions v ON v.id=f.version_id WHERE v.resource_id=r.id)
 AND (r.source<>'gallery' OR EXISTS(SELECT 1 FROM office_resource_versions v WHERE v.resource_id=r.id))
 ORDER BY r.created_at LIMIT $1`, limit)
	if err != nil {
		return nil, err
	}
	type candidate struct {
		user int64
		id   string
	}
	var candidates []candidate
	for rows.Next() {
		var c candidate
		if err = rows.Scan(&c.user, &c.id); err != nil {
			_ = rows.Close()
			return nil, err
		}
		candidates = append(candidates, c)
	}
	err = rows.Err()
	_ = rows.Close()
	if err != nil {
		return nil, err
	}
	result := []service.OfficeResourceCleanup{}
	for _, c := range candidates {
		tx, e := r.db.BeginTx(ctx, nil)
		if e != nil {
			return nil, e
		}
		if e = galleryLockUser(ctx, tx, c.user); e != nil {
			_ = tx.Rollback()
			return nil, e
		}
		var source string
		var eligible bool
		e = tx.QueryRowContext(ctx, `SELECT r.source,(u.deleted_at IS NOT NULL OR r.deleted_at IS NOT NULL OR (r.latest_version_id IS NULL AND r.created_at<NOW()-INTERVAL '1 hour')) AND NOT EXISTS(SELECT 1 FROM office_resource_refs f JOIN office_resource_versions v ON v.id=f.version_id WHERE v.resource_id=r.id) FROM office_resources r JOIN users u ON u.id=r.user_id WHERE r.user_id=$1 AND r.id=$2 FOR UPDATE OF r`, c.user, c.id).Scan(&source, &eligible)
		if e == sql.ErrNoRows {
			_ = tx.Rollback()
			continue
		}
		if e != nil {
			_ = tx.Rollback()
			return nil, e
		}
		if !eligible {
			_ = tx.Rollback()
			continue
		}
		_, e = tx.ExecContext(ctx, `UPDATE office_resources SET deleted_at=COALESCE(deleted_at,NOW()) WHERE id=$1`, c.id)
		if e != nil {
			_ = tx.Rollback()
			return nil, e
		}
		if source == "gallery" {
			_, e = tx.ExecContext(ctx, `DELETE FROM office_gallery_pins WHERE user_id=$1 AND resource_id=$2`, c.user, c.id)
			if e == nil {
				_, e = tx.ExecContext(ctx, `DELETE FROM office_resource_versions WHERE resource_id=$1`, c.id)
			}
			if e == nil {
				_, e = tx.ExecContext(ctx, `UPDATE office_resources SET latest_version_id=NULL WHERE id=$1`, c.id)
			}
			if e != nil {
				_ = tx.Rollback()
				return nil, e
			}
			if e = tx.Commit(); e != nil {
				return nil, e
			}
			continue
		}
		_, e = tx.ExecContext(ctx, `UPDATE office_resource_versions SET state='deleting',updated_at=NOW() WHERE resource_id=$1`, c.id)
		if e != nil {
			_ = tx.Rollback()
			return nil, e
		}
		vs, e := tx.QueryContext(ctx, `SELECT id::text,storage_endpoint,storage_region,storage_bucket,object_key,preview_key FROM office_resource_versions WHERE resource_id=$1 ORDER BY created_at DESC`, c.id)
		if e != nil {
			_ = tx.Rollback()
			return nil, e
		}
		var pending []service.OfficeResourceCleanup
		for vs.Next() {
			v := service.OfficeResourceCleanup{Kind: "version", UserID: c.user}
			var key, preview string
			if e = vs.Scan(&v.ID, &v.Endpoint, &v.Region, &v.Bucket, &key, &preview); e != nil {
				_ = vs.Close()
				_ = tx.Rollback()
				return nil, e
			}
			v.Keys = []string{key}
			if preview != "" {
				v.Keys = append(v.Keys, preview)
			}
			pending = append(pending, v)
		}
		e = vs.Err()
		_ = vs.Close()
		if e != nil {
			_ = tx.Rollback()
			return nil, e
		}
		if len(pending) == 0 {
			_, e = tx.ExecContext(ctx, `DELETE FROM office_resources r WHERE r.user_id=$1 AND r.id=$2 AND source<>'gallery' AND NOT EXISTS(SELECT 1 FROM office_resource_versions v WHERE v.resource_id=r.id)`, c.user, c.id)
			if e != nil {
				_ = tx.Rollback()
				return nil, e
			}
		}
		if e = tx.Commit(); e != nil {
			return nil, e
		}
		result = append(result, pending...)
	}
	// A cancelled/stale publication into an otherwise live resource is safe to
	// discard. No ready version ever points to a pending parent.
	_, err = r.db.ExecContext(ctx, `UPDATE office_resource_versions SET state='deleting',updated_at=NOW() WHERE state='pending' AND updated_at<NOW()-INTERVAL '1 hour'`)
	if err != nil {
		return nil, err
	}
	more, err := r.db.QueryContext(ctx, `SELECT v.id::text,v.user_id,v.storage_endpoint,v.storage_region,v.storage_bucket,v.object_key,v.preview_key FROM office_resource_versions v WHERE v.state='deleting' AND NOT EXISTS(SELECT 1 FROM office_resource_refs f WHERE f.version_id=v.id) ORDER BY v.created_at DESC LIMIT $1`, limit)
	if err != nil {
		return nil, err
	}
	seen := map[string]bool{}
	for _, v := range result {
		seen[v.ID] = true
	}
	for more.Next() {
		v := service.OfficeResourceCleanup{Kind: "version"}
		var key, preview string
		if err = more.Scan(&v.ID, &v.UserID, &v.Endpoint, &v.Region, &v.Bucket, &key, &preview); err != nil {
			_ = more.Close()
			return nil, err
		}
		if seen[v.ID] {
			continue
		}
		v.Keys = []string{key}
		if preview != "" {
			v.Keys = append(v.Keys, preview)
		}
		result = append(result, v)
	}
	err = more.Err()
	_ = more.Close()
	if err != nil {
		return nil, err
	}
	// All snapshots of an unfinished/uncertain run stay immutable and available
	// for checkpoint recovery. After termination, a seven-day grace period applies.
	_, err = r.db.ExecContext(ctx, `UPDATE office_workspace_snapshots s SET state='deleting',updated_at=NOW() FROM office_agent_runs r,users u WHERE r.id=s.run_id AND u.id=s.user_id AND (u.deleted_at IS NOT NULL OR (r.finished_at<NOW()-INTERVAL '7 days' AND r.status IN('completed','partially_completed','failed','cancelled'))) AND s.state<>'deleting'`)
	if err != nil {
		return nil, err
	}
	snaps, err := r.db.QueryContext(ctx, `SELECT id::text,user_id,storage_endpoint,storage_region,storage_bucket,object_key FROM office_workspace_snapshots WHERE state='deleting' ORDER BY created_at LIMIT $1`, limit)
	if err != nil {
		return nil, err
	}
	defer func() { _ = snaps.Close() }()
	for snaps.Next() {
		v := service.OfficeResourceCleanup{Kind: "snapshot"}
		var key string
		if err = snaps.Scan(&v.ID, &v.UserID, &v.Endpoint, &v.Region, &v.Bucket, &key); err != nil {
			return nil, err
		}
		v.Keys = []string{key}
		result = append(result, v)
	}
	return result, snaps.Err()
}

func (r *officeResourcesRepository) FinalizeCleanup(ctx context.Context, item service.OfficeResourceCleanup) error {
	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback() }()
	if err = galleryLockUser(ctx, tx, item.UserID); err != nil {
		return err
	}
	if item.Kind == "snapshot" {
		_, err = tx.ExecContext(ctx, `DELETE FROM office_workspace_snapshots WHERE id=$1 AND user_id=$2 AND state='deleting'`, item.ID, item.UserID)
	} else if item.Kind == "version" {
		// Child versions are removed first; an interrupted pass is retryable.
		_, err = tx.ExecContext(ctx, `DELETE FROM office_resource_versions v WHERE v.id=$1 AND v.user_id=$2 AND v.state='deleting' AND NOT EXISTS(SELECT 1 FROM office_resource_refs f WHERE f.version_id=v.id) AND NOT EXISTS(SELECT 1 FROM office_resource_versions child WHERE child.parent_version_id=v.id)`, item.ID, item.UserID)
		if err == nil {
			_, err = tx.ExecContext(ctx, `DELETE FROM office_resources r WHERE r.user_id=$1 AND source<>'gallery' AND NOT EXISTS(SELECT 1 FROM office_resource_versions v WHERE v.resource_id=r.id)`, item.UserID)
		}
	} else {
		return service.ErrOfficeInvalid
	}
	if err != nil {
		return err
	}
	return tx.Commit()
}
