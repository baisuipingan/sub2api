package service

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"path"
	"strings"
	"time"

	"github.com/google/uuid"
)

type officeWorkerInput struct {
	VersionID string `json:"version_id"`
	Name      string `json:"name"`
	SHA256    string `json:"sha256"`
	Bytes     int64  `json:"bytes"`
	URL       string `json:"url"`
}

type officeWorkerRequest struct {
	Action string              `json:"action"`
	Args   json.RawMessage     `json:"args"`
	Input  *officeWorkerInput  `json:"input,omitempty"`
	Images []officeWorkerInput `json:"images,omitempty"`
}

type officeWorkerJob struct {
	Status  string              `json:"status"`
	Result  json.RawMessage     `json:"result"`
	Error   string              `json:"error"`
	Message string              `json:"message"`
	Output  *officeWorkerOutput `json:"output"`
}

type officeWorkerOutput struct {
	Name       string `json:"name"`
	Bytes      int64  `json:"bytes"`
	SHA256     string `json:"sha256"`
	ContentMD5 string `json:"content_md5"`
}

var (
	errOfficeOutputAlreadyExists  = errors.New("office output already exists")
	errOfficeOutputExpired        = errors.New("office output expired")
	errOfficeStoredOutputMismatch = fmt.Errorf("%w: stored office output does not match the receipt", ErrOfficeConflict)
)

func permanentOfficeDeliveryError(err error) bool {
	return errors.Is(err, ErrOfficeResourceQuota) || errors.Is(err, ErrOfficeResourceType) ||
		errors.Is(err, ErrOfficeResourceTooLarge) || errors.Is(err, ErrOfficeNotFound) ||
		errors.Is(err, ErrGalleryGone) || errors.Is(err, ErrOfficeConflict) ||
		errors.Is(err, ErrOfficeInvalid) || errors.Is(err, errOfficeOutputExpired)
}

var officeWorkerHTTP = &http.Client{
	Timeout: 20 * time.Second,
	CheckRedirect: func(*http.Request, []*http.Request) error {
		return http.ErrUseLastResponse
	},
}

func (s *OfficeAgentService) officeToolsReadiness(ctx context.Context) string {
	if !s.Enabled() || s.settings == nil {
		return "disabled"
	}
	settings, err := s.settings.GetWorkbenchConfig(ctx)
	if err != nil {
		return "unavailable"
	}
	if !settings.ChatEnabled || !settings.OfficeEnabled || settings.AgentGroupID <= 0 {
		return "disabled"
	}
	nodes, err := s.officeNodes(ctx)
	if err == nil && len(nodes) > 0 {
		for _, node := range nodes {
			if node.State == "active" && node.officeNodeOnline() {
				return "ready"
			}
		}
		return "unavailable"
	}
	if s.cfg.ToolsURL == "" {
		return "unconfigured"
	}
	probe, cancel := context.WithTimeout(ctx, 2*time.Second)
	defer cancel()
	body, err := s.callOfficeWorker(probe, http.MethodGet, "/v1/ready", nil, 1024)
	var readiness struct {
		Status string `json:"status"`
	}
	if err != nil || json.Unmarshal(body, &readiness) != nil || readiness.Status != "ready" {
		return "unavailable"
	}
	return "ready"
}

func officeWorkerID(runID, operationID string) string {
	return officeDigest([]byte(runID + ":" + operationID))
}

func (s *OfficeAgentService) callOfficeWorker(ctx context.Context, method, endpoint string, body []byte, limit int64) ([]byte, error) {
	if target, ok := ctx.Value(officeDispatchContextKey{}).(*officeDispatchTarget); ok {
		status, raw, err := s.officeNodeRequest(ctx, target.node, method, endpoint, body, limit)
		if err != nil {
			return nil, ErrOfficeStepBusy
		}
		return officeWorkerResponse(method, status, raw, true)
	}
	req, err := http.NewRequestWithContext(ctx, method, strings.TrimRight(s.cfg.ToolsURL, "/")+endpoint, bytes.NewReader(body))
	if err != nil {
		return nil, ErrOfficeInvalid
	}
	req.Header.Set("Authorization", "Bearer "+s.cfg.ToolsToken)
	if len(body) > 0 {
		req.Header.Set("Content-Type", "application/json")
	}
	resp, err := officeWorkerHTTP.Do(req)
	if err != nil {
		return nil, ErrOfficeStepBusy
	}
	defer resp.Body.Close()
	data, err := io.ReadAll(io.LimitReader(resp.Body, limit+1))
	if err != nil {
		return nil, ErrOfficeStepBusy
	}
	if int64(len(data)) > limit {
		return nil, ErrOfficeResourceTooLarge
	}
	return officeWorkerResponse(method, resp.StatusCode, data, false)
}

func officeWorkerResponse(method string, status int, data []byte, pooled bool) ([]byte, error) {
	switch status {
	case http.StatusNotFound:
		return nil, ErrOfficeNotFound
	case http.StatusPreconditionFailed:
		return nil, errOfficeOutputAlreadyExists
	case http.StatusConflict:
		return nil, ErrOfficeConflict
	}
	if status >= 200 && status < 300 {
		return data, nil
	}
	var failure struct {
		Error string `json:"error"`
	}
	_ = json.Unmarshal(data, &failure)
	if pooled && method == http.MethodPost && status == http.StatusServiceUnavailable && failure.Error == "WORKER_BUSY" {
		return nil, errOfficeWorkerRefused
	}
	if status >= 500 || status == http.StatusTooManyRequests {
		return nil, ErrOfficeStepBusy
	}
	if !officeIdentifier(failure.Error, 64) {
		failure.Error = "UNRECOGNIZED_WORKER_ERROR"
	}
	slog.Warn("office tool request rejected", "method", method, "status", status, "code", failure.Error)
	return nil, ErrOfficeInvalid
}

func (s *OfficeAgentService) workbenchInput(ctx context.Context, repo workbenchBrowserRepository, run OfficeRun, version string) (*OfficeResource, error) {
	if s.resources == nil {
		return nil, ErrOfficeDisabled
	}
	allowed, err := repo.BrowserResourceAllowed(ctx, run, version)
	if err != nil {
		return nil, err
	}
	if !allowed {
		return nil, ErrOfficeNotFound
	}
	return s.resources.repo.GetResourceVersion(ctx, run.UserID, version, true)
}

func (s *OfficeAgentService) officeWorkerRequest(ctx context.Context, action string, raw json.RawMessage, source *OfficeResource, images []*OfficeResource) ([]byte, error) {
	var args map[string]json.RawMessage
	if json.Unmarshal(raw, &args) != nil || args == nil {
		return nil, ErrOfficeInvalid
	}
	delete(args, "version_id")
	// normalize numbers and preserve field order across replay
	body, err := json.Marshal(args)
	if err != nil {
		return nil, err
	}
	request := officeWorkerRequest{Action: action, Args: body}
	if source != nil {
		input, err := s.signOfficeWorkerInput(ctx, source)
		if err != nil {
			return nil, err
		}
		request.Input = &input
	}
	for _, image := range images {
		input, err := s.signOfficeWorkerInput(ctx, image)
		if err != nil {
			return nil, err
		}
		request.Images = append(request.Images, input)
	}
	return json.Marshal(request)
}

func (s *OfficeAgentService) signOfficeWorkerInput(ctx context.Context, source *OfficeResource) (officeWorkerInput, error) {
	store, err := s.resources.storage(ctx, source.Endpoint, source.Region, source.Bucket)
	if err != nil {
		return officeWorkerInput{}, err
	}
	link, err := store.PresignGet(ctx, source.ObjectKey, 10*time.Minute)
	return officeWorkerInput{source.VersionID, source.Name, source.SHA256, source.Bytes, link}, err
}

func (s *OfficeAgentService) officeWorkerImages(ctx context.Context, repo workbenchBrowserRepository, run OfficeRun, call workbenchNativeCall) ([]*OfficeResource, error) {
	if call.Name != "edit_pptx" {
		return nil, nil
	}
	var args struct {
		ReplaceImages []struct {
			VersionID string `json:"image_version_id"`
		} `json:"replace_images"`
	}
	if json.Unmarshal(call.Arguments, &args) != nil || len(args.ReplaceImages) > 32 {
		return nil, ErrOfficeInvalid
	}
	var images []*OfficeResource
	var bytes int64
	seen := make(map[string]bool, len(args.ReplaceImages))
	for _, edit := range args.ReplaceImages {
		if seen[edit.VersionID] {
			continue
		}
		image, err := s.workbenchInput(ctx, repo, run, edit.VersionID)
		if err != nil {
			return nil, err
		}
		if image.MIME != "image/png" && image.MIME != "image/jpeg" {
			return nil, ErrOfficeResourceType
		}
		bytes += image.Bytes
		if image.Bytes > 8<<20 || bytes > 24<<20 || len(images) >= 16 {
			return nil, ErrOfficeResourceTooLarge
		}
		images = append(images, image)
		seen[edit.VersionID] = true
	}
	return images, nil
}

func (s *OfficeAgentService) runRemoteOfficeTool(ctx context.Context, repo workbenchBrowserRepository, lease OfficeRunLease, op *OfficeOperation, call workbenchNativeCall, version string) (resultErr error) {
	id := op.ID
	key := officeWorkerID(lease.Run.ID, id)
	seed := OfficeDispatch{Key: key, UserID: lease.Run.UserID, RunID: lease.Run.ID}
	if _, err := s.authorize(ctx, lease.Run); err != nil {
		return s.completeBrowserToolError(ctx, lease, id, call.Name, err)
	}
	stepTimedOut := op.DispatchedAt != nil && time.Since(*op.DispatchedAt) >= officeDispatchDeadline
	if stepTimedOut {
		state, err := s.existingOfficeDispatch(ctx, key)
		if err != nil {
			return err
		}
		if state == nil || len(state.Receipt) == 0 {
			if state != nil && s.cancelAssignedOfficeJob(ctx, *state) {
				_ = s.finishOfficeDispatch(ctx, &officeDispatchTarget{item: state}, "cancelled")
			}
			return s.completeBrowserToolError(ctx, lease, id, call.Name, context.DeadlineExceeded)
		}
	}
	ctx, target, err := s.officeDispatchTarget(ctx, seed, call.Name, false)
	if err != nil {
		if stepTimedOut || errors.Is(err, ErrOfficeConflict) {
			return s.completeBrowserToolError(ctx, lease, id, call.Name, context.DeadlineExceeded)
		}
		if errors.Is(err, errOfficeAwaitingNode) {
			if queued, getErr := s.existingOfficeDispatch(ctx, key); getErr == nil && queued != nil {
				s.officeDispatchProgress(ctx, lease, id, call.Name, "wait", "等待办公算力", queued.Attempt)
			}
		}
		return err
	}
	if target != nil {
		target.stepTimedOut = stepTimedOut
	}
	failDelivery := func(err error) error {
		if permanentOfficeDeliveryError(err) {
			return s.completeBrowserToolError(ctx, lease, id, call.Name, err)
		}
		if target.expired() {
			return s.completeBrowserToolError(ctx, lease, id, call.Name, errOfficeOutputExpired)
		}
		return err
	}
	if target != nil && len(target.item.Receipt) == 0 {
		s.officeDispatchProgress(ctx, lease, id, call.Name, "work", "正在处理文件", target.item.Attempt)
	}
	defer func() {
		if resultErr == nil {
			resultErr = s.finishOfficeDispatch(ctx, target, "done")
		}
	}()
	endpoint := target.endpoint(key)
	var source *OfficeResource
	if version != "" {
		input, inputErr := s.workbenchInput(ctx, repo, lease.Run, version)
		if inputErr != nil {
			return failDelivery(inputErr)
		}
		if !strings.EqualFold(path.Ext(input.Name), remoteOfficeExtension(call.Name)) {
			return s.completeBrowserToolError(ctx, lease, id, call.Name, ErrOfficeResourceType)
		}
		source = input
	}
	raw, err := s.officeWorkerReceipt(ctx, target, endpoint, func() ([]byte, error) {
		images, err := s.officeWorkerImages(ctx, repo, lease.Run, call)
		if err != nil {
			return nil, err
		}
		return s.officeWorkerRequest(ctx, call.Name, call.Arguments, source, images)
	})
	if err != nil {
		return failDelivery(err)
	}
	var job officeWorkerJob
	if err := json.Unmarshal(raw, &job); err != nil {
		return failDelivery(ErrOfficeStepBusy)
	}
	result := OfficeOperationResult{Output: job.Result}
	switch job.Status {
	case "running", "queued":
		return failDelivery(ErrOfficeStepBusy)
	case "completed", "partial":
		if job.Status == "partial" {
			result.ToolStatus = "partial"
		}
	case "failed", "cancelled", "unknown":
		result.ErrorCode, result.ErrorMessage = job.Error, job.Message
		if !officeIdentifier(result.ErrorCode, 64) {
			result.ErrorCode = "OFFICE_TOOL_FAILED"
		}
		if len(result.ErrorMessage) > 1000 || result.ErrorMessage == "" {
			result.ErrorMessage = "办公操作暂未完成。"
		}
		if job.Status == "cancelled" {
			result.ToolStatus = "cancelled"
		}
		result.Output, _ = json.Marshal(map[string]string{"error": result.ErrorCode, "message": result.ErrorMessage})
	default:
		return failDelivery(ErrOfficeStepBusy)
	}
	if len(result.Output) == 0 {
		result.Output = json.RawMessage(`{}`)
	}
	if (job.Status == "completed" || job.Status == "partial") && job.Output == nil && !strings.HasPrefix(call.Name, "read_") {
		return s.completeBrowserToolError(ctx, lease, id, call.Name, ErrOfficeInvalid)
	}
	if job.Output != nil && (job.Status == "completed" || job.Status == "partial") {
		if job.Output.Bytes <= 0 || job.Output.Bytes > OfficeResourceMaxBytes ||
			!strings.EqualFold(path.Ext(job.Output.Name), remoteOfficeExtension(call.Name)) {
			return s.completeBrowserToolError(ctx, lease, id, call.Name, ErrOfficeResourceType)
		}
		if err := s.repo.CheckLease(ctx, lease); err != nil {
			return err
		}
		if call.Name == "batch_edit_docx" || call.Name == "template_pptx" {
			source = nil
		}
		if target != nil {
			s.officeDispatchProgress(ctx, lease, id, call.Name, "save", "正在保存成品", target.item.Attempt)
		}
		artifact, err := s.publishRemoteOfficeFile(ctx, lease, id, endpoint, source, job)
		if err != nil {
			return failDelivery(err)
		}
		var output map[string]json.RawMessage
		if json.Unmarshal(result.Output, &output) != nil || output == nil {
			output = make(map[string]json.RawMessage)
		}
		output["artifact"], _ = json.Marshal(artifact)
		result.Output, err = json.Marshal(output)
		if err != nil {
			return err
		}
	}
	_, err = s.CompleteTool(ctx, lease, id, result)
	return err
}

func remoteOfficeExtension(action string) string {
	if action == "batch_edit_docx" {
		return ".zip"
	}
	_, extension, _ := strings.Cut(action, "_")
	return "." + extension
}

type officePresignedWriter interface {
	PresignOfficePut(context.Context, string, string, int64, string, bool, time.Duration) (string, http.Header, error)
}

func (s *OfficeAgentService) publishRemoteOfficeFile(ctx context.Context, lease OfficeRunLease, operation, endpoint string, parent *OfficeResource, job officeWorkerJob) (*OfficeResource, error) {
	meta := job.Output
	if !validOfficeWorkerOutput(meta) {
		return nil, ErrOfficeInvalid
	}
	mime := officeFileMIMEs[strings.ToLower(path.Ext(meta.Name))]
	if mime == "" {
		return nil, ErrOfficeResourceType
	}
	record := browserFileRecord(lease, operation, parent, meta.Name, mime, "unsupported", meta.SHA256, meta.Bytes)
	reserved, err := s.resources.reserveVersion(ctx, record, &lease, false)
	if err != nil || reserved.State == "ready" {
		return reserved, err
	}
	store, err := s.resources.storage(ctx, reserved.Endpoint, reserved.Region, reserved.Bucket)
	if err != nil {
		return nil, err
	}
	if err = s.uploadOfficeWorkerOutput(ctx, store, endpoint, reserved.ObjectKey, reserved.MIME, reserved.VersionID, meta); err != nil {
		return nil, err
	}
	return s.resources.repo.FinishVersion(ctx, lease.Run.UserID, reserved.VersionID, &lease)
}

func validOfficeWorkerOutput(meta *officeWorkerOutput) bool {
	if meta == nil || meta.Bytes <= 0 || meta.Bytes > OfficeResourceMaxBytes || !officeSafeName(meta.Name) {
		return false
	}
	digest, err := hex.DecodeString(meta.SHA256)
	if err != nil || len(digest) != 32 {
		return false
	}
	checksum, err := base64.StdEncoding.DecodeString(meta.ContentMD5)
	return err == nil && len(checksum) == 16 && base64.StdEncoding.EncodeToString(checksum) == meta.ContentMD5
}

func (s *OfficeAgentService) uploadOfficeWorkerOutput(ctx context.Context, store GalleryObjectStore, endpoint, key, mime, versionID string, meta *officeWorkerOutput) error {
	target, _ := ctx.Value(officeDispatchContextKey{}).(*officeDispatchTarget)
	if target.expired() {
		if err := verifyOfficeStoredOutput(ctx, store, key, meta); err != nil {
			return errOfficeOutputExpired
		}
		return nil
	}
	writer, ok := store.(officePresignedWriter)
	if !ok {
		return ErrOfficeStorageUnavailable
	}
	url, headers, err := writer.PresignOfficePut(ctx, key, mime, meta.Bytes,
		meta.ContentMD5, !s.cfg.ToolsLegacyPut, 2*time.Minute)
	if err != nil {
		return err
	}
	signedHeaders := make(map[string]string, len(headers))
	for key, values := range headers {
		if strings.EqualFold(key, "Content-Length") {
			continue // The worker sets the length from the immutable output receipt.
		}
		if len(values) != 1 {
			return ErrOfficeInvalid
		}
		signedHeaders[key] = values[0]
	}
	body, err := json.Marshal(struct {
		URL       string            `json:"url"`
		Headers   map[string]string `json:"headers"`
		VersionID string            `json:"version_id,omitempty"`
	}{url, signedHeaders, versionID})
	if err != nil {
		return err
	}
	if _, err = s.callOfficeWorker(ctx, http.MethodPost, endpoint+"/upload", body, OfficeMaxOperationResultBytes); err != nil {
		if errors.Is(err, errOfficeOutputAlreadyExists) {
			return verifyOfficeStoredOutput(ctx, store, key, meta)
		}
		if target != nil && (errors.Is(err, ErrOfficeNotFound) || errors.Is(err, ErrOfficeStepBusy)) {
			if verifyOfficeStoredOutput(ctx, store, key, meta) == nil {
				return nil
			}
		}
		if errors.Is(err, ErrOfficeNotFound) {
			return errOfficeOutputExpired
		}
		return err
	}
	return nil
}

func verifyOfficeStoredOutput(ctx context.Context, store GalleryObjectStore, key string, meta *officeWorkerOutput) error {
	reader, ok := store.(GalleryObjectReader)
	if !ok {
		return ErrOfficeStorageUnavailable
	}
	data, err := reader.Get(ctx, key, meta.Bytes)
	if err != nil {
		if errors.Is(err, ErrGalleryInvalid) {
			return errOfficeStoredOutputMismatch
		}
		return err
	}
	if int64(len(data)) != meta.Bytes || officeResourceHash(data) != meta.SHA256 {
		return errOfficeStoredOutputMismatch
	}
	return nil
}

func browserFileRecord(lease OfficeRunLease, operation string, parent *OfficeResource, name, mime, kind, digest string, size int64) OfficeResource {
	record := OfficeResource{
		ID: uuid.NewString(), VersionID: uuid.NewString(), UserID: lease.Run.UserID, RunID: lease.Run.ID,
		Name: name, MIME: mime, Bytes: size, SHA256: digest,
		PreviewKind: kind, Source: "generated", PublicationID: "tool:" + officeWorkerID(lease.Run.ID, operation),
		PreviewSHA256: officeResourceHash(nil),
	}
	if parent != nil {
		record.ID, record.ParentVersionID = parent.ID, parent.VersionID
	}
	return record
}

func (s *OfficeAgentService) publishBrowserFile(ctx context.Context, lease OfficeRunLease, operation string, parent *OfficeResource, name string, data []byte) (*OfficeResource, error) {
	mime, kind, err := validateOfficeFile(name, data)
	if err != nil {
		return nil, err
	}
	release, err := s.resources.AcquireIO()
	if err != nil {
		return nil, err
	}
	defer release()
	record := browserFileRecord(lease, operation, parent, name, mime, kind, officeResourceHash(data), int64(len(data)))
	return s.resources.persistVersion(ctx, record, &lease, data, nil)
}

func (s *OfficeAgentService) completeBrowserToolError(ctx context.Context, lease OfficeRunLease, id, name string, cause error) error {
	message := browserToolError(name, cause)
	encoded, _ := json.Marshal(map[string]string{"error": "TOOL_FAILED", "message": message})
	_, err := s.CompleteTool(ctx, lease, id, OfficeOperationResult{Output: encoded, ErrorCode: "TOOL_FAILED", ErrorMessage: message})
	return err
}

func (s *OfficeAgentService) cancelRemoteOfficeTools(ctx context.Context, run *OfficeRun) {
	repo, ok := s.repo.(workbenchBrowserRepository)
	if !ok || run == nil || !run.BrowserDriven || run.Status != OfficeRunCancelled {
		return
	}
	operations, err := repo.BrowserOperations(ctx, run.UserID, run.ID)
	if err != nil {
		return
	}
	for _, op := range operations {
		if op.Kind != OfficeOperationTool || op.State != OfficeOperationDispatched {
			continue
		}
		var input struct {
			Remote bool `json:"remote"`
		}
		if json.Unmarshal(op.Input, &input) == nil && input.Remote {
			key := officeWorkerID(run.ID, op.ID)
			item, err := s.existingOfficeDispatch(ctx, key)
			if err != nil {
				continue
			}
			if item != nil {
				if s.cancelAssignedOfficeJob(ctx, *item) {
					_ = s.repo.(officeDispatchRepository).FinishOfficeDispatch(ctx, *item, "cancelled")
				}
			} else if s.cfg.ToolsURL != "" {
				_, _ = s.callOfficeWorker(ctx, http.MethodDelete, "/v1/jobs/"+key, nil, 64<<10)
			}
		}
	}
}
