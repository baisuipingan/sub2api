package service

import (
	"context"
	"fmt"
	"net/http"
	"strings"
	"time"
	"unicode/utf8"

	infraerrors "github.com/Wei-Shaw/sub2api/internal/pkg/errors"
	"github.com/Wei-Shaw/sub2api/internal/pkg/pagination"
)

// 工单状态。与前端展示一一对应：
//
//	pending  待管理员处理（最后一条消息来自用户）
//	replied  待用户回复（最后一条消息来自管理员）
//	closed   已关闭（用户不可再回复）
const (
	TicketStatusPending = "pending"
	TicketStatusReplied = "replied"
	TicketStatusClosed  = "closed"
)

const (
	TicketPriorityNormal = "normal"
	TicketPriorityHigh   = "high"
)

const (
	ticketTitleMaxRunes   = 120
	ticketContentMaxRunes = 5000
	ticketPageSizeMax     = 100

	TicketMaxOpen = 5

	// 防刷：同一用户 10 分钟内最多新建 5 个工单、最多发 30 条消息。
	// 用数据库计数而不是 Redis：工单量本身很小，一次 COUNT 足够便宜，
	// 也不引入「Redis 挂了要不要 fail-open」的判断。
	ticketCreateWindow        = 10 * time.Minute
	ticketCreateMaxPerWindow  = 5
	ticketMessageWindow       = 10 * time.Minute
	ticketMessageMaxPerWindow = 30
)

// Ticket 工单主体。列表场景会额外带上用户邮箱与消息数。
type Ticket struct {
	ID              int64     `json:"id"`
	UserID          int64     `json:"user_id"`
	Title           string    `json:"title"`
	Status          string    `json:"status"`
	Priority        string    `json:"priority"`
	LastMessageAt   time.Time `json:"last_message_at"`
	LastMessageRole string    `json:"last_message_role"`
	CreatedAt       time.Time `json:"created_at"`
	UpdatedAt       time.Time `json:"updated_at"`

	// 以下字段仅列表/详情补充，不属于 tickets 表
	UserEmail    string `json:"user_email,omitempty"`
	MessageCount int    `json:"message_count"`
	// Unread 表示「对查看方而言有新回复」，用于侧栏红点与列表强调
	Unread bool `json:"unread"`
}

// TicketMessage 工单里的一条消息。
type TicketMessage struct {
	ID          int64              `json:"id"`
	TicketID    int64              `json:"ticket_id"`
	SenderID    *int64             `json:"sender_id"`
	SenderRole  string             `json:"sender_role"`
	Content     string             `json:"content"`
	CreatedAt   time.Time          `json:"created_at"`
	Attachments []TicketAttachment `json:"attachments,omitempty"`
}

// TicketListFilters 管理端列表筛选。
type TicketListFilters struct {
	Status string
	Search string
}

// TicketRepository 工单存储。
//
// 用原生 SQL 实现：tickets 是独立新表，走 ent 需要改 schema 并重新生成，
// 收益不抵成本（这个教训在 key_type 那次已经踩过）。
type TicketRepository interface {
	Create(ctx context.Context, userID int64, title, priority, content string) (*Ticket, error)
	GetByID(ctx context.Context, id int64) (*Ticket, error)
	ListByUser(ctx context.Context, userID int64, params pagination.PaginationParams, status string) ([]Ticket, *pagination.PaginationResult, error)
	ListAll(ctx context.Context, params pagination.PaginationParams, filters TicketListFilters) ([]Ticket, *pagination.PaginationResult, error)
	ListMessages(ctx context.Context, ticketID int64) ([]TicketMessage, error)
	AddMessage(ctx context.Context, ticketID, senderID int64, role, content, nextStatus string) (*TicketMessage, error)
	UpdateStatus(ctx context.Context, ticketID int64, status string) error
	// MarkRead 把某一角色对该工单的已读时间推到「该工单最后消息时间」
	MarkRead(ctx context.Context, ticketID int64, role string) error
	// CountUnread 统计某用户/管理员视角下的未读工单数
	CountUnread(ctx context.Context, userID int64, role string) (int64, error)
	// CountUserTicketsSince 统计用户在某时刻之后创建的工单数（防刷）
	CountUserTicketsSince(ctx context.Context, userID int64, since time.Time) (int64, error)
	// CountUserMessagesSince 统计用户在某时刻之后发出的消息数（防刷）
	CountUserMessagesSince(ctx context.Context, userID int64, since time.Time) (int64, error)
	// SoftDelete 软删除工单
	SoftDelete(ctx context.Context, ticketID int64) error
}

// TicketService 工单业务逻辑。
//
// 权限与状态流转都收在这里，handler 只做参数解析：
// 用户只能看到自己的工单；关闭后用户不能再回复；管理员回复即视为重新处理。
type TicketService struct {
	repo        TicketRepository
	attachments *TicketAttachmentService
}

func NewTicketService(repo TicketRepository) *TicketService {
	return &TicketService{repo: repo}
}

func (s *TicketService) SetAttachmentService(attachments *TicketAttachmentService) {
	s.attachments = attachments
}

func normalizeTicketStatus(status string) string {
	switch strings.TrimSpace(status) {
	case TicketStatusPending:
		return TicketStatusPending
	case TicketStatusReplied:
		return TicketStatusReplied
	case TicketStatusClosed:
		return TicketStatusClosed
	default:
		return ""
	}
}

func validateTicketTitle(title string) (string, error) {
	trimmed := strings.TrimSpace(title)
	if trimmed == "" {
		return "", infraerrors.BadRequest("TICKET_TITLE_REQUIRED", "ticket title is required")
	}
	if utf8.RuneCountInString(trimmed) > ticketTitleMaxRunes {
		return "", infraerrors.BadRequest("TICKET_TITLE_TOO_LONG", "ticket title is too long")
	}
	return trimmed, nil
}

func validateTicketContent(content string) (string, error) {
	trimmed := strings.TrimSpace(content)
	if trimmed == "" {
		return "", infraerrors.BadRequest("TICKET_CONTENT_REQUIRED", "ticket content is required")
	}
	if utf8.RuneCountInString(trimmed) > ticketContentMaxRunes {
		return "", infraerrors.BadRequest("TICKET_CONTENT_TOO_LONG", "ticket content is too long")
	}
	return trimmed, nil
}

func normalizeTicketPriority(priority string) string {
	if strings.TrimSpace(priority) == TicketPriorityHigh {
		return TicketPriorityHigh
	}
	return TicketPriorityNormal
}

// Create 用户提交新工单：标题 + 首条消息。
func (s *TicketService) Create(ctx context.Context, userID int64, title, content, priority string) (*Ticket, error) {
	cleanTitle, err := validateTicketTitle(title)
	if err != nil {
		return nil, err
	}
	cleanContent, err := validateTicketContent(content)
	if err != nil {
		return nil, err
	}
	if userID <= 0 {
		return nil, infraerrors.BadRequest("TICKET_USER_REQUIRED", "user is required")
	}

	if err := s.checkCreateRate(ctx, userID); err != nil {
		return nil, err
	}
	return s.repo.Create(ctx, userID, cleanTitle, normalizeTicketPriority(priority), cleanContent)
}

// ListMine 用户自己的工单列表。
func (s *TicketService) ListMine(ctx context.Context, userID int64, params pagination.PaginationParams, status string) ([]Ticket, *pagination.PaginationResult, error) {
	params, err := normalizeTicketParams(params)
	if err != nil {
		return nil, nil, err
	}
	status = normalizeTicketStatus(status)
	return s.repo.ListByUser(ctx, userID, params, status)
}

// ListAll 管理端全量列表。
func (s *TicketService) ListAll(ctx context.Context, params pagination.PaginationParams, filters TicketListFilters) ([]Ticket, *pagination.PaginationResult, error) {
	params, err := normalizeTicketParams(params)
	if err != nil {
		return nil, nil, err
	}
	filters.Status = normalizeTicketStatus(filters.Status)
	filters.Search = strings.TrimSpace(filters.Search)
	if utf8.RuneCountInString(filters.Search) > 100 {
		filters.Search = string([]rune(filters.Search)[:100])
	}
	return s.repo.ListAll(ctx, params, filters)
}

// GetDetail 取工单 + 全部消息。
//
// userID > 0 时校验归属（用户侧）；userID == 0 表示管理端，可读任意工单。
func (s *TicketService) GetDetail(ctx context.Context, ticketID, userID int64) (*Ticket, []TicketMessage, error) {
	ticket, err := s.repo.GetByID(ctx, ticketID)
	if err != nil {
		return nil, nil, err
	}
	if ticket == nil {
		return nil, nil, infraerrors.NotFound("TICKET_NOT_FOUND", "ticket not found")
	}
	if userID > 0 && ticket.UserID != userID {
		// 不用 NotFound，避免通过 id 探测别人工单是否存在
		return nil, nil, infraerrors.Forbidden("TICKET_FORBIDDEN", "ticket does not belong to current user")
	}
	messages, err := s.repo.ListMessages(ctx, ticketID)
	if err != nil {
		return nil, nil, err
	}

	if s.attachments != nil {
		if err := s.attachments.attachToMessages(ctx, ticketID, messages); err != nil {
			return nil, nil, err
		}
	}

	// 打开即视为已读。失败不影响读取本身（最多红点晚消失一次）。
	role := TicketSenderRoleAdmin
	if userID > 0 {
		role = TicketSenderRoleUser
	}
	_ = s.repo.MarkRead(ctx, ticketID, role)

	ticket.Unread = false
	return ticket, messages, nil
}

// ReplyAsUser 用户追问。关闭后不允许再回复。
func (s *TicketService) ReplyAsUser(ctx context.Context, ticketID, userID int64, content string) (*TicketMessage, error) {
	cleanContent, err := validateTicketContent(content)
	if err != nil {
		return nil, err
	}
	ticket, err := s.repo.GetByID(ctx, ticketID)
	if err != nil {
		return nil, err
	}
	if ticket == nil {
		return nil, infraerrors.NotFound("TICKET_NOT_FOUND", "ticket not found")
	}
	if ticket.UserID != userID {
		return nil, infraerrors.Forbidden("TICKET_FORBIDDEN", "ticket does not belong to current user")
	}
	if ticket.Status == TicketStatusClosed {
		return nil, infraerrors.BadRequest("TICKET_CLOSED", "ticket is closed")
	}
	if err := s.checkMessageRate(ctx, userID); err != nil {
		return nil, err
	}
	// 用户发言后重新回到「待管理员处理」
	return s.repo.AddMessage(ctx, ticketID, userID, TicketSenderRoleUser, cleanContent, TicketStatusPending)
}

// ReplyAsAdmin 管理员回复。回复即视为重新处理，已关闭的工单会被重新打开。
func (s *TicketService) ReplyAsAdmin(ctx context.Context, ticketID, adminID int64, content string) (*TicketMessage, error) {
	cleanContent, err := validateTicketContent(content)
	if err != nil {
		return nil, err
	}
	ticket, err := s.repo.GetByID(ctx, ticketID)
	if err != nil {
		return nil, err
	}
	if ticket == nil {
		return nil, NewTicketNotFoundError(ticketID)
	}
	return s.repo.AddMessage(ctx, ticketID, adminID, TicketSenderRoleAdmin, cleanContent, TicketStatusReplied)
}

// Delete 软删除工单。
//
// userID > 0 时校验归属（用户只能删自己的）；userID == 0 表示管理端。
func (s *TicketService) Delete(ctx context.Context, ticketID, userID int64) error {
	ticket, err := s.repo.GetByID(ctx, ticketID)
	if err != nil {
		return err
	}
	if ticket == nil {
		return NewTicketNotFoundError(ticketID)
	}
	if userID > 0 && ticket.UserID != userID {
		return infraerrors.Forbidden("TICKET_FORBIDDEN", "ticket does not belong to current user")
	}
	return s.repo.SoftDelete(ctx, ticketID)
}

// UnreadCount 侧栏红点用。userID>0 查该用户的未读；userID==0 表示管理端全局未读。
func (s *TicketService) UnreadCount(ctx context.Context, userID int64) (int64, error) {
	role := TicketSenderRoleAdmin
	if userID > 0 {
		role = TicketSenderRoleUser
	}
	return s.repo.CountUnread(ctx, userID, role)
}

// UpdateStatus 管理端修改状态 / 优先级。
func (s *TicketService) UpdateStatus(ctx context.Context, ticketID int64, status string) (*Ticket, error) {
	normalized := normalizeTicketStatus(status)
	if normalized == "" {
		return nil, infraerrors.BadRequest("TICKET_STATUS_INVALID", "invalid ticket status")
	}
	if err := s.repo.UpdateStatus(ctx, ticketID, normalized); err != nil {
		return nil, err
	}
	return s.repo.GetByID(ctx, ticketID)
}

// TicketSenderRoleUser / Admin 消息发送方角色。
const (
	TicketSenderRoleUser  = "user"
	TicketSenderRoleAdmin = "admin"
)

// checkCreateRate 限制短时间内大量建单。
//
// 计数查询失败时**放行**：工单是用户求助通道，不该因为一次统计查询失败就把人挡在门外。
func (s *TicketService) checkCreateRate(ctx context.Context, userID int64) error {
	count, err := s.repo.CountUserTicketsSince(ctx, userID, time.Now().Add(-ticketCreateWindow))
	if err != nil {
		return nil
	}
	if count >= ticketCreateMaxPerWindow {
		return infraerrors.New(http.StatusTooManyRequests, "TICKET_CREATE_RATE_LIMITED",
			fmt.Sprintf("too many tickets created, please wait %d minutes", int(ticketCreateWindow.Minutes())))
	}
	return nil
}

// checkMessageRate 限制短时间刷消息。失败同样放行。
func (s *TicketService) checkMessageRate(ctx context.Context, userID int64) error {
	count, err := s.repo.CountUserMessagesSince(ctx, userID, time.Now().Add(-ticketMessageWindow))
	if err != nil {
		return nil
	}
	if count >= ticketMessageMaxPerWindow {
		return infraerrors.New(http.StatusTooManyRequests, "TICKET_MESSAGE_RATE_LIMITED",
			fmt.Sprintf("too many messages sent, please wait %d minutes", int(ticketMessageWindow.Minutes())))
	}
	return nil
}

func normalizeTicketParams(params pagination.PaginationParams) (pagination.PaginationParams, error) {
	if params.Page <= 0 {
		params.Page = 1
	}
	if params.PageSize <= 0 {
		params.PageSize = 20
	}
	if params.PageSize > ticketPageSizeMax {
		params.PageSize = ticketPageSizeMax
	}
	if params.SortBy == "" {
		params.SortBy = "last_message_at"
	}
	if params.SortOrder == "" {
		params.SortOrder = "desc"
	}
	if params.SortBy != "last_message_at" && params.SortBy != "created_at" {
		params.SortBy = "last_message_at"
	}
	// 只接受这两种排序字段，避免把用户输入拼进 SQL
	if params.SortOrder != "asc" && params.SortOrder != "desc" {
		params.SortOrder = "desc"
	}
	return params, nil
}

// NewTicketNotFoundError 统一构造「工单不存在」错误，仓储层也会用到。
func NewTicketNotFoundError(id int64) error {
	return infraerrors.NotFound("TICKET_NOT_FOUND", fmt.Sprintf("ticket %d not found", id))
}
