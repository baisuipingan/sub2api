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

func TestDesktopManagedCredentialRemainsReadableWithSystemProtectionMetadata(t *testing.T) {
	for _, tc := range []struct {
		name    string
		purpose string
		keyType string
	}{
		{name: "desktop agent", purpose: service.APIKeyPurposeDesktopAgent, keyType: service.APIKeyTypeSystem},
		{name: "legacy image studio", keyType: service.APIKeyTypeSystem},
		{name: "user key", keyType: service.APIKeyTypeUser},
	} {
		t.Run(tc.name, func(t *testing.T) {
			key := &service.APIKey{ID: 7, UserID: 11, Key: "fixture-secret", Purpose: tc.purpose, KeyType: tc.keyType}
			direct := APIKeyFromService(key)
			nested := UsageLogFromService(&service.UsageLog{APIKey: key})
			// 本人读取沿既有 Key 合同；可读不改变系统托管标识和服务层写入保护。
			for _, mapped := range []*APIKey{direct, nested.APIKey} {
				require.Equal(t, key.Key, mapped.Key)
				require.Equal(t, key.UserID, mapped.UserID)
				require.Equal(t, tc.keyType == service.APIKeyTypeSystem, mapped.IsSystem)
			}
		})
	}
}
