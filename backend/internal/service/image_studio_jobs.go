package service

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"sync"
	"time"
	"unicode"
	"unicode/utf8"

	"github.com/Wei-Shaw/sub2api/internal/config"
)

const (
	StudioJobQueued     = "queued"
	StudioJobGenerating = "generating"
	StudioJobSaving     = "saving"
	StudioJobCompleted  = "completed"
	StudioJobPartial    = "partial"
	StudioJobFailed     = "failed"
	StudioJobUnknown    = "unknown"
	StudioJobCancelled  = "cancelled"
	studioJobMaxRequest = 64 << 10
)

var (
	ErrStudioJobInvalid      = errors.New("invalid studio job")
	ErrStudioJobConflict     = errors.New("studio job conflicts with existing task")
	ErrStudioJobNotFound     = errors.New("studio job not found")
	ErrStudioJobUnavailable  = errors.New("studio job storage is unavailable")
	ErrStudioJobQueueFull    = errors.New("too many active studio jobs")
	ErrStudioJobRouteChanged = errors.New("studio model route changed after admission")
)

// StudioJobSpec holds only parameters and gallery object IDs. It must never
// contain image bytes, a browser JWT or a plaintext API key.
type StudioJobSpec struct {
	ProviderProfile      string                    `json:"provider_profile,omitempty"`
	AdmissionProfiles    []ImageStudioModelProfile `json:"admission_profiles,omitempty"`
	Protocol             string                    `json:"protocol"`
	Model                string                    `json:"model"`
	Prompt               string                    `json:"prompt"`
	Params               json.RawMessage           `json:"params"`
	InputImageIDs        []string                  `json:"input_image_ids"`
	RequestInputImageIDs []string                  `json:"request_input_image_ids"`
	MaskImageID          string                    `json:"mask_image_id,omitempty"`
	MaskTargetImageID    string                    `json:"mask_target_image_id,omitempty"`
	Postprocess          string                    `json:"postprocess"`
	OutputFormat         string                    `json:"output_format,omitempty"`
	OutputCompression    *int                      `json:"output_compression,omitempty"`
	ImageCount           int                       `json:"image_count"`
}

type StudioJobSubmit struct {
	TaskID string          `json:"task_id"`
	Task   json.RawMessage `json:"task"`
	Spec   StudioJobSpec   `json:"spec"`
}

// Only fields used by the current Studio task view may be persisted. This
// prevents arbitrary client JSON (including credentials) entering PostgreSQL
// and later being reflected by the job query API.
type studioSubmittedTask struct {
	ID                string          `json:"id"`
	Prompt            string          `json:"prompt"`
	Params            json.RawMessage `json:"params"`
	APIKind           string          `json:"apiKind,omitempty"`
	ModelID           string          `json:"modelId,omitempty"`
	ModelName         string          `json:"modelName,omitempty"`
	APIModel          string          `json:"apiModel,omitempty"`
	TransparentOutput bool            `json:"transparentOutput,omitempty"`
	TransparentPrompt string          `json:"transparentPrompt,omitempty"`
	Execution         string          `json:"execution,omitempty"`
	InputImageIDs     []string        `json:"inputImageIds"`
	EditMode          string          `json:"editMode,omitempty"`
	MaskTargetImageID *string         `json:"maskTargetImageId"`
	MaskImageID       *string         `json:"maskImageId"`
	OutputImages      []string        `json:"outputImages"`
	Status            string          `json:"status"`
	Error             *string         `json:"error"`
	CreatedAt         int64           `json:"createdAt"`
	FinishedAt        *int64          `json:"finishedAt"`
	Elapsed           *int64          `json:"elapsed"`
}

type StudioJobCall struct {
	Ordinal        int        `json:"ordinal"`
	Status         string     `json:"status"`
	Attempt        int        `json:"attempt"`
	OutputImageIDs []string   `json:"output_image_ids,omitempty"`
	RawImageIDs    []string   `json:"raw_image_ids,omitempty"`
	ErrorCode      string     `json:"error_code,omitempty"`
	ErrorMessage   string     `json:"error_message,omitempty"`
	BillingState   string     `json:"billing_state"`
	LeaseToken     string     `json:"-"`
	LeaseUntil     time.Time  `json:"-"`
	SentAt         *time.Time `json:"-"`
	SpoolPath      string     `json:"-"`
	SpoolOwner     string     `json:"-"`
	UpdatedAt      time.Time  `json:"-"`
}

type StudioJob struct {
	StorageEndpoint string          `json:"-"`
	StorageRegion   string          `json:"-"`
	StorageBucket   string          `json:"-"`
	UserID          int64           `json:"-"`
	TaskID          string          `json:"task_id"`
	APIKeyID        int64           `json:"-"`
	Status          string          `json:"status"`
	Task            json.RawMessage `json:"task"`
	Spec            StudioJobSpec   `json:"-"`
	Calls           []StudioJobCall `json:"calls"`
	CreatedAt       time.Time       `json:"created_at"`
	UpdatedAt       time.Time       `json:"updated_at"`
	DeletedAt       *time.Time      `json:"-"`
}

type StudioJobPage struct {
	Items      []StudioJob `json:"items"`
	NextCursor *string     `json:"next_cursor"`
}

type StudioJobCreate struct {
	UserID          int64
	APIKeyID        int64
	TaskID          string
	Digest          string
	Task            json.RawMessage
	Spec            StudioJobSpec
	CallCount       int
	StorageEndpoint string
	StorageRegion   string
	StorageBucket   string
}

type StudioJobClaim struct {
	StudioJob
	Call       StudioJobCall
	SpoolOwner string
}

type StudioJobRepository interface {
	Create(ctx context.Context, in StudioJobCreate) (*StudioJob, error)
	Get(ctx context.Context, userID int64, taskID string) (*StudioJob, error)
	List(ctx context.Context, userID int64, cursor string, limit int, active bool) (StudioJobPage, error)
	Claim(ctx context.Context, spoolOwner string) (*StudioJobClaim, error)
	MarkDispatched(ctx context.Context, claim StudioJobClaim) error
	RecoverUnsent(ctx context.Context, claim StudioJobClaim) error
	MarkSaving(ctx context.Context, claim StudioJobClaim, spoolPath, spoolOwner string) error
	RecoverSaving(ctx context.Context, claim StudioJobClaim, spoolPath, spoolOwner string) error
	ExtendLease(ctx context.Context, claim StudioJobClaim) error
	CancelClaim(ctx context.Context, claim StudioJobClaim) error
	CompleteCall(ctx context.Context, claim StudioJobClaim, outputs, raws []string, billingState string) error
	FailCall(ctx context.Context, claim StudioJobClaim, code, message string) error
	RetrySaving(ctx context.Context, claim StudioJobClaim, delay time.Duration, code string) error
	Finalize(ctx context.Context, userID int64, taskID, status string, task json.RawMessage) error
	Cancel(ctx context.Context, userID int64, taskID string) error
	ExpiredGenerating(ctx context.Context, limit int) ([]StudioJobClaim, error)
	ExpiredSavingOtherOwner(ctx context.Context, owner string, limit int) ([]StudioJobClaim, error)
	MarkUnknown(ctx context.Context, claim StudioJobClaim) error
	MarkUncertain(ctx context.Context, claim StudioJobClaim) error
	MarkSavingLost(ctx context.Context, claim StudioJobClaim, expiredOnly bool) error
	HasActive(ctx context.Context) (bool, error)
	BillingRecorded(ctx context.Context, requestID string, apiKeyID int64) (bool, error)
	PendingBilling(ctx context.Context, limit int) ([]StudioJobClaim, error)
	SetBillingState(ctx context.Context, claim StudioJobClaim, state string) error
	ReleaseInputs(ctx context.Context, userID int64, taskID string) ([]GalleryAsset, error)
	FinalizationCandidates(ctx context.Context, limit int) ([]StudioJob, error)
	CleanupCandidates(ctx context.Context, limit int) ([]StudioJob, error)
}

type StudioJobService struct {
	router        http.Handler
	spoolDir      string
	owner         string
	mu            sync.Mutex
	cancel        context.CancelFunc
	workers       sync.WaitGroup
	spoolMu       sync.Mutex
	spoolUsed     int64
	spoolReserved int64
	repo          StudioJobRepository
	gallery       *ImageStudioGalleryService
	settings      *ImageStorageSettingService
	keys          *APIKeyService
	gateway       *GatewayService
}

func NewStudioJobService(repo StudioJobRepository, gallery *ImageStudioGalleryService, settings *ImageStorageSettingService, keys *APIKeyService, gateway *GatewayService) *StudioJobService {
	return &StudioJobService{repo: repo, gallery: gallery, settings: settings, keys: keys, gateway: gateway}
}

// Enabled is the admission gate for new durable jobs. The worker itself keeps
// running after this switch is turned off so previously accepted jobs finish.
func (s *StudioJobService) Enabled(ctx context.Context) bool {
	if s == nil || s.repo == nil || s.gallery == nil || s.settings == nil {
		return false
	}
	s.mu.Lock()
	running := s.router != nil && s.cancel != nil
	s.mu.Unlock()
	if !running {
		return false
	}
	settings, err := s.settings.Get(ctx)
	if err != nil || settings == nil || !settings.StudioJobsEnabled {
		return false
	}
	galleryEnabled, err := s.gallery.Status(ctx)
	return err == nil && galleryEnabled
}

func (s *StudioJobService) Submit(ctx context.Context, userID, apiKeyID int64, in StudioJobSubmit) (*StudioJob, error) {
	if s == nil || s.repo == nil || s.gallery == nil || s.settings == nil {
		return nil, ErrStudioJobUnavailable
	}
	if !s.Enabled(ctx) {
		return nil, ErrGalleryDisabled
	}
	if userID <= 0 || apiKeyID <= 0 || !galleryIDValid(in.TaskID) {
		return nil, ErrStudioJobInvalid
	}
	if err := validateStudioJobSpec(in.Spec); err != nil {
		return nil, err
	}
	var task studioSubmittedTask
	if len(in.Task) == 0 || len(in.Task) > 32<<10 {
		return nil, ErrStudioJobInvalid
	}
	decoder := json.NewDecoder(bytes.NewReader(in.Task))
	decoder.DisallowUnknownFields()
	if decoder.Decode(&task) != nil || task.ID != in.TaskID || task.Status != "running" ||
		len(task.OutputImages) != 0 || task.Error != nil || task.FinishedAt != nil || task.Elapsed != nil ||
		len(task.Prompt) > 16<<10 || len(task.TransparentPrompt) > 16<<10 ||
		len(task.ModelID) > 128 || len(task.ModelName) > 256 || task.APIModel != in.Spec.Model ||
		!equalStudioIDs(task.InputImageIDs, in.Spec.InputImageIDs) ||
		stringOrEmpty(task.MaskImageID) != in.Spec.MaskImageID ||
		stringOrEmpty(task.MaskTargetImageID) != in.Spec.MaskTargetImageID ||
		(task.APIKind != "openai" && task.APIKind != "gemini") ||
		(task.APIKind == "gemini") != (in.Spec.Protocol == "gemini") ||
		(task.EditMode != "" && task.EditMode != "region" && task.EditMode != "erase") {
		return nil, ErrStudioJobInvalid
	}
	if task.Execution != "" && task.Execution != "cloud" {
		return nil, ErrStudioJobInvalid
	}
	task.Execution = "cloud"
	if task.CreatedAt <= 0 || task.CreatedAt > time.Now().Add(5*time.Minute).UnixMilli() {
		return nil, ErrStudioJobInvalid
	}
	if err := validateStudioTaskParams(task.Params, in.Spec.ImageCount); err != nil {
		return nil, err
	}
	task.OutputImages = []string{}
	canonicalTask, err := json.Marshal(task)
	if err != nil {
		return nil, ErrStudioJobInvalid
	}
	if s.gateway == nil || s.keys == nil {
		return nil, ErrStudioJobUnavailable
	}
	if existing, err := s.repo.Get(ctx, userID, in.TaskID); err == nil {
		stored := existing.Spec
		stored.AdmissionProfiles = nil
		incoming := in.Spec
		incoming.AdmissionProfiles = nil
		storedJSON, _ := json.Marshal(stored)
		incomingJSON, _ := json.Marshal(incoming)
		if existing.DeletedAt != nil || !bytes.Equal(storedJSON, incomingJSON) {
			return nil, ErrStudioJobConflict
		}
		return existing, nil
	} else if !errors.Is(err, ErrStudioJobNotFound) {
		return nil, err
	}
	key, err := s.keys.GetByID(ctx, apiKeyID)
	if err != nil || key == nil || key.UserID != userID || key.Group == nil {
		return nil, ErrStudioJobUnavailable
	}
	models, err := s.gateway.ListImageStudioModels(ctx, key.Group)
	if err != nil {
		return nil, ErrStudioJobUnavailable
	}
	profiles := studioCurrentProfiles(in.Spec, models)
	if len(profiles) == 0 {
		return nil, ErrStudioJobInvalid
	}
	if in.Spec.ProviderProfile != "" && (len(profiles) != 1 || profiles[0].Profile != in.Spec.ProviderProfile) {
		// A pre-upgrade page may still carry the old private-target profile.
		// Require a refresh instead of silently reinterpreting its parameters.
		return nil, ErrStudioJobRouteChanged
	}
	in.Spec.AdmissionProfiles = profiles
	canonicalSpec, err := json.Marshal(in.Spec)
	if err != nil || len(canonicalSpec) > studioJobMaxRequest || strings.Contains(strings.ToLower(string(canonicalSpec)), "base64,") {
		return nil, ErrStudioJobInvalid
	}
	// Admin target changes take the same advisory lock. Admission is atomic
	// with target validation and job creation, including the pre-first-asset gap.
	releaseTarget, err := s.gallery.repo.LockTarget(ctx)
	if err != nil {
		return nil, err
	}
	defer releaseTarget()
	currentSettings, err := s.settings.Get(ctx)
	if err != nil || currentSettings == nil || !currentSettings.StudioJobsEnabled {
		return nil, ErrGalleryDisabled
	}
	cfg, enabled, err := s.settings.StudioGalleryConfig(ctx)
	if err != nil {
		return nil, fmt.Errorf("%w: gallery settings: %v", ErrStudioJobUnavailable, err)
	}
	if !enabled || cfg == nil || !cfg.IsConfigured() {
		return nil, ErrGalleryDisabled
	}
	assets := make(map[string]*GalleryAsset)
	for _, id := range studioJobAssetIDs(in.Spec) {
		asset, err := s.gallery.repo.GetAsset(ctx, userID, id)
		if err != nil || asset == nil || asset.State != "ready" || asset.Endpoint != cfg.Endpoint || asset.Region != cfg.Region || asset.Bucket != cfg.Bucket {
			return nil, ErrStudioJobInvalid
		}
		assets[id] = asset
	}
	var inputBytes int64
	for _, id := range in.Spec.RequestInputImageIDs {
		inputBytes += assets[id].Bytes
	}
	if in.Spec.MaskImageID != "" {
		inputBytes += assets[in.Spec.MaskImageID].Bytes
	}
	if inputBytes > studioJobMaxInputBytes {
		return nil, ErrStudioJobInvalid
	}
	if in.Spec.Postprocess == "mask" {
		source, mask := assets[in.Spec.MaskTargetImageID], assets[in.Spec.MaskImageID]
		if source == nil || mask == nil || source.Width != mask.Width || source.Height != mask.Height {
			return nil, ErrStudioJobInvalid
		}
	}
	hash := sha256.Sum256(append(canonicalTask, canonicalSpec...))
	callCount := in.Spec.ImageCount
	if in.Spec.Protocol == "grok" {
		callCount = 1
	}
	return s.repo.Create(ctx, StudioJobCreate{
		UserID: userID, APIKeyID: apiKeyID, TaskID: in.TaskID, Digest: hex.EncodeToString(hash[:]),
		Task: canonicalTask, Spec: in.Spec, CallCount: callCount,
		StorageEndpoint: cfg.Endpoint, StorageRegion: cfg.Region, StorageBucket: cfg.Bucket,
	})
}

func (s *StudioJobService) Get(ctx context.Context, userID int64, taskID string) (*StudioJob, error) {
	if s == nil || s.repo == nil || userID <= 0 || !galleryIDValid(taskID) {
		return nil, ErrStudioJobInvalid
	}
	job, err := s.repo.Get(ctx, userID, taskID)
	if err != nil {
		return nil, err
	}
	if job.DeletedAt != nil {
		return nil, ErrStudioJobNotFound
	}
	return job, nil
}

func (s *StudioJobService) List(ctx context.Context, userID int64, cursor string, limit int, active bool) (StudioJobPage, error) {
	if s == nil || s.repo == nil || userID <= 0 || limit < 1 || limit > 100 {
		return StudioJobPage{}, ErrStudioJobInvalid
	}
	return s.repo.List(ctx, userID, cursor, limit, active)
}

func (s *StudioJobService) Cancel(ctx context.Context, userID int64, taskID string) error {
	if s == nil || s.repo == nil || userID <= 0 || !galleryIDValid(taskID) {
		return ErrStudioJobInvalid
	}
	if err := s.repo.Cancel(ctx, userID, taskID); err != nil {
		return err
	}
	if err := s.gallery.DeleteTask(ctx, userID, taskID); err != nil {
		return err
	}
	assets, err := s.repo.ReleaseInputs(ctx, userID, taskID)
	if errors.Is(err, ErrStudioJobNotFound) {
		return nil
	}
	if err != nil {
		return err
	}
	for _, asset := range assets {
		s.gallery.cleanupAsset(ctx, asset)
	}
	return nil
}

func validateStudioJobSpec(spec StudioJobSpec) error {
	if len(spec.AdmissionProfiles) != 0 {
		return ErrStudioJobInvalid
	}
	if spec.Protocol != "openai" && spec.Protocol != "grok" && spec.Protocol != "gemini" {
		return ErrStudioJobInvalid
	}
	if len(spec.Model) == 0 || len(spec.Model) > 128 || !utf8.ValidString(spec.Model) ||
		strings.TrimSpace(spec.Model) != spec.Model || len(spec.Prompt) > 16<<10 ||
		spec.ImageCount < 1 || spec.ImageCount > 10 {
		return ErrStudioJobInvalid
	}
	for _, ch := range spec.Model {
		if unicode.IsControl(ch) {
			return ErrStudioJobInvalid
		}
	}
	if spec.Protocol == "gemini" && !IsSafeGeminiModelPathSegment(spec.Model) {
		return ErrStudioJobInvalid
	}
	if spec.Postprocess != "none" && spec.Postprocess != "mask" && spec.Postprocess != "keyed" {
		return ErrStudioJobInvalid
	}
	if spec.Postprocess == "mask" && (spec.MaskImageID == "" || spec.MaskTargetImageID == "" || len(spec.RequestInputImageIDs) == 0) {
		return ErrStudioJobInvalid
	}
	if spec.Protocol != "openai" && spec.Postprocess == "mask" {
		return ErrStudioJobInvalid
	}
	if spec.Postprocess != "none" && spec.OutputFormat != "png" && spec.OutputFormat != "jpeg" && spec.OutputFormat != "webp" {
		return ErrStudioJobInvalid
	}
	if spec.Postprocess == "keyed" && spec.OutputFormat == "jpeg" {
		return ErrStudioJobInvalid
	}
	if spec.OutputCompression != nil && (*spec.OutputCompression < 0 || *spec.OutputCompression > 100) {
		return ErrStudioJobInvalid
	}
	if len(spec.Params) == 0 || len(spec.Params) > 16<<10 || !json.Valid(spec.Params) || spec.Params[0] != '{' {
		return ErrStudioJobInvalid
	}
	if err := validateStudioProviderParams(spec.Protocol, spec.Params, spec.ImageCount); err != nil {
		return err
	}
	if len(spec.InputImageIDs) > 20 || len(spec.RequestInputImageIDs) > 20 || len(spec.InputImageIDs) != len(spec.RequestInputImageIDs) {
		return ErrStudioJobInvalid
	}
	for _, id := range studioJobAssetIDs(spec) {
		if !galleryIDValid(id) {
			return ErrStudioJobInvalid
		}
	}
	return nil
}

func stringOrEmpty(value *string) string {
	if value == nil {
		return ""
	}
	return *value
}

func equalStudioIDs(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

func validateStudioTaskParams(raw json.RawMessage, count int) error {
	if len(raw) == 0 || len(raw) > 8<<10 || !json.Valid(raw) {
		return ErrStudioJobInvalid
	}
	var params map[string]any
	if json.Unmarshal(raw, &params) != nil || params == nil {
		return ErrStudioJobInvalid
	}
	allowed := map[string]bool{
		"resolution": true, "size": true, "quality": true, "output_format": true,
		"output_compression": true, "moderation": true, "n": true,
		"transparent_output": true, "partial_images": true,
		"aspect_ratio": true, "complexity": true,
	}
	for key, value := range params {
		if !allowed[key] || !studioScalarValid(value) {
			return ErrStudioJobInvalid
		}
	}
	n, ok := params["n"].(float64)
	if !ok || n != float64(count) {
		return ErrStudioJobInvalid
	}
	return nil
}

func validateStudioProviderParams(protocol string, raw json.RawMessage, count int) error {
	var params map[string]any
	if json.Unmarshal(raw, &params) != nil || params == nil {
		return ErrStudioJobInvalid
	}
	var allowed map[string]bool
	switch protocol {
	case "openai":
		allowed = map[string]bool{"size": true, "quality": true, "output_format": true,
			"output_compression": true, "moderation": true, "complexity": true,
			"background": true, "n": true}
	case "grok":
		allowed = map[string]bool{"resolution": true, "aspect_ratio": true,
			"quality": true, "response_format": true, "n": true}
	case "gemini":
		allowed = map[string]bool{"generationConfig": true}
	default:
		return ErrStudioJobInvalid
	}
	for key, value := range params {
		if !allowed[key] {
			return ErrStudioJobInvalid
		}
		if key == "n" {
			n, ok := value.(float64)
			if !ok || n != float64(count) {
				return ErrStudioJobInvalid
			}
			continue
		}
		if key == "generationConfig" {
			generation, ok := value.(map[string]any)
			if !ok || len(generation) < 1 || len(generation) > 2 {
				return ErrStudioJobInvalid
			}
			modalities, ok := generation["responseModalities"].([]any)
			if !ok || len(modalities) != 1 || modalities[0] != "IMAGE" {
				return ErrStudioJobInvalid
			}
			for configKey, configValue := range generation {
				if configKey == "responseModalities" {
					continue
				}
				if configKey != "imageConfig" {
					return ErrStudioJobInvalid
				}
				config, ok := configValue.(map[string]any)
				if !ok || len(config) > 2 {
					return ErrStudioJobInvalid
				}
				for name, setting := range config {
					if name != "imageSize" && name != "aspectRatio" || !studioScalarValid(setting) {
						return ErrStudioJobInvalid
					}
				}
				if len(config) == 0 {
					return ErrStudioJobInvalid
				}
			}
			continue
		}
		if !studioScalarValid(value) {
			return ErrStudioJobInvalid
		}
	}
	return nil
}

func studioScalarValid(value any) bool {
	switch v := value.(type) {
	case nil:
		return true
	case string:
		if len(v) > 128 || !utf8.ValidString(v) {
			return false
		}
		for _, ch := range v {
			if unicode.IsControl(ch) {
				return false
			}
		}
		return true
	case bool:
		return true
	case float64:
		return v >= 0 && v <= 100_000_000
	default:
		return false
	}
}

func studioJobAssetIDs(spec StudioJobSpec) []string {
	ids := make([]string, 0, len(spec.InputImageIDs)+len(spec.RequestInputImageIDs)+2)
	seen := make(map[string]struct{})
	for _, id := range append(append(append([]string{}, spec.InputImageIDs...), spec.RequestInputImageIDs...), spec.MaskImageID, spec.MaskTargetImageID) {
		if id == "" {
			continue
		}
		if _, ok := seen[id]; ok {
			continue
		}
		seen[id] = struct{}{}
		ids = append(ids, id)
	}
	return ids
}

// StorageTarget is checked at job acceptance and before each accepted-job
// archival step, so a storage switch cannot make accepted results disappear.
func (s *StudioJobService) StorageTarget(ctx context.Context) (*config.ImageStorageConfig, error) {
	if s == nil || s.settings == nil {
		return nil, ErrStudioJobUnavailable
	}
	cfg, _, err := s.settings.StudioGalleryConfig(ctx)
	return cfg, err
}
