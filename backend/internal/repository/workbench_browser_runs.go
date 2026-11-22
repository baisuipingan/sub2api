package repository

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/google/uuid"
	"github.com/lib/pq"
)

func (r *officeAgentRepository) ClaimBrowserStep(ctx context.Context, userID int64, id string, duration time.Duration) (*service.OfficeRunLease, *service.OfficeRun, error) {
	if !officeUUID(id) || !officeLeaseValidDuration(duration) {
		return nil, nil, service.ErrOfficeInvalid
	}
	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return nil, nil, err
	}
	defer func() { _ = tx.Rollback() }()

	var occupied bool
	run, err := scanOfficeRun(tx.QueryRowContext(ctx, `SELECT `+officeRunColumns+`,COALESCE(lease_until>clock_timestamp(),false)
 FROM office_agent_runs WHERE id=$1 AND user_id=$2 AND browser_driven FOR UPDATE`, id, userID), &occupied)
	if err != nil {
		return nil, nil, err
	}
	if run.Status != service.OfficeRunQueued && run.Status != service.OfficeRunRunning {
		return nil, run, tx.Commit()
	}
	if occupied {
		return nil, nil, service.ErrOfficeStepBusy
	}
	lease := &service.OfficeRunLease{}
	current, err := scanOfficeRun(tx.QueryRowContext(ctx, `UPDATE office_agent_runs SET status='running',
 lease_owner='browser',lease_token=$2,lease_epoch=lease_epoch+1,lease_until=clock_timestamp()+($3*INTERVAL '1 millisecond'),
 started_at=COALESCE(started_at,NOW()),updated_at=NOW() WHERE id=$1 RETURNING `+officeRunColumns+
		`,lease_owner,lease_token::text,lease_epoch,lease_until`, id, uuid.NewString(), duration.Milliseconds()),
		&lease.Owner, &lease.Token, &lease.Epoch, &lease.Until)
	if err != nil {
		return nil, nil, err
	}
	lease.Run = *current
	// An expired lease can be reclaimed. Any operation that had already been
	// dispatched is resolved by the operation journal, never sent again.
	if run.Status == service.OfficeRunQueued {
		if _, err := appendOfficeEvent(ctx, tx, userID, id, "system:browser-start", "run.running", json.RawMessage(`{}`)); err != nil {
			return nil, nil, err
		}
	}
	return lease, current, tx.Commit()
}

func (r *officeAgentRepository) ReleaseBrowserStep(ctx context.Context, lease service.OfficeRunLease) error {
	_, err := r.db.ExecContext(ctx, `UPDATE office_agent_runs SET lease_until=NULL,lease_owner=NULL,lease_token=NULL
 WHERE id=$1 AND user_id=$2 AND browser_driven AND lease_epoch=$3 AND lease_token=$4`, lease.Run.ID, lease.Run.UserID, lease.Epoch, lease.Token)
	return err
}

// ResolveBrowserDispatch transitions a step left in flight after its lease
// expired. The recorded dispatcher can still deposit a late original result.
func (r *officeAgentRepository) ResolveBrowserDispatch(ctx context.Context, lease service.OfficeRunLease, operationID string) error {
	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback() }()
	run, err := checkOfficeLease(ctx, tx, lease)
	if err != nil {
		return err
	}
	if strings.HasPrefix(operationID, "browser:tool:") {
		digest := sha256.Sum256([]byte(run.ID + ":" + operationID))
		publication := fmt.Sprintf("docx:%x", digest)
		otherPublication := "tool:" + hex.EncodeToString(digest[:])
		artifact, err := scanOfficeResource(tx.QueryRowContext(ctx, `SELECT `+officeResourceColumns+`
 FROM office_resource_versions v JOIN office_resources r ON r.id=v.resource_id
 WHERE v.user_id=$1 AND v.run_id=$2 AND v.publication_id IN ($3,$4) AND v.state='ready'`, run.UserID, run.ID, publication, otherPublication))
		if err == nil {
			artifact.Role = "output"
			output, err := json.Marshal(service.WorkbenchDOCXEditResult{Artifact: artifact, Verified: true})
			if err != nil {
				return err
			}
			result, err := json.Marshal(service.OfficeOperationResult{Output: output})
			if err != nil {
				return err
			}
			if _, err := tx.ExecContext(ctx, `UPDATE office_agent_operations SET state='completed',result_json=$3,completed_at=NOW()
 WHERE run_id=$1 AND operation_id=$2 AND state IN ('dispatched','unknown')`, run.ID, operationID, result); err != nil {
				return err
			}
			return tx.Commit()
		}
		if !errors.Is(err, service.ErrOfficeNotFound) {
			return err
		}
	}
	var state string
	err = tx.QueryRowContext(ctx, `UPDATE office_agent_operations SET state='unknown'
 WHERE run_id=$1 AND operation_id=$2 AND state IN ('dispatched','unknown') RETURNING state`, run.ID, operationID).Scan(&state)
	if errors.Is(err, sql.ErrNoRows) {
		// The dispatch may have completed while we were acquiring the lock.
		return service.ErrOfficeStepBusy
	}
	if err != nil {
		return officeDBError(err)
	}
	const message = "上次调用已发出，结果尚未确认。你可以发一条新消息继续。"
	if _, err = tx.ExecContext(ctx, `UPDATE office_agent_runs SET status='failed',error_code='OPERATION_RESULT_UNKNOWN',
 error_message=$2,finished_at=NOW(),updated_at=NOW(),lease_token=NULL,lease_until=NULL,lease_owner=NULL WHERE id=$1`, run.ID, message); err != nil {
		return err
	}
	payload, _ := json.Marshal(map[string]string{"error_code": "OPERATION_RESULT_UNKNOWN", "error_message": message})
	if _, err = appendOfficeEvent(ctx, tx, run.UserID, run.ID, "system:browser-unknown", "run.failed", payload); err != nil {
		return err
	}
	return tx.Commit()
}

func (r *officeAgentRepository) BrowserOperations(ctx context.Context, userID int64, id string) ([]service.OfficeOperation, error) {
	rows, err := r.db.QueryContext(ctx, `SELECT `+officeOperationColumns+` FROM office_agent_operations WHERE user_id=$1 AND run_id=$2 ORDER BY created_at,operation_id`, userID, id)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()
	var operations []service.OfficeOperation
	for rows.Next() {
		op, err := scanOfficeOperation(rows)
		if err != nil {
			return nil, err
		}
		operations = append(operations, *op)
	}
	return operations, rows.Err()
}

func (r *officeAgentRepository) BrowserResourceAllowed(ctx context.Context, run service.OfficeRun, versionID string) (bool, error) {
	if !officeUUID(versionID) {
		return false, nil
	}
	var allowed bool
	err := r.db.QueryRowContext(ctx, `SELECT EXISTS (SELECT 1 FROM office_resource_refs
 WHERE user_id=$1 AND conversation_id=$2 AND version_id=$3)`, run.UserID, run.ConversationID, versionID).Scan(&allowed)
	return allowed, err
}

func (r *officeAgentRepository) BrowserDeliveredArtifacts(ctx context.Context, userID int64, ids []string) (map[string][]service.OfficeResource, error) {
	runs := make([]service.OfficeRun, len(ids))
	for i, id := range ids {
		runs[i].ID = id
	}
	if err := hydrateOfficeRunArtifacts(ctx, r.db, userID, runs); err != nil {
		return nil, err
	}
	artifacts := make(map[string][]service.OfficeResource, len(ids))
	for _, run := range runs {
		if run.Artifacts != nil && len(*run.Artifacts) > 0 {
			artifacts[run.ID] = *run.Artifacts
		}
	}
	return artifacts, nil
}

func (r *officeAgentRepository) BrowserContextArtifacts(ctx context.Context, run service.OfficeRun, ids []string) (map[string][]service.OfficeResource, error) {
	artifacts := make(map[string][]service.OfficeResource, len(ids))
	if len(ids) == 0 {
		return artifacts, nil
	}
	// Versions pinned to the conversation can still be used when the user
	// removes them from their file library. Keep their references stable.
	rows, err := r.db.QueryContext(ctx, `
SELECT v.run_id::text,v.id::text,v.name FROM office_resource_versions v
JOIN office_resource_refs f ON f.user_id=v.user_id AND f.version_id=v.id
WHERE v.user_id=$1 AND f.conversation_id=$2 AND v.run_id=ANY($3::uuid[]) AND v.state='ready'
ORDER BY v.run_id,v.created_at,v.id`, run.UserID, run.ConversationID, pq.Array(ids))
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()
	for rows.Next() {
		var runID string
		var artifact service.OfficeResource
		if err := rows.Scan(&runID, &artifact.VersionID, &artifact.Name); err != nil {
			return nil, err
		}
		artifacts[runID] = append(artifacts[runID], artifact)
	}
	return artifacts, rows.Err()
}
