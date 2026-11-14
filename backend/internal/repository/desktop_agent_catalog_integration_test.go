//go:build integration

package repository

import (
	"context"
	"testing"
	"time"

	"github.com/Wei-Shaw/sub2api/ent/setting"
	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/stretchr/testify/require"
)

func TestDesktopAgentCatalogCompareAndSwapProtectsSourceAndLastGood(t *testing.T) {
	ctx := context.Background()
	repo := NewSettingRepository(testEntClient(t)).(*settingRepository)
	key := service.SettingKeyDesktopAgentProfileCatalog
	require.NoError(t, repo.Delete(ctx, key))
	t.Cleanup(func() { _ = repo.Delete(ctx, key) })
	first := `{"source_url":"https://example.test/first","revision":"first"}`
	next := `{"source_url":"https://example.test/next","revision":"next"}`
	changed, err := repo.CompareAndSwapDesktopAgentProfileCatalog(ctx, "", first)
	require.NoError(t, err)
	require.True(t, changed)
	changed, err = repo.CompareAndSwapDesktopAgentProfileCatalog(ctx, "", next)
	require.NoError(t, err)
	require.False(t, changed)
	changed, err = repo.CompareAndSwapDesktopAgentProfileCatalog(ctx, first, next)
	require.NoError(t, err)
	require.True(t, changed)
	changed, err = repo.CompareAndSwapDesktopAgentProfileCatalog(ctx, first, first)
	require.NoError(t, err)
	require.False(t, changed)
	raw, err := repo.GetValue(ctx, key)
	require.NoError(t, err)
	require.Equal(t, next, raw)
	require.NoError(t, repo.Delete(ctx, key))
	changed, err = repo.CompareAndSwapDesktopAgentProfileCatalog(ctx, next, first)
	require.NoError(t, err)
	require.False(t, changed)
}

func TestDesktopAgentCatalogAndConfigPublicationRejectQueuedStaleSnapshots(t *testing.T) {
	ctx := context.Background()
	repo := NewSettingRepository(testEntClient(t)).(*settingRepository)
	catalogKey := service.SettingKeyDesktopAgentProfileCatalog
	configKey := service.SettingKeyDesktopAgentConfig
	t.Cleanup(func() { _ = repo.Delete(ctx, catalogKey); _ = repo.Delete(ctx, configKey) })
	require.NoError(t, repo.Set(ctx, catalogKey, "catalog-old"))
	require.NoError(t, repo.Set(ctx, configKey, "config-old"))
	type result struct {
		changed bool
		err     error
	}
	tx, err := repo.client.Tx(ctx)
	require.NoError(t, err)
	_, err = lockDesktopAgentProfileCatalog(ctx, tx)
	require.NoError(t, err)
	done := make(chan result, 1)
	go func() {
		changed, err := repo.CompareAndSwapDesktopAgentProfileCatalog(ctx, "catalog-old", "catalog-new", "config-old")
		done <- result{changed, err}
	}()
	select {
	case <-done:
		t.Fatal("catalog write passed an uncommitted catalog lock")
	case <-time.After(50 * time.Millisecond):
	}
	_, err = tx.Setting.Update().Where(setting.KeyEQ(configKey)).SetValue("config-new").Save(ctx)
	require.NoError(t, err)
	require.NoError(t, tx.Commit())
	completed := <-done
	require.NoError(t, completed.err)
	require.False(t, completed.changed)
	raw, err := repo.GetValue(ctx, catalogKey)
	require.NoError(t, err)
	require.Equal(t, "catalog-old", raw)
	tx, err = repo.client.Tx(ctx)
	require.NoError(t, err)
	_, err = lockDesktopAgentProfileCatalog(ctx, tx)
	require.NoError(t, err)
	go func() {
		changed, err := repo.SaveDesktopAgentConfigWithCatalog(ctx, "catalog-old", "config-stale")
		done <- result{changed, err}
	}()
	select {
	case <-done:
		t.Fatal("config write passed an uncommitted catalog lock")
	case <-time.After(50 * time.Millisecond):
	}
	_, err = tx.Setting.Update().Where(setting.KeyEQ(catalogKey)).SetValue("catalog-new").Save(ctx)
	require.NoError(t, err)
	require.NoError(t, tx.Commit())
	completed = <-done
	require.NoError(t, completed.err)
	require.False(t, completed.changed)
	raw, err = repo.GetValue(ctx, configKey)
	require.NoError(t, err)
	require.Equal(t, "config-new", raw)
	changed, err := repo.SaveDesktopAgentConfigWithCatalog(ctx, "catalog-new", "config-current")
	require.NoError(t, err)
	require.True(t, changed)
}
