package service

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
	"github.com/tidwall/gjson"
)

func TestWorkbenchPublicModelAliasUsesOnlyExplicitOfficialProfile(t *testing.T) {
	profiles := desktopAgentBuiltinProfiles()
	alias := "公司办公助理"
	require.Nil(t, workbenchModelProfile(alias, nil, profiles), "unknown public IDs must not guess a supplier family")
	bindings := map[string]DesktopAgentModelProfileBinding{alias: {ProfileID: "gpt-6.1-sol"}}
	require.True(t, validWorkbenchModelProfiles(bindings, profiles))
	profile := workbenchModelProfile(alias, bindings, profiles)
	require.NotNil(t, profile)
	require.Equal(t, "gpt-6.1-sol", profile.ID)
	require.EqualValues(t, 1050000, *profile.OfficialContextWindow)
	require.EqualValues(t, 128000, *profile.OfficialMaxOutputTokens)
	for _, invalid := range []map[string]DesktopAgentModelProfileBinding{
		{alias: {ProfileID: "not-a-profile"}},
		{alias: {ProfileID: "gpt-6.1-sol", Override: true}},
		{"bad\nmodel": {ProfileID: "gpt-6.1-sol"}},
		{"gpt-6.1-sol": {ProfileID: "gpt-6-luna"}},
		{alias: {ProfileID: ""}},
	} {
		require.False(t, validWorkbenchModelProfiles(invalid, profiles))
	}
	require.Equal(t, "gpt-6.1-sol", workbenchModelProfile("gpt-6.1-sol", nil, profiles).ID)
	require.Nil(t, workbenchModelProfile("gpt-6.1-sol-provider-suffix", nil, profiles), "public IDs use exact catalog declarations, never a prefix guess")
}

func TestWorkbenchDeepSeekV41FlashUsesOfficialFlashProfile(t *testing.T) {
	profiles := desktopAgentBuiltinProfiles()
	const publicID = "deepseek-v4.1-flash"
	profile := workbenchModelProfile(publicID, nil, profiles)
	require.NotNil(t, profile)
	require.Equal(t, "deepseek-flash", profile.ID)
	require.Equal(t, []string{"none", "low", "high", "max"}, profile.OfficialReasoningLevels)
	require.True(t, *profile.Model.SupportsToolCall)
	require.Nil(t, workbenchModelProfile(publicID+"-custom", nil, profiles))

	// A saved administrator binding remains authoritative for a public alias.
	bindings := map[string]DesktopAgentModelProfileBinding{publicID: {ProfileID: "deepseek-v4-pro"}}
	require.Equal(t, "deepseek-v4-pro", workbenchModelProfile(publicID, bindings, profiles).ID)
}

func TestWorkbenchChatCatalogFiltersDedicatedServicesAndUsesAvailableDefault(t *testing.T) {
	const preferred = "gpt-6.1-sol"
	settings := &workbenchSettingsStub{values: map[string]string{"workbench_chat_enabled": "true"}}
	svc, _ := officeBridgeFixture(http.NotFoundHandler())
	svc.settings = NewSettingService(settings, nil)
	svc.groups = NewGroupService(&workbenchGroupStub{group: &Group{
		ID: 12, Status: StatusActive, Platform: PlatformComposite, SubscriptionType: SubscriptionTypeStandard,
	}}, nil)
	accounts := &desktopModelAccounts{accounts: []Account{
		desktopModelAccount(PlatformOpenAI, map[string]any{
			"deepseek-asr": "deepseek-asr", "deepseek-tts": "deepseek-tts", "deepseek-ocr": "deepseek-ocr",
			"deepseek-fetch": "deepseek-fetch", "deepseek-search": "deepseek-search", "gpt-image-2": "gpt-image-2",
			"deepseek-search-assistant": preferred, "deepseek-v4.1-flash": "deepseek-v4.1-flash", preferred: preferred,
		}),
	}}
	svc.catalog = NewWorkbenchChatModelCatalog(accounts, compositeRouteRepoStub{}, nil)
	for _, tc := range []struct{ name, config, expected string }{
		{"shared default", `{"enabled":true,"group_id":12,"default_model_id":"gpt-6.1-sol"}`, preferred},
		{"legacy default", `{"enabled":true,"group_id":12,"model":{"id":"gpt-6.1-sol"}}`, preferred},
		{"excluded default", `{"enabled":true,"group_id":12,"default_model_id":"deepseek-asr"}`, ""},
		{"unavailable default", `{"enabled":true,"group_id":12,"default_model_id":"not-routed"}`, ""},
		{"malformed optional default", `{"enabled":true,"group_id":12,"default_model_id":7}`, ""},
	} {
		t.Run(tc.name, func(t *testing.T) {
			settings.values[SettingKeyDesktopAgentConfig] = tc.config
			catalog, err := svc.ChatModels(context.Background())
			require.NoError(t, err)
			ids := make([]string, 0, len(catalog.Models))
			for _, model := range catalog.Models {
				ids = append(ids, model.ID)
			}
			require.Equal(t, []string{"deepseek-search-assistant", "deepseek-v4.1-flash", preferred}, ids)
			require.Equal(t, tc.expected, catalog.DefaultModel)
		})
	}
}

func TestWorkbenchGeminiCatalogDefaultMatchesNativeRequest(t *testing.T) {
	const carbon = "gemini-4-carbon"
	const carbonAlias = "carbon-office"
	const carbonTier = "gemini-4-carbon-low"
	const responsesAlias = "carbon-responses"
	svc, _ := officeBridgeFixture(http.NotFoundHandler())
	svc.settings = NewSettingService(&workbenchSettingsStub{values: map[string]string{
		"workbench_chat_enabled":      "true",
		SettingKeyDesktopAgentConfig:  `{"enabled":true,"group_id":12}`,
		"workbench_model_profiles_v1": `{"carbon-office":{"profile_id":"gemini-4-carbon"},"carbon-responses":{"profile_id":"gemini-4-carbon"}}`,
	}}, nil)
	svc.groups = NewGroupService(&workbenchGroupStub{group: &Group{
		ID: 12, Status: StatusActive, Platform: PlatformComposite, SubscriptionType: SubscriptionTypeStandard,
	}}, nil)
	accounts := &desktopModelAccounts{accounts: []Account{
		desktopModelAccount(PlatformGemini, map[string]any{carbon: carbon, carbonTier: carbonTier}),
		desktopModelAccount(PlatformOpenAI, map[string]any{"openai-target": "openai-target"}),
	}}
	routes := compositeRouteRepoStub{routes: []CompositeModelRoute{
		{ID: 1, GroupID: 12, Enabled: true, MatchType: CompositeRouteMatchExact, PublicModel: carbonAlias, Endpoint: CompositeRouteEndpointGemini, TargetPlatform: PlatformGemini, UpstreamModel: carbon},
		{ID: 2, GroupID: 12, Enabled: true, MatchType: CompositeRouteMatchExact, PublicModel: responsesAlias, Endpoint: CompositeRouteEndpointResponses, TargetPlatform: PlatformOpenAI, UpstreamModel: "openai-target"},
	}}
	svc.catalog = NewWorkbenchChatModelCatalog(accounts, routes, nil)
	catalog, err := svc.ChatModels(context.Background())
	require.NoError(t, err)
	models := make(map[string]WorkbenchChatModel, len(catalog.Models))
	for _, item := range catalog.Models {
		models[item.ID] = item
	}
	for _, id := range []string{carbon, carbonAlias} {
		model := models[id]
		require.Equal(t, workbenchProtocolGemini, model.Protocol)
		require.Equal(t, []string{"low", "medium", "high"}, model.ReasoningEfforts)
		require.Equal(t, "medium", model.DefaultReasoningEffort)
		catalog, err := json.Marshal(model)
		require.NoError(t, err)
		require.Equal(t, "medium", gjson.GetBytes(catalog, "default_reasoning_effort").String())
		request := workbenchNativeRequest{Protocol: model.Protocol, Model: model.ID, ProfileModel: model.profileModel,
			History: []workbenchNativeMessage{{Role: "user", Text: "Hello"}}}
		_, payload, err := buildWorkbenchNativeRequest(request)
		require.NoError(t, err)
		require.Equal(t, "MEDIUM", gjson.GetBytes(payload, "generationConfig.thinkingConfig.thinkingLevel").String())
		request.ReasoningEffort = "high"
		_, payload, err = buildWorkbenchNativeRequest(request)
		require.NoError(t, err)
		require.Equal(t, "HIGH", gjson.GetBytes(payload, "generationConfig.thinkingConfig.thinkingLevel").String())
	}
	require.Equal(t, workbenchProtocolGemini, models[carbonTier].Protocol)
	require.Empty(t, models[carbonTier].ReasoningEfforts)
	require.Equal(t, workbenchProtocolResponses, models[responsesAlias].Protocol)
	require.NotEmpty(t, models[responsesAlias].ReasoningEfforts)
	for _, id := range []string{carbonTier, responsesAlias} {
		catalog, err := json.Marshal(models[id])
		require.NoError(t, err)
		require.False(t, gjson.GetBytes(catalog, "default_reasoning_effort").Exists())
	}
}

func TestWorkbenchDeepSeekReasoningAndToolsReachRawGateway(t *testing.T) {
	const model = "deepseek-v4.1-flash"
	const searchTool = `[{"type":"function","function":{"name":"web_search","parameters":{"type":"object"}}}]`
	const question = `{"role":"user","content":"请搜索资料"}`
	const toolHistory = `,{"role":"assistant","content":"","reasoning_content":"需查证","tool_calls":[{"id":"search-1","type":"function","function":{"name":"web_search","arguments":"{}"}}]},{"role":"tool","tool_call_id":"search-1","content":"结果"}`
	for _, effort := range []string{"none", "high"} {
		t.Run(effort, func(t *testing.T) {
			history := question
			if effort == "high" {
				history += toolHistory
			}
			body := []byte(`{"model":"` + model + `","reasoning_effort":"` + effort + `","messages":[` + history + `],"tools":` + searchTool + `,"max_tokens":1024}`)
			run := OfficeRun{Model: model, Mode: "chat", SearchEnabled: true, Limits: OfficeRunLimits{MaxOutputTokens: 1024}}
			if effort == "high" {
				_, err := normalizeOfficeModelRequest(body, run)
				require.ErrorIs(t, err, ErrOfficeInvalid, "old runs cannot pass back reasoning")
			}
			run.Parameters = json.RawMessage(`{"replay_tool_reasoning":true}`)
			payload, err := normalizeOfficeModelRequest(body, run)
			require.NoError(t, err)
			upstream := &httpUpstreamRecorder{resp: &http.Response{
				StatusCode: http.StatusOK,
				Header:     http.Header{"Content-Type": {"application/json"}},
				Body:       io.NopCloser(strings.NewReader(`{"id":"fixture","choices":[{"message":{"role":"assistant","content":"已找到资料"},"finish_reason":"stop"}],"usage":{"prompt_tokens":20,"completion_tokens":6}}`)),
			}}
			svc := &OpenAIGatewayService{cfg: deepSeekChatFallbackTestConfig(), httpUpstream: upstream}
			account := &Account{ID: 1, Platform: PlatformOpenCodeGo, Type: AccountTypeAPIKey, Concurrency: 1,
				Credentials: map[string]any{"api_key": "fixture-key", "base_url": DefaultOpenCodeGoBaseURL}}
			c, _ := gin.CreateTestContext(httptest.NewRecorder())
			c.Request = httptest.NewRequest(http.MethodPost, "/v1/chat/completions", bytes.NewReader(payload))
			_, err = svc.forwardAsRawChatCompletions(context.Background(), c, account, payload, "")
			require.NoError(t, err)
			require.Equal(t, model, gjson.GetBytes(upstream.lastBody, "model").String())
			require.Equal(t, effort, gjson.GetBytes(upstream.lastBody, "reasoning_effort").String())
			require.Equal(t, "web_search", gjson.GetBytes(upstream.lastBody, "tools.0.function.name").String())
			if effort == "high" {
				require.Equal(t, "需查证", gjson.GetBytes(upstream.lastBody, "messages.1.reasoning_content").String())
			}
		})
	}
}

func TestWorkbenchModelBindingsPersistSeparatelyFromDesktopSettings(t *testing.T) {
	ctx := context.Background()
	repo := &workbenchSettingsStub{values: map[string]string{SettingKeyDesktopAgentProfileCatalog: "unchanged-catalog", "desktop_agent_config_v1": "unchanged-desktop"}}
	settings := NewSettingService(repo, nil)
	svc := NewWorkbenchAdminService(nil, settings, nil)
	cfg := WorkbenchConfig{ModelProfiles: map[string]DesktopAgentModelProfileBinding{"my-office-model": {ProfileID: "gpt-6.1-sol"}}}
	_, err := svc.UpdateConfig(ctx, cfg)
	require.NoError(t, err)
	loaded, err := settings.GetWorkbenchConfig(ctx)
	require.NoError(t, err)
	require.Equal(t, cfg.ModelProfiles, loaded.ModelProfiles)
	require.Equal(t, "unchanged-catalog", repo.values[SettingKeyDesktopAgentProfileCatalog])
	require.Equal(t, "unchanged-desktop", repo.values["desktop_agent_config_v1"])
	require.Contains(t, repo.written, "workbench_model_profiles_v1")
	require.NotContains(t, repo.written, SettingKeyDesktopAgentProfileCatalog)
	bad := *loaded
	bad.ModelProfiles = map[string]DesktopAgentModelProfileBinding{"gpt-6.1-sol": {ProfileID: "gpt-6-luna"}}
	_, err = svc.UpdateConfig(ctx, bad)
	require.ErrorIs(t, err, ErrWorkbenchInvalid)
	loadedAgain, err := settings.GetWorkbenchConfig(ctx)
	require.NoError(t, err)
	require.Equal(t, cfg.ModelProfiles, loadedAgain.ModelProfiles, "an invalid draft must not overwrite the last valid mapping")
}
