package repository

import (
	"context"
	"database/sql"
	"errors"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/google/uuid"
)

const (
	workbenchReplayMaxTurns       = 1024
	workbenchReplayMaxVisibleSize = 8 << 20
	workbenchReplayMaxSummarySize = 64 << 10
)

func (r *officeAgentRepository) BrowserReplayState(ctx context.Context, run service.OfficeRun) (service.OfficeReplayState, error) {
	var state service.OfficeReplayState
	tx, err := r.db.BeginTx(ctx, &sql.TxOptions{Isolation: sql.LevelRepeatableRead, ReadOnly: true})
	if err != nil {
		return state, err
	}
	defer func() { _ = tx.Rollback() }()

	var currentSequence, boundary int64
	var checkpointID string
	err = tx.QueryRowContext(ctx, `SELECT m.sequence, r.context_boundary,
		r.replay_pinned, r.replay_native_from, COALESCE(r.replay_checkpoint_id::text,'')
	FROM office_agent_runs r
	JOIN office_agent_conversations c ON c.id=r.conversation_id AND c.user_id=r.user_id AND c.deleted_at IS NULL
	JOIN office_agent_messages m ON m.run_id=COALESCE(r.regeneration_of,r.id) AND m.role='user'
	WHERE r.id=$1 AND r.user_id=$2 AND r.conversation_id=$3 AND r.browser_driven`,
		run.ID, run.UserID, run.ConversationID).Scan(&currentSequence, &boundary, &state.Pinned, &state.NativeFrom, &checkpointID)
	if err != nil {
		return state, officeDBError(err)
	}
	checkpoint, err := loadWorkbenchReplayCheckpoint(ctx, tx, run, checkpointID, currentSequence, state.Pinned)
	if err != nil {
		return state, err
	}
	state.Checkpoint = checkpoint
	var before int64
	if checkpoint != nil {
		before = checkpoint.BeforeSequence
	}
	state.Turns, state.Truncated, err = loadWorkbenchReplayTurns(ctx, tx, run, currentSequence, boundary, before, workbenchReplayMaxVisibleSize)
	if err != nil {
		return state, err
	}
	return state, tx.Commit()
}

func loadWorkbenchReplayCheckpoint(ctx context.Context, tx *sql.Tx, run service.OfficeRun, id string, currentSequence int64, pinned bool) (*service.OfficeReplayCheckpoint, error) {
	if pinned && id == "" {
		return nil, nil
	}
	checkpoint := &service.OfficeReplayCheckpoint{}
	err := tx.QueryRowContext(ctx, `SELECT id::text,before_sequence,content
	FROM office_agent_replay_checkpoints WHERE user_id=$1 AND conversation_id=$2 AND before_sequence<=$3
		AND (($4 AND id=NULLIF($5,'')::uuid) OR (NOT $4 AND published))`,
		run.UserID, run.ConversationID, currentSequence, pinned, id).Scan(&checkpoint.ID, &checkpoint.BeforeSequence, &checkpoint.Content)
	if errors.Is(err, sql.ErrNoRows) {
		if pinned {
			return nil, service.ErrOfficeResultUnknown
		}
		return nil, nil
	}
	return checkpoint, err
}

const workbenchReplayTurnsQuery = `
WITH roots AS (
	SELECT root.id,m.sequence
	FROM office_agent_messages m
	JOIN office_agent_runs root ON root.id=m.run_id AND root.user_id=m.user_id AND root.regeneration_of IS NULL
	WHERE m.user_id=$1 AND m.conversation_id=$2 AND m.role='user'
		AND m.sequence BETWEEN $3 AND $4 AND m.sequence<$5
		AND root.status IN ('completed','partially_completed','failed','cancelled')
	ORDER BY m.sequence DESC LIMIT $6
), chosen AS (
	SELECT roots.sequence,COALESCE(variant.id,roots.id) AS run_id
	FROM roots
	LEFT JOIN LATERAL (
		SELECT id FROM office_agent_runs
		WHERE user_id=$1 AND conversation_id=$2 AND regeneration_of=roots.id AND selected
			AND status IN ('completed','partially_completed')
		ORDER BY created_at DESC,id DESC LIMIT 1
	) variant ON TRUE
	UNION ALL SELECT $5,$7::uuid
), candidates AS (
	SELECT chosen.sequence,chosen.run_id,question.id AS question_id,answer.id AS answer_id,
		SUM(octet_length(question.content::text)::bigint+COALESCE(octet_length(answer.content::text),0))
			OVER (ORDER BY chosen.sequence DESC ROWS UNBOUNDED PRECEDING) AS visible_bytes,
		COUNT(*) OVER () AS available_turns
	FROM chosen
	JOIN office_agent_runs r ON r.id=chosen.run_id AND r.user_id=$1 AND r.conversation_id=$2
	JOIN office_agent_messages question ON question.run_id=r.id AND question.role='user'
	LEFT JOIN office_agent_messages answer ON answer.run_id=r.id AND answer.role='assistant' AND r.selected
		AND r.status IN ('completed','partially_completed') AND r.id<>$7
), retained AS (
	SELECT * FROM candidates WHERE sequence=$5 OR visible_bytes<=$8
	ORDER BY sequence DESC LIMIT $9
)
SELECT retained.sequence,r.id::text,r.group_id,r.model,r.parameters,r.search_enabled,
	question.id::text,question.sequence,question.content,question.created_at,
	COALESCE(answer.id::text,''),COALESCE(answer.sequence,0),COALESCE(answer.content,'null'::jsonb),answer.created_at,
	COALESCE(r.browser_driven AND r.replay_pinned AND r.selected AND r.status='completed'
		AND r.parameters->>'history_version'='2' AND r.replay_invalidated_at IS NULL
		AND r.payloads_compacted_at IS NULL,FALSE),r.replay_native_from,COALESCE(r.replay_checkpoint_id::text,''),
	retained.available_turns
FROM retained
JOIN office_agent_runs r ON r.id=retained.run_id
JOIN office_agent_messages question ON question.id=retained.question_id
LEFT JOIN office_agent_messages answer ON answer.id=retained.answer_id
ORDER BY retained.sequence`

func loadWorkbenchReplayTurns(ctx context.Context, tx *sql.Tx, run service.OfficeRun, currentSequence, boundary, before int64, maxVisibleSize int) ([]service.OfficeReplayTurn, bool, error) {
	// Count serialized bytes before returning content to the application.
	// Include one extra older candidate to detect truncation by turn count.
	rows, err := tx.QueryContext(ctx, workbenchReplayTurnsQuery, run.UserID, run.ConversationID, before, boundary, currentSequence,
		workbenchReplayMaxTurns+1, run.ID, maxVisibleSize, workbenchReplayMaxTurns+1)
	if err != nil {
		return nil, false, err
	}
	defer func() { _ = rows.Close() }()
	var turns []service.OfficeReplayTurn
	var availableTurns int
	for rows.Next() {
		var turn service.OfficeReplayTurn
		var answer service.OfficeMessage
		var answeredAt sql.NullTime
		if err := rows.Scan(&turn.Sequence, &turn.RunID, &turn.GroupID, &turn.Model, &turn.Parameters, &turn.SearchEnabled,
			&turn.User.ID, &turn.User.Sequence, &turn.User.Content, &turn.User.CreatedAt,
			&answer.ID, &answer.Sequence, &answer.Content, &answeredAt, &turn.NativeAvailable, &turn.NativeFrom, &turn.CheckpointID, &availableTurns); err != nil {
			return nil, false, err
		}
		turn.User.RunID = turn.RunID
		turn.User.ConversationID = run.ConversationID
		turn.User.Role = "user"
		if answer.ID != "" {
			answer.ConversationID = run.ConversationID
			answer.RunID = turn.RunID
			answer.Role = "assistant"
			answer.CreatedAt = answeredAt.Time
			turn.Assistant = &answer
		}
		turns = append(turns, turn)
	}
	if err := rows.Err(); err != nil {
		return nil, false, err
	}
	if len(turns) == 0 || turns[len(turns)-1].RunID != run.ID {
		return nil, false, service.ErrOfficeResultUnknown
	}
	return turns, len(turns) < availableTurns, nil
}

// PinBrowserReplay records a run's context choices before its first model
// operation. The checkpoint can be shared by later turns after this run
// completes. Retry uses the persisted choice even across different leases.
func (r *officeAgentRepository) PinBrowserReplay(ctx context.Context, lease service.OfficeRunLease, checkpoint *service.OfficeReplayCheckpoint, nativeFrom int64) error {
	if nativeFrom < 0 {
		return service.ErrOfficeInvalid
	}
	if checkpoint != nil && (checkpoint.BeforeSequence <= 0 || !officeJSON(checkpoint.Content, workbenchReplayMaxSummarySize) ||
		(checkpoint.ID != "" && !officeUUID(checkpoint.ID))) {
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
	var owner int64
	if err = tx.QueryRowContext(ctx, `SELECT user_id FROM office_agent_conversations
		WHERE id=$1 AND user_id=$2 AND deleted_at IS NULL FOR UPDATE`, run.ConversationID, run.UserID).Scan(&owner); err != nil {
		return officeDBError(err)
	}
	var earlierID string
	var currentSequence, earlierFrom int64
	var pinned bool
	err = tx.QueryRowContext(ctx, `SELECT m.sequence,r.replay_pinned,r.replay_native_from,COALESCE(r.replay_checkpoint_id::text,'')
		FROM office_agent_runs r
		JOIN office_agent_messages m ON m.run_id=COALESCE(r.regeneration_of,r.id) AND m.role='user'
		WHERE r.id=$1 AND r.browser_driven AND r.parameters->>'history_version'='2'`, run.ID).
		Scan(&currentSequence, &pinned, &earlierFrom, &earlierID)
	if err != nil {
		return officeDBError(err)
	}
	if nativeFrom > currentSequence || (checkpoint != nil && checkpoint.BeforeSequence > currentSequence) {
		return service.ErrOfficeInvalid
	}
	var checkpointID string
	if checkpoint != nil {
		checkpointID = checkpoint.ID
		if pinned && earlierID == "" {
			return service.ErrOfficeConflict
		}
		if pinned && checkpointID == "" {
			checkpointID = earlierID
		}
		if checkpointID == "" {
			checkpointID = uuid.NewString()
			_, err = tx.ExecContext(ctx, `INSERT INTO office_agent_replay_checkpoints(id,user_id,conversation_id,before_sequence,content)
				VALUES ($1,$2,$3,$4,$5)`, checkpointID, run.UserID, run.ConversationID, checkpoint.BeforeSequence, []byte(checkpoint.Content))
		} else {
			var same bool
			err = tx.QueryRowContext(ctx, `SELECT before_sequence=$4 AND content=$5::jsonb
				FROM office_agent_replay_checkpoints WHERE id=$1 AND user_id=$2 AND conversation_id=$3
				AND (published OR $6) FOR UPDATE`,
				checkpointID, run.UserID, run.ConversationID, checkpoint.BeforeSequence, []byte(checkpoint.Content), pinned).Scan(&same)
			if errors.Is(err, sql.ErrNoRows) || err == nil && !same {
				return service.ErrOfficeConflict
			}
		}
		if err != nil {
			return err
		}
	}
	if pinned {
		if earlierID != checkpointID || earlierFrom != nativeFrom {
			return service.ErrOfficeConflict
		}
		return tx.Commit()
	}
	if nativeFrom > 0 && nativeFrom < currentSequence {
		turns, truncated, err := loadWorkbenchReplayTurns(ctx, tx, *run, currentSequence, run.ContextBoundary, nativeFrom, workbenchReplayMaxVisibleSize)
		if err != nil {
			return err
		}
		if truncated || turns[0].Sequence != nativeFrom {
			return service.ErrOfficeResultUnknown
		}
		for _, turn := range turns[:len(turns)-1] {
			if !turn.NativeAvailable {
				return service.ErrOfficeResultUnknown
			}
		}
	}
	if _, err = tx.ExecContext(ctx, `UPDATE office_agent_runs SET replay_pinned=TRUE,
		replay_checkpoint_id=NULLIF($2,'')::uuid,replay_native_from=$3 WHERE id=$1`, run.ID, checkpointID, nativeFrom); err != nil {
		return err
	}
	return tx.Commit()
}

// Regeneration can build a private checkpoint at an earlier turn. Keep the
// conversation checkpoint at the latest successful ordinary turn instead.
func publishWorkbenchReplayCheckpoint(ctx context.Context, tx *sql.Tx, run service.OfficeRun) error {
	var id string
	var before int64
	err := tx.QueryRowContext(ctx, `SELECT c.id::text,c.before_sequence FROM office_agent_runs r
		JOIN office_agent_replay_checkpoints c ON c.id=r.replay_checkpoint_id
		WHERE r.id=$1 AND r.replay_pinned AND c.user_id=$2 AND c.conversation_id=$3
		AND NOT c.published`, run.ID, run.UserID, run.ConversationID).Scan(&id, &before)
	if errors.Is(err, sql.ErrNoRows) {
		return nil
	}
	if err != nil {
		return err
	}
	if _, err = tx.ExecContext(ctx, `UPDATE office_agent_replay_checkpoints SET published=FALSE
		WHERE conversation_id=$1 AND published AND before_sequence<$2`, run.ConversationID, before); err != nil {
		return err
	}
	_, err = tx.ExecContext(ctx, `UPDATE office_agent_replay_checkpoints SET published=TRUE
		WHERE id=$1 AND NOT EXISTS (SELECT 1 FROM office_agent_replay_checkpoints
			WHERE conversation_id=$2 AND published)`, id, run.ConversationID)
	return err
}

func invalidateWorkbenchReplayAfter(ctx context.Context, tx *sql.Tx, userID int64, conversationID, rootRunID string) error {
	var sequence int64
	err := tx.QueryRowContext(ctx, `SELECT sequence FROM office_agent_messages
		WHERE user_id=$1 AND conversation_id=$2 AND run_id=$3 AND role='user'`, userID, conversationID, rootRunID).Scan(&sequence)
	if err != nil {
		return officeDBError(err)
	}
	if _, err = tx.ExecContext(ctx, `UPDATE office_agent_replay_checkpoints SET published=FALSE
		WHERE user_id=$1 AND conversation_id=$2 AND published AND before_sequence>$3`, userID, conversationID, sequence); err != nil {
		return err
	}
	_, err = tx.ExecContext(ctx, `UPDATE office_agent_runs r SET replay_invalidated_at=NOW()
		FROM office_agent_messages m
		WHERE r.user_id=$1 AND r.conversation_id=$2 AND r.replay_pinned AND r.replay_invalidated_at IS NULL
			AND r.status IN ('completed','partially_completed') AND m.run_id=COALESCE(r.regeneration_of,r.id)
			AND m.role='user' AND m.sequence>$3`, userID, conversationID, sequence)
	return err
}

func (r *officeAgentRepository) cleanupWorkbenchReplayCheckpoints(ctx context.Context, cutoff time.Time, limit int) error {
	_, err := r.db.ExecContext(ctx, `DELETE FROM office_agent_replay_checkpoints WHERE id IN (
		SELECT checkpoint.id FROM office_agent_replay_checkpoints checkpoint
		WHERE NOT checkpoint.published AND checkpoint.created_at<$1 AND NOT EXISTS (
			SELECT 1 FROM office_agent_runs active
			WHERE active.replay_checkpoint_id=checkpoint.id
			AND active.status IN ('queued','running','recovering','waiting_user','needs_confirmation')
		)
		ORDER BY checkpoint.created_at,checkpoint.id LIMIT $2 FOR UPDATE OF checkpoint SKIP LOCKED
	)`, cutoff, limit)
	return err
}
