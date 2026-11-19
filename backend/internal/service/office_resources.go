package service

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"image"
	"log/slog"
	"path"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/google/uuid"
)

type OfficeResourcesService struct {
	repo      OfficeResourcesRepository
	settings  *ImageStorageSettingService
	factory   GalleryObjectStoreFactory
	gallery   *ImageStudioGalleryService
	ioSlots   chan struct{}
	lifecycle sync.Mutex
	cancel    context.CancelFunc
	done      chan struct{}
}

func NewOfficeResourcesService(repo OfficeResourcesRepository, settings *ImageStorageSettingService, factory GalleryObjectStoreFactory, gallery *ImageStudioGalleryService) *OfficeResourcesService {
	s := &OfficeResourcesService{repo: repo, settings: settings, factory: factory, gallery: gallery, ioSlots: make(chan struct{}, 2)}
	if settings != nil && repo != nil {
		settings.SetOfficeStorageUsage(repo.HasStorage)
	}
	return s
}
func ProvideOfficeResourcesService(repo OfficeResourcesRepository, settings *ImageStorageSettingService, factory GalleryObjectStoreFactory, gallery *ImageStudioGalleryService) *OfficeResourcesService {
	s := NewOfficeResourcesService(repo, settings, factory, gallery)
	s.Start()
	return s
}
func (s *OfficeResourcesService) Start() {
	s.lifecycle.Lock()
	defer s.lifecycle.Unlock()
	if s.cancel != nil {
		return
	}
	ctx, cancel := context.WithCancel(context.Background())
	s.cancel = cancel
	s.done = make(chan struct{})
	go func() {
		defer close(s.done)
		ticker := time.NewTicker(5 * time.Minute)
		defer ticker.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				s.Cleanup(ctx)
			}
		}
	}()
}
func (s *OfficeResourcesService) Stop() {
	s.lifecycle.Lock()
	cancel, done := s.cancel, s.done
	if cancel == nil {
		s.lifecycle.Unlock()
		return
	}
	cancel()
	s.lifecycle.Unlock()
	<-done
}
func (s *OfficeResourcesService) AcquireIO() (func(), error) {
	select {
	case s.ioSlots <- struct{}{}:
		var once sync.Once
		return func() { once.Do(func() { <-s.ioSlots }) }, nil
	default:
		return nil, ErrOfficeCapacity
	}
}

func officeResourceHash(data []byte) string {
	v := sha256.Sum256(data)
	return hex.EncodeToString(v[:])
}

func (s *OfficeResourcesService) ResolveVersions(ctx context.Context, userID int64, ids []string) ([]OfficeResourceVersion, error) {
	return s.repo.ResolveVersions(ctx, userID, ids)
}
func (s *OfficeResourcesService) PinRunInputs(ctx context.Context, run OfficeRun) error {
	return s.repo.PinRunInputs(ctx, run)
}

func (s *OfficeResourcesService) List(ctx context.Context, userID int64, cursor string, limit int) ([]OfficeResource, string, error) {
	if err := s.repo.SyncGallery(ctx, userID); err != nil {
		return nil, "", err
	}
	return s.repo.ListResources(ctx, userID, cursor, limit)
}
func (s *OfficeResourcesService) Delete(ctx context.Context, userID int64, id string) error {
	return s.repo.DeleteResource(ctx, userID, id)
}

func (s *OfficeResourcesService) storage(ctx context.Context, endpoint, region, bucket string) (GalleryObjectStore, error) {
	if s.settings == nil || s.factory == nil {
		return nil, ErrOfficeStorageUnavailable
	}
	cfg, err := s.settings.PrivateOfficeStorageConfig(ctx)
	if err != nil {
		return nil, err
	}
	if cfg.Endpoint != endpoint || cfg.Region != region || cfg.Bucket != bucket {
		// Adopted gallery versions keep their original target and quota. Office
		// files can use a separate bucket without copying old image objects.
		galleryCfg, e := s.settings.effectiveConfig(ctx)
		if e != nil || galleryCfg == nil || galleryCfg.PublicBaseURL != "" || galleryCfg.Endpoint != endpoint || galleryCfg.Region != region || galleryCfg.Bucket != bucket {
			return nil, ErrOfficeStorageUnavailable
		}
		cfg = galleryCfg
	}
	return s.factory(ctx, cfg)
}

func (s *OfficeResourcesService) Access(ctx context.Context, userID int64, id string) (*OfficeResourceAccess, error) {
	v, err := s.repo.GetResourceVersion(ctx, userID, id, true)
	if err != nil {
		return nil, err
	}
	ctx, cancel := context.WithTimeout(ctx, 20*time.Second)
	defer cancel()
	store, err := s.storage(ctx, v.Endpoint, v.Region, v.Bucket)
	if err != nil {
		return nil, err
	}
	u, err := store.PresignGet(ctx, v.ObjectKey, 10*time.Minute)
	if err != nil {
		return nil, err
	}
	result := &OfficeResourceAccess{URL: u, ExpiresAt: time.Now().Add(10 * time.Minute), PreviewKind: v.PreviewKind}
	if v.PreviewKey != "" {
		result.PreviewURL, err = store.PresignGet(ctx, v.PreviewKey, 10*time.Minute)
		if err != nil {
			return nil, err
		}
	}
	return result, nil
}

func officeImagePreview(data []byte) ([]byte, string, error) {
	cfg, _, err := image.DecodeConfig(bytes.NewReader(data))
	if err != nil {
		return nil, "", ErrOfficeResourceType
	}
	release, err := acquireStudioImageMemory(cfg.Width, cfg.Height)
	if err != nil {
		return nil, "", err
	}
	defer release()
	img, _, err := image.Decode(bytes.NewReader(data))
	if err != nil {
		return nil, "", ErrOfficeResourceType
	}
	preview, _, _, mime, err := galleryVariant(img, 1280)
	return preview, mime, err
}

func (s *OfficeResourcesService) Upload(ctx context.Context, userID int64, name string, data []byte, operationID string) (*OfficeResource, error) {
	mime, kind, err := validateOfficeFile(name, data)
	if err != nil {
		return nil, err
	}
	var preview []byte
	previewMIME := ""
	if kind == "image" {
		preview, previewMIME, err = officeImagePreview(data)
		if err != nil {
			return nil, err
		}
	}
	if operationID == "" {
		operationID = uuid.NewString()
	}
	if len(operationID) > 128 || strings.ContainsAny(operationID, "\n\r") {
		return nil, ErrOfficeInvalid
	}
	resource := OfficeResource{ID: uuid.NewString(), VersionID: uuid.NewString(), UserID: userID, Name: name, MIME: mime, Bytes: int64(len(data)), SHA256: officeResourceHash(data), PreviewKind: kind, Source: "upload", PublicationID: "upload:" + officeResourceHash([]byte(operationID)), PreviewBytes: int64(len(preview)), PreviewMIME: previewMIME, PreviewSHA256: officeResourceHash(preview)}
	return s.persistVersion(ctx, resource, nil, data, preview)
}

func (s *OfficeResourcesService) persistVersion(ctx context.Context, in OfficeResource, lease *OfficeRunLease, data, preview []byte) (*OfficeResource, error) {
	if s.settings == nil {
		return nil, ErrOfficeStorageUnavailable
	}
	cfg, release, err := s.settings.AcquireOfficeStorage(ctx)
	if err != nil {
		return nil, err
	}
	in.Endpoint, in.Region, in.Bucket = cfg.Endpoint, cfg.Region, cfg.Bucket
	in.ObjectKey = "workspace/v1/resources/" + in.VersionID + "/original"
	if len(preview) > 0 {
		in.PreviewKey = "workspace/v1/resources/" + in.VersionID + "/preview"
	}
	reserved, err := s.repo.ReserveVersion(ctx, in, lease)
	release()
	if err != nil {
		return nil, err
	}
	if reserved.State == "ready" {
		return reserved, nil
	}
	ctx, cancel := context.WithTimeout(ctx, 45*time.Second)
	defer cancel()
	store, err := s.storage(ctx, reserved.Endpoint, reserved.Region, reserved.Bucket)
	if err != nil {
		return nil, err
	}
	if err = store.Put(ctx, reserved.ObjectKey, reserved.MIME, data); err != nil {
		return nil, err
	}
	if len(preview) > 0 {
		if err = store.Put(ctx, reserved.PreviewKey, reserved.PreviewMIME, preview); err != nil {
			return nil, err
		}
	}
	return s.repo.FinishVersion(ctx, in.UserID, reserved.VersionID, lease)
}

func (s *OfficeResourcesService) initialWorkspace(ctx context.Context, lease OfficeRunLease) (*OfficeWorkspace, error) {
	workspace := &OfficeWorkspace{Files: []OfficeWorkspaceFile{}}
	for _, ref := range lease.Run.InputResources {
		v, err := s.repo.GetResourceVersion(ctx, lease.Run.UserID, ref.VersionID, true)
		if err != nil {
			return nil, err
		}
		if v.ID != ref.ResourceID {
			return nil, ErrOfficeNotFound
		}
		store, err := s.storage(ctx, v.Endpoint, v.Region, v.Bucket)
		if err != nil {
			return nil, err
		}
		reader, ok := store.(GalleryObjectReader)
		if !ok {
			return nil, ErrOfficeStorageUnavailable
		}
		data, err := reader.Get(ctx, v.ObjectKey, OfficeResourceMaxBytes)
		if err != nil {
			return nil, err
		}
		if officeResourceHash(data) != v.SHA256 || int64(len(data)) != v.Bytes {
			return nil, ErrOfficeConflict
		}
		workspace.Files = append(workspace.Files, OfficeWorkspaceFile{Path: "uploads/" + v.VersionID + "/" + v.Name, Size: v.Bytes, SHA256: v.SHA256, MIME: v.MIME, ContentBase64: base64.StdEncoding.EncodeToString(data)})
	}
	if _, err := validateOfficeWorkspace(workspace.Files); err != nil {
		return nil, err
	}
	return workspace, nil
}

func (s *OfficeResourcesService) GetWorkspace(ctx context.Context, lease OfficeRunLease, version *int64) (*OfficeWorkspace, error) {
	ctx, cancel := context.WithTimeout(ctx, 45*time.Second)
	defer cancel()
	head, err := s.repo.LatestSnapshot(ctx, lease, 0)
	if err != nil && !errors.Is(err, ErrOfficeNotFound) {
		return nil, err
	}
	if version != nil && *version < 0 {
		return nil, ErrOfficeInvalid
	}
	if (version != nil && *version == 0) || head == nil {
		if version != nil && *version > 0 {
			return nil, ErrOfficeNotFound
		}
		initial, e := s.initialWorkspace(ctx, lease)
		if initial != nil && head != nil {
			initial.HeadVersion = head.Version
		}
		return initial, e
	}
	selected := head
	if version != nil && *version != head.Version {
		selected, err = s.repo.LatestSnapshot(ctx, lease, *version)
		if err != nil {
			return nil, err
		}
	}
	store, err := s.storage(ctx, selected.Endpoint, selected.Region, selected.Bucket)
	if err != nil {
		return nil, err
	}
	reader, ok := store.(GalleryObjectReader)
	if !ok {
		return nil, ErrOfficeStorageUnavailable
	}
	data, err := reader.Get(ctx, selected.ObjectKey, 40<<20)
	if err != nil {
		return nil, err
	}
	if int64(len(data)) != selected.Bytes || officeResourceHash(data) != selected.Hash {
		return nil, ErrOfficeConflict
	}
	var files []OfficeWorkspaceFile
	if json.Unmarshal(data, &files) != nil {
		return nil, ErrOfficeInvalid
	}
	if _, err = validateOfficeWorkspace(files); err != nil {
		return nil, err
	}
	return &OfficeWorkspace{Version: selected.Version, HeadVersion: head.Version, SnapshotID: selected.ID, Files: files}, nil
}

func (s *OfficeResourcesService) SaveWorkspace(ctx context.Context, lease OfficeRunLease, in OfficeWorkspaceSave) (*OfficeWorkspace, error) {
	if _, err := validateOfficeWorkspace(in.Files); err != nil {
		return nil, err
	}
	sort.Slice(in.Files, func(i, j int) bool { return in.Files[i].Path < in.Files[j].Path })
	data, err := json.Marshal(in.Files)
	if err != nil {
		return nil, err
	}
	cfg, release, err := s.settings.AcquireOfficeStorage(ctx)
	if err != nil {
		return nil, err
	}
	candidate := OfficeSnapshot{ID: uuid.NewString(), UserID: lease.Run.UserID, RunID: lease.Run.ID, OperationID: in.OperationID, Hash: officeResourceHash(data), Bytes: int64(len(data)), Endpoint: cfg.Endpoint, Region: cfg.Region, Bucket: cfg.Bucket}
	candidate.ObjectKey = "workspace/v1/staging/" + lease.Run.ID + "/" + candidate.ID + ".json"
	reserved, err := s.repo.ReserveSnapshot(ctx, lease, candidate, in.ExpectedVersion)
	release()
	if err != nil {
		return nil, err
	}
	if reserved.State != "ready" {
		ctx, cancel := context.WithTimeout(ctx, 45*time.Second)
		defer cancel()
		store, e := s.storage(ctx, reserved.Endpoint, reserved.Region, reserved.Bucket)
		if e != nil {
			return nil, e
		}
		if e = store.Put(ctx, reserved.ObjectKey, "application/json", data); e != nil {
			return nil, e
		}
		reserved, err = s.repo.FinishSnapshot(ctx, lease, reserved.ID)
		if err != nil {
			return nil, err
		}
	}
	head, err := s.repo.LatestSnapshot(ctx, lease, 0)
	if err != nil {
		return nil, err
	}
	return &OfficeWorkspace{Version: reserved.Version, HeadVersion: head.Version, SnapshotID: reserved.ID}, nil
}

func (s *OfficeResourcesService) PublishArtifact(ctx context.Context, lease OfficeRunLease, in OfficeArtifactPublish) (*OfficeResource, error) {
	if !strings.HasPrefix(in.Path, "output/") || !officeWorkspacePath(in.Path) || in.WorkspaceVersion < 1 {
		return nil, ErrOfficeInvalid
	}
	if in.PublicationID == "" || len(in.PublicationID) > 128 {
		return nil, ErrOfficeInvalid
	}
	workspace, err := s.GetWorkspace(ctx, lease, &in.WorkspaceVersion)
	if err != nil {
		return nil, err
	}
	files, err := validateOfficeWorkspace(workspace.Files)
	if err != nil {
		return nil, err
	}
	data, ok := files[in.Path]
	if !ok || int64(len(data)) != in.Size || officeResourceHash(data) != in.SHA256 {
		return nil, ErrOfficeInvalid
	}
	mime, kind, err := validateOfficeFile(path.Base(in.Path), data)
	if err != nil {
		return nil, err
	}
	if mime != in.MIME {
		return nil, ErrOfficeResourceType
	}
	var preview []byte
	previewMIME := ""
	if in.PreviewPath != "" {
		if !officeWorkspacePath(in.PreviewPath) {
			return nil, ErrOfficeInvalid
		}
		var exists bool
		preview, exists = files[in.PreviewPath]
		if !exists || len(preview) > 8<<20 {
			return nil, ErrOfficeInvalid
		}
		pm, pk, e := validateOfficeFile(path.Base(in.PreviewPath), preview)
		if e != nil || (pk != "image" && pk != "pdf") {
			return nil, ErrOfficeResourceType
		}
		previewMIME = pm
		kind = pk
	} else if kind == "image" {
		preview, previewMIME, err = officeImagePreview(data)
		if err != nil {
			return nil, err
		}
	}
	id := in.ResourceID
	if id == "" {
		id = uuid.NewString()
	}
	resource := OfficeResource{ID: id, VersionID: uuid.NewString(), UserID: lease.Run.UserID, RunID: lease.Run.ID, ParentVersionID: in.ParentVersionID, Name: path.Base(in.Path), MIME: mime, Bytes: in.Size, SHA256: in.SHA256, PreviewKind: kind, Source: "generated", PublicationID: "artifact:" + officeResourceHash([]byte(lease.Run.ID+":"+in.PublicationID)), PreviewBytes: int64(len(preview)), PreviewMIME: previewMIME, PreviewSHA256: officeResourceHash(preview)}
	return s.persistVersion(ctx, resource, &lease, data, preview)
}

func (s *OfficeResourcesService) Cleanup(ctx context.Context) {
	items, err := s.repo.ListCleanup(ctx, 50)
	if err != nil {
		slog.Warn("office resources cleanup metadata failed", "error", err)
		return
	}
	for _, item := range items {
		if ctx.Err() != nil {
			return
		}
		task, cancel := context.WithTimeout(ctx, 45*time.Second)
		store, e := s.storage(task, item.Endpoint, item.Region, item.Bucket)
		if e == nil {
			for _, key := range item.Keys {
				if !strings.HasPrefix(key, "workspace/v1/") {
					e = ErrOfficeInvalid
					break
				}
				if e = store.Delete(task, key); e != nil {
					break
				}
			}
		}
		if e == nil {
			e = s.repo.FinalizeCleanup(task, item)
		}
		cancel()
		if e != nil {
			slog.Warn("office resource cleanup deferred", "kind", item.Kind, "id", item.ID, "error", e)
		}
	}
}
