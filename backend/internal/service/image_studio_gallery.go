package service

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"image"
	_ "image/gif"
	"image/jpeg"
	"image/png"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/config"
	"golang.org/x/image/draw"
	_ "golang.org/x/image/webp"
)

const (
	GalleryMaxImageBytes = 32 << 20
	galleryMaxPixels     = 20_000_000
	galleryPrefix        = "studio/v1/"
	galleryURLLifetime   = 10 * time.Minute
)

var (
	ErrGalleryDisabled    = errors.New("gallery storage is disabled")
	ErrGalleryUnavailable = errors.New("gallery storage is unavailable")
	ErrGalleryNotFound    = errors.New("gallery item not found")
	ErrGalleryGone        = errors.New("gallery task was deleted")
	ErrGalleryConflict    = errors.New("gallery item already exists with different content")
	ErrGalleryInvalid     = errors.New("invalid gallery item")
	ErrGalleryBusy        = errors.New("gallery image processing is busy")
	ErrGalleryQuota       = errors.New("gallery storage quota exceeded")
)

type GalleryAsset struct {
	UserID                                int64
	ImageID, SHA256, MIME                 string
	Bytes                                 int64
	Width, Height                         int
	PreviewBytes                          int64
	PreviewWidth, PreviewHeight           int
	ThumbnailBytes                        int64
	ThumbnailWidth, ThumbnailHeight       int
	Endpoint, Region, Bucket              string
	OriginalKey, PreviewKey, ThumbnailKey string
	State                                 string
	AcceptedJobTaskID                     string // admission reservation owner; never stored in asset rows
}

type GalleryTaskLink struct {
	ImageID, Role string
	Ordinal       int
}

type GalleryTaskPage struct {
	Items      []json.RawMessage `json:"items"`
	NextCursor *string           `json:"next_cursor"`
}

// GalleryStats is derived from the current user's committed gallery records,
// never from a partially loaded browser page or local sync queue.
type GalleryStats struct {
	Tasks        int64 `json:"tasks"`
	DoneTasks    int64 `json:"done_tasks"`
	FailedTasks  int64 `json:"failed_tasks"`
	OutputImages int64 `json:"output_images"`
	Assets       int64 `json:"assets"`
	UsedBytes    int64 `json:"used_bytes"`
	MaxTasks     int64 `json:"max_tasks"`
	MaxAssets    int64 `json:"max_assets"`
	MaxBytes     int64 `json:"max_bytes"`
}

type GalleryRepository interface {
	Stats(ctx context.Context, userID int64) (GalleryStats, error)
	ReserveAsset(ctx context.Context, asset GalleryAsset) (*GalleryAsset, error)
	MarkAssetReady(ctx context.Context, userID int64, imageID string) error
	GetAsset(ctx context.Context, userID int64, imageID string) (*GalleryAsset, error)
	SaveTask(ctx context.Context, userID int64, taskID, digest string, task json.RawMessage, links []GalleryTaskLink) (json.RawMessage, error)
	SaveAcceptedTask(ctx context.Context, userID int64, taskID, digest string, task json.RawMessage, links []GalleryTaskLink) (json.RawMessage, error)
	GetTask(ctx context.Context, userID int64, taskID string) (json.RawMessage, error)
	ListTasks(ctx context.Context, userID int64, cursor string, limit int) (GalleryTaskPage, error)
	DeleteTask(ctx context.Context, userID int64, taskID string) ([]GalleryAsset, error)
	ListCleanupAssets(ctx context.Context, limit int) ([]GalleryAsset, error)
	FinalizeAssetDelete(ctx context.Context, userID int64, imageID string) error
	HasAssets(ctx context.Context) (bool, error)
	LockTarget(ctx context.Context) (func(), error)
	LockCleanup(ctx context.Context, userID int64) (func(), error)
	MarkDeletedUsersForCleanup(ctx context.Context, limit int) error
}

type GalleryObjectStore interface {
	Put(ctx context.Context, key, contentType string, data []byte) error
	Delete(ctx context.Context, key string) error
	PresignGet(ctx context.Context, key string, expiry time.Duration) (string, error)
}

// GalleryObjectReader is implemented only by private server-side stores. It
// avoids fetching our own presigned URLs over HTTP while executing Studio jobs.
type GalleryObjectReader interface {
	Get(ctx context.Context, key string, maxBytes int64) ([]byte, error)
}

type GalleryObjectStoreFactory func(ctx context.Context, cfg *config.ImageStorageConfig) (GalleryObjectStore, error)

type ImageStudioGalleryService struct {
	repo         GalleryRepository
	settings     *ImageStorageSettingService
	factory      GalleryObjectStoreFactory
	mu           sync.Mutex
	storage      GalleryObjectStore
	storageHash  string
	stop         chan struct{}
	stopOnce     sync.Once
	convertSlots chan struct{}
}

func NewImageStudioGalleryService(repo GalleryRepository, settings *ImageStorageSettingService, factory GalleryObjectStoreFactory) *ImageStudioGalleryService {
	return &ImageStudioGalleryService{repo: repo, settings: settings, factory: factory, stop: make(chan struct{}), convertSlots: make(chan struct{}, 2)}
}

func ProvideImageStudioGalleryService(repo GalleryRepository, settings *ImageStorageSettingService, factory GalleryObjectStoreFactory) *ImageStudioGalleryService {
	s := NewImageStudioGalleryService(repo, settings, factory)
	s.StartCleaner()
	return s
}

// AcquireConversion is called before reading an upload body so concurrent 4K
// decodes cannot occupy unbounded gateway memory or CPU.
func (s *ImageStudioGalleryService) AcquireConversion() (func(), error) {
	if s == nil {
		return nil, ErrGalleryUnavailable
	}
	select {
	case s.convertSlots <- struct{}{}:
		return func() { <-s.convertSlots }, nil
	default:
		return nil, ErrGalleryBusy
	}
}

func (s *ImageStudioGalleryService) Status(ctx context.Context) (bool, error) {
	if s == nil || s.settings == nil {
		return false, nil
	}
	_, enabled, err := s.settings.StudioGalleryConfig(ctx)
	return enabled, err
}

func (s *ImageStudioGalleryService) Stats(ctx context.Context, userID int64) (GalleryStats, error) {
	if s == nil || s.repo == nil || userID <= 0 {
		return GalleryStats{}, ErrGalleryUnavailable
	}
	return s.repo.Stats(ctx, userID)
}

func (s *ImageStudioGalleryService) storeFor(ctx context.Context, write bool) (GalleryObjectStore, *config.ImageStorageConfig, error) {
	if s == nil || s.settings == nil || s.factory == nil {
		return nil, nil, ErrGalleryUnavailable
	}
	cfg, enabled, err := s.settings.StudioGalleryConfig(ctx)
	if err != nil {
		return nil, nil, fmt.Errorf("load gallery storage: %w", err)
	}
	if write && !enabled {
		return nil, nil, ErrGalleryDisabled
	}
	if cfg == nil || !cfg.IsConfigured() {
		return nil, nil, ErrGalleryUnavailable
	}
	fingerprint := sha256.Sum256([]byte(strings.Join([]string{cfg.Endpoint, cfg.Region, cfg.Bucket, cfg.AccessKeyID, cfg.SecretAccessKey, strconv.FormatBool(cfg.ForcePathStyle)}, "\x00")))
	hash := hex.EncodeToString(fingerprint[:])
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.storage == nil || s.storageHash != hash {
		storage, err := s.factory(ctx, cfg)
		if err != nil {
			return nil, nil, fmt.Errorf("%w: build S3 client: %v", ErrGalleryUnavailable, err)
		}
		s.storage, s.storageHash = storage, hash
	}
	return s.storage, cfg, nil
}

func galleryIDValid(id string) bool {
	if len(id) == 0 || len(id) > 128 {
		return false
	}
	for _, ch := range id {
		if ch >= 'a' && ch <= 'z' || ch >= 'A' && ch <= 'Z' || ch >= '0' && ch <= '9' || ch == '-' || ch == '_' {
			continue
		}
		return false
	}
	return true
}

func galleryImageMIME(format string) string {
	switch format {
	case "png":
		return "image/png"
	case "jpeg":
		return "image/jpeg"
	case "webp":
		return "image/webp"
	case "gif":
		return "image/gif"
	default:
		return ""
	}
}

func galleryImageExtension(mime string) string {
	switch mime {
	case "image/jpeg":
		return ".jpg"
	case "image/webp":
		return ".webp"
	case "image/gif":
		return ".gif"
	default:
		return ".png"
	}
}

func galleryStoredVariantMIME(key string) string {
	if strings.HasSuffix(key, ".jpg") {
		return "image/jpeg"
	}
	return "image/png"
}

func galleryVariant(src image.Image, maxEdge int) ([]byte, int, int, string, error) {
	bounds := src.Bounds()
	width, height := bounds.Dx(), bounds.Dy()
	if width > maxEdge || height > maxEdge {
		if width >= height {
			height = max(1, int(int64(height)*int64(maxEdge)/int64(width)))
			width = maxEdge
		} else {
			width = max(1, int(int64(width)*int64(maxEdge)/int64(height)))
			height = maxEdge
		}
	}
	dst := image.NewNRGBA(image.Rect(0, 0, width, height))
	draw.ApproxBiLinear.Scale(dst, dst.Bounds(), src, bounds, draw.Over, nil)
	var out bytes.Buffer
	if dst.Opaque() {
		if err := jpeg.Encode(&out, dst, &jpeg.Options{Quality: 85}); err != nil {
			return nil, 0, 0, "", err
		}
		return out.Bytes(), width, height, "image/jpeg", nil
	}
	if err := png.Encode(&out, dst); err != nil {
		return nil, 0, 0, "", err
	}
	return out.Bytes(), width, height, "image/png", nil
}

func (s *ImageStudioGalleryService) PutAsset(ctx context.Context, userID int64, imageID, contentType string, data []byte) error {
	return s.putAsset(ctx, userID, "", imageID, contentType, data, true)
}

// PutAcceptedJobAsset is internal to an already-admitted Studio job. Disabling
// new cloud gallery writes must not strand a paid image already in flight.
func (s *ImageStudioGalleryService) PutAcceptedJobAsset(ctx context.Context, userID int64, taskID, imageID, contentType string, data []byte) error {
	return s.putAsset(ctx, userID, taskID, imageID, contentType, data, false)
}

func (s *ImageStudioGalleryService) putAsset(ctx context.Context, userID int64, acceptedTaskID, imageID, contentType string, data []byte, requireEnabled bool) error {
	if userID <= 0 || !galleryIDValid(imageID) || len(data) == 0 || len(data) > GalleryMaxImageBytes {
		return ErrGalleryInvalid
	}
	digest := sha256.Sum256(data)
	hexDigest := hex.EncodeToString(digest[:])
	previous, err := s.repo.GetAsset(ctx, userID, imageID)
	if err == nil {
		if previous.SHA256 != hexDigest {
			return ErrGalleryConflict
		}
		if previous.State == "ready" {
			release, err := s.repo.LockTarget(ctx)
			if err != nil {
				return err
			}
			defer release()
			_, cfg, err := s.storeFor(ctx, requireEnabled)
			if err != nil {
				return err
			}
			if previous.Endpoint != cfg.Endpoint || previous.Region != cfg.Region || previous.Bucket != cfg.Bucket {
				return ErrGalleryUnavailable
			}
			fresh, err := s.repo.GetAsset(ctx, userID, imageID)
			if err != nil {
				return err
			}
			if fresh.SHA256 != hexDigest || fresh.State != "ready" {
				return ErrGalleryConflict
			}
			return nil
		}
		if previous.State != "uploading" {
			return ErrGalleryConflict
		}
	} else if !errors.Is(err, ErrGalleryNotFound) {
		return err
	}
	decodedCfg, format, err := image.DecodeConfig(bytes.NewReader(data))
	mime := galleryImageMIME(format)
	if err != nil || mime == "" || decodedCfg.Width <= 0 || decodedCfg.Height <= 0 || int64(decodedCfg.Width)*int64(decodedCfg.Height) > galleryMaxPixels {
		return ErrGalleryInvalid
	}
	if declared := strings.TrimSpace(strings.Split(contentType, ";")[0]); declared != "" && declared != mime {
		return ErrGalleryInvalid
	}
	decoded, _, err := image.Decode(bytes.NewReader(data))
	if err != nil {
		return ErrGalleryInvalid
	}
	preview, previewWidth, previewHeight, previewMIME, err := galleryVariant(decoded, 1600)
	if err != nil {
		return err
	}
	thumbnail, thumbnailWidth, thumbnailHeight, thumbnailMIME, err := galleryVariant(decoded, 480)
	if err != nil {
		return err
	}
	// Conversion is complete before taking the target lock; only target lookup
	// and durable reservation are serialized with admin S3 target changes.
	releaseTarget, err := s.repo.LockTarget(ctx)
	if err != nil {
		return err
	}
	defer func() {
		if releaseTarget != nil {
			releaseTarget()
		}
	}()
	storage, cfg, err := s.storeFor(ctx, requireEnabled)
	if err != nil {
		return err
	}
	previous, err = s.repo.GetAsset(ctx, userID, imageID)
	if err == nil {
		if previous.SHA256 != hexDigest {
			return ErrGalleryConflict
		}
		if previous.State == "ready" {
			return nil
		}
		if previous.State != "uploading" {
			return ErrGalleryConflict
		}
		if previous.Endpoint != cfg.Endpoint || previous.Region != cfg.Region || previous.Bucket != cfg.Bucket {
			return ErrGalleryUnavailable
		}
	} else if !errors.Is(err, ErrGalleryNotFound) {
		return err
	}
	base := galleryPrefix + strconv.FormatInt(userID, 10) + "/" + imageID + "/" + hexDigest + "/"
	asset := GalleryAsset{
		UserID: userID, ImageID: imageID, SHA256: hexDigest, MIME: mime, Bytes: int64(len(data)),
		Width: decodedCfg.Width, Height: decodedCfg.Height,
		PreviewBytes: int64(len(preview)), PreviewWidth: previewWidth, PreviewHeight: previewHeight,
		ThumbnailBytes: int64(len(thumbnail)), ThumbnailWidth: thumbnailWidth, ThumbnailHeight: thumbnailHeight,
		Endpoint: cfg.Endpoint, Region: cfg.Region, Bucket: cfg.Bucket,
		AcceptedJobTaskID: acceptedTaskID,
		OriginalKey:       base + "original" + galleryImageExtension(mime),
		PreviewKey:        base + "preview" + galleryImageExtension(previewMIME),
		ThumbnailKey:      base + "thumbnail" + galleryImageExtension(thumbnailMIME), State: "uploading",
	}
	reserved, err := s.repo.ReserveAsset(ctx, asset)
	if err != nil {
		return err
	}
	if reserved.SHA256 != hexDigest {
		return ErrGalleryConflict
	}
	if reserved.State == "ready" {
		return nil
	}
	if reserved.State != "uploading" {
		return ErrGalleryConflict
	}
	if reserved.Endpoint != cfg.Endpoint || reserved.Region != cfg.Region || reserved.Bucket != cfg.Bucket {
		return ErrGalleryUnavailable
	}
	releaseTarget()
	releaseTarget = nil
	for _, object := range []struct {
		key, mime string
		data      []byte
	}{
		{asset.OriginalKey, mime, data}, {asset.PreviewKey, previewMIME, preview}, {asset.ThumbnailKey, thumbnailMIME, thumbnail},
	} {
		if err := storage.Put(ctx, object.key, object.mime, object.data); err != nil {
			return fmt.Errorf("%w: store image: %v", ErrGalleryUnavailable, err)
		}
	}
	return s.repo.MarkAssetReady(ctx, userID, imageID)
}

type galleryTaskInput struct {
	ID                        string   `json:"id"`
	Status                    string   `json:"status"`
	InputImageIDs             []string `json:"inputImageIds"`
	OutputImages              []string `json:"outputImages"`
	TransparentOriginalImages []string `json:"transparentOriginalImages"`
	MaskImageID               *string  `json:"maskImageId"`
	MaskTargetImageID         *string  `json:"maskTargetImageId"`
}

func (s *ImageStudioGalleryService) SaveTask(ctx context.Context, userID int64, raw json.RawMessage) (json.RawMessage, error) {
	return s.saveTask(ctx, userID, raw, true)
}

func (s *ImageStudioGalleryService) SaveAcceptedJobTask(ctx context.Context, userID int64, raw json.RawMessage) (json.RawMessage, error) {
	return s.saveTask(ctx, userID, raw, false)
}

func (s *ImageStudioGalleryService) saveTask(ctx context.Context, userID int64, raw json.RawMessage, requireEnabled bool) (json.RawMessage, error) {
	if userID <= 0 || len(raw) == 0 || len(raw) > 256<<10 {
		return nil, ErrGalleryInvalid
	}
	if _, _, err := s.storeFor(ctx, requireEnabled); err != nil {
		return nil, err
	}
	var input galleryTaskInput
	if err := json.Unmarshal(raw, &input); err != nil || !galleryIDValid(input.ID) || input.Status != "done" && input.Status != "error" {
		return nil, ErrGalleryInvalid
	}
	if input.Status == "done" && len(input.OutputImages) == 0 {
		return nil, ErrGalleryInvalid
	}
	var normalized any
	if err := json.Unmarshal(raw, &normalized); err != nil {
		return nil, ErrGalleryInvalid
	}
	canonical, err := json.Marshal(normalized)
	if err != nil {
		return nil, ErrGalleryInvalid
	}
	digest := sha256.Sum256(canonical)
	links := make([]GalleryTaskLink, 0)
	appendLinks := func(role string, ids []string) error {
		for ordinal, id := range ids {
			if !galleryIDValid(id) {
				return ErrGalleryInvalid
			}
			links = append(links, GalleryTaskLink{ImageID: id, Role: role, Ordinal: ordinal})
		}
		return nil
	}
	for _, group := range []struct {
		role string
		ids  []string
	}{
		{"input", input.InputImageIDs}, {"output", input.OutputImages}, {"transparent_original", input.TransparentOriginalImages},
	} {
		if err := appendLinks(group.role, group.ids); err != nil {
			return nil, err
		}
	}
	if input.MaskImageID != nil && *input.MaskImageID != "" {
		if err := appendLinks("mask", []string{*input.MaskImageID}); err != nil {
			return nil, err
		}
	}
	if input.MaskTargetImageID != nil && *input.MaskTargetImageID != "" {
		if err := appendLinks("mask_target", []string{*input.MaskTargetImageID}); err != nil {
			return nil, err
		}
	}
	if len(links) > 64 {
		return nil, ErrGalleryInvalid
	}
	if requireEnabled {
		return s.repo.SaveTask(ctx, userID, input.ID, hex.EncodeToString(digest[:]), canonical, links)
	}
	return s.repo.SaveAcceptedTask(ctx, userID, input.ID, hex.EncodeToString(digest[:]), canonical, links)
}

func (s *ImageStudioGalleryService) GetTask(ctx context.Context, userID int64, id string) (json.RawMessage, error) {
	if !galleryIDValid(id) {
		return nil, ErrGalleryInvalid
	}
	return s.repo.GetTask(ctx, userID, id)
}

func (s *ImageStudioGalleryService) ListTasks(ctx context.Context, userID int64, cursor string, limit int) (GalleryTaskPage, error) {
	return s.repo.ListTasks(ctx, userID, cursor, limit)
}

type GalleryAssetURL struct {
	URL       string `json:"url"`
	ExpiresAt string `json:"expires_at"`
	Bytes     int64  `json:"bytes"`
	MIME      string `json:"mime"`
	Width     int    `json:"width"`
	Height    int    `json:"height"`
}

func (s *ImageStudioGalleryService) AssetURL(ctx context.Context, userID int64, id, variant string) (GalleryAssetURL, error) {
	if !galleryIDValid(id) {
		return GalleryAssetURL{}, ErrGalleryInvalid
	}
	asset, err := s.repo.GetAsset(ctx, userID, id)
	if err != nil {
		return GalleryAssetURL{}, err
	}
	if asset.State != "ready" {
		return GalleryAssetURL{}, ErrGalleryNotFound
	}
	storage, cfg, err := s.storeFor(ctx, false)
	if err != nil {
		return GalleryAssetURL{}, err
	}
	if cfg.Endpoint != asset.Endpoint || cfg.Region != asset.Region || cfg.Bucket != asset.Bucket {
		return GalleryAssetURL{}, ErrGalleryUnavailable
	}
	result := GalleryAssetURL{ExpiresAt: time.Now().UTC().Add(galleryURLLifetime).Format(time.RFC3339)}
	key := ""
	switch variant {
	case "original":
		key, result.MIME, result.Bytes, result.Width, result.Height = asset.OriginalKey, asset.MIME, asset.Bytes, asset.Width, asset.Height
	case "preview":
		key, result.MIME, result.Bytes, result.Width, result.Height = asset.PreviewKey, galleryStoredVariantMIME(asset.PreviewKey), asset.PreviewBytes, asset.PreviewWidth, asset.PreviewHeight
	case "thumbnail":
		key, result.MIME, result.Bytes, result.Width, result.Height = asset.ThumbnailKey, galleryStoredVariantMIME(asset.ThumbnailKey), asset.ThumbnailBytes, asset.ThumbnailWidth, asset.ThumbnailHeight
	default:
		return GalleryAssetURL{}, ErrGalleryInvalid
	}
	result.URL, err = storage.PresignGet(ctx, key, galleryURLLifetime)
	if err != nil {
		return GalleryAssetURL{}, fmt.Errorf("%w: sign image: %v", ErrGalleryUnavailable, err)
	}
	return result, nil
}

// ReadAsset checks ownership in PostgreSQL and reads the private original
// directly through the S3 SDK. It never follows an external URL or returns a
// presigned link to the worker.
func (s *ImageStudioGalleryService) ReadAsset(ctx context.Context, userID int64, id string) ([]byte, string, error) {
	if s == nil || s.repo == nil || !galleryIDValid(id) || userID <= 0 {
		return nil, "", ErrGalleryInvalid
	}
	asset, err := s.repo.GetAsset(ctx, userID, id)
	if err != nil {
		return nil, "", err
	}
	if asset.State != "ready" || asset.Bytes <= 0 || asset.Bytes > GalleryMaxImageBytes {
		return nil, "", ErrGalleryNotFound
	}
	storage, cfg, err := s.storeFor(ctx, false)
	if err != nil {
		return nil, "", err
	}
	if cfg.Endpoint != asset.Endpoint || cfg.Region != asset.Region || cfg.Bucket != asset.Bucket {
		return nil, "", ErrGalleryUnavailable
	}
	reader, ok := storage.(GalleryObjectReader)
	if !ok {
		return nil, "", ErrGalleryUnavailable
	}
	data, err := reader.Get(ctx, asset.OriginalKey, GalleryMaxImageBytes)
	if err != nil || int64(len(data)) != asset.Bytes {
		return nil, "", ErrGalleryUnavailable
	}
	return data, asset.MIME, nil
}

func (s *ImageStudioGalleryService) DeleteTask(ctx context.Context, userID int64, id string) error {
	if !galleryIDValid(id) {
		return ErrGalleryInvalid
	}
	assets, err := s.repo.DeleteTask(ctx, userID, id)
	if err != nil {
		return err
	}
	for _, asset := range assets {
		s.cleanupAsset(ctx, asset)
	}
	return nil
}

func (s *ImageStudioGalleryService) cleanupAsset(ctx context.Context, asset GalleryAsset) {
	release, err := s.repo.LockCleanup(ctx, asset.UserID)
	if err != nil {
		return
	}
	defer release()
	current, err := s.repo.GetAsset(ctx, asset.UserID, asset.ImageID)
	if err != nil || current.State != "deleting" {
		return
	}
	asset = *current
	storage, cfg, err := s.storeFor(ctx, false)
	if err != nil || cfg.Endpoint != asset.Endpoint || cfg.Region != asset.Region || cfg.Bucket != asset.Bucket {
		return
	}
	for _, key := range []string{asset.OriginalKey, asset.PreviewKey, asset.ThumbnailKey} {
		if err := storage.Delete(ctx, key); err != nil {
			return
		}
	}
	_ = s.repo.FinalizeAssetDelete(ctx, asset.UserID, asset.ImageID)
}

func (s *ImageStudioGalleryService) CleanupOnce(ctx context.Context) {
	if err := s.repo.MarkDeletedUsersForCleanup(ctx, 20); err != nil {
		return
	}
	assets, err := s.repo.ListCleanupAssets(ctx, 100)
	if err != nil {
		return
	}
	for _, asset := range assets {
		s.cleanupAsset(ctx, asset)
	}
}

func (s *ImageStudioGalleryService) StartCleaner() {
	if s == nil {
		return
	}
	go func() {
		ticker := time.NewTicker(15 * time.Minute)
		defer ticker.Stop()
		for {
			select {
			case <-s.stop:
				return
			case <-ticker.C:
				ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
				s.CleanupOnce(ctx)
				cancel()
			}
		}
	}()
}

func (s *ImageStudioGalleryService) StopCleaner() {
	if s != nil {
		s.stopOnce.Do(func() { close(s.stop) })
	}
}
