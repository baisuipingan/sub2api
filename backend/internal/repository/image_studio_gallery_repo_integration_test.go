//go:build integration

package repository

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/stretchr/testify/require"
)

func galleryTestUser(t *testing.T) int64 {
	t.Helper()
	var userID int64
	err := integrationDB.QueryRowContext(context.Background(),
		`INSERT INTO users(email,password_hash) VALUES($1,'gallery-test') RETURNING id`,
		fmt.Sprintf("gallery-%d@example.test", time.Now().UnixNano())).Scan(&userID)
	require.NoError(t, err)
	t.Cleanup(func() {
		ctx := context.Background()
		_, _ = integrationDB.ExecContext(ctx, `DELETE FROM image_studio_gallery_tasks WHERE user_id=$1`, userID)
		_, _ = integrationDB.ExecContext(ctx, `DELETE FROM image_studio_gallery_assets WHERE user_id=$1`, userID)
		_, _ = integrationDB.ExecContext(ctx, `DELETE FROM users WHERE id=$1`, userID)
	})
	return userID
}

func galleryTestAsset(userID int64, id string) service.GalleryAsset {
	return service.GalleryAsset{
		UserID: userID, ImageID: id, SHA256: strings.Repeat("a", 64), MIME: "image/png", Bytes: 100, Width: 10, Height: 10,
		PreviewBytes: 50, PreviewWidth: 10, PreviewHeight: 10, ThumbnailBytes: 20, ThumbnailWidth: 10, ThumbnailHeight: 10,
		Endpoint: "https://s3.test", Region: "auto", Bucket: "gallery", OriginalKey: id + "/original.png", PreviewKey: id + "/preview.png", ThumbnailKey: id + "/thumbnail.png",
	}
}

func TestImageStudioGalleryRepositoryLifecycleAndSoftDeletedUser(t *testing.T) {
	ctx := context.Background()
	userID := galleryTestUser(t)
	repo := NewImageStudioGalleryRepository(integrationDB)
	asset, err := repo.ReserveAsset(ctx, galleryTestAsset(userID, "image-one"))
	require.NoError(t, err)
	require.Equal(t, "uploading", asset.State)
	require.NoError(t, repo.MarkAssetReady(ctx, userID, "image-one"))
	asset, err = repo.GetAsset(ctx, userID, "image-one")
	require.NoError(t, err)
	require.Equal(t, "ready", asset.State)
	_, err = repo.GetAsset(ctx, userID+999, "image-one")
	require.ErrorIs(t, err, service.ErrGalleryNotFound)

	task := json.RawMessage(`{"id":"task-one","status":"done","outputImages":["image-one"]}`)
	stored, err := repo.SaveTask(ctx, userID, "task-one", strings.Repeat("b", 64), task, []service.GalleryTaskLink{{ImageID: "image-one", Role: "output", Ordinal: 0}})
	require.NoError(t, err)
	require.JSONEq(t, string(task), string(stored))
	stored, err = repo.SaveTask(ctx, userID, "task-one", strings.Repeat("b", 64), task, nil)
	require.NoError(t, err)
	require.JSONEq(t, string(task), string(stored))
	_, err = repo.SaveTask(ctx, userID, "task-one", strings.Repeat("c", 64), task, nil)
	require.ErrorIs(t, err, service.ErrGalleryConflict)
	page, err := repo.ListTasks(ctx, userID, "", 20)
	require.NoError(t, err)
	require.Len(t, page.Items, 1)
	require.Nil(t, page.NextCursor)
	got, err := repo.GetTask(ctx, userID, "task-one")
	require.NoError(t, err)
	require.JSONEq(t, string(task), string(got))

	_, err = integrationDB.ExecContext(ctx, `UPDATE users SET deleted_at=NOW() WHERE id=$1`, userID)
	require.NoError(t, err)
	require.NoError(t, repo.MarkDeletedUsersForCleanup(ctx, 20))
	_, err = repo.GetTask(ctx, userID, "task-one")
	require.ErrorIs(t, err, service.ErrGalleryNotFound)
	toDelete, err := repo.ListCleanupAssets(ctx, 100)
	require.NoError(t, err)
	found := false
	for _, item := range toDelete {
		if item.UserID == userID && item.ImageID == "image-one" {
			found = true
		}
	}
	require.True(t, found)
	require.NoError(t, repo.FinalizeAssetDelete(ctx, userID, "image-one"))
	_, err = repo.GetAsset(ctx, userID, "image-one")
	require.True(t, errors.Is(err, service.ErrGalleryNotFound))
}

func TestImageStudioGalleryRepositoryQuotaAndTargetLock(t *testing.T) {
	ctx := context.Background()
	userID := galleryTestUser(t)
	repo := NewImageStudioGalleryRepository(integrationDB)
	_, err := repo.ReserveAsset(ctx, galleryTestAsset(userID, "first"))
	require.NoError(t, err)
	_, err = integrationDB.ExecContext(ctx, `UPDATE image_studio_gallery_assets SET preview_byte_size=$1 WHERE user_id=$2`, int64(1<<30), userID)
	require.NoError(t, err)
	_, err = repo.ReserveAsset(ctx, galleryTestAsset(userID, "second"))
	require.ErrorIs(t, err, service.ErrGalleryQuota)

	release, err := repo.LockTarget(ctx)
	require.NoError(t, err)
	defer release()
	blockedCtx, cancel := context.WithTimeout(ctx, 80*time.Millisecond)
	defer cancel()
	_, err = repo.LockTarget(blockedCtx)
	require.Error(t, err, "a second target configuration change must wait for the active lock")
}

func TestImageStudioGalleryRepositoryConcurrentByteQuota(t *testing.T) {
	ctx := context.Background()
	userID := galleryTestUser(t)
	repo := NewImageStudioGalleryRepository(integrationDB)
	_, err := repo.ReserveAsset(ctx, galleryTestAsset(userID, "base"))
	require.NoError(t, err)
	// The existing row uses all but 200 bytes; each competing image needs 170.
	_, err = integrationDB.ExecContext(ctx, `UPDATE image_studio_gallery_assets
        SET preview_byte_size=$1 WHERE user_id=$2 AND image_id='base'`,
		int64(1<<30)-320, userID)
	require.NoError(t, err)
	var wg sync.WaitGroup
	results := make(chan error, 2)
	for _, id := range []string{"first", "second"} {
		wg.Add(1)
		go func(id string) {
			defer wg.Done()
			_, err := repo.ReserveAsset(ctx, galleryTestAsset(userID, id))
			results <- err
		}(id)
	}
	wg.Wait()
	close(results)
	success, quota := 0, 0
	for err := range results {
		if err == nil {
			success++
		} else if errors.Is(err, service.ErrGalleryQuota) {
			quota++
		} else {
			t.Errorf("unexpected reserve error: %v", err)
		}
	}
	require.Equal(t, 1, success)
	require.Equal(t, 1, quota)
}

func TestImageStudioGalleryRepositoryTaskCountQuota(t *testing.T) {
	ctx := context.Background()
	userID := galleryTestUser(t)
	repo := NewImageStudioGalleryRepository(integrationDB)
	_, err := integrationDB.ExecContext(ctx, `INSERT INTO image_studio_gallery_tasks(user_id,task_id,task_json,sha256)
        SELECT $1,'task-'||n,'{}'::jsonb,repeat('b',64) FROM generate_series(1,1000) AS n`, userID)
	require.NoError(t, err)
	_, err = repo.SaveTask(ctx, userID, "extra", strings.Repeat("c", 64), json.RawMessage(`{"id":"extra","status":"error"}`), nil)
	require.ErrorIs(t, err, service.ErrGalleryQuota)
}

func TestImageStudioGalleryRepositoryCursorPagination(t *testing.T) {
	ctx := context.Background()
	userID := galleryTestUser(t)
	repo := NewImageStudioGalleryRepository(integrationDB)
	for _, id := range []string{"older", "newer"} {
		task := json.RawMessage(fmt.Sprintf(`{"id":%q,"status":"error"}`, id))
		_, err := repo.SaveTask(ctx, userID, id, strings.Repeat("c", 64), task, nil)
		require.NoError(t, err)
	}
	first, err := repo.ListTasks(ctx, userID, "", 1)
	require.NoError(t, err)
	require.Len(t, first.Items, 1)
	require.NotNil(t, first.NextCursor)
	second, err := repo.ListTasks(ctx, userID, *first.NextCursor, 1)
	require.NoError(t, err)
	require.Len(t, second.Items, 1)
	require.Nil(t, second.NextCursor)
	require.NotEqual(t, string(first.Items[0]), string(second.Items[0]))
}

func TestImageStudioGalleryRepositoryDeletedTaskCannotResurrect(t *testing.T) {
	ctx := context.Background()
	userID := galleryTestUser(t)
	repo := NewImageStudioGalleryRepository(integrationDB)
	_, err := repo.ReserveAsset(ctx, galleryTestAsset(userID, "image-one"))
	require.NoError(t, err)
	require.NoError(t, repo.MarkAssetReady(ctx, userID, "image-one"))
	task := json.RawMessage(`{"id":"task-one","status":"done","outputImages":["image-one"]}`)
	links := []service.GalleryTaskLink{{ImageID: "image-one", Role: "output", Ordinal: 0}}
	_, err = repo.SaveTask(ctx, userID, "task-one", strings.Repeat("a", 64), task, links)
	require.NoError(t, err)
	objects, err := repo.DeleteTask(ctx, userID, "task-one")
	require.NoError(t, err)
	require.Len(t, objects, 1)
	_, err = repo.GetTask(ctx, userID, "task-one")
	require.ErrorIs(t, err, service.ErrGalleryNotFound)
	_, err = repo.SaveTask(ctx, userID, "task-one", strings.Repeat("a", 64), task, links)
	require.ErrorIs(t, err, service.ErrGalleryGone)
	_, err = repo.SaveTask(ctx, userID, "task-one", strings.Repeat("b", 64), task, links)
	require.ErrorIs(t, err, service.ErrGalleryGone)
	_, err = repo.DeleteTask(ctx, userID, "task-one")
	require.NoError(t, err, "repeat delete remains idempotent")
	var deletedAt time.Time
	var stored []byte
	err = integrationDB.QueryRowContext(ctx, `SELECT deleted_at,task_json FROM image_studio_gallery_tasks
        WHERE user_id=$1 AND task_id='task-one'`, userID).Scan(&deletedAt, &stored)
	require.NoError(t, err)
	require.JSONEq(t, `{}`, string(stored), "tombstone must discard prompt and task metadata")
}
