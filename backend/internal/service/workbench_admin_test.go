package service

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/config"
	"github.com/stretchr/testify/require"
)

type workbenchReadStub struct {
	counts WorkbenchTaskCounts
	calls  int
	filter WorkbenchTaskFilter
}

func (r *workbenchReadStub) Overview(context.Context, time.Time, time.Time) (*WorkbenchOverview, error) {
	r.calls++
	return &WorkbenchOverview{Tasks: r.counts}, nil
}
func (r *workbenchReadStub) ListTasks(_ context.Context, f WorkbenchTaskFilter) (*WorkbenchTaskPage, error) {
	r.calls++
	r.filter = f
	return &WorkbenchTaskPage{Items: []WorkbenchTaskSummary{}}, nil
}

type workbenchSettingsStub struct {
	SettingRepository
	values   map[string]string
	written  map[string]string
	writeErr error
}

func (r *workbenchSettingsStub) GetMultiple(_ context.Context, keys []string) (map[string]string, error) {
	out := map[string]string{}
	for _, key := range keys {
		out[key] = r.values[key]
	}
	return out, nil
}
func (r *workbenchSettingsStub) SetMultiple(_ context.Context, values map[string]string) error {
	r.written = values
	if r.writeErr != nil {
		return r.writeErr
	}
	for key, value := range values {
		r.values[key] = value
	}
	return nil
}

type workbenchGroupStub struct {
	GroupRepository
	group *Group
	err   error
}

func (r *workbenchGroupStub) GetByID(context.Context, int64) (*Group, error) { return r.group, r.err }

func TestWorkbenchOverviewSuccessRateExcludesCancelledAndUnknown(t *testing.T) {
	repo := &workbenchReadStub{counts: WorkbenchTaskCounts{Total: 20, Completed: 6, Partial: 2, Failed: 2, Cancelled: 4, Unknown: 6}}
	svc := NewWorkbenchAdminService(repo, nil, nil)
	result, err := svc.Overview(context.Background(), 7)
	require.NoError(t, err)
	require.NotNil(t, result.SuccessRate)
	require.Equal(t, 0.6, *result.SuccessRate)
	require.Equal(t, "studio_cloud_jobs", result.Scope)
	require.Equal(t, 7, result.Days)
	repo.counts = WorkbenchTaskCounts{Queued: 1, Unknown: 2, Cancelled: 3}
	result, err = svc.Overview(context.Background(), 30)
	require.NoError(t, err)
	require.Nil(t, result.SuccessRate)
	_, err = svc.Overview(context.Background(), 365)
	require.ErrorIs(t, err, ErrWorkbenchInvalid)
	require.Equal(t, 2, repo.calls)
}

func TestWorkbenchTaskFilterRejectsUnboundedAndInvalidQueries(t *testing.T) {
	repo := &workbenchReadStub{}
	svc := NewWorkbenchAdminService(repo, nil, nil)
	for _, f := range []WorkbenchTaskFilter{{Page: 0, PageSize: 20}, {Page: 10001, PageSize: 20}, {Page: 1, PageSize: 101}, {Page: 1, PageSize: 20, Status: "anything"}, {Page: 1, PageSize: 20, UserID: -1}, {Page: 1, PageSize: 20, Model: "model\nsecret"}, {Page: 1, PageSize: 20, Days: 365}} {
		_, err := svc.ListTasks(context.Background(), f)
		require.ErrorIs(t, err, ErrWorkbenchInvalid)
	}
	require.Zero(t, repo.calls)
	_, err := svc.ListTasks(context.Background(), WorkbenchTaskFilter{Page: 2, PageSize: 20, Status: StudioJobUnknown, UserID: 7, Model: " gpt-image-2 "})
	require.NoError(t, err)
	require.Equal(t, "gpt-image-2", repo.filter.Model)
	require.Equal(t, int64(7), repo.filter.UserID)
}

func TestWorkbenchConfigOnlyWritesItsOwnKeysAndInvalidatesAfterSuccess(t *testing.T) {
	repo := &workbenchSettingsStub{values: map[string]string{"site_name": "keep", SettingKeyImageStudioEnabled: "false"}}
	settings := NewSettingService(repo, nil)
	invalidated := 0
	settings.SetOnUpdateCallback(func() { invalidated++ })
	groups := &workbenchGroupStub{group: &Group{ID: 12, Status: StatusActive, AllowImageGeneration: true}}
	svc := NewWorkbenchAdminService(nil, settings, groups)
	_, err := svc.UpdateConfig(context.Background(), WorkbenchConfig{ImageStudioEnabled: true, ImageStudioGroupID: 12})
	require.NoError(t, err)
	require.Len(t, repo.written, 14)
	require.Equal(t, "keep", repo.values["site_name"])
	require.Equal(t, 1, invalidated)
	loaded, err := svc.GetConfig(context.Background())
	require.NoError(t, err)
	require.True(t, loaded.ImageStudioEnabled)
	require.Equal(t, int64(12), loaded.ImageStudioGroupID)
	groups.group.AllowImageGeneration = false
	_, err = svc.UpdateConfig(context.Background(), WorkbenchConfig{ImageStudioEnabled: true, ImageStudioGroupID: 12})
	require.ErrorIs(t, err, ErrWorkbenchInvalid)
	require.Equal(t, 1, invalidated)
	_, err = svc.UpdateConfig(context.Background(), WorkbenchConfig{ImageStudioEnabled: true})
	require.ErrorIs(t, err, ErrWorkbenchInvalid)
	_, err = svc.UpdateConfig(context.Background(), WorkbenchConfig{ImageStudioEnabled: false, ImageStudioGroupID: 12})
	require.NoError(t, err, "admission can be disabled even after the configured group became unavailable")
	require.Equal(t, 2, invalidated)
	repo.writeErr = errors.New("write unavailable")
	_, err = svc.UpdateConfig(context.Background(), WorkbenchConfig{})
	require.Error(t, err)
	require.Equal(t, 2, invalidated)
}

func TestOfficeBudgetSettingsDefaultsBoundsAndDeploymentClamp(t *testing.T) {
	ctx := context.Background()
	repo := &workbenchSettingsStub{values: map[string]string{}}
	deployment := &config.Config{OfficeAgent: config.OfficeAgentConfig{MaxModelCalls: 12, MaxToolCalls: 24, MaxOutputTokens: 16384, MaxRunSeconds: 900}}
	settings := NewSettingService(repo, deployment)
	defaults, err := settings.GetWorkbenchConfig(ctx)
	require.NoError(t, err)
	require.Equal(t, 3, defaults.ChatConcurrentRuns)
	require.Equal(t, 1, defaults.OfficeConcurrentRuns)
	require.Equal(t, 12, defaults.MaxModelCalls)
	require.Equal(t, 24, defaults.MaxToolCalls)
	require.Equal(t, 16384, defaults.MaxOutputTokens)
	require.Equal(t, 900, defaults.MaxRunSeconds)
	require.Zero(t, defaults.MaxRunCredits)
	require.Equal(t, 8, defaults.DeploymentLimits.ChatConcurrentRuns)
	require.Equal(t, 2, defaults.DeploymentLimits.OfficeConcurrentRuns)
	for _, mutate := range []func(*WorkbenchConfig){
		func(c *WorkbenchConfig) { c.ChatConcurrentRuns = 9 },
		func(c *WorkbenchConfig) { c.OfficeConcurrentRuns = 3 },
		func(c *WorkbenchConfig) { c.MaxModelCalls = 13 },
		func(c *WorkbenchConfig) { c.MaxToolCalls = 25 },
		func(c *WorkbenchConfig) { c.MaxOutputTokens = 16385 },
		func(c *WorkbenchConfig) { c.MaxOutputTokens = 127 },
		func(c *WorkbenchConfig) { c.MaxRunSeconds = 901 },
		func(c *WorkbenchConfig) { c.MaxRunSeconds = 29 },
		func(c *WorkbenchConfig) { c.MaxRunCredits = -1 },
		func(c *WorkbenchConfig) { c.MaxRunCredits = 9_007_199_254_740_992 },
	} {
		bad := *defaults
		mutate(&bad)
		require.ErrorIs(t, settings.UpdateWorkbenchConfig(ctx, bad), ErrWorkbenchInvalid)
		require.Nil(t, repo.written)
	}
	custom := *defaults
	custom.MaxModelCalls, custom.MaxToolCalls, custom.MaxOutputTokens, custom.MaxRunSeconds = 10, 20, 12288, 750
	custom.ChatConcurrentRuns, custom.OfficeConcurrentRuns, custom.MaxRunCredits = 5, 2, 1234567
	require.NoError(t, settings.UpdateWorkbenchConfig(ctx, custom))
	loaded, err := settings.GetWorkbenchConfig(ctx)
	require.NoError(t, err)
	require.Equal(t, custom, *loaded)
	deployment.OfficeAgent = config.OfficeAgentConfig{MaxModelCalls: 6, MaxToolCalls: 12, MaxOutputTokens: 8192, MaxRunSeconds: 600}
	loaded, err = settings.GetWorkbenchConfig(ctx)
	require.NoError(t, err)
	require.Equal(t, 6, loaded.MaxModelCalls)
	require.Equal(t, 12, loaded.MaxToolCalls)
	require.Equal(t, 8192, loaded.MaxOutputTokens)
	require.Equal(t, 600, loaded.MaxRunSeconds)
	require.EqualValues(t, 1234567, loaded.MaxRunCredits)
	require.Equal(t, "10", repo.values["workbench_max_model_calls"], "lower deployment caps do not overwrite stored preferences or historical run snapshots")
}
