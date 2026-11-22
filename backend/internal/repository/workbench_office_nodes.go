package repository

import (
	"context"
	"encoding/json"
	"errors"

	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/lib/pq"
)

const officeNodeColumns = `n.id::text,n.name,n.url,n.state,n.max_concurrent,n.token_encrypted,n.worker_identity,n.revision,n.health,n.checked_at,n.status,
    (SELECT COUNT(*) FROM workbench_office_dispatch d WHERE d.node_id=n.id AND d.state='active')`

func scanOfficeNode(row officeScanner) (service.OfficeNode, error) {
	var node service.OfficeNode
	var status []byte
	err := row.Scan(&node.ID, &node.Name, &node.URL, &node.State, &node.MaxConcurrent, &node.TokenEncrypted, &node.WorkerIdentity,
		&node.Revision, &node.Health, &node.LastCheckedAt, &status, &node.Assigned)
	if err != nil {
		var pg *pq.Error
		if errors.As(err, &pg) && pg.Code == "23505" {
			return node, service.ErrOfficeNodeDuplicate
		}
		return node, officeDBError(err)
	}
	if status != nil {
		if err := json.Unmarshal(status, &node.Status); err != nil {
			return node, err
		}
	}
	node.TokenConfigured = node.TokenEncrypted != ""
	return node, nil
}

func (r *officeAgentRepository) ListOfficeNodes(ctx context.Context) ([]service.OfficeNode, error) {
	rows, err := r.db.QueryContext(ctx, `SELECT `+officeNodeColumns+` FROM workbench_office_nodes n ORDER BY created_at,id`)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()
	nodes := make([]service.OfficeNode, 0)
	for rows.Next() {
		node, err := scanOfficeNode(rows)
		if err != nil {
			return nil, err
		}
		nodes = append(nodes, node)
	}
	return nodes, rows.Err()
}

func (r *officeAgentRepository) GetOfficeNode(ctx context.Context, id string) (service.OfficeNode, error) {
	return scanOfficeNode(r.db.QueryRowContext(ctx, `SELECT `+officeNodeColumns+` FROM workbench_office_nodes n WHERE n.id=$1`, id))
}

func (r *officeAgentRepository) CountWaitingOfficeNodes(ctx context.Context) (int, error) {
	var waiting int
	err := r.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM workbench_office_dispatch WHERE state='waiting' AND updated_at>=NOW()-INTERVAL '12 minutes'`).Scan(&waiting)
	return waiting, err
}

func officeNodeStatusJSON(status *service.OfficeNodeStatus) (any, error) {
	if status == nil {
		return nil, nil
	}
	data, err := json.Marshal(status)
	return string(data), err
}

func (r *officeAgentRepository) CreateOfficeNode(ctx context.Context, node service.OfficeNode) (service.OfficeNode, error) {
	status, err := officeNodeStatusJSON(node.Status)
	if err != nil {
		return service.OfficeNode{}, err
	}
	return scanOfficeNode(r.db.QueryRowContext(ctx, `INSERT INTO workbench_office_nodes AS n
        (id,name,url,state,max_concurrent,token_encrypted,worker_identity,health,checked_at,status)
        VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10::jsonb) RETURNING `+officeNodeColumns,
		node.ID, node.Name, node.URL, node.State, node.MaxConcurrent, node.TokenEncrypted, node.WorkerIdentity, node.Health, node.LastCheckedAt, status))
}

func (r *officeAgentRepository) UpdateOfficeNode(ctx context.Context, node service.OfficeNode) (service.OfficeNode, error) {
	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return service.OfficeNode{}, err
	}
	defer func() { _ = tx.Rollback() }()
	var currentURL, currentToken, currentIdentity string
	var currentRevision int64
	err = tx.QueryRowContext(ctx, `SELECT url,token_encrypted,worker_identity,revision FROM workbench_office_nodes WHERE id=$1 FOR UPDATE`, node.ID).
		Scan(&currentURL, &currentToken, &currentIdentity, &currentRevision)
	if err != nil {
		return service.OfficeNode{}, officeDBError(err)
	}
	if currentRevision != node.Revision || currentIdentity != "" && node.WorkerIdentity != currentIdentity {
		return service.OfficeNode{}, service.ErrOfficeConflict
	}
	connectionChanged := currentURL != node.URL || currentToken != node.TokenEncrypted
	if connectionChanged || node.State == service.OfficeNodeDisabled {
		var assigned bool
		if err = tx.QueryRowContext(ctx, `SELECT EXISTS(SELECT 1 FROM workbench_office_dispatch WHERE node_id=$1 AND state='active')`, node.ID).Scan(&assigned); err != nil {
			return service.OfficeNode{}, err
		}
		if assigned {
			return service.OfficeNode{}, service.ErrOfficeNodeBusy
		}
	}
	status, err := officeNodeStatusJSON(node.Status)
	if err != nil {
		return service.OfficeNode{}, err
	}
	result, err := scanOfficeNode(tx.QueryRowContext(ctx, `UPDATE workbench_office_nodes AS n
        SET name=$2,url=$3,state=$4,max_concurrent=$5,token_encrypted=$6,
            worker_identity=COALESCE(NULLIF($7,''),worker_identity),revision=nextval('workbench_office_node_revision_seq'),
            health=CASE WHEN $11 OR checked_at IS NULL OR checked_at <= $9 THEN $8 ELSE health END,
            checked_at=CASE WHEN $11 OR checked_at IS NULL OR checked_at <= $9 THEN $9 ELSE checked_at END,
            status=CASE WHEN $11 OR checked_at IS NULL OR checked_at <= $9 THEN $10::jsonb ELSE status END,
            updated_at=NOW() WHERE id=$1 RETURNING `+officeNodeColumns,
		node.ID, node.Name, node.URL, node.State, node.MaxConcurrent, node.TokenEncrypted, node.WorkerIdentity,
		node.Health, node.LastCheckedAt, status, connectionChanged))
	if err != nil {
		return service.OfficeNode{}, err
	}
	return result, tx.Commit()
}

func (r *officeAgentRepository) SaveOfficeNodeStatus(ctx context.Context, node service.OfficeNode) (service.OfficeNode, error) {
	status, err := officeNodeStatusJSON(node.Status)
	if err != nil {
		return service.OfficeNode{}, err
	}
	result, err := scanOfficeNode(r.db.QueryRowContext(ctx, `UPDATE workbench_office_nodes AS n
        SET worker_identity=COALESCE(NULLIF($3,''),worker_identity),health=$4,checked_at=$5,status=$6::jsonb
        WHERE id=$1 AND revision=$2 AND (worker_identity='' OR worker_identity=$3)
        AND (checked_at IS NULL OR checked_at <= $5) RETURNING `+officeNodeColumns,
		node.ID, node.Revision, node.WorkerIdentity, node.Health, node.LastCheckedAt, status))
	if errors.Is(err, service.ErrOfficeNotFound) {
		return r.GetOfficeNode(ctx, node.ID)
	}
	return result, err
}

func (r *officeAgentRepository) DeleteOfficeNode(ctx context.Context, id string) error {
	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback() }()
	var existing string
	if err = tx.QueryRowContext(ctx, `SELECT id::text FROM workbench_office_nodes WHERE id=$1 FOR UPDATE`, id).Scan(&existing); err != nil {
		return officeDBError(err)
	}
	var assigned bool
	if err = tx.QueryRowContext(ctx, `SELECT EXISTS(SELECT 1 FROM workbench_office_dispatch WHERE node_id=$1 AND state='active')`, id).Scan(&assigned); err != nil {
		return err
	}
	if assigned {
		return service.ErrOfficeNodeBusy
	}
	if _, err = tx.ExecContext(ctx, `DELETE FROM workbench_office_nodes WHERE id=$1`, id); err != nil {
		return officeDBError(err)
	}
	return tx.Commit()
}
