//go:build integration

package repository

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"slices"
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

func TestImageStudioGalleryStatsAreUserScopedAndExcludeDeleted(t *testing.T) {
	ctx := context.Background()
	userID := galleryTestUser(t)
	otherUserID := galleryTestUser(t)
	repo := NewImageStudioGalleryRepository(integrationDB)
	asset := galleryTestAsset(userID, "output-one")
	_, err := repo.ReserveAsset(ctx, asset)
	require.NoError(t, err)
	require.NoError(t, repo.MarkAssetReady(ctx, userID, asset.ImageID))
	_, err = repo.SaveTask(ctx, userID, "done-one", strings.Repeat("d", 64),
		json.RawMessage(`{"id":"done-one","status":"done","outputImages":["output-one"]}`),
		[]service.GalleryTaskLink{{ImageID: asset.ImageID, Role: "output", Ordinal: 0}})
	require.NoError(t, err)
	_, err = repo.SaveTask(ctx, userID, "error-one", strings.Repeat("e", 64),
		json.RawMessage(`{"id":"error-one","status":"error"}`), nil)
	require.NoError(t, err)
	_, err = repo.SaveTask(ctx, otherUserID, "other", strings.Repeat("f", 64),
		json.RawMessage(`{"id":"other","status":"error"}`), nil)
	require.NoError(t, err)

	stats, err := repo.Stats(ctx, userID)
	require.NoError(t, err)
	require.Equal(t, service.GalleryStats{
		Tasks: 2, DoneTasks: 1, FailedTasks: 1, OutputImages: 1,
		Assets: 1, UsedBytes: 170, MaxTasks: 1000, MaxAssets: 500, MaxBytes: 1 << 30,
	}, stats)
	_, err = repo.DeleteTask(ctx, userID, "done-one")
	require.NoError(t, err)
	stats, err = repo.Stats(ctx, userID)
	require.NoError(t, err)
	require.Equal(t, int64(1), stats.Tasks)
	require.Equal(t, int64(0), stats.OutputImages)
	// Deleting objects still occupy the quota until S3 cleanup finishes.
	require.Equal(t, int64(1), stats.Assets)
	require.Equal(t, int64(170), stats.UsedBytes)
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
	_, err = repo.SaveTask(ctx, userID, "late-task", strings.Repeat("e", 64), json.RawMessage(`{"id":"late-task","status":"error"}`), nil)
	require.ErrorIs(t, err, service.ErrGalleryGone)
	_, err = repo.ReserveAsset(ctx, galleryTestAsset(userID, "late-asset"))
	require.ErrorIs(t, err, service.ErrGalleryGone)
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

func TestImageStudioSoftDeletedUserCancelsJobsAndReleasesInputs(t *testing.T) {
	ctx := context.Background()
	userID := galleryTestUser(t)
	gallery := NewImageStudioGalleryRepository(integrationDB)
	asset := galleryTestAsset(userID, "pending-input")
	_, err := gallery.ReserveAsset(ctx, asset)
	require.NoError(t, err)
	require.NoError(t, gallery.MarkAssetReady(ctx, userID, asset.ImageID))
	var keyID int64
	err = integrationDB.QueryRowContext(ctx, `INSERT INTO api_keys
        (user_id,key,name,status,quota,key_type,created_at,updated_at)
        VALUES ($1,$2,$3,'active',0,'system',NOW(),NOW()) RETURNING id`,
		userID, fmt.Sprintf("studio-key-%d", userID), "studio-test").Scan(&keyID)
	require.NoError(t, err)
	t.Cleanup(func() {
		_, _ = integrationDB.ExecContext(ctx, `DELETE FROM image_studio_jobs WHERE user_id=$1`, userID)
		_, _ = integrationDB.ExecContext(ctx, `DELETE FROM api_keys WHERE id=$1`, keyID)
	})
	jobs := NewStudioJobRepository(integrationDB)
	_, err = jobs.Create(ctx, service.StudioJobCreate{
		UserID: userID, APIKeyID: keyID, TaskID: "deleted-user-job", Digest: strings.Repeat("a", 64),
		Task:      json.RawMessage(`{"id":"deleted-user-job","status":"running"}`),
		Spec:      service.StudioJobSpec{Protocol: "openai", Model: "gpt-image-2", InputImageIDs: []string{asset.ImageID}, ImageCount: 1},
		CallCount: 1, StorageEndpoint: asset.Endpoint, StorageRegion: asset.Region, StorageBucket: asset.Bucket,
	})
	require.NoError(t, err)
	_, err = integrationDB.ExecContext(ctx, `UPDATE users SET deleted_at=NOW() WHERE id=$1`, userID)
	require.NoError(t, err)
	require.NoError(t, gallery.MarkDeletedUsersForCleanup(ctx, 20))
	job, err := jobs.Get(ctx, userID, "deleted-user-job")
	require.NoError(t, err)
	require.Equal(t, service.StudioJobCancelled, job.Status)
	require.NotNil(t, job.DeletedAt)
	var references int
	require.NoError(t, integrationDB.QueryRowContext(ctx, `SELECT COUNT(*) FROM image_studio_job_inputs WHERE user_id=$1`, userID).Scan(&references))
	require.Zero(t, references)
	claim, err := jobs.Claim(ctx, "11111111-1111-4111-8111-111111111111")
	require.NoError(t, err)
	require.Nil(t, claim)
	toDelete, err := gallery.ListCleanupAssets(ctx, 100)
	require.NoError(t, err)
	require.True(t, slices.ContainsFunc(toDelete, func(item service.GalleryAsset) bool { return item.UserID == userID && item.ImageID == asset.ImageID }))
}

func TestStudioJobClaimRecoveryDoesNotRepeatDispatchedCall(t *testing.T) {
	ctx := context.Background()
	userID := galleryTestUser(t)
	var keyID int64
	err := integrationDB.QueryRowContext(ctx, `INSERT INTO api_keys
        (user_id,key,name,status,quota,key_type,created_at,updated_at)
        VALUES ($1,$2,$3,'active',0,'system',NOW(),NOW()) RETURNING id`,
		userID, fmt.Sprintf("claim-key-%d", userID), "claim-test").Scan(&keyID)
	require.NoError(t, err)
	t.Cleanup(func() {
		_, _ = integrationDB.ExecContext(ctx, `DELETE FROM image_studio_jobs WHERE user_id=$1`, userID)
		_, _ = integrationDB.ExecContext(ctx, `DELETE FROM api_keys WHERE id=$1`, keyID)
	})
	repo := NewStudioJobRepository(integrationDB)
	_, err = repo.Create(ctx, service.StudioJobCreate{
		UserID: userID, APIKeyID: keyID, TaskID: "claim-recovery", Digest: strings.Repeat("b", 64),
		Task:      json.RawMessage(`{"id":"claim-recovery","status":"running"}`),
		Spec:      service.StudioJobSpec{Protocol: "openai", Model: "gpt-image-2", ImageCount: 2},
		CallCount: 2, StorageEndpoint: "https://s3.test", StorageRegion: "auto", StorageBucket: "gallery",
	})
	require.NoError(t, err)
	owner := "11111111-1111-4111-8111-111111111111"
	claim, err := repo.Claim(ctx, owner)
	require.NoError(t, err)
	require.NotNil(t, claim)
	require.Equal(t, 0, claim.Call.Ordinal)
	require.Nil(t, claim.Call.SentAt)
	_, err = integrationDB.ExecContext(ctx, `UPDATE image_studio_job_calls SET lease_until=NOW()-INTERVAL '1 minute'
        WHERE user_id=$1 AND task_id=$2 AND ordinal=0`, userID, "claim-recovery")
	require.NoError(t, err)
	expired, err := repo.ExpiredGenerating(ctx, 10)
	require.NoError(t, err)
	require.Len(t, expired, 1)
	require.NoError(t, repo.RecoverUnsent(ctx, expired[0]))
	_, err = integrationDB.ExecContext(ctx, `UPDATE image_studio_job_calls
        SET updated_at=NOW()+INTERVAL '1 hour'
        WHERE user_id=$1 AND task_id=$2 AND ordinal=1`, userID, "claim-recovery")
	require.NoError(t, err)
	reclaimed, err := repo.Claim(ctx, owner)
	require.NoError(t, err)
	require.NotNil(t, reclaimed)
	require.Equal(t, 0, reclaimed.Call.Ordinal)
	require.Equal(t, 2, reclaimed.Call.Attempt)
	require.NoError(t, repo.MarkDispatched(ctx, *reclaimed))
	_, err = integrationDB.ExecContext(ctx, `UPDATE image_studio_job_calls SET lease_until=NOW()-INTERVAL '1 minute'
        WHERE user_id=$1 AND task_id=$2 AND ordinal=0`, userID, "claim-recovery")
	require.NoError(t, err)
	expired, err = repo.ExpiredGenerating(ctx, 10)
	require.NoError(t, err)
	require.Len(t, expired, 1)
	require.NotNil(t, expired[0].Call.SentAt)
	require.NoError(t, repo.MarkUnknown(ctx, expired[0]))
	job, err := repo.Get(ctx, userID, "claim-recovery")
	require.NoError(t, err)
	require.Equal(t, service.StudioJobUnknown, job.Calls[0].Status)
	require.Equal(t, service.StudioJobCancelled, job.Calls[1].Status)
	claim, err = repo.Claim(ctx, owner)
	require.NoError(t, err)
	require.Nil(t, claim)
	ready, err := repo.FinalizationCandidates(ctx, 10)
	require.NoError(t, err)
	require.True(t, slices.ContainsFunc(ready, func(item service.StudioJob) bool { return item.TaskID == job.TaskID }))
	require.NoError(t, repo.Cancel(ctx, userID, job.TaskID))
	require.NoError(t, repo.Cancel(ctx, userID, job.TaskID), "delete must be retryable after a later cleanup failure")

	_, err = repo.Create(ctx, service.StudioJobCreate{
		UserID: userID, APIKeyID: keyID, TaskID: "lost-spool", Digest: strings.Repeat("c", 64),
		Task:      json.RawMessage(`{"id":"lost-spool","status":"running"}`),
		Spec:      service.StudioJobSpec{Protocol: "grok", Model: "grok-imagine-image", ImageCount: 2},
		CallCount: 2, StorageEndpoint: "https://s3.test", StorageRegion: "auto", StorageBucket: "gallery",
	})
	require.NoError(t, err)
	claim, err = repo.Claim(ctx, owner)
	require.NoError(t, err)
	require.NotNil(t, claim)
	require.Equal(t, "lost-spool", claim.TaskID)
	require.NoError(t, repo.MarkDispatched(ctx, *claim))
	require.NoError(t, repo.MarkSaving(ctx, *claim, "/app/data/studio-jobs/lost.json", owner))
	_, err = integrationDB.ExecContext(ctx, `UPDATE image_studio_job_calls SET lease_until=NOW()-INTERVAL '2 hours'
        WHERE user_id=$1 AND task_id=$2 AND ordinal=0`, userID, "lost-spool")
	require.NoError(t, err)
	foreign, err := repo.ExpiredSavingOtherOwner(ctx, "22222222-2222-4222-8222-222222222222", 10)
	require.NoError(t, err)
	require.Len(t, foreign, 1)
	require.NoError(t, repo.MarkSavingLost(ctx, foreign[0], true))
	job, err = repo.Get(ctx, userID, "lost-spool")
	require.NoError(t, err)
	require.Equal(t, service.StudioJobUnknown, job.Calls[0].Status)
	require.Equal(t, service.StudioJobCancelled, job.Calls[1].Status)
	_, err = repo.Create(ctx, service.StudioJobCreate{
		UserID: userID, APIKeyID: keyID, TaskID: "billing-lag", Digest: strings.Repeat("d", 64),
		Task:      json.RawMessage(`{"id":"billing-lag","status":"running"}`),
		Spec:      service.StudioJobSpec{Protocol: "openai", Model: "gpt-image-2", ImageCount: 1},
		CallCount: 1, StorageEndpoint: "https://s3.test", StorageRegion: "auto", StorageBucket: "gallery",
	})
	require.NoError(t, err)
	claim, err = repo.Claim(ctx, owner)
	require.NoError(t, err)
	require.NotNil(t, claim)
	require.Equal(t, "billing-lag", claim.TaskID)
	require.NoError(t, repo.MarkDispatched(ctx, *claim))
	require.NoError(t, repo.MarkSaving(ctx, *claim, "/app/data/studio-jobs/billing.json", owner))
	require.NoError(t, repo.CompleteCall(ctx, *claim, []string{"saved-output"}, nil, "pending"))
	job, err = repo.Get(ctx, userID, "billing-lag")
	require.NoError(t, err)
	require.Equal(t, service.StudioJobSaving, job.Status, "image saved, billing still unsettled")
	ready, err = repo.FinalizationCandidates(ctx, 10)
	require.NoError(t, err)
	require.False(t, slices.ContainsFunc(ready, func(item service.StudioJob) bool { return item.TaskID == "billing-lag" }))
	require.NoError(t, repo.SetBillingState(ctx, service.StudioJobClaim{StudioJob: *job, Call: job.Calls[0]}, "recorded"))
	ready, err = repo.FinalizationCandidates(ctx, 10)
	require.NoError(t, err)
	require.True(t, slices.ContainsFunc(ready, func(item service.StudioJob) bool { return item.TaskID == "billing-lag" }))
	// Historical jobs must not block physical key deletion.
	_, err = integrationDB.ExecContext(ctx, `DELETE FROM api_keys WHERE id=$1`, keyID)
	require.NoError(t, err)
}

func TestStudioJobAdmissionReservesGalleryHeadroomBeforeBilling(t *testing.T) {
	ctx := context.Background()
	userID := galleryTestUser(t)
	var keyID int64
	err := integrationDB.QueryRowContext(ctx, `INSERT INTO api_keys
        (user_id,key,name,status,quota,key_type,created_at,updated_at)
        VALUES ($1,$2,$3,'active',0,'system',NOW(),NOW()) RETURNING id`,
		userID, fmt.Sprintf("quota-key-%d", userID), "quota-test").Scan(&keyID)
	require.NoError(t, err)
	t.Cleanup(func() {
		_, _ = integrationDB.ExecContext(ctx, `DELETE FROM image_studio_jobs WHERE user_id=$1`, userID)
		_, _ = integrationDB.ExecContext(ctx, `DELETE FROM api_keys WHERE id=$1`, keyID)
	})
	repo := NewStudioJobRepository(integrationDB)
	require.NoError(t, repo.Cancel(ctx, userID, "cancel-before-submit"))
	_, err = repo.Create(ctx, service.StudioJobCreate{
		UserID: userID, APIKeyID: keyID, TaskID: "cancel-before-submit", Digest: strings.Repeat("e", 64),
		Task:      json.RawMessage(`{"id":"cancel-before-submit","status":"running"}`),
		Spec:      service.StudioJobSpec{Protocol: "openai", Model: "gpt-image-2", ImageCount: 1},
		CallCount: 1, StorageEndpoint: "https://s3.test", StorageRegion: "auto", StorageBucket: "gallery",
	})
	require.ErrorIs(t, err, service.ErrStudioJobConflict, "a delayed POST cannot resurrect a cancelled task")
	create := func(id string, count int, postprocess string) error {
		_, err := repo.Create(ctx, service.StudioJobCreate{
			UserID: userID, APIKeyID: keyID, TaskID: id, Digest: strings.Repeat(id[:1], 64),
			Task:      json.RawMessage(fmt.Sprintf(`{"id":%q,"status":"running"}`, id)),
			Spec:      service.StudioJobSpec{Protocol: "openai", Model: "gpt-image-2", ImageCount: count, Postprocess: postprocess},
			CallCount: count, StorageEndpoint: "https://s3.test", StorageRegion: "auto", StorageBucket: "gallery",
		})
		return err
	}
	require.NoError(t, create("first", 10, "keyed")) // reserves 20 * 48 MiB
	require.NoError(t, create("second", 1, "none"))
	require.ErrorIs(t, create("third", 1, "none"), service.ErrGalleryQuota)
	gallery := NewImageStudioGalleryRepository(integrationDB)
	large := galleryTestAsset(userID, "unrelated-upload")
	large.Bytes = 20 << 20
	_, err = gallery.ReserveAsset(ctx, large)
	require.ErrorIs(t, err, service.ErrGalleryQuota, "ordinary uploads cannot consume accepted-job headroom")
	large.ImageID = "accepted-output"
	large.AcceptedJobTaskID = "first"
	_, err = gallery.ReserveAsset(ctx, large)
	require.NoError(t, err, "accepted output may use its own reservation")
	_, err = integrationDB.ExecContext(ctx, `INSERT INTO image_studio_gallery_tasks
        (user_id,task_id,task_json,sha256)
        SELECT $1,'bulk-'||n,'{}'::jsonb,repeat('f',64)
        FROM generate_series(1,998) AS n`, userID)
	require.NoError(t, err)
	_, err = gallery.SaveTask(ctx, userID, "unrelated-task", strings.Repeat("a", 64), json.RawMessage(`{"id":"unrelated-task","status":"error"}`), nil)
	require.ErrorIs(t, err, service.ErrGalleryQuota, "local sync cannot consume accepted task slots")
	_, err = gallery.SaveAcceptedTask(ctx, userID, "first", strings.Repeat("b", 64), json.RawMessage(`{"id":"first","status":"error"}`), nil)
	require.NoError(t, err, "accepted job may use its own task slot")
	var thirdRows int
	require.NoError(t, integrationDB.QueryRowContext(ctx, `SELECT COUNT(*) FROM image_studio_jobs
        WHERE user_id=$1 AND task_id='third'`, userID).Scan(&thirdRows))
	require.Zero(t, thirdRows, "a quota-full job must not be accepted or dispatched")
	// Existing gallery bytes also participate in admission, not only queued jobs.
	require.NoError(t, repo.Cancel(ctx, userID, "first"))
	require.NoError(t, repo.Cancel(ctx, userID, "second"))
	_, err = gallery.ReserveAsset(ctx, galleryTestAsset(userID, "near-full"))
	require.NoError(t, err)
	_, err = integrationDB.ExecContext(ctx, `UPDATE image_studio_gallery_assets
        SET preview_byte_size=$1 WHERE user_id=$2 AND image_id='near-full'`, int64(1<<30)-20<<20, userID)
	require.NoError(t, err)
	require.ErrorIs(t, create("fourth", 1, "none"), service.ErrGalleryQuota)
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

	previousMaxOpen := integrationDB.Stats().MaxOpenConnections
	integrationDB.SetMaxOpenConns(2)
	defer integrationDB.SetMaxOpenConns(previousMaxOpen)
	release, err := repo.LockTarget(ctx)
	require.NoError(t, err)
	released := false
	defer func() {
		if !released {
			release()
		}
	}()
	blockedCtx, cancel := context.WithTimeout(ctx, 350*time.Millisecond)
	defer cancel()
	waiting := make(chan error, 1)
	go func() {
		waitRelease, lockErr := repo.LockTarget(blockedCtx)
		if lockErr == nil {
			waitRelease()
		}
		waiting <- lockErr
	}()
	var one int
	require.NoError(t, integrationDB.QueryRowContext(ctx, "SELECT 1").Scan(&one))
	require.Equal(t, 1, one)
	release()
	released = true
	select {
	case lockErr := <-waiting:
		require.NoError(t, lockErr)
	case <-time.After(time.Second):
		t.Fatal("target-lock waiter did not acquire the lock after it was released")
	}
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
