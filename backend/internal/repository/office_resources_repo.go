package repository

import (
	"context"
	"database/sql"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/google/uuid"
)

type officeResourcesRepository struct{ db *sql.DB }

func NewOfficeResourcesRepository(db *sql.DB) service.OfficeResourcesRepository {
	return &officeResourcesRepository{db: db}
}

const officeResourceColumns = `r.id::text,v.id::text,COALESCE(v.parent_version_id::text,''),v.name,v.mime,v.byte_size,v.sha256,v.preview_kind,r.source,v.created_at,r.user_id,v.state,v.publication_id,COALESCE(v.run_id::text,''),COALESCE(r.gallery_image_id,''),v.storage_endpoint,v.storage_region,v.storage_bucket,v.object_key,v.preview_key,v.preview_mime,v.preview_bytes,v.preview_sha256`

func scanOfficeResource(row officeScanner) (*service.OfficeResource, error) {
	v := &service.OfficeResource{}
	err := row.Scan(&v.ID, &v.VersionID, &v.ParentVersionID, &v.Name, &v.MIME, &v.Bytes, &v.SHA256, &v.PreviewKind, &v.Source, &v.CreatedAt, &v.UserID, &v.State, &v.PublicationID, &v.RunID, &v.GalleryImageID, &v.Endpoint, &v.Region, &v.Bucket, &v.ObjectKey, &v.PreviewKey, &v.PreviewMIME, &v.PreviewBytes, &v.PreviewSHA256)
	if v.RunID != "" {
		v.Role = "output"
	} else {
		v.Role = "input"
	}
	return v, officeDBError(err)
}

func officeResourceQuota(ctx context.Context, tx *sql.Tx, userID int64, bytes int64, files int) error {
	var used, count int64
	err := tx.QueryRowContext(ctx, `SELECT
 (SELECT COALESCE(SUM(v.byte_size+v.preview_bytes),0) FROM office_resource_versions v JOIN office_resources r ON r.id=v.resource_id WHERE v.user_id=$1 AND r.source<>'gallery')+
 (SELECT COALESCE(SUM(byte_size),0) FROM office_workspace_snapshots WHERE user_id=$1),
 (SELECT COUNT(*) FROM office_resources WHERE user_id=$1 AND source<>'gallery')`, userID).Scan(&used, &count)
	if err != nil {
		return err
	}
	if used+bytes > service.OfficeResourceUserBytes || count+int64(files) > service.OfficeResourceUserFiles {
		return service.ErrOfficeResourceQuota
	}
	return nil
}

func (r *officeResourcesRepository) StorePrintPreview(ctx context.Context, userID int64, versionID, key string, bytes int64, hash string, assignment *service.OfficeDispatch, upload func() error) error {
	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback() }()
	if err = galleryLockUser(ctx, tx, userID); err != nil {
		return err
	}
	if err = galleryRequireActiveUser(ctx, tx, userID); err != nil {
		return err
	}
	var existing string
	err = tx.QueryRowContext(ctx, `SELECT v.preview_key FROM office_resource_versions v JOIN office_resources r ON r.id=v.resource_id
		WHERE v.user_id=$1 AND v.id=$2 AND v.state='ready' AND r.deleted_at IS NULL FOR UPDATE OF v`, userID, versionID).Scan(&existing)
	if err != nil {
		return officeDBError(err)
	}
	if existing == key {
		return tx.Commit()
	}
	if existing != "" {
		return service.ErrOfficeConflict
	}
	if assignment != nil {
		var active bool
		err = tx.QueryRowContext(ctx, `SELECT state='active' AND attempt=$4 FROM workbench_office_dispatch
WHERE job_key=$1 AND user_id=$2 AND version_id=$3 FOR UPDATE`, assignment.Key, userID, versionID, assignment.Attempt).Scan(&active)
		if errors.Is(err, sql.ErrNoRows) || err == nil && !active {
			return service.ErrOfficeConflict
		}
		if err != nil {
			return err
		}
	}
	if err = officeResourceQuota(ctx, tx, userID, bytes, 0); err != nil {
		return err
	}
	if err = upload(); err != nil {
		return err
	}
	_, err = tx.ExecContext(ctx, `UPDATE office_resource_versions SET preview_key=$1,preview_kind='pdf',
		preview_mime='application/pdf',preview_bytes=$2,preview_sha256=$3,updated_at=NOW() WHERE user_id=$4 AND id=$5`, key, bytes, hash, userID, versionID)
	if err != nil {
		return err
	}
	return tx.Commit()
}

// SyncGallery shares the gallery's user lock with deletion and cleanup. Pins
// become visible atomically with adoption; no image object or bytes are copied.
func (r *officeResourcesRepository) SyncGallery(ctx context.Context, userID int64) error {
	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback() }()
	if err = galleryLockUser(ctx, tx, userID); err != nil {
		return err
	}
	if err = galleryRequireActiveUser(ctx, tx, userID); err != nil {
		return err
	}
	rows, err := tx.QueryContext(ctx, `SELECT image_id,sha256,mime_type,byte_size,storage_endpoint,storage_region,storage_bucket,original_key,thumbnail_key,thumbnail_byte_size,created_at FROM image_studio_gallery_assets a WHERE user_id=$1 AND state='ready' AND NOT EXISTS(SELECT 1 FROM office_resources r WHERE r.user_id=a.user_id AND r.gallery_image_id=a.image_id) ORDER BY a.created_at LIMIT 500`, userID)
	if err != nil {
		return err
	}
	var items []service.OfficeResource
	for rows.Next() {
		var v service.OfficeResource
		if err = rows.Scan(&v.GalleryImageID, &v.SHA256, &v.MIME, &v.Bytes, &v.Endpoint, &v.Region, &v.Bucket, &v.ObjectKey, &v.PreviewKey, &v.PreviewBytes, &v.CreatedAt); err != nil {
			_ = rows.Close()
			return err
		}
		v.UserID = userID
		items = append(items, v)
	}
	err = rows.Err()
	_ = rows.Close()
	if err != nil {
		return err
	}
	for _, v := range items {
		v.ID = uuid.NewSHA1(uuid.NameSpaceOID, []byte(fmt.Sprintf("office-gallery:%d:%s", userID, v.GalleryImageID))).String()
		v.VersionID = uuid.NewSHA1(uuid.NameSpaceOID, []byte(v.ID+":original")).String()
		ext := ".png"
		if v.MIME == "image/jpeg" {
			ext = ".jpg"
		}
		if v.MIME == "image/webp" {
			ext = ".webp"
		}
		if v.MIME == "image/gif" {
			ext = ".gif"
		}
		_, err = tx.ExecContext(ctx, `INSERT INTO office_resources(id,user_id,source,gallery_image_id,latest_version_id,created_at) VALUES($1,$2,'gallery',$3,$4,$5)`, v.ID, userID, v.GalleryImageID, v.VersionID, v.CreatedAt)
		if err != nil {
			return officeDBError(err)
		}
		_, err = tx.ExecContext(ctx, `INSERT INTO office_resource_versions(id,user_id,resource_id,publication_id,name,mime,byte_size,sha256,state,storage_endpoint,storage_region,storage_bucket,object_key,preview_key,preview_mime,preview_bytes,preview_kind,created_at) VALUES($1,$2,$3,$4,$5,$6,$7,$8,'ready',$9,$10,$11,$12,$13,'image/jpeg',$14,'image',$15)`, v.VersionID, userID, v.ID, "gallery:"+v.ID, "图片-"+v.GalleryImageID+ext, v.MIME, v.Bytes, v.SHA256, v.Endpoint, v.Region, v.Bucket, v.ObjectKey, v.PreviewKey, v.PreviewBytes, v.CreatedAt)
		if err != nil {
			return officeDBError(err)
		}
		_, err = tx.ExecContext(ctx, `INSERT INTO office_gallery_pins(user_id,image_id,resource_id) VALUES($1,$2,$3)`, userID, v.GalleryImageID, v.ID)
		if err != nil {
			return err
		}
	}
	return tx.Commit()
}

type officeResourceCursor struct {
	At time.Time `json:"at"`
	ID string    `json:"id"`
}

func (r *officeResourcesRepository) ListResources(ctx context.Context, userID int64, cursor string, limit int) ([]service.OfficeResource, string, error) {
	limit = officeLimit(limit)
	var c officeResourceCursor
	if cursor != "" {
		b, e := base64.RawURLEncoding.DecodeString(cursor)
		if e != nil || len(b) > 256 || json.Unmarshal(b, &c) != nil || !officeUUID(c.ID) {
			return nil, "", service.ErrOfficeInvalid
		}
	}
	rows, err := r.db.QueryContext(ctx, `SELECT `+officeResourceColumns+` FROM office_resources r JOIN office_resource_versions v ON v.id=r.latest_version_id WHERE r.user_id=$1 AND r.deleted_at IS NULL AND v.state='ready' AND ($2::timestamptz IS NULL OR (r.created_at,r.id)<($2,$3::uuid)) ORDER BY r.created_at DESC,r.id DESC LIMIT $4`, userID, officeNullableTime(c.At), officeNullableUUID(c.ID), limit+1)
	if err != nil {
		return nil, "", err
	}
	defer func() { _ = rows.Close() }()
	result := []service.OfficeResource{}
	for rows.Next() {
		v, e := scanOfficeResource(rows)
		if e != nil {
			return nil, "", e
		}
		result = append(result, *v)
	}
	if err = rows.Err(); err != nil {
		return nil, "", err
	}
	next := ""
	if len(result) > limit {
		result = result[:limit]
		last := result[limit-1]
		var at time.Time
		err = r.db.QueryRowContext(ctx, `SELECT created_at FROM office_resources WHERE id=$1`, last.ID).Scan(&at)
		if err != nil {
			return nil, "", err
		}
		b, _ := json.Marshal(officeResourceCursor{At: at, ID: last.ID})
		next = base64.RawURLEncoding.EncodeToString(b)
	}
	return result, next, nil
}

func officeNullableTime(t time.Time) any {
	if t.IsZero() {
		return nil
	}
	return t
}
func officeNullableUUID(v string) any {
	if v == "" {
		return nil
	}
	return v
}

func (r *officeResourcesRepository) GetResourceVersion(ctx context.Context, userID int64, id string, retained bool) (*service.OfficeResource, error) {
	if !officeUUID(id) {
		return nil, service.ErrOfficeNotFound
	}
	return scanOfficeResource(r.db.QueryRowContext(ctx, `SELECT `+officeResourceColumns+` FROM office_resources r JOIN office_resource_versions v ON v.resource_id=r.id WHERE v.user_id=$1 AND v.id=$2 AND v.state='ready' AND (r.deleted_at IS NULL OR ($3 AND EXISTS(SELECT 1 FROM office_resource_refs f JOIN office_agent_conversations c ON c.id=f.conversation_id WHERE f.user_id=$1 AND f.version_id=v.id AND c.deleted_at IS NULL)))`, userID, id, retained))
}

func (r *officeResourcesRepository) ResolveVersions(ctx context.Context, userID int64, ids []string) ([]service.OfficeResourceVersion, error) {
	if len(ids) > service.OfficeWorkspaceMaxFiles {
		return nil, service.ErrOfficeResourceTooLarge
	}
	refs := []service.OfficeResourceVersion{}
	seen := map[string]bool{}
	var total int64
	for _, id := range ids {
		if seen[id] {
			return nil, service.ErrOfficeInvalid
		}
		seen[id] = true
		v, err := r.GetResourceVersion(ctx, userID, id, false)
		if err != nil {
			return nil, err
		}
		total += v.Bytes
		if total > service.OfficeResourceMaxBytes {
			return nil, service.ErrOfficeResourceTooLarge
		}
		refs = append(refs, service.OfficeResourceVersion{ResourceID: v.ID, VersionID: v.VersionID})
	}
	return refs, nil
}

func validOfficeResource(v service.OfficeResource) bool {
	return v.UserID > 0 && officeUUID(v.ID) && officeUUID(v.VersionID) && officeToken.MatchString(v.PublicationID) && officeHash.MatchString(v.SHA256) && v.Bytes > 0 && v.Bytes <= service.OfficeResourceMaxBytes && v.PreviewBytes >= 0 && v.PreviewBytes <= 8<<20 && v.Bucket != "" && strings.HasPrefix(v.ObjectKey, "workspace/v1/") && len(v.Name) > 0 && len(v.Name) <= 240 && utf8.ValidString(v.Name) && (v.Source == "upload" || v.Source == "generated") && (v.ParentVersionID == "" || officeUUID(v.ParentVersionID))
}

func (r *officeResourcesRepository) ReserveVersion(ctx context.Context, in service.OfficeResource, lease *service.OfficeRunLease) (*service.OfficeResource, error) {
	if !validOfficeResource(in) {
		return nil, service.ErrOfficeInvalid
	}
	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return nil, err
	}
	defer func() { _ = tx.Rollback() }()
	if lease != nil {
		if in.UserID != lease.Run.UserID || in.RunID != lease.Run.ID {
			return nil, service.ErrOfficeInvalid
		}
		if _, err = checkOfficeLease(ctx, tx, *lease); err != nil {
			return nil, err
		}
	}
	if err = galleryLockUser(ctx, tx, in.UserID); err != nil {
		return nil, err
	}
	if err = galleryRequireActiveUser(ctx, tx, in.UserID); err != nil {
		return nil, err
	}
	old, e := scanOfficeResource(tx.QueryRowContext(ctx, `SELECT `+officeResourceColumns+` FROM office_resource_versions v JOIN office_resources r ON r.id=v.resource_id WHERE v.user_id=$1 AND v.publication_id=$2 AND r.deleted_at IS NULL`, in.UserID, in.PublicationID))
	if e == nil {
		if old.SHA256 != in.SHA256 || old.Name != in.Name || old.Bytes != in.Bytes || old.MIME != in.MIME || old.RunID != in.RunID || old.ParentVersionID != in.ParentVersionID || old.PreviewBytes != in.PreviewBytes || old.PreviewSHA256 != in.PreviewSHA256 || old.PreviewMIME != in.PreviewMIME || old.State == "deleting" || (in.ParentVersionID != "" && in.ID != old.ID) {
			return nil, service.ErrOfficeConflict
		}
		return old, tx.Commit()
	}
	if !errors.Is(e, service.ErrOfficeNotFound) {
		return nil, e
	}
	newResource := in.ParentVersionID == ""
	if newResource {
		if err = officeResourceQuota(ctx, tx, in.UserID, in.Bytes+in.PreviewBytes, 1); err != nil {
			return nil, err
		}
		_, err = tx.ExecContext(ctx, `INSERT INTO office_resources(id,user_id,source) VALUES($1,$2,$3)`, in.ID, in.UserID, in.Source)
	} else {
		var latest string
		err = tx.QueryRowContext(ctx, `SELECT COALESCE(latest_version_id::text,'') FROM office_resources WHERE user_id=$1 AND id=$2 AND deleted_at IS NULL AND source<>'gallery' FOR UPDATE`, in.UserID, in.ID).Scan(&latest)
		if err != nil {
			return nil, officeDBError(err)
		}
		if latest != in.ParentVersionID {
			return nil, service.ErrOfficeConflict
		}
		if err = officeResourceQuota(ctx, tx, in.UserID, in.Bytes+in.PreviewBytes, 0); err != nil {
			return nil, err
		}
	}
	if err != nil {
		return nil, officeDBError(err)
	}
	_, err = tx.ExecContext(ctx, `INSERT INTO office_resource_versions(id,user_id,resource_id,parent_version_id,run_id,publication_id,name,mime,byte_size,sha256,state,storage_endpoint,storage_region,storage_bucket,object_key,preview_key,preview_mime,preview_bytes,preview_kind,preview_sha256) VALUES($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,'pending',$11,$12,$13,$14,$15,$16,$17,$18,$19)`, in.VersionID, in.UserID, in.ID, officeNullableUUID(in.ParentVersionID), officeNullableUUID(in.RunID), in.PublicationID, in.Name, in.MIME, in.Bytes, in.SHA256, in.Endpoint, in.Region, in.Bucket, in.ObjectKey, in.PreviewKey, in.PreviewMIME, in.PreviewBytes, in.PreviewKind, in.PreviewSHA256)
	if err != nil {
		return nil, officeDBError(err)
	}
	in.State = "pending"
	return &in, tx.Commit()
}

func (r *officeResourcesRepository) FinishVersion(ctx context.Context, userID int64, id string, lease *service.OfficeRunLease) (*service.OfficeResource, error) {
	if !officeUUID(id) {
		return nil, service.ErrOfficeInvalid
	}
	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return nil, err
	}
	defer func() { _ = tx.Rollback() }()
	var run *service.OfficeRun
	if lease != nil {
		run, err = checkOfficeLease(ctx, tx, *lease)
		if err != nil {
			return nil, err
		}
		if userID != run.UserID {
			return nil, service.ErrOfficeInvalid
		}
	}
	if err = galleryLockUser(ctx, tx, userID); err != nil {
		return nil, err
	}
	v, err := scanOfficeResource(tx.QueryRowContext(ctx, `SELECT `+officeResourceColumns+` FROM office_resources r JOIN office_resource_versions v ON v.resource_id=r.id WHERE v.user_id=$1 AND v.id=$2 AND r.deleted_at IS NULL FOR UPDATE OF r,v`, userID, id))
	if err != nil {
		return nil, err
	}
	if v.State == "deleting" || (lease != nil && v.RunID != run.ID) {
		return nil, service.ErrOfficeConflict
	}
	if v.State == "ready" {
		return v, tx.Commit()
	}
	if v.ParentVersionID != "" {
		var latest string
		err = tx.QueryRowContext(ctx, `SELECT COALESCE(latest_version_id::text,'') FROM office_resources WHERE id=$1`, v.ID).Scan(&latest)
		if err != nil {
			return nil, err
		}
		if latest != v.ParentVersionID {
			return nil, service.ErrOfficeConflict
		}
	}
	_, err = tx.ExecContext(ctx, `UPDATE office_resource_versions SET state='ready',updated_at=NOW() WHERE id=$1;`, id)
	if err != nil {
		return nil, err
	}
	_, err = tx.ExecContext(ctx, `UPDATE office_resources SET latest_version_id=$1 WHERE id=$2`, id, v.ID)
	if err != nil {
		return nil, err
	}
	if run != nil {
		_, err = tx.ExecContext(ctx, `INSERT INTO office_resource_refs(user_id,version_id,conversation_id) VALUES($1,$2,$3) ON CONFLICT DO NOTHING`, userID, id, run.ConversationID)
		if err != nil {
			return nil, err
		}
		// Archival and its public event commit together. A later model failure
		// must not hide an already archived file, and a ready replay above
		// must not append a duplicate event. Internal storage fields are
		// excluded by OfficeResource's JSON contract.
		payload, marshalErr := json.Marshal(map[string]any{"artifact": v})
		if marshalErr != nil {
			return nil, marshalErr
		}
		if _, err = appendOfficeEvent(ctx, tx, userID, run.ID, "system:artifact:"+id, "artifact.published", payload); err != nil {
			return nil, err
		}
	}
	v.State = "ready"
	return v, tx.Commit()
}

// officePinRunInputs is called inside run admission's transaction. The shared
// advisory lock makes deletion vs admission deterministic, with no TOCTOU gap.
func officePinRunInputs(ctx context.Context, tx *sql.Tx, run service.OfficeRun) error {
	if len(run.InputResources) == 0 {
		return nil
	}
	if err := galleryLockUser(ctx, tx, run.UserID); err != nil {
		return err
	}
	var total int64
	for _, ref := range run.InputResources {
		var bytes int64
		err := tx.QueryRowContext(ctx, `SELECT v.byte_size FROM office_resource_versions v JOIN office_resources r ON r.id=v.resource_id WHERE v.user_id=$1 AND v.id=$2 AND v.resource_id=$3 AND v.state='ready' AND r.deleted_at IS NULL FOR SHARE OF r,v`, run.UserID, ref.VersionID, ref.ResourceID).Scan(&bytes)
		if err != nil {
			return officeDBError(err)
		}
		total += bytes
		if total > service.OfficeResourceMaxBytes {
			return service.ErrOfficeResourceTooLarge
		}
		_, err = tx.ExecContext(ctx, `INSERT INTO office_resource_refs(user_id,version_id,conversation_id) VALUES($1,$2,$3) ON CONFLICT DO NOTHING`, run.UserID, ref.VersionID, run.ConversationID)
		if err != nil {
			return err
		}
	}
	return nil
}

func (r *officeResourcesRepository) PinRunInputs(ctx context.Context, run service.OfficeRun) error {
	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback() }()
	if err = officePinRunInputs(ctx, tx, run); err != nil {
		return err
	}
	return tx.Commit()
}

func (r *officeResourcesRepository) DeleteResource(ctx context.Context, userID int64, id string) error {
	if !officeUUID(id) {
		return service.ErrOfficeNotFound
	}
	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback() }()
	if err = galleryLockUser(ctx, tx, userID); err != nil {
		return err
	}
	result, err := tx.ExecContext(ctx, `UPDATE office_resources SET deleted_at=COALESCE(deleted_at,NOW()) WHERE user_id=$1 AND id=$2`, userID, id)
	if err != nil {
		return err
	}
	n, _ := result.RowsAffected()
	if n == 0 {
		return service.ErrOfficeNotFound
	}
	return tx.Commit()
}

const officeSnapshotColumns = `id::text,user_id,run_id::text,operation_id,version,sha256,byte_size,state,storage_endpoint,storage_region,storage_bucket,object_key`

func scanOfficeSnapshot(row officeScanner) (*service.OfficeSnapshot, error) {
	s := &service.OfficeSnapshot{}
	err := row.Scan(&s.ID, &s.UserID, &s.RunID, &s.OperationID, &s.Version, &s.Hash, &s.Bytes, &s.State, &s.Endpoint, &s.Region, &s.Bucket, &s.ObjectKey)
	return s, officeDBError(err)
}

func (r *officeResourcesRepository) LatestSnapshot(ctx context.Context, lease service.OfficeRunLease, version int64) (*service.OfficeSnapshot, error) {
	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return nil, err
	}
	defer func() { _ = tx.Rollback() }()
	if _, err = checkOfficeLease(ctx, tx, lease); err != nil {
		return nil, err
	}
	v, err := scanOfficeSnapshot(tx.QueryRowContext(ctx, `SELECT `+officeSnapshotColumns+` FROM office_workspace_snapshots WHERE user_id=$1 AND run_id=$2 AND state='ready' AND ($3=0 OR version=$3) ORDER BY version DESC LIMIT 1`, lease.Run.UserID, lease.Run.ID, version))
	if err != nil {
		return nil, err
	}
	return v, tx.Commit()
}

func (r *officeResourcesRepository) ReserveSnapshot(ctx context.Context, lease service.OfficeRunLease, in service.OfficeSnapshot, expected int64) (*service.OfficeSnapshot, error) {
	if !officeUUID(in.ID) || !officeToken.MatchString(in.OperationID) || !officeHash.MatchString(in.Hash) || in.Bytes <= 0 || in.Bytes > 40<<20 || expected < 0 || in.UserID != lease.Run.UserID || in.RunID != lease.Run.ID || !strings.HasPrefix(in.ObjectKey, "workspace/v1/staging/") {
		return nil, service.ErrOfficeInvalid
	}
	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return nil, err
	}
	defer func() { _ = tx.Rollback() }()
	if _, err = checkOfficeLease(ctx, tx, lease); err != nil {
		return nil, err
	}
	if err = galleryLockUser(ctx, tx, in.UserID); err != nil {
		return nil, err
	}
	old, e := scanOfficeSnapshot(tx.QueryRowContext(ctx, `SELECT `+officeSnapshotColumns+` FROM office_workspace_snapshots WHERE run_id=$1 AND operation_id=$2`, in.RunID, in.OperationID))
	if e == nil {
		if old.Hash != in.Hash || old.Version != expected+1 || old.State == "deleting" {
			return nil, service.ErrOfficeConflict
		}
		return old, tx.Commit()
	}
	if !errors.Is(e, service.ErrOfficeNotFound) {
		return nil, e
	}
	var latest int64
	var pending bool
	err = tx.QueryRowContext(ctx, `SELECT COALESCE(MAX(version) FILTER(WHERE state='ready'),0),COALESCE(BOOL_OR(state='pending'),false) FROM office_workspace_snapshots WHERE run_id=$1`, in.RunID).Scan(&latest, &pending)
	if err != nil {
		return nil, err
	}
	if latest != expected || pending {
		return nil, service.ErrOfficeConflict
	}
	if err = officeResourceQuota(ctx, tx, in.UserID, in.Bytes, 0); err != nil {
		return nil, err
	}
	in.Version = expected + 1
	in.State = "pending"
	_, err = tx.ExecContext(ctx, `INSERT INTO office_workspace_snapshots(id,user_id,run_id,operation_id,version,sha256,byte_size,state,storage_endpoint,storage_region,storage_bucket,object_key) VALUES($1,$2,$3,$4,$5,$6,$7,'pending',$8,$9,$10,$11)`, in.ID, in.UserID, in.RunID, in.OperationID, in.Version, in.Hash, in.Bytes, in.Endpoint, in.Region, in.Bucket, in.ObjectKey)
	if err != nil {
		return nil, officeDBError(err)
	}
	return &in, tx.Commit()
}

func (r *officeResourcesRepository) FinishSnapshot(ctx context.Context, lease service.OfficeRunLease, id string) (*service.OfficeSnapshot, error) {
	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return nil, err
	}
	defer func() { _ = tx.Rollback() }()
	if _, err = checkOfficeLease(ctx, tx, lease); err != nil {
		return nil, err
	}
	v, err := scanOfficeSnapshot(tx.QueryRowContext(ctx, `UPDATE office_workspace_snapshots SET state='ready',updated_at=NOW() WHERE id=$1 AND user_id=$2 AND run_id=$3 AND state IN('ready','pending') RETURNING `+officeSnapshotColumns, id, lease.Run.UserID, lease.Run.ID))
	if err != nil {
		return nil, err
	}
	return v, tx.Commit()
}

func (r *officeResourcesRepository) HasStorage(ctx context.Context) (bool, error) {
	var found bool
	err := r.db.QueryRowContext(ctx, `SELECT EXISTS(SELECT 1 FROM office_resource_versions v JOIN office_resources r ON r.id=v.resource_id WHERE r.source<>'gallery' UNION ALL SELECT 1 FROM office_workspace_snapshots)`).Scan(&found)
	return found, err
}
