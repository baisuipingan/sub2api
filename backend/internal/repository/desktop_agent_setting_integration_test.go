//go:build integration

package repository

import (
	"context"
	"testing"

	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/stretchr/testify/require"
)

func TestDesktopAgentSettingCompareAndSwapPreservesConcurrentSave(t *testing.T) {
	ctx := context.Background()
	repo := NewSettingRepository(testEntClient(t)).(*settingRepository)
	key := service.SettingKeyDesktopAgentConfig
	t.Cleanup(func() { _ = repo.Delete(ctx, key) })
	before := `{"enabled":true,"models":[{"id":"older"}]}`
	latest := `{"enabled":true,"models":[{"id":"latest","custom":"preserved"}],"group_id":12}`
	disabled := `{"enabled":false,"models":[{"id":"latest","custom":"preserved"}],"group_id":12}`
	require.NoError(t, repo.Set(ctx, key, before))
	require.NoError(t, repo.Set(ctx, key, latest))
	changed, err := repo.CompareAndSwapDesktopAgentConfig(ctx, before, `{"enabled":false}`)
	require.NoError(t, err)
	require.False(t, changed)
	raw, err := repo.GetValue(ctx, key)
	require.NoError(t, err)
	require.Equal(t, latest, raw)
	changed, err = repo.CompareAndSwapDesktopAgentConfig(ctx, latest, disabled)
	require.NoError(t, err)
	require.True(t, changed)
	raw, err = repo.GetValue(ctx, key)
	require.NoError(t, err)
	require.Equal(t, disabled, raw)
	require.NoError(t, repo.Delete(ctx, key))
	changed, err = repo.CompareAndSwapDesktopAgentConfig(ctx, disabled, before)
	require.NoError(t, err)
	require.False(t, changed)
	_, err = repo.GetValue(ctx, key)
	require.ErrorIs(t, err, service.ErrSettingNotFound)
}
