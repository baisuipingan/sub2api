package repository

import (
	"context"
	"database/sql"
	"fmt"
	"strings"
	"time"

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

// unreadExpr 生成「对该角色而言是否未读」的 SQL 表达式。
//
// 判定两条同时成立：
//  1. 最后一条消息晚于该角色的已读时间（NULL 用 created_at 兜底，防止历史数据全变未读）
//  2. 最后一条消息**不是自己发的**
//
// 第 2 条不能省：用户刚提交工单时 last_message_at 就是刚才，
// 没有它用户自己那侧会立刻出现未读红点，管理员侧同理。
//
// roleParam 是该角色值在参数列表中的占位序号（如 3 表示 $3）。
func unreadExpr(readCol string, roleParam int) string {
	return fmt.Sprintf(
		`(t.last_message_at > COALESCE(t.%s, t.created_at) AND t.last_message_role <> $%d)`,
		readCol, roleParam,
	)
}

func (r *ticketRepository) Create(ctx context.Context, userID int64, title, priority, content string) (*service.Ticket, error) {
	result, err := r.SubmitTicket(ctx, service.TicketSubmission{OwnerID: userID, SenderID: userID, Role: service.TicketSenderRoleUser, Title: title, Priority: priority, Content: content})
	if err != nil {
		return nil, err
	}
	return result.Ticket, nil
}

func (r *ticketRepository) GetByID(ctx context.Context, id int64) (*service.Ticket, error) {
	// role 参数只用于 unread 表达式；详情接口不关心未读，传空角色即可
	row := r.db.QueryRowContext(ctx, `
		SELECT `+ticketColumns+`, COALESCE(u.email, ''), `+unreadExpr("user_read_at", 2)+`
		FROM tickets t LEFT JOIN users u ON u.id = t.user_id
		WHERE t.id = $1 AND t.deleted_at IS NULL`, id, service.TicketSenderRoleUser)

	var tk service.Ticket
	err := row.Scan(
		&tk.ID, &tk.UserID, &tk.Title, &tk.Status, &tk.Priority,
		&tk.LastMessageAt, &tk.LastMessageRole, &tk.CreatedAt, &tk.UpdatedAt,
		&tk.UserEmail, &tk.Unread,
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
	where := []string{"t.user_id = $1", "t.deleted_at IS NULL"}
	if status != "" {
		args = append(args, status)
		where = append(where, fmt.Sprintf("t.status = $%d", len(args)))
	}
	return r.list(ctx, params, strings.Join(where, " AND "), args, "user_read_at", service.TicketSenderRoleUser)
}

func (r *ticketRepository) ListAll(ctx context.Context, params pagination.PaginationParams, filters service.TicketListFilters) ([]service.Ticket, *pagination.PaginationResult, error) {
	args := []any{}
	where := []string{"t.deleted_at IS NULL"}

	if filters.Status != "" {
		args = append(args, filters.Status)
		where = append(where, fmt.Sprintf("t.status = $%d", len(args)))
	}
	if filters.Search != "" {
		args = append(args, "%"+filters.Search+"%")
		where = append(where, fmt.Sprintf("(t.title ILIKE $%d OR u.email ILIKE $%d)", len(args), len(args)))
	}

	return r.list(ctx, params, strings.Join(where, " AND "), args, "admin_read_at", service.TicketSenderRoleAdmin)
}

// list 列表查询公共部分。
//
// 注意：count 与列表查询的参数集合不同（列表多一个「查看方角色」用于未读表达式），
// 所以两者各自拼参数，不能共用同一个 args 切片。
//
// SortBy/SortOrder 已在 service 层白名单化，这里再映射一次，
// 保证任何调用方都不可能把任意字符串拼进 SQL。
func (r *ticketRepository) list(
	ctx context.Context,
	params pagination.PaginationParams,
	where string,
	args []any,
	readCol string,
	viewerRole string,
) ([]service.Ticket, *pagination.PaginationResult, error) {
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

	// 列表查询：角色作为新参数追加到末尾，供 unreadExpr 使用
	roleParam := len(args) + 1
	listArgs := append(append([]any{}, args...), viewerRole, params.PageSize, (params.Page-1)*params.PageSize)
	listSQL := fmt.Sprintf(`
		SELECT %s, COALESCE(u.email, ''), %s,
		       (SELECT COUNT(*) FROM ticket_messages m WHERE m.ticket_id = t.id) AS message_count
		FROM tickets t LEFT JOIN users u ON u.id = t.user_id
		WHERE %s
		ORDER BY %s %s, t.id DESC
		LIMIT $%d OFFSET $%d`,
		ticketColumns, unreadExpr(readCol, roleParam), where, sortColumn, sortOrder, roleParam+1, roleParam+2)

	rows, err := r.db.QueryContext(ctx, listSQL, listArgs...)
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
			&tk.UserEmail, &tk.Unread, &tk.MessageCount,
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
	ticket, err := r.GetByID(ctx, ticketID)
	if err != nil {
		return nil, err
	}
	if ticket == nil {
		return nil, service.NewTicketNotFoundError(ticketID)
	}
	result, err := r.SubmitTicket(ctx, service.TicketSubmission{TicketID: ticketID, OwnerID: ticket.UserID, SenderID: senderID, Role: role, Content: content})
	if err != nil {
		return nil, err
	}
	return result.Message, nil
}

func (r *ticketRepository) UpdateStatus(ctx context.Context, ticketID int64, status string) error {
	ticket, err := r.GetByID(ctx, ticketID)
	if err != nil {
		return err
	}
	if ticket == nil {
		return service.NewTicketNotFoundError(ticketID)
	}
	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback() }()
	if err = lockTicketOwner(ctx, tx, ticket.UserID); err != nil {
		return err
	}
	var current string
	var deleted sql.NullTime
	if err = tx.QueryRowContext(ctx, `SELECT status,deleted_at FROM tickets WHERE id=$1 FOR UPDATE`, ticketID).Scan(&current, &deleted); err != nil {
		return err
	}
	if deleted.Valid {
		return service.NewTicketNotFoundError(ticketID)
	}
	if current == service.TicketStatusClosed && status != service.TicketStatusClosed {
		if err = ticketOpenLimit(ctx, tx, ticket.UserID); err != nil {
			return err
		}
	}
	if _, err = tx.ExecContext(ctx, `UPDATE tickets SET status=$2,updated_at=NOW() WHERE id=$1`, ticketID, status); err != nil {
		return err
	}
	return tx.Commit()
}

// MarkRead 把某角色对该工单的已读时间推到最新消息时间。
//
// 使用 GREATEST 保证时间只前进不后退：并发下先读后写的旧请求不会把已读时间打回去。
func (r *ticketRepository) MarkRead(ctx context.Context, ticketID int64, role string) error {
	readCol := "user_read_at"
	if role == service.TicketSenderRoleAdmin {
		readCol = "admin_read_at"
	}
	_, err := r.db.ExecContext(ctx, fmt.Sprintf(`
		UPDATE tickets SET %s = GREATEST(COALESCE(%s, created_at), last_message_at)
		WHERE id = $1`, readCol, readCol), ticketID)
	if err != nil {
		return fmt.Errorf("mark ticket read: %w", err)
	}
	return nil
}

// CountUnread 统计未读数。
//
// userID > 0：该用户自己工单里的未读；
// userID == 0：管理端全局未读（所有用户），仅管理员调用。
func (r *ticketRepository) CountUnread(ctx context.Context, userID int64, role string) (int64, error) {
	readCol := "user_read_at"
	if role == service.TicketSenderRoleAdmin {
		readCol = "admin_read_at"
	}

	args := []any{role}
	where := "t.status <> 'closed' AND t.deleted_at IS NULL"
	if userID > 0 {
		args = append(args, userID)
		where += " AND t.user_id = $2"
	}

	var count int64
	query := fmt.Sprintf(`SELECT COUNT(*) FROM tickets t WHERE %s AND %s`, where, unreadExpr(readCol, 1))
	if err := r.db.QueryRowContext(ctx, query, args...).Scan(&count); err != nil {
		return 0, fmt.Errorf("count unread tickets: %w", err)
	}
	return count, nil
}

// CountUserTicketsSince 统计用户在 since 之后创建的工单数（防刷用）。
func (r *ticketRepository) CountUserTicketsSince(ctx context.Context, userID int64, since time.Time) (int64, error) {
	var count int64
	err := r.db.QueryRowContext(ctx,
		`SELECT COUNT(*) FROM tickets WHERE user_id = $1 AND created_at >= $2`, userID, since).Scan(&count)
	if err != nil {
		return 0, fmt.Errorf("count user tickets: %w", err)
	}
	return count, nil
}

// CountUserMessagesSince 统计用户在 since 之后发出的消息数（防刷用）。
func (r *ticketRepository) CountUserMessagesSince(ctx context.Context, userID int64, since time.Time) (int64, error) {
	var count int64
	err := r.db.QueryRowContext(ctx,
		`SELECT COUNT(*) FROM ticket_messages WHERE sender_id = $1 AND created_at >= $2`, userID, since).Scan(&count)
	if err != nil {
		return 0, fmt.Errorf("count user messages: %w", err)
	}
	return count, nil
}

// SoftDelete 软删除工单。重复删除视为成功（幂等）。
func (r *ticketRepository) SoftDelete(ctx context.Context, ticketID int64) error {
	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback() }()
	var ownerID int64
	if err = tx.QueryRowContext(ctx, `SELECT user_id FROM tickets WHERE id=$1`, ticketID).Scan(&ownerID); err != nil {
		if err == sql.ErrNoRows {
			return nil
		}
		return err
	}
	if err = lockTicketOwner(ctx, tx, ownerID); err != nil {
		return err
	}
	if _, err = tx.ExecContext(ctx, `UPDATE tickets SET deleted_at=NOW() WHERE id=$1 AND deleted_at IS NULL`, ticketID); err != nil {
		return err
	}
	if _, err = tx.ExecContext(ctx, `UPDATE ticket_message_attachments SET state='deleting',updated_at=NOW() WHERE message_id IN (SELECT id FROM ticket_messages WHERE ticket_id=$1)`, ticketID); err != nil {
		return err
	}
	return tx.Commit()
}
