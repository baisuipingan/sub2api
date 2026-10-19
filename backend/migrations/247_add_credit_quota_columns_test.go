package migrations

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestAddCreditQuotaColumnsMigration(t *testing.T) {
	content, err := FS.ReadFile("247_add_credit_quota_columns.sql")
	require.NoError(t, err)
	sql := strings.Join(strings.Fields(string(content)), " ")

	for _, fragment := range []string{
		"ADD COLUMN IF NOT EXISTS quota_credits BIGINT NOT NULL DEFAULT 0",
		"ADD COLUMN IF NOT EXISTS quota_used_credits BIGINT NOT NULL DEFAULT 0",
		"ADD COLUMN IF NOT EXISTS daily_limit_credits BIGINT",
		"ADD COLUMN IF NOT EXISTS daily_usage_credits BIGINT NOT NULL DEFAULT 0",
		"ADD COLUMN IF NOT EXISTS value_credits BIGINT NOT NULL DEFAULT 0",
		"sync_sub2api_credit_pair",
		"sync_sub2api_api_key_credit_columns",
		"sync_sub2api_group_credit_columns",
		"sync_sub2api_subscription_credit_columns",
		"sync_sub2api_platform_quota_credit_columns",
		"sync_sub2api_redeem_code_credit_columns",
	} {
		require.Contains(t, sql, fragment)
	}
}
