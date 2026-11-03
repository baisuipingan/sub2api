package repository

import (
	"context"
	"database/sql"
	"fmt"
	"strings"

	"github.com/Wei-Shaw/sub2api/internal/pkg/pagination"
	"github.com/Wei-Shaw/sub2api/internal/service"
)

// ticketRepository 工单存储（原生 SQL）。
//
// 新表不进 ent：走 ent 需要改 schema 并重新生成代码，而工单只有两张表、
// 几条固定查询，原生 SQL 更直接，也不会出现「加了列 ent 读不到」的问题。
type ticketRepository struct {
	db *sql.DB
}

func NewTicketRepository(db *sql.DB) service.TicketRepository {
	return &ticketRepository{db: db}
}

const ticketColumns = `t.id, t.user_id, t.title, t.status, t.priority,
	t.last_message_at, t.last_message_role, t.created_at, t.updated_at`

func scanTicket(row interface {
	Scan(dest ...any) error
}) (*service.Ticket, error) {
	var tk service.Ticket
	if err := row.Scan(
		&tk.ID, &tk.UserID, &tk.Title, &tk.Status, &tk.Priority,
		&tk.LastMessageAt, &tk.LastMessageRole, &tk.CreatedAt, &tk.UpdatedAt,
	); err != nil {
		return nil, err
	}
	return &tk, nil
}

func (r *ticketRepository) Create(ctx context.Context, userID int64, title, priority, content string) (*service.Ticket, error) {
	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return nil, fmt.Errorf("begin ticket tx: %w", err)
	}
	defer func() { _ = tx.Rollback() }()

	var ticket service.Ticket
	err = tx.QueryRowContext(ctx, `
		INSERT INTO tickets (user_id, title, status, priority, last_message_at, last_message_role)
		VALUES ($1, $2, $3, $4, NOW(), $5)
		RETURNING id, user_id, title, status, priority, last_message_at, last_message_role, created_at, updated_at`,
		userID, title, service.TicketStatusPending, priority, service.TicketSenderRoleUser,
	).Scan(
		&ticket.ID, &ticket.UserID, &ticket.Title, &ticket.Status, &ticket.Priority,
		&ticket.LastMessageAt, &ticket.LastMessageRole, &ticket.CreatedAt, &ticket.UpdatedAt,
	)
	if err != nil {
		return nil, fmt.Errorf("insert ticket: %w", err)
	}

	if _, err := tx.ExecContext(ctx, `
		INSERT INTO ticket_messages (ticket_id, sender_id, sender_role, content)
		VALUES ($1, $2, $3, $4)`,
		ticket.ID, userID, service.TicketSenderRoleUser, content,
	); err != nil {
		return nil, fmt.Errorf("insert ticket message: %w", err)
	}

	if err := tx.Commit(); err != nil {
		return nil, fmt.Errorf("commit ticket tx: %w", err)
	}
	ticket.MessageCount = 1
	return &ticket, nil
}

func (r *ticketRepository) GetByID(ctx context.Context, id int64) (*service.Ticket, error) {
	row := r.db.QueryRowContext(ctx, `
		SELECT `+ticketColumns+`, COALESCE(u.email, '')
		FROM tickets t LEFT JOIN users u ON u.id = t.user_id
		WHERE t.id = $1`, id)

	var tk service.Ticket
	err := row.Scan(
		&tk.ID, &tk.UserID, &tk.Title, &tk.Status, &tk.Priority,
		&tk.LastMessageAt, &tk.LastMessageRole, &tk.CreatedAt, &tk.UpdatedAt,
		&tk.UserEmail,
	)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("get ticket: %w", err)
	}
	return &tk, nil
}

func (r *ticketRepository) ListByUser(ctx context.Context, userID int64, params pagination.PaginationParams, status string) ([]service.Ticket, *pagination.PaginationResult, error) {
	args := []any{userID}
	where := []string{"t.user_id = $1"}
	if status != "" {
		args = append(args, status)
		where = append(where, fmt.Sprintf("t.status = $%d", len(args)))
	}
	return r.list(ctx, params, strings.Join(where, " AND "), args)
}

func (r *ticketRepository) ListAll(ctx context.Context, params pagination.PaginationParams, filters service.TicketListFilters) ([]service.Ticket, *pagination.PaginationResult, error) {
	args := []any{}
	where := []string{"1 = 1"}

	if filters.Status != "" {
		args = append(args, filters.Status)
		where = append(where, fmt.Sprintf("t.status = $%d", len(args)))
	}
	if filters.Search != "" {
		args = append(args, "%"+filters.Search+"%")
		where = append(where, fmt.Sprintf("(t.title ILIKE $%d OR u.email ILIKE $%d)", len(args), len(args)))
	}

	return r.list(ctx, params, strings.Join(where, " AND "), args)
}

// list 列表查询公共部分。
//
// SortBy/SortOrder 已在 service 层白名单化，这里再映射一次，
// 保证任何调用方都不可能把任意字符串拼进 SQL。
func (r *ticketRepository) list(ctx context.Context, params pagination.PaginationParams, where string, args []any) ([]service.Ticket, *pagination.PaginationResult, error) {
	sortColumn := "t.last_message_at"
	if params.SortBy == "created_at" {
		sortColumn = "t.created_at"
	}
	sortOrder := "DESC"
	if strings.EqualFold(params.SortOrder, "asc") {
		sortOrder = "ASC"
	}

	var total int64
	countSQL := `SELECT COUNT(*) FROM tickets t LEFT JOIN users u ON u.id = t.user_id WHERE ` + where
	if err := r.db.QueryRowContext(ctx, countSQL, args...).Scan(&total); err != nil {
		return nil, nil, fmt.Errorf("count tickets: %w", err)
	}

	offset := (params.Page - 1) * params.PageSize
	queryArgs := append(append([]any{}, args...), params.PageSize, offset)
	listSQL := fmt.Sprintf(`
		SELECT %s, COALESCE(u.email, ''),
		       (SELECT COUNT(*) FROM ticket_messages m WHERE m.ticket_id = t.id) AS message_count
		FROM tickets t LEFT JOIN users u ON u.id = t.user_id
		WHERE %s
		ORDER BY %s %s, t.id DESC
		LIMIT $%d OFFSET $%d`,
		ticketColumns, where, sortColumn, sortOrder, len(args)+1, len(args)+2)

	rows, err := r.db.QueryContext(ctx, listSQL, queryArgs...)
	if err != nil {
		return nil, nil, fmt.Errorf("list tickets: %w", err)
	}
	defer func() { _ = rows.Close() }()

	items := make([]service.Ticket, 0, params.PageSize)
	for rows.Next() {
		var tk service.Ticket
		if err := rows.Scan(
			&tk.ID, &tk.UserID, &tk.Title, &tk.Status, &tk.Priority,
			&tk.LastMessageAt, &tk.LastMessageRole, &tk.CreatedAt, &tk.UpdatedAt,
			&tk.UserEmail, &tk.MessageCount,
		); err != nil {
			return nil, nil, fmt.Errorf("scan ticket row: %w", err)
		}
		items = append(items, tk)
	}
	if err := rows.Err(); err != nil {
		return nil, nil, fmt.Errorf("iterate tickets: %w", err)
	}

	pages := 0
	if params.PageSize > 0 {
		pages = int((total + int64(params.PageSize) - 1) / int64(params.PageSize))
	}
	return items, &pagination.PaginationResult{
		Total:    total,
		Page:     params.Page,
		PageSize: params.PageSize,
		Pages:    pages,
	}, nil
}

func (r *ticketRepository) ListMessages(ctx context.Context, ticketID int64) ([]service.TicketMessage, error) {
	rows, err := r.db.QueryContext(ctx, `
		SELECT id, ticket_id, sender_id, sender_role, content, created_at
		FROM ticket_messages
		WHERE ticket_id = $1
		ORDER BY created_at ASC, id ASC`, ticketID)
	if err != nil {
		return nil, fmt.Errorf("list ticket messages: %w", err)
	}
	defer func() { _ = rows.Close() }()

	messages := make([]service.TicketMessage, 0, 8)
	for rows.Next() {
		var m service.TicketMessage
		if err := rows.Scan(&m.ID, &m.TicketID, &m.SenderID, &m.SenderRole, &m.Content, &m.CreatedAt); err != nil {
			return nil, fmt.Errorf("scan ticket message: %w", err)
		}
		messages = append(messages, m)
	}
	return messages, rows.Err()
}

// AddMessage 追加消息并同步工单状态与最后消息时间（同一事务）。
func (r *ticketRepository) AddMessage(ctx context.Context, ticketID, senderID int64, role, content, nextStatus string) (*service.TicketMessage, error) {
	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return nil, fmt.Errorf("begin message tx: %w", err)
	}
	defer func() { _ = tx.Rollback() }()

	var msg service.TicketMessage
	if err := tx.QueryRowContext(ctx, `
		INSERT INTO ticket_messages (ticket_id, sender_id, sender_role, content)
		VALUES ($1, $2, $3, $4)
		RETURNING id, ticket_id, sender_id, sender_role, content, created_at`,
		ticketID, senderID, role, content,
	).Scan(&msg.ID, &msg.TicketID, &msg.SenderID, &msg.SenderRole, &msg.Content, &msg.CreatedAt); err != nil {
		return nil, fmt.Errorf("insert message: %w", err)
	}

	if _, err := tx.ExecContext(ctx, `
		UPDATE tickets SET status = $2, last_message_at = $3, last_message_role = $4, updated_at = NOW()
		WHERE id = $1`, ticketID, nextStatus, msg.CreatedAt, role); err != nil {
		return nil, fmt.Errorf("update ticket after message: %w", err)
	}

	if err := tx.Commit(); err != nil {
		return nil, fmt.Errorf("commit message tx: %w", err)
	}
	return &msg, nil
}

func (r *ticketRepository) UpdateStatus(ctx context.Context, ticketID int64, status string) error {
	res, err := r.db.ExecContext(ctx, `
		UPDATE tickets SET status = $2, updated_at = NOW() WHERE id = $1`, ticketID, status)
	if err != nil {
		return fmt.Errorf("update ticket status: %w", err)
	}
	affected, err := res.RowsAffected()
	if err != nil {
		return err
	}
	if affected == 0 {
		return service.NewTicketNotFoundError(ticketID)
	}
	return nil
}
