package service

import (
	"context"
	"net/url"
	"strings"

	"github.com/Wei-Shaw/sub2api/internal/pkg/ctxkey"
	"github.com/tidwall/sjson"
)

func supportsWorkbenchAnthropicMessages(account *Account) bool {
	return account != nil && account.Platform == PlatformAnthropic && account.Type == AccountTypeAPIKey
}

func isWorkbenchAnthropicMessages(ctx context.Context) bool {
	native, _ := ctx.Value(ctxkey.WorkbenchAnthropicMessages).(bool)
	return native
}

func withWorkbenchAnthropicPromptCache(account *Account, body []byte) ([]byte, error) {
	// Relay operators can confirm support for the automatic breakpoint. Native
	// Messages compatibility alone does not imply support for a top-level marker.
	configured, hasOverride := account.Extra["workbench_anthropic_prompt_cache"].(bool)
	if !hasOverride {
		endpoint, err := url.Parse(getAnthropicGatewayBaseURL(account))
		configured = err == nil && endpoint.Scheme == "https" && strings.EqualFold(endpoint.Hostname(), "api.anthropic.com")
	}
	if !configured {
		return body, nil
	}
	return sjson.SetRawBytes(body, "cache_control", []byte(`{"type":"ephemeral"}`))
}
