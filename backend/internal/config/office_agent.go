package config

import (
	"fmt"

	"github.com/spf13/viper"
)

// OfficeAgentConfig enables the private, admin-only integration pilot. It does
// not enable a public chat UI or replace existing Studio/desktop settings.
type OfficeAgentConfig struct {
	Enabled            bool   `mapstructure:"enabled"`
	WorkerToken        string `mapstructure:"worker_token" json:"-"`
	MaxConcurrentCalls int    `mapstructure:"max_concurrent_calls"`
	MaxModelCalls      int    `mapstructure:"max_model_calls"`
	MaxToolCalls       int    `mapstructure:"max_tool_calls"`
	MaxOutputTokens    int    `mapstructure:"max_output_tokens"`
	MaxRunSeconds      int    `mapstructure:"max_run_seconds"`
}

func setOfficeAgentDefaults() {
	viper.SetDefault("office_agent.enabled", false)
	viper.SetDefault("office_agent.worker_token", "")
	viper.SetDefault("office_agent.max_concurrent_calls", 1)
	viper.SetDefault("office_agent.max_model_calls", 8)
	viper.SetDefault("office_agent.max_tool_calls", 16)
	viper.SetDefault("office_agent.max_output_tokens", 8192)
	viper.SetDefault("office_agent.max_run_seconds", 600)
}

func (c OfficeAgentConfig) Validate() error {
	if !c.Enabled {
		return nil
	}
	if len(c.WorkerToken) < 32 || len(c.WorkerToken) > 256 {
		return fmt.Errorf("office_agent.worker_token must contain 32..256 printable ASCII bytes without spaces when enabled")
	}
	for _, ch := range c.WorkerToken {
		if ch < 33 || ch > 126 {
			return fmt.Errorf("office_agent.worker_token must contain 32..256 printable ASCII bytes without spaces when enabled")
		}
	}
	if c.MaxConcurrentCalls < 1 || c.MaxConcurrentCalls > 4 || c.MaxModelCalls < 1 || c.MaxModelCalls > 32 ||
		c.MaxToolCalls < 1 || c.MaxToolCalls > 64 || c.MaxOutputTokens < 128 || c.MaxOutputTokens > 32768 ||
		c.MaxRunSeconds < 30 || c.MaxRunSeconds > 1800 {
		return fmt.Errorf("office_agent pilot limits are outside the supported range")
	}
	return nil
}
