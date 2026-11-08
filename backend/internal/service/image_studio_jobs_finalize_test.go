//go:build unit

package service

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

type studioFinishRepo struct {
	StudioJobRepository
	job       StudioJob
	recorded  bool
	finalized int
}

func (r *studioFinishRepo) Get(context.Context, int64, string) (*StudioJob, error) {
	copyJob := r.job
	copyJob.Calls = append([]StudioJobCall(nil), r.job.Calls...)
	return &copyJob, nil
}
func (r *studioFinishRepo) BillingRecorded(context.Context, string, int64) (bool, error) {
	return r.recorded, nil
}
func (r *studioFinishRepo) SetBillingState(_ context.Context, claim StudioJobClaim, state string) error {
	r.job.Calls[claim.Call.Ordinal].BillingState = state
	return nil
}
func (r *studioFinishRepo) Finalize(_ context.Context, _ int64, _, status string, task json.RawMessage) error {
	r.finalized++
	r.job.Status, r.job.Task = status, task
	return nil
}
func (r *studioFinishRepo) ReleaseInputs(context.Context, int64, string) ([]GalleryAsset, error) {
	return nil, nil
}

func TestStudioCallFinishesImmediatelyWithoutPeriodicScan(t *testing.T) {
	for _, tc := range []struct {
		name, billing string
		recorded      bool
		wantFinal     bool
	}{
		{"gateway rejection", "not_started", false, true},
		{"billing already committed", "pending", true, true},
		{"billing not committed yet", "pending", false, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			gallery, _, _, _ := galleryTestService(t)
			created := time.Now().Add(-time.Second)
			task, err := json.Marshal(map[string]any{
				"id": "instant-finish", "status": "running", "createdAt": created.UnixMilli(),
				"inputImageIds": []string{}, "outputImages": []string{},
			})
			require.NoError(t, err)
			repo := &studioFinishRepo{recorded: tc.recorded, job: StudioJob{
				UserID: 9, TaskID: "instant-finish", APIKeyID: 11, Status: StudioJobGenerating,
				Task: task, CreatedAt: created,
				Calls: []StudioJobCall{{Ordinal: 0, Status: StudioJobFailed, BillingState: tc.billing,
					ErrorCode: "GATEWAY_HTTP_403", ErrorMessage: "insufficient balance", UpdatedAt: created.Add(100 * time.Millisecond)}},
			}}
			svc := &StudioJobService{repo: repo, gallery: gallery}
			svc.finishStudioJobAfterCall(9, "instant-finish")
			if !tc.wantFinal {
				require.Zero(t, repo.finalized, "do not finish before billing evidence becomes available")
				return
			}
			require.Equal(t, 1, repo.finalized, "the terminal parent is written in this call without any ticker")
			require.Equal(t, StudioJobFailed, repo.job.Status)
			var finished map[string]any
			require.NoError(t, json.Unmarshal(repo.job.Task, &finished))
			require.Equal(t, float64(100), finished["elapsed"])
			svc.finishStudioJobAfterCall(9, "instant-finish")
			require.Equal(t, 1, repo.finalized, "terminal parents are not republished")
		})
	}
}
