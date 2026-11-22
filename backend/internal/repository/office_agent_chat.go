package repository

import (
	"context"
	"database/sql"
	"encoding/base64"
	"encoding/json"
	"errors"
	"github.com/lib/pq"
	"strings"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/service"
)

func (r *officeAgentRepository) ListConversationPage(ctx context.Context, user int64, cursor string, limit int) ([]service.OfficeConversation, string, error) {
	limit = max(1, min(limit, 50))
	before := time.Now().Add(time.Hour)
	id := "ffffffff-ffff-ffff-ffff-ffffffffffff"
	if cursor != "" {
		b, e := base64.RawURLEncoding.DecodeString(cursor)
		if e != nil {
			return nil, "", service.ErrOfficeInvalid
		}
		parts := strings.Split(string(b), "|")
		if len(parts) != 2 || !officeUUID(parts[1]) {
			return nil, "", service.ErrOfficeInvalid
		}
		before, e = time.Parse(time.RFC3339Nano, parts[0])
		if e != nil {
			return nil, "", service.ErrOfficeInvalid
		}
		id = parts[1]
	}
	rows, e := r.db.QueryContext(ctx, `SELECT id::text,user_id,title,created_at,updated_at FROM office_agent_conversations WHERE user_id=$1 AND deleted_at IS NULL AND (updated_at,id)<($2,$3::uuid) ORDER BY updated_at DESC,id DESC LIMIT $4`, user, before, id, limit+1)
	if e != nil {
		return nil, "", e
	}
	defer func() { _ = rows.Close() }()
	items := []service.OfficeConversation{}
	for rows.Next() {
		var c service.OfficeConversation
		if e = rows.Scan(&c.ID, &c.UserID, &c.Title, &c.CreatedAt, &c.UpdatedAt); e != nil {
			return nil, "", e
		}
		items = append(items, c)
	}
	if e = rows.Err(); e != nil {
		return nil, "", e
	}
	next := ""
	if len(items) > limit {
		items = items[:limit]
		c := items[len(items)-1]
		next = base64.RawURLEncoding.EncodeToString([]byte(c.UpdatedAt.Format(time.RFC3339Nano) + "|" + c.ID))
	}
	return items, next, nil
}
func (r *officeAgentRepository) RenameConversation(ctx context.Context, user int64, id, title string) (*service.OfficeConversation, error) {
	if !officeUUID(id) || strings.TrimSpace(title) == "" || len([]rune(title)) > 100 {
		return nil, service.ErrOfficeInvalid
	}
	c := &service.OfficeConversation{}
	e := r.db.QueryRowContext(ctx, `UPDATE office_agent_conversations SET title=$3,updated_at=NOW() WHERE user_id=$1 AND id=$2 AND deleted_at IS NULL RETURNING id::text,user_id,title,created_at,updated_at`, user, id, strings.TrimSpace(title)).Scan(&c.ID, &c.UserID, &c.Title, &c.CreatedAt, &c.UpdatedAt)
	return c, officeDBError(e)
}
func (r *officeAgentRepository) DeleteConversation(ctx context.Context, user int64, id string) error {
	if !officeUUID(id) {
		return service.ErrOfficeNotFound
	}
	tx, e := r.db.BeginTx(ctx, nil)
	if e != nil {
		return e
	}
	defer func() { _ = tx.Rollback() }()
	// Admission locks the user before creating a run. Hold that same lock so
	// no new run can appear between fencing the existing runs and deleting the
	// conversation. FinishRun locks run -> conversation; follow that order here
	// to avoid deletion racing a paid result into a PostgreSQL deadlock.
	var owner int64
	if e = tx.QueryRowContext(ctx, `SELECT id FROM users WHERE id=$1 AND deleted_at IS NULL FOR UPDATE`, user).Scan(&owner); e != nil {
		return officeDBError(e)
	}
	var locked int
	if e = tx.QueryRowContext(ctx, `SELECT COUNT(*) FROM (SELECT id FROM office_agent_runs WHERE user_id=$1 AND conversation_id=$2 AND status IN ('queued','running','waiting_user','recovering','needs_confirmation') ORDER BY id FOR UPDATE) locked`, user, id).Scan(&locked); e != nil {
		return e
	}
	var found string
	if e = tx.QueryRowContext(ctx, `UPDATE office_agent_conversations SET deleted_at=NOW() WHERE user_id=$1 AND id=$2 AND deleted_at IS NULL RETURNING id::text`, user, id).Scan(&found); e != nil {
		return officeDBError(e)
	}
	_, e = tx.ExecContext(ctx, `UPDATE office_agent_runs SET status='cancelled',lease_epoch=lease_epoch+1,lease_token=NULL,lease_until=NULL,lease_owner=NULL,finished_at=NOW(),updated_at=NOW() WHERE user_id=$1 AND conversation_id=$2 AND status IN ('queued','running','waiting_user','recovering','needs_confirmation')`, user, id)
	if e != nil {
		return e
	}
	_, e = tx.ExecContext(ctx, `UPDATE office_agent_replay_checkpoints SET published=FALSE
		WHERE user_id=$1 AND conversation_id=$2 AND published`, user, id)
	if e != nil {
		return e
	}
	return tx.Commit()
}
func (r *officeAgentRepository) ContextMessages(ctx context.Context, run service.OfficeRun) ([]service.OfficeMessage, error) {
	// A replacement answer occupies the original turn's position. A stopped
	// root contributes its question until a completed retry replaces it.
	rows, e := r.db.QueryContext(ctx, `
WITH history AS (
SELECT m.id::text,m.conversation_id::text,m.run_id::text,m.sequence,m.role,m.content,m.created_at
     ,COALESCE(root.sequence,m.sequence) AS turn_sequence
     ,m.run_id=$5 AS current_turn
FROM office_agent_messages m
JOIN office_agent_runs r ON r.id=m.run_id
LEFT JOIN office_agent_messages root ON root.run_id=r.regeneration_of AND root.user_id=m.user_id
  AND root.conversation_id=m.conversation_id AND root.role='user'
WHERE m.user_id=$1 AND m.conversation_id=$2
  AND ((COALESCE(root.sequence,m.sequence)<=$3
        AND ($4='' OR (r.id<>NULLIF($4,'')::uuid AND r.regeneration_of IS DISTINCT FROM NULLIF($4,'')::uuid))
        AND ((r.selected AND r.status IN ('completed','partially_completed'))
          OR (m.role='user' AND r.selected AND r.regeneration_of IS NULL AND r.status IN ('cancelled','failed'))))
    OR (m.run_id=$5 AND m.role='user'))
)
SELECT id,conversation_id,run_id,sequence,role,content,created_at FROM history
ORDER BY current_turn DESC,turn_sequence DESC,sequence DESC LIMIT 2001`, run.UserID, run.ConversationID, run.ContextBoundary, run.RegenerationOf, run.ID)
	if e != nil {
		return nil, e
	}
	defer func() { _ = rows.Close() }()
	result := []service.OfficeMessage{}
	for rows.Next() {
		var m service.OfficeMessage
		if e = rows.Scan(&m.ID, &m.ConversationID, &m.RunID, &m.Sequence, &m.Role, &m.Content, &m.CreatedAt); e != nil {
			return nil, e
		}
		result = append(result, m)
	}
	if len(result) > 2000 {
		return nil, service.ErrOfficeBudgetExceeded
	}
	for i, j := 0, len(result)-1; i < j; i, j = i+1, j-1 {
		result[i], result[j] = result[j], result[i]
	}
	return result, rows.Err()
}
func (r *officeAgentRepository) ListOperations(ctx context.Context, user int64, run string) ([]service.OfficeOperation, error) {
	if _, e := r.GetRun(ctx, user, run); e != nil {
		return nil, e
	}
	// Hydrate source metadata only for confirmed network-tool inboxes. Model
	// prompts/responses and unrelated file tools remain absent from this listing.
	rows, e := r.db.QueryContext(ctx, `SELECT operation_id,kind,state,request_id,created_at,dispatched_at,completed_at,
 CASE WHEN kind='tool' AND state='completed' AND input_json->>'tool' IN ('web_search','web_read') THEN input_json END,
 CASE WHEN kind='tool' AND state='completed' AND input_json->>'tool' IN ('web_search','web_read') THEN result_json END,
 CASE WHEN kind='tool' AND state='completed' AND tool_projection ? 'citations'
      THEN jsonb_build_object('citations',tool_projection->'citations') END
 FROM office_agent_operations WHERE user_id=$1 AND run_id=$2 ORDER BY created_at,operation_id LIMIT 200`, user, run)
	if e != nil {
		return nil, e
	}
	defer func() { _ = rows.Close() }()
	out := []service.OfficeOperation{}
	for rows.Next() {
		var o service.OfficeOperation
		var input, result, projection []byte
		o.RunID = run
		if e = rows.Scan(&o.ID, &o.Kind, &o.State, &o.RequestID, &o.CreatedAt, &o.DispatchedAt, &o.CompletedAt, &input, &result, &projection); e != nil {
			return nil, e
		}
		o.Input = input
		if len(result) > 0 {
			if e = json.Unmarshal(result, &o.Result); e != nil {
				return nil, e
			}
		}
		if len(projection) > 0 {
			if e = json.Unmarshal(projection, &o.ToolProjection); e != nil {
				return nil, e
			}
		}
		out = append(out, o)
	}
	return out, rows.Err()
}
func (r *officeAgentRepository) Checkpoint(ctx context.Context, lease service.OfficeRunLease) (json.RawMessage, int64, error) {
	tx, e := r.db.BeginTx(ctx, nil)
	if e != nil {
		return nil, 0, e
	}
	defer func() { _ = tx.Rollback() }()
	if _, e = checkOfficeLease(ctx, tx, lease); e != nil {
		return nil, 0, e
	}
	var data json.RawMessage
	var v int64
	e = tx.QueryRowContext(ctx, `SELECT envelope,version FROM office_checkpoints.snapshots WHERE run_id=$1`, lease.Run.ID).Scan(&data, &v)
	if errors.Is(e, sql.ErrNoRows) {
		e = nil
	}
	if e != nil {
		return nil, 0, e
	}
	return data, v, tx.Commit()
}
func (r *officeAgentRepository) SaveCheckpoint(ctx context.Context, lease service.OfficeRunLease, data json.RawMessage, expected int64, writeID string) (int64, error) {
	if !officeJSON(data, 1<<20) || !officeToken.MatchString(writeID) || expected < 0 {
		return 0, service.ErrOfficeInvalid
	}
	tx, e := r.db.BeginTx(ctx, nil)
	if e != nil {
		return 0, e
	}
	defer func() { _ = tx.Rollback() }()
	if _, e = checkOfficeLease(ctx, tx, lease); e != nil {
		return 0, e
	}
	var v int64
	var previous string
	var same bool
	e = tx.QueryRowContext(ctx, `SELECT version,write_id,envelope=$2::jsonb FROM office_checkpoints.snapshots WHERE run_id=$1 FOR UPDATE`, lease.Run.ID, []byte(data)).Scan(&v, &previous, &same)
	if e != nil && !errors.Is(e, sql.ErrNoRows) {
		return 0, e
	}
	if previous == writeID {
		if !same {
			return 0, service.ErrOfficeConflict
		}
		return v, tx.Commit()
	}
	if v != expected {
		return 0, service.ErrOfficeConflict
	}
	v++
	_, e = tx.ExecContext(ctx, `INSERT INTO office_checkpoints.snapshots(run_id,epoch,version,write_id,envelope) VALUES($1,$2,$3,$4,$5) ON CONFLICT(run_id) DO UPDATE SET epoch=EXCLUDED.epoch,version=EXCLUDED.version,write_id=EXCLUDED.write_id,envelope=EXCLUDED.envelope,updated_at=NOW()`, lease.Run.ID, lease.Epoch, v, writeID, []byte(data))
	if e != nil {
		return 0, e
	}
	return v, tx.Commit()
}
func (r *officeAgentRepository) WaitForInput(ctx context.Context, lease service.OfficeRunLease, requestID string, payload json.RawMessage) (*service.OfficeRun, error) {
	if !officeToken.MatchString(requestID) || !officeJSON(payload, 32768) {
		return nil, service.ErrOfficeInvalid
	}
	tx, e := r.db.BeginTx(ctx, nil)
	if e != nil {
		return nil, e
	}
	defer func() { _ = tx.Rollback() }()
	if _, e = checkOfficeLease(ctx, tx, lease); e != nil {
		return nil, e
	}
	var safe bool
	if e = tx.QueryRowContext(ctx, `SELECT EXISTS(SELECT 1 FROM office_checkpoints.snapshots WHERE run_id=$1) AND NOT EXISTS(SELECT 1 FROM office_agent_operations WHERE run_id=$1 AND state IN ('dispatched','unknown'))`, lease.Run.ID).Scan(&safe); e != nil {
		return nil, e
	}
	if !safe {
		return nil, service.ErrOfficeResultUnknown
	}
	_, e = tx.ExecContext(ctx, `UPDATE office_agent_runs SET status='waiting_user',paused_at=NOW(),input_request_id=$2,lease_token=NULL,lease_until=NULL,lease_owner=NULL,updated_at=NOW() WHERE id=$1`, lease.Run.ID, requestID)
	if e != nil {
		return nil, e
	}
	if _, e = appendOfficeEvent(ctx, tx, lease.Run.UserID, lease.Run.ID, "clarify:"+requestID, "clarification.requested", payload); e != nil {
		return nil, e
	}
	run, e := scanOfficeRun(tx.QueryRowContext(ctx, `SELECT `+officeRunColumns+` FROM office_agent_runs WHERE id=$1`, lease.Run.ID))
	if e != nil {
		return nil, e
	}
	return run, tx.Commit()
}
func (r *officeAgentRepository) ResumeInput(ctx context.Context, user int64, id, requestID string, value json.RawMessage) (*service.OfficeRun, error) {
	if !officeUUID(id) || !officeToken.MatchString(requestID) || !officeJSON(value, 32768) {
		return nil, service.ErrOfficeInvalid
	}
	tx, e := r.db.BeginTx(ctx, nil)
	if e != nil {
		return nil, e
	}
	defer func() { _ = tx.Rollback() }()
	run, e := scanOfficeRun(tx.QueryRowContext(ctx, `SELECT `+officeRunColumns+` FROM office_agent_runs WHERE user_id=$1 AND id=$2 FOR UPDATE`, user, id))
	if e != nil {
		return nil, e
	}
	var previous json.RawMessage
	e = tx.QueryRowContext(ctx, `SELECT value FROM office_agent_user_inputs WHERE run_id=$1 AND request_id=$2`, id, requestID).Scan(&previous)
	if e == nil {
		var same bool
		if e = tx.QueryRowContext(ctx, `SELECT $1::jsonb=$2::jsonb`, []byte(previous), []byte(value)).Scan(&same); e != nil {
			return nil, e
		}
		if !same {
			return nil, service.ErrOfficeConflict
		}
		return run, tx.Commit()
	}
	if !errors.Is(e, sql.ErrNoRows) {
		return nil, e
	}
	if run.Status != service.OfficeRunWaitingUser || run.InputRequestID != requestID {
		return nil, service.ErrOfficeConflict
	}
	_, e = tx.ExecContext(ctx, `INSERT INTO office_agent_user_inputs(run_id,request_id,value) VALUES($1,$2,$3)`, id, requestID, []byte(value))
	if e != nil {
		return nil, e
	}
	_, e = tx.ExecContext(ctx, `UPDATE office_agent_runs SET status='queued',paused_seconds=paused_seconds+GREATEST(0,EXTRACT(EPOCH FROM (NOW()-paused_at))::bigint),paused_at=NULL,resume_value=$2,lease_epoch=lease_epoch+1,updated_at=NOW() WHERE id=$1`, id, []byte(value))
	if e != nil {
		return nil, e
	}
	if _, e = appendOfficeEvent(ctx, tx, user, id, "resume:"+requestID, "run.queued", json.RawMessage(`{"resumed":true}`)); e != nil {
		return nil, e
	}
	run, e = scanOfficeRun(tx.QueryRowContext(ctx, `SELECT `+officeRunColumns+` FROM office_agent_runs WHERE id=$1`, id))
	if e != nil {
		return nil, e
	}
	return run, tx.Commit()
}
func (r *officeAgentRepository) SelectAnswer(ctx context.Context, user int64, id string) (*service.OfficeRun, error) {
	run, e := r.GetRun(ctx, user, id)
	if e != nil {
		return nil, e
	}
	if run.Status != service.OfficeRunCompleted && run.Status != service.OfficeRunPartiallyCompleted {
		return nil, service.ErrOfficeConflict
	}
	root := run.RegenerationOf
	if root == "" {
		root = run.ID
	}
	tx, e := r.db.BeginTx(ctx, nil)
	if e != nil {
		return nil, e
	}
	defer func() { _ = tx.Rollback() }()
	var owner int64
	if e = tx.QueryRowContext(ctx, `SELECT user_id FROM office_agent_conversations WHERE id=$1 AND user_id=$2 AND deleted_at IS NULL FOR UPDATE`, run.ConversationID, user).Scan(&owner); e != nil {
		return nil, officeDBError(e)
	}
	var active bool
	if e = tx.QueryRowContext(ctx, `SELECT EXISTS(SELECT 1 FROM office_agent_runs WHERE conversation_id=$1 AND status IN ('queued','running','waiting_user','recovering','needs_confirmation'))`, run.ConversationID).Scan(&active); e != nil {
		return nil, e
	}
	if active {
		return nil, service.ErrOfficeActiveRun
	}
	var selected bool
	if e = tx.QueryRowContext(ctx, `SELECT selected FROM office_agent_runs WHERE id=$1 AND user_id=$2`, id, user).Scan(&selected); e != nil {
		return nil, officeDBError(e)
	}
	if selected {
		run.Selected = true
		return run, tx.Commit()
	}
	_, e = tx.ExecContext(ctx, `UPDATE office_agent_runs SET selected=(id=$3) WHERE user_id=$1 AND conversation_id=$2 AND (id=$4 OR regeneration_of=$4)`, user, run.ConversationID, id, root)
	if e != nil {
		return nil, e
	}
	if e = invalidateWorkbenchReplayAfter(ctx, tx, user, run.ConversationID, root); e != nil {
		return nil, e
	}
	run.Selected = true
	return run, tx.Commit()
}

func (r *officeAgentRepository) MessagePage(ctx context.Context, user int64, id string, before int64) ([]service.OfficeMessage, []service.OfficeRun, bool, int64, error) {
	if _, e := r.GetConversation(ctx, user, id); e != nil {
		return nil, nil, false, 0, e
	}
	return officeMessagePage(ctx, r.db, user, id, before)
}

type officeMessageQuerier interface {
	QueryContext(context.Context, string, ...any) (*sql.Rows, error)
}

const officeMessageColumns = `id::text,conversation_id::text,run_id::text,sequence,role,content,created_at`

func scanOfficeMessage(row officeScanner) (service.OfficeMessage, error) {
	var message service.OfficeMessage
	err := row.Scan(&message.ID, &message.ConversationID, &message.RunID, &message.Sequence, &message.Role, &message.Content, &message.CreatedAt)
	return message, err
}

func officeMessagePage(ctx context.Context, db officeMessageQuerier, user int64, id string, before int64) ([]service.OfficeMessage, []service.OfficeRun, bool, int64, error) {
	if before <= 0 {
		before = 1 << 62
	}
	rows, e := db.QueryContext(ctx, `SELECT `+officeMessageColumns+` FROM office_agent_messages WHERE user_id=$1 AND conversation_id=$2 AND sequence<$3 ORDER BY sequence DESC LIMIT 51`, user, id, before)
	if e != nil {
		return nil, nil, false, 0, e
	}
	defer func() { _ = rows.Close() }()
	items := []service.OfficeMessage{}
	ids := []string{}
	for rows.Next() {
		m, e := scanOfficeMessage(rows)
		if e != nil {
			return nil, nil, false, 0, e
		}
		items = append(items, m)
	}
	if e = rows.Err(); e != nil {
		return nil, nil, false, 0, e
	}
	more := len(items) > 50
	if more {
		items = items[:50]
	}
	next := int64(0)
	if len(items) > 0 {
		next = items[len(items)-1].Sequence
	}
	for i, j := 0, len(items)-1; i < j; i, j = i+1, j-1 {
		items[i], items[j] = items[j], items[i]
	}
	for _, m := range items {
		ids = append(ids, m.RunID)
	}
	rr, e := db.QueryContext(ctx, `SELECT `+officeRunColumns+` FROM office_agent_runs WHERE user_id=$1 AND conversation_id=$2 AND (id=ANY($3::uuid[]) OR status IN ('queued','running','recovering','waiting_user','needs_confirmation')) ORDER BY created_at,id LIMIT 100`, user, id, pq.Array(ids))
	if e != nil {
		return nil, nil, false, 0, e
	}
	defer func() { _ = rr.Close() }()
	runs := []service.OfficeRun{}
	for rr.Next() {
		run, e := scanOfficeRun(rr)
		if e != nil {
			return nil, nil, false, 0, e
		}
		runs = append(runs, *run)
	}
	if e = rr.Err(); e != nil {
		return nil, nil, false, 0, e
	}
	_ = rr.Close()
	if e = hydrateOfficeRunArtifacts(ctx, db, user, runs); e != nil {
		return nil, nil, false, 0, e
	}
	return items, runs, more, next, nil
}

// hydrateOfficeRunArtifacts reads one bounded metadata batch for the history
// page. Events are transient UI progress; immutable versions, rather than the
// resource's latest version, remain the authority for each run's file cards.
// This deliberately reads neither private storage targets nor object bytes and
// never signs URLs. Edited uploads also have a run_id, even though the resource
// retains its original source="upload".
func hydrateOfficeRunArtifacts(ctx context.Context, db officeMessageQuerier, user int64, runs []service.OfficeRun) error {
	if len(runs) == 0 {
		return nil
	}
	ids := make([]string, 0, len(runs))
	byID := make(map[string]*[]service.OfficeResource, len(runs))
	for i := range runs {
		artifacts := []service.OfficeResource{}
		runs[i].Artifacts = &artifacts
		byID[runs[i].ID] = runs[i].Artifacts
		ids = append(ids, runs[i].ID)
	}
	rows, err := db.QueryContext(ctx, `SELECT run_id::text,resource_id::text,id::text,COALESCE(parent_version_id::text,''),name,mime,byte_size,sha256,preview_kind,source,created_at FROM (
 SELECT v.run_id,v.resource_id,v.id,v.parent_version_id,v.name,v.mime,v.byte_size,v.sha256,v.preview_kind,r.source,v.created_at,
 ROW_NUMBER() OVER(PARTITION BY v.run_id ORDER BY v.created_at,v.id) AS artifact_position
 FROM office_resource_versions v JOIN office_resources r ON r.id=v.resource_id AND r.user_id=v.user_id
 WHERE v.user_id=$1 AND v.run_id=ANY($2::uuid[]) AND v.state='ready' AND r.deleted_at IS NULL
) artifacts WHERE artifact_position<=$3 ORDER BY run_id,created_at,id`, user, pq.Array(ids), service.OfficeWorkspaceMaxFiles)
	if err != nil {
		return err
	}
	defer func() { _ = rows.Close() }()
	for rows.Next() {
		var runID string
		var artifact service.OfficeResource
		if err = rows.Scan(&runID, &artifact.ID, &artifact.VersionID, &artifact.ParentVersionID, &artifact.Name, &artifact.MIME, &artifact.Bytes, &artifact.SHA256, &artifact.PreviewKind, &artifact.Source, &artifact.CreatedAt); err != nil {
			return err
		}
		artifact.Role = "output"
		if target := byID[runID]; target != nil {
			*target = append(*target, artifact)
		}
	}
	return rows.Err()
}
