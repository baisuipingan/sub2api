package service

import (
	"context"
	"encoding/json"
	"errors"
	"path"
	"strings"
	"time"
)

var ErrOfficePrintPreview = errors.New("office print preview render failed")

type WorkbenchPrintPreview struct {
	Status       string     `json:"status"`
	PreviewURL   string     `json:"preview_url,omitempty"`
	ExpiresAt    *time.Time `json:"expires_at,omitempty"`
	RetryAfterMS int        `json:"retry_after_ms,omitempty"`
}

func workbenchPrintKey(original string) string {
	return path.Join(path.Dir(original), "print.pdf")
}

func (s *OfficeAgentService) HasPrintPreview(ctx context.Context) bool {
	if s == nil || !s.cfg.Enabled || s.resources == nil || !s.remoteOfficeConfigured(ctx) {
		return false
	}
	status, err := s.ChatStatus(ctx)
	return err == nil && status.OfficeEnabled && status.FilesEnabled
}

func (s *OfficeAgentService) PreparePrintPreview(ctx context.Context, userID int64, versionID string, retry bool) (*WorkbenchPrintPreview, error) {
	if !s.HasPrintPreview(ctx) {
		return nil, ErrOfficeDisabled
	}
	v, err := s.resources.repo.GetResourceVersion(ctx, userID, versionID, true)
	if err != nil {
		return nil, err
	}
	return s.preparePrintPreviewVersion(ctx, v, retry)
}

func (s *OfficeAgentService) preparePrintPreviewVersion(ctx context.Context, v *OfficeResource, retry bool) (*WorkbenchPrintPreview, error) {
	if !strings.EqualFold(path.Ext(v.Name), ".docx") {
		return nil, ErrOfficeResourceType
	}
	key := workbenchPrintKey(v.ObjectKey)
	if v.PreviewKey == "" {
		result, err := s.renderPrintPreview(ctx, v, key, retry)
		if errors.Is(err, ErrOfficePrintPreview) || errors.Is(err, ErrOfficeConflict) {
			// Another request can commit while this one is looking up its attempt.
			current, readErr := s.resources.repo.GetResourceVersion(ctx, v.UserID, v.VersionID, true)
			if readErr != nil || current.PreviewKey != key || current.PreviewMIME != "application/pdf" {
				return nil, err
			}
		} else if err != nil || result != nil {
			return result, err
		}
	} else {
		if v.PreviewMIME != "application/pdf" {
			return nil, ErrOfficeResourceType
		}
		key = v.PreviewKey
		if item, err := s.existingOfficeDispatch(ctx, officePrintJobKey(v.VersionID)); err == nil && item != nil && item.State == "active" {
			_ = s.finishOfficeDispatch(ctx, &officeDispatchTarget{item: item}, "done")
		}
	}
	store, err := s.resources.storage(ctx, v.Endpoint, v.Region, v.Bucket)
	if err != nil {
		return nil, err
	}
	const expiry = 10 * time.Minute
	link, err := store.PresignGet(ctx, key, expiry)
	if err != nil {
		return nil, err
	}
	expires := time.Now().Add(expiry)
	return &WorkbenchPrintPreview{Status: "ready", PreviewURL: link, ExpiresAt: &expires}, nil
}

func officePrintJobKey(versionID string) string {
	return officeDigest([]byte("print-pdf:" + versionID))
}

func (s *OfficeAgentService) renderPrintPreview(ctx context.Context, v *OfficeResource, key string, retry bool) (result *WorkbenchPrintPreview, resultErr error) {
	jobKey := officePrintJobKey(v.VersionID)
	pending := &WorkbenchPrintPreview{Status: "pending", RetryAfterMS: 1000}
	ctx, target, err := s.officeDispatchTarget(ctx, OfficeDispatch{Key: jobKey, UserID: v.UserID, VersionID: v.VersionID}, "render_docx_pdf", retry)
	if errors.Is(err, ErrOfficeStepBusy) {
		return pending, nil
	}
	if err != nil {
		if errors.Is(err, ErrOfficeConflict) {
			return nil, ErrOfficePrintPreview
		}
	}
	defer func() {
		switch {
		case resultErr == nil && result == nil:
			resultErr = s.finishOfficeDispatch(ctx, target, "done")
		case errors.Is(resultErr, ErrOfficePrintPreview), permanentOfficeDeliveryError(resultErr), resultErr != nil && target.expired():
			if finishErr := s.finishOfficeDispatch(ctx, target, "cancelled"); finishErr != nil {
				resultErr = errors.Join(resultErr, finishErr)
			}
		}
	}()
	if err != nil {
		return nil, err
	}
	store, err := s.resources.storage(ctx, v.Endpoint, v.Region, v.Bucket)
	if err != nil {
		return nil, err
	}
	endpoint := target.endpoint(jobKey)
	data, err := s.officeWorkerReceipt(ctx, target, endpoint, func() ([]byte, error) {
		return s.officeWorkerRequest(ctx, "render_docx_pdf", json.RawMessage(`{}`), v, nil)
	})
	if errors.Is(err, errOfficeOutputExpired) {
		return nil, ErrOfficePrintPreview
	}
	if errors.Is(err, ErrOfficeStepBusy) {
		return pending, nil
	}
	if err != nil {
		return nil, err
	}
	var job officeWorkerJob
	if err := json.Unmarshal(data, &job); err != nil {
		return nil, ErrOfficePrintPreview
	}
	if job.Status == "running" || job.Status == "queued" {
		return pending, nil
	}
	if job.Status != "completed" || !validOfficeWorkerOutput(job.Output) || path.Ext(job.Output.Name) != ".pdf" {
		return nil, ErrOfficePrintPreview
	}
	meta := job.Output
	var assignment *OfficeDispatch
	if target != nil {
		assignment = target.item
	}
	err = s.resources.repo.StorePrintPreview(ctx, v.UserID, v.VersionID, key, meta.Bytes, meta.SHA256, assignment, func() error {
		err := s.uploadOfficeWorkerOutput(ctx, store, endpoint, key, "application/pdf", "", meta)
		if target == nil || target.item.Attempt <= 1 || !errors.Is(err, errOfficeStoredOutputMismatch) {
			return err
		}
		// A previous attempt may have uploaded before its database commit failed.
		// StorePrintPreview holds the version and this assignment for the whole replacement.
		if err := store.Delete(ctx, key); err != nil {
			return err
		}
		return s.uploadOfficeWorkerOutput(ctx, store, endpoint, key, "application/pdf", "", meta)
	})
	switch {
	case errors.Is(err, ErrOfficeStepBusy) && !target.expired():
		return pending, nil
	case errors.Is(err, errOfficeOutputExpired):
		return nil, ErrOfficePrintPreview
	default:
		return nil, err
	}
}
