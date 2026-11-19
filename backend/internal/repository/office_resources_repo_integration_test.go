//go:build integration

package repository

import (
	"context"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/config"
	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
)

type officeResourceSettings struct{}

func (officeResourceSettings) Get(context.Context, string) (*service.Setting, error) {
	return nil, service.ErrSettingNotFound
}
func (officeResourceSettings) GetValue(context.Context, string) (string, error) {
	return "", service.ErrSettingNotFound
}
func (officeResourceSettings) Set(context.Context, string, string) error { return nil }
func (officeResourceSettings) GetMultiple(context.Context, []string) (map[string]string, error) {
	return nil, nil
}
func (officeResourceSettings) SetMultiple(context.Context, map[string]string) error { return nil }
func (officeResourceSettings) GetAll(context.Context) (map[string]string, error)    { return nil, nil }
func (officeResourceSettings) Delete(context.Context, string) error                 { return nil }

type officeMemoryStore struct {
	mu     sync.Mutex
	values map[string][]byte
	fail   bool
}

func (s *officeMemoryStore) Put(_ context.Context, key, _ string, data []byte) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.fail {
		return service.ErrOfficeStorageUnavailable
	}
	s.values[key] = append([]byte{}, data...)
	return nil
}
func (s *officeMemoryStore) Get(_ context.Context, key string, max int64) ([]byte, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	v, ok := s.values[key]
	if !ok {
		return nil, service.ErrOfficeNotFound
	}
	if int64(len(v)) > max {
		return nil, service.ErrOfficeResourceTooLarge
	}
	return append([]byte{}, v...), nil
}
func (s *officeMemoryStore) Delete(_ context.Context, key string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	delete(s.values, key)
	return nil
}
func (s *officeMemoryStore) PresignGet(_ context.Context, key string, _ time.Duration) (string, error) {
	return "https://private-signed.example/" + key + "?temporary=1", nil
}

func officeResourceFixture(t *testing.T) (service.OfficeAgentRepository, service.OfficeRunAdmission, service.OfficeResourcesRepository, *service.OfficeResourcesService, *officeMemoryStore) {
	t.Helper()
	agent, in := officeTestAdmission(t)
	t.Cleanup(func() {
		ctx := context.Background()
		for _, table := range []string{"office_resource_refs", "office_gallery_pins", "office_resource_versions", "office_resources", "office_workspace_snapshots"} {
			_, _ = integrationDB.ExecContext(ctx, `DELETE FROM `+table+` WHERE user_id=$1`, in.UserID)
		}
	})
	repo := NewOfficeResourcesRepository(integrationDB)
	store := &officeMemoryStore{values: map[string][]byte{}}
	settings := service.NewImageStorageSettingService(officeResourceSettings{}, nil, nil, nil, config.ImageStorageConfig{Endpoint: "https://private.example", Region: "auto", Bucket: "private-files", AccessKeyID: "test", SecretAccessKey: "test"})
	svc := service.NewOfficeResourcesService(repo, settings, func(context.Context, *config.ImageStorageConfig) (service.GalleryObjectStore, error) {
		return store, nil
	}, nil)
	return agent, in, repo, svc, store
}
func officeWorkspaceFile(path string, data []byte) service.OfficeWorkspaceFile {
	sum := sha256.Sum256(data)
	return service.OfficeWorkspaceFile{Path: path, Size: int64(len(data)), MIME: "text/plain", SHA256: hex.EncodeToString(sum[:]), ContentBase64: base64.StdEncoding.EncodeToString(data)}
}
func officeResourceClaim(t *testing.T, agent service.OfficeAgentRepository, in service.OfficeRunAdmission) *service.OfficeRunLease {
	t.Helper()
	_, _, err := agent.AdmitRun(context.Background(), in)
	require.NoError(t, err)
	lease, err := agent.ClaimRun(context.Background(), "resources-test", time.Minute)
	require.NoError(t, err)
	require.NotNil(t, lease)
	require.Equal(t, in.RunID, lease.Run.ID)
	return lease
}

func TestOfficeResourcesOwnedImmutableAndDeletedReferences(t *testing.T) {
	ctx := context.Background()
	agent, in, repo, svc, _ := officeResourceFixture(t)
	upload, err := svc.Upload(ctx, in.UserID, "notes.txt", []byte("original"), "upload-1")
	require.NoError(t, err)
	replay, err := svc.Upload(ctx, in.UserID, "notes.txt", []byte("original"), "upload-1")
	require.NoError(t, err)
	require.Equal(t, upload.VersionID, replay.VersionID)
	_, err = svc.Upload(ctx, in.UserID, "notes.txt", []byte("changed!"), "upload-1")
	require.ErrorIs(t, err, service.ErrOfficeConflict)
	_, err = svc.Access(ctx, in.UserID+999, upload.VersionID)
	require.ErrorIs(t, err, service.ErrOfficeNotFound)
	refs, err := svc.ResolveVersions(ctx, in.UserID, []string{upload.VersionID})
	require.NoError(t, err)
	require.Equal(t, upload.ID, refs[0].ResourceID)
	in.InputResources = refs
	lease := officeResourceClaim(t, agent, in)
	require.NoError(t, svc.Delete(ctx, in.UserID, upload.ID))
	_, err = svc.ResolveVersions(ctx, in.UserID, []string{upload.VersionID})
	require.ErrorIs(t, err, service.ErrOfficeNotFound)
	access, err := svc.Access(ctx, in.UserID, upload.VersionID)
	require.NoError(t, err)
	require.Contains(t, access.URL, "temporary=1")
	initial, err := svc.GetWorkspace(ctx, *lease, nil)
	require.NoError(t, err)
	require.Len(t, initial.Files, 1)
	require.Equal(t, base64.StdEncoding.EncodeToString([]byte("original")), initial.Files[0].ContentBase64)
	_, err = repo.ListCleanup(ctx, 20)
	require.NoError(t, err)
	_, err = repo.GetResourceVersion(ctx, in.UserID, upload.VersionID, true)
	require.NoError(t, err, "conversation reference retains file after My Files deletion")
	_, err = integrationDB.ExecContext(ctx, `UPDATE office_agent_conversations SET deleted_at=NOW() WHERE id=$1`, in.ConversationID)
	require.NoError(t, err)
	svc.Cleanup(ctx)
	_, err = repo.GetResourceVersion(ctx, in.UserID, upload.VersionID, true)
	require.ErrorIs(t, err, service.ErrOfficeNotFound)
}

func TestOfficeResourcesWorkspaceCASRecoveryAndLatePublicationFence(t *testing.T) {
	ctx := context.Background()
	agent, in, repo, svc, _ := officeResourceFixture(t)
	lease := officeResourceClaim(t, agent, in)
	f1 := officeWorkspaceFile("output/report.txt", []byte("first"))
	save := service.OfficeWorkspaceSave{OperationID: "save-1", ExpectedVersion: 0, Files: []service.OfficeWorkspaceFile{f1}}
	one, err := svc.SaveWorkspace(ctx, *lease, save)
	require.NoError(t, err)
	require.EqualValues(t, 1, one.Version)
	replay, err := svc.SaveWorkspace(ctx, *lease, save)
	require.NoError(t, err)
	require.Equal(t, one.SnapshotID, replay.SnapshotID)
	changed := save
	changed.Files = []service.OfficeWorkspaceFile{officeWorkspaceFile(f1.Path, []byte("other"))}
	_, err = svc.SaveWorkspace(ctx, *lease, changed)
	require.ErrorIs(t, err, service.ErrOfficeConflict)
	stale := save
	stale.OperationID = "save-stale"
	_, err = svc.SaveWorkspace(ctx, *lease, stale)
	require.ErrorIs(t, err, service.ErrOfficeConflict)
	two, err := svc.SaveWorkspace(ctx, *lease, service.OfficeWorkspaceSave{OperationID: "save-2", ExpectedVersion: 1, Files: []service.OfficeWorkspaceFile{officeWorkspaceFile(f1.Path, []byte("second"))}})
	require.NoError(t, err)
	require.EqualValues(t, 2, two.Version)
	v1 := int64(1)
	old, err := svc.GetWorkspace(ctx, *lease, &v1)
	require.NoError(t, err)
	require.EqualValues(t, 1, old.Version)
	require.EqualValues(t, 2, old.HeadVersion)
	require.Equal(t, f1.ContentBase64, old.Files[0].ContentBase64)
	v0 := int64(0)
	initial, err := svc.GetWorkspace(ctx, *lease, &v0)
	require.NoError(t, err)
	require.Empty(t, initial.Files)
	require.EqualValues(t, 2, initial.HeadVersion)
	publish := service.OfficeArtifactPublish{PublicationID: "public-1", Path: f1.Path, SHA256: f1.SHA256, MIME: f1.MIME, Size: f1.Size, WorkspaceVersion: 1}
	artifact, err := svc.PublishArtifact(ctx, *lease, publish)
	require.NoError(t, err)
	same, err := svc.PublishArtifact(ctx, *lease, publish)
	require.NoError(t, err)
	require.Equal(t, artifact.VersionID, same.VersionID)
	events, err := agent.ListEvents(ctx, in.UserID, in.RunID, 0, 100)
	require.NoError(t, err)
	publicationEvents := 0
	for _, event := range events {
		if event.Type != "artifact.published" {
			continue
		}
		publicationEvents++
		var payload struct {
			Artifact service.OfficeResource `json:"artifact"`
		}
		require.NoError(t, json.Unmarshal(event.Payload, &payload))
		require.Equal(t, artifact.VersionID, payload.Artifact.VersionID)
		require.Equal(t, artifact.Name, payload.Artifact.Name)
		require.Equal(t, "output", payload.Artifact.Role)
		require.NotContains(t, string(event.Payload), "private-files")
		require.NotContains(t, string(event.Payload), "workspace/v1/")
	}
	require.Equal(t, 1, publicationEvents, "archive ready and its event commit together, while stable replay does not duplicate it")
	pending := service.OfficeResource{ID: uuid.NewString(), VersionID: uuid.NewString(), UserID: in.UserID, RunID: in.RunID, PublicationID: "late-archive", Name: "late.txt", MIME: "text/plain", Bytes: 1, SHA256: strings.Repeat("b", 64), Source: "generated", PreviewKind: "text", Endpoint: "https://private.example", Region: "auto", Bucket: "private-files", ObjectKey: "workspace/v1/resources/late-original"}
	_, err = repo.ReserveVersion(ctx, pending, lease)
	require.NoError(t, err)
	_, err = agent.CancelRun(ctx, in.UserID, in.RunID)
	require.NoError(t, err)
	publish.PublicationID = "late"
	_, err = svc.PublishArtifact(ctx, *lease, publish)
	require.ErrorIs(t, err, service.ErrOfficeLeaseLost)
	_, err = repo.FinishSnapshot(ctx, *lease, two.SnapshotID)
	require.ErrorIs(t, err, service.ErrOfficeLeaseLost)
	_, err = repo.FinishVersion(ctx, in.UserID, pending.VersionID, lease)
	require.ErrorIs(t, err, service.ErrOfficeLeaseLost, "S3 archival finishing after cancellation must not publish metadata")
	var archivedEvents int
	require.NoError(t, integrationDB.QueryRowContext(ctx, `SELECT COUNT(*) FROM office_agent_run_events WHERE run_id=$1 AND event_type='artifact.published'`, in.RunID).Scan(&archivedEvents))
	require.Equal(t, 1, archivedEvents, "later cancellation preserves the first archived result without publishing a late one")
}

func TestOfficeResourcesModifiedDocumentPreservesParentAndRejectsLostUpdate(t *testing.T) {
	ctx := context.Background()
	agent, in, repo, svc, _ := officeResourceFixture(t)
	parent, err := svc.Upload(ctx, in.UserID, "office.txt", []byte("original"), "immutable-parent")
	require.NoError(t, err)
	lease := officeResourceClaim(t, agent, in)
	first := service.OfficeResource{ID: parent.ID, VersionID: uuid.NewString(), ParentVersionID: parent.VersionID, UserID: in.UserID, RunID: in.RunID, PublicationID: "edit-1", Name: parent.Name, MIME: "text/plain", Bytes: 8, SHA256: strings.Repeat("b", 64), Source: "generated", PreviewKind: "text", Endpoint: "https://private.example", Region: "auto", Bucket: "private-files", ObjectKey: "workspace/v1/resources/edit-1"}
	second := first
	second.VersionID = uuid.NewString()
	second.PublicationID = "edit-2"
	second.ObjectKey = "workspace/v1/resources/edit-2"
	_, err = repo.ReserveVersion(ctx, first, lease)
	require.NoError(t, err)
	_, err = repo.ReserveVersion(ctx, second, lease)
	require.NoError(t, err)
	_, err = repo.FinishVersion(ctx, in.UserID, first.VersionID, lease)
	require.NoError(t, err)
	_, err = repo.FinishVersion(ctx, in.UserID, second.VersionID, lease)
	require.ErrorIs(t, err, service.ErrOfficeConflict, "another edit must not overwrite the newly published version")
	original, err := repo.GetResourceVersion(ctx, in.UserID, parent.VersionID, true)
	require.NoError(t, err)
	require.Equal(t, parent.SHA256, original.SHA256)
	list, _, err := repo.ListResources(ctx, in.UserID, "", 20)
	require.NoError(t, err)
	require.Len(t, list, 1)
	require.Equal(t, first.VersionID, list[0].VersionID)
	require.Equal(t, "output", list[0].Role)
}

func TestOfficeResourcesPendingQuotaAndStorageFailureAreDurable(t *testing.T) {
	ctx := context.Background()
	_, in, repo, svc, store := officeResourceFixture(t)
	store.fail = true
	_, err := svc.Upload(ctx, in.UserID, "failed.txt", []byte("pending bytes"), "fail")
	require.Error(t, err)
	has, err := repo.HasStorage(ctx)
	require.NoError(t, err)
	require.True(t, has)
	var pending int
	require.NoError(t, integrationDB.QueryRowContext(ctx, `SELECT COUNT(*) FROM office_resource_versions WHERE user_id=$1 AND state='pending'`, in.UserID).Scan(&pending))
	require.Equal(t, 1, pending)
	store.fail = false
	_, err = svc.Upload(ctx, in.UserID, "failed.txt", []byte("pending bytes"), "fail")
	require.NoError(t, err, "stable operation resumes reservation instead of orphaning it")
	// Bound reservations count even before S3 succeeds. Populate only metadata;
	// no large files or objects are allocated by this semantic quota test.
	_, err = integrationDB.ExecContext(ctx, `INSERT INTO office_resources(id,user_id,source) SELECT md5($1::text||n::text)::uuid,$2,'upload' FROM generate_series(1,43)n`, uuid.NewString(), in.UserID)
	require.NoError(t, err)
	_, err = integrationDB.ExecContext(ctx, `INSERT INTO office_resource_versions(id,user_id,resource_id,publication_id,name,mime,byte_size,sha256,state,storage_endpoint,storage_region,storage_bucket,object_key,preview_kind) SELECT r.id,r.user_id,r.id,r.id::text,'quota.txt','text/plain',25165824,$2,'pending','','','private-files','workspace/v1/quota/'||r.id,'text' FROM office_resources r WHERE r.user_id=$1 AND r.latest_version_id IS NULL`, in.UserID, strings.Repeat("a", 64))
	require.NoError(t, err)
	_, err = svc.Upload(ctx, in.UserID, "too-much.txt", []byte("x"), "over-quota")
	require.ErrorIs(t, err, service.ErrOfficeResourceQuota)
}

func TestOfficeResourcesAdoptGalleryPinsSurviveTaskDeletion(t *testing.T) {
	ctx := context.Background()
	_, in, repo, svc, _ := officeResourceFixture(t)
	gallery := NewImageStudioGalleryRepository(integrationDB)
	imageID := "image-" + uuid.NewString()
	asset := galleryTestAsset(in.UserID, imageID)
	_, err := gallery.ReserveAsset(ctx, asset)
	require.NoError(t, err)
	require.NoError(t, gallery.MarkAssetReady(ctx, in.UserID, imageID))
	taskID := "task-" + uuid.NewString()
	task := json.RawMessage(`{"status":"done"}`)
	_, err = gallery.SaveTask(ctx, in.UserID, taskID, strings.Repeat("b", 64), task, []service.GalleryTaskLink{{ImageID: imageID, Role: "output", Ordinal: 0}})
	require.NoError(t, err)
	require.NoError(t, repo.SyncGallery(ctx, in.UserID))
	items, _, err := repo.ListResources(ctx, in.UserID, "", 20)
	require.NoError(t, err)
	require.Len(t, items, 1)
	require.Equal(t, "gallery", items[0].Source)
	require.Equal(t, asset.OriginalKey, items[0].ObjectKey)
	deleted, err := gallery.DeleteTask(ctx, in.UserID, taskID)
	require.NoError(t, err)
	require.Empty(t, deleted, "adopted image must not be deleted with old generation task")
	cleanup, err := gallery.ListCleanupAssets(ctx, 100)
	require.NoError(t, err)
	for _, v := range cleanup {
		require.NotEqual(t, imageID, v.ImageID)
	}
	require.NoError(t, svc.Delete(ctx, in.UserID, items[0].ID))
	svc.Cleanup(ctx)
	var pins int
	require.NoError(t, integrationDB.QueryRowContext(ctx, `SELECT COUNT(*) FROM office_gallery_pins WHERE user_id=$1`, in.UserID).Scan(&pins))
	require.Zero(t, pins)
	require.NoError(t, repo.SyncGallery(ctx, in.UserID))
	items, _, err = repo.ListResources(ctx, in.UserID, "", 20)
	require.NoError(t, err)
	require.Empty(t, items, "a deleted gallery adoption stays deleted rather than reappearing at next list")
}

func TestOfficeResourcesGalleryAdoptionsDoNotConsumeOfficeFileQuota(t *testing.T) {
	ctx := context.Background()
	_, in, _, svc, _ := officeResourceFixture(t)
	// Gallery has its own admission/quota. Its zero-copy metadata and deletion
	// tombstones must not exhaust office upload slots or require image copying.
	_, err := integrationDB.ExecContext(ctx, `INSERT INTO office_resources(id,user_id,source,gallery_image_id,deleted_at) SELECT md5($1::text||n::text)::uuid,$2,'gallery',$1::text||n::text,NOW() FROM generate_series(1,1001)n`, uuid.NewString(), in.UserID)
	require.NoError(t, err)
	_, err = svc.Upload(ctx, in.UserID, "available.txt", []byte("still available"), "gallery-no-double-quota")
	require.NoError(t, err)
}

func TestOfficeResourcesCleanupProgressPastTombstonesAndRetainedResources(t *testing.T) {
	ctx := context.Background()
	agent, in, repo, svc, _ := officeResourceFixture(t)
	var retained []*service.OfficeResource
	for index := 0; index < 60; index++ {
		item, err := svc.Upload(ctx, in.UserID, "retained.txt", []byte("retained"), uuid.NewString())
		require.NoError(t, err)
		retained = append(retained, item)
		in.InputResources = append(in.InputResources, service.OfficeResourceVersion{ResourceID: item.ID, VersionID: item.VersionID})
	}
	visible, err := svc.Upload(ctx, in.UserID, "visible.txt", []byte("visible"), uuid.NewString())
	require.NoError(t, err)
	in.InputResources = append(in.InputResources, service.OfficeResourceVersion{ResourceID: visible.ID, VersionID: visible.VersionID})
	_ = officeResourceClaim(t, agent, in)
	for _, item := range retained {
		require.NoError(t, svc.Delete(ctx, in.UserID, item.ID))
	}
	_, err = integrationDB.ExecContext(ctx, `INSERT INTO office_resources(id,user_id,source,gallery_image_id,created_at,deleted_at) SELECT md5($1::text||n::text)::uuid,$2,'gallery',$1::text||n::text,NOW()-INTERVAL '2 days',NOW() FROM generate_series(1,60)n`, uuid.NewString(), in.UserID)
	require.NoError(t, err)
	orphan := uuid.NewString()
	_, err = integrationDB.ExecContext(ctx, `INSERT INTO office_resources(id,user_id,source,created_at,deleted_at) VALUES($1,$2,'upload',NOW()-INTERVAL '2 days',NOW())`, orphan, in.UserID)
	require.NoError(t, err)
	victim, err := svc.Upload(ctx, in.UserID, "delete-me.txt", []byte("can delete"), uuid.NewString())
	require.NoError(t, err)
	require.NoError(t, svc.Delete(ctx, in.UserID, victim.ID))
	svc.Cleanup(ctx)
	_, err = repo.GetResourceVersion(ctx, in.UserID, victim.VersionID, true)
	require.ErrorIs(t, err, service.ErrOfficeNotFound, "more than one page of older tombstones and retained files must not starve a later deletable file")
	var count int
	require.NoError(t, integrationDB.QueryRowContext(ctx, `SELECT COUNT(*) FROM office_resources WHERE id=$1`, orphan).Scan(&count))
	require.Zero(t, count, "empty ordinary metadata is collected instead of occupying every candidate page")
	_, err = repo.GetResourceVersion(ctx, in.UserID, retained[0].VersionID, true)
	require.NoError(t, err, "a live conversation still protects its deleted resource")
	require.NoError(t, integrationDB.QueryRowContext(ctx, `SELECT COUNT(*) FROM office_resource_refs WHERE user_id=$1`, in.UserID).Scan(&count))
	require.Equal(t, 61, count)
	_, err = integrationDB.ExecContext(ctx, `UPDATE office_agent_conversations SET deleted_at=NOW() WHERE id=$1`, in.ConversationID)
	require.NoError(t, err)
	svc.Cleanup(ctx)
	require.NoError(t, integrationDB.QueryRowContext(ctx, `SELECT COUNT(*) FROM office_resource_refs WHERE user_id=$1`, in.UserID).Scan(&count))
	require.Zero(t, count, "deleted-conversation references are released even for a resource that remains visible in My Files")
	_, err = repo.GetResourceVersion(ctx, in.UserID, visible.VersionID, false)
	require.NoError(t, err, "conversation deletion does not delete an independently visible file")
}
