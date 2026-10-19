package migrations

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestBillingCurrencyCreditsMigration(t *testing.T) {
	content, err := FS.ReadFile("246_billing_currency_credits.sql")
	require.NoError(t, err)
	sql := strings.Join(strings.Fields(string(content)), " ")

	for _, fragment := range []string{
		"ADD COLUMN IF NOT EXISTS balance_credits BIGINT NOT NULL DEFAULT 0",
		"ADD COLUMN IF NOT EXISTS frozen_balance_credits BIGINT NOT NULL DEFAULT 0",
		"sync_sub2api_user_credit_columns",
		"ADD COLUMN IF NOT EXISTS charge_credits BIGINT NOT NULL DEFAULT 0",
		"ADD COLUMN IF NOT EXISTS credited_credits BIGINT NOT NULL DEFAULT 0",
		"CREATE TABLE IF NOT EXISTS billing_ledger",
		"billing_ledger_idempotency",
		"'billing_currency_config'",
		"\"engine\":\"shadow\"",
		"\"display_currency\":\"CNY\"",
	} {
		require.Contains(t, sql, fragment)
	}
	require.NotContains(t, sql, "billing_currency_settings")
}
