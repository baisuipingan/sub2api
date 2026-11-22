package service

import (
	"context"
	"net/http"
	"strings"
	"testing"

	"github.com/Wei-Shaw/sub2api/internal/config"
	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
	"github.com/tidwall/gjson"
)

type workbenchRunAdmissionRecorder struct {
	OfficeAgentRepository
	admission OfficeRunAdmission
}

func (*workbenchRunAdmissionRecorder) GetConversation(_ context.Context, userID int64, id string) (*OfficeConversation, error) {
	return &OfficeConversation{ID: id, UserID: userID}, nil
}

func (r *workbenchRunAdmissionRecorder) AdmitRun(_ context.Context, in OfficeRunAdmission) (*OfficeRun, bool, error) {
	r.admission = in
	return &OfficeRun{ID: in.RunID, Limits: in.Limits}, true, nil
}

func TestWorkbenchSubmitUsesDeploymentLimitsInsteadOfLegacyTaskBudgets(t *testing.T) {
	d := config.OfficeAgentConfig{Enabled: true, WorkerToken: strings.Repeat("x", 32), MaxConcurrentCalls: 1,
		MaxModelCalls: 12, MaxToolCalls: 24, MaxOutputTokens: 16384, MaxRunSeconds: 900}
	settings := NewSettingService(&workbenchSettingsStub{values: map[string]string{
		"workbench_chat_enabled": "true", "workbench_office_enabled": "true", "workbench_files_enabled": "true",
		SettingKeyImageStudioGroupID: "9", SettingKeyDesktopAgentConfig: `{"enabled":true,"group_id":7,
			"models":[{"id":"gpt-6.1-sol","default_reasoning":"high"}],"default_model_id":"gpt-6.1-sol"}`,
		"workbench_chat_concurrent_runs": "7", "workbench_office_concurrent_runs": "2",
		"workbench_max_model_calls": "1", "workbench_max_tool_calls": "1", "workbench_max_output_tokens": "128",
		"workbench_max_run_seconds": "30", "workbench_max_run_credits": "1",
	}}, &config.Config{OfficeAgent: d})
	repo := &workbenchRunAdmissionRecorder{}
	svc := &OfficeAgentService{cfg: d, repo: repo, settings: settings,
		groups:  NewGroupService(&workbenchGroupStub{group: &Group{ID: 7, Status: StatusActive, Platform: PlatformComposite, SubscriptionType: SubscriptionTypeStandard}}, nil),
		catalog: &desktopCatalogStub{models: []string{"gpt-6.1-sol", "gpt-6-sol"}},
		resolve: func(context.Context, int64, string) (int64, int64, error) { return 5, 7, nil }}
	svc.BindGatewayRouter(http.NotFoundHandler())

	for _, legacyTaskMode := range []bool{false, true} {
		run, err := svc.SubmitChat(context.Background(), 4, uuid.NewString(), WorkbenchChatSubmit{
			RunID: uuid.NewString(), Model: "gpt-6.1-sol", Content: "Hello", TaskMode: legacyTaskMode,
		})
		require.NoError(t, err)
		require.True(t, repo.admission.BrowserDriven)
		require.Equal(t, "chat", repo.admission.Mode)
		require.Zero(t, repo.admission.MaxActiveRuns)
		require.NotEmpty(t, repo.admission.AgentConfigRevision)
		require.Equal(t, "high", gjson.GetBytes(repo.admission.Parameters, "reasoning_effort").String())
		require.Equal(t, OfficeRunLimits{MaxModelCalls: 12, MaxToolCalls: 24, MaxOutputTokens: 16384, MaxRunSeconds: 900}, run.Limits)
	}

	_, err := svc.SubmitChat(context.Background(), 4, uuid.NewString(), WorkbenchChatSubmit{
		RunID: uuid.NewString(), Model: "gpt-6.1-sol", Content: "Explicit effort", ReasoningEffort: "low",
	})
	require.NoError(t, err)
	require.Equal(t, "low", gjson.GetBytes(repo.admission.Parameters, "reasoning_effort").String())

	_, err = svc.SubmitChat(context.Background(), 4, uuid.NewString(), WorkbenchChatSubmit{
		RunID: uuid.NewString(), Model: "gpt-6-sol", Content: "Unlisted model",
	})
	require.ErrorIs(t, err, ErrOfficeRouteChanged)

	repo.admission = OfficeRunAdmission{}
	svc.resolve = func(context.Context, int64, string) (int64, int64, error) { return 6, 8, nil }
	_, err = svc.SubmitChat(context.Background(), 4, uuid.NewString(), WorkbenchChatSubmit{
		RunID: uuid.NewString(), Model: "gpt-6.1-sol", Content: "Group changed",
	})
	require.ErrorIs(t, err, ErrOfficeRouteChanged)
	require.Empty(t, repo.admission.RunID)
}

func TestWorkbenchTurnRespectsDeploymentAndModelCeilings(t *testing.T) {
	svc := &OfficeAgentService{cfg: config.OfficeAgentConfig{MaxModelCalls: 12, MaxToolCalls: 24, MaxOutputTokens: 16384, MaxRunSeconds: 900}}
	modelMax := 4096
	require.Equal(t, OfficeRunLimits{MaxModelCalls: 12, MaxToolCalls: 24, MaxOutputTokens: 4096, MaxRunSeconds: 900}, svc.workbenchTurnLimits(&modelMax))
	svc.cfg.MaxModelCalls, svc.cfg.MaxToolCalls, svc.cfg.MaxOutputTokens = 3, 4, 2048
	require.Equal(t, OfficeRunLimits{MaxModelCalls: 3, MaxToolCalls: 4, MaxOutputTokens: 2048, MaxRunSeconds: 900}, svc.workbenchTurnLimits(&modelMax))
}
