package service

import (
	"context"
	"testing"

	"github.com/Wei-Shaw/sub2api/internal/pkg/ctxkey"
	"github.com/stretchr/testify/require"
	"github.com/tidwall/gjson"
)

func TestWorkbenchMessagesPrefersNativeAnthropicRouteAndKeepsCompatibleEndpoint(t *testing.T) {
	const model = "claude-sonnet-4-6"
	group := &Group{ID: 12, Platform: PlatformComposite}
	native := desktopModelAccount(PlatformAnthropic, map[string]any{model: model})
	compatible := desktopModelAccount(PlatformOpenAI, map[string]any{"claude-compatible": model})
	mixed := desktopModelAccount(PlatformAntigravity, map[string]any{"claude-mixed": model})
	accounts := &desktopModelAccounts{accounts: []Account{native, compatible, mixed}}
	routes := compositeRouteRepoStub{routes: []CompositeModelRoute{
		{ID: 1, GroupID: group.ID, Enabled: true, MatchType: CompositeRouteMatchExact, PublicModel: model, Endpoint: CompositeRouteEndpointChatCompletions, TargetPlatform: PlatformOpenAI, UpstreamModel: "claude-compatible"},
	}}
	catalog, ok := NewWorkbenchChatModelCatalog(accounts, routes, nil).(*workbenchChatModelCatalog)
	require.True(t, ok)
	ctx := context.Background()
	protocols, err := catalog.modelRoutes(ctx, group)
	require.NoError(t, err)
	require.Equal(t, workbenchProtocolAnthropic, protocols[model].protocol)
	require.Equal(t, workbenchProtocolResponses, protocols["claude-compatible"].protocol)
	require.NotEqual(t, workbenchProtocolAnthropic, protocols["claude-mixed"].protocol)
	compatibleModels, err := catalog.base.workbenchEndpointModels(ctx, group, workbenchProtocolEndpoint(workbenchProtocolChatCompletions), false)
	require.NoError(t, err)
	require.Contains(t, compatibleModels, model, "older turns retain their admitted protocol while its route remains available")
}

func TestWorkbenchMessagesUsesResponsesCapabilityWithoutLosingBridgedAliases(t *testing.T) {
	group := &Group{ID: 12, Platform: PlatformComposite}
	native := desktopModelAccount(PlatformOpenAI, map[string]any{"gpt-6-sol": "gpt-6-sol"})
	chat := desktopModelAccount(PlatformOpenAI, map[string]any{"chat-public": "gpt-5", "bridge-target": "gpt-5"})
	chat.Extra = map[string]any{"openai_responses_supported": false}
	accounts := &desktopModelAccounts{accounts: []Account{native, chat}}
	routes := compositeRouteRepoStub{routes: []CompositeModelRoute{
		{ID: 1, GroupID: group.ID, Enabled: true, MatchType: CompositeRouteMatchExact, PublicModel: "bridge-public", Endpoint: CompositeRouteEndpointResponses, TargetPlatform: PlatformOpenAI, UpstreamModel: "bridge-target"},
	}}
	catalog, ok := NewWorkbenchChatModelCatalog(accounts, routes, nil).(*workbenchChatModelCatalog)
	require.True(t, ok)
	choices, err := catalog.modelRoutes(context.Background(), group)
	require.NoError(t, err)
	require.Equal(t, workbenchModelRoute{protocol: workbenchProtocolResponses, nativeResponses: true}, choices["gpt-6-sol"])
	require.Equal(t, workbenchModelRoute{protocol: workbenchProtocolChatCompletions}, choices["chat-public"])
	require.Equal(t, workbenchModelRoute{protocol: workbenchProtocolResponses}, choices["bridge-public"])
}

func TestWorkbenchMessagesSchedulerExcludesTranslatedAndOAuthAccounts(t *testing.T) {
	const model = "claude-sonnet-4-6"
	native := desktopModelAccount(PlatformAnthropic, map[string]any{model: model})
	oauth := native
	oauth.Type = AccountTypeOAuth
	compatible := desktopModelAccount(PlatformOpenAI, map[string]any{model: model})
	ctx := context.WithValue(context.Background(), ctxkey.WorkbenchAnthropicMessages, true)
	scheduler := &GatewayService{}
	require.True(t, scheduler.isModelSupportedByAccountWithContext(ctx, &native, model))
	require.False(t, scheduler.isModelSupportedByAccountWithContext(ctx, &oauth, model))
	require.False(t, scheduler.isModelSupportedByAccountWithContext(ctx, &compatible, model))
	require.True(t, scheduler.isModelSupportedByAccountWithContext(context.Background(), &compatible, model))
}

func TestWorkbenchMessagesCacheControlUsesAccountCapability(t *testing.T) {
	body := []byte(`{"model":"claude-sonnet-4-6","messages":[{"role":"user","content":"Example"}]}`)
	for _, tc := range []struct {
		name, baseURL string
		flag          any
		wantCache     bool
	}{
		{name: "official default", wantCache: true},
		{name: "official URL", baseURL: "https://api.anthropic.com/v1", wantCache: true},
		{name: "official disabled", flag: false},
		{name: "relay unknown", baseURL: "https://relay.example"},
		{name: "relay confirmed", baseURL: "https://relay.example", flag: true, wantCache: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			account := &Account{Platform: PlatformAnthropic, Type: AccountTypeAPIKey, Credentials: map[string]any{"base_url": tc.baseURL}}
			if tc.flag != nil {
				account.Extra = map[string]any{"workbench_anthropic_prompt_cache": tc.flag}
			}
			actual, err := withWorkbenchAnthropicPromptCache(account, body)
			require.NoError(t, err)
			if tc.wantCache {
				require.Equal(t, "ephemeral", gjson.GetBytes(actual, "cache_control.type").String())
				require.Equal(t, gjson.GetBytes(body, "messages").Raw, gjson.GetBytes(actual, "messages").Raw)
			} else {
				require.Equal(t, body, actual)
			}
		})
	}
}
