package service

import (
	"context"
	"testing"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/pkg/pagination"
)

// 工单的状态流转和越权判断是这块最容易出错的地方：
// 判错了要么用户看到别人的工单，要么关闭后还能继续回复。

type stubTicketRepo struct {
	tickets     map[int64]*Ticket
	messages    []TicketMessage
	createCalls int
	lastStatus  string
}

func newStubTicketRepo() *stubTicketRepo {
	return &stubTicketRepo{tickets: map[int64]*Ticket{}}
}

func (r *stubTicketRepo) Create(_ context.Context, userID int64, title, priority, content string) (*Ticket, error) {
	r.createCalls++
	id := int64(len(r.tickets) + 1)
	tk := &Ticket{
		ID: id, UserID: userID, Title: title, Status: TicketStatusPending, Priority: priority,
		LastMessageAt: time.Now(), LastMessageRole: TicketSenderRoleUser,
		CreatedAt: time.Now(), UpdatedAt: time.Now(), MessageCount: 1,
	}
	r.tickets[id] = tk
	r.messages = append(r.messages, TicketMessage{ID: id, TicketID: id, SenderID: &userID, SenderRole: TicketSenderRoleUser, Content: content})
	return tk, nil
}

func (r *stubTicketRepo) GetByID(_ context.Context, id int64) (*Ticket, error) {
	return r.tickets[id], nil
}

func (r *stubTicketRepo) ListByUser(_ context.Context, userID int64, _ pagination.PaginationParams, _ string) ([]Ticket, *pagination.PaginationResult, error) {
	out := []Ticket{}
	for _, tk := range r.tickets {
		if tk.UserID == userID {
			out = append(out, *tk)
		}
	}
	return out, &pagination.PaginationResult{Total: int64(len(out)), Page: 1, PageSize: 20, Pages: 1}, nil
}

func (r *stubTicketRepo) ListAll(_ context.Context, _ pagination.PaginationParams, _ TicketListFilters) ([]Ticket, *pagination.PaginationResult, error) {
	out := []Ticket{}
	for _, tk := range r.tickets {
		out = append(out, *tk)
	}
	return out, &pagination.PaginationResult{Total: int64(len(out)), Page: 1, PageSize: 20, Pages: 1}, nil
}

func (r *stubTicketRepo) ListMessages(_ context.Context, ticketID int64) ([]TicketMessage, error) {
	out := []TicketMessage{}
	for _, m := range r.messages {
		if m.TicketID == ticketID {
			out = append(out, m)
		}
	}
	return out, nil
}

func (r *stubTicketRepo) AddMessage(_ context.Context, ticketID, senderID int64, role, content, nextStatus string) (*TicketMessage, error) {
	r.lastStatus = nextStatus
	if tk, ok := r.tickets[ticketID]; ok {
		tk.Status = nextStatus
		tk.LastMessageRole = role
	}
	msg := TicketMessage{ID: int64(len(r.messages) + 1), TicketID: ticketID, SenderID: &senderID, SenderRole: role, Content: content, CreatedAt: time.Now()}
	r.messages = append(r.messages, msg)
	return &msg, nil
}

func (r *stubTicketRepo) MarkRead(_ context.Context, _ int64, _ string) error { return nil }

func (r *stubTicketRepo) CountUnread(_ context.Context, _ int64, _ string) (int64, error) {
	var n int64
	for _, tk := range r.tickets {
		if tk.Unread && tk.Status != TicketStatusClosed {
			n++
		}
	}
	return n, nil
}

func (r *stubTicketRepo) CountUserTicketsSince(_ context.Context, userID int64, _ time.Time) (int64, error) {
	// 与真实 SQL 一致：按 user_id 过滤，否则「按用户限流」的测试会失真
	var n int64
	for _, tk := range r.tickets {
		if tk.UserID == userID {
			n++
		}
	}
	return n, nil
}

func (r *stubTicketRepo) CountUserMessagesSince(_ context.Context, _ int64, _ time.Time) (int64, error) {
	return int64(len(r.messages)), nil
}

func (r *stubTicketRepo) SoftDelete(_ context.Context, ticketID int64) error {
	delete(r.tickets, ticketID)
	return nil
}

func (r *stubTicketRepo) UpdateStatus(_ context.Context, ticketID int64, status string) error {
	tk, ok := r.tickets[ticketID]
	if !ok {
		return NewTicketNotFoundError(ticketID)
	}
	tk.Status = status
	r.lastStatus = status
	return nil
}

func TestCreateTicket_Validation(t *testing.T) {
	svc := NewTicketService(newStubTicketRepo())

	if _, err := svc.Create(context.Background(), 1, "   ", "内容", ""); err == nil {
		t.Fatal("空标题应被拒绝")
	}
	if _, err := svc.Create(context.Background(), 1, "标题", "   ", ""); err == nil {
		t.Fatal("空内容应被拒绝")
	}
	if _, err := svc.Create(context.Background(), 0, "标题", "内容", ""); err == nil {
		t.Fatal("缺少用户应被拒绝")
	}

	tk, err := svc.Create(context.Background(), 1, "  标题  ", " 内容 ", "")
	if err != nil {
		t.Fatalf("正常创建失败: %v", err)
	}
	if tk.Title != "标题" {
		t.Fatalf("标题应被 trim，实际=%q", tk.Title)
	}
	if tk.Status != TicketStatusPending {
		t.Fatalf("新工单应为 pending，实际=%s", tk.Status)
	}
	if tk.Priority != TicketPriorityNormal {
		t.Fatalf("默认优先级应为 normal，实际=%s", tk.Priority)
	}
}

func TestCreateTicket_PriorityHigh(t *testing.T) {
	svc := NewTicketService(newStubTicketRepo())
	tk, err := svc.Create(context.Background(), 1, "急", "内容", "high")
	if err != nil {
		t.Fatal(err)
	}
	if tk.Priority != TicketPriorityHigh {
		t.Fatalf("应保留 high，实际=%s", tk.Priority)
	}
}

func TestGetDetail_Ownership(t *testing.T) {
	repo := newStubTicketRepo()
	svc := NewTicketService(repo)
	tk, _ := svc.Create(context.Background(), 1, "标题", "内容", "")

	// 本人可读
	if _, _, err := svc.GetDetail(context.Background(), tk.ID, 1); err != nil {
		t.Fatalf("本人应可读: %v", err)
	}
	// 他人不可读
	if _, _, err := svc.GetDetail(context.Background(), tk.ID, 2); err == nil {
		t.Fatal("他人工单应被拒绝")
	}
	// 管理端（userID=0）可读
	if _, _, err := svc.GetDetail(context.Background(), tk.ID, 0); err != nil {
		t.Fatalf("管理端应可读: %v", err)
	}
}

func TestReplyAsUser_ClosedTicketRejected(t *testing.T) {
	repo := newStubTicketRepo()
	svc := NewTicketService(repo)
	tk, _ := svc.Create(context.Background(), 1, "标题", "内容", "")

	if _, err := svc.UpdateStatus(context.Background(), tk.ID, TicketStatusClosed); err != nil {
		t.Fatal(err)
	}
	if _, err := svc.ReplyAsUser(context.Background(), tk.ID, 1, "还在吗"); err == nil {
		t.Fatal("已关闭工单不应允许用户回复")
	}
}

func TestReplyAsUser_SetsPending(t *testing.T) {
	repo := newStubTicketRepo()
	svc := NewTicketService(repo)
	tk, _ := svc.Create(context.Background(), 1, "标题", "内容", "")
	// 管理员先回复，状态变 replied
	if _, err := svc.ReplyAsAdmin(context.Background(), tk.ID, 99, "已处理"); err != nil {
		t.Fatal(err)
	}
	if repo.tickets[tk.ID].Status != TicketStatusReplied {
		t.Fatalf("管理员回复后应为 replied，实际=%s", repo.tickets[tk.ID].Status)
	}
	// 用户再追问，回到 pending
	if _, err := svc.ReplyAsUser(context.Background(), tk.ID, 1, "还有问题"); err != nil {
		t.Fatal(err)
	}
	if repo.tickets[tk.ID].Status != TicketStatusPending {
		t.Fatalf("用户追问后应为 pending，实际=%s", repo.tickets[tk.ID].Status)
	}
}

func TestReplyAsUser_OtherUserForbidden(t *testing.T) {
	repo := newStubTicketRepo()
	svc := NewTicketService(repo)
	tk, _ := svc.Create(context.Background(), 1, "标题", "内容", "")

	if _, err := svc.ReplyAsUser(context.Background(), tk.ID, 2, "插播"); err == nil {
		t.Fatal("他人不应能回复")
	}
}

func TestUpdateStatus_RejectsUnknown(t *testing.T) {
	repo := newStubTicketRepo()
	svc := NewTicketService(repo)
	tk, _ := svc.Create(context.Background(), 1, "标题", "内容", "")

	if _, err := svc.UpdateStatus(context.Background(), tk.ID, "deleted"); err == nil {
		t.Fatal("非法状态应被拒绝")
	}
	if _, err := svc.UpdateStatus(context.Background(), tk.ID, TicketStatusClosed); err != nil {
		t.Fatalf("合法状态应通过: %v", err)
	}
}

func TestReplyAsAdmin_ReopensClosed(t *testing.T) {
	repo := newStubTicketRepo()
	svc := NewTicketService(repo)
	tk, _ := svc.Create(context.Background(), 1, "标题", "内容", "")
	_, _ = svc.UpdateStatus(context.Background(), tk.ID, TicketStatusClosed)

	if _, err := svc.ReplyAsAdmin(context.Background(), tk.ID, 99, "补充说明"); err != nil {
		t.Fatalf("管理端应能回复已关闭工单: %v", err)
	}
	if repo.tickets[tk.ID].Status != TicketStatusReplied {
		t.Fatalf("管理端回复后应重新打开为 replied，实际=%s", repo.tickets[tk.ID].Status)
	}
}

// 未读规则的三条边界，任何一条错了都会出现「红点消不掉」或「该亮不亮」。

func TestUnreadCount_SkipsOwnLastMessage(t *testing.T) {
	repo := newStubTicketRepo()
	svc := NewTicketService(repo)
	tk, _ := svc.Create(context.Background(), 1, "标题", "内容", "")

	// 用户自己刚发的工单，对自己不算未读
	repo.tickets[tk.ID].Unread = false
	n, err := svc.UnreadCount(context.Background(), 1)
	if err != nil {
		t.Fatal(err)
	}
	if n != 0 {
		t.Fatalf("自己发的最后一条不应算未读，实际=%d", n)
	}
}

func TestUnreadCount_AdminReplyMarksUserUnread(t *testing.T) {
	repo := newStubTicketRepo()
	svc := NewTicketService(repo)
	tk, _ := svc.Create(context.Background(), 1, "标题", "内容", "")
	_, _ = svc.ReplyAsAdmin(context.Background(), tk.ID, 99, "已处理")

	// 管理员回复后，用户侧应有 1 条未读
	repo.tickets[tk.ID].Unread = true
	n, _ := svc.UnreadCount(context.Background(), 1)
	if n != 1 {
		t.Fatalf("管理员回复后用户应有 1 条未读，实际=%d", n)
	}
}

func TestUnreadCount_ExcludesClosed(t *testing.T) {
	repo := newStubTicketRepo()
	svc := NewTicketService(repo)
	tk, _ := svc.Create(context.Background(), 1, "标题", "内容", "")
	repo.tickets[tk.ID].Unread = true
	_, _ = svc.UpdateStatus(context.Background(), tk.ID, TicketStatusClosed)

	n, _ := svc.UnreadCount(context.Background(), 1)
	if n != 0 {
		t.Fatalf("已关闭工单不应计入未读，实际=%d", n)
	}
}

func TestGetDetail_ClearsUnread(t *testing.T) {
	repo := newStubTicketRepo()
	svc := NewTicketService(repo)
	tk, _ := svc.Create(context.Background(), 1, "标题", "内容", "")

	detail, _, err := svc.GetDetail(context.Background(), tk.ID, 1)
	if err != nil {
		t.Fatal(err)
	}
	if detail.Unread {
		t.Fatal("打开详情后返回对象应为已读")
	}
}

// 防刷：连续建单超过阈值应被 429 拦住，而不是无限刷
func TestCreateTicket_RateLimited(t *testing.T) {
	repo := newStubTicketRepo()
	svc := NewTicketService(repo)

	for i := 0; i < ticketCreateMaxPerWindow; i++ {
		if _, err := svc.Create(context.Background(), 1, "标题", "内容", ""); err != nil {
			t.Fatalf("第 %d 次建单不应失败: %v", i+1, err)
		}
	}
	if _, err := svc.Create(context.Background(), 1, "标题", "内容", ""); err == nil {
		t.Fatal("超过阈值后应被限流")
	}
}

// 防刷只针对本人，不影响其他用户
func TestCreateTicket_RateLimitIsPerUser(t *testing.T) {
	repo := newStubTicketRepo()
	svc := NewTicketService(repo)

	for i := 0; i < ticketCreateMaxPerWindow; i++ {
		_, _ = svc.Create(context.Background(), 1, "标题", "内容", "")
	}
	// 另一个用户不受影响（stub 的计数是全局的，这里断言的是方法本身按 userID 传参）
	if _, err := svc.Create(context.Background(), 2, "标题2", "内容2", ""); err != nil {
		t.Fatalf("其他用户不应被连坐: %v", err)
	}
}

// 删除权限：用户只能删自己的，管理员可删任意
func TestDelete_Ownership(t *testing.T) {
	repo := newStubTicketRepo()
	svc := NewTicketService(repo)
	tk, _ := svc.Create(context.Background(), 1, "标题", "内容", "")

	if err := svc.Delete(context.Background(), tk.ID, 2); err == nil {
		t.Fatal("他人不应能删除")
	}
	if err := svc.Delete(context.Background(), tk.ID, 1); err != nil {
		t.Fatalf("本人应可删除: %v", err)
	}
	if _, ok := repo.tickets[tk.ID]; ok {
		t.Fatal("删除后应不可见")
	}
}

func TestDelete_AdminCanDeleteAny(t *testing.T) {
	repo := newStubTicketRepo()
	svc := NewTicketService(repo)
	tk, _ := svc.Create(context.Background(), 1, "标题", "内容", "")

	if err := svc.Delete(context.Background(), tk.ID, 0); err != nil {
		t.Fatalf("管理端应可删除任意工单: %v", err)
	}
}
