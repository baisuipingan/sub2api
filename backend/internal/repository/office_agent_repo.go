package repository

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"regexp"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/google/uuid"
	"github.com/lib/pq"
)

type officeAgentRepository struct{ db *sql.DB }

func NewOfficeAgentRepository(db *sql.DB) service.OfficeAgentRepository {
	return &officeAgentRepository{db: db}
}

var officeToken = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._:-]{0,127}$`)
var officeHash = regexp.MustCompile(`^[0-9a-f]{64}$`)

const officeRunColumns = `id::text,user_id,conversation_id::text,api_key_id,group_id,model,status,
 limits_json,input_resources,model_calls,tool_calls,error_code,error_message,created_at,updated_at,started_at,finished_at,mode,search_enabled,parameters,context_boundary,COALESCE(regeneration_of::text,''),selected,COALESCE(resume_value,'null'::jsonb),COALESCE(input_request_id,''),paused_seconds,browser_driven`

type officeScanner interface{ Scan(...any) error }

func scanOfficeRun(row officeScanner, extra ...any) (*service.OfficeRun, error) {
	r := &service.OfficeRun{}
	var limits, resources []byte
	targets := []any{&r.ID, &r.UserID, &r.ConversationID, &r.APIKeyID, &r.GroupID, &r.Model, &r.Status,
		&limits, &resources, &r.ModelCalls, &r.ToolCalls, &r.ErrorCode, &r.ErrorMessage, &r.CreatedAt, &r.UpdatedAt, &r.StartedAt, &r.FinishedAt, &r.Mode, &r.SearchEnabled, &r.Parameters, &r.ContextBoundary, &r.RegenerationOf, &r.Selected, &r.ResumeValue, &r.InputRequestID, &r.PausedSeconds, &r.BrowserDriven}
	if err := row.Scan(append(targets, extra...)...); err != nil {
		return nil, officeDBError(err)
	}
	if err := json.Unmarshal(limits, &r.Limits); err != nil {
		return nil, err
	}
	if err := json.Unmarshal(resources, &r.InputResources); err != nil {
		return nil, err
	}
	return r, nil
}

func officeDBError(err error) error {
	if errors.Is(err, sql.ErrNoRows) {
		return service.ErrOfficeNotFound
	}
	var pg *pq.Error
	if errors.As(err, &pg) {
		if pg.Code == "23505" {
			if strings.Contains(pg.Constraint, "active_run") {
				return service.ErrOfficeActiveRun
			}
			return service.ErrOfficeConflict
		}
		if pg.Code == "23503" {
			return service.ErrOfficeNotFound
		}
		if pg.Code == "23514" || pg.Code == "22001" || pg.Code == "22P02" {
			return service.ErrOfficeInvalid
		}
	}
	return err
}

func officeUUID(s string) bool { _, err := uuid.Parse(s); return err == nil }
func officeJSON(data json.RawMessage, limit int) bool {
	return len(data) > 0 && len(data) <= limit && json.Valid(data)
}
func officeLimit(n int) int {
	if n <= 0 {
		return 50
	}
	if n > 200 {
		return 200
	}
	return n
}

func (r *officeAgentRepository) CreateConversation(ctx context.Context, userID int64, id, title string) (*service.OfficeConversation, error) {
	if userID <= 0 || !officeUUID(id) || !utf8.ValidString(title) || utf8.RuneCountInString(title) > 200 {
		return nil, service.ErrOfficeInvalid
	}
	c := &service.OfficeConversation{}
	err := r.db.QueryRowContext(ctx, `INSERT INTO office_agent_conversations(id,user_id,title)
 SELECT $1,$2,$3 FROM users WHERE id=$2 AND deleted_at IS NULL AND status='active'
 RETURNING id::text,user_id,title,created_at,updated_at`, id, userID, title).Scan(&c.ID, &c.UserID, &c.Title, &c.CreatedAt, &c.UpdatedAt)
	return c, officeDBError(err)
}

func (r *officeAgentRepository) GetConversation(ctx context.Context, userID int64, id string) (*service.OfficeConversation, error) {
	if !officeUUID(id) {
		return nil, service.ErrOfficeNotFound
	}
	c := &service.OfficeConversation{}
	err := r.db.QueryRowContext(ctx, `SELECT id::text,user_id,title,created_at,updated_at FROM office_agent_conversations WHERE user_id=$1 AND id=$2 AND deleted_at IS NULL`, userID, id).Scan(&c.ID, &c.UserID, &c.Title, &c.CreatedAt, &c.UpdatedAt)
	return c, officeDBError(err)
}

func (r *officeAgentRepository) ListConversations(ctx context.Context, userID int64, limit int) ([]service.OfficeConversation, error) {
	rows, err := r.db.QueryContext(ctx, `SELECT id::text,user_id,title,created_at,updated_at FROM office_agent_conversations WHERE user_id=$1 AND deleted_at IS NULL ORDER BY updated_at DESC,id LIMIT $2`, userID, officeLimit(limit))
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()
	result := []service.OfficeConversation{}
	for rows.Next() {
		var c service.OfficeConversation
		if err := rows.Scan(&c.ID, &c.UserID, &c.Title, &c.CreatedAt, &c.UpdatedAt); err != nil {
			return nil, err
		}
		result = append(result, c)
	}
	return result, rows.Err()
}

func (r *officeAgentRepository) ListMessages(ctx context.Context, userID int64, conversationID string, after int64, limit int) ([]service.OfficeMessage, error) {
	if _, err := r.GetConversation(ctx, userID, conversationID); err != nil {
		return nil, err
	}
	rows, err := r.db.QueryContext(ctx, `SELECT id::text,conversation_id::text,run_id::text,sequence,role,content,created_at FROM office_agent_messages WHERE user_id=$1 AND conversation_id=$2 AND sequence>$3 ORDER BY sequence LIMIT $4`, userID, conversationID, after, officeLimit(limit))
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()
	result := []service.OfficeMessage{}
	for rows.Next() {
		var m service.OfficeMessage
		if err := rows.Scan(&m.ID, &m.ConversationID, &m.RunID, &m.Sequence, &m.Role, &m.Content, &m.CreatedAt); err != nil {
			return nil, err
		}
		result = append(result, m)
	}
	return result, rows.Err()
}

func validOfficeAdmission(in service.OfficeRunAdmission) bool {
	if in.UserID <= 0 || in.APIKeyID <= 0 || in.GroupID <= 0 || !officeUUID(in.ConversationID) || !officeUUID(in.RunID) || !officeToken.MatchString(in.IdempotencyKey) || !officeHash.MatchString(in.RequestHash) || len(in.Model) == 0 || len(in.Model) > 128 || !utf8.ValidString(in.Model) || !officeJSON(in.UserContent, service.OfficeMaxOperationInputBytes) {
		return false
	}
	l := in.Limits
	if l.MaxModelCalls < 1 || l.MaxModelCalls > 64 || l.MaxToolCalls < 0 || l.MaxToolCalls > 128 || l.MaxOutputTokens < 1 || l.MaxOutputTokens > 2_000_000 || l.MaxRunSeconds < 1 || l.MaxRunSeconds > 3600 || l.MaxChargeCredits < 0 || l.MaxChargeCredits > 9_007_199_254_740_991 || len(in.InputResources) > 64 {
		return false
	}
	for _, ref := range in.InputResources {
		if !officeToken.MatchString(ref.ResourceID) || !officeToken.MatchString(ref.VersionID) {
			return false
		}
	}
	return true
}

func (r *officeAgentRepository) AdmitRun(ctx context.Context, in service.OfficeRunAdmission) (*service.OfficeRun, bool, error) {
	if in.Mode == "" {
		in.Mode = "office"
	}
	if in.Mode != "chat" && in.Mode != "office" {
		return nil, false, service.ErrOfficeInvalid
	}
	if len(in.Parameters) == 0 {
		in.Parameters = json.RawMessage(`{}`)
	}
	if len(in.Parameters) > 4096 || !json.Valid(in.Parameters) {
		return nil, false, service.ErrOfficeInvalid
	}

	if !validOfficeAdmission(in) {
		return nil, false, service.ErrOfficeInvalid
	}
	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return nil, false, err
	}
	defer func() { _ = tx.Rollback() }()
	if len(in.InputResources) > 0 {
		if err = galleryLockUser(ctx, tx, in.UserID); err != nil {
			return nil, false, err
		}
	}
	// Serialize admission for this user, including different conversations. A
	// unique partial index remains the independent database safety net.
	var userID int64
	if err := tx.QueryRowContext(ctx, `SELECT id FROM users WHERE id=$1 AND deleted_at IS NULL AND status='active' FOR UPDATE`, in.UserID).Scan(&userID); err != nil {
		return nil, false, officeDBError(err)
	}
	var existingID, hash, conversationID string
	err = tx.QueryRowContext(ctx, `SELECT id::text,request_hash,conversation_id::text FROM office_agent_runs WHERE user_id=$1 AND idempotency_key=$2`, in.UserID, in.IdempotencyKey).Scan(&existingID, &hash, &conversationID)
	if err == nil {
		if hash != in.RequestHash || conversationID != in.ConversationID {
			return nil, false, service.ErrOfficeConflict
		}
		run, err := scanOfficeRun(tx.QueryRowContext(ctx, `SELECT `+officeRunColumns+` FROM office_agent_runs WHERE id=$1`, existingID))
		if err != nil {
			return nil, false, err
		}
		return run, false, tx.Commit()
	}
	if !errors.Is(err, sql.ErrNoRows) {
		return nil, false, err
	}
	var active int
	if err := tx.QueryRowContext(ctx, `SELECT COUNT(*) FROM office_agent_runs WHERE user_id=$1 AND mode=$2 AND status IN ('queued','running','recovering')`, in.UserID, in.Mode).Scan(&active); err != nil {
		return nil, false, err
	}
	maxActive := in.MaxActiveRuns
	if maxActive <= 0 {
		maxActive = 1
	}
	if !in.BrowserDriven && active >= maxActive {
		return nil, false, service.ErrOfficeActiveRun
	}
	var found int
	if err := tx.QueryRowContext(ctx, `SELECT 1 FROM api_keys WHERE id=$1 AND user_id=$2 AND group_id=$3 AND deleted_at IS NULL`, in.APIKeyID, in.UserID, in.GroupID).Scan(&found); err != nil {
		return nil, false, officeDBError(err)
	}
	var seq int64
	if err := tx.QueryRowContext(ctx, `UPDATE office_agent_conversations SET next_message_seq=next_message_seq+1,updated_at=NOW() WHERE user_id=$1 AND id=$2 AND deleted_at IS NULL RETURNING next_message_seq`, in.UserID, in.ConversationID).Scan(&seq); err != nil {
		return nil, false, officeDBError(err)
	}
	if in.RegenerationOf == "" {
		in.ContextBoundary = seq - 1
	} else {
		var boundary int64
		if err = tx.QueryRowContext(ctx, `SELECT context_boundary FROM office_agent_runs WHERE id=$1 AND user_id=$2 AND conversation_id=$3 AND status IN ('completed','partially_completed') AND regeneration_of IS NULL`, in.RegenerationOf, in.UserID, in.ConversationID).Scan(&boundary); err != nil {
			return nil, false, officeDBError(err)
		}
		in.ContextBoundary = boundary
	}
	limits, _ := json.Marshal(in.Limits)
	if in.InputResources == nil {
		in.InputResources = []service.OfficeResourceVersion{}
	}
	refs, _ := json.Marshal(in.InputResources)
	_, err = tx.ExecContext(ctx, `INSERT INTO office_agent_runs(id,user_id,conversation_id,idempotency_key,request_hash,api_key_id,group_id,model,limits_json,input_resources,mode,search_enabled,parameters,context_boundary,regeneration_of,browser_driven) VALUES($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13,$14,NULLIF($15,'')::uuid,$16)`, in.RunID, in.UserID, in.ConversationID, in.IdempotencyKey, in.RequestHash, in.APIKeyID, in.GroupID, in.Model, limits, refs, in.Mode, in.SearchEnabled, []byte(in.Parameters), in.ContextBoundary, in.RegenerationOf, in.BrowserDriven)
	if err != nil {
		return nil, false, officeDBError(err)
	}
	_, err = tx.ExecContext(ctx, `INSERT INTO office_agent_messages(id,user_id,conversation_id,run_id,sequence,role,content) VALUES($1,$2,$3,$4,$5,'user',$6)`, uuid.NewString(), in.UserID, in.ConversationID, in.RunID, seq, []byte(in.UserContent))
	if err != nil {
		return nil, false, officeDBError(err)
	}
	if !in.BrowserDriven {
		if _, err = tx.ExecContext(ctx, `INSERT INTO office_agent_run_outbox(run_id) VALUES($1)`, in.RunID); err != nil {
			return nil, false, err
		}
	}
	if _, err = appendOfficeEvent(ctx, tx, in.UserID, in.RunID, "system:queued", "run.queued", json.RawMessage(`{}`)); err != nil {
		return nil, false, err
	}
	run, err := scanOfficeRun(tx.QueryRowContext(ctx, `SELECT `+officeRunColumns+` FROM office_agent_runs WHERE id=$1`, in.RunID))
	if err != nil {
		return nil, false, err
	}
	if err = officePinRunInputs(ctx, tx, *run); err != nil {
		return nil, false, err
	}
	return run, true, tx.Commit()
}

func (r *officeAgentRepository) GetRun(ctx context.Context, userID int64, id string) (*service.OfficeRun, error) {
	if !officeUUID(id) {
		return nil, service.ErrOfficeNotFound
	}
	return scanOfficeRun(r.db.QueryRowContext(ctx, `SELECT `+officeRunColumns+` FROM office_agent_runs WHERE user_id=$1 AND id=$2`, userID, id))
}

func (r *officeAgentRepository) ListRuns(ctx context.Context, userID int64, conversationID string, limit int) ([]service.OfficeRun, error) {
	if !officeUUID(conversationID) {
		return nil, service.ErrOfficeNotFound
	}
	rows, err := r.db.QueryContext(ctx, `SELECT `+officeRunColumns+` FROM office_agent_runs WHERE user_id=$1 AND conversation_id=$2 ORDER BY created_at DESC,id LIMIT $3`, userID, conversationID, officeLimit(limit))
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()
	result := []service.OfficeRun{}
	for rows.Next() {
		run, err := scanOfficeRun(rows)
		if err != nil {
			return nil, err
		}
		result = append(result, *run)
	}
	return result, rows.Err()
}

func officeLeaseValidDuration(d time.Duration) bool { return d >= time.Second && d <= 5*time.Minute }

func (r *officeAgentRepository) ClaimRun(ctx context.Context, owner string, duration time.Duration) (*service.OfficeRunLease, error) {
	return r.ClaimPreset(ctx, owner, "office", duration)
}
func (r *officeAgentRepository) ClaimPreset(ctx context.Context, owner, preset string, duration time.Duration) (*service.OfficeRunLease, error) {
	if preset != "office" && preset != "chat" {
		return nil, service.ErrOfficeInvalid
	}

	if !officeToken.MatchString(owner) || !officeLeaseValidDuration(duration) {
		return nil, service.ErrOfficeInvalid
	}
	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return nil, err
	}
	defer func() { _ = tx.Rollback() }()
	var id string
	err = tx.QueryRowContext(ctx, `SELECT id::text FROM office_agent_runs WHERE mode=$1 AND NOT browser_driven AND (status='queued' OR (status IN ('running','recovering') AND lease_until<clock_timestamp())) ORDER BY created_at,id FOR UPDATE SKIP LOCKED LIMIT 1`, preset).Scan(&id)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	var needsConfirmation bool
	if err := tx.QueryRowContext(ctx, `SELECT EXISTS(SELECT 1 FROM office_agent_operations WHERE run_id=$1 AND (state IN ('dispatched','unknown') OR (state='completed' AND NOT EXISTS(SELECT 1 FROM office_checkpoints.snapshots WHERE run_id=$1))))`, id).Scan(&needsConfirmation); err != nil {
		return nil, err
	}
	if needsConfirmation {
		_, err = tx.ExecContext(ctx, `UPDATE office_agent_operations SET state='unknown' WHERE run_id=$1 AND state='dispatched'`, id)
		if err != nil {
			return nil, err
		}
		var userID int64
		err = tx.QueryRowContext(ctx, `UPDATE office_agent_runs SET status='needs_confirmation',lease_epoch=lease_epoch+1,lease_token=NULL,lease_until=NULL,lease_owner=NULL,error_code='RECOVERY_CONFIRMATION_REQUIRED',error_message='The previous execution stopped after an operation was dispatched; automatic replay is disabled.',updated_at=NOW() WHERE id=$1 RETURNING user_id`, id).Scan(&userID)
		if err != nil {
			return nil, err
		}
		if _, err = appendOfficeEvent(ctx, tx, userID, id, "system:recovery-confirmation", "run.needs_confirmation", json.RawMessage(`{"code":"RECOVERY_CONFIRMATION_REQUIRED"}`)); err != nil {
			return nil, err
		}
		return nil, tx.Commit()
	}
	// Reclaiming a worker that never dispatched is safe, but it must not reset
	// the run's total time budget. Otherwise an old unspent run is handed out
	// forever with a credential that is already expired.
	var expired bool
	if err := tx.QueryRowContext(ctx, `SELECT COALESCE(started_at + (((limits_json->>'max_run_seconds')::bigint+paused_seconds) * INTERVAL '1 second') <= clock_timestamp(),false) FROM office_agent_runs WHERE id=$1`, id).Scan(&expired); err != nil {
		return nil, err
	}
	if expired {
		var userID int64
		if err := tx.QueryRowContext(ctx, `UPDATE office_agent_runs SET status='failed',lease_epoch=lease_epoch+1,lease_token=NULL,lease_until=NULL,lease_owner=NULL,error_code='RUN_TIME_LIMIT',error_message='The run time budget expired before an operation was dispatched.',finished_at=NOW(),updated_at=NOW() WHERE id=$1 RETURNING user_id`, id).Scan(&userID); err != nil {
			return nil, err
		}
		if _, err := appendOfficeEvent(ctx, tx, userID, id, "system:time-limit", "run.failed", json.RawMessage(`{"code":"RUN_TIME_LIMIT"}`)); err != nil {
			return nil, err
		}
		return nil, tx.Commit()
	}
	lease := &service.OfficeRunLease{}
	run, err := scanOfficeRun(tx.QueryRowContext(ctx, `UPDATE office_agent_runs SET status='running',lease_owner=$2,lease_token=$3,lease_epoch=lease_epoch+1,lease_until=clock_timestamp()+($4*INTERVAL '1 millisecond'),started_at=COALESCE(started_at,NOW()),updated_at=NOW() WHERE id=$1 RETURNING `+officeRunColumns+`,lease_owner,lease_token::text,lease_epoch,lease_until`, id, owner, uuid.NewString(), duration.Milliseconds()), &lease.Owner, &lease.Token, &lease.Epoch, &lease.Until)
	if err != nil {
		return nil, err
	}
	lease.Run = *run
	if _, err = tx.ExecContext(ctx, `UPDATE office_agent_run_outbox SET claimed_at=NOW() WHERE run_id=$1`, id); err != nil {
		return nil, err
	}
	payload, _ := json.Marshal(map[string]any{"epoch": lease.Epoch})
	if _, err = appendOfficeEvent(ctx, tx, run.UserID, id, "system:claimed:"+lease.Token, "run.running", payload); err != nil {
		return nil, err
	}
	return lease, tx.Commit()
}

func checkOfficeLease(ctx context.Context, tx *sql.Tx, lease service.OfficeRunLease) (*service.OfficeRun, error) {
	if !officeUUID(lease.Run.ID) || !officeUUID(lease.Token) || lease.Epoch <= 0 {
		return nil, service.ErrOfficeLeaseLost
	}
	run, err := scanOfficeRun(tx.QueryRowContext(ctx, `SELECT `+officeRunColumns+` FROM office_agent_runs WHERE id=$1 AND user_id=$2 AND status='running' AND lease_owner=$3 AND lease_token=$4 AND lease_epoch=$5 FOR UPDATE`, lease.Run.ID, lease.Run.UserID, lease.Owner, lease.Token, lease.Epoch))
	if errors.Is(err, service.ErrOfficeNotFound) {
		return nil, service.ErrOfficeLeaseLost
	}
	if err != nil {
		return nil, err
	}
	// Check wall-clock expiry only after taking the row lock. NOW() is the
	// transaction start time and can incorrectly admit an expired lease after a
	// request has waited for another transaction's lock.
	var live bool
	if err := tx.QueryRowContext(ctx, `SELECT COALESCE(lease_until>clock_timestamp() AND (browser_driven OR started_at + (($2+paused_seconds) * INTERVAL '1 second') > clock_timestamp()),false) FROM office_agent_runs WHERE id=$1`, run.ID, run.Limits.MaxRunSeconds).Scan(&live); err != nil {
		return nil, err
	}
	if !live {
		return nil, service.ErrOfficeLeaseLost
	}
	return run, nil
}

func (r *officeAgentRepository) CheckLease(ctx context.Context, lease service.OfficeRunLease) error {
	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback() }()
	if _, err = checkOfficeLease(ctx, tx, lease); err != nil {
		return err
	}
	return tx.Commit()
}

func (r *officeAgentRepository) RenewLease(ctx context.Context, lease service.OfficeRunLease, duration time.Duration) (*service.OfficeRunLease, error) {
	if !officeLeaseValidDuration(duration) {
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
	if err = tx.QueryRowContext(ctx, `UPDATE office_agent_runs SET lease_until=clock_timestamp()+($2*INTERVAL '1 millisecond'),updated_at=NOW() WHERE id=$1 RETURNING lease_until`, run.ID, duration.Milliseconds()).Scan(&lease.Until); err != nil {
		return nil, err
	}
	lease.Run = *run
	return &lease, tx.Commit()
}

func (r *officeAgentRepository) CancelRun(ctx context.Context, userID int64, id string) (*service.OfficeRun, error) {
	if !officeUUID(id) {
		return nil, service.ErrOfficeNotFound
	}
	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return nil, err
	}
	defer func() { _ = tx.Rollback() }()
	run, err := scanOfficeRun(tx.QueryRowContext(ctx, `SELECT `+officeRunColumns+` FROM office_agent_runs WHERE user_id=$1 AND id=$2 FOR UPDATE`, userID, id))
	if err != nil {
		return nil, err
	}
	if officeRunActive(run.Status) {
		if _, err = tx.ExecContext(ctx, `UPDATE office_agent_runs SET status='cancelled',lease_epoch=lease_epoch+1,lease_token=NULL,lease_until=NULL,lease_owner=NULL,finished_at=NOW(),updated_at=NOW() WHERE id=$1`, id); err != nil {
			return nil, err
		}
		if _, err = appendOfficeEvent(ctx, tx, userID, id, "system:cancelled", "run.cancelled", json.RawMessage(`{}`)); err != nil {
			return nil, err
		}
	}
	run, err = scanOfficeRun(tx.QueryRowContext(ctx, `SELECT `+officeRunColumns+` FROM office_agent_runs WHERE id=$1`, id))
	if err != nil {
		return nil, err
	}
	return run, tx.Commit()
}

func officeRunActive(status string) bool {
	switch status {
	case service.OfficeRunQueued, service.OfficeRunRunning, service.OfficeRunWaitingUser, service.OfficeRunRecovering, service.OfficeRunNeedsConfirmation:
		return true
	}
	return false
}

func (r *officeAgentRepository) SuspendRun(ctx context.Context, lease service.OfficeRunLease, code, message string) (*service.OfficeRun, error) {
	if !officeUUID(lease.Run.ID) || !officeUUID(lease.Token) || lease.Epoch <= 0 || len(code) > 64 || len(message) > 1000 {
		return nil, service.ErrOfficeInvalid
	}
	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return nil, err
	}
	defer func() { _ = tx.Rollback() }()
	// A task time limit can expire just before this report. Exact identity is
	// sufficient to stop it, but never to dispatch another model/tool call.
	run, err := scanOfficeRun(tx.QueryRowContext(ctx, `SELECT `+officeRunColumns+` FROM office_agent_runs WHERE id=$1 AND user_id=$2 AND status='running' AND lease_owner=$3 AND lease_token=$4 AND lease_epoch=$5 FOR UPDATE`, lease.Run.ID, lease.Run.UserID, lease.Owner, lease.Token, lease.Epoch))
	if errors.Is(err, service.ErrOfficeNotFound) {
		return nil, service.ErrOfficeLeaseLost
	}
	if err != nil {
		return nil, err
	}
	// Preserve original dispatch_epoch/token: a late original response may be
	// deposited, but cannot resume or finish this fenced run.
	if _, err = tx.ExecContext(ctx, `UPDATE office_agent_operations SET state='unknown' WHERE run_id=$1 AND state='dispatched'`, run.ID); err != nil {
		return nil, err
	}
	if _, err = tx.ExecContext(ctx, `UPDATE office_agent_runs SET status='needs_confirmation',error_code=$2,error_message=$3,lease_epoch=lease_epoch+1,lease_token=NULL,lease_until=NULL,lease_owner=NULL,updated_at=NOW() WHERE id=$1`, run.ID, code, message); err != nil {
		return nil, err
	}
	payload, _ := json.Marshal(map[string]string{"error_code": code, "error_message": message})
	if _, err = appendOfficeEvent(ctx, tx, run.UserID, run.ID, "system:suspended", "run.needs_confirmation", payload); err != nil {
		return nil, err
	}
	run, err = scanOfficeRun(tx.QueryRowContext(ctx, `SELECT `+officeRunColumns+` FROM office_agent_runs WHERE id=$1`, run.ID))
	if err != nil {
		return nil, err
	}
	return run, tx.Commit()
}

func (r *officeAgentRepository) FinishRun(ctx context.Context, lease service.OfficeRunLease, status string, content json.RawMessage, code, message string) (*service.OfficeRun, error) {
	if status != service.OfficeRunCompleted && status != service.OfficeRunPartiallyCompleted && status != service.OfficeRunFailed {
		return nil, service.ErrOfficeInvalid
	}
	if len(code) > 64 || len(message) > 4096 || len(content) > 0 && !officeJSON(content, service.OfficeMaxOperationResultBytes) {
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
	var pending bool
	if err = tx.QueryRowContext(ctx, `SELECT EXISTS(SELECT 1 FROM office_agent_operations WHERE run_id=$1 AND state IN ('dispatched','unknown'))`, run.ID).Scan(&pending); err != nil {
		return nil, err
	}
	if pending {
		return nil, service.ErrOfficeResultUnknown
	}
	if status != service.OfficeRunFailed {
		if err = tx.QueryRowContext(ctx, `SELECT EXISTS(SELECT 1 FROM office_agent_operations WHERE run_id=$1 AND state='prepared')`, run.ID).Scan(&pending); err != nil {
			return nil, err
		}
		if pending {
			return nil, service.ErrOfficeConflict
		}
	}
	if status == service.OfficeRunCompleted || status == service.OfficeRunPartiallyCompleted {
		if run.RegenerationOf != "" {
			if _, err = tx.ExecContext(ctx, `UPDATE office_agent_runs SET selected=(id=$4) WHERE user_id=$1 AND conversation_id=$2 AND (id=$3 OR regeneration_of=$3)`, run.UserID, run.ConversationID, run.RegenerationOf, run.ID); err != nil {
				return nil, err
			}
			if err = invalidateWorkbenchReplayAfter(ctx, tx, run.UserID, run.ConversationID, run.RegenerationOf); err != nil {
				return nil, err
			}
		} else if run.BrowserDriven && status == service.OfficeRunCompleted {
			if err = publishWorkbenchReplayCheckpoint(ctx, tx, *run); err != nil {
				return nil, err
			}
		}
	}
	if len(content) > 0 {
		var seq int64
		if err = tx.QueryRowContext(ctx, `UPDATE office_agent_conversations SET next_message_seq=next_message_seq+1,updated_at=NOW() WHERE id=$1 RETURNING next_message_seq`, run.ConversationID).Scan(&seq); err != nil {
			return nil, err
		}
		if _, err = tx.ExecContext(ctx, `INSERT INTO office_agent_messages(id,user_id,conversation_id,run_id,sequence,role,content) VALUES($1,$2,$3,$4,$5,'assistant',$6)`, uuid.NewString(), run.UserID, run.ConversationID, run.ID, seq, []byte(content)); err != nil {
			return nil, officeDBError(err)
		}
	}
	if _, err = tx.ExecContext(ctx, `UPDATE office_agent_runs SET status=$2,error_code=$3,error_message=$4,finished_at=NOW(),updated_at=NOW(),lease_token=NULL,lease_until=NULL,lease_owner=NULL WHERE id=$1`, run.ID, status, code, message); err != nil {
		return nil, err
	}
	payload, _ := json.Marshal(map[string]string{"status": status, "error_code": code, "error_message": message})
	if _, err = appendOfficeEvent(ctx, tx, run.UserID, run.ID, "system:finished", "run."+status, payload); err != nil {
		return nil, err
	}
	run, err = scanOfficeRun(tx.QueryRowContext(ctx, `SELECT `+officeRunColumns+` FROM office_agent_runs WHERE id=$1`, run.ID))
	if err != nil {
		return nil, err
	}
	return run, tx.Commit()
}
