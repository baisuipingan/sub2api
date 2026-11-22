package service

import (
	"context"
	"errors"
	"testing"
	"time"

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

func (r *workbenchSettingsStub) GetValue(_ context.Context, key string) (string, error) {
	if value, ok := r.values[key]; ok {
		return value, nil
	}
	return "", ErrSettingNotFound
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
	require.Equal(t, map[string]string{
		SettingKeyImageStudioEnabled: "true",
		SettingKeyImageStudioGroupID: "12",
		"workbench_chat_enabled":     "false",
		"workbench_office_enabled":   "false",
		"workbench_search_enabled":   "false",
		"workbench_files_enabled":    "false",
	}, repo.written)
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

func TestWorkbenchConfigLeavesLegacyBudgetStorageIntact(t *testing.T) {
	ctx := context.Background()
	legacy := map[string]string{"workbench_chat_concurrent_runs": "5", "workbench_office_concurrent_runs": "2",
		"workbench_max_model_calls": "10", "workbench_max_tool_calls": "20", "workbench_max_output_tokens": "12288",
		"workbench_max_run_seconds": "750", "workbench_max_run_credits": "1234567"}
	repo := &workbenchSettingsStub{values: map[string]string{}}
	for key, value := range legacy {
		repo.values[key] = value
	}
	settings := NewSettingService(repo, nil)
	require.NoError(t, settings.UpdateWorkbenchConfig(ctx, WorkbenchConfig{ChatEnabled: true}))
	loaded, err := settings.GetWorkbenchConfig(ctx)
	require.NoError(t, err)
	require.True(t, loaded.ChatEnabled)
	for key, value := range legacy {
		require.NotContains(t, repo.written, key)
		require.Equal(t, value, repo.values[key])
	}
}

func TestWorkbenchChatUsesPublishedAgentGroupWithoutImageGroup(t *testing.T) {
	ctx := context.Background()
	const agentConfig = `{"enabled":true,"group_id":12}`
	repo := &workbenchSettingsStub{values: map[string]string{SettingKeyDesktopAgentConfig: agentConfig}}
	groups := &workbenchGroupStub{group: &Group{ID: 12, Status: StatusActive, Platform: PlatformComposite, SubscriptionType: SubscriptionTypeStandard}}
	svc := NewWorkbenchAdminService(nil, NewSettingService(repo, nil), groups)
	cfg, err := svc.UpdateConfig(ctx, WorkbenchConfig{ChatEnabled: true, AgentGroupID: 99})
	require.NoError(t, err)
	require.EqualValues(t, 12, cfg.AgentGroupID)
	require.Zero(t, cfg.ImageStudioGroupID)
	require.Equal(t, agentConfig, repo.values[SettingKeyDesktopAgentConfig])
	require.NotContains(t, repo.written, SettingKeyDesktopAgentConfig)

	repo.values[SettingKeyDesktopAgentConfig] = `{"enabled":false,"group_id":12}`
	_, err = svc.UpdateConfig(ctx, WorkbenchConfig{ChatEnabled: true, AgentGroupID: 12})
	require.ErrorIs(t, err, ErrWorkbenchInvalid)
}
