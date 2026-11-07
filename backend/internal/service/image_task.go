package service

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"time"

	infraerrors "github.com/Wei-Shaw/sub2api/internal/pkg/errors"
	"github.com/Wei-Shaw/sub2api/internal/pkg/logger"
	"github.com/google/uuid"
	"go.uber.org/zap"
)

const (
	ImageTaskStatusProcessing = "processing"
	ImageTaskStatusCompleted  = "completed"
	ImageTaskStatusFailed     = "failed"

	defaultImageTaskTTL              = 24 * time.Hour
	defaultImageTaskExecutionTimeout = 30 * time.Minute
	maxImageTaskResultBytes          = 256 << 10
	maxImageTaskErrorBytes           = 4 << 10
	imageTaskOffloadAttempts         = 3
	imageTaskFailurePersistTimeout   = 15 * time.Second
)

var (
	ErrImageTaskNotFound    = infraerrors.New(http.StatusNotFound, "IMAGE_TASK_NOT_FOUND", "image task not found")
	ErrImageTaskForbidden   = infraerrors.New(http.StatusForbidden, "IMAGE_TASK_FORBIDDEN", "image task does not belong to this API key")
	ErrImageTaskUnavailable = infraerrors.New(http.StatusServiceUnavailable, "IMAGE_TASK_UNAVAILABLE", "image task storage is unavailable")
	ErrImageTaskDisabled    = infraerrors.New(http.StatusNotFound, "IMAGE_TASK_DISABLED", "async image tasks are not enabled")
)

type imageTaskStorageBinding struct {
	uploader *ImageResultUploader
	release  func()
}

// ImageTaskRecord is the private Redis representation of an asynchronous image
// request. Ownership fields are intentionally omitted from the public view.
type ImageTaskRecord struct {
	ID          string          `json:"id"`
	UserID      int64           `json:"user_id"`
	APIKeyID    int64           `json:"api_key_id"`
	Status      string          `json:"status"`
	HTTPStatus  int             `json:"http_status,omitempty"`
	Result      json.RawMessage `json:"result,omitempty"`
	Error       json.RawMessage `json:"error,omitempty"`
	CreatedAt   int64           `json:"created_at"`
	CompletedAt *int64          `json:"completed_at,omitempty"`
	ExpiresAt   int64           `json:"expires_at"`
}

// ImageTask is the API-safe task representation returned to callers.
type ImageTask struct {
	ID          string          `json:"id"`
	TaskID      string          `json:"task_id"`
	Object      string          `json:"object"`
	Status      string          `json:"status"`
	HTTPStatus  int             `json:"http_status,omitempty"`
	ImageURL    string          `json:"image_url,omitempty"`
	Result      json.RawMessage `json:"result,omitempty"`
	Error       json.RawMessage `json:"error,omitempty"`
	CreatedAt   int64           `json:"created_at"`
	CompletedAt *int64          `json:"completed_at,omitempty"`
	ExpiresAt   int64           `json:"expires_at"`
}

type ImageTaskOwner struct {
	UserID   int64
	APIKeyID int64
}

type ImageTaskStore interface {
	Save(ctx context.Context, task *ImageTaskRecord, ttl time.Duration) error
	Get(ctx context.Context, id string) (*ImageTaskRecord, error)
}

// ImageStorageResolver acquires a binding for one accepted in-process task.
// The release callback frees its target-change guard when execution finishes.
// Admin setting changes affect later acquisitions, not the accepted uploader.
type ImageStorageResolver func() (uploader *ImageResultUploader, release func(), enabled bool)

type ImageTaskService struct {
	store            ImageTaskStore
	uploader         *ImageResultUploader
	enabled          bool
	resolve          ImageStorageResolver
	ttl              time.Duration
	executionTimeout time.Duration
	// Bindings live only as long as the in-process async execution. Keeping the
	// accepted uploader here prevents an admin switch from changing its target
	// between the gateway response and the object-storage upload.
	bindings sync.Map // task ID -> *imageTaskStorageBinding
}

func NewImageTaskService(store ImageTaskStore) *ImageTaskService {
	return NewImageTaskServiceWithOptions(store, defaultImageTaskTTL, defaultImageTaskExecutionTimeout)
}

func NewImageTaskServiceWithOptions(store ImageTaskStore, ttl, executionTimeout time.Duration) *ImageTaskService {
	if ttl <= 0 {
		ttl = defaultImageTaskTTL
	}
	if executionTimeout <= 0 {
		executionTimeout = defaultImageTaskExecutionTimeout
	}
	return &ImageTaskService{store: store, ttl: ttl, executionTimeout: executionTimeout}
}

// NewImageTaskServiceWithUploader 构造一个已启用的图片任务服务：结果会先经 uploader
// 转存到对象存储再落 Redis。uploader 为 nil 时不做转存（仅用于测试）。
func NewImageTaskServiceWithUploader(store ImageTaskStore, uploader *ImageResultUploader, ttl, executionTimeout time.Duration) *ImageTaskService {
	s := NewImageTaskServiceWithOptions(store, ttl, executionTimeout)
	s.uploader = uploader
	s.enabled = true
	return s
}

// NewImageTaskServiceWithResolver 构造一个由 resolver 决定启用状态的服务：
// 开关与凭证来自后台设置，保存后立即生效，无需重启。
func NewImageTaskServiceWithResolver(store ImageTaskStore, resolve ImageStorageResolver, ttl, executionTimeout time.Duration) *ImageTaskService {
	s := NewImageTaskServiceWithOptions(store, ttl, executionTimeout)
	s.resolve = resolve
	return s
}

// current 返回当前生效的 uploader、release 回调与启用状态。
// 注入了 resolver 时以 resolver 为准（后台设置可热切换），否则回落到构造时固定的值。
func (s *ImageTaskService) current() (*ImageResultUploader, func(), bool) {
	if s == nil {
		return nil, nil, false
	}
	if s.resolve != nil {
		return s.resolve()
	}
	return s.uploader, nil, s.enabled
}

// Enabled 表示异步图片任务功能是否可用（总开关 + 凭证齐全）。
// 关闭时 handler 直接返回 404，不创建任务、不写 Redis。
func (s *ImageTaskService) Enabled() bool {
	if s == nil || s.store == nil {
		return false
	}
	uploader, release, enabled := s.current()
	if release != nil {
		release()
	}
	return enabled && (s.resolve == nil || uploader != nil)
}

// Pollable 表示已创建的任务能否被查询。
// 比 Enabled 弱：只要 store 可用即可，从而在功能被关掉后仍能取回进行中的任务结果。
func (s *ImageTaskService) Pollable() bool {
	return s != nil && s.store != nil
}

func (s *ImageTaskService) ExecutionTimeout() time.Duration {
	if s == nil || s.executionTimeout <= 0 {
		return defaultImageTaskExecutionTimeout
	}
	return s.executionTimeout
}

func (s *ImageTaskService) Create(ctx context.Context, owner ImageTaskOwner) (*ImageTask, error) {
	if s == nil || s.store == nil {
		return nil, ErrImageTaskUnavailable
	}
	uploader, release, enabled := s.current()
	if s.resolve != nil && (!enabled || uploader == nil) {
		if release != nil {
			release()
		}
		return nil, ErrImageTaskDisabled
	}
	now := time.Now().UTC()
	task := &ImageTaskRecord{
		ID:        "imgtask_" + strings.ReplaceAll(uuid.NewString(), "-", ""),
		UserID:    owner.UserID,
		APIKeyID:  owner.APIKeyID,
		Status:    ImageTaskStatusProcessing,
		CreatedAt: now.Unix(),
		ExpiresAt: now.Add(s.ttl).Unix(),
	}
	if err := s.store.Save(ctx, task, s.ttl); err != nil {
		if release != nil {
			release()
		}
		return nil, ErrImageTaskUnavailable.WithCause(err)
	}
	s.bindings.Store(task.ID, &imageTaskStorageBinding{uploader: uploader, release: release})
	return imageTaskToPublic(task), nil
}

func (s *ImageTaskService) Get(ctx context.Context, owner ImageTaskOwner, id string) (*ImageTask, error) {
	if s == nil || s.store == nil {
		return nil, ErrImageTaskUnavailable
	}
	task, err := s.store.Get(ctx, strings.TrimSpace(id))
	if err != nil {
		if errors.Is(err, ErrImageTaskNotFound) {
			return nil, ErrImageTaskNotFound
		}
		return nil, ErrImageTaskUnavailable.WithCause(err)
	}
	if task.UserID != owner.UserID || task.APIKeyID != owner.APIKeyID {
		// Do not reveal whether a random task ID exists for another caller.
		return nil, ErrImageTaskNotFound
	}
	return imageTaskToPublic(task), nil
}

func (s *ImageTaskService) Complete(ctx context.Context, id string, statusCode int, result json.RawMessage) error {
	if s == nil {
		return ErrImageTaskUnavailable
	}
	defer s.finishBinding(id)
	if !json.Valid(result) {
		return s.failCompletedTask(id, http.StatusBadGateway, "api_error", "upstream returned a non-JSON image response")
	}
	bound, ok := s.bindings.Load(id)
	if !ok && s.resolve != nil {
		return s.failCompletedTask(id, http.StatusBadGateway, "api_error", "image storage binding was lost")
	}
	var uploader *ImageResultUploader
	if ok {
		binding, valid := bound.(*imageTaskStorageBinding)
		if !valid {
			return s.failCompletedTask(id, http.StatusBadGateway, "api_error", "image storage binding was invalid")
		}
		uploader = binding.uploader
	}
	if uploader != nil {
		rewritten, err := offloadImageTaskResult(ctx, uploader, id, result)
		if err != nil {
			// 转存失败不回退存 base64，避免大 blob 撑爆 Redis：直接把任务标记为失败。
			logger.L().Error("image_task.offload_failed", zap.String("task_id", id), zap.Error(err))
			if ctx.Err() != nil {
				return s.failCompletedTask(id, http.StatusGatewayTimeout, "timeout_error", "image storage upload timed out")
			}
			return s.failCompletedTask(id, http.StatusBadGateway, "api_error", "failed to store generated image to object storage")
		}
		result = rewritten
	}
	if !safeImageTaskResult(result) {
		return s.failCompletedTask(id, http.StatusBadGateway, "api_error", "image result could not be stored safely")
	}
	return s.finish(ctx, id, ImageTaskStatusCompleted, statusCode, result, nil)
}

// The completion context may have expired while uploading the image. Persist
// the terminal failure with a fresh, bounded context so it remains pollable.
func (s *ImageTaskService) failCompletedTask(id string, statusCode int, errorType, message string) error {
	ctx, cancel := context.WithTimeout(context.Background(), imageTaskFailurePersistTimeout)
	defer cancel()
	return s.Fail(ctx, id, statusCode, imageTaskErrorJSON(errorType, message))
}

// Retry only object-storage transfer. Rewriting an image uses deterministic
// per-task object keys, so a partial upload is safe to overwrite on retry.
// Never retry the paid upstream request from this completion path.
func offloadImageTaskResult(ctx context.Context, uploader *ImageResultUploader, id string, result json.RawMessage) (json.RawMessage, error) {
	var err error
	for attempt := 0; attempt < imageTaskOffloadAttempts; attempt++ {
		var rewritten json.RawMessage
		rewritten, err = uploader.Rewrite(ctx, id, result)
		if err == nil {
			return rewritten, nil
		}
		if ctx.Err() != nil {
			return nil, ctx.Err()
		}
		if attempt+1 == imageTaskOffloadAttempts {
			break
		}
		wait := time.Duration(1<<attempt) * 200 * time.Millisecond
		timer := time.NewTimer(wait)
		select {
		case <-ctx.Done():
			timer.Stop()
			return nil, ctx.Err()
		case <-timer.C:
		}
	}
	return nil, err
}

func (s *ImageTaskService) Fail(ctx context.Context, id string, statusCode int, taskErr json.RawMessage) error {
	if s == nil {
		return ErrImageTaskUnavailable
	}
	defer s.finishBinding(id)
	if unsafeImageTaskError(taskErr) {
		taskErr = imageTaskErrorJSON("api_error", "image generation failed")
	}
	return s.finish(ctx, id, ImageTaskStatusFailed, statusCode, nil, taskErr)
}

func (s *ImageTaskService) finishBinding(id string) {
	if bound, ok := s.bindings.LoadAndDelete(id); ok {
		binding, valid := bound.(*imageTaskStorageBinding)
		if !valid {
			return
		}
		if release := binding.release; release != nil {
			release()
		}
	}
}

func (s *ImageTaskService) finish(ctx context.Context, id, status string, statusCode int, result, taskErr json.RawMessage) error {
	if s == nil || s.store == nil {
		return ErrImageTaskUnavailable
	}
	// No caller, including future completion paths, may persist inline media or
	// an unbounded upstream error in Redis. Do not fall back to the raw response.
	if status == ImageTaskStatusCompleted && !safeImageTaskResult(result) {
		status, statusCode, result = ImageTaskStatusFailed, http.StatusBadGateway, nil
		taskErr = imageTaskErrorJSON("api_error", "image result could not be stored safely")
	}
	if len(taskErr) > 0 && unsafeImageTaskError(taskErr) {
		taskErr = imageTaskErrorJSON("api_error", "image generation failed")
	}
	task, err := s.store.Get(ctx, id)
	if err != nil {
		if errors.Is(err, ErrImageTaskNotFound) {
			return ErrImageTaskNotFound
		}
		return ErrImageTaskUnavailable.WithCause(err)
	}
	now := time.Now().UTC()
	completedAt := now.Unix()
	task.Status = status
	task.HTTPStatus = statusCode
	task.Result = result
	task.Error = taskErr
	task.CompletedAt = &completedAt
	task.ExpiresAt = now.Add(s.ttl).Unix()
	if err := s.store.Save(ctx, task, s.ttl); err != nil {
		return ErrImageTaskUnavailable.WithCause(err)
	}
	return nil
}

func safeImageTaskResult(result json.RawMessage) bool {
	if len(result) == 0 || len(result) > maxImageTaskResultBytes || !json.Valid(result) {
		return false
	}
	var top map[string]json.RawMessage
	if json.Unmarshal(result, &top) != nil {
		return false
	}
	var data []map[string]json.RawMessage
	rawData := bytes.TrimSpace(top["data"])
	if len(rawData) == 0 || rawData[0] != '[' || json.Unmarshal(rawData, &data) != nil || len(data) == 0 {
		return false
	}
	// Rewrite only removes the expected data[i].b64_json field. A malformed or
	// malicious upstream response could hide inline media in sibling fields.
	// Parse escaped JSON strings as well, rather than scanning only raw bytes.
	var all any
	if json.Unmarshal(result, &all) != nil || containsImageTaskInlineMedia(all) {
		return false
	}
	for _, item := range data {
		if _, inline := item["b64_json"]; inline {
			return false
		}
		rawURL, exists := item["url"]
		if !exists {
			return false
		}
		var imageURL string
		if json.Unmarshal(rawURL, &imageURL) != nil {
			return false
		}
		parsed, err := url.Parse(strings.TrimSpace(imageURL))
		if err != nil || (parsed.Scheme != "http" && parsed.Scheme != "https") || parsed.Host == "" {
			return false
		}
	}
	return true
}

func unsafeImageTaskError(taskErr json.RawMessage) bool {
	if len(taskErr) > maxImageTaskErrorBytes || !json.Valid(taskErr) {
		return true
	}
	var value any
	return json.Unmarshal(taskErr, &value) != nil || containsImageTaskInlineMedia(value)
}

func containsImageTaskInlineMedia(value any) bool {
	switch typed := value.(type) {
	case map[string]any:
		for key, child := range typed {
			lower := strings.ToLower(key)
			if strings.Contains(lower, "base64") || strings.Contains(lower, "b64") ||
				strings.Contains(lower, "inlinedata") || strings.Contains(lower, "inline_data") ||
				containsImageTaskInlineMedia(child) {
				return true
			}
		}
	case []any:
		for _, child := range typed {
			if containsImageTaskInlineMedia(child) {
				return true
			}
		}
	case string:
		return strings.Contains(strings.ToLower(typed), "data:image/")
	}
	return false
}

func imageTaskToPublic(task *ImageTaskRecord) *ImageTask {
	if task == nil {
		return nil
	}
	return &ImageTask{
		ID:          task.ID,
		TaskID:      task.ID,
		Object:      "image.generation.task",
		Status:      task.Status,
		HTTPStatus:  task.HTTPStatus,
		ImageURL:    firstImageTaskURL(task.Result),
		Result:      task.Result,
		Error:       task.Error,
		CreatedAt:   task.CreatedAt,
		CompletedAt: task.CompletedAt,
		ExpiresAt:   task.ExpiresAt,
	}
}

func firstImageTaskURL(result json.RawMessage) string {
	if len(result) == 0 || !json.Valid(result) {
		return ""
	}
	var response struct {
		Data []struct {
			URL string `json:"url"`
		} `json:"data"`
	}
	if json.Unmarshal(result, &response) != nil || len(response.Data) == 0 {
		return ""
	}
	return strings.TrimSpace(response.Data[0].URL)
}

func imageTaskErrorJSON(errorType, message string) json.RawMessage {
	data, _ := json.Marshal(map[string]string{"type": errorType, "message": message})
	return data
}
