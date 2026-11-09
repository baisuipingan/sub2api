package service

import (
	"bytes"
	"context"
	"encoding/binary"
	"errors"
	"hash/crc32"
	"image"
	"image/color"
	"image/png"
	"strings"
	"testing"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/config"
	infraerrors "github.com/Wei-Shaw/sub2api/internal/pkg/errors"
	"github.com/stretchr/testify/require"
)

func ticketTestPNG(t *testing.T, w, h int) []byte {
	t.Helper()
	img := image.NewNRGBA(image.Rect(0, 0, w, h))
	img.Set(0, 0, color.NRGBA{R: 255, A: 255})
	var b bytes.Buffer
	require.NoError(t, png.Encode(&b, img))
	return b.Bytes()
}
func TestTicketImageValidationAndVariants(t *testing.T) {
	raw := ticketTestPNG(t, 2800, 100)
	normal, thumb, w, h, mime, tw, th, _, err := prepareTicketImage(raw)
	require.NoError(t, err)
	require.Equal(t, 2560, w)
	require.Equal(t, 91, h)
	require.Equal(t, 480, tw)
	require.Equal(t, 17, th)
	require.Equal(t, "image/png", mime)
	_, format, err := image.DecodeConfig(bytes.NewReader(normal))
	require.NoError(t, err)
	require.Equal(t, "png", format)
	require.NotEmpty(t, thumb)
	forged := append([]byte(nil), raw...)
	binary.BigEndian.PutUint32(forged[16:20], 4000)
	binary.BigEndian.PutUint32(forged[20:24], 4000)
	binary.BigEndian.PutUint32(forged[29:33], crc32.ChecksumIEEE(forged[12:29]))
	_, _, _, _, _, _, _, _, err = prepareTicketImage(forged)
	require.ErrorIs(t, err, ErrTicketAttachmentInvalid)
	_, _, _, _, _, _, _, _, err = prepareTicketImage([]byte("<svg/>"))
	require.ErrorIs(t, err, ErrTicketAttachmentInvalid)
	animation := make([]byte, 20)
	binary.BigEndian.PutUint32(animation[:4], 8)
	copy(animation[4:8], "acTL")
	binary.BigEndian.PutUint32(animation[8:12], 2)
	binary.BigEndian.PutUint32(animation[16:20], crc32.ChecksumIEEE(animation[4:16]))
	animated := append(append(append([]byte{}, raw[:33]...), animation...), raw[33:]...)
	_, _, _, _, _, _, _, _, err = prepareTicketImage(animated)
	require.ErrorIs(t, err, ErrTicketAttachmentInvalid)
}

type ticketAttachmentRepoStub struct {
	TicketAttachmentRepository
	preflightErr   error
	previous       *TicketSubmissionResult
	reserved       []TicketStoredAttachment
	deleting       bool
	finalized      []string
	submitErr      error
	submissions    int
	committed      bool
	cleanupStarted chan struct{}
	lastSubmission TicketSubmission
}

func (r *ticketAttachmentRepoStub) FindTicketOperation(context.Context, string, string) (*TicketSubmissionResult, error) {
	return r.previous, nil
}
func (r *ticketAttachmentRepoStub) CheckTicketSubmission(context.Context, TicketSubmission) error {
	return r.preflightErr
}
func (r *ticketAttachmentRepoStub) ReserveTicketAttachments(_ context.Context, assets []TicketStoredAttachment) error {
	r.reserved = assets
	return nil
}
func (r *ticketAttachmentRepoStub) SubmitTicket(_ context.Context, in TicketSubmission) (*TicketSubmissionResult, error) {
	r.submissions++
	r.lastSubmission = in
	if r.submitErr != nil {
		return nil, r.submitErr
	}
	ticket := &Ticket{ID: 1, UserID: in.OwnerID, Title: in.Title, Priority: in.Priority, Status: TicketStatusPending}
	msg := &TicketMessage{ID: int64(r.submissions), TicketID: 1, SenderID: &in.SenderID, SenderRole: in.Role, Content: in.Content}
	for i := range r.reserved {
		if r.reserved[i].OperationID == in.OperationID {
			r.reserved[i].State = "ready"
			r.reserved[i].MessageID = msg.ID
			msg.Attachments = append(msg.Attachments, r.reserved[i].TicketAttachment)
		}
	}
	return &TicketSubmissionResult{Ticket: ticket, Message: msg}, nil
}
func (r *ticketAttachmentRepoStub) MarkTicketOperationAttachmentsDeleting(context.Context, string) error {
	if !r.committed {
		r.deleting = true
	}
	return nil
}
func (r *ticketAttachmentRepoStub) LockTicketAttachmentCleanup(context.Context) (func(), error) {
	return func() {}, nil
}
func (r *ticketAttachmentRepoStub) ListDeletingTicketOperationAttachments(context.Context, string) ([]TicketStoredAttachment, error) {
	if r.deleting {
		return r.reserved, nil
	}
	return nil, nil
}
func (r *ticketAttachmentRepoStub) FinalizeTicketAttachmentDelete(_ context.Context, id string) error {
	r.finalized = append(r.finalized, id)
	return nil
}

type ticketAttachmentStoreStub struct {
	puts, deletes  int
	failPut        bool
	targetReleased *bool
	objects        map[string][]byte
	presignedKeys  []string
}

func (s *ticketAttachmentStoreStub) Put(_ context.Context, key, _ string, data []byte) error {
	s.puts++
	if s.targetReleased != nil && !*s.targetReleased {
		return errors.New("target lock held during put")
	}
	if s.failPut {
		return errors.New("s3 down")
	}
	if s.objects == nil {
		s.objects = make(map[string][]byte)
	}
	s.objects[key] = append([]byte(nil), data...)
	return nil
}
func (s *ticketAttachmentStoreStub) Delete(context.Context, string) error { s.deletes++; return nil }
func (s *ticketAttachmentStoreStub) PresignGet(_ context.Context, key string, _ time.Duration) (string, error) {
	s.presignedKeys = append(s.presignedKeys, key)
	return "https://private.test/signed/" + key, nil
}
func ticketAttachmentFixture(repo *ticketAttachmentRepoStub, store *ticketAttachmentStoreStub) *TicketAttachmentService {
	return NewTicketAttachmentService(repo, func(context.Context, bool) (*config.ImageStorageConfig, func(), error) {
		return &config.ImageStorageConfig{Endpoint: "https://r2.test", Region: "auto", Bucket: "private"}, func() {
			if store.targetReleased != nil {
				*store.targetReleased = true
			}
		}, nil
	}, func(context.Context, *config.ImageStorageConfig) (GalleryObjectStore, error) { return store, nil })
}
func TestTicketAttachmentPreflightAndLimitsBeforeS3(t *testing.T) {
	repo := &ticketAttachmentRepoStub{preflightErr: ErrTicketOpenLimit}
	store := &ticketAttachmentStoreStub{}
	svc := ticketAttachmentFixture(repo, store)
	input := TicketSubmission{OwnerID: 1, SenderID: 1, Role: TicketSenderRoleUser, OperationID: "op"}
	_, err := svc.submit(context.Background(), input, []TicketAttachmentUpload{{Data: []byte("not even decoded")}})
	require.ErrorIs(t, err, ErrTicketOpenLimit)
	require.Zero(t, store.puts)
	_, err = svc.submit(context.Background(), input, make([]TicketAttachmentUpload, 4))
	require.ErrorIs(t, err, ErrTicketAttachmentTooMany)
	_, err = svc.submit(context.Background(), input, []TicketAttachmentUpload{{Data: make([]byte, TicketMaxAttachmentBytes+1)}})
	require.ErrorIs(t, err, ErrTicketAttachmentTooLarge)
}
func TestTicketAttachmentFailureCleansKnownObjectsAndReleasesTarget(t *testing.T) {
	released := false
	repo := &ticketAttachmentRepoStub{}
	store := &ticketAttachmentStoreStub{failPut: true, targetReleased: &released}
	svc := ticketAttachmentFixture(repo, store)
	_, err := svc.submit(context.Background(), TicketSubmission{OwnerID: 1, SenderID: 1, Role: TicketSenderRoleUser, OperationID: "op"}, []TicketAttachmentUpload{{Data: ticketTestPNG(t, 10, 10)}})
	require.Equal(t, "TICKET_STORAGE_UNAVAILABLE", infraerrors.Reason(err))
	require.True(t, released)
	require.True(t, repo.deleting)
	require.Zero(t, repo.submissions)
	require.Equal(t, 2, store.deletes)
	require.Len(t, repo.finalized, 1)
	require.True(t, strings.HasPrefix(repo.reserved[0].ImageKey, "tickets/v1/1/"))
}
func TestTicketAttachmentReplaySkipsUploadAndTextStillUsesOperation(t *testing.T) {
	repo := &ticketAttachmentRepoStub{previous: &TicketSubmissionResult{Ticket: &Ticket{ID: 42}, Message: &TicketMessage{ID: 43}}}
	store := &ticketAttachmentStoreStub{}
	svc := ticketAttachmentFixture(repo, store)
	result, err := svc.submit(context.Background(), TicketSubmission{OwnerID: 1, OperationID: "op"}, []TicketAttachmentUpload{{Data: []byte("skipped decode")}})
	require.NoError(t, err)
	require.Equal(t, int64(42), result.Ticket.ID)
	require.Zero(t, store.puts)
	repo.previous = nil
	_, err = svc.submit(context.Background(), TicketSubmission{OwnerID: 1, OperationID: "text-op"}, nil)
	require.NoError(t, err)
	require.Equal(t, 1, repo.submissions)
	require.Zero(t, store.puts)
}

func (r *ticketAttachmentRepoStub) ClaimTicketAttachmentCleanup(ctx context.Context, _ time.Time, _ int) ([]TicketStoredAttachment, error) {
	if r.cleanupStarted != nil {
		close(r.cleanupStarted)
		<-ctx.Done()
		return nil, ctx.Err()
	}
	return nil, nil
}
func TestTicketAmbiguousCommitDoesNotDeleteReadyAttachment(t *testing.T) {
	repo := &ticketAttachmentRepoStub{submitErr: errors.New("commit connection lost"), committed: true}
	store := &ticketAttachmentStoreStub{}
	svc := ticketAttachmentFixture(repo, store)
	_, err := svc.submit(context.Background(), TicketSubmission{OwnerID: 1, SenderID: 1, Role: TicketSenderRoleUser, OperationID: "op"}, []TicketAttachmentUpload{{Data: ticketTestPNG(t, 10, 10)}})
	require.Error(t, err)
	require.Equal(t, 2, store.puts)
	require.Zero(t, store.deletes)
	require.Empty(t, repo.finalized)
}
func TestTicketCleanerStopCancelsAndWaits(t *testing.T) {
	repo := &ticketAttachmentRepoStub{cleanupStarted: make(chan struct{})}
	svc := ticketAttachmentFixture(repo, &ticketAttachmentStoreStub{})
	svc.StartCleaner()
	select {
	case <-repo.cleanupStarted:
	case <-time.After(time.Second):
		t.Fatal("cleaner did not start")
	}
	stopped := make(chan struct{})
	go func() { svc.Stop(); close(stopped) }()
	select {
	case <-stopped:
	case <-time.After(time.Second):
		t.Fatal("Stop did not cancel and join cleaner")
	}
	svc.Stop()
	svc.StartCleaner()
}

func (r *ticketAttachmentRepoStub) GetTicketAttachment(_ context.Context, ticketID int64, id string, userID int64) (*TicketStoredAttachment, error) {
	for _, asset := range r.reserved {
		if asset.ID == id && asset.State == "ready" && ticketID == 1 && (userID == 0 || userID == asset.OwnerID) {
			return &asset, nil
		}
	}
	return nil, infraerrors.NotFound("TICKET_ATTACHMENT_NOT_FOUND", "not found")
}
func TestTicketServiceAttachmentSuccessAndAuthorizedVariants(t *testing.T) {
	ctx := context.Background()
	released := false
	attachmentRepo := &ticketAttachmentRepoStub{}
	store := &ticketAttachmentStoreStub{targetReleased: &released}
	attachments := ticketAttachmentFixture(attachmentRepo, store)
	repo := newStubTicketRepo()
	svc := NewTicketService(repo)
	svc.SetAttachmentService(attachments)
	ticket, err := svc.CreateWithAttachments(ctx, 7, "  Screenshot report  ", "  Detailed description  ", "high", "create-success", []TicketAttachmentUpload{{Data: ticketTestPNG(t, 1000, 500)}})
	require.NoError(t, err)
	require.Equal(t, "Screenshot report", ticket.Title)
	require.Equal(t, int64(7), ticket.UserID)
	require.Equal(t, "Detailed description", attachmentRepo.lastSubmission.Content)
	require.True(t, released)
	require.Equal(t, 2, store.puts)
	require.False(t, attachmentRepo.deleting)
	asset := attachmentRepo.reserved[0]
	require.Equal(t, "ready", asset.State)
	require.Equal(t, int64(1), asset.MessageID)
	require.NotEmpty(t, asset.CacheVersion)
	before := time.Now()
	preview, err := attachments.GetURL(ctx, ticket.ID, asset.ID, 7, "preview")
	require.NoError(t, err)
	require.Equal(t, asset.Bytes, preview.Bytes)
	require.Equal(t, 1000, preview.Width)
	require.Equal(t, 500, preview.Height)
	require.Equal(t, asset.MIME, preview.MIME)
	require.Equal(t, asset.CacheVersion, preview.CacheVersion)
	require.Equal(t, "https://private.test/signed/"+asset.ImageKey, preview.URL)
	require.WithinDuration(t, before.Add(10*time.Minute), preview.ExpiresAt, time.Second)
	thumbnail, err := attachments.GetURL(ctx, ticket.ID, asset.ID, 7, "thumbnail")
	require.NoError(t, err)
	require.Equal(t, asset.ThumbnailBytes, thumbnail.Bytes)
	require.Equal(t, 480, thumbnail.Width)
	require.Equal(t, 240, thumbnail.Height)
	require.Equal(t, asset.ThumbnailMIME, thumbnail.MIME)
	require.Equal(t, preview.CacheVersion, thumbnail.CacheVersion)
	require.Equal(t, asset.ThumbnailKey, store.presignedKeys[1])
	require.Equal(t, "https://private.test/signed/"+asset.ThumbnailKey, thumbnail.URL)
	_, err = attachments.GetURL(ctx, ticket.ID, asset.ID, 8, "preview")
	require.Equal(t, "TICKET_ATTACHMENT_NOT_FOUND", infraerrors.Reason(err))
	require.Len(t, store.presignedKeys, 2, "unauthorized reads must not sign a URL")
	repo.tickets[ticket.ID] = ticket
	reply, err := svc.ReplyAsUserWithAttachments(ctx, ticket.ID, 7, " Follow up ", "reply-success", []TicketAttachmentUpload{{Data: ticketTestPNG(t, 200, 100)}})
	require.NoError(t, err)
	require.Equal(t, "Follow up", reply.Content)
	require.Len(t, reply.Attachments, 1)
	require.Equal(t, 4, store.puts)
	require.Equal(t, int64(1), attachmentRepo.lastSubmission.TicketID)
	require.Equal(t, int64(7), attachmentRepo.lastSubmission.OwnerID)
	require.Equal(t, TicketSenderRoleUser, attachmentRepo.lastSubmission.Role)
}
