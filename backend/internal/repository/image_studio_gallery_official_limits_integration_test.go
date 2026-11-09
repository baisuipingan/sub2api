//go:build integration

package repository

import (
	"context"
	"testing"

	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/Wei-Shaw/sub2api/migrations"
	"github.com/stretchr/testify/require"
)

func TestGalleryOfficialInputMigrationAcceptsOriginalLargeReferences(t *testing.T) {
	ctx := context.Background()
	userID := galleryTestUser(t)
	repo := NewImageStudioGalleryRepository(integrationDB)
	asset := galleryTestAsset(userID, "official-large-input")
	asset.Width, asset.Height = 9000, 4367
	asset.Bytes = service.GalleryMaxImageBytes
	asset.MIME = "image/jpeg"
	stored, err := repo.ReserveAsset(ctx, asset)
	require.NoError(t, err, "fresh migrations must widen the gallery schema before using official input limits")
	require.Equal(t, asset.Width, stored.Width)
	require.Equal(t, asset.Height, stored.Height)
	require.Equal(t, asset.Bytes, stored.Bytes)
	require.Equal(t, asset.SHA256, stored.SHA256)
	require.NoError(t, repo.MarkAssetReady(ctx, userID, asset.ImageID))
	stored, err = repo.GetAsset(ctx, userID, asset.ImageID)
	require.NoError(t, err)
	require.Equal(t, asset.OriginalKey, stored.OriginalKey)
	require.Equal(t, "ready", stored.State)
	migration, err := migrations.FS.ReadFile("dsapi_005_gallery_official_input_limits.sql")
	require.NoError(t, err)
	_, err = integrationDB.ExecContext(ctx, string(migration))
	require.NoError(t, err, "constraint replacement must also be idempotent on an upgraded database")
	_, err = integrationDB.ExecContext(ctx, `UPDATE image_studio_gallery_assets SET byte_size=50000000 WHERE user_id=$1 AND image_id=$2`, userID, asset.ImageID)
	require.Error(t, err, "50 MB itself is outside the documented strict less-than bound")
	_, err = integrationDB.ExecContext(ctx, `UPDATE image_studio_gallery_assets SET byte_size=0 WHERE user_id=$1 AND image_id=$2`, userID, asset.ImageID)
	require.Error(t, err)
	_, err = integrationDB.ExecContext(ctx, `UPDATE image_studio_gallery_assets SET width=0 WHERE user_id=$1 AND image_id=$2`, userID, asset.ImageID)
	require.Error(t, err)
	_, err = integrationDB.ExecContext(ctx, `UPDATE image_studio_gallery_assets SET height=-1 WHERE user_id=$1 AND image_id=$2`, userID, asset.ImageID)
	require.Error(t, err)
	_, err = integrationDB.ExecContext(ctx, `UPDATE image_studio_gallery_assets SET state='unsafe' WHERE user_id=$1 AND image_id=$2`, userID, asset.ImageID)
	require.Error(t, err, "unrelated state constraints must survive the focused replacement")
}
