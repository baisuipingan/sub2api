//go:build unit

package service

import (
	"bytes"
	"context"
	"encoding/binary"
	"encoding/json"
	"errors"
	"hash/crc32"
	"image"
	"image/color"
	"image/png"
	"net/http"
	"net/http/httptest"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/config"
	"github.com/stretchr/testify/require"
)

type galleryTestRepo struct {
	assets         map[string]GalleryAsset
	reserveCalls   int
	hasAssets      bool
	mu             sync.Mutex
	reserveEntered chan struct{}
	reserveRelease chan struct{}
	pending        []GalleryAsset
	finalized      int
}

func (r *galleryTestRepo) Stats(context.Context, int64) (GalleryStats, error) {
	return GalleryStats{MaxTasks: 1000, MaxAssets: 500, MaxBytes: 1 << 30}, nil
}

func (r *galleryTestRepo) ReserveAsset(_ context.Context, a GalleryAsset) (*GalleryAsset, error) {
	if r.reserveEntered != nil {
		r.reserveEntered <- struct{}{}
		<-r.reserveRelease
	}
	r.reserveCalls++
	if old, ok := r.assets[a.ImageID]; ok {
		return &old, nil
	}
	r.assets[a.ImageID] = a
	r.hasAssets = true
	return &a, nil
}
func (r *galleryTestRepo) MarkAssetReady(_ context.Context, _ int64, id string) error {
	a := r.assets[id]
	a.State = "ready"
	r.assets[id] = a
	return nil
}
func (r *galleryTestRepo) GetAsset(_ context.Context, _ int64, id string) (*GalleryAsset, error) {
	a, ok := r.assets[id]
	if !ok {
		return nil, ErrGalleryNotFound
	}
	return &a, nil
}
func (r *galleryTestRepo) SaveTask(_ context.Context, _ int64, _ string, _ string, task json.RawMessage, _ []GalleryTaskLink) (json.RawMessage, error) {
	return task, nil
}
func (r *galleryTestRepo) SaveAcceptedTask(ctx context.Context, userID int64, taskID, digest string, task json.RawMessage, links []GalleryTaskLink) (json.RawMessage, error) {
	return r.SaveTask(ctx, userID, taskID, digest, task, links)
}
func (r *galleryTestRepo) GetTask(context.Context, int64, string) (json.RawMessage, error) {
	return nil, ErrGalleryNotFound
}
func (r *galleryTestRepo) ListTasks(context.Context, int64, string, int) (GalleryTaskPage, error) {
	return GalleryTaskPage{}, nil
}
func (r *galleryTestRepo) DeleteTask(context.Context, int64, string) ([]GalleryAsset, error) {
	return r.pending, nil
}
func (r *galleryTestRepo) ListCleanupAssets(context.Context, int) ([]GalleryAsset, error) {
	return r.pending, nil
}
func (r *galleryTestRepo) FinalizeAssetDelete(context.Context, int64, string) error {
	r.finalized++
	r.pending = nil
	return nil
}
func (r *galleryTestRepo) HasAssets(context.Context) (bool, error) { return r.hasAssets, nil }
func (r *galleryTestRepo) LockTarget(context.Context) (func(), error) {
	r.mu.Lock()
	return r.mu.Unlock, nil
}
func (r *galleryTestRepo) MarkDeletedUsersForCleanup(context.Context, int) error { return nil }
func (r *galleryTestRepo) LockCleanup(context.Context, int64) (func(), error)    { return func() {}, nil }

type galleryTestStore struct {
	objects   map[string][]byte
	puts      int
	putErr    error
	deleteErr error
}

func (s *galleryTestStore) Put(_ context.Context, key, _ string, data []byte) error {
	s.puts++
	if s.putErr != nil {
		return s.putErr
	}
	s.objects[key] = append([]byte(nil), data...)
	return nil
}
func (s *galleryTestStore) Delete(_ context.Context, key string) error {
	if s.deleteErr != nil {
		return s.deleteErr
	}
	delete(s.objects, key)
	return nil
}
func (s *galleryTestStore) PresignGet(_ context.Context, key string, _ time.Duration) (string, error) {
	return "https://gallery.test/" + key, nil
}

func galleryPNG(t *testing.T, w, h int) []byte {
	t.Helper()
	img := image.NewNRGBA(image.Rect(0, 0, w, h))
	img.SetNRGBA(0, 0, color.NRGBA{R: 255, A: 255})
	var out bytes.Buffer
	require.NoError(t, png.Encode(&out, img))
	return out.Bytes()
}

func galleryPNGHeader(w, h uint32) []byte {
	data := make([]byte, 33)
	copy(data, []byte{137, 80, 78, 71, 13, 10, 26, 10})
	binary.BigEndian.PutUint32(data[8:12], 13)
	copy(data[12:16], "IHDR")
	binary.BigEndian.PutUint32(data[16:20], w)
	binary.BigEndian.PutUint32(data[20:24], h)
	data[24], data[25] = 8, 6
	binary.BigEndian.PutUint32(data[29:33], crc32.ChecksumIEEE(data[12:29]))
	return data
}

func galleryTestService(t *testing.T) (*ImageStudioGalleryService, *galleryTestRepo, *galleryTestStore, *ImageStorageSettingService) {
	t.Helper()
	settings, _, _ := newImageStorageFixture(t, config.ImageStorageConfig{})
	_, err := settings.Update(context.Background(), ImageStorageSettings{Enabled: false, StudioGalleryEnabled: true, Bucket: "gallery", Endpoint: "https://s3.test", Region: "auto", AccessKeyID: "ak", SecretAccessKey: "secret"})
	require.NoError(t, err)
	repo := &galleryTestRepo{assets: map[string]GalleryAsset{}}
	store := &galleryTestStore{objects: map[string][]byte{}}
	svc := NewImageStudioGalleryService(repo, settings, func(context.Context, *config.ImageStorageConfig) (GalleryObjectStore, error) { return store, nil })
	return svc, repo, store, settings
}

func TestStudioArchiveUsesDurableURLAfterObjectStoreRecovery(t *testing.T) {
	gallery, assets, objects, _ := galleryTestService(t)
	data := galleryPNG(t, 16, 12)
	var downloads atomic.Int32
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		downloads.Add(1)
		w.Header().Set("Content-Type", "image/png")
		_, _ = w.Write(data)
	}))
	defer upstream.Close()
	response, err := json.Marshal(map[string]any{"data": []any{map[string]string{"url": upstream.URL + "/expires.png"}}})
	require.NoError(t, err)
	claim := StudioJobClaim{StudioJob: StudioJob{UserID: 17, TaskID: "paid-url", Spec: StudioJobSpec{Protocol: "openai", Postprocess: "none"}}}
	s := &StudioJobService{spoolDir: t.TempDir(), gallery: gallery}
	require.NoError(t, s.cacheStudioURLImages(context.Background(), claim, response, NewImageResultUploader(nil, "", GalleryMaxImageBytes, upstream.Client())))
	objects.putErr = errors.New("R2 temporarily unavailable")
	_, _, err = s.archiveStudioImages(context.Background(), claim, response)
	require.ErrorIs(t, err, ErrGalleryUnavailable)
	upstream.Close()
	objects.putErr = nil
	outputs, _, err := s.archiveStudioImages(context.Background(), claim, response)
	require.NoError(t, err)
	require.Equal(t, []string{studioOutputID(claim, 0, "out")}, outputs)
	require.Equal(t, data, objects.objects[assets.assets[outputs[0]].OriginalKey])
	require.Equal(t, int32(1), downloads.Load(), "R2 retries must never need the expired upstream link")
}

func TestImageStudioGalleryAssetVariantsAndIdempotency(t *testing.T) {
	svc, repo, store, _ := galleryTestService(t)
	ctx := context.Background()
	enabled, err := svc.Status(ctx)
	require.NoError(t, err)
	require.True(t, enabled, "gallery switch must not depend on async image enabled")
	imageBytes := galleryPNG(t, 800, 600)
	require.NoError(t, svc.PutAsset(ctx, 17, "image-one", "image/png", imageBytes))
	require.Equal(t, 3, store.puts)
	asset := repo.assets["image-one"]
	require.Equal(t, "studio/v1/17/image-one/", asset.OriginalKey[:len("studio/v1/17/image-one/")])
	require.Equal(t, 480, asset.ThumbnailWidth)
	require.Equal(t, 360, asset.ThumbnailHeight)
	require.Equal(t, 800, asset.PreviewWidth)
	require.Equal(t, 600, asset.PreviewHeight)
	require.Equal(t, int64(len(imageBytes)), asset.Bytes)
	require.Equal(t, 3, len(store.objects))
	for _, variant := range []string{"original", "preview", "thumbnail"} {
		url, err := svc.AssetURL(ctx, 17, "image-one", variant)
		require.NoError(t, err)
		require.NotEmpty(t, url.URL)
		require.Positive(t, url.Bytes)
	}
	require.NoError(t, svc.PutAsset(ctx, 17, "image-one", "image/png", imageBytes))
	require.Equal(t, 1, repo.reserveCalls, "ready asset retry must skip image decoding and upload")
	require.Equal(t, 3, store.puts)
	require.ErrorIs(t, svc.PutAsset(ctx, 17, "image-one", "image/png", galleryPNG(t, 10, 10)), ErrGalleryConflict)
}

func TestImageStudioGalleryOpaquePreviewUsesJPEGAndTransparentPreviewKeepsPNG(t *testing.T) {
	svc, repo, store, _ := galleryTestService(t)
	ctx := context.Background()
	opaque := image.NewNRGBA(image.Rect(0, 0, 16, 16))
	for y := 0; y < 16; y++ {
		for x := 0; x < 16; x++ {
			opaque.SetNRGBA(x, y, color.NRGBA{R: uint8(x * 12), G: uint8(y * 12), B: 80, A: 255})
		}
	}
	var source bytes.Buffer
	require.NoError(t, png.Encode(&source, opaque))
	require.NoError(t, svc.PutAsset(ctx, 17, "opaque-image", "image/png", source.Bytes()))
	asset := repo.assets["opaque-image"]
	require.Equal(t, "image/jpeg", galleryStoredVariantMIME(asset.PreviewKey))
	require.True(t, bytes.HasPrefix(store.objects[asset.PreviewKey], []byte{0xff, 0xd8}))
	url, err := svc.AssetURL(ctx, 17, "opaque-image", "preview")
	require.NoError(t, err)
	require.Equal(t, "image/jpeg", url.MIME)

	require.NoError(t, svc.PutAsset(ctx, 17, "transparent-image", "image/png", galleryPNG(t, 16, 16)))
	transparent := repo.assets["transparent-image"]
	require.Equal(t, "image/png", galleryStoredVariantMIME(transparent.PreviewKey))
	require.True(t, bytes.HasPrefix(store.objects[transparent.PreviewKey], []byte{137, 80, 78, 71}))
}

func TestImageStudioGalleryRejectsTooManyPixelsBeforeDecode(t *testing.T) {
	svc, repo, store, _ := galleryTestService(t)
	require.GreaterOrEqual(t, galleryMaxPixels, 4096*4096, "4K square must remain allowed")
	err := svc.PutAsset(context.Background(), 17, "large", "image/png", galleryPNGHeader(5000, 5000))
	require.ErrorIs(t, err, ErrGalleryInvalid)
	require.Zero(t, repo.reserveCalls)
	require.Zero(t, store.puts)
}

func TestImageStudioGalleryConversionSlots(t *testing.T) {
	svc, _, _, _ := galleryTestService(t)
	first, err := svc.AcquireConversion()
	require.NoError(t, err)
	second, err := svc.AcquireConversion()
	require.NoError(t, err)
	_, err = svc.AcquireConversion()
	require.True(t, errors.Is(err, ErrGalleryBusy))
	first()
	second()
	release, err := svc.AcquireConversion()
	require.NoError(t, err)
	release()
}

func TestImageStudioGalleryTargetChangeWaitsForFirstReservation(t *testing.T) {
	svc, repo, _, settings := galleryTestService(t)
	settings.SetGalleryRepository(repo)
	repo.reserveEntered = make(chan struct{}, 1)
	repo.reserveRelease = make(chan struct{})
	ctx := context.Background()
	uploadDone := make(chan error, 1)
	imageBytes := galleryPNG(t, 10, 10)
	go func() { uploadDone <- svc.PutAsset(ctx, 17, "first-image", "image/png", imageBytes) }()
	select {
	case <-repo.reserveEntered:
	case <-time.After(time.Second):
		t.Fatal("reservation did not start")
	}
	changeDone := make(chan error, 1)
	go func() {
		_, err := settings.Update(ctx, ImageStorageSettings{Enabled: false, StudioGalleryEnabled: true, Bucket: "new-gallery", Endpoint: "https://new-s3.test", Region: "auto", AccessKeyID: "ak", SecretAccessKey: "secret"})
		changeDone <- err
	}()
	select {
	case err := <-changeDone:
		t.Fatalf("target changed before first asset reservation: %v", err)
	case <-time.After(30 * time.Millisecond):
	}
	close(repo.reserveRelease)
	require.NoError(t, <-uploadDone)
	require.ErrorIs(t, <-changeDone, ErrGalleryStorageTargetInUse)
	require.Equal(t, "gallery", repo.assets["first-image"].Bucket)
}

func TestImageStudioGalleryBackupTargetGuardAllowsCredentialRotation(t *testing.T) {
	ctx := context.Background()
	repo := newStubSettingRepo()
	encryptor := reversibleEncryptor{}
	backup := NewBackupService(repo, &config.Config{Totp: config.TotpConfig{EncryptionKeyConfigured: true}}, encryptor, nil, nil)
	seedBackupS3(t, repo, BackupS3Config{Endpoint: "https://s3.test", Region: "auto", Bucket: "backups", AccessKeyID: "ak", SecretAccessKey: "secret"})
	settings := NewImageStorageSettingService(repo, encryptor, backup, nil, config.ImageStorageConfig{})
	_, err := settings.Update(ctx, ImageStorageSettings{StudioGalleryEnabled: true, ReuseBackupS3: true, Bucket: "gallery"})
	require.NoError(t, err)
	assets := &galleryTestRepo{assets: map[string]GalleryAsset{}, hasAssets: true}
	backup.SetGalleryRepository(assets)
	_, err = backup.UpdateS3Config(ctx, BackupS3Config{Endpoint: "https://new-s3.test", Region: "auto", Bucket: "backups", AccessKeyID: "ak", SecretAccessKey: "secret"})
	require.ErrorIs(t, err, ErrGalleryStorageTargetInUse)
	_, err = backup.UpdateS3Config(ctx, BackupS3Config{Endpoint: "https://s3.test", Region: "auto", Bucket: "backups", AccessKeyID: "ak2", SecretAccessKey: "rotated"})
	require.NoError(t, err, "credential rotation on the same target must remain possible")
}

func TestImageStudioGalleryRejectsPublicBaseURL(t *testing.T) {
	ctx := context.Background()
	settings, repo, _ := newImageStorageFixture(t, config.ImageStorageConfig{})
	_, err := settings.Update(ctx, ImageStorageSettings{
		StudioGalleryEnabled: true, Bucket: "gallery", Endpoint: "https://s3.test", AccessKeyID: "ak", SecretAccessKey: "secret", PublicBaseURL: "https://cdn.test",
	})
	require.ErrorIs(t, err, ErrGalleryPublicStorage)
	raw, err := repo.GetValue(ctx, settingKeyImageStorageConfig)
	require.NoError(t, err)
	require.Empty(t, raw)
	// Also reject a persisted legacy/malformed setting at runtime.
	require.NoError(t, repo.Set(ctx, settingKeyImageStorageConfig, `{"studio_gallery_enabled":true,"bucket":"gallery","endpoint":"https://s3.test","access_key_id":"ak","secret_access_key":"enc:secret","public_base_url":"https://cdn.test"}`))
	_, _, err = settings.StudioGalleryConfig(ctx)
	require.ErrorIs(t, err, ErrGalleryPublicStorage)
}

func TestImageStudioGalleryCleanupRetriesS3Failure(t *testing.T) {
	svc, repo, store, settings := galleryTestService(t)
	ctx := context.Background()
	require.NoError(t, svc.PutAsset(ctx, 17, "image-one", "image/png", galleryPNG(t, 10, 10)))
	repo.pending = []GalleryAsset{repo.assets["image-one"]}
	deleting := repo.assets["image-one"]
	deleting.State = "deleting"
	repo.assets["image-one"] = deleting
	store.deleteErr = errors.New("S3 is temporarily unavailable")
	_, err := settings.Update(ctx, ImageStorageSettings{Enabled: false, StudioGalleryEnabled: false, Bucket: "gallery", Endpoint: "https://s3.test", Region: "auto", AccessKeyID: "ak", SecretAccessKey: "secret"})
	require.NoError(t, err)
	require.NoError(t, svc.DeleteTask(ctx, 17, "task-one"), "metadata deletion remains committed")
	require.Zero(t, repo.finalized, "failed object delete must retain a retryable marker")
	store.deleteErr = nil
	svc.CleanupOnce(ctx)
	require.Equal(t, 1, repo.finalized)
	require.Empty(t, store.objects)
}
