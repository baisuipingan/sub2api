package service

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/url"
	"strings"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/config"
	infraerrors "github.com/Wei-Shaw/sub2api/internal/pkg/errors"
)

const (
	settingKeyOfficeStorageConfig = "office_storage_config"
	officeStorageBackup           = "backup"
	officeStorageImages           = "images"
	officeStorageCustom           = "custom"
	officeStoragePrefix           = "workspace/v1/"
)

var (
	ErrOfficeStorageConfigInvalid = infraerrors.BadRequest("OFFICE_STORAGE_CONFIG_INVALID", "configure a private office bucket and a valid S3 connection")
	ErrOfficeStorageTargetInUse   = infraerrors.Conflict("OFFICE_STORAGE_TARGET_IN_USE", "office files already use this storage location")
	ErrOfficeStorageProbeFailed   = infraerrors.BadRequest("OFFICE_STORAGE_PROBE_FAILED", "office storage connection check failed. Verify bucket access and signed read permissions.")
)

type OfficeStorageSettings struct {
	Source          string `json:"source"`
	Bucket          string `json:"bucket"`
	Endpoint        string `json:"endpoint"`
	Region          string `json:"region"`
	AccessKeyID     string `json:"access_key_id"`
	SecretAccessKey string `json:"secret_access_key,omitempty"`
	ForcePathStyle  bool   `json:"force_path_style"`
}

type OfficeStorageSettingsView struct {
	Config           OfficeStorageSettings `json:"config"`
	SecretConfigured bool                  `json:"secret_configured"`
	Configured       bool                  `json:"configured"`
	TargetInUse      bool                  `json:"target_in_use"`
	EffectiveBucket  string                `json:"effective_bucket,omitempty"`
}

func (s *ImageStorageSettingService) loadOfficeSettings(ctx context.Context) (*OfficeStorageSettings, error) {
	if s.settingRepo == nil {
		return nil, nil
	}
	raw, err := s.settingRepo.GetValue(ctx, settingKeyOfficeStorageConfig)
	if errors.Is(err, ErrSettingNotFound) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	if strings.TrimSpace(raw) == "" {
		return nil, nil
	}
	var settings OfficeStorageSettings
	if err := json.Unmarshal([]byte(raw), &settings); err != nil {
		return nil, fmt.Errorf("parse office storage settings: %w", err)
	}
	return &settings, nil
}

// inheritedOfficeSettings describes the connection used before office settings
// were saved separately, including the test stack's IMAGE_STORAGE_* fallback.
func inheritedOfficeSettings(images *ImageStorageSettings) OfficeStorageSettings {
	settings := OfficeStorageSettings{Source: officeStorageImages}
	if images != nil {
		settings.Bucket = images.OfficeBucket
	}
	return settings
}

func (s *ImageStorageSettingService) resolveLegacyOfficeStorage(ctx context.Context, images *ImageStorageSettings, backup *BackupS3Config) (*config.ImageStorageConfig, error) {
	if (images == nil && s.fallback.PublicBaseURL != "") || (images != nil && images.PublicBaseURL != "") {
		return nil, ErrOfficeStorageUnavailable
	}
	return s.resolveOfficeStorage(ctx, inheritedOfficeSettings(images), images, backup)
}

func (s *ImageStorageSettingService) checkOfficeImageChange(ctx context.Context, images *ImageStorageSettings) error {
	office, err := s.loadOfficeSettings(ctx)
	if err != nil {
		return err
	}
	if office != nil && office.Source != officeStorageImages {
		return nil
	}
	return s.checkOfficeDependencyChange(ctx, office, images, nil)
}

func (s *ImageStorageSettingService) checkOfficeBackupChange(ctx context.Context, backup *BackupS3Config) error {
	office, err := s.loadOfficeSettings(ctx)
	if err != nil {
		return err
	}
	if office != nil && office.Source == officeStorageCustom {
		return nil
	}
	var images *ImageStorageSettings
	if office == nil || office.Source == officeStorageImages {
		images, err = s.load(ctx)
		if err != nil {
			return err
		}
		if images == nil || !images.ReuseBackupS3 {
			return nil
		}
	}
	return s.checkOfficeDependencyChange(ctx, office, images, backup)
}

func (s *ImageStorageSettingService) checkOfficeDependencyChange(ctx context.Context, office *OfficeStorageSettings, images *ImageStorageSettings, backup *BackupS3Config) error {
	inUse, err := s.officeStorageInUse(ctx)
	if err != nil {
		return err
	}
	// Legacy image storage becomes an office dependency when it serves office
	// files or the operator has explicitly selected an office bucket.
	if !inUse && office == nil && (images == nil || images.OfficeBucket == "") {
		return nil
	}
	current, currentErr := s.officeStorageConfigLocked(ctx)
	if inUse && currentErr != nil {
		return ErrOfficeStorageUnavailable
	}
	var next *config.ImageStorageConfig
	var targetInUse error = ErrOfficeStorageTargetInUse
	if office == nil {
		targetInUse = ErrOfficeStorageUnavailable
		next, err = s.resolveLegacyOfficeStorage(ctx, images, backup)
	} else {
		next, err = s.resolveOfficeStorage(ctx, *office, images, backup)
	}
	if inUse && (err != nil || !sameGalleryStorageTarget(current, next)) {
		return targetInUse
	}
	if err != nil {
		return ErrOfficeStorageConfigInvalid
	}
	return s.verifyOfficeStorageChange(ctx, current, next)
}

func (s *ImageStorageSettingService) officeStorageInUse(ctx context.Context) (bool, error) {
	if s.activeOfficeStorage > 0 {
		return true, nil
	}
	if s.officeStorageUsage == nil {
		return false, nil
	}
	return s.officeStorageUsage(ctx)
}

// resolveOfficeStorage composes only the chosen connection fields. Passing a
// candidate backup config lets backup saves validate changes before persisting.
func (s *ImageStorageSettingService) resolveOfficeStorage(ctx context.Context, office OfficeStorageSettings, images *ImageStorageSettings, backup *BackupS3Config) (*config.ImageStorageConfig, error) {
	var cfg config.ImageStorageConfig
	switch office.Source {
	case officeStorageImages:
		if images == nil {
			cfg = s.fallback
		} else {
			resolved, err := s.toImageStorageConfigWithBackup(ctx, images, backup)
			if err != nil {
				return nil, ErrOfficeStorageUnavailable
			}
			cfg = *resolved
		}
		if cfg.PublicBaseURL != "" && (office.Bucket == "" || office.Bucket == cfg.Bucket) {
			return nil, ErrOfficeStorageUnavailable
		}
	case officeStorageBackup:
		if backup == nil {
			var err error
			backup, err = s.backupCredentials(ctx)
			if err != nil || backup == nil {
				return nil, ErrOfficeStorageUnavailable
			}
		}
		cfg.Endpoint, cfg.Region, cfg.ForcePathStyle = backup.Endpoint, backup.Region, backup.ForcePathStyle
		cfg.AccessKeyID, cfg.SecretAccessKey = backup.AccessKeyID, backup.SecretAccessKey
	case officeStorageCustom:
		cfg.Endpoint, cfg.Region, cfg.ForcePathStyle = office.Endpoint, office.Region, office.ForcePathStyle
		cfg.AccessKeyID = office.AccessKeyID
		if s.encryptor == nil || office.SecretAccessKey == "" {
			return nil, ErrOfficeStorageUnavailable
		}
		secret, err := s.encryptor.Decrypt(office.SecretAccessKey)
		if err != nil {
			return nil, ErrOfficeStorageUnavailable
		}
		cfg.SecretAccessKey = secret
	default:
		return nil, ErrOfficeStorageConfigInvalid
	}
	if office.Bucket != "" {
		cfg.Bucket = office.Bucket
	}
	if !cfg.IsConfigured() {
		return nil, ErrOfficeStorageUnavailable
	}
	cfg.Enabled = true
	cfg.Prefix = officeStoragePrefix
	cfg.PublicBaseURL = ""
	return &cfg, nil
}

func (s *ImageStorageSettingService) officeSettingsView(ctx context.Context, office OfficeStorageSettings, images *ImageStorageSettings, inUse bool) *OfficeStorageSettingsView {
	view := &OfficeStorageSettingsView{Config: office, SecretConfigured: office.SecretAccessKey != "", TargetInUse: inUse}
	view.Config.SecretAccessKey = ""
	if cfg, err := s.resolveOfficeStorage(ctx, office, images, nil); err == nil {
		view.Configured = true
		view.EffectiveBucket = cfg.Bucket
	}
	return view
}

func (s *ImageStorageSettingService) GetOfficeStorageSettings(ctx context.Context) (*OfficeStorageSettingsView, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	office, err := s.loadOfficeSettings(ctx)
	if err != nil {
		return nil, err
	}
	images, err := s.load(ctx)
	if err != nil {
		return nil, err
	}
	inUse, err := s.officeStorageInUse(ctx)
	if err != nil {
		return nil, err
	}
	if office != nil {
		return s.officeSettingsView(ctx, *office, images, inUse), nil
	}
	legacy := inheritedOfficeSettings(images)
	view := s.officeSettingsView(ctx, legacy, images, inUse)
	if _, err := s.resolveLegacyOfficeStorage(ctx, images, nil); err != nil {
		view.Configured = false
		view.EffectiveBucket = ""
	}
	if !view.Configured && !inUse && legacy.Bucket == "" {
		view.Config.Source = officeStorageBackup
	}
	return view, nil
}

func (s *ImageStorageSettingService) prepareOfficeStorage(in OfficeStorageSettings, stored *OfficeStorageSettings) (OfficeStorageSettings, error) {
	in.Bucket = strings.TrimSpace(in.Bucket)
	if (in.Bucket == "" && in.Source != officeStorageImages) || strings.ContainsAny(in.Bucket, "/\\?# \t\r\n") {
		return in, ErrOfficeStorageConfigInvalid
	}
	switch in.Source {
	case officeStorageBackup, officeStorageImages:
		return OfficeStorageSettings{Source: in.Source, Bucket: in.Bucket}, nil
	case officeStorageCustom:
		in.Endpoint = strings.TrimRight(strings.TrimSpace(in.Endpoint), "/")
		endpoint, err := url.Parse(in.Endpoint)
		if err != nil || endpoint.Hostname() == "" || (endpoint.Scheme != "https" && endpoint.Scheme != "http") || endpoint.User != nil || endpoint.RawQuery != "" || endpoint.ForceQuery || endpoint.Fragment != "" {
			return in, ErrOfficeStorageConfigInvalid
		}
		in.Region = strings.TrimSpace(in.Region)
		if in.Region == "" {
			in.Region = "auto"
		}
		in.AccessKeyID = strings.TrimSpace(in.AccessKeyID)
		in.SecretAccessKey = strings.TrimSpace(in.SecretAccessKey)
		if in.SecretAccessKey == "" && stored != nil && stored.Source == officeStorageCustom && stored.AccessKeyID == in.AccessKeyID && stored.Endpoint == in.Endpoint {
			in.SecretAccessKey = stored.SecretAccessKey
			return in, nil
		}
		if in.AccessKeyID == "" || in.SecretAccessKey == "" {
			return in, ErrOfficeStorageConfigInvalid
		}
		if s.backup == nil || !s.backup.EncryptionKeyConfigured() || s.encryptor == nil {
			return in, ErrSecretEncryptionKeyNotConfigured
		}
		in.SecretAccessKey, err = s.encryptor.Encrypt(in.SecretAccessKey)
		if err != nil {
			return in, fmt.Errorf("encrypt office storage secret: %w", err)
		}
		return in, nil
	default:
		return in, ErrOfficeStorageConfigInvalid
	}
}

func (s *ImageStorageSettingService) UpdateOfficeStorageSettings(ctx context.Context, in OfficeStorageSettings) (*OfficeStorageSettingsView, error) {
	if s.galleryRepo != nil {
		release, err := s.galleryRepo.LockTarget(ctx)
		if err != nil {
			return nil, err
		}
		defer release()
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	stored, err := s.loadOfficeSettings(ctx)
	if err != nil {
		return nil, err
	}
	in, err = s.prepareOfficeStorage(in, stored)
	if err != nil {
		return nil, err
	}
	images, err := s.load(ctx)
	if err != nil {
		return nil, err
	}
	next, err := s.resolveOfficeStorage(ctx, in, images, nil)
	if err != nil {
		return nil, ErrOfficeStorageConfigInvalid
	}
	inUse, err := s.officeStorageInUse(ctx)
	if err != nil {
		return nil, err
	}
	current, currentErr := s.officeStorageConfigLocked(ctx)
	if inUse && (currentErr != nil || !sameGalleryStorageTarget(current, next)) {
		return nil, ErrOfficeStorageTargetInUse
	}
	if stored == nil || stored.Source != in.Source {
		current = nil
	}
	if err := s.verifyOfficeStorageChange(ctx, current, next); err != nil {
		return nil, err
	}
	raw, err := json.Marshal(in)
	if err != nil {
		return nil, err
	}
	if err := s.settingRepo.Set(ctx, settingKeyOfficeStorageConfig, string(raw)); err != nil {
		return nil, err
	}
	return s.officeSettingsView(ctx, in, images, inUse), nil
}

func (s *ImageStorageSettingService) TestOfficeStorageConnection(ctx context.Context, in OfficeStorageSettings) error {
	stored, err := s.loadOfficeSettings(ctx)
	if err != nil {
		return err
	}
	in, err = s.prepareOfficeStorage(in, stored)
	if err != nil {
		return err
	}
	images, err := s.load(ctx)
	if err != nil {
		return err
	}
	cfg, err := s.resolveOfficeStorage(ctx, in, images, nil)
	if err != nil {
		return ErrOfficeStorageConfigInvalid
	}
	return s.probeOfficeStorage(ctx, cfg)
}

func (s *ImageStorageSettingService) verifyOfficeStorageChange(ctx context.Context, current, next *config.ImageStorageConfig) error {
	if sameGalleryStorageTarget(current, next) && current.AccessKeyID == next.AccessKeyID && current.SecretAccessKey == next.SecretAccessKey {
		return nil
	}
	if err := s.probeOfficeStorage(ctx, next); err != nil {
		return ErrOfficeStorageProbeFailed.WithCause(err)
	}
	return nil
}

func (s *ImageStorageSettingService) probeOfficeStorage(ctx context.Context, cfg *config.ImageStorageConfig) error {
	if s.factory == nil {
		return ErrOfficeStorageUnavailable
	}
	ctx, cancel := context.WithTimeout(ctx, 20*time.Second)
	defer cancel()
	store, err := s.factory(ctx, cfg)
	if err != nil {
		return err
	}
	prober, ok := store.(interface {
		ProbeReadWriteDeletePrefix(context.Context, string) error
	})
	if !ok {
		return ErrOfficeStorageUnavailable
	}
	if err := prober.ProbeReadWriteDeletePrefix(ctx, officeStoragePrefix); err != nil {
		return err
	}
	signedProber, ok := store.(interface {
		ProbeSignedOfficeRead(context.Context, string) error
	})
	if !ok {
		return ErrOfficeStorageUnavailable
	}
	return signedProber.ProbeSignedOfficeRead(ctx, officeStoragePrefix)
}
