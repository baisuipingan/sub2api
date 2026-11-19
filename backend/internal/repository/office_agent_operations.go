package repository

import (
	"bytes"
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"strings"

	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/google/uuid"
)

const officeOperationColumns = `operation_id,run_id::text,kind,state,input_hash,input_json,request_id,result_json,
 COALESCE(dispatch_epoch,0),COALESCE(dispatch_token::text,''),created_at,dispatched_at,completed_at`

func scanOfficeOperation(row officeScanner) (*service.OfficeOperation, error) {
	o := &service.OfficeOperation{}
	var result []byte
	if err := row.Scan(&o.ID, &o.RunID, &o.Kind, &o.State, &o.InputHash, &o.Input, &o.RequestID, &result, &o.DispatchEpoch, &o.DispatchToken, &o.CreatedAt, &o.DispatchedAt, &o.CompletedAt); err != nil {
		return nil, officeDBError(err)
	}
	if len(result) > 0 {
		if err := json.Unmarshal(result, &o.Result); err != nil {
			return nil, err
		}
	}
	return o, nil
}

func officeJSONEqual(a, b []byte) bool {
	normalize := func(data []byte) []byte {
		var v any
		d := json.NewDecoder(bytes.NewReader(data))
		d.UseNumber()
		if d.Decode(&v) != nil {
			return nil
		}
		result, _ := json.Marshal(v)
		return result
	}
	return bytes.Equal(normalize(a), normalize(b))
}

func (r *officeAgentRepository) PrepareOperation(ctx context.Context, lease service.OfficeRunLease, in service.OfficeOperationPrepare) (*service.OfficeOperation, bool, error) {
	if !officeToken.MatchString(in.ID) || !officeHash.MatchString(in.InputHash) || !officeToken.MatchString(in.RequestID) || len(in.RequestID) > 64 || !officeJSON(in.Input, service.OfficeMaxCompactionInputBytes) || (in.Kind != service.OfficeOperationModel && in.Kind != service.OfficeOperationTool) {
		return nil, false, service.ErrOfficeInvalid
	}
	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return nil, false, err
	}
	defer func() { _ = tx.Rollback() }()
	run, err := checkOfficeLease(ctx, tx, lease)
	if err != nil {
		return nil, false, err
	}
	existing, err := scanOfficeOperation(tx.QueryRowContext(ctx, `SELECT `+officeOperationColumns+` FROM office_agent_operations WHERE run_id=$1 AND operation_id=$2`, run.ID, in.ID))
	if err == nil {
		if existing.Kind != in.Kind || existing.InputHash != in.InputHash || existing.RequestID != in.RequestID || !officeJSONEqual(existing.Input, in.Input) {
			return nil, false, service.ErrOfficeConflict
		}
		return existing, false, tx.Commit()
	}
	if !errors.Is(err, service.ErrOfficeNotFound) {
		return nil, false, err
	}
	// Prepared operations consume the shared budget as well. A crashed caller
	// cannot avoid a limit by reserving operations without dispatching them.
	if in.Kind == service.OfficeOperationModel {
		if run.ModelCalls >= run.Limits.MaxModelCalls {
			return nil, false, service.ErrOfficeBudgetExceeded
		}
		if run.Limits.MaxChargeCredits > 0 {
			// Read the original settled ledger under the run admission lock. No
			// estimate or second charge counter is introduced. A completed inbox
			// was returned above, even if that paid call exhausted the cap.
			var spent int64
			var pending, unsettled bool
			err = tx.QueryRowContext(ctx, `SELECT
 COALESCE((SELECT SUM(u.charge_credits) FROM office_agent_operations op
 JOIN usage_logs u ON u.request_id='client:'||op.request_id AND u.user_id=op.user_id AND u.api_key_id=$3
 WHERE op.user_id=$1 AND op.run_id=$2 AND op.kind='model'),0),
 EXISTS(SELECT 1 FROM office_agent_operations WHERE user_id=$1 AND run_id=$2 AND kind='model' AND state IN ('prepared','dispatched','unknown')),
 EXISTS(SELECT 1 FROM office_agent_operations op WHERE op.user_id=$1 AND op.run_id=$2 AND op.kind='model' AND op.state='completed'
 AND (op.result_json->>'http_status')::integer BETWEEN 200 AND 399
 AND NOT EXISTS(SELECT 1 FROM usage_logs u WHERE u.user_id=op.user_id AND u.api_key_id=$3 AND u.request_id='client:'||op.request_id))`, run.UserID, run.ID, run.APIKeyID).Scan(&spent, &pending, &unsettled)
			if err != nil {
				return nil, false, err
			}
			if spent >= run.Limits.MaxChargeCredits {
				return nil, false, service.ErrOfficeBudgetExceeded
			}
			if unsettled {
				return nil, false, service.ErrOfficeSettlementPending
			}
			if pending {
				// One paid request can exceed the remaining credit budget because
				// the supplier settles afterwards; parallel calls may not multiply
				// that overrun. Stable operation IDs remain recoverable above.
				return nil, false, service.ErrOfficeCapacity
			}
		}
		_, err = tx.ExecContext(ctx, `UPDATE office_agent_runs SET model_calls=model_calls+1,updated_at=NOW() WHERE id=$1`, run.ID)
	} else {
		if run.ToolCalls >= run.Limits.MaxToolCalls {
			return nil, false, service.ErrOfficeBudgetExceeded
		}
		_, err = tx.ExecContext(ctx, `UPDATE office_agent_runs SET tool_calls=tool_calls+1,updated_at=NOW() WHERE id=$1`, run.ID)
	}
	if err != nil {
		return nil, false, err
	}
	op, err := scanOfficeOperation(tx.QueryRowContext(ctx, `INSERT INTO office_agent_operations(user_id,run_id,operation_id,kind,input_hash,input_json,request_id) VALUES($1,$2,$3,$4,$5,$6,$7) RETURNING `+officeOperationColumns, run.UserID, run.ID, in.ID, in.Kind, in.InputHash, []byte(in.Input), in.RequestID))
	if err != nil {
		return nil, false, err
	}
	return op, true, tx.Commit()
}

func (r *officeAgentRepository) DispatchOperation(ctx context.Context, lease service.OfficeRunLease, id string) (*service.OfficeOperation, error) {
	if !officeToken.MatchString(id) {
		return nil, service.ErrOfficeInvalid
	}
	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return nil, err
	}
	defer func() { _ = tx.Rollback() }()
	run, err := checkOfficeLease(ctx, tx, lease)
	if err != nil {
		return nil, err
	}
	op, err := scanOfficeOperation(tx.QueryRowContext(ctx, `SELECT `+officeOperationColumns+` FROM office_agent_operations WHERE run_id=$1 AND operation_id=$2 FOR UPDATE`, run.ID, id))
	if err != nil {
		return nil, err
	}
	if op.State != service.OfficeOperationPrepared {
		if op.State == service.OfficeOperationUnknown {
			return nil, service.ErrOfficeResultUnknown
		}
		return nil, service.ErrOfficeOperationDispatched
	}
	op, err = scanOfficeOperation(tx.QueryRowContext(ctx, `UPDATE office_agent_operations SET state='dispatched',dispatch_epoch=$3,dispatch_token=$4,dispatched_at=NOW() WHERE run_id=$1 AND operation_id=$2 RETURNING `+officeOperationColumns, run.ID, id, lease.Epoch, lease.Token))
	if err != nil {
		return nil, err
	}
	return op, tx.Commit()
}

func (r *officeAgentRepository) GetOperation(ctx context.Context, userID int64, runID, id string) (*service.OfficeOperation, error) {
	if !officeUUID(runID) || !officeToken.MatchString(id) {
		return nil, service.ErrOfficeNotFound
	}
	return scanOfficeOperation(r.db.QueryRowContext(ctx, `SELECT `+officeOperationColumns+` FROM office_agent_operations WHERE user_id=$1 AND run_id=$2 AND operation_id=$3`, userID, runID, id))
}

func (r *officeAgentRepository) RecordOperationResult(ctx context.Context, lease service.OfficeRunLease, id string, result service.OfficeOperationResult) (*service.OfficeOperation, error) {
	if !officeUUID(lease.Run.ID) || !officeUUID(lease.Token) || lease.Epoch <= 0 || !officeToken.MatchString(id) || len(result.ErrorCode) > 64 || len(result.ErrorMessage) > 4096 || !officeJSON(result.Output, service.OfficeMaxOperationResultBytes) {
		return nil, service.ErrOfficeInvalid
	}
	encoded, err := json.Marshal(result)
	if err != nil || len(encoded) > service.OfficeMaxOperationResultBytes {
		return nil, service.ErrOfficeInvalid
	}
	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return nil, err
	}
	defer func() { _ = tx.Rollback() }()
	// This lock serializes all run/operation state transitions in one order.
	// It intentionally does not require a live lease: an original dispatcher
	// may deposit the paid response after cancellation or recovery fencing.
	var userID int64
	if err = tx.QueryRowContext(ctx, `SELECT user_id FROM office_agent_runs WHERE id=$1 AND user_id=$2 FOR UPDATE`, lease.Run.ID, lease.Run.UserID).Scan(&userID); err != nil {
		return nil, officeDBError(err)
	}
	op, err := scanOfficeOperation(tx.QueryRowContext(ctx, `SELECT `+officeOperationColumns+` FROM office_agent_operations WHERE run_id=$1 AND operation_id=$2 FOR UPDATE`, lease.Run.ID, id))
	if err != nil {
		return nil, err
	}
	if op.DispatchEpoch != lease.Epoch || op.DispatchToken != lease.Token {
		return nil, service.ErrOfficeLeaseLost
	}
	if op.State == service.OfficeOperationCompleted {
		previous, _ := json.Marshal(op.Result)
		if !officeJSONEqual(previous, encoded) {
			return nil, service.ErrOfficeConflict
		}
		return op, tx.Commit()
	}
	if op.State != service.OfficeOperationDispatched && op.State != service.OfficeOperationUnknown {
		return nil, service.ErrOfficeConflict
	}
	op, err = scanOfficeOperation(tx.QueryRowContext(ctx, `UPDATE office_agent_operations SET state='completed',result_json=$3,completed_at=NOW() WHERE run_id=$1 AND operation_id=$2 RETURNING `+officeOperationColumns, lease.Run.ID, id, encoded))
	if err != nil {
		return nil, err
	}
	return op, tx.Commit()
}

func (r *officeAgentRepository) MarkOperationUnknown(ctx context.Context, lease service.OfficeRunLease, id, message string) error {
	if !officeToken.MatchString(id) || len(message) > 4096 {
		return service.ErrOfficeInvalid
	}
	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback() }()
	run, err := checkOfficeLease(ctx, tx, lease)
	if err != nil {
		return err
	}
	op, err := scanOfficeOperation(tx.QueryRowContext(ctx, `SELECT `+officeOperationColumns+` FROM office_agent_operations WHERE run_id=$1 AND operation_id=$2 FOR UPDATE`, run.ID, id))
	if err != nil {
		return err
	}
	if op.State != service.OfficeOperationDispatched || op.DispatchEpoch != lease.Epoch || op.DispatchToken != lease.Token {
		return service.ErrOfficeConflict
	}
	if _, err = tx.ExecContext(ctx, `UPDATE office_agent_operations SET state='unknown' WHERE run_id=$1 AND operation_id=$2`, run.ID, id); err != nil {
		return err
	}
	if _, err = tx.ExecContext(ctx, `UPDATE office_agent_runs SET status='needs_confirmation',error_code='OPERATION_RESULT_UNKNOWN',error_message=$2,lease_epoch=lease_epoch+1,lease_token=NULL,lease_until=NULL,lease_owner=NULL,updated_at=NOW() WHERE id=$1`, run.ID, message); err != nil {
		return err
	}
	payload, _ := json.Marshal(map[string]string{"operation_id": id, "code": "OPERATION_RESULT_UNKNOWN", "message": message})
	if _, err = appendOfficeEvent(ctx, tx, run.UserID, run.ID, "system:unknown:"+uuid.NewSHA1(uuid.NameSpaceOID, []byte(id)).String(), "run.needs_confirmation", payload); err != nil {
		return err
	}
	return tx.Commit()
}

func scanOfficeEvent(row officeScanner) (*service.OfficeRunEvent, error) {
	e := &service.OfficeRunEvent{}
	if err := row.Scan(&e.ID, &e.RunID, &e.Sequence, &e.Type, &e.Payload, &e.CreatedAt); err != nil {
		return nil, officeDBError(err)
	}
	return e, nil
}

func appendOfficeEvent(ctx context.Context, tx *sql.Tx, userID int64, runID, id, kind string, payload json.RawMessage) (*service.OfficeRunEvent, error) {
	existing, err := scanOfficeEvent(tx.QueryRowContext(ctx, `SELECT event_id,run_id::text,sequence,event_type,payload,created_at FROM office_agent_run_events WHERE run_id=$1 AND event_id=$2`, runID, id))
	if err == nil {
		if existing.Type != kind || !officeJSONEqual(existing.Payload, payload) {
			return nil, service.ErrOfficeConflict
		}
		return existing, nil
	}
	if !errors.Is(err, service.ErrOfficeNotFound) {
		return nil, err
	}
	var seq int64
	// Leave a small independent allowance for final/cancel/recovery events even
	// if a streaming client exhausts its public-event budget.
	maxEvents, maxBytes := 9900, 16_711_680
	if strings.HasPrefix(id, "system:") {
		maxEvents, maxBytes = 10000, 16_777_216
	}
	err = tx.QueryRowContext(ctx, `UPDATE office_agent_runs SET next_event_seq=next_event_seq+1,event_bytes=event_bytes+$2 WHERE id=$1 AND next_event_seq<$3 AND event_bytes+$2<=$4 RETURNING next_event_seq`, runID, len(payload), maxEvents, maxBytes).Scan(&seq)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, service.ErrOfficeBudgetExceeded
	}
	if err != nil {
		return nil, err
	}
	return scanOfficeEvent(tx.QueryRowContext(ctx, `INSERT INTO office_agent_run_events(user_id,run_id,event_id,sequence,event_type,payload) VALUES($1,$2,$3,$4,$5,$6) RETURNING event_id,run_id::text,sequence,event_type,payload,created_at`, userID, runID, id, seq, kind, []byte(payload)))
}

func (r *officeAgentRepository) AppendEvent(ctx context.Context, lease service.OfficeRunLease, id, kind string, payload json.RawMessage) (*service.OfficeRunEvent, error) {
	if !officeToken.MatchString(id) || strings.HasPrefix(id, "system:") || !officeToken.MatchString(kind) || len(kind) > 64 || !officeJSON(payload, service.OfficeMaxEventBytes) {
		return nil, service.ErrOfficeInvalid
	}
	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return nil, err
	}
	defer func() { _ = tx.Rollback() }()
	run, err := checkOfficeLease(ctx, tx, lease)
	if err != nil {
		return nil, err
	}
	event, err := appendOfficeEvent(ctx, tx, run.UserID, run.ID, id, kind, payload)
	if err != nil {
		return nil, err
	}
	return event, tx.Commit()
}

func (r *officeAgentRepository) ListEvents(ctx context.Context, userID int64, runID string, after int64, limit int) ([]service.OfficeRunEvent, error) {
	if _, err := r.GetRun(ctx, userID, runID); err != nil {
		return nil, err
	}
	rows, err := r.db.QueryContext(ctx, `SELECT event_id,run_id::text,sequence,event_type,payload,created_at FROM office_agent_run_events WHERE user_id=$1 AND run_id=$2 AND sequence>$3 ORDER BY sequence LIMIT $4`, userID, runID, after, officeLimit(limit))
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()
	events := []service.OfficeRunEvent{}
	for rows.Next() {
		event, err := scanOfficeEvent(rows)
		if err != nil {
			return nil, err
		}
		events = append(events, *event)
	}
	return events, rows.Err()
}
