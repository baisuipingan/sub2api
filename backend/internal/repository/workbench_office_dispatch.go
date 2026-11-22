package repository

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/service"
)

const officeDispatchColumns = `job_key,user_id,COALESCE(run_id::text,''),COALESCE(version_id::text,''),
COALESCE(node_id::text,''),attempt,state,assigned_at,receipt`

func scanOfficeDispatch(row interface{ Scan(...any) error }) (*service.OfficeDispatch, error) {
	var item service.OfficeDispatch
	var receipt []byte
	err := row.Scan(&item.Key, &item.UserID, &item.RunID, &item.VersionID, &item.NodeID,
		&item.Attempt, &item.State, &item.AssignedAt, &receipt)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	item.Receipt = receipt
	return &item, err
}

func (r *officeAgentRepository) QueueOfficeDispatch(ctx context.Context, item service.OfficeDispatch) (*service.OfficeDispatch, error) {
	_, err := r.db.ExecContext(ctx, `INSERT INTO workbench_office_dispatch(job_key,user_id,run_id,version_id)
VALUES($1,$2,NULLIF($3,'')::uuid,NULLIF($4,'')::uuid) ON CONFLICT DO NOTHING`, item.Key, item.UserID, item.RunID, item.VersionID)
	if err != nil {
		return nil, err
	}
	return r.OfficeDispatch(ctx, item.Key)
}

func (r *officeAgentRepository) OfficeDispatch(ctx context.Context, key string) (*service.OfficeDispatch, error) {
	return scanOfficeDispatch(r.db.QueryRowContext(ctx, `SELECT `+officeDispatchColumns+` FROM workbench_office_dispatch WHERE job_key=$1`, key))
}

func (r *officeAgentRepository) RetryOfficePreview(ctx context.Context, item service.OfficeDispatch) (*service.OfficeDispatch, error) {
	reopened, err := scanOfficeDispatch(r.db.QueryRowContext(ctx, `UPDATE workbench_office_dispatch
SET state='waiting',assigned_at=NULL,next_check_at=NOW(),updated_at=NOW()
WHERE job_key=$1 AND attempt=$2 AND version_id IS NOT NULL AND state='cancelled' RETURNING `+officeDispatchColumns, item.Key, item.Attempt))
	if err != nil || reopened != nil {
		return reopened, err
	}
	return r.OfficeDispatch(ctx, item.Key)
}

func (r *officeAgentRepository) AssignOfficeDispatch(ctx context.Context, key, node string, capacity int) (*service.OfficeDispatch, error) {
	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return nil, err
	}
	defer func() { _ = tx.Rollback() }()

	var state string
	var cap int
	err = tx.QueryRowContext(ctx, `SELECT state,max_concurrent FROM workbench_office_nodes WHERE id=$1 FOR UPDATE`, node).Scan(&state, &cap)
	if errors.Is(err, sql.ErrNoRows) || err == nil && state != "active" {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	var active int
	if err = tx.QueryRowContext(ctx, `SELECT COUNT(*) FROM workbench_office_dispatch WHERE node_id=$1 AND state='active'`, node).Scan(&active); err != nil {
		return nil, err
	}
	if active >= min(cap, capacity) {
		return nil, nil
	}
	item, err := scanOfficeDispatch(tx.QueryRowContext(ctx, `UPDATE workbench_office_dispatch
SET node_id=$2, attempt=attempt+1, state='active', assigned_at=NOW(), next_check_at=NOW(), updated_at=NOW()
WHERE job_key=$1 AND state='waiting' RETURNING `+officeDispatchColumns, key, node))
	if err != nil {
		return nil, err
	}
	return item, tx.Commit()
}

func (r *officeAgentRepository) SaveOfficeDispatchReceipt(ctx context.Context, item service.OfficeDispatch, receipt json.RawMessage) error {
	result, err := r.db.ExecContext(ctx, `UPDATE workbench_office_dispatch SET receipt=$3,updated_at=NOW()
WHERE job_key=$1 AND attempt=$2 AND state='active' AND (receipt IS NULL OR receipt=$3)`, item.Key, item.Attempt, []byte(receipt))
	if err != nil {
		return err
	}
	rows, err := result.RowsAffected()
	if err == nil && rows == 0 {
		return service.ErrOfficeConflict
	}
	return err
}

func (r *officeAgentRepository) FinishOfficeDispatch(ctx context.Context, item service.OfficeDispatch, state string) error {
	_, err := r.db.ExecContext(ctx, `UPDATE workbench_office_dispatch
SET state=$3,receipt=NULL,updated_at=NOW()
WHERE job_key=$1 AND attempt=$2 AND state=$4 AND state IN ('active','waiting')`, item.Key, item.Attempt, state, item.State)
	return err
}

func (r *officeAgentRepository) ClaimOfficeDispatchChecks(ctx context.Context, limit int, delay time.Duration) ([]service.OfficeDispatch, error) {
	rows, err := r.db.QueryContext(ctx, `UPDATE workbench_office_dispatch SET next_check_at=NOW()+$1*INTERVAL '1 millisecond'
WHERE job_key IN (SELECT job_key FROM workbench_office_dispatch WHERE state='active' AND next_check_at<=NOW()
ORDER BY next_check_at LIMIT $2 FOR UPDATE SKIP LOCKED) RETURNING `+officeDispatchColumns, delay.Milliseconds(), limit)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()
	var out []service.OfficeDispatch
	for rows.Next() {
		item, err := scanOfficeDispatch(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, *item)
	}
	return out, rows.Err()
}

func (r *officeAgentRepository) CleanOfficeDispatch(ctx context.Context) error {
	_, err := r.db.ExecContext(ctx, `DELETE FROM workbench_office_dispatch WHERE job_key IN (
SELECT job_key FROM workbench_office_dispatch WHERE state<>'active' AND updated_at<NOW()-INTERVAL '7 days' LIMIT 500)`)
	return err
}
