package service

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/google/uuid"
)

const (
	studioJobMaxResponseBytes  int64 = 128 << 20
	studioJobMaxTaskSpoolBytes int64 = 512 << 20
	studioJobMaxSpoolBytes     int64 = 1 << 30
	studioJobMaxInputBytes     int64 = 96 << 20
)

// Start enables durable admission only after the persistent spool is usable.
// The same Gin router handles Studio calls, so auth, routing, quota, and usage
// billing remain exactly the normal gateway path.
func (s *StudioJobService) Start(router http.Handler, dir string) error {
	if s == nil || s.repo == nil || router == nil || dir == "" {
		return ErrStudioJobUnavailable
	}
	if err := os.MkdirAll(dir, 0700); err != nil {
		return fmt.Errorf("create Studio spool: %w", err)
	}
	if err := os.Chmod(dir, 0700); err != nil {
		return err
	}
	owner, err := studioSpoolOwner(dir)
	if err != nil {
		return err
	}
	entries, err := os.ReadDir(dir)
	if err != nil {
		return err
	}
	var used int64
	for _, entry := range entries {
		if entry.IsDir() {
			continue
		}
		info, err := entry.Info()
		if err != nil {
			return err
		}
		if strings.HasSuffix(entry.Name(), ".json") || strings.HasSuffix(entry.Name(), ".tmp") || strings.Contains(entry.Name(), ".json.image-") {
			used += info.Size()
		}
	}
	if used > studioJobMaxSpoolBytes {
		// Existing paid responses still need archival; capacity only stops new calls.
		slog.Warn("Studio spool is over capacity; recovery continues", "bytes", used)
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.cancel != nil {
		return nil
	}
	ctx, cancel := context.WithCancel(context.Background())
	s.router, s.spoolDir, s.owner, s.cancel = router, dir, owner, cancel
	s.spoolUsed = used
	// One global executor bounds image decoding and multipart memory usage.
	for i := 0; i < 1; i++ {
		s.workers.Add(1)
		go s.runStudioJobs(ctx)
	}
	s.workers.Add(1)
	go s.reconcileStudioJobs(ctx)
	return nil
}

func (s *StudioJobService) Stop() {
	if s == nil {
		return
	}
	s.mu.Lock()
	cancel := s.cancel
	s.cancel = nil
	s.mu.Unlock()
	if cancel != nil {
		cancel()
	}
	s.workers.Wait()
	s.mu.Lock()
	s.router = nil
	s.mu.Unlock()
}

func studioSpoolOwner(dir string) (string, error) {
	path := filepath.Join(dir, ".owner")
	data, err := os.ReadFile(path)
	if err == nil {
		if owner := strings.TrimSpace(string(data)); uuid.Validate(owner) == nil {
			return owner, nil
		}
		// A crash while creating the owner marker can leave a partial file.
		// Regenerate it; foreign saving leases become unknown after their grace
		// period instead of disabling all new and existing jobs forever.
		if err := os.Remove(path); err != nil {
			return "", err
		}
		return studioSpoolOwner(dir)
	}
	if !errors.Is(err, os.ErrNotExist) {
		return "", err
	}
	owner := uuid.NewString()
	file, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0600)
	if errors.Is(err, os.ErrExist) {
		return studioSpoolOwner(dir)
	}
	if err != nil {
		return "", err
	}
	if _, err = file.WriteString(owner); err == nil {
		err = file.Sync()
	}
	closeErr := file.Close()
	if err != nil {
		return "", err
	}
	if closeErr != nil {
		return "", closeErr
	}
	return owner, nil
}

func (s *StudioJobService) runStudioJobs(ctx context.Context) {
	defer s.workers.Done()
	ticker := time.NewTicker(2 * time.Second)
	defer ticker.Stop()
	for {
		if ctx.Err() != nil {
			return
		}
		claim, err := s.repo.Claim(ctx, s.owner)
		if err != nil {
			slog.Error("Studio job claim failed", "error", err)
		}
		if claim != nil {
			s.executeStudioClaim(ctx, *claim)
			continue
		}
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
		}
	}
}

func (s *StudioJobService) executeStudioClaim(parent context.Context, claim StudioJobClaim) {
	ctx, cancel := context.WithTimeout(parent, 25*time.Minute)
	defer cancel()
	stopHeartbeat := make(chan struct{})
	go func() {
		ticker := time.NewTicker(30 * time.Second)
		defer ticker.Stop()
		for {
			select {
			case <-stopHeartbeat:
				return
			case <-ctx.Done():
				return
			case <-ticker.C:
				leaseCtx, leaseCancel := context.WithTimeout(context.Background(), 10*time.Second)
				err := s.repo.ExtendLease(leaseCtx, claim)
				leaseCancel()
				if err != nil {
					cancel()
					return
				}
			}
		}
	}()
	defer close(stopHeartbeat)
	if claim.Call.Status == StudioJobSaving {
		s.saveStudioClaim(ctx, claim, claim.Call.SpoolPath)
		return
	}
	if claim.Call.Status != StudioJobGenerating {
		return
	}
	path := s.studioSpoolPath(claim)
	if _, err := os.Stat(path); err == nil {
		if err := s.repo.MarkSaving(ctx, claim, path, s.owner); err == nil {
			s.saveStudioClaim(ctx, claim, path)
		}
		return
	}
	request, err := s.buildStudioGatewayRequest(ctx, claim)
	if err != nil {
		if ctx.Err() != nil {
			return
		} // restart recovers the unsent claim
		slog.Warn("Studio job input preparation failed", "task_id", claim.TaskID, "error", err)
		if errors.Is(err, ErrStudioJobRouteChanged) {
			s.failStudioClaim(claim, "MODEL_ROUTE_CHANGED", "this image model route changed while queued; no model call was made")
			return
		}
		s.failStudioClaim(claim, "INPUT_UNAVAILABLE", "reference image, model route, or Studio key is unavailable")
		return
	}
	// A crash before sent_at is committed can leave an untrusted temp file.
	// The claim has a new lease; only the final .json file is recoverable.
	s.removeStudioSpool(path + ".tmp")
	if err := s.reserveStudioSpool(claim); err != nil {
		slog.Warn("Studio spool admission failed", "task_id", claim.TaskID, "error", err)
		s.failStudioClaim(claim, "SPOOL_FULL", "Studio temporary storage is full")
		return
	}
	defer s.releaseStudioReservation(claim)
	file, err := os.OpenFile(path+".tmp", os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0600)
	if err != nil {
		slog.Error("Studio spool create failed", "task_id", claim.TaskID, "error", err)
		s.failStudioClaim(claim, "SPOOL_UNAVAILABLE", "Studio temporary storage is unavailable")
		return
	}
	writer := &studioSpoolWriter{file: file, header: make(http.Header), limit: studioJobMaxResponseBytes}
	// Once sent_at is committed, any failure without a durable response is
	// ambiguous. It must never trigger a second paid gateway call.
	if err := s.repo.MarkDispatched(ctx, claim); err != nil {
		_ = file.Close()
		_ = os.Remove(path + ".tmp")
		if ctx.Err() != nil {
			return
		}
		slog.Error("Studio dispatch checkpoint failed", "task_id", claim.TaskID, "error", err)
		s.failStudioClaim(claim, "DISPATCH_UNAVAILABLE", "Studio task could not be dispatched")
		return
	}
	s.router.ServeHTTP(writer, request)
	if writer.status == 0 {
		writer.status = http.StatusOK
	}
	if err := file.Sync(); err != nil {
		writer.writeErr = err
	}
	if err := file.Close(); err != nil {
		writer.writeErr = err
	}
	if ctx.Err() != nil || writer.writeErr != nil {
		_ = os.Remove(path + ".tmp")
		s.markStudioUnknown(claim)
		return
	}
	if writer.status < 200 || writer.status >= 300 {
		message := studioGatewayError(path+".tmp", writer.status)
		_ = os.Remove(path + ".tmp")
		if writer.status >= 500 || writer.status == http.StatusRequestTimeout {
			// A gateway timeout/5xx may be emitted after the upstream accepted
			// the request; do not run another paid call for this child.
			s.markStudioUnknown(claim)
			return
		}
		s.failStudioClaim(claim, fmt.Sprintf("GATEWAY_HTTP_%d", writer.status), message)
		return
	}
	if writer.written == 0 {
		_ = os.Remove(path + ".tmp")
		s.markStudioUnknown(claim)
		return
	}
	if err := os.Rename(path+".tmp", path); err != nil {
		s.markStudioUnknown(claim)
		return
	}
	if err := syncStudioDirectory(s.spoolDir); err != nil {
		slog.Warn("Studio spool directory sync failed", "error", err)
	}
	s.spoolMu.Lock()
	s.spoolUsed += writer.written
	s.spoolMu.Unlock()
	if err := s.repo.MarkSaving(ctx, claim, path, s.owner); err != nil {
		// Keep the durable response; recovery sees the deterministic file.
		slog.Error("Studio job response saved but state update failed", "task_id", claim.TaskID, "error", err)
		return
	}
	s.saveStudioClaim(ctx, claim, path)
}

type studioSpoolWriter struct {
	file           *os.File
	header         http.Header
	status         int
	limit, written int64
	writeErr       error
}

func (w *studioSpoolWriter) Header() http.Header { return w.header }
func (w *studioSpoolWriter) WriteHeader(status int) {
	if w.status == 0 {
		w.status = status
	}
}
func (w *studioSpoolWriter) Write(data []byte) (int, error) {
	if w.status == 0 {
		w.status = http.StatusOK
	}
	if w.writeErr != nil {
		return 0, w.writeErr
	}
	if w.written+int64(len(data)) > w.limit {
		w.writeErr = errors.New("studio gateway response exceeded spool limit")
		return 0, w.writeErr
	}
	n, err := w.file.Write(data)
	w.written += int64(n)
	if err != nil {
		w.writeErr = err
	}
	return n, err
}
func (w *studioSpoolWriter) Flush() { _ = w.file.Sync() }

func studioGatewayError(path string, status int) string {
	file, err := os.Open(path)
	if err == nil {
		defer func() { _ = file.Close() }()
		data, readErr := io.ReadAll(io.LimitReader(file, 8<<10))
		if readErr != nil {
			return fmt.Sprintf("gateway returned HTTP %d", status)
		}
		var parsed struct {
			Error struct {
				Message string `json:"message"`
			} `json:"error"`
		}
		if json.Unmarshal(data, &parsed) == nil && parsed.Error.Message != "" {
			message := strings.TrimSpace(parsed.Error.Message)
			if len(message) > 200 {
				message = message[:200]
			}
			return fmt.Sprintf("gateway returned HTTP %d: %s", status, message)
		}
	}
	return fmt.Sprintf("gateway returned HTTP %d", status)
}

func syncStudioDirectory(dir string) error {
	file, err := os.Open(dir)
	if err != nil {
		return err
	}
	if err := file.Sync(); err != nil {
		_ = file.Close()
		return err
	}
	return file.Close()
}

func (s *StudioJobService) studioSpoolPath(claim StudioJobClaim) string {
	return filepath.Join(s.spoolDir, "u"+strconv.FormatInt(claim.UserID, 10)+"__"+claim.TaskID+"__"+strconv.Itoa(claim.Call.Ordinal)+".json")
}

func (s *StudioJobService) reserveStudioSpool(claim StudioJobClaim) error {
	s.spoolMu.Lock()
	defer s.spoolMu.Unlock()
	reservation := studioSpoolReservationBytes(claim)
	if s.spoolUsed+s.spoolReserved+reservation > studioJobMaxSpoolBytes {
		return errors.New("studio spool capacity exhausted")
	}
	entries, err := os.ReadDir(s.spoolDir)
	if err != nil {
		return err
	}
	prefix := "u" + strconv.FormatInt(claim.UserID, 10) + "__" + claim.TaskID + "__"
	var taskBytes int64
	for _, entry := range entries {
		if entry.IsDir() || !strings.HasPrefix(entry.Name(), prefix) {
			continue
		}
		info, err := entry.Info()
		if err != nil {
			return err
		}
		taskBytes += info.Size()
	}
	if taskBytes+reservation > studioJobMaxTaskSpoolBytes {
		return errors.New("studio task spool capacity exhausted")
	}
	s.spoolReserved += reservation
	return nil
}

func studioSpoolReservationBytes(claim StudioJobClaim) int64 {
	count := 1
	if claim.Spec.Protocol == "grok" {
		count = max(1, min(10, claim.Spec.ImageCount))
	}
	// Leave room for a bounded gateway response and all possible URL originals
	// before making a paid call, even if a supplier returns native n=10.
	return studioJobMaxResponseBytes + int64(count)*GalleryMaxImageBytes
}

func (s *StudioJobService) releaseStudioReservation(claim StudioJobClaim) {
	s.spoolMu.Lock()
	s.spoolReserved -= studioSpoolReservationBytes(claim)
	s.spoolMu.Unlock()
}
func (s *StudioJobService) removeStudioSpool(path string) {
	if path == "" || filepath.Dir(path) != s.spoolDir {
		return
	}
	info, err := os.Stat(path)
	if err != nil {
		return
	}
	if err := os.Remove(path); err == nil {
		s.spoolMu.Lock()
		s.spoolUsed -= info.Size()
		if s.spoolUsed < 0 {
			s.spoolUsed = 0
		}
		s.spoolMu.Unlock()
	}
}

func (s *StudioJobService) failStudioClaim(claim StudioJobClaim, code, message string) {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	if err := s.repo.FailCall(ctx, claim, code, message); err != nil {
		slog.Error("Studio call failure state write failed", "task_id", claim.TaskID, "error", err)
	}
}
func (s *StudioJobService) markStudioUnknown(claim StudioJobClaim) {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	if err := s.repo.MarkUncertain(ctx, claim); err != nil {
		slog.Error("Studio unknown result state write failed", "task_id", claim.TaskID, "error", err)
	}
}

func (s *StudioJobService) markStudioSavingLost(claim StudioJobClaim, expiredOnly bool) {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	if err := s.repo.MarkSavingLost(ctx, claim, expiredOnly); err != nil {
		slog.Error("Studio paid spool loss state write failed", "task_id", claim.TaskID, "error", err)
	}
}

func (s *StudioJobService) saveStudioClaim(ctx context.Context, claim StudioJobClaim, path string) {
	if path != s.studioSpoolPath(claim) {
		s.markStudioSavingLost(claim, false)
		return
	}
	current, err := s.repo.Get(ctx, claim.UserID, claim.TaskID)
	if err == nil && current.DeletedAt != nil {
		s.removeStudioResponse(path)
		return
	}
	if errors.Is(err, ErrStudioJobNotFound) {
		s.removeStudioResponse(path)
		return
	}
	if err != nil {
		s.retryStudioSaving(claim, "JOB_STATE_UNAVAILABLE")
		return
	}
	data, err := os.ReadFile(path)
	if err != nil || len(data) == 0 || int64(len(data)) > studioJobMaxResponseBytes {
		s.markStudioSavingLost(claim, false)
		return
	}
	if err := s.cacheStudioURLImages(ctx, claim, data, NewImageResultUploader(nil, "", GalleryMaxImageBytes, nil)); err != nil {
		if errors.Is(err, ErrGalleryInvalid) {
			s.failStudioClaim(claim, "INVALID_IMAGE_RESULT", "upstream returned an invalid image result")
		} else {
			s.retryStudioSaving(claim, "IMAGE_DOWNLOAD_RETRY")
		}
		return
	}
	cfg, err := s.StorageTarget(ctx)
	if err != nil || cfg == nil || cfg.Endpoint != claim.StorageEndpoint || cfg.Region != claim.StorageRegion || cfg.Bucket != claim.StorageBucket {
		s.retryStudioSaving(claim, "STORAGE_TARGET_UNAVAILABLE")
		return
	}
	outputs, raws, err := s.archiveStudioImages(ctx, claim, data)
	if err != nil {
		if errors.Is(err, ErrGalleryQuota) {
			s.failStudioClaim(claim, "GALLERY_QUOTA_EXCEEDED", "cloud gallery quota was exhausted after this paid image result; no further calls were made")
			return
		}
		if errors.Is(err, ErrGalleryInvalid) || errors.Is(err, errStudioImageInvalid) {
			s.failStudioClaim(claim, "INVALID_IMAGE_RESULT", err.Error())
			return
		}
		s.retryStudioSaving(claim, "IMAGE_ARCHIVE_RETRY")
		return
	}
	billingState := "pending"
	if recorded, err := s.repo.BillingRecorded(ctx, studioBillingRequestID(claim), claim.APIKeyID); err == nil && recorded {
		billingState = "recorded"
	}
	if err := s.repo.CompleteCall(ctx, claim, outputs, raws, billingState); err != nil {
		if errors.Is(err, ErrStudioJobNotFound) {
			if latest, getErr := s.repo.Get(ctx, claim.UserID, claim.TaskID); getErr == nil && latest.DeletedAt != nil {
				s.removeStudioResponse(path)
				return
			}
		}
		// Preserve the response for a retry, including a crash after R2 upload.
		slog.Error("Studio call completion write failed", "task_id", claim.TaskID, "error", err)
		return
	}
	s.removeStudioResponse(path)
}

func (s *StudioJobService) retryStudioSaving(claim StudioJobClaim, code string) {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	if err := s.repo.RetrySaving(ctx, claim, 30*time.Second, code); err != nil {
		slog.Error("Studio archive retry state write failed", "task_id", claim.TaskID, "error", err)
	}
}

func studioBillingRequestID(claim StudioJobClaim) string {
	return "client:" + studioClientRequestID(claim)
}
func studioClientRequestID(claim StudioJobClaim) string {
	// ClientRequestID middleware preserves only IDs up to 64 bytes. A bounded
	// digest keeps billing dedup stable even for the longest valid task ID.
	return "studio-" + strings.TrimPrefix(studioOutputID(claim, 0, "request"), "job-")
}

func (s *StudioJobService) reconcileStudioJobs(ctx context.Context) {
	defer s.workers.Done()
	ticker := time.NewTicker(20 * time.Second)
	defer ticker.Stop()
	lastClean := time.Time{}
	for {
		s.reconcileStudioJobsOnce(ctx)
		if time.Since(lastClean) >= 15*time.Minute {
			s.cleanupStudioSpool(ctx)
			lastClean = time.Now()
		}
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
		}
	}
}

func (s *StudioJobService) cleanupStudioSpool(ctx context.Context) {
	entries, err := os.ReadDir(s.spoolDir)
	if err != nil {
		return
	}
	for _, entry := range entries {
		if ctx.Err() != nil {
			return
		}
		if entry.IsDir() {
			continue
		}
		name := entry.Name()
		path := filepath.Join(s.spoolDir, name)
		info, err := entry.Info()
		if err != nil {
			continue
		}
		if strings.HasSuffix(name, ".tmp") {
			if time.Since(info.ModTime()) > time.Hour {
				s.removeStudioSpool(path)
			}
			continue
		}
		responseName := name
		if offset := strings.Index(name, ".json.image-"); offset >= 0 {
			if index, err := strconv.Atoi(name[offset+len(".json.image-"):]); err != nil || index < 0 || index >= 10 {
				continue
			}
			responseName = name[:offset+len(".json")]
		}
		if !strings.HasSuffix(responseName, ".json") || time.Since(info.ModTime()) < time.Hour {
			continue
		}
		stem := strings.TrimSuffix(responseName, ".json")
		first, last := strings.Index(stem, "__"), strings.LastIndex(stem, "__")
		if first < 2 || last <= first || stem[0] != 'u' {
			continue
		}
		userID, userErr := strconv.ParseInt(stem[1:first], 10, 64)
		ordinal, ordinalErr := strconv.Atoi(stem[last+2:])
		if userErr != nil || ordinalErr != nil {
			continue
		}
		job, err := s.repo.Get(ctx, userID, stem[first+2:last])
		if err == nil {
			if ordinal < 0 || ordinal >= len(job.Calls) {
				continue
			}
			call := job.Calls[ordinal]
			if job.DeletedAt == nil && (call.Status == StudioJobGenerating || call.Status == StudioJobSaving) {
				continue
			}
			// A malformed paid response is not retried, but its private raw spool
			// remains for one day for operator diagnosis or manual recovery.
			if (call.ErrorCode == "INVALID_IMAGE_RESULT" || call.ErrorCode == "GALLERY_QUOTA_EXCEEDED") && time.Since(info.ModTime()) < 24*time.Hour {
				continue
			}
		} else if !errors.Is(err, ErrStudioJobNotFound) {
			continue
		}
		s.removeStudioSpool(path)
	}
}

func (s *StudioJobService) reconcileStudioJobsOnce(ctx context.Context) {
	if ctx.Err() != nil {
		return
	}
	expired, err := s.repo.ExpiredGenerating(ctx, 50)
	if err != nil {
		slog.Error("Studio expired-call scan failed", "error", err)
	} else {
		for _, claim := range expired {
			if claim.Call.SentAt == nil {
				_ = s.repo.RecoverUnsent(ctx, claim)
				continue
			}
			path := s.studioSpoolPath(claim)
			if _, err := os.Stat(path); err == nil {
				_ = s.repo.RecoverSaving(ctx, claim, path, s.owner)
			} else {
				_ = s.repo.MarkUnknown(ctx, claim)
			}
		}
	}
	foreign, err := s.repo.ExpiredSavingOtherOwner(ctx, s.owner, 50)
	if err != nil {
		slog.Error("Studio foreign spool scan failed", "error", err)
	} else {
		for _, claim := range foreign {
			s.markStudioSavingLost(claim, true)
		}
	}
	candidates, err := s.repo.FinalizationCandidates(ctx, 50)
	if err != nil {
		slog.Error("Studio finalization scan failed", "error", err)
	} else {
		for _, job := range candidates {
			s.finalizeStudioJob(ctx, job)
		}
	}
	cleanup, err := s.repo.CleanupCandidates(ctx, 50)
	if err != nil {
		slog.Error("Studio cleanup scan failed", "error", err)
	} else {
		for _, job := range cleanup {
			if job.Status == StudioJobCancelled {
				if err := s.gallery.DeleteTask(ctx, job.UserID, job.TaskID); err != nil {
					continue
				}
			}
			assets, err := s.repo.ReleaseInputs(ctx, job.UserID, job.TaskID)
			if err == nil {
				for _, asset := range assets {
					s.gallery.cleanupAsset(ctx, asset)
				}
			}
		}
	}
	pending, err := s.repo.PendingBilling(ctx, 50)
	if err != nil {
		slog.Error("Studio billing reconciliation scan failed", "error", err)
	} else {
		for _, claim := range pending {
			recorded, err := s.repo.BillingRecorded(ctx, studioBillingRequestID(claim), claim.APIKeyID)
			if err != nil {
				continue
			}
			if recorded {
				_ = s.repo.SetBillingState(ctx, claim, "recorded")
			} else if time.Since(claim.Call.UpdatedAt) > 10*time.Minute {
				_ = s.repo.SetBillingState(ctx, claim, "unknown")
			}
		}
	}
}

func (s *StudioJobService) finalizeStudioJob(ctx context.Context, job StudioJob) {
	if job.DeletedAt != nil {
		return
	}
	outputs, raws := []string{}, []string{}
	var failures []map[string]any
	unknown := false
	finished := job.CreatedAt.UnixMilli()
	for _, call := range job.Calls {
		if at := call.UpdatedAt.UnixMilli(); at > finished {
			finished = at
		}
		switch call.Status {
		case StudioJobQueued, StudioJobGenerating, StudioJobSaving:
			return
		case StudioJobUnknown:
			unknown = true
			failures = append(failures, map[string]any{"requestIndex": call.Ordinal, "error": call.ErrorMessage})
		case StudioJobFailed:
			failures = append(failures, map[string]any{"requestIndex": call.Ordinal, "error": call.ErrorMessage})
		}
		if call.BillingState == "pending" {
			return
		}
		if call.BillingState == "unknown" {
			unknown = true
			failures = append(failures, map[string]any{"requestIndex": call.Ordinal, "error": "billing confirmation is unavailable; check usage records"})
		}
		outputs = append(outputs, call.OutputImageIDs...)
		raws = append(raws, call.RawImageIDs...)
	}
	if job.Spec.Protocol == "grok" && len(outputs) < job.Spec.ImageCount {
		for index := len(outputs); index < job.Spec.ImageCount; index++ {
			failures = append(failures, map[string]any{
				"requestIndex": index,
				"error":        "upstream returned fewer images than requested",
			})
		}
	}
	status, taskStatus := StudioJobCompleted, "done"
	if unknown && len(outputs) == 0 {
		status = StudioJobUnknown
		taskStatus = "error"
	} else if len(outputs) == 0 {
		status = StudioJobFailed
		taskStatus = "error"
	} else if len(failures) != 0 {
		status = StudioJobPartial
	}
	var task map[string]any
	if err := json.Unmarshal(job.Task, &task); err != nil {
		return
	}
	task["outputImages"] = outputs
	if len(raws) != 0 {
		task["transparentOriginalImages"] = raws
	}
	if len(failures) != 0 {
		task["outputErrors"] = failures
	}
	task["status"] = taskStatus
	task["finishedAt"] = finished
	if created, ok := task["createdAt"].(float64); ok {
		elapsed := finished - int64(created)
		if elapsed < 0 {
			elapsed = 0
		}
		task["elapsed"] = elapsed
	}
	if taskStatus == "error" {
		message := "image generation failed"
		if unknown {
			message = "upstream result is uncertain; no automatic retry was made"
		} else if len(failures) != 0 {
			message = fmt.Sprint(failures[0]["error"])
		}
		task["error"] = message
	} else {
		task["error"] = nil
	}
	raw, err := json.Marshal(task)
	if err != nil {
		return
	}
	if _, err := s.gallery.SaveAcceptedJobTask(ctx, job.UserID, raw); err != nil {
		if !errors.Is(err, ErrGalleryGone) {
			slog.Error("Studio task archive failed", "task_id", job.TaskID, "error", err)
		}
		return
	}
	if err := s.repo.Finalize(ctx, job.UserID, job.TaskID, status, raw); err != nil {
		return
	}
	assets, err := s.repo.ReleaseInputs(ctx, job.UserID, job.TaskID)
	if err == nil {
		for _, asset := range assets {
			s.gallery.cleanupAsset(ctx, asset)
		}
	}
}
