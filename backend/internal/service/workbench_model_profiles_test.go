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

func TestWorkbenchPublicModelAliasCapabilityPrecedence(t *testing.T) {
	profiles := desktopAgentBuiltinProfiles()
	const alias = "公司办公助理"
	legacy := map[string]DesktopAgentModelProfileBinding{
		alias:         {ProfileID: "gpt-6-luna"},
		"gpt-6.1-sol": {ProfileID: "gpt-6-luna"},
	}
	agent := map[string]DesktopAgentModelProfileBinding{alias: {ProfileID: "gpt-6.1-sol", Override: true}}
	profile := workbenchModelProfile(alias, agent, legacy, profiles)
	require.NotNil(t, profile)
	require.Equal(t, "gpt-6.1-sol", profile.ID)
	require.EqualValues(t, 1050000, *profile.OfficialContextWindow)
	require.EqualValues(t, 128000, *profile.OfficialMaxOutputTokens)
	require.Equal(t, "gpt-6-luna", workbenchModelProfile(alias, nil, legacy, profiles).ID)

	agent[alias] = DesktopAgentModelProfileBinding{ProfileID: "missing-profile"}
	require.Nil(t, workbenchModelProfile(alias, agent, legacy, profiles), "an explicit Agent binding takes precedence")
	legacy[alias] = DesktopAgentModelProfileBinding{ProfileID: "gpt-6-luna", Override: true}
	require.Nil(t, workbenchModelProfile(alias, nil, legacy, profiles))
	require.Equal(t, "gpt-6.1-sol", workbenchModelProfile("gpt-6.1-sol", nil, legacy, profiles).ID)
	require.Nil(t, workbenchModelProfile("gpt-6.1-sol-provider-suffix", nil, nil, profiles))
}

func TestWorkbenchDeepSeekV41FlashUsesOfficialFlashProfile(t *testing.T) {
	profiles := desktopAgentBuiltinProfiles()
	const publicID = "deepseek-v4.1-flash"
	profile := workbenchModelProfile(publicID, nil, nil, profiles)
	require.NotNil(t, profile)
	require.Equal(t, "deepseek-flash", profile.ID)
	require.Equal(t, []string{"none", "low", "high", "max"}, profile.OfficialReasoningLevels)
	require.True(t, *profile.Model.SupportsToolCall)
	require.Nil(t, workbenchModelProfile(publicID+"-custom", nil, nil, profiles))

	// A saved administrator binding remains authoritative for a public alias.
	bindings := map[string]DesktopAgentModelProfileBinding{publicID: {ProfileID: "deepseek-v4-pro"}}
	require.Equal(t, "deepseek-v4-pro", workbenchModelProfile(publicID, bindings, nil, profiles).ID)
}

func TestWorkbenchChatCatalogUsesSavedAgentSelection(t *testing.T) {
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
	for _, tc := range []struct {
		name, config, expectedDefault string
		models, efforts               []string
	}{
		{
			name: "saved order, defaults and Agent alias profile",
			config: `{"enabled":true,"group_id":12,"default_model_id":"deepseek-search-assistant",
				"models":[{"id":"gpt-6.1-sol","default_reasoning":"high"},{"id":"deepseek-search-assistant","default_reasoning":"low"}],
				"model_profiles":{"deepseek-search-assistant":{"profile_id":"gpt-6.1-sol"}}}`,
			expectedDefault: "deepseek-search-assistant",
			models:          []string{preferred, "deepseek-search-assistant"}, efforts: []string{"high", "low"},
		},
		{
			name:            "legacy model",
			config:          `{"enabled":true,"group_id":12,"model":{"id":"gpt-6.1-sol","default_reasoning":"medium"}}`,
			expectedDefault: preferred, models: []string{preferred}, efforts: []string{"medium"},
		},
		{
			name: "lost route and incompatible reasoning",
			config: `{"enabled":true,"group_id":12,"default_model_id":"not-routed",
				"models":[{"id":"not-routed"},{"id":"gpt-6.1-sol","default_reasoning":"minimal"}]}`,
			models: []string{preferred}, efforts: []string{""},
		},
		{
			name:   "empty selection does not expose all group models",
			config: `{"enabled":true,"group_id":12,"models":[]}`,
			models: []string{}, efforts: []string{},
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			settings.values[SettingKeyDesktopAgentConfig] = tc.config
			catalog, err := svc.ChatModels(context.Background())
			require.NoError(t, err)
			ids := make([]string, 0, len(catalog.Models))
			efforts := make([]string, 0, len(catalog.Models))
			for _, model := range catalog.Models {
				ids = append(ids, model.ID)
				efforts = append(efforts, model.DefaultReasoningEffort)
			}
			require.Equal(t, tc.models, ids)
			require.Equal(t, tc.efforts, efforts)
			require.Equal(t, tc.expectedDefault, catalog.DefaultModel)
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
		"workbench_chat_enabled": "true",
		SettingKeyDesktopAgentConfig: `{"enabled":true,"group_id":12,"models":[
			{"id":"gemini-4-carbon"},{"id":"carbon-office","default_reasoning":"low"},
			{"id":"gemini-4-carbon-low"},{"id":"carbon-responses"}]}`,
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
		expectedEffort := "medium"
		if id == carbonAlias {
			expectedEffort = "low"
		}
		require.Equal(t, workbenchProtocolGemini, model.Protocol)
		require.Equal(t, []string{"low", "medium", "high"}, model.ReasoningEfforts)
		require.Equal(t, expectedEffort, model.DefaultReasoningEffort)
		catalog, err := json.Marshal(model)
		require.NoError(t, err)
		require.Equal(t, expectedEffort, gjson.GetBytes(catalog, "default_reasoning_effort").String())
		request := workbenchNativeRequest{Protocol: model.Protocol, Model: model.ID, ProfileModel: model.profileModel, ReasoningEffort: model.DefaultReasoningEffort,
			History: []workbenchNativeMessage{{Role: "user", Text: "Hello"}}}
		_, payload, err := buildWorkbenchNativeRequest(request)
		require.NoError(t, err)
		require.Equal(t, strings.ToUpper(expectedEffort), gjson.GetBytes(payload, "generationConfig.thinkingConfig.thinkingLevel").String())
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

func TestWorkbenchLegacyModelBindingsAreReadOnly(t *testing.T) {
	ctx := context.Background()
	const key = "workbench_model_profiles_v1"
	const legacy = `{"my-office-model":{"profile_id":"removed-profile"}}`
	repo := &workbenchSettingsStub{values: map[string]string{
		key: legacy, SettingKeyDesktopAgentProfileCatalog: "unchanged-catalog", SettingKeyDesktopAgentConfig: "unchanged-desktop",
	}}
	settings := NewSettingService(repo, nil)
	svc := NewWorkbenchAdminService(nil, settings, nil)
	saved, err := svc.UpdateConfig(ctx, WorkbenchConfig{FilesEnabled: true})
	require.NoError(t, err)
	require.True(t, saved.FilesEnabled)
	require.Equal(t, "removed-profile", saved.ModelProfiles["my-office-model"].ProfileID)
	require.Equal(t, legacy, repo.values[key])
	require.Equal(t, "unchanged-catalog", repo.values[SettingKeyDesktopAgentProfileCatalog])
	require.Equal(t, "unchanged-desktop", repo.values[SettingKeyDesktopAgentConfig])
	require.NotContains(t, repo.written, key)

	repo.values[key] = "invalid legacy data"
	saved, err = svc.UpdateConfig(ctx, WorkbenchConfig{})
	require.NoError(t, err)
	require.Empty(t, saved.ModelProfiles)
	require.Equal(t, "invalid legacy data", repo.values[key])
}
