//go:build unit

package service

import (
	"context"
	"errors"
	"testing"

	"github.com/Wei-Shaw/sub2api/internal/config"
	"github.com/stretchr/testify/require"
)

type officeConnectionProbeStorage struct {
	recordingStorage
	readWriteDelete func(context.Context, string) error
	signedRead      func(context.Context, string) error
}

func (s *officeConnectionProbeStorage) ProbeReadWriteDeletePrefix(ctx context.Context, prefix string) error {
	return s.readWriteDelete(ctx, prefix)
}

func (s *officeConnectionProbeStorage) ProbeSignedOfficeRead(ctx context.Context, prefix string) error {
	return s.signedRead(ctx, prefix)
}

type officeConnectionProbes struct {
	attempts         []config.ImageStorageConfig
	signedReads      int
	rejectSecret     string
	rejectSignedRead bool
}

func newOfficeStorageFixture(t *testing.T, fallback config.ImageStorageConfig) (*ImageStorageSettingService, *stubSettingRepo, *officeConnectionProbes) {
	t.Helper()
	svc, repo, _ := newImageStorageFixture(t, fallback)
	probes := &officeConnectionProbes{}
	svc.factory = func(_ context.Context, cfg *config.ImageStorageConfig) (ImageStorage, error) {
		return &officeConnectionProbeStorage{
			readWriteDelete: func(ctx context.Context, prefix string) error {
				t.Helper()
				require.Equal(t, officeStoragePrefix, prefix)
				require.Empty(t, cfg.PublicBaseURL)
				_, bounded := ctx.Deadline()
				require.True(t, bounded)
				probes.attempts = append(probes.attempts, *cfg)
				if cfg.SecretAccessKey == probes.rejectSecret {
					return errors.New("read/write denied")
				}
				return nil
			},
			signedRead: func(_ context.Context, prefix string) error {
				t.Helper()
				require.Equal(t, officeStoragePrefix, prefix)
				probes.signedReads++
				if probes.rejectSignedRead {
					return errors.New("signed GET denied")
				}
				return nil
			},
		}, nil
	}
	return svc, repo, probes
}

func TestOfficeStorageConfiguredSourceIsIndependent(t *testing.T) {
	ctx := context.Background()
	svc, repo, _ := newOfficeStorageFixture(t, config.ImageStorageConfig{})
	image := ImageStorageSettings{Endpoint: "https://images.test", Region: "auto", Bucket: "images", AccessKeyID: "img", SecretAccessKey: "image-secret", PublicBaseURL: "https://cdn.images.test"}
	_, err := svc.Update(ctx, image)
	require.NoError(t, err)
	seedBackupS3(t, repo, BackupS3Config{Endpoint: "https://backup.test", Region: "auto", Bucket: "backups", AccessKeyID: "backup", SecretAccessKey: "backup-secret"})

	_, err = svc.UpdateOfficeStorageSettings(ctx, OfficeStorageSettings{Source: officeStorageImages})
	require.ErrorIs(t, err, ErrOfficeStorageConfigInvalid)
	office := OfficeStorageSettings{Source: officeStorageImages, Bucket: "office"}
	_, err = svc.UpdateOfficeStorageSettings(ctx, office)
	require.NoError(t, err)
	cfg, err := svc.PrivateOfficeStorageConfig(ctx)
	require.NoError(t, err)
	require.Equal(t, "office", cfg.Bucket)
	require.Equal(t, image.Endpoint, cfg.Endpoint)
	require.Empty(t, cfg.PublicBaseURL)
	require.Equal(t, "workspace/v1/", cfg.Prefix)

	office.Source = officeStorageBackup
	_, err = svc.UpdateOfficeStorageSettings(ctx, office)
	require.NoError(t, err)
	svc.SetOfficeStorageUsage(func(context.Context) (bool, error) { return true, nil })
	image.Endpoint = "https://other-images.test"
	_, err = svc.Update(ctx, image)
	require.NoError(t, err)
	backupCfg := BackupS3Config{Endpoint: "https://backup.test", Region: "auto", Bucket: "other-backups", AccessKeyID: "backup"}
	_, err = svc.backup.UpdateS3Config(ctx, backupCfg)
	require.NoError(t, err)
	backupCfg.Endpoint = "https://other-backup.test"
	_, err = svc.backup.UpdateS3Config(ctx, backupCfg)
	require.ErrorIs(t, err, ErrOfficeStorageTargetInUse)

	office = OfficeStorageSettings{Source: officeStorageCustom, Bucket: "office", Endpoint: "https://backup.test", Region: "auto", AccessKeyID: "office", SecretAccessKey: "dedicated-secret"}
	view, err := svc.UpdateOfficeStorageSettings(ctx, office)
	require.NoError(t, err, "the same location may switch to a dedicated key")
	require.True(t, view.SecretConfigured)
	require.Empty(t, view.Config.SecretAccessKey)
	raw, err := repo.GetValue(ctx, settingKeyOfficeStorageConfig)
	require.NoError(t, err)
	require.Contains(t, raw, "enc:dedicated-secret")
	office.SecretAccessKey = ""
	_, err = svc.UpdateOfficeStorageSettings(ctx, office)
	require.NoError(t, err)
	_, err = svc.backup.UpdateS3Config(ctx, backupCfg)
	require.NoError(t, err, "dedicated office settings do not depend on backups")
	office.Bucket = "another-office"
	_, err = svc.UpdateOfficeStorageSettings(ctx, office)
	require.ErrorIs(t, err, ErrOfficeStorageTargetInUse)
}

func TestOfficeStorageUsesImageBackupDependencyAndRetainsEnvFallback(t *testing.T) {
	ctx := context.Background()
	svc, repo, _ := newOfficeStorageFixture(t, config.ImageStorageConfig{
		Endpoint: "http://minio:9000", Region: "us-east-1", Bucket: "office-test", AccessKeyID: "test", SecretAccessKey: "test-secret", ForcePathStyle: true,
	})
	cfg, err := svc.PrivateOfficeStorageConfig(ctx)
	require.NoError(t, err)
	require.Equal(t, "office-test", cfg.Bucket)
	seedBackupS3(t, repo, BackupS3Config{Endpoint: "https://backup.test", Region: "auto", Bucket: "backups", AccessKeyID: "backup", SecretAccessKey: "backup-secret"})
	images := ImageStorageSettings{ReuseBackupS3: true, Bucket: "images"}
	_, err = svc.Update(ctx, images)
	require.NoError(t, err)
	_, err = svc.UpdateOfficeStorageSettings(ctx, OfficeStorageSettings{Source: officeStorageImages, Bucket: "private-office"})
	require.NoError(t, err)
	svc.SetOfficeStorageUsage(func(context.Context) (bool, error) { return true, nil })
	_, err = svc.backup.UpdateS3Config(ctx, BackupS3Config{Endpoint: "https://other-backup.test", Region: "auto", Bucket: "backups", AccessKeyID: "backup"})
	require.ErrorIs(t, err, ErrOfficeStorageTargetInUse)
	images.PublicBaseURL = "https://cdn.test"
	_, err = svc.Update(ctx, images)
	require.NoError(t, err, "the office overrides the public image bucket")
	images.Bucket = "private-office"
	_, err = svc.Update(ctx, images)
	require.ErrorIs(t, err, ErrOfficeStorageTargetInUse, "changing image storage cannot expose the selected office bucket")
}

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

func TestOfficeResourcesReadsPrivateGalleryWithoutOfficeUploadStorage(t *testing.T) {
	ctx := context.Background()
	settings, repo, _ := newOfficeStorageFixture(t, config.ImageStorageConfig{})
	image := ImageStorageSettings{
		Endpoint: "https://images.test", Region: "auto", Bucket: "gallery",
		AccessKeyID: "key", SecretAccessKey: "secret", StudioGalleryEnabled: true,
	}
	_, err := settings.Update(ctx, image)
	require.NoError(t, err)
	require.NoError(t, repo.Set(ctx, settingKeyOfficeStorageConfig, `{"source":"custom"}`))
	_, err = settings.PrivateOfficeStorageConfig(ctx)
	require.Error(t, err)

	calls := 0
	store := &galleryTestStore{}
	resources := NewOfficeResourcesService(nil, settings, func(_ context.Context, cfg *config.ImageStorageConfig) (GalleryObjectStore, error) {
		calls++
		require.Equal(t, image.Bucket, cfg.Bucket)
		require.Equal(t, image.Endpoint, cfg.Endpoint)
		require.Empty(t, cfg.PublicBaseURL)
		return store, nil
	}, nil)
	resolved, err := resources.storage(ctx, image.Endpoint, image.Region, image.Bucket)
	require.NoError(t, err)
	require.Same(t, store, resolved)
	_, err = resources.storage(ctx, image.Endpoint, image.Region, "some-other-bucket")
	require.ErrorIs(t, err, ErrOfficeStorageUnavailable)
	require.Equal(t, 1, calls)
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
	settings, _, _ := newOfficeStorageFixture(t, config.ImageStorageConfig{})
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

func TestOfficeStorageRejectsFailedConnectionsBeforeSaving(t *testing.T) {
	ctx := context.Background()
	svc, repo, probes := newOfficeStorageFixture(t, config.ImageStorageConfig{})
	office := OfficeStorageSettings{Source: officeStorageCustom, Bucket: "office", Endpoint: "https://s3.test", AccessKeyID: "office", SecretAccessKey: "good"}
	probes.rejectSignedRead = true
	_, err := svc.UpdateOfficeStorageSettings(ctx, office)
	require.ErrorIs(t, err, ErrOfficeStorageProbeFailed)
	raw, err := repo.GetValue(ctx, settingKeyOfficeStorageConfig)
	require.NoError(t, err)
	require.Empty(t, raw)

	probes.rejectSignedRead = false
	_, err = svc.UpdateOfficeStorageSettings(ctx, office)
	require.NoError(t, err)
	require.Equal(t, 2, probes.signedReads)
	svc.SetOfficeStorageUsage(func(context.Context) (bool, error) { return true, nil })
	raw, err = repo.GetValue(ctx, settingKeyOfficeStorageConfig)
	require.NoError(t, err)

	probes.rejectSecret = "bad"
	office.SecretAccessKey = "bad"
	_, err = svc.UpdateOfficeStorageSettings(ctx, office)
	require.ErrorIs(t, err, ErrOfficeStorageProbeFailed)
	got, err := repo.GetValue(ctx, settingKeyOfficeStorageConfig)
	require.NoError(t, err)
	require.Equal(t, raw, got)
	require.Len(t, probes.attempts, 3)
	require.Equal(t, 2, probes.signedReads)

	office.SecretAccessKey = ""
	_, err = svc.UpdateOfficeStorageSettings(ctx, office)
	require.NoError(t, err)
	require.Len(t, probes.attempts, 3)
	office.SecretAccessKey = "rotated"
	_, err = svc.UpdateOfficeStorageSettings(ctx, office)
	require.NoError(t, err)
	require.Equal(t, 3, probes.signedReads)
	cfg, err := svc.PrivateOfficeStorageConfig(ctx)
	require.NoError(t, err)
	require.Equal(t, "rotated", cfg.SecretAccessKey)
}

func TestOfficeStorageVerifiesInheritedChangesBeforeFirstUpload(t *testing.T) {
	ctx := context.Background()
	for _, tc := range []struct {
		name   string
		source string
		reuse  bool
	}{
		{name: "backup", source: officeStorageBackup},
		{name: "images", source: officeStorageImages},
		{name: "images inherit backup", source: officeStorageImages, reuse: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			svc, repo, probes := newOfficeStorageFixture(t, config.ImageStorageConfig{})
			backup := BackupS3Config{Endpoint: "https://backup.test", Region: "auto", Bucket: "backup", AccessKeyID: "backup", SecretAccessKey: "good"}
			seedBackupS3(t, repo, backup)
			images := ImageStorageSettings{Endpoint: "https://images.test", Region: "auto", Bucket: "images", AccessKeyID: "images", SecretAccessKey: "good", ReuseBackupS3: tc.reuse}
			_, err := svc.Update(ctx, images)
			require.NoError(t, err)
			_, err = svc.UpdateOfficeStorageSettings(ctx, OfficeStorageSettings{Source: tc.source, Bucket: "office"})
			require.NoError(t, err)
			require.Len(t, probes.attempts, 1)

			probes.rejectSecret = "bad"
			if tc.source == officeStorageBackup || tc.reuse {
				backup.SecretAccessKey = "bad"
				_, err = svc.backup.UpdateS3Config(ctx, backup)
				require.ErrorIs(t, err, ErrOfficeStorageProbeFailed)
				backup.SecretAccessKey = ""
				backup.Prefix = "other-prefix"
				_, err = svc.backup.UpdateS3Config(ctx, backup)
				require.NoError(t, err)
			} else {
				images.SecretAccessKey = "bad"
				_, err = svc.Update(ctx, images)
				require.ErrorIs(t, err, ErrOfficeStorageProbeFailed)
				images.SecretAccessKey = ""
				images.Prefix = "other-prefix"
				_, err = svc.Update(ctx, images)
				require.NoError(t, err)
			}
			require.Len(t, probes.attempts, 2)
			require.Equal(t, "office", probes.attempts[1].Bucket)
			cfg, err := svc.PrivateOfficeStorageConfig(ctx)
			require.NoError(t, err)
			require.Equal(t, "good", cfg.SecretAccessKey)
		})
	}
}
