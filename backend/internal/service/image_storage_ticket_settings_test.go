//go:build unit

package service

import (
	"context"
	"errors"
	"sync/atomic"
	"testing"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/config"
	"github.com/stretchr/testify/require"
)

func ticketStorageSettings() ImageStorageSettings {
	return ImageStorageSettings{
		TicketAttachmentsEnabled: true, Bucket: "private-images", Endpoint: "https://s3.test",
		Region: "auto", AccessKeyID: "ak", SecretAccessKey: "secret", Prefix: "images/",
	}
}

func TestTicketStorageIndependentSwitchAndDisabledReads(t *testing.T) {
	settings, _, _ := newImageStorageFixture(t, config.ImageStorageConfig{})
	ctx := context.Background()
	in := ticketStorageSettings()
	_, err := settings.Update(ctx, in)
	require.NoError(t, err)
	cfg, err := settings.TicketStorageConfig(ctx, true)
	require.NoError(t, err)
	require.Equal(t, "tickets/v1/", cfg.Prefix)
	require.Equal(t, "secret", cfg.SecretAccessKey)
	require.Empty(t, cfg.PublicBaseURL)
	_, enabled := resolveImageStorageForTest(settings)
	require.False(t, enabled, "enabling screenshots must not enable native async images")

	in.TicketAttachmentsEnabled = false
	in.SecretAccessKey = ""
	_, err = settings.Update(ctx, in)
	require.NoError(t, err)
	_, err = settings.TicketStorageConfig(ctx, true)
	require.ErrorIs(t, err, ErrTicketAttachmentsDisabled)
	cfg, err = settings.TicketStorageConfig(ctx, false)
	require.NoError(t, err, "disabling uploads must preserve reads and cleanup")
	require.Equal(t, "private-images", cfg.Bucket)
	require.Equal(t, "tickets/v1/", cfg.Prefix)
}

func TestTicketStorageRejectsPublicAndIncompleteTargets(t *testing.T) {
	settings, repo, _ := newImageStorageFixture(t, config.ImageStorageConfig{})
	ctx := context.Background()
	in := ticketStorageSettings()
	in.PublicBaseURL = "https://public.test"
	_, err := settings.Update(ctx, in)
	require.ErrorIs(t, err, ErrTicketStorageUnavailable)
	require.ErrorIs(t, settings.TestConnection(ctx, in), ErrTicketStorageUnavailable)
	_, err = settings.Update(ctx, ImageStorageSettings{TicketAttachmentsEnabled: true})
	require.ErrorIs(t, err, ErrTicketStorageUnavailable)
	// Runtime validation also rejects settings written outside the admin form.
	require.NoError(t, repo.Set(ctx, settingKeyImageStorageConfig,
		`{"ticket_attachments_enabled":true,"bucket":"private-images","access_key_id":"ak","secret_access_key":"enc:secret","public_base_url":"https://public.test"}`))
	_, err = settings.TicketStorageConfig(ctx, true)
	require.ErrorIs(t, err, ErrTicketStorageUnavailable)
}

func TestTicketStorageExistingAttachmentsPinTargetAndPrivacy(t *testing.T) {
	settings, _, _ := newImageStorageFixture(t, config.ImageStorageConfig{})
	ctx := context.Background()
	in := ticketStorageSettings()
	_, err := settings.Update(ctx, in)
	require.NoError(t, err)
	settings.SetTicketStorageUsage(func(context.Context) (bool, error) { return true, nil })
	in.TicketAttachmentsEnabled = false
	in.SecretAccessKey = ""
	_, err = settings.Update(ctx, in)
	require.NoError(t, err, "uploads may be disabled without stranding existing screenshots")
	in.Bucket = "different-bucket"
	_, err = settings.Update(ctx, in)
	require.ErrorIs(t, err, ErrTicketStorageTargetInUse)
	in.Bucket = "private-images"
	in.PublicBaseURL = "https://public.test"
	_, err = settings.Update(ctx, in)
	require.ErrorIs(t, err, ErrTicketStorageUnavailable)
	in.PublicBaseURL = ""
	in.AccessKeyID = ""
	_, err = settings.Update(ctx, in)
	require.ErrorIs(t, err, ErrTicketStorageUnavailable, "existing screenshots must retain usable credentials even when uploads are off")
	in.AccessKeyID = "ak"
	in.SecretAccessKey = "rotated"
	_, err = settings.Update(ctx, in)
	require.NoError(t, err, "credential rotation on the same target remains supported")
	settings.SetTicketStorageUsage(func(context.Context) (bool, error) { return false, errors.New("database unavailable") })
	_, err = settings.Update(ctx, in)
	require.ErrorContains(t, err, "database unavailable", "usage query failures must not allow target changes")
}

func TestTicketStorageReservationSerializesTargetChangesAcrossInstances(t *testing.T) {
	settings, repo, _ := newImageStorageFixture(t, config.ImageStorageConfig{})
	ctx := context.Background()
	in := ticketStorageSettings()
	_, err := settings.Update(ctx, in)
	require.NoError(t, err)
	targetLock := &galleryTestRepo{}
	settings.SetGalleryRepository(targetLock)
	other := NewImageStorageSettingService(repo, reversibleEncryptor{}, settings.backup, nil, config.ImageStorageConfig{})
	other.SetGalleryRepository(targetLock)
	var hasPending atomic.Bool
	other.SetTicketStorageUsage(func(context.Context) (bool, error) { return hasPending.Load(), nil })
	_, release, err := settings.AcquireTicketStorage(ctx, true)
	require.NoError(t, err)
	defer release()
	in.Bucket = "different-bucket"
	changed := make(chan error, 1)
	go func() { _, err := other.Update(ctx, in); changed <- err }()
	select {
	case err := <-changed:
		t.Fatalf("target update overtook first attachment reservation: %v", err)
	case <-time.After(30 * time.Millisecond):
	}
	hasPending.Store(true)
	release()
	release() // release is safe when deferred in addition to an early release
	require.ErrorIs(t, <-changed, ErrTicketStorageTargetInUse)
}

func TestTicketStorageBackupTargetGuard(t *testing.T) {
	settings, repo, _ := newImageStorageFixture(t, config.ImageStorageConfig{})
	ctx := context.Background()
	backupCfg := BackupS3Config{Endpoint: "https://s3.test", Region: "auto", Bucket: "backups", AccessKeyID: "ak", SecretAccessKey: "secret"}
	seedBackupS3(t, repo, backupCfg)
	_, err := settings.Update(ctx, ImageStorageSettings{TicketAttachmentsEnabled: true, ReuseBackupS3: true, Bucket: "private-images"})
	require.NoError(t, err)
	settings.SetTicketStorageUsage(func(context.Context) (bool, error) { return true, nil })
	backupCfg.Endpoint = "https://different-s3.test"
	_, err = settings.backup.UpdateS3Config(ctx, backupCfg)
	require.ErrorIs(t, err, ErrTicketStorageTargetInUse)
	backupCfg.Endpoint = "https://s3.test"
	backupCfg.AccessKeyID = "rotated"
	_, err = settings.backup.UpdateS3Config(ctx, backupCfg)
	require.NoError(t, err)
}

type ticketPrefixProbeStorage struct {
	recordingStorage
	prefixes []string
}

func (s *ticketPrefixProbeStorage) ProbeReadWriteDeletePrefix(_ context.Context, prefix string) error {
	s.prefixes = append(s.prefixes, prefix)
	return nil
}

func TestTicketStorageConnectionTestsItsOwnObjectPrefix(t *testing.T) {
	settings, _, _ := newImageStorageFixture(t, config.ImageStorageConfig{})
	storage := &ticketPrefixProbeStorage{}
	settings.factory = func(context.Context, *config.ImageStorageConfig) (ImageStorage, error) { return storage, nil }
	in := ticketStorageSettings()
	require.NoError(t, settings.TestConnection(context.Background(), in))
	require.Equal(t, []string{"tickets/v1/"}, storage.prefixes)
	require.Zero(t, storage.probeCalls, "ticket-only configuration must not require access to the Studio prefix")
	in.StudioGalleryEnabled = true
	require.NoError(t, settings.TestConnection(context.Background(), in))
	require.Equal(t, 1, storage.probeCalls, "enabling both features tests both prefixes")
}
