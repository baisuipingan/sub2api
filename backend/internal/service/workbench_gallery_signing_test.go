//go:build unit

package service

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func TestWorkbenchGallerySignsOnlyAuthorizedReducedVariants(t *testing.T) {
	ctx := context.Background()
	gallery, assets, _, _ := galleryTestService(t)
	require.NoError(t, gallery.PutAsset(ctx, 7, "output-one", "image/png", galleryPNG(t, 8, 6)))
	repo := &workbenchGalleryStub{exists: true}
	svc := NewWorkbenchAdminService(repo, nil, nil)
	svc.gallery = gallery
	asset := assets.assets["output-one"]
	for _, variant := range []string{"thumbnail", "preview"} {
		result, err := svc.GalleryImageURL(ctx, 7, "task-one", "output-one", variant)
		require.NoError(t, err)
		require.NotContains(t, result.URL, asset.OriginalKey)
		key := asset.ThumbnailKey
		if variant == "preview" {
			key = asset.PreviewKey
		}
		require.Contains(t, result.URL, key)
		expires, err := time.Parse(time.RFC3339, result.ExpiresAt)
		require.NoError(t, err)
		require.InDelta(t, (10 * time.Minute).Seconds(), time.Until(expires).Seconds(), 2)
	}
	_, err := svc.GalleryImageURL(ctx, 7, "task-one", "output-one", "original")
	require.ErrorIs(t, err, ErrWorkbenchInvalid)
	require.Equal(t, 2, repo.calls, "original access is rejected before any asset lookup")
}
