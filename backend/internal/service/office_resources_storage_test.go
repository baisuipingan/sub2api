//go:build unit

package service

import (
	"context"
	"testing"

	"github.com/Wei-Shaw/sub2api/internal/config"
	"github.com/stretchr/testify/require"
)

func TestOfficeResourcesPrivateStorageIndependentAndGuarded(t *testing.T) {
	ctx := context.Background()
	settings, _, _ := newImageStorageFixture(t, config.ImageStorageConfig{})
	in := ImageStorageSettings{Endpoint: "https://private.test", Region: "auto", Bucket: "files", AccessKeyID: "key", SecretAccessKey: "secret"}
	_, err := settings.Update(ctx, in)
	require.NoError(t, err)
	cfg, release, err := settings.AcquireOfficeStorage(ctx)
	require.NoError(t, err)
	require.True(t, cfg.Enabled)
	require.Equal(t, "workspace/v1/", cfg.Prefix)
	require.Empty(t, cfg.PublicBaseURL)
	changed := in
	changed.Bucket = "different"
	_, err = settings.Update(ctx, changed)
	require.ErrorIs(t, err, ErrOfficeStorageUnavailable, "first reservation protects the target even with all image toggles off")
	release()
	release()
	settings.SetOfficeStorageUsage(func(context.Context) (bool, error) { return true, nil })
	_, err = settings.Update(ctx, changed)
	require.ErrorIs(t, err, ErrOfficeStorageUnavailable, "pending, ready and deleting records keep the target pinned")
	public := in
	public.PublicBaseURL = "https://public.test"
	_, err = settings.Update(ctx, public)
	require.ErrorIs(t, err, ErrOfficeStorageUnavailable)
	_, err = settings.Update(ctx, in)
	require.NoError(t, err, "unrelated flags can still be saved")
}

func TestOfficeResourcesRejectPublicAndGuardReusedBackup(t *testing.T) {
	ctx := context.Background()
	settings, repo, _ := newImageStorageFixture(t, config.ImageStorageConfig{})
	seedBackupS3(t, repo, BackupS3Config{Endpoint: "https://private.test", Region: "auto", Bucket: "backup", AccessKeyID: "key", SecretAccessKey: "secret"})
	in := ImageStorageSettings{ReuseBackupS3: true, Bucket: "files"}
	_, err := settings.Update(ctx, in)
	require.NoError(t, err)
	settings.SetOfficeStorageUsage(func(context.Context) (bool, error) { return true, nil })
	_, err = settings.backup.UpdateS3Config(ctx, BackupS3Config{Endpoint: "https://different.test", Region: "auto", Bucket: "backup", AccessKeyID: "key", SecretAccessKey: "secret"})
	require.ErrorIs(t, err, ErrOfficeStorageUnavailable)
	public := NewImageStorageSettingService(newStubSettingRepo(), nil, nil, nil, config.ImageStorageConfig{Endpoint: "https://private.test", Bucket: "files", AccessKeyID: "key", SecretAccessKey: "secret", PublicBaseURL: "https://public.test"})
	_, err = public.PrivateOfficeStorageConfig(ctx)
	require.ErrorIs(t, err, ErrOfficeStorageUnavailable)
}

func TestOfficeResourcesSeparateBucketHasIndependentEffectiveTarget(t *testing.T) {
	ctx := context.Background()
	settings, _, _ := newImageStorageFixture(t, config.ImageStorageConfig{})
	in := ImageStorageSettings{Endpoint: "https://private.test", Region: "auto", Bucket: "images", OfficeBucket: "documents", AccessKeyID: "key", SecretAccessKey: "secret"}
	_, err := settings.Update(ctx, in)
	require.NoError(t, err)
	cfg, err := settings.PrivateOfficeStorageConfig(ctx)
	require.NoError(t, err)
	require.Equal(t, "documents", cfg.Bucket)
	settings.SetOfficeStorageUsage(func(context.Context) (bool, error) { return true, nil })
	changed := in
	changed.Bucket = "images-v2"
	_, err = settings.Update(ctx, changed)
	require.NoError(t, err, "independent image bucket may change if only office documents use storage")
	changed.OfficeBucket = "other-documents"
	_, err = settings.Update(ctx, changed)
	require.ErrorIs(t, err, ErrOfficeStorageUnavailable)
	changed = in
	changed.OfficeBucket = ""
	_, err = settings.Update(ctx, changed)
	require.ErrorIs(t, err, ErrOfficeStorageUnavailable, "clearing override cannot silently strand existing private office objects")
}
