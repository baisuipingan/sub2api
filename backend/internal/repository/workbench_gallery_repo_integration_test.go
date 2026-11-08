//go:build integration

package repository

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/stretchr/testify/require"
)

func TestWorkbenchGalleryListsImportedOutputsWithoutPrivateTaskData(t *testing.T) {
	ctx := context.Background()
	userID, otherID := galleryTestUser(t), galleryTestUser(t)
	gallery := NewImageStudioGalleryRepository(integrationDB)
	for _, user := range []int64{userID, otherID} {
		for _, id := range []string{"output-one", "reference-one"} {
			_, err := gallery.ReserveAsset(ctx, galleryTestAsset(user, id))
			require.NoError(t, err)
			require.NoError(t, gallery.MarkAssetReady(ctx, user, id))
		}
		_, err := gallery.SaveTask(ctx, user, "same-imported-task", strings.Repeat("a", 64), json.RawMessage(`{"id":"same-imported-task","status":"done","apiModel":"gpt-image-2","prompt":"private-prompt-marker","inputImageIds":["reference-one"],"outputImages":["output-one"]}`), []service.GalleryTaskLink{{ImageID: "output-one", Role: "output", Ordinal: 0}, {ImageID: "reference-one", Role: "input", Ordinal: 0}})
		require.NoError(t, err)
	}
	repo := &workbenchAdminRepository{db: integrationDB}
	page, err := repo.ListGalleryTasks(ctx, service.WorkbenchGalleryFilter{Page: 1, PageSize: 10, UserID: userID, TaskID: "same-imported-task"})
	require.NoError(t, err)
	require.Equal(t, int64(1), page.Total)
	require.Len(t, page.Items, 1)
	require.Equal(t, []string{"output-one"}, page.Items[0].ImageIDs)
	require.Equal(t, "gpt-image-2", page.Items[0].Model)
	encoded, err := json.Marshal(page)
	require.NoError(t, err)
	for _, secret := range []string{"private-prompt-marker", "reference-one", "task_json", "storage_bucket", "original_key", "url", "https://"} {
		require.NotContains(t, string(encoded), secret)
	}
	for _, tc := range []struct {
		user        int64
		task, image string
		exists      bool
	}{{userID, "same-imported-task", "output-one", true}, {userID, "same-imported-task", "reference-one", false}, {otherID, "wrong-task", "output-one", false}} {
		ok, err := repo.GalleryOutputExists(ctx, tc.user, tc.task, tc.image)
		require.NoError(t, err)
		require.Equal(t, tc.exists, ok)
	}
	_, err = gallery.DeleteTask(ctx, userID, "same-imported-task")
	require.NoError(t, err)
	ok, err := repo.GalleryOutputExists(ctx, userID, "same-imported-task", "output-one")
	require.NoError(t, err)
	require.False(t, ok)
	page, err = repo.ListGalleryTasks(ctx, service.WorkbenchGalleryFilter{Page: 1, PageSize: 10, UserID: userID})
	require.NoError(t, err)
	require.Zero(t, page.Total)
	_, err = integrationDB.ExecContext(ctx, `UPDATE users SET deleted_at=NOW() WHERE id=$1`, otherID)
	require.NoError(t, err)
	ok, err = repo.GalleryOutputExists(ctx, otherID, "same-imported-task", "output-one")
	require.NoError(t, err)
	require.False(t, ok)
}
