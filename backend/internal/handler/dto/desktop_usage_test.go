package dto

import (
	"encoding/json"
	"math"
	"testing"

	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/stretchr/testify/require"
)

func TestDesktopUsageCreditsRemainExactBeyondJavaScriptIntegerRange(t *testing.T) {
	usage := UsageLogFromService(&service.UsageLog{ChargeCredits: math.MaxInt64, CreditsPerUSD: 9007199254740993, ActualCost: 1.25})
	raw, err := json.Marshal(usage)
	require.NoError(t, err)
	var result map[string]any
	require.NoError(t, json.Unmarshal(raw, &result))
	require.Equal(t, "9223372036854775807", result["charge_credits_string"])
	require.Equal(t, "9007199254740993", result["credits_per_usd_string"])
	require.Contains(t, result, "charge_credits")
	require.Equal(t, 1.25, result["actual_cost"])
	legacy := UsageLogFromService(&service.UsageLog{ActualCost: 0.5})
	require.Equal(t, "0", legacy.ChargeCreditsString)
	require.Equal(t, 0.5, legacy.ActualCost)
}

func TestDesktopManagedCredentialIsRedactedAcrossKeyAndUsageDTOs(t *testing.T) {
	for _, purpose := range []string{service.APIKeyPurposeDesktopAgent, ""} {
		key := &service.APIKey{ID: 7, Key: "fixture-secret", Purpose: purpose, KeyType: service.APIKeyTypeSystem}
		direct := APIKeyFromService(key)
		nested := UsageLogFromService(&service.UsageLog{APIKey: key})
		if purpose == service.APIKeyPurposeDesktopAgent {
			require.Empty(t, direct.Key)
			require.Empty(t, nested.APIKey.Key)
		} else {
			require.Equal(t, "fixture-secret", direct.Key)
			require.Equal(t, "fixture-secret", nested.APIKey.Key)
		}
	}
}
