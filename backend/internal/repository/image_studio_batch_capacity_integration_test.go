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

func TestGalleryGeneratedBatchReservationsRetainTenImageCapacity(t *testing.T) {
	require.Equal(t, int64(48<<20), galleryReservedBytesPerJobAsset)
	for _, tc := range []struct {
		name, protocol, postprocess string
		calls                       int
	}{
		{"grok-native-ten", "grok", "none", 1},
		{"keyed-ten-raw-and-result", "openai", "keyed", 10},
	} {
		t.Run(tc.name, func(t *testing.T) {
			ctx := context.Background()
			userID := galleryTestUser(t)
			t.Cleanup(func() {
				_, _ = integrationDB.ExecContext(ctx, `DELETE FROM image_studio_jobs WHERE user_id=$1`, userID)
			})
			repo := NewStudioJobRepository(integrationDB)
			job, err := repo.Create(ctx, service.StudioJobCreate{
				UserID: userID, APIKeyID: 1, TaskID: tc.name, Digest: strings.Repeat("c", 64),
				Task:      json.RawMessage(`{"id":"batch","status":"running"}`),
				Spec:      service.StudioJobSpec{Protocol: tc.protocol, Model: "image-model", ImageCount: 10, Postprocess: tc.postprocess},
				CallCount: tc.calls, StorageEndpoint: "https://s3.test", StorageRegion: "auto", StorageBucket: "gallery",
			})
			require.NoError(t, err, "raising input-reference capacity must not reject previously supported generated batches in an empty gallery")
			require.Len(t, job.Calls, tc.calls)
		})
	}
}
