//go:build integration

package repository

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/stretchr/testify/require"
)

func insertWorkbenchReadJob(t *testing.T, userID int64, taskID, status, childStatus, billing string, created time.Time, deleted bool, outputs []string) {
	t.Helper()
	ctx := context.Background()
	var removed any
	if deleted {
		removed = time.Now()
	}
	var finished any
	if status == "completed" || status == "partial" || status == "failed" {
		finished = created.Add(3 * time.Second)
	}
	_, err := integrationDB.ExecContext(ctx, `INSERT INTO image_studio_jobs
        (user_id,task_id,api_key_id,request_hash,task_json,request_json,protocol,model,status,storage_endpoint,storage_region,storage_bucket,created_at,updated_at,finished_at,deleted_at)
        VALUES ($1,$2,1,$3,'{"prompt":"private-prompt-marker"}'::jsonb,'{"image_count":2}'::jsonb,'openai','gpt-image-2',$4,'https://private-endpoint','auto','private-bucket',$5,$5,$6,$7)`, userID, taskID, strings.Repeat("a", 64), status, created, finished, removed)
	require.NoError(t, err)
	ids, err := json.Marshal(outputs)
	require.NoError(t, err)
	_, err = integrationDB.ExecContext(ctx, `INSERT INTO image_studio_job_calls
        (user_id,task_id,ordinal,status,billing_state,output_image_ids,error_code,error_message)
        VALUES ($1,$2,0,$3,$4,$5,'INPUT_UNAVAILABLE','access_token=private-secret-marker')`, userID, taskID, childStatus, billing, ids)
	require.NoError(t, err)
	t.Cleanup(func() {
		_, _ = integrationDB.ExecContext(ctx, `DELETE FROM image_studio_jobs WHERE user_id=$1 AND task_id=$2`, userID, taskID)
	})
}

func TestWorkbenchOverviewSeparatesCurrentQueueWindowedJobsAndGallery(t *testing.T) {
	ctx := context.Background()
	repo := NewWorkbenchAdminRepository(integrationDB)
	userID := galleryTestUser(t)
	otherID := galleryTestUser(t)
	goneID := galleryTestUser(t)
	now := time.Now().UTC()
	since := now.AddDate(0, 0, -7)
	until := now.Add(time.Minute)
	before, err := repo.Overview(ctx, since, until)
	require.NoError(t, err)
	insertWorkbenchReadJob(t, userID, "wb-completed", "completed", "completed", "recorded", now.Add(-time.Hour), false, []string{"one", "two"})
	insertWorkbenchReadJob(t, userID, "wb-partial", "partial", "completed", "unknown", now.Add(-2*time.Hour), false, []string{"three"})
	insertWorkbenchReadJob(t, otherID, "wb-queued", "queued", "queued", "not_started", now.Add(-3*time.Hour), false, []string{})
	insertWorkbenchReadJob(t, userID, "wb-old-queued", "queued", "queued", "not_started", now.AddDate(0, 0, -30), false, []string{})
	insertWorkbenchReadJob(t, userID, "wb-old-unknown", "unknown", "unknown", "unknown", now.AddDate(0, 0, -30), false, []string{})
	insertWorkbenchReadJob(t, userID, "wb-tombstone", "cancelled", "cancelled", "unknown", now, false, []string{})
	_, err = integrationDB.ExecContext(ctx, `UPDATE image_studio_jobs SET deleted_at=NOW() WHERE user_id=$1 AND task_id='wb-tombstone'`, userID)
	require.NoError(t, err)
	insertWorkbenchReadJob(t, goneID, "wb-user-gone", "completed", "completed", "recorded", now, false, []string{"hidden"})
	_, err = integrationDB.ExecContext(ctx, `UPDATE users SET deleted_at=NOW() WHERE id=$1`, goneID)
	require.NoError(t, err)
	gallery := NewImageStudioGalleryRepository(integrationDB)
	_, err = gallery.ReserveAsset(ctx, galleryTestAsset(userID, "wb-output"))
	require.NoError(t, err)
	require.NoError(t, gallery.MarkAssetReady(ctx, userID, "wb-output"))
	_, err = gallery.SaveTask(ctx, userID, "wb-gallery-history", strings.Repeat("b", 64), json.RawMessage(`{"id":"wb-gallery-history","status":"done"}`), []service.GalleryTaskLink{{ImageID: "wb-output", Role: "output", Ordinal: 0}})
	require.NoError(t, err)
	_, err = gallery.ReserveAsset(ctx, galleryTestAsset(userID, "wb-uploading"))
	require.NoError(t, err)
	after, err := repo.Overview(ctx, since, until)
	require.NoError(t, err)
	require.Equal(t, int64(3), after.Tasks.Total-before.Tasks.Total)
	require.Equal(t, int64(1), after.Tasks.Completed-before.Tasks.Completed)
	require.Equal(t, int64(1), after.Tasks.Partial-before.Tasks.Partial)
	require.Equal(t, int64(1), after.Tasks.Queued-before.Tasks.Queued)
	require.Equal(t, int64(0), after.Tasks.Cancelled-before.Tasks.Cancelled)
	require.Equal(t, int64(3), after.Calls.Total-before.Calls.Total, "planned queued children count, not paid call count")
	require.Equal(t, int64(3), after.OutputImages-before.OutputImages)
	require.Equal(t, int64(2), after.Queue.Queued-before.Queue.Queued, "current queue includes older jobs")
	require.Equal(t, int64(1), after.Queue.Unknown-before.Queue.Unknown)
	require.Equal(t, int64(1), after.Gallery.Tasks-before.Gallery.Tasks)
	require.Equal(t, int64(1), after.Gallery.OutputImages-before.Gallery.OutputImages)
	require.Equal(t, int64(2), after.Gallery.Assets-before.Gallery.Assets)
	require.Equal(t, int64(340), after.Gallery.UsedBytes-before.Gallery.UsedBytes, "uploading assets retain quota bytes")
}

func TestWorkbenchTaskPageFiltersStablePaginationAndSafeProjection(t *testing.T) {
	ctx := context.Background()
	repo := NewWorkbenchAdminRepository(integrationDB)
	id := galleryTestUser(t)
	now := time.Now().UTC()
	insertWorkbenchReadJob(t, id, "wb-first", "completed", "completed", "recorded", now, false, []string{"one"})
	insertWorkbenchReadJob(t, id, "wb-second", "failed", "failed", "pending", now.Add(-time.Minute), false, []string{})
	insertWorkbenchReadJob(t, id, "wb-hidden", "cancelled", "cancelled", "unknown", now.Add(time.Hour), true, []string{})
	filter := service.WorkbenchTaskFilter{Page: 1, PageSize: 1, UserID: id, Model: "gpt-image-2"}
	page, err := repo.ListTasks(ctx, filter)
	require.NoError(t, err)
	require.Equal(t, int64(2), page.Total)
	require.Equal(t, int64(2), page.Pages)
	require.Len(t, page.Items, 1)
	require.Equal(t, "wb-first", page.Items[0].TaskID)
	require.NotNil(t, page.Items[0].ElapsedMS)
	require.Equal(t, int64(3000), *page.Items[0].ElapsedMS)
	encoded, err := json.Marshal(page)
	require.NoError(t, err)
	for _, secret := range []string{"private-prompt-marker", "private-secret-marker", "private-bucket", "private-endpoint", "api_key_id", "spool_path", "request_json"} {
		require.NotContains(t, string(encoded), secret)
	}
	filter.Page = 2
	page, err = repo.ListTasks(ctx, filter)
	require.NoError(t, err)
	require.Equal(t, "wb-second", page.Items[0].TaskID)
	require.Contains(t, page.Items[0].ErrorMessage, "***")
	require.NotContains(t, page.Items[0].ErrorMessage, "private-secret-marker")
	require.Equal(t, int64(1), page.Items[0].BillingPending)
	filter.Page = 1
	filter.Status = "failed"
	page, err = repo.ListTasks(ctx, filter)
	require.NoError(t, err)
	require.Equal(t, int64(1), page.Total)
	filter.Model = "' OR TRUE --"
	page, err = repo.ListTasks(ctx, filter)
	require.NoError(t, err)
	require.Zero(t, page.Total)
	require.Empty(t, page.Items)
}
