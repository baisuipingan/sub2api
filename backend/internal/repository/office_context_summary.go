package repository

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"errors"
	"github.com/Wei-Shaw/sub2api/internal/service"
	"strings"
	"unicode/utf8"
)

func (r *officeAgentRepository) RegenerationSource(ctx context.Context, user int64, conversation, id string) (*service.OfficeRun, *service.OfficeMessage, error) {
	run, e := r.GetRun(ctx, user, id)
	if e != nil {
		return nil, nil, e
	}
	if run.ConversationID != conversation || (run.Status != service.OfficeRunCompleted && run.Status != service.OfficeRunPartiallyCompleted) {
		return nil, nil, service.ErrOfficeConflict
	}
	if run.RegenerationOf != "" {
		run, e = r.GetRun(ctx, user, run.RegenerationOf)
		if e != nil {
			return nil, nil, e
		}
	}
	m := &service.OfficeMessage{}
	e = r.db.QueryRowContext(ctx, `SELECT id::text,conversation_id::text,run_id::text,sequence,role,content,created_at FROM office_agent_messages WHERE user_id=$1 AND run_id=$2 AND role='user'`, user, run.ID).Scan(&m.ID, &m.ConversationID, &m.RunID, &m.Sequence, &m.Role, &m.Content, &m.CreatedAt)
	return run, m, officeDBError(e)
}

func officeContextSourceHash(rows []service.OfficeMessage, through string) (string, bool) {
	ids := make([]string, 0, len(rows))
	for _, m := range rows {
		ids = append(ids, m.ID)
		if m.ID == through {
			if m.Role != "assistant" {
				return "", false
			}
			sum := sha256.Sum256([]byte(strings.Join(ids, "\n")))
			return hex.EncodeToString(sum[:]), true
		}
	}
	return "", false
}

func (r *officeAgentRepository) ContextSummary(ctx context.Context, lease service.OfficeRunLease) (*service.OfficeContextSummary, error) {
	s := &service.OfficeContextSummary{}
	e := r.db.QueryRowContext(ctx, `SELECT version,content,through_message_id::text,source_hash FROM office_agent_context_summaries WHERE conversation_id=$1 AND user_id=$2`, lease.Run.ConversationID, lease.Run.UserID).Scan(&s.Version, &s.Content, &s.ThroughMessageID, &s.SourceHash)
	if errors.Is(e, sql.ErrNoRows) {
		return s, nil
	}
	if e != nil {
		return nil, e
	}
	rows, e := r.ContextMessages(ctx, lease.Run)
	if e != nil {
		return nil, e
	}
	hash, ok := officeContextSourceHash(rows, s.ThroughMessageID)
	if !ok || hash != s.SourceHash {
		return &service.OfficeContextSummary{Version: s.Version}, nil
	}
	return s, nil
}

func (r *officeAgentRepository) SaveContextSummary(ctx context.Context, lease service.OfficeRunLease, s service.OfficeContextSummary, expected int64, operation string) (int64, error) {
	if expected < 0 || !officeToken.MatchString(operation) || len(s.Content) == 0 || len(s.Content) > 16<<10 || !utf8.ValidString(s.Content) || !officeUUID(s.ThroughMessageID) || !officeHash.MatchString(s.SourceHash) {
		return 0, service.ErrOfficeInvalid
	}
	rows, e := r.ContextMessages(ctx, lease.Run)
	if e != nil {
		return 0, e
	}
	hash, ok := officeContextSourceHash(rows, s.ThroughMessageID)
	if !ok || hash != s.SourceHash {
		return 0, service.ErrOfficeConflict
	}
	tx, e := r.db.BeginTx(ctx, nil)
	if e != nil {
		return 0, e
	}
	defer func() { _ = tx.Rollback() }()
	if _, e = checkOfficeLease(ctx, tx, lease); e != nil {
		return 0, e
	}
	var version int64
	var oldop, content, oldhash string
	e = tx.QueryRowContext(ctx, `SELECT version,operation_id,content,source_hash FROM office_agent_context_summaries WHERE conversation_id=$1 FOR UPDATE`, lease.Run.ConversationID).Scan(&version, &oldop, &content, &oldhash)
	if e != nil && !errors.Is(e, sql.ErrNoRows) {
		return 0, e
	}
	if oldop == operation {
		if oldhash != s.SourceHash || content != s.Content {
			return 0, service.ErrOfficeConflict
		}
		return version, tx.Commit()
	}
	if version != expected {
		return 0, service.ErrOfficeConflict
	}
	e = tx.QueryRowContext(ctx, `INSERT INTO office_agent_context_summaries(conversation_id,user_id,version,operation_id,content,through_message_id,source_hash) VALUES($1,$2,$3,$4,$5,$6,$7) ON CONFLICT(conversation_id) DO UPDATE SET version=EXCLUDED.version,operation_id=EXCLUDED.operation_id,content=EXCLUDED.content,through_message_id=EXCLUDED.through_message_id,source_hash=EXCLUDED.source_hash,updated_at=NOW() RETURNING version`, lease.Run.ConversationID, lease.Run.UserID, expected+1, operation, s.Content, s.ThroughMessageID, s.SourceHash).Scan(&version)
	if e != nil {
		return 0, e
	}
	return version, tx.Commit()
}
