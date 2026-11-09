package service

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/binary"
	"encoding/hex"
	"encoding/json"
	"image"
	"image/color"
	"image/draw"
	"image/jpeg"
	"image/png"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/config"
	infraerrors "github.com/Wei-Shaw/sub2api/internal/pkg/errors"
	xdraw "golang.org/x/image/draw"
)

const (
	TicketMaxAttachments        = 3
	TicketMaxAttachmentBytes    = 2 << 20
	ticketMaxImagePixels        = 12_000_000
	ticketAttachmentURLLifetime = 10 * time.Minute
)

var (
	ErrTicketOpenLimit          = infraerrors.Conflict("TICKET_OPEN_LIMIT", "每个用户最多同时有 5 个未关闭的工单，请等待处理，或删除不再需要的工单后再新建")
	ErrTicketAttachmentInvalid  = infraerrors.BadRequest("TICKET_ATTACHMENT_INVALID", "截图必须是有效的静态 PNG、JPEG 或 WebP 图片，且不超过 1200 万像素")
	ErrTicketAttachmentTooLarge = infraerrors.BadRequest("TICKET_ATTACHMENT_TOO_LARGE", "每张截图压缩后不能超过 2 MiB")
	ErrTicketAttachmentTooMany  = infraerrors.BadRequest("TICKET_ATTACHMENT_TOO_MANY", "每条消息最多附带 3 张截图")
	ErrTicketAttachmentBusy     = infraerrors.New(429, "TICKET_ATTACHMENT_BUSY", "截图处理繁忙，请稍后重试")
	ErrTicketOperationConflict  = infraerrors.Conflict("TICKET_OPERATION_CONFLICT", "该提交标识已用于不同的工单内容，请重新提交")
)

// TicketAttachment contains display metadata only. Object keys and bucket
// credentials remain server-side; authorized URL requests resolve them afresh.
type TicketAttachment struct {
	ID              string `json:"id"`
	MIME            string `json:"mime"`
	Bytes           int64  `json:"bytes"`
	Width           int    `json:"width"`
	Height          int    `json:"height"`
	CacheVersion    string `json:"cache_version"`
	ThumbnailWidth  int    `json:"thumbnail_width"`
	ThumbnailHeight int    `json:"thumbnail_height"`
}

type TicketAttachmentURL struct {
	URL          string    `json:"url"`
	ExpiresAt    time.Time `json:"expires_at"`
	Bytes        int64     `json:"bytes"`
	MIME         string    `json:"mime"`
	Width        int       `json:"width"`
	Height       int       `json:"height"`
	CacheVersion string    `json:"cache_version"`
}

type TicketAttachmentUpload struct{ Data []byte }

type TicketStoredAttachment struct {
	TicketAttachment
	OwnerID, MessageID                    int64
	OperationID, Digest                   string
	Ordinal                               int
	State                                 string
	Endpoint, Region, Bucket              string
	ImageKey, ThumbnailKey, ThumbnailMIME string
	ThumbnailBytes                        int64
}

type TicketSubmission struct {
	TicketID, OwnerID, SenderID    int64
	Role, Title, Content, Priority string
	OperationID, Digest            string
	Attachments                    []TicketStoredAttachment
}

type TicketSubmissionResult struct {
	Ticket  *Ticket
	Message *TicketMessage
}

type TicketAttachmentRepository interface {
	CheckTicketSubmission(context.Context, TicketSubmission) error
	FindTicketOperation(context.Context, string, string) (*TicketSubmissionResult, error)
	SubmitTicket(context.Context, TicketSubmission) (*TicketSubmissionResult, error)
	ReserveTicketAttachments(context.Context, []TicketStoredAttachment) error
	ListTicketAttachments(context.Context, int64) ([]TicketStoredAttachment, error)
	GetTicketAttachment(context.Context, int64, string, int64) (*TicketStoredAttachment, error)
	MarkTicketOperationAttachmentsDeleting(context.Context, string) error
	ListDeletingTicketOperationAttachments(context.Context, string) ([]TicketStoredAttachment, error)
	LockTicketAttachmentCleanup(context.Context) (func(), error)
	ClaimTicketAttachmentCleanup(context.Context, time.Time, int) ([]TicketStoredAttachment, error)
	FinalizeTicketAttachmentDelete(context.Context, string) error
	HasAttachments(context.Context) (bool, error)
}

// Resolver pins a private target while durable pending rows are registered.
// Release must run before S3 I/O: pending rows then protect the target themselves.
type TicketStorageResolver func(context.Context, bool) (*config.ImageStorageConfig, func(), error)

type TicketAttachmentService struct {
	repo                      TicketAttachmentRepository
	resolver                  TicketStorageResolver
	factory                   GalleryObjectStoreFactory
	uploadSlots, convertSlots chan struct{}
	stop                      chan struct{}
	startOnce, stopOnce       sync.Once
	lifecycleMu               sync.Mutex
	stopped                   bool
	cleanupCtx                context.Context
	cleanupCancel             context.CancelFunc
	cleanupWG                 sync.WaitGroup
}

func NewTicketAttachmentService(repo TicketAttachmentRepository, resolver TicketStorageResolver, factory GalleryObjectStoreFactory) *TicketAttachmentService {
	cleanupCtx, cancel := context.WithCancel(context.Background())
	return &TicketAttachmentService{repo: repo, resolver: resolver, factory: factory, uploadSlots: make(chan struct{}, 2), convertSlots: make(chan struct{}, 2), stop: make(chan struct{}), cleanupCtx: cleanupCtx, cleanupCancel: cancel}
}

func (s *TicketAttachmentService) AcquireUpload() (func(), error) {
	select {
	case s.uploadSlots <- struct{}{}:
		return func() { <-s.uploadSlots }, nil
	default:
		return nil, ErrTicketAttachmentBusy
	}
}

func (s *TicketAttachmentService) Status(ctx context.Context) (bool, error) {
	if s == nil || s.resolver == nil {
		return false, nil
	}
	_, release, err := s.resolver(ctx, true)
	if release != nil {
		release()
	}
	if infraerrors.Reason(err) == "TICKET_ATTACHMENTS_DISABLED" || infraerrors.Reason(err) == "TICKET_STORAGE_UNAVAILABLE" {
		return false, nil
	}
	return err == nil, err
}

func (s *TicketService) CreateWithAttachments(ctx context.Context, userID int64, title, content, priority, operationID string, uploads []TicketAttachmentUpload) (*Ticket, error) {
	title, err := validateTicketTitle(title)
	if err != nil {
		return nil, err
	}
	content, err = validateTicketContent(content)
	if err != nil {
		return nil, err
	}
	if userID <= 0 {
		return nil, infraerrors.BadRequest("TICKET_USER_REQUIRED", "user is required")
	}
	if s.attachments == nil {
		if len(uploads) > 0 {
			return nil, ticketStorageError()
		}
		return s.Create(ctx, userID, title, content, priority)
	}
	result, err := s.attachments.submit(ctx, TicketSubmission{OwnerID: userID, SenderID: userID, Role: TicketSenderRoleUser, Title: title, Content: content, Priority: normalizeTicketPriority(priority), OperationID: operationID}, uploads)
	if err != nil {
		return nil, err
	}
	return result.Ticket, nil
}

func (s *TicketService) ReplyAsUserWithAttachments(ctx context.Context, ticketID, userID int64, content, operationID string, uploads []TicketAttachmentUpload) (*TicketMessage, error) {
	return s.replyWithAttachments(ctx, ticketID, userID, TicketSenderRoleUser, content, operationID, uploads)
}
func (s *TicketService) ReplyAsAdminWithAttachments(ctx context.Context, ticketID, adminID int64, content, operationID string, uploads []TicketAttachmentUpload) (*TicketMessage, error) {
	return s.replyWithAttachments(ctx, ticketID, adminID, TicketSenderRoleAdmin, content, operationID, uploads)
}
func (s *TicketService) replyWithAttachments(ctx context.Context, ticketID, senderID int64, role, content, operationID string, uploads []TicketAttachmentUpload) (*TicketMessage, error) {
	content, err := validateTicketContent(content)
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
	if role == TicketSenderRoleUser && ticket.UserID != senderID {
		return nil, infraerrors.Forbidden("TICKET_FORBIDDEN", "ticket does not belong to current user")
	}
	// Persistent operation replay runs before status checks: an accepted reply
	// remains replayable if an administrator closed the ticket during a retry.
	if s.attachments == nil {
		if len(uploads) > 0 {
			return nil, ticketStorageError()
		}
		if role == TicketSenderRoleUser {
			return s.ReplyAsUser(ctx, ticketID, senderID, content)
		}
		return s.ReplyAsAdmin(ctx, ticketID, senderID, content)
	}
	result, err := s.attachments.submit(ctx, TicketSubmission{TicketID: ticketID, OwnerID: ticket.UserID, SenderID: senderID, Role: role, Content: content, OperationID: operationID}, uploads)
	if err != nil {
		return nil, err
	}
	return result.Message, nil
}

func ticketStorageError() *infraerrors.ApplicationError {
	return infraerrors.ServiceUnavailable("TICKET_STORAGE_UNAVAILABLE", "工单截图存储暂不可用")
}

func ticketSubmissionDigest(input TicketSubmission, uploads []TicketAttachmentUpload) string {
	digests := make([]string, len(uploads))
	for i, upload := range uploads {
		sum := sha256.Sum256(upload.Data)
		digests[i] = hex.EncodeToString(sum[:])
	}
	raw, _ := json.Marshal(struct {
		TicketID, OwnerID, SenderID    int64
		Role, Title, Content, Priority string
		Images                         []string
	}{input.TicketID, input.OwnerID, input.SenderID, input.Role, input.Title, input.Content, input.Priority, digests})
	sum := sha256.Sum256(raw)
	return hex.EncodeToString(sum[:])
}

func (s *TicketAttachmentService) submit(ctx context.Context, input TicketSubmission, uploads []TicketAttachmentUpload) (*TicketSubmissionResult, error) {
	if len(uploads) > TicketMaxAttachments {
		return nil, ErrTicketAttachmentTooMany
	}
	if len(input.OperationID) > 128 || strings.TrimSpace(input.OperationID) != input.OperationID {
		return nil, ErrTicketOperationConflict
	}
	if len(uploads) > 0 && input.OperationID == "" {
		return nil, infraerrors.BadRequest("IDEMPOTENCY_KEY_REQUIRED", "截图提交需要 Idempotency-Key")
	}
	for _, upload := range uploads {
		if len(upload.Data) == 0 {
			return nil, ErrTicketAttachmentInvalid
		}
		if len(upload.Data) > TicketMaxAttachmentBytes {
			return nil, ErrTicketAttachmentTooLarge
		}
	}
	input.Digest = ticketSubmissionDigest(input, uploads)
	if input.OperationID != "" {
		previous, err := s.repo.FindTicketOperation(ctx, input.OperationID, input.Digest)
		if err != nil || previous != nil {
			return previous, err
		}
	}
	if err := s.repo.CheckTicketSubmission(ctx, input); err != nil {
		return nil, err
	}
	if len(uploads) == 0 {
		return s.repo.SubmitTicket(ctx, input)
	}
	select {
	case s.convertSlots <- struct{}{}:
		defer func() { <-s.convertSlots }()
	default:
		return nil, ErrTicketAttachmentBusy
	}
	type preparedImage struct {
		asset            TicketStoredAttachment
		image, thumbnail []byte
	}
	prepared := make([]preparedImage, 0, len(uploads))
	for i, upload := range uploads {
		normal, thumb, w, h, mime, tw, th, tmime, err := prepareTicketImage(upload.Data)
		if err != nil {
			return nil, err
		}
		sum := sha256.Sum256(upload.Data)
		digest := hex.EncodeToString(sum[:])
		idSum := sha256.Sum256([]byte(input.OperationID + ":" + strconv.Itoa(i)))
		id := hex.EncodeToString(idSum[:])
		base := "tickets/v1/" + strconv.FormatInt(input.OwnerID, 10) + "/" + id + "/"
		asset := TicketStoredAttachment{TicketAttachment: TicketAttachment{ID: id, MIME: mime, Bytes: int64(len(normal)), Width: w, Height: h, CacheVersion: digest, ThumbnailWidth: tw, ThumbnailHeight: th}, OwnerID: input.OwnerID, OperationID: input.OperationID, Digest: digest, Ordinal: i, State: "pending", ImageKey: base + "image" + galleryImageExtension(mime), ThumbnailKey: base + "thumbnail" + galleryImageExtension(tmime), ThumbnailMIME: tmime, ThumbnailBytes: int64(len(thumb))}
		prepared = append(prepared, preparedImage{asset, normal, thumb})
	}
	if s.resolver == nil || s.factory == nil {
		return nil, ticketStorageError()
	}
	cfg, release, err := s.resolver(ctx, true)
	if err != nil {
		return nil, err
	}
	if release == nil {
		release = func() {}
	}
	if cfg == nil {
		release()
		return nil, ticketStorageError()
	}
	storage, err := s.factory(ctx, cfg)
	if err != nil {
		release()
		return nil, ticketStorageError().WithCause(err)
	}
	for i := range prepared {
		prepared[i].asset.Endpoint = cfg.Endpoint
		prepared[i].asset.Region = cfg.Region
		prepared[i].asset.Bucket = cfg.Bucket
		input.Attachments = append(input.Attachments, prepared[i].asset)
	}
	err = s.repo.ReserveTicketAttachments(ctx, input.Attachments)
	release() // Durable pending rows now prevent a target change; no DB lock spans S3.
	if err != nil {
		return nil, err
	}
	failed := true
	defer func() {
		if failed {
			cleanupCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 5*time.Second)
			defer cancel()
			if s.repo.MarkTicketOperationAttachmentsDeleting(cleanupCtx, input.OperationID) == nil {
				// Best effort immediate rollback permits a normal retry without waiting
				// for the cleaner. Durable deleting rows survive any failed DELETE.
				s.cleanupOperation(cleanupCtx, storage, input.OperationID)
			}
		}
	}()
	for _, image := range prepared {
		if err = storage.Put(ctx, image.asset.ImageKey, image.asset.MIME, image.image); err != nil {
			return nil, ticketStorageError()
		}
		if err = storage.Put(ctx, image.asset.ThumbnailKey, image.asset.ThumbnailMIME, image.thumbnail); err != nil {
			return nil, ticketStorageError()
		}
	}
	result, err := s.repo.SubmitTicket(ctx, input)
	if err != nil {
		return nil, err
	}
	failed = false
	return result, nil
}

// prepareTicketImage validates actual bytes, rejects animation and bounds decode
// memory before decoding. Re-encoding strips metadata and creates two variants.
func prepareTicketImage(data []byte) (normal, thumbnail []byte, w, h int, mime string, tw, th int, tmime string, err error) {
	cfg, format, decodeErr := image.DecodeConfig(bytes.NewReader(data))
	if decodeErr != nil || (format != "png" && format != "jpeg" && format != "webp") || cfg.Width <= 0 || cfg.Height <= 0 || int64(cfg.Width)*int64(cfg.Height) > ticketMaxImagePixels || ticketImageAnimated(data, format) {
		err = ErrTicketAttachmentInvalid
		return
	}
	decoded, _, decodeErr := image.Decode(bytes.NewReader(data))
	if decodeErr != nil {
		err = ErrTicketAttachmentInvalid
		return
	}
	normal, w, h, mime, err = ticketImageVariant(decoded, 2560, format == "png")
	if err != nil {
		return
	}
	thumbnail, tw, th, tmime, err = galleryVariant(decoded, 480)
	return
}

func ticketImageAnimated(data []byte, format string) bool {
	if format == "png" {
		for pos := 8; pos+12 <= len(data); {
			n := int64(binary.BigEndian.Uint32(data[pos : pos+4]))
			if n > int64(len(data)-pos-12) {
				return true
			}
			if string(data[pos+4:pos+8]) == "acTL" {
				return true
			}
			pos += int(n) + 12
		}
	}
	if format == "webp" {
		for pos := 12; pos+8 <= len(data); {
			n := int64(binary.LittleEndian.Uint32(data[pos+4 : pos+8]))
			if n > int64(len(data)-pos-8) {
				return true
			}
			kind := string(data[pos : pos+4])
			if kind == "ANIM" || kind == "ANMF" || (kind == "VP8X" && n > 0 && data[pos+8]&2 != 0) {
				return true
			}
			pos += 8 + int(n) + (int(n) & 1)
		}
	}
	return false
}

func ticketImageVariant(src image.Image, maxEdge int, preferPNG bool) ([]byte, int, int, string, error) {
	bounds := src.Bounds()
	w, h := bounds.Dx(), bounds.Dy()
	if w > maxEdge || h > maxEdge {
		if w >= h {
			h = max(1, int(int64(h)*int64(maxEdge)/int64(w)))
			w = maxEdge
		} else {
			w = max(1, int(int64(w)*int64(maxEdge)/int64(h)))
			h = maxEdge
		}
	}
	dst := image.NewNRGBA(image.Rect(0, 0, w, h))
	xdraw.ApproxBiLinear.Scale(dst, dst.Bounds(), src, bounds, draw.Src, nil)
	var output bytes.Buffer
	if preferPNG || !dst.Opaque() {
		if err := png.Encode(&output, dst); err != nil {
			return nil, 0, 0, "", err
		}
		if output.Len() <= TicketMaxAttachmentBytes {
			return output.Bytes(), w, h, "image/png", nil
		}
	}
	flat := image.NewRGBA(dst.Bounds())
	draw.Draw(flat, flat.Bounds(), &image.Uniform{C: color.White}, image.Point{}, draw.Src)
	draw.Draw(flat, flat.Bounds(), dst, image.Point{}, draw.Over)
	for _, quality := range []int{90, 80, 70} {
		output.Reset()
		if err := jpeg.Encode(&output, flat, &jpeg.Options{Quality: quality}); err != nil {
			return nil, 0, 0, "", err
		}
		if output.Len() <= TicketMaxAttachmentBytes {
			return output.Bytes(), w, h, "image/jpeg", nil
		}
	}
	return nil, 0, 0, "", ErrTicketAttachmentTooLarge
}

func (s *TicketAttachmentService) attachToMessages(ctx context.Context, ticketID int64, messages []TicketMessage) error {
	assets, err := s.repo.ListTicketAttachments(ctx, ticketID)
	if err != nil {
		return err
	}
	byMessage := make(map[int64][]TicketAttachment)
	for _, asset := range assets {
		byMessage[asset.MessageID] = append(byMessage[asset.MessageID], asset.TicketAttachment)
	}
	for i := range messages {
		messages[i].Attachments = byMessage[messages[i].ID]
	}
	return nil
}

func (s *TicketAttachmentService) GetURL(ctx context.Context, ticketID int64, id string, userID int64, variant string) (*TicketAttachmentURL, error) {
	if variant != "image" && variant != "preview" && variant != "thumbnail" {
		return nil, ErrTicketAttachmentInvalid
	}
	asset, err := s.repo.GetTicketAttachment(ctx, ticketID, id, userID)
	if err != nil {
		return nil, err
	}
	cfg, release, err := s.resolver(ctx, false)
	if err != nil {
		return nil, err
	}
	if release != nil {
		defer release()
	}
	if cfg == nil || asset.Endpoint != cfg.Endpoint || asset.Region != cfg.Region || asset.Bucket != cfg.Bucket {
		return nil, ticketStorageError()
	}
	store, err := s.factory(ctx, cfg)
	if err != nil {
		return nil, ticketStorageError()
	}
	key := asset.ImageKey
	result := &TicketAttachmentURL{Bytes: asset.Bytes, MIME: asset.MIME, Width: asset.Width, Height: asset.Height, CacheVersion: asset.CacheVersion, ExpiresAt: time.Now().Add(ticketAttachmentURLLifetime)}
	if variant == "thumbnail" {
		key = asset.ThumbnailKey
		result.Bytes = asset.ThumbnailBytes
		result.MIME = asset.ThumbnailMIME
		result.Width = asset.ThumbnailWidth
		result.Height = asset.ThumbnailHeight
	}
	result.URL, err = store.PresignGet(ctx, key, ticketAttachmentURLLifetime)
	if err != nil {
		return nil, ticketStorageError()
	}
	return result, nil
}

func (s *TicketAttachmentService) StartCleaner() {
	s.lifecycleMu.Lock()
	defer s.lifecycleMu.Unlock()
	if s.stopped {
		return
	}
	s.startOnce.Do(func() {
		s.cleanupWG.Add(1)
		go func() {
			defer s.cleanupWG.Done()
			s.cleanup()
			ticker := time.NewTicker(time.Minute)
			defer ticker.Stop()
			for {
				select {
				case <-ticker.C:
					s.cleanup()
				case <-s.stop:
					return
				}
			}
		}()
	})
}
func (s *TicketAttachmentService) Stop() {
	s.lifecycleMu.Lock()
	s.stopOnce.Do(func() { s.stopped = true; s.cleanupCancel(); close(s.stop) })
	s.lifecycleMu.Unlock()
	s.cleanupWG.Wait()
}
func (s *TicketAttachmentService) StopCleaner() { s.Stop() }
func (s *TicketAttachmentService) cleanupOperation(ctx context.Context, store GalleryObjectStore, operationID string) {
	release, err := s.repo.LockTicketAttachmentCleanup(ctx)
	if err != nil {
		return
	}
	defer release()
	assets, err := s.repo.ListDeletingTicketOperationAttachments(ctx, operationID)
	if err != nil {
		return
	}
	for _, asset := range assets {
		if err := store.Delete(ctx, asset.ImageKey); err != nil {
			continue
		}
		if err := store.Delete(ctx, asset.ThumbnailKey); err != nil {
			continue
		}
		_ = s.repo.FinalizeTicketAttachmentDelete(ctx, asset.ID)
	}
}

func (s *TicketAttachmentService) cleanup() {
	ctx, cancel := context.WithTimeout(s.cleanupCtx, 45*time.Second)
	defer cancel()
	cfg, release, err := s.resolver(ctx, false)
	if err != nil {
		return
	}
	if release == nil {
		release = func() {}
	}
	if cfg == nil {
		release()
		return
	}
	store, err := s.factory(ctx, cfg)
	if err != nil {
		release()
		return
	}
	// Existing rows protect their target. Release this lock before acquiring the
	// separate cleaner lock, so cleanup also works with a two-connection pool.
	release()
	cleanupRelease, err := s.repo.LockTicketAttachmentCleanup(ctx)
	if err != nil {
		return
	}
	defer cleanupRelease()
	assets, err := s.repo.ClaimTicketAttachmentCleanup(ctx, time.Now().Add(-time.Hour), 30)
	if err != nil {
		return
	}
	for _, asset := range assets {
		if asset.Endpoint != cfg.Endpoint || asset.Region != cfg.Region || asset.Bucket != cfg.Bucket {
			continue
		}
		if err := store.Delete(ctx, asset.ImageKey); err != nil {
			continue
		}
		if err := store.Delete(ctx, asset.ThumbnailKey); err != nil {
			continue
		}
		_ = s.repo.FinalizeTicketAttachmentDelete(ctx, asset.ID)
	}
}
