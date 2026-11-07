//go:build unit

package service

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"sync"
	"testing"

	"github.com/Wei-Shaw/sub2api/internal/config"
	"github.com/stretchr/testify/require"
)

type stubSettingRepo struct {
	mu     sync.Mutex
	values map[string]string
}

func newStubSettingRepo() *stubSettingRepo {
	return &stubSettingRepo{values: map[string]string{}}
}

func (r *stubSettingRepo) Get(context.Context, string) (*Setting, error) { return nil, nil }
func (r *stubSettingRepo) GetValue(_ context.Context, key string) (string, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.values[key], nil
}

func (r *stubSettingRepo) Set(_ context.Context, key, value string) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.values[key] = value
	return nil
}
func (r *stubSettingRepo) GetMultiple(context.Context, []string) (map[string]string, error) {
	return map[string]string{}, nil
}
func (r *stubSettingRepo) SetMultiple(context.Context, map[string]string) error { return nil }
func (r *stubSettingRepo) GetAll(context.Context) (map[string]string, error) {
	return map[string]string{}, nil
}
func (r *stubSettingRepo) Delete(context.Context, string) error { return nil }

// reversibleEncryptor stands in for AES: prefixed so a test can tell ciphertext
// from plaintext, and so decrypting a plaintext value fails like the real one.
type reversibleEncryptor struct{}

func (reversibleEncryptor) Encrypt(plaintext string) (string, error) {
	return "enc:" + plaintext, nil
}

func (reversibleEncryptor) Decrypt(ciphertext string) (string, error) {
	rest, ok := strings.CutPrefix(ciphertext, "enc:")
	if !ok {
		return "", errors.New("not encrypted")
	}
	return rest, nil
}

type recordingStorage struct {
	saved      []string
	probeCalls int
	probeErr   error
}

func (s *recordingStorage) Save(_ context.Context, key, _ string, _ []byte) (string, error) {
	s.saved = append(s.saved, key)
	return "https://cdn.example.com/" + key, nil
}

func (s *recordingStorage) ProbeReadWriteDelete(context.Context) error {
	s.probeCalls++
	return s.probeErr
}

func TestImageStorageGalleryConnectionProbe(t *testing.T) {
	repo := newStubSettingRepo()
	backup := NewBackupService(repo, &config.Config{Totp: config.TotpConfig{EncryptionKeyConfigured: true}}, reversibleEncryptor{}, nil, nil)
	storage := &recordingStorage{}
	settings := NewImageStorageSettingService(repo, reversibleEncryptor{}, backup,
		func(context.Context, *config.ImageStorageConfig) (ImageStorage, error) { return storage, nil },
		config.ImageStorageConfig{})
	in := ImageStorageSettings{StudioGalleryEnabled: true, Bucket: "gallery", Endpoint: "https://s3.test", Region: "auto", AccessKeyID: "ak", SecretAccessKey: "enc:secret"}
	require.NoError(t, settings.TestConnection(context.Background(), in))
	require.Equal(t, 1, storage.probeCalls)
	storage.probeErr = errors.New("read permission denied")
	require.ErrorContains(t, settings.TestConnection(context.Background(), in), "read permission denied")
	in.StudioGalleryEnabled = false
	require.NoError(t, settings.TestConnection(context.Background(), in))
	require.Equal(t, 2, storage.probeCalls)
	in.StudioGalleryEnabled = true
	in.PublicBaseURL = "https://public.example"
	require.ErrorIs(t, settings.TestConnection(context.Background(), in), ErrGalleryPublicStorage)
}

func newImageStorageFixture(t *testing.T, fallback config.ImageStorageConfig) (*ImageStorageSettingService, *stubSettingRepo, *[]config.ImageStorageConfig) {
	return newImageStorageFixtureWithKey(t, fallback, true)
}

func newImageStorageFixtureWithKey(t *testing.T, fallback config.ImageStorageConfig, encryptionKeyConfigured bool) (*ImageStorageSettingService, *stubSettingRepo, *[]config.ImageStorageConfig) {
	t.Helper()
	repo := newStubSettingRepo()
	encryptor := reversibleEncryptor{}
	backup := NewBackupService(repo, &config.Config{
		Totp: config.TotpConfig{EncryptionKeyConfigured: encryptionKeyConfigured},
	}, encryptor, nil, nil)

	var built []config.ImageStorageConfig
	factory := func(_ context.Context, cfg *config.ImageStorageConfig) (ImageStorage, error) {
		built = append(built, *cfg)
		return &recordingStorage{}, nil
	}
	return NewImageStorageSettingService(repo, encryptor, backup, factory, fallback), repo, &built
}

func seedBackupS3(t *testing.T, repo *stubSettingRepo, cfg BackupS3Config) {
	t.Helper()
	cfg.SecretAccessKey = "enc:" + cfg.SecretAccessKey
	data, err := json.Marshal(cfg)
	require.NoError(t, err)
	require.NoError(t, repo.Set(context.Background(), settingKeyBackupS3Config, string(data)))
}

// The admin switch must take effect without a restart: that is the entire point
// of moving image_storage out of config.yaml (#4542).
func resolveImageStorageForTest(svc *ImageStorageSettingService) (*ImageResultUploader, bool) {
	uploader, release, enabled := svc.Resolver()()
	if release != nil {
		release()
	}
	return uploader, enabled
}

func TestImageStorageSettingsToggleTakesEffectWithoutRestart(t *testing.T) {
	svc, repo, built := newImageStorageFixture(t, config.ImageStorageConfig{})
	ctx := context.Background()
	seedBackupS3(t, repo, BackupS3Config{
		Endpoint: "https://acct.r2.cloudflarestorage.com", Region: "auto",
		Bucket: "backup-bucket", AccessKeyID: "ak", SecretAccessKey: "sk",
		Prefix: "backups/",
	})

	uploader, enabled := resolveImageStorageForTest(svc)
	require.False(t, enabled, "disabled until an admin turns it on")
	require.Nil(t, uploader)

	_, err := svc.Update(ctx, ImageStorageSettings{Enabled: true, ReuseBackupS3: true})
	require.NoError(t, err)

	uploader, enabled = resolveImageStorageForTest(svc)
	require.True(t, enabled, "saving the setting must enable the feature immediately")
	require.NotNil(t, uploader)

	_, err = svc.Update(ctx, ImageStorageSettings{Enabled: false, ReuseBackupS3: true})
	require.NoError(t, err)
	_, enabled = resolveImageStorageForTest(svc)
	require.False(t, enabled, "turning it back off must also apply immediately")

	require.Len(t, *built, 1, "the S3 client is built only when the feature is on")
}

func TestAsyncImageStorageBindingPinsTargetUntilTaskFinishes(t *testing.T) {
	settings, _, _ := newImageStorageFixture(t, config.ImageStorageConfig{})
	ctx := context.Background()
	_, err := settings.Update(ctx, ImageStorageSettings{
		Enabled: true, Bucket: "original-images", Endpoint: "https://s3.example.test",
		Region: "auto", AccessKeyID: "ak", SecretAccessKey: "sk",
	})
	require.NoError(t, err)
	uploader, release, enabled := settings.Resolver()()
	require.True(t, enabled)
	require.NotNil(t, uploader)
	require.NotNil(t, release)
	defer release()

	_, err = settings.Update(ctx, ImageStorageSettings{
		Enabled: true, Bucket: "different-images", Endpoint: "https://s3.example.test",
		Region: "auto", AccessKeyID: "ak",
	})
	require.ErrorIs(t, err, ErrAsyncImageStorageTargetInUse)
	_, err = settings.Update(ctx, ImageStorageSettings{
		Enabled: true, Bucket: "original-images", Endpoint: "https://s3.example.test",
		Region: "auto", AccessKeyID: "ak", Prefix: "new-prefix/",
	})
	require.NoError(t, err, "a pinned job keeps its original prefix; new jobs may use another")
	_, err = settings.Update(ctx, ImageStorageSettings{
		Enabled: false, Bucket: "original-images", Endpoint: "https://s3.example.test",
		Region: "auto", AccessKeyID: "ak", Prefix: "new-prefix/",
	})
	require.NoError(t, err, "disabling admissions must not block accepted jobs")
	_, _, enabled = settings.Resolver()()
	require.False(t, enabled)

	release()
	_, err = settings.Update(ctx, ImageStorageSettings{
		Enabled: true, Bucket: "different-images", Endpoint: "https://s3.example.test",
		Region: "auto", AccessKeyID: "ak",
	})
	require.NoError(t, err, "target can change after accepted tasks finish")
}

func TestBackupS3TargetChangeRespectsNativeAsyncBinding(t *testing.T) {
	settings, repo, built := newImageStorageFixture(t, config.ImageStorageConfig{})
	ctx := context.Background()
	seedBackupS3(t, repo, BackupS3Config{
		Endpoint: "https://original-s3.test", Region: "auto", Bucket: "backups",
		AccessKeyID: "ak", SecretAccessKey: "sk", Prefix: "backups/",
	})
	_, err := settings.Update(ctx, ImageStorageSettings{
		Enabled: true, ReuseBackupS3: true, Bucket: "images", Prefix: "images/",
	})
	require.NoError(t, err)
	_, release, enabled := settings.Resolver()()
	require.True(t, enabled)
	defer release()
	require.Len(t, *built, 1)

	_, err = settings.backup.UpdateS3Config(ctx, BackupS3Config{
		Endpoint: "https://different-s3.test", Region: "auto", Bucket: "backups",
		AccessKeyID: "ak", SecretAccessKey: "sk", Prefix: "backups/",
	})
	require.ErrorIs(t, err, ErrAsyncImageStorageTargetInUse)
	_, err = settings.backup.UpdateS3Config(ctx, BackupS3Config{
		Endpoint: "https://original-s3.test", Region: "auto", Bucket: "backups",
		AccessKeyID: "ak", SecretAccessKey: "sk", Prefix: "new-backups/",
	})
	require.NoError(t, err, "backup prefix may change without moving the image bucket")
	_, nextRelease, enabled := settings.Resolver()()
	require.True(t, enabled)
	nextRelease()
	require.Len(t, *built, 2, "the new admission must rebuild after backup settings change")

	release()
	_, err = settings.backup.UpdateS3Config(ctx, BackupS3Config{
		Endpoint: "https://different-s3.test", Region: "auto", Bucket: "backups",
		AccessKeyID: "ak", SecretAccessKey: "sk", Prefix: "new-backups/",
	})
	require.NoError(t, err, "target can change once all accepted jobs have completed")
}

func TestImageStorageSettingsReuseBackupCredentials(t *testing.T) {
	svc, repo, built := newImageStorageFixture(t, config.ImageStorageConfig{})
	ctx := context.Background()
	seedBackupS3(t, repo, BackupS3Config{
		Endpoint: "https://acct.r2.cloudflarestorage.com", Region: "wnam",
		Bucket: "backup-bucket", AccessKeyID: "backup-ak", SecretAccessKey: "backup-sk",
		Prefix: "backups/", ForcePathStyle: true,
	})

	_, err := svc.Update(ctx, ImageStorageSettings{Enabled: true, ReuseBackupS3: true, Prefix: "images"})
	require.NoError(t, err)
	_, enabled := resolveImageStorageForTest(svc)
	require.True(t, enabled)

	require.Len(t, *built, 1)
	got := (*built)[0]
	require.Equal(t, "https://acct.r2.cloudflarestorage.com", got.Endpoint)
	require.Equal(t, "wnam", got.Region)
	require.Equal(t, "backup-ak", got.AccessKeyID)
	require.Equal(t, "backup-sk", got.SecretAccessKey, "the backup secret must be decrypted before use")
	require.True(t, got.ForcePathStyle)
	require.Equal(t, "backup-bucket", got.Bucket, "an empty bucket falls back to the backup bucket")
	require.Equal(t, "images/", got.Prefix, "images stay under their own prefix so they never collide with backups/")

	// Reusing must not duplicate the secret into a second row.
	raw, err := repo.GetValue(ctx, settingKeyImageStorageConfig)
	require.NoError(t, err)
	require.NotContains(t, raw, "backup-sk")
	require.NotContains(t, raw, "enc:")
}

func TestImageStorageSettingsOwnCredentialsAreEncryptedAndMasked(t *testing.T) {
	svc, repo, built := newImageStorageFixture(t, config.ImageStorageConfig{})
	ctx := context.Background()

	saved, err := svc.Update(ctx, ImageStorageSettings{
		Enabled: true, Bucket: "my-images",
		Endpoint:    "https://acct.r2.cloudflarestorage.com",
		AccessKeyID: "ak", SecretAccessKey: "super-secret",
	})
	require.NoError(t, err)
	require.Empty(t, saved.SecretAccessKey, "the response must never echo the secret back")

	raw, err := repo.GetValue(ctx, settingKeyImageStorageConfig)
	require.NoError(t, err)
	require.NotContains(t, raw, `"secret_access_key":"super-secret"`, "the secret must be encrypted at rest")
	require.Contains(t, raw, "enc:super-secret")

	fetched, err := svc.Get(ctx)
	require.NoError(t, err)
	require.Empty(t, fetched.SecretAccessKey)
	require.True(t, svc.SecretConfigured(ctx))

	_, enabled := resolveImageStorageForTest(svc)
	require.True(t, enabled)
	require.Equal(t, "super-secret", (*built)[0].SecretAccessKey, "the stored secret must be decrypted before use")

	// An update that omits the secret keeps the stored one rather than wiping it.
	_, err = svc.Update(ctx, ImageStorageSettings{
		Enabled: true, Bucket: "my-images",
		Endpoint: "https://acct.r2.cloudflarestorage.com", AccessKeyID: "ak",
	})
	require.NoError(t, err)
	resolveImageStorageForTest(svc)
	require.Equal(t, "super-secret", (*built)[1].SecretAccessKey)
}

// Persisting the service's own S3 secret must be refused when the encryption key
// is auto-generated, otherwise the ciphertext cannot be decrypted after a
// restart (#4524). Reusing the backup credentials stays allowed because it does
// not persist a second copy of the secret.
func TestImageStorageSettingsRejectSecretWithEphemeralKey(t *testing.T) {
	svc, repo, built := newImageStorageFixtureWithKey(t, config.ImageStorageConfig{}, false)
	ctx := context.Background()

	_, err := svc.Update(ctx, ImageStorageSettings{
		Enabled: true, Bucket: "my-images",
		Endpoint:    "https://acct.r2.cloudflarestorage.com",
		AccessKeyID: "ak", SecretAccessKey: "super-secret",
	})
	require.ErrorIs(t, err, ErrSecretEncryptionKeyNotConfigured)

	raw, _ := repo.GetValue(ctx, settingKeyImageStorageConfig)
	require.Empty(t, raw, "nothing must be persisted when the secret is rejected")
	require.Empty(t, *built)

	// Reusing backup credentials does not persist a secret, so it stays allowed.
	seedBackupS3(t, repo, BackupS3Config{
		Endpoint: "https://acct.r2.cloudflarestorage.com", Region: "auto",
		Bucket: "backup-bucket", AccessKeyID: "ak", SecretAccessKey: "sk", Prefix: "backups/",
	})
	_, err = svc.Update(ctx, ImageStorageSettings{Enabled: true, ReuseBackupS3: true})
	require.NoError(t, err)
}

func TestImageStorageSettingsIncompleteStaysDisabled(t *testing.T) {
	svc, _, built := newImageStorageFixture(t, config.ImageStorageConfig{})
	ctx := context.Background()

	_, err := svc.Update(ctx, ImageStorageSettings{Enabled: true, Bucket: "my-images"})
	require.NoError(t, err)

	_, enabled := resolveImageStorageForTest(svc)
	require.False(t, enabled, "missing credentials must not enable the feature")
	require.Empty(t, *built, "no client is built from an incomplete configuration")
}

// Deployments that already enabled the feature through config.yaml must keep
// working after the setting moves into the database.
func TestImageStorageSettingsFallBackToConfigFile(t *testing.T) {
	svc, _, built := newImageStorageFixture(t, config.ImageStorageConfig{
		Enabled: true, Endpoint: "https://acct.r2.cloudflarestorage.com", Region: "auto",
		Bucket: "yaml-bucket", AccessKeyID: "yaml-ak", SecretAccessKey: "yaml-sk",
		Prefix: "images/", MaxDownloadByte: 1024,
	})

	_, enabled := resolveImageStorageForTest(svc)
	require.True(t, enabled, "config.yaml still enables the feature when nothing is stored yet")
	require.Equal(t, "yaml-bucket", (*built)[0].Bucket)

	fetched, err := svc.Get(context.Background())
	require.NoError(t, err)
	require.True(t, fetched.Enabled)
	require.Equal(t, "yaml-bucket", fetched.Bucket)
	require.Empty(t, fetched.SecretAccessKey)
}
