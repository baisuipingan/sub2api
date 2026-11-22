package config

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestOfficeAgentConfigIsDisabledByDefaultAndRequiresExplicitBounds(t *testing.T) {
	require.NoError(t, (OfficeAgentConfig{}).Validate())
	cfg := OfficeAgentConfig{Enabled: true, WorkerToken: strings.Repeat("x", 32), MaxConcurrentCalls: 1,
		MaxModelCalls: 32, MaxToolCalls: 48, MaxOutputTokens: 8192, MaxRunSeconds: 600}
	require.NoError(t, cfg.Validate())
	for _, mutate := range []func(*OfficeAgentConfig){
		func(c *OfficeAgentConfig) { c.WorkerToken = "short" },
		func(c *OfficeAgentConfig) { c.WorkerToken = " " + c.WorkerToken },
		func(c *OfficeAgentConfig) { c.WorkerToken = c.WorkerToken[:16] + "\n" + c.WorkerToken[16:] },
		func(c *OfficeAgentConfig) { c.MaxConcurrentCalls = 0 },
		func(c *OfficeAgentConfig) { c.MaxConcurrentCalls = 100 },
		func(c *OfficeAgentConfig) { c.MaxModelCalls = 0 },
		func(c *OfficeAgentConfig) { c.MaxModelCalls = 33 },
		func(c *OfficeAgentConfig) { c.MaxToolCalls = 65 },
		func(c *OfficeAgentConfig) { c.MaxRunSeconds = 3600 },
	} {
		changed := cfg
		mutate(&changed)
		require.Error(t, changed.Validate())
	}
}
