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
