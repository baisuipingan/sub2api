package service

import (
	"context"
	"log/slog"
	"time"
)

// finishStudioJobAfterCall is the normal completion path. Periodic reconciliation
// remains a recovery path for crashes, unavailable storage, and delayed billing.
// It never dispatches or repeats a model request.
func (s *StudioJobService) finishStudioJobAfterCall(userID int64, taskID string) {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	job, err := s.repo.Get(ctx, userID, taskID)
	if err != nil || job == nil || job.DeletedAt != nil {
		return
	}
	if job.Status != StudioJobQueued && job.Status != StudioJobGenerating && job.Status != StudioJobSaving {
		return
	}
	for _, call := range job.Calls {
		if call.BillingState != "pending" {
			continue
		}
		claim := StudioJobClaim{StudioJob: *job, Call: call}
		if s.reconcileStudioCallBilling(ctx, claim) {
			latest, err := s.repo.Get(ctx, userID, taskID)
			if err != nil || latest == nil {
				return
			}
			job = latest
		}
	}
	if s.gallery != nil {
		s.finalizeStudioJob(ctx, *job)
	}
}

func (s *StudioJobService) reconcileStudioCallBilling(ctx context.Context, claim StudioJobClaim) bool {
	recorded, err := s.repo.BillingRecorded(ctx, studioBillingRequestID(claim), claim.APIKeyID)
	if err != nil {
		return false
	}
	state := ""
	if recorded {
		state = "recorded"
	} else if time.Since(claim.Call.UpdatedAt) > 10*time.Minute {
		state = "unknown"
	}
	if state == "" {
		return false
	}
	if err := s.repo.SetBillingState(ctx, claim, state); err != nil {
		slog.Warn("Studio billing state update deferred", "task_id", claim.TaskID, "error", err)
		return false
	}
	return true
}
