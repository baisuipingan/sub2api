//go:build integration

package repository

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"testing"

	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/stretchr/testify/require"
)

func TestSaveDesktopAgentConfigRebindsAllIssuedKeys(t *testing.T) {
	f := newDesktopDBFixture(t)
	ctx := context.Background()
	client := testEntClient(t)
	repo := NewSettingRepository(client).(*settingRepository)
	// The existing group is OpenAI and the target is Composite.
	f.groupID, f.otherGroupID = f.otherGroupID, f.groupID
	f.configure(t, f.groupID, true)
	key, _, err := f.repo.EnsureDesktopAgentKey(ctx, f.candidate("migrate"))
	require.NoError(t, err)
	_, err = integrationDB.ExecContext(ctx, `UPDATE api_keys SET quota=10,quota_used=3,rate_limit_5h=4,usage_5h=1 WHERE id=$1`, key.ID)
	require.NoError(t, err)
	image := &service.APIKey{UserID: f.userID, Key: fmt.Sprintf("fixture-studio-%d", f.userID), Name: service.ImageStudioKeyName, GroupID: &f.groupID, Status: service.StatusActive}
	require.NoError(t, f.repo.CreateSystemKey(ctx, image))
	issued := []int64{key.ID}
	for _, mode := range []string{"disabled", "expired", "exhausted", "restricted", "inactive user"} {
		u, err := client.User.Create().SetEmail(fmt.Sprintf("%s-%d@test.invalid", mode, f.userID)).SetPasswordHash("fixture").SetStatus(service.StatusActive).Save(ctx)
		require.NoError(t, err)
		t.Cleanup(func() {
			_, _ = integrationDB.ExecContext(ctx, `DELETE FROM api_keys WHERE user_id=$1`, u.ID)
			_, _ = integrationDB.ExecContext(ctx, `DELETE FROM users WHERE id=$1`, u.ID)
		})
		candidate := f.candidate(mode)
		candidate.UserID = u.ID
		protected, _, err := f.repo.EnsureDesktopAgentKey(ctx, candidate)
		require.NoError(t, err)
		issued = append(issued, protected.ID)
		switch mode {
		case "disabled":
			_, err = integrationDB.ExecContext(ctx, `UPDATE api_keys SET status='disabled' WHERE id=$1`, protected.ID)
		case "expired":
			_, err = integrationDB.ExecContext(ctx, `UPDATE api_keys SET expires_at=NOW()-INTERVAL '1 hour' WHERE id=$1`, protected.ID)
		case "exhausted":
			_, err = integrationDB.ExecContext(ctx, `UPDATE api_keys SET quota=1,quota_used=1 WHERE id=$1`, protected.ID)
		case "restricted":
			_, err = integrationDB.ExecContext(ctx, `UPDATE users SET restrict_public_groups=true WHERE id=$1`, u.ID)
		case "inactive user":
			_, err = integrationDB.ExecContext(ctx, `UPDATE users SET status='disabled' WHERE id=$1`, u.ID)
		}
		require.NoError(t, err)
	}
	originalKeys := make(map[int64]*service.APIKey, len(issued))
	var expectedHashes []string
	for _, id := range issued {
		before, err := f.repo.GetByID(ctx, id)
		require.NoError(t, err)
		originalKeys[id] = before
		sum := sha256.Sum256([]byte(before.Key))
		expectedHashes = append(expectedHashes, hex.EncodeToString(sum[:]))
	}
	before, err := repo.GetValue(ctx, service.SettingKeyDesktopAgentConfig)
	require.NoError(t, err)
	value := fmt.Sprintf(`{"enabled":true,"group_id":%d}`, f.otherGroupID)
	saved, _, err := repo.SaveDesktopAgentConfig(ctx, `{"enabled":true,"group_id":9223372036854775807}`, nil)
	require.ErrorIs(t, err, service.ErrDesktopAgentInvalid)
	require.False(t, saved)
	unchanged, err := repo.GetValue(ctx, service.SettingKeyDesktopAgentConfig)
	require.NoError(t, err)
	require.JSONEq(t, before, unchanged)

	var cursor int64
	require.NoError(t, integrationDB.QueryRowContext(ctx, `SELECT COALESCE(MAX(id),0) FROM auth_cache_invalidation_outbox`).Scan(&cursor))
	saved, hashes, err := repo.SaveDesktopAgentConfig(ctx, value, nil)
	require.NoError(t, err)
	require.True(t, saved)
	require.ElementsMatch(t, expectedHashes, hashes)
	for _, hash := range hashes {
		var invalidations int
		require.NoError(t, integrationDB.QueryRowContext(ctx, `SELECT COUNT(*) FROM auth_cache_invalidation_outbox WHERE id > $1 AND cache_key=$2`, cursor, hash).Scan(&invalidations))
		require.Equal(t, 1, invalidations)
	}
	for _, id := range issued {
		updated, err := f.repo.GetByID(ctx, id)
		require.NoError(t, err)
		before := originalKeys[id]
		require.Equal(t, &f.otherGroupID, updated.GroupID)
		require.Equal(t, before.Key, updated.Key)
		require.Equal(t, before.Status, updated.Status)
		require.Equal(t, before.Quota, updated.Quota)
		require.Equal(t, before.QuotaUsed, updated.QuotaUsed)
		require.Equal(t, before.ExpiresAt, updated.ExpiresAt)
		require.Equal(t, before.RateLimit5h, updated.RateLimit5h)
		require.Equal(t, before.Usage5h, updated.Usage5h)
	}
	kept, err := f.repo.GetByID(ctx, image.ID)
	require.NoError(t, err)
	require.Equal(t, f.groupID, *kept.GroupID)
	saved, hashes, err = repo.SaveDesktopAgentConfig(ctx, value, nil)
	require.NoError(t, err)
	require.True(t, saved)
	require.Empty(t, hashes)
}
