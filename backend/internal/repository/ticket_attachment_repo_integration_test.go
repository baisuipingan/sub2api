//go:build integration

package repository

import (
	"context"
	"fmt"
	"strings"
	"sync"
	"testing"
	"time"

	infraerrors "github.com/Wei-Shaw/sub2api/internal/pkg/errors"
	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/stretchr/testify/require"
)

func ticketTestUser(t *testing.T) int64 {
	t.Helper()
	var id int64
	require.NoError(t, integrationDB.QueryRow(`INSERT INTO users(email,password_hash) VALUES($1,'ticket-test') RETURNING id`, fmt.Sprintf("ticket-%d@example.test", time.Now().UnixNano())).Scan(&id))
	t.Cleanup(func() {
		_, _ = integrationDB.Exec(`DELETE FROM ticket_message_attachments WHERE owner_id=$1`, id)
		_, _ = integrationDB.Exec(`DELETE FROM users WHERE id=$1`, id)
	})
	return id
}
func ticketClosedFixture(t *testing.T, userID int64) int64 {
	t.Helper()
	var id int64
	require.NoError(t, integrationDB.QueryRow(`INSERT INTO tickets(user_id,title,status,created_at) VALUES($1,'closed','closed',NOW()-INTERVAL '1 hour') RETURNING id`, userID).Scan(&id))
	return id
}
func TestTicketConcurrentCreateHardOpenLimit(t *testing.T) {
	ctx := context.Background()
	userID := ticketTestUser(t)
	repo := &ticketRepository{db: integrationDB}
	var wg sync.WaitGroup
	var mu sync.Mutex
	success := 0
	var failures []error
	for i := 0; i < 12; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			_, err := repo.Create(ctx, userID, "title", "normal", "content")
			mu.Lock()
			defer mu.Unlock()
			if err == nil {
				success++
			} else {
				failures = append(failures, err)
			}
		}()
	}
	wg.Wait()
	require.Equal(t, 5, success)
	for _, err := range failures {
		require.Equal(t, "TICKET_OPEN_LIMIT", infraerrors.Reason(err))
	}
	var count int
	require.NoError(t, integrationDB.QueryRow(`SELECT COUNT(*) FROM tickets WHERE user_id=$1 AND status<>'closed' AND deleted_at IS NULL`, userID).Scan(&count))
	require.Equal(t, 5, count)
	closedID := ticketClosedFixture(t, userID)
	require.ErrorIs(t, repo.UpdateStatus(ctx, closedID, service.TicketStatusPending), service.ErrTicketOpenLimit)
	_, err := repo.AddMessage(ctx, closedID, userID, service.TicketSenderRoleAdmin, "admin", "replied")
	require.ErrorIs(t, err, service.ErrTicketOpenLimit)
}
func TestTicketConcurrentReopenAndReplyStatusChecks(t *testing.T) {
	ctx := context.Background()
	userID := ticketTestUser(t)
	repo := &ticketRepository{db: integrationDB}
	for i := 0; i < 4; i++ {
		_, err := repo.Create(ctx, userID, "title", "normal", "content")
		require.NoError(t, err)
	}
	first, second := ticketClosedFixture(t, userID), ticketClosedFixture(t, userID)
	var wg sync.WaitGroup
	errs := make(chan error, 2)
	for _, id := range []int64{first, second} {
		wg.Add(1)
		go func(id int64) { defer wg.Done(); errs <- repo.UpdateStatus(ctx, id, service.TicketStatusPending) }(id)
	}
	wg.Wait()
	close(errs)
	ok := 0
	for err := range errs {
		if err == nil {
			ok++
		} else {
			require.ErrorIs(t, err, service.ErrTicketOpenLimit)
		}
	}
	require.Equal(t, 1, ok)
	require.NoError(t, repo.UpdateStatus(ctx, first, service.TicketStatusClosed))
	_, err := repo.AddMessage(ctx, first, userID, service.TicketSenderRoleUser, "closed reply", "pending")
	require.Equal(t, "TICKET_CLOSED", infraerrors.Reason(err))
	require.NoError(t, repo.SoftDelete(ctx, first))
	_, err = repo.AddMessage(ctx, first, userID, service.TicketSenderRoleAdmin, "deleted reply", "replied")
	require.Equal(t, "TICKET_NOT_FOUND", infraerrors.Reason(err))
}
func TestTicketOperationReplayAndAttachmentLifecycle(t *testing.T) {
	ctx := context.Background()
	userID := ticketTestUser(t)
	repo := &ticketRepository{db: integrationDB}
	op := fmt.Sprintf("ticket-op-%d", userID)
	digest := strings.Repeat("b", 64)
	input := service.TicketSubmission{OwnerID: userID, SenderID: userID, Role: service.TicketSenderRoleUser, Title: "title", Content: "content", Priority: "normal", OperationID: op, Digest: digest}
	asset := service.TicketStoredAttachment{TicketAttachment: service.TicketAttachment{ID: strings.Repeat("a", 64), MIME: "image/png", Bytes: 10, Width: 10, Height: 10, ThumbnailWidth: 5, ThumbnailHeight: 5}, OwnerID: userID, OperationID: op, Digest: strings.Repeat("c", 64), Ordinal: 0, Endpoint: "https://r2.test", Region: "auto", Bucket: "private", ImageKey: "tickets/v1/img", ThumbnailKey: "tickets/v1/thumb", ThumbnailMIME: "image/png", ThumbnailBytes: 5}
	// Isolate fixed-length object identity across parallel tests.
	asset.ID = fmt.Sprintf("%064x", userID)
	require.NoError(t, repo.ReserveTicketAttachments(ctx, []service.TicketStoredAttachment{asset}))
	require.ErrorIs(t, repo.ReserveTicketAttachments(ctx, []service.TicketStoredAttachment{asset}), service.ErrTicketAttachmentBusy)
	input.Attachments = []service.TicketStoredAttachment{asset}
	result, err := repo.SubmitTicket(ctx, input)
	require.NoError(t, err)
	require.Len(t, result.Message.Attachments, 1)
	replay, err := repo.SubmitTicket(ctx, input)
	require.NoError(t, err)
	require.Equal(t, result.Message.ID, replay.Message.ID)
	require.Equal(t, result.Ticket.ID, replay.Ticket.ID)
	_, err = repo.FindTicketOperation(ctx, op, strings.Repeat("d", 64))
	require.ErrorIs(t, err, service.ErrTicketOperationConflict)
	_, err = repo.GetTicketAttachment(ctx, result.Ticket.ID, asset.ID, userID+1)
	require.Equal(t, "TICKET_ATTACHMENT_NOT_FOUND", infraerrors.Reason(err))
	_, err = repo.GetTicketAttachment(ctx, result.Ticket.ID+1, asset.ID, 0)
	require.Equal(t, "TICKET_ATTACHMENT_NOT_FOUND", infraerrors.Reason(err))
	require.NoError(t, repo.MarkTicketOperationAttachmentsDeleting(ctx, op))
	deleting, err := repo.ListDeletingTicketOperationAttachments(ctx, op)
	require.NoError(t, err)
	require.Empty(t, deleting, "ambiguous commit rollback must not delete a ready message attachment")
	require.NoError(t, repo.SoftDelete(ctx, result.Ticket.ID))
	_, err = repo.GetTicketAttachment(ctx, result.Ticket.ID, asset.ID, userID)
	require.Equal(t, "TICKET_ATTACHMENT_NOT_FOUND", infraerrors.Reason(err))
	cleanup, err := repo.ClaimTicketAttachmentCleanup(ctx, time.Now().Add(-time.Hour), 100)
	require.NoError(t, err)
	found := false
	for _, a := range cleanup {
		if a.ID == asset.ID {
			found = true
		}
	}
	require.True(t, found)
	require.NoError(t, repo.FinalizeTicketAttachmentDelete(ctx, asset.ID))
}

func TestTicketCleanupDoesNotRaceFreshUploadForDeletedUser(t *testing.T) {
	ctx := context.Background()
	userID := ticketTestUser(t)
	repo := &ticketRepository{db: integrationDB}
	op := fmt.Sprintf("pending-op-%d", userID)
	asset := service.TicketStoredAttachment{TicketAttachment: service.TicketAttachment{ID: fmt.Sprintf("%064x", userID), MIME: "image/png", Bytes: 10, Width: 10, Height: 10, ThumbnailWidth: 5, ThumbnailHeight: 5}, OwnerID: userID, OperationID: op, Digest: strings.Repeat("c", 64), Ordinal: 0, Endpoint: "https://r2.test", Region: "auto", Bucket: "private", ImageKey: "tickets/v1/pending", ThumbnailKey: "tickets/v1/thumb", ThumbnailMIME: "image/png", ThumbnailBytes: 5}
	require.NoError(t, repo.ReserveTicketAttachments(ctx, []service.TicketStoredAttachment{asset}))
	_, err := integrationDB.Exec(`UPDATE users SET deleted_at=NOW() WHERE id=$1`, userID)
	require.NoError(t, err)
	cleanup, err := repo.ClaimTicketAttachmentCleanup(ctx, time.Now().Add(-time.Hour), 100)
	require.NoError(t, err)
	for _, a := range cleanup {
		require.NotEqual(t, asset.ID, a.ID, "a fresh upload must finish/abort before physical cleanup")
	}
	_, err = integrationDB.Exec(`UPDATE ticket_message_attachments SET updated_at=NOW()-INTERVAL '2 hours' WHERE id=$1`, asset.ID)
	require.NoError(t, err)
	cleanup, err = repo.ClaimTicketAttachmentCleanup(ctx, time.Now().Add(-time.Hour), 100)
	require.NoError(t, err)
	found := false
	for _, a := range cleanup {
		if a.ID == asset.ID {
			found = true
		}
	}
	require.True(t, found)
}

func concurrentTicketOperation(t *testing.T, repo *ticketRepository, input service.TicketSubmission) *service.TicketSubmissionResult {
	t.Helper()
	const count = 12
	start := make(chan struct{})
	results := make(chan *service.TicketSubmissionResult, count)
	errs := make(chan error, count)
	var wg sync.WaitGroup
	for i := 0; i < count; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			<-start
			result, err := repo.SubmitTicket(context.Background(), input)
			if err != nil {
				errs <- err
				return
			}
			results <- result
		}()
	}
	close(start)
	wg.Wait()
	close(errs)
	close(results)
	for err := range errs {
		require.NoError(t, err)
	}
	var accepted *service.TicketSubmissionResult
	delivered := 0
	for result := range results {
		delivered++
		if accepted == nil {
			accepted = result
		}
		require.Equal(t, accepted.Ticket.ID, result.Ticket.ID)
		require.Equal(t, accepted.Message.ID, result.Message.ID)
	}
	require.Equal(t, count, delivered)
	return accepted
}
func TestTicketConcurrentSameOperationCreateReplay(t *testing.T) {
	userID := ticketTestUser(t)
	repo := &ticketRepository{db: integrationDB}
	input := service.TicketSubmission{OwnerID: userID, SenderID: userID, Role: service.TicketSenderRoleUser, Title: "same create", Content: "same description", Priority: "normal", OperationID: fmt.Sprintf("concurrent-create-%d", userID), Digest: strings.Repeat("a", 64)}
	result := concurrentTicketOperation(t, repo, input)
	var tickets, messages int
	require.NoError(t, integrationDB.QueryRow(`SELECT COUNT(*) FROM tickets WHERE user_id=$1`, userID).Scan(&tickets))
	require.Equal(t, 1, tickets)
	require.NoError(t, integrationDB.QueryRow(`SELECT COUNT(*) FROM ticket_messages WHERE ticket_id=$1`, result.Ticket.ID).Scan(&messages))
	require.Equal(t, 1, messages)
}
func TestTicketConcurrentSameOperationReplyReplay(t *testing.T) {
	userID := ticketTestUser(t)
	repo := &ticketRepository{db: integrationDB}
	ticket, err := repo.Create(context.Background(), userID, "initial", "normal", "first message")
	require.NoError(t, err)
	input := service.TicketSubmission{TicketID: ticket.ID, OwnerID: userID, SenderID: userID, Role: service.TicketSenderRoleUser, Content: "same follow up", OperationID: fmt.Sprintf("concurrent-reply-%d", userID), Digest: strings.Repeat("b", 64)}
	result := concurrentTicketOperation(t, repo, input)
	var messages int
	require.Equal(t, ticket.ID, result.Ticket.ID)
	require.NoError(t, integrationDB.QueryRow(`SELECT COUNT(*) FROM ticket_messages WHERE ticket_id=$1`, ticket.ID).Scan(&messages))
	require.Equal(t, 2, messages, "the initial message and exactly one follow-up must exist")
}
