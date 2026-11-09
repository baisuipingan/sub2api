package repository

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"time"

	infraerrors "github.com/Wei-Shaw/sub2api/internal/pkg/errors"
	"github.com/Wei-Shaw/sub2api/internal/service"
)

func NewTicketAttachmentRepository(db *sql.DB) service.TicketAttachmentRepository {
	return &ticketRepository{db: db}
}

type ticketQueryer interface {
	QueryRowContext(context.Context, string, ...any) *sql.Row
}

func lockTicketOwner(ctx context.Context, tx *sql.Tx, userID int64) error {
	var id int64
	err := tx.QueryRowContext(ctx, `SELECT id FROM users WHERE id=$1 AND deleted_at IS NULL FOR UPDATE`, userID).Scan(&id)
	if errors.Is(err, sql.ErrNoRows) {
		return infraerrors.NotFound("TICKET_USER_NOT_FOUND", "user not found")
	}
	return err
}

func ticketOpenLimit(ctx context.Context, q ticketQueryer, userID int64) error {
	var count int
	if err := q.QueryRowContext(ctx, `SELECT COUNT(*) FROM tickets WHERE user_id=$1 AND deleted_at IS NULL AND status IN ('pending','replied')`, userID).Scan(&count); err != nil {
		return err
	}
	if count >= service.TicketMaxOpen {
		return service.ErrTicketOpenLimit
	}
	return nil
}

func ticketSubmissionRate(ctx context.Context, q ticketQueryer, input service.TicketSubmission) error {
	var count int
	if input.TicketID == 0 {
		if err := q.QueryRowContext(ctx, `SELECT COUNT(*) FROM tickets WHERE user_id=$1 AND created_at >= $2`, input.OwnerID, time.Now().Add(-10*time.Minute)).Scan(&count); err != nil {
			return err
		}
		if count >= 5 {
			return infraerrors.New(429, "TICKET_CREATE_RATE_LIMITED", "新建工单过于频繁，请在 10 分钟后重试")
		}
	} else if input.Role == service.TicketSenderRoleUser {
		if err := q.QueryRowContext(ctx, `SELECT COUNT(*) FROM ticket_messages WHERE sender_id=$1 AND created_at >= $2`, input.SenderID, time.Now().Add(-10*time.Minute)).Scan(&count); err != nil {
			return err
		}
		if count >= 30 {
			return infraerrors.New(429, "TICKET_MESSAGE_RATE_LIMITED", "回复过于频繁，请在 10 分钟后重试")
		}
	}
	return nil
}

func validateLockedTicket(input service.TicketSubmission, ownerID int64, status string, deleted bool) error {
	if deleted {
		return service.NewTicketNotFoundError(input.TicketID)
	}
	if input.OwnerID != ownerID {
		return infraerrors.Forbidden("TICKET_FORBIDDEN", "ticket ownership changed")
	}
	if input.Role == service.TicketSenderRoleUser {
		if input.SenderID != ownerID {
			return infraerrors.Forbidden("TICKET_FORBIDDEN", "ticket does not belong to current user")
		}
		if status == service.TicketStatusClosed {
			return infraerrors.BadRequest("TICKET_CLOSED", "ticket is closed")
		}
	}
	return nil
}

// Preflight is intentionally repeated inside SubmitTicket after S3 work. It
// avoids needless uploads in normal cases without holding a transaction open.
func (r *ticketRepository) CheckTicketSubmission(ctx context.Context, input service.TicketSubmission) error {
	if input.TicketID == 0 {
		if err := ticketOpenLimit(ctx, r.db, input.OwnerID); err != nil {
			return err
		}
	} else {
		var ownerID int64
		var status string
		var deleted sql.NullTime
		if err := r.db.QueryRowContext(ctx, `SELECT user_id,status,deleted_at FROM tickets WHERE id=$1`, input.TicketID).Scan(&ownerID, &status, &deleted); err != nil {
			if errors.Is(err, sql.ErrNoRows) {
				return service.NewTicketNotFoundError(input.TicketID)
			}
			return err
		}
		if err := validateLockedTicket(input, ownerID, status, deleted.Valid); err != nil {
			return err
		}
		if status == service.TicketStatusClosed {
			if err := ticketOpenLimit(ctx, r.db, ownerID); err != nil {
				return err
			}
		}
	}
	return ticketSubmissionRate(ctx, r.db, input)
}

func scanTicketRow(row *sql.Row) (*service.Ticket, error) {
	var ticket service.Ticket
	err := row.Scan(&ticket.ID, &ticket.UserID, &ticket.Title, &ticket.Status, &ticket.Priority, &ticket.LastMessageAt, &ticket.LastMessageRole, &ticket.CreatedAt, &ticket.UpdatedAt)
	return &ticket, err
}
func ticketOperation(ctx context.Context, q ticketQueryer, operationID, digest string) (*service.TicketSubmissionResult, error) {
	if operationID == "" {
		return nil, nil
	}
	var msg service.TicketMessage
	var storedDigest string
	var deleted sql.NullTime
	err := q.QueryRowContext(ctx, `SELECT m.id,m.ticket_id,m.sender_id,m.sender_role,m.content,m.created_at,m.operation_digest,t.deleted_at FROM ticket_messages m JOIN tickets t ON t.id=m.ticket_id WHERE m.operation_id=$1`, operationID).Scan(&msg.ID, &msg.TicketID, &msg.SenderID, &msg.SenderRole, &msg.Content, &msg.CreatedAt, &storedDigest, &deleted)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	if deleted.Valid || storedDigest != digest {
		return nil, service.ErrTicketOperationConflict
	}
	ticket, err := scanTicketRow(q.QueryRowContext(ctx, `SELECT id,user_id,title,status,priority,last_message_at,last_message_role,created_at,updated_at FROM tickets WHERE id=$1 AND deleted_at IS NULL`, msg.TicketID))
	if err != nil {
		return nil, err
	}
	return &service.TicketSubmissionResult{Ticket: ticket, Message: &msg}, nil
}
func (r *ticketRepository) FindTicketOperation(ctx context.Context, operationID, digest string) (*service.TicketSubmissionResult, error) {
	result, err := ticketOperation(ctx, r.db, operationID, digest)
	if err != nil || result == nil {
		return result, err
	}
	return r.withSubmissionAttachments(ctx, result)
}
func (r *ticketRepository) withSubmissionAttachments(ctx context.Context, result *service.TicketSubmissionResult) (*service.TicketSubmissionResult, error) {
	assets, err := r.ListTicketAttachments(ctx, result.Ticket.ID)
	if err != nil {
		return nil, err
	}
	for _, asset := range assets {
		if asset.MessageID == result.Message.ID {
			result.Message.Attachments = append(result.Message.Attachments, asset.TicketAttachment)
		}
	}
	return result, nil
}

func (r *ticketRepository) SubmitTicket(ctx context.Context, input service.TicketSubmission) (*service.TicketSubmissionResult, error) {
	if input.Role != service.TicketSenderRoleUser && input.Role != service.TicketSenderRoleAdmin {
		return nil, infraerrors.BadRequest("TICKET_ROLE_INVALID", "invalid sender role")
	}
	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return nil, err
	}
	defer func() { _ = tx.Rollback() }()
	if err := lockTicketOwner(ctx, tx, input.OwnerID); err != nil {
		return nil, err
	}
	if previous, err := ticketOperation(ctx, tx, input.OperationID, input.Digest); err != nil || previous != nil {
		if err != nil {
			return nil, err
		}
		if err = tx.Commit(); err != nil {
			return nil, err
		}
		return r.withSubmissionAttachments(ctx, previous)
	}
	var ticket *service.Ticket
	if input.TicketID == 0 {
		if input.Role != service.TicketSenderRoleUser || input.OwnerID != input.SenderID {
			return nil, infraerrors.Forbidden("TICKET_FORBIDDEN", "invalid ticket owner")
		}
		if err = ticketOpenLimit(ctx, tx, input.OwnerID); err != nil {
			return nil, err
		}
	} else {
		var ownerID int64
		var status string
		var deleted sql.NullTime
		err = tx.QueryRowContext(ctx, `SELECT user_id,status,deleted_at FROM tickets WHERE id=$1 FOR UPDATE`, input.TicketID).Scan(&ownerID, &status, &deleted)
		if errors.Is(err, sql.ErrNoRows) {
			return nil, service.NewTicketNotFoundError(input.TicketID)
		}
		if err != nil {
			return nil, err
		}
		if err = validateLockedTicket(input, ownerID, status, deleted.Valid); err != nil {
			return nil, err
		}
		if status == service.TicketStatusClosed {
			if err = ticketOpenLimit(ctx, tx, ownerID); err != nil {
				return nil, err
			}
		}
	}
	if err = ticketSubmissionRate(ctx, tx, input); err != nil {
		return nil, err
	}
	if input.TicketID == 0 {
		ticket, err = scanTicketRow(tx.QueryRowContext(ctx, `INSERT INTO tickets(user_id,title,status,priority,last_message_at,last_message_role) VALUES($1,$2,'pending',$3,NOW(),'user') RETURNING id,user_id,title,status,priority,last_message_at,last_message_role,created_at,updated_at`, input.OwnerID, input.Title, input.Priority))
		if err != nil {
			return nil, err
		}
		input.TicketID = ticket.ID
	}
	var msg service.TicketMessage
	err = tx.QueryRowContext(ctx, `INSERT INTO ticket_messages(ticket_id,sender_id,sender_role,content,operation_id,operation_digest) VALUES($1,$2,$3,$4,NULLIF($5,''),NULLIF($6,'')) RETURNING id,ticket_id,sender_id,sender_role,content,created_at`, input.TicketID, input.SenderID, input.Role, input.Content, input.OperationID, input.Digest).Scan(&msg.ID, &msg.TicketID, &msg.SenderID, &msg.SenderRole, &msg.Content, &msg.CreatedAt)
	if err != nil {
		return nil, err
	}
	for _, asset := range input.Attachments {
		res, err := tx.ExecContext(ctx, `UPDATE ticket_message_attachments SET message_id=$2,state='ready',updated_at=NOW() WHERE id=$1 AND operation_id=$3 AND owner_id=$4 AND digest=$5 AND state='pending' AND message_id IS NULL`, asset.ID, msg.ID, input.OperationID, input.OwnerID, asset.Digest)
		if err != nil {
			return nil, err
		}
		n, err := res.RowsAffected()
		if err != nil {
			return nil, err
		}
		if n != 1 {
			return nil, service.ErrTicketOperationConflict
		}
		msg.Attachments = append(msg.Attachments, asset.TicketAttachment)
	}
	nextStatus := service.TicketStatusPending
	readCol := "user_read_at"
	if input.Role == service.TicketSenderRoleAdmin {
		nextStatus = service.TicketStatusReplied
		readCol = "admin_read_at"
	}
	ticket, err = scanTicketRow(tx.QueryRowContext(ctx, fmt.Sprintf(`UPDATE tickets SET status=$2,last_message_at=$3,last_message_role=$4,%s=$3,updated_at=NOW() WHERE id=$1 RETURNING id,user_id,title,status,priority,last_message_at,last_message_role,created_at,updated_at`, readCol), input.TicketID, nextStatus, msg.CreatedAt, input.Role))
	if err != nil {
		return nil, err
	}
	if err = tx.Commit(); err != nil {
		return nil, err
	}
	ticket.MessageCount = 1
	return &service.TicketSubmissionResult{Ticket: ticket, Message: &msg}, nil
}

const ticketAttachmentColumns = `a.id,a.owner_id,COALESCE(a.message_id,0),a.operation_id,a.ordinal,a.digest,a.state,a.mime,a.bytes,a.width,a.height,a.thumbnail_mime,a.thumbnail_bytes,a.thumbnail_width,a.thumbnail_height,a.storage_endpoint,a.storage_region,a.storage_bucket,a.image_key,a.thumbnail_key`

type ticketScanner interface{ Scan(...any) error }

func scanTicketAttachment(row ticketScanner) (service.TicketStoredAttachment, error) {
	var a service.TicketStoredAttachment
	err := row.Scan(&a.ID, &a.OwnerID, &a.MessageID, &a.OperationID, &a.Ordinal, &a.Digest, &a.State, &a.MIME, &a.Bytes, &a.Width, &a.Height, &a.ThumbnailMIME, &a.ThumbnailBytes, &a.ThumbnailWidth, &a.ThumbnailHeight, &a.Endpoint, &a.Region, &a.Bucket, &a.ImageKey, &a.ThumbnailKey)
	a.CacheVersion = a.Digest
	return a, err
}
func (r *ticketRepository) ReserveTicketAttachments(ctx context.Context, assets []service.TicketStoredAttachment) error {
	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback() }()
	for _, a := range assets {
		res, insertErr := tx.ExecContext(ctx, `INSERT INTO ticket_message_attachments(id,owner_id,operation_id,ordinal,digest,state,mime,bytes,width,height,thumbnail_mime,thumbnail_bytes,thumbnail_width,thumbnail_height,storage_endpoint,storage_region,storage_bucket,image_key,thumbnail_key) VALUES($1,$2,$3,$4,$5,'pending',$6,$7,$8,$9,$10,$11,$12,$13,$14,$15,$16,$17,$18) ON CONFLICT(id) DO NOTHING`, a.ID, a.OwnerID, a.OperationID, a.Ordinal, a.Digest, a.MIME, a.Bytes, a.Width, a.Height, a.ThumbnailMIME, a.ThumbnailBytes, a.ThumbnailWidth, a.ThumbnailHeight, a.Endpoint, a.Region, a.Bucket, a.ImageKey, a.ThumbnailKey)
		if insertErr != nil {
			return insertErr
		}
		inserted, err := res.RowsAffected()
		if err != nil {
			return err
		}
		stored, err := scanTicketAttachment(tx.QueryRowContext(ctx, `SELECT `+ticketAttachmentColumns+` FROM ticket_message_attachments a WHERE a.id=$1 FOR UPDATE`, a.ID))
		if err != nil {
			return err
		}
		if stored.OperationID != a.OperationID || stored.Digest != a.Digest || stored.OwnerID != a.OwnerID || stored.Endpoint != a.Endpoint || stored.Region != a.Region || stored.Bucket != a.Bucket {
			return service.ErrTicketOperationConflict
		}
		if inserted == 0 || stored.State != "pending" || stored.MessageID != 0 {
			return service.ErrTicketAttachmentBusy
		}
		if _, err = tx.ExecContext(ctx, `UPDATE ticket_message_attachments SET updated_at=NOW() WHERE id=$1`, a.ID); err != nil {
			return err
		}
	}
	return tx.Commit()
}
func (r *ticketRepository) ListTicketAttachments(ctx context.Context, ticketID int64) ([]service.TicketStoredAttachment, error) {
	rows, err := r.db.QueryContext(ctx, `SELECT `+ticketAttachmentColumns+` FROM ticket_message_attachments a JOIN ticket_messages m ON m.id=a.message_id JOIN tickets t ON t.id=m.ticket_id WHERE t.id=$1 AND t.deleted_at IS NULL AND a.state='ready' ORDER BY m.id,a.ordinal`, ticketID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var assets []service.TicketStoredAttachment
	for rows.Next() {
		a, err := scanTicketAttachment(rows)
		if err != nil {
			return nil, err
		}
		assets = append(assets, a)
	}
	return assets, rows.Err()
}
func (r *ticketRepository) GetTicketAttachment(ctx context.Context, ticketID int64, id string, userID int64) (*service.TicketStoredAttachment, error) {
	a, err := scanTicketAttachment(r.db.QueryRowContext(ctx, `SELECT `+ticketAttachmentColumns+` FROM ticket_message_attachments a JOIN ticket_messages m ON m.id=a.message_id JOIN tickets t ON t.id=m.ticket_id JOIN users u ON u.id=t.user_id WHERE a.id=$1 AND t.id=$2 AND ($3=0 OR t.user_id=$3) AND t.deleted_at IS NULL AND u.deleted_at IS NULL AND a.state='ready'`, id, ticketID, userID))
	if errors.Is(err, sql.ErrNoRows) {
		return nil, infraerrors.NotFound("TICKET_ATTACHMENT_NOT_FOUND", "截图不存在或无权访问")
	}
	return &a, err
}
func (r *ticketRepository) MarkTicketOperationAttachmentsDeleting(ctx context.Context, operationID string) error {
	_, err := r.db.ExecContext(ctx, `UPDATE ticket_message_attachments SET state='deleting',updated_at=NOW() WHERE operation_id=$1 AND message_id IS NULL AND state='pending'`, operationID)
	return err
}
func (r *ticketRepository) ClaimTicketAttachmentCleanup(ctx context.Context, before time.Time, limit int) ([]service.TicketStoredAttachment, error) {
	rows, err := r.db.QueryContext(ctx, `WITH candidates AS (SELECT a.id FROM ticket_message_attachments a LEFT JOIN ticket_messages m ON m.id=a.message_id LEFT JOIN tickets t ON t.id=m.ticket_id LEFT JOIN users u ON u.id=a.owner_id WHERE a.state='deleting' OR (a.state='pending' AND a.updated_at<$1) OR (a.state='ready' AND (a.message_id IS NULL OR t.deleted_at IS NOT NULL OR u.id IS NULL OR u.deleted_at IS NOT NULL)) ORDER BY a.updated_at LIMIT $2 FOR UPDATE OF a SKIP LOCKED) UPDATE ticket_message_attachments a SET state='deleting',updated_at=NOW() FROM candidates c WHERE a.id=c.id RETURNING `+ticketAttachmentColumns, before, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var assets []service.TicketStoredAttachment
	for rows.Next() {
		a, err := scanTicketAttachment(rows)
		if err != nil {
			return nil, err
		}
		assets = append(assets, a)
	}
	return assets, rows.Err()
}
func (r *ticketRepository) FinalizeTicketAttachmentDelete(ctx context.Context, id string) error {
	_, err := r.db.ExecContext(ctx, `DELETE FROM ticket_message_attachments WHERE id=$1 AND state='deleting'`, id)
	return err
}
func (r *ticketRepository) HasAttachments(ctx context.Context) (bool, error) {
	var exists bool
	err := r.db.QueryRowContext(ctx, `SELECT EXISTS(SELECT 1 FROM ticket_message_attachments)`).Scan(&exists)
	return exists, err
}

// Serializes physical DELETE + row removal across instances and synchronous
// rollback. It uses a dedicated session advisory lock, never an open transaction.
func (r *ticketRepository) LockTicketAttachmentCleanup(ctx context.Context) (func(), error) {
	return (&imageStudioGalleryRepository{db: r.db}).lockSession(ctx, 6_000_000_000_000_001)
}

func (r *ticketRepository) ListDeletingTicketOperationAttachments(ctx context.Context, operationID string) ([]service.TicketStoredAttachment, error) {
	rows, err := r.db.QueryContext(ctx, `SELECT `+ticketAttachmentColumns+` FROM ticket_message_attachments a WHERE a.operation_id=$1 AND a.state='deleting' AND a.message_id IS NULL ORDER BY a.ordinal`, operationID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var assets []service.TicketStoredAttachment
	for rows.Next() {
		a, err := scanTicketAttachment(rows)
		if err != nil {
			return nil, err
		}
		assets = append(assets, a)
	}
	return assets, rows.Err()
}
