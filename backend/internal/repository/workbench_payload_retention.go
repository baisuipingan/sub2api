package repository

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/service"
)

func (r *officeAgentRepository) SweepWorkbenchPayloads(ctx context.Context, staleBefore, terminalBefore time.Time, limit int) error {
	for range limit {
		found, err := r.expireStaleWorkbenchRun(ctx, staleBefore)
		if err != nil {
			return err
		}
		if !found {
			break
		}
	}
	for range limit {
		found, err := r.compactTerminalWorkbenchRun(ctx, terminalBefore)
		if err != nil {
			return err
		}
		if !found {
			break
		}
	}
	return r.cleanupWorkbenchReplayCheckpoints(ctx, terminalBefore, limit)
}

func (r *officeAgentRepository) expireStaleWorkbenchRun(ctx context.Context, cutoff time.Time) (bool, error) {
	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return false, err
	}
	defer func() { _ = tx.Rollback() }()
	var id string
	var userID int64
	err = tx.QueryRowContext(ctx, `SELECT id::text,user_id FROM office_agent_runs
		WHERE browser_driven AND status IN ('queued','running','recovering','waiting_user','needs_confirmation')
		AND updated_at < $1 AND (lease_until IS NULL OR lease_until <= clock_timestamp())
		ORDER BY updated_at,id LIMIT 1 FOR UPDATE SKIP LOCKED`, cutoff).Scan(&id, &userID)
	if errors.Is(err, sql.ErrNoRows) {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	const code = "WORKBENCH_RUN_EXPIRED"
	const message = "这轮对话长时间未继续，已结束。你可以发送新消息继续。"
	if _, err = tx.ExecContext(ctx, `UPDATE office_agent_runs SET status='failed',error_code=$2,error_message=$3,
		finished_at=NOW(),updated_at=NOW(),lease_epoch=lease_epoch+1,lease_token=NULL,lease_until=NULL,lease_owner=NULL WHERE id=$1`, id, code, message); err != nil {
		return false, err
	}
	if _, err = tx.ExecContext(ctx, `UPDATE office_agent_operations SET state='unknown' WHERE run_id=$1 AND state='dispatched'`, id); err != nil {
		return false, err
	}
	payload, _ := json.Marshal(map[string]string{"status": "failed", "error_code": code, "error_message": message})
	if _, err = appendOfficeEvent(ctx, tx, userID, id, "system:expired", "run.failed", payload); err != nil && !errors.Is(err, service.ErrOfficeBudgetExceeded) {
		return false, err
	}
	return true, tx.Commit()
}

func (r *officeAgentRepository) compactTerminalWorkbenchRun(ctx context.Context, cutoff time.Time) (bool, error) {
	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return false, err
	}
	defer func() { _ = tx.Rollback() }()
	var id string
	var userID int64
	var conversationID string
	err = tx.QueryRowContext(ctx, `SELECT r.id::text,r.user_id,r.conversation_id::text FROM office_agent_runs r
		WHERE r.finished_at < $1 AND r.payloads_compacted_at IS NULL
		AND r.status IN ('completed','partially_completed','failed','cancelled')
		AND NOT `+keepLegacyWorkbenchPayload+`
		AND (r.duplicate_payloads_compacted_at IS NULL OR NOT `+keepWorkbenchReplayPayload+`)
		ORDER BY r.finished_at,r.id LIMIT 1 FOR UPDATE OF r SKIP LOCKED`, cutoff).Scan(&id, &userID, &conversationID)
	if errors.Is(err, sql.ErrNoRows) {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	// Pin and answer selection share this lock. Skip it when an answer change
	// could be waiting for the run row already locked by the candidate scan.
	var owner int64
	if err = tx.QueryRowContext(ctx, `SELECT user_id FROM office_agent_conversations
		WHERE id=$1 AND user_id=$2 FOR UPDATE SKIP LOCKED`, conversationID, userID).Scan(&owner); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return false, nil
		}
		return false, err
	}
	// The candidate scan may have overlapped a new replay plan or admission.
	var kept, alreadyTrimmed, legacyLock bool
	err = tx.QueryRowContext(ctx, `SELECT `+keepWorkbenchReplayPayload+`,r.duplicate_payloads_compacted_at IS NOT NULL,`+keepLegacyWorkbenchPayload+`
		FROM office_agent_runs r WHERE id=$1`, id).Scan(&kept, &alreadyTrimmed, &legacyLock)
	if err != nil {
		return false, err
	}
	if legacyLock || kept && alreadyTrimmed {
		return false, tx.Commit()
	}
	if err = compactWorkbenchOperations(ctx, tx, userID, id, kept); err != nil {
		return false, err
	}
	if !alreadyTrimmed {
		if err = compactWorkbenchDeltas(ctx, tx, userID, id); err != nil {
			return false, err
		}
	}
	_, err = tx.ExecContext(ctx, `UPDATE office_agent_runs SET
		duplicate_payloads_compacted_at=COALESCE(duplicate_payloads_compacted_at,NOW()),
		payloads_compacted_at=CASE WHEN NOT $2 THEN NOW() END WHERE id=$1`, id, kept)
	if err != nil {
		return false, err
	}
	return true, tx.Commit()
}

const keepLegacyWorkbenchPayload = `EXISTS (
	SELECT 1 FROM office_agent_runs active
	WHERE active.user_id=r.user_id AND active.conversation_id=r.conversation_id AND active.browser_driven
	AND active.status IN ('queued','running','recovering','waiting_user','needs_confirmation')
	AND COALESCE(active.parameters->>'history_version','0')='0'
)`

const keepWorkbenchReplayPayload = `(
	r.browser_driven AND r.replay_pinned AND r.selected AND r.status='completed'
	AND r.parameters->>'history_version'='2' AND r.replay_invalidated_at IS NULL
	AND EXISTS (
		SELECT 1 FROM office_agent_conversations conversation
		JOIN office_agent_messages root ON root.run_id=COALESCE(r.regeneration_of,r.id) AND root.role='user'
		WHERE conversation.id=r.conversation_id AND conversation.user_id=r.user_id AND conversation.deleted_at IS NULL
		AND (
			root.sequence>=COALESCE((
				SELECT checkpoint.before_sequence FROM office_agent_replay_checkpoints checkpoint
				WHERE checkpoint.conversation_id=r.conversation_id AND checkpoint.published
			),0)
			OR EXISTS (
				SELECT 1 FROM office_agent_runs active
				LEFT JOIN office_agent_replay_checkpoints checkpoint ON checkpoint.id=active.replay_checkpoint_id
				WHERE active.conversation_id=r.conversation_id AND active.user_id=r.user_id AND active.replay_pinned
				  AND active.status IN ('queued','running','recovering','waiting_user','needs_confirmation')
				  AND root.sequence BETWEEN GREATEST(active.replay_native_from,COALESCE(checkpoint.before_sequence,0)) AND active.context_boundary
			)
		)
	)
)`

func compactWorkbenchOperations(ctx context.Context, tx *sql.Tx, userID int64, runID string, keepReplay bool) error {
	rows, err := tx.QueryContext(ctx, `SELECT `+officeOperationColumns+` FROM office_agent_operations
		WHERE user_id=$1 AND run_id=$2 AND (result_hash IS NULL AND result_json IS NOT NULL
		OR kind='tool' AND tool_projection IS NULL)`, userID, runID)
	if err != nil {
		return err
	}
	// Hold only the hashes and small display projections in memory while reading the old payloads.
	type backfill struct {
		id, hash   string
		projection any
	}
	var pending []backfill
	for rows.Next() {
		op, err := scanOfficeOperation(rows)
		if err != nil {
			_ = rows.Close()
			return err
		}
		hash := op.ResultHash
		if hash == "" && op.Result != nil {
			result, err := json.Marshal(op.Result)
			if err != nil {
				_ = rows.Close()
				return err
			}
			hash = officeOperationResultHash(result)
		}
		projection, err := marshalWorkbenchToolProjection(*op)
		if err != nil {
			_ = rows.Close()
			return err
		}
		pending = append(pending, backfill{op.ID, hash, projection})
	}
	_ = rows.Close()
	if err := rows.Err(); err != nil {
		return err
	}
	for _, item := range pending {
		if _, err := tx.ExecContext(ctx, `UPDATE office_agent_operations SET result_hash=NULLIF($3,''),
			tool_projection=COALESCE(tool_projection,$4) WHERE run_id=$1 AND operation_id=$2`, runID, item.id, item.hash, item.projection); err != nil {
			return err
		}
	}
	_, err = tx.ExecContext(ctx, `UPDATE office_agent_operations
		SET input_json=CASE WHEN kind='tool' THEN jsonb_strip_nulls(jsonb_build_object('tool',input_json->'tool','remote',input_json->'remote')) ELSE '{}'::jsonb END,
		result_json=CASE WHEN $3 AND state='completed' THEN result_json
			WHEN result_json IS NOT NULL THEN
			(result_json - 'output' - 'error_message') || '{"output":null}'::jsonb END
		WHERE user_id=$1 AND run_id=$2`, userID, runID, keepReplay)
	return err
}

func marshalWorkbenchToolProjection(op service.OfficeOperation) (any, error) {
	if op.Kind != service.OfficeOperationTool {
		return nil, nil
	}
	snapshot := op.ToolProjection
	if snapshot == nil {
		snapshot = service.BuildWorkbenchToolProjection(op)
	}
	if snapshot == nil {
		return nil, nil
	}
	return json.Marshal(snapshot)
}

func compactWorkbenchDeltas(ctx context.Context, tx *sql.Tx, userID int64, runID string) error {
	var text, reasoning string
	if err := tx.QueryRowContext(ctx, `SELECT
		CASE WHEN EXISTS (SELECT 1 FROM office_agent_messages m WHERE m.user_id=$1 AND m.run_id=$2 AND m.role='assistant')
			THEN '' ELSE LEFT(COALESCE(string_agg(payload->>'text','' ORDER BY sequence) FILTER (WHERE event_type='assistant.delta'),''),6000) END,
		CASE WHEN EXISTS (SELECT 1 FROM office_agent_messages m WHERE m.user_id=$1 AND m.run_id=$2 AND m.role='assistant' AND COALESCE(m.content->>'reasoning_summary','')<>'')
			THEN '' ELSE LEFT(COALESCE(string_agg(payload->>'text','' ORDER BY sequence) FILTER (WHERE event_type='reasoning.delta'),''),2000) END
		FROM office_agent_run_events WHERE user_id=$1 AND run_id=$2 AND event_type IN ('assistant.delta','reasoning.delta')`, userID, runID).
		Scan(&text, &reasoning); err != nil {
		return err
	}
	if text != "" || reasoning != "" {
		payload, err := json.Marshal(map[string]string{"text": text, "reasoning_summary": reasoning})
		if err != nil {
			return err
		}
		// Terminal snapshots are written outside the streaming quota and keep existing cursors monotonic.
		var seq int64
		if err := tx.QueryRowContext(ctx, `UPDATE office_agent_runs SET next_event_seq=next_event_seq+1 WHERE id=$1 RETURNING next_event_seq`, runID).Scan(&seq); err != nil {
			return err
		}
		if _, err := tx.ExecContext(ctx, `INSERT INTO office_agent_run_events(user_id,run_id,event_id,sequence,event_type,payload)
			VALUES ($1,$2,'system:partial',$3,'assistant.partial',$4)`, userID, runID, seq, payload); err != nil {
			return err
		}
	}
	_, err := tx.ExecContext(ctx, `DELETE FROM office_agent_run_events
		WHERE user_id=$1 AND run_id=$2 AND event_type IN ('assistant.delta','reasoning.delta')`, userID, runID)
	return err
}
