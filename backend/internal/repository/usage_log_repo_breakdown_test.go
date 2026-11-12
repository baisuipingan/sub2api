//go:build unit

package repository

import (
	"context"
	"regexp"
	"testing"
	"time"

	"github.com/DATA-DOG/go-sqlmock"
	"github.com/Wei-Shaw/sub2api/internal/pkg/usagestats"
	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/stretchr/testify/require"
)

func TestResolveEndpointColumn(t *testing.T) {
	tests := []struct {
		endpointType string
		want         string
	}{
		{"inbound", "ul.inbound_endpoint"},
		{"upstream", "ul.upstream_endpoint"},
		{"path", "ul.inbound_endpoint || ' -> ' || ul.upstream_endpoint"},
		{"", "ul.inbound_endpoint"},        // default
		{"unknown", "ul.inbound_endpoint"}, // fallback
	}

	for _, tc := range tests {
		t.Run(tc.endpointType, func(t *testing.T) {
			got := resolveEndpointColumn(tc.endpointType)
			require.Equal(t, tc.want, got)
		})
	}
}

func TestResolveModelDimensionExpression(t *testing.T) {
	tests := []struct {
		modelType string
		want      string
	}{
		{usagestats.ModelSourceRequested, "COALESCE(NULLIF(TRIM(requested_model), ''), model)"},
		{usagestats.ModelSourceUpstream, "COALESCE(NULLIF(TRIM(upstream_model), ''), model)"},
		{usagestats.ModelSourceMapping, "(COALESCE(NULLIF(TRIM(requested_model), ''), model) || ' -> ' || COALESCE(NULLIF(TRIM(upstream_model), ''), model))"},
		{"", "COALESCE(NULLIF(TRIM(requested_model), ''), model)"},
		{"invalid", "COALESCE(NULLIF(TRIM(requested_model), ''), model)"},
	}

	for _, tc := range tests {
		t.Run(tc.modelType, func(t *testing.T) {
			got := resolveModelDimensionExpression(tc.modelType)
			require.Equal(t, tc.want, got)
		})
	}
}

func TestGetUserBreakdownStatsRequestTypeIncludesLegacyFallback(t *testing.T) {
	db, mock := newSQLMock(t)
	repo := &usageLogRepository{sql: db}
	start := time.Date(2026, 7, 1, 0, 0, 0, 0, time.UTC)
	end := start.Add(24 * time.Hour)
	requestType := int16(service.RequestTypeStream)

	legacyFilter := `(ul.request_type = $3 OR (ul.request_type = 0 AND ul.stream = TRUE AND ul.openai_ws_mode = FALSE))`
	mock.ExpectQuery(regexp.QuoteMeta(legacyFilter)).
		WithArgs(start, end, requestType).
		WillReturnRows(sqlmock.NewRows([]string{
			"user_id", "email", "requests", "input_tokens", "output_tokens",
			"cache_tokens", "total_tokens", "cost", "actual_cost", "account_cost",
		}))

	rows, err := repo.GetUserBreakdownStats(context.Background(), start, end, usagestats.UserBreakdownDimension{
		RequestType: &requestType,
	}, 0)

	require.NoError(t, err)
	require.Empty(t, rows)
	require.NoError(t, mock.ExpectationsWereMet())
}

func TestGetUserBreakdownStatsFiltersNativeCompactionV2(t *testing.T) {
	db, mock := newSQLMock(t)
	repo := &usageLogRepository{sql: db}
	start := time.Date(2026, 7, 1, 0, 0, 0, 0, time.UTC)
	end := start.Add(24 * time.Hour)
	nativeCompactionV2 := true

	mock.ExpectQuery(regexp.QuoteMeta("AND ul.native_compaction_v2 = $3")).
		WithArgs(start, end, true).
		WillReturnRows(sqlmock.NewRows([]string{
			"user_id", "email", "requests", "input_tokens", "output_tokens",
			"cache_tokens", "total_tokens", "cost", "actual_cost", "account_cost",
		}))

	rows, err := repo.GetUserBreakdownStats(context.Background(), start, end, usagestats.UserBreakdownDimension{
		NativeCompactionV2: &nativeCompactionV2,
	}, 0)

	require.NoError(t, err)
	require.Empty(t, rows)
	require.NoError(t, mock.ExpectationsWereMet())
}

func TestGetUserBreakdownStatsFiltersUpstreamModelMismatch(t *testing.T) {
	for _, tc := range []struct {
		name      string
		mismatch  bool
		condition string
	}{
		{name: "mismatch", mismatch: true, condition: "ul.upstream_model_mismatch IS TRUE"},
		{name: "matching", condition: "ul.upstream_model_mismatch IS FALSE"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			db, mock := newSQLMock(t)
			repo := &usageLogRepository{sql: db}
			start := time.Date(2026, 7, 1, 0, 0, 0, 0, time.UTC)
			end := start.Add(24 * time.Hour)
			billingType := int8(1)
			// Keep the selected mapping intact while applying the same mismatch
			// semantics as the parent chart; NULL historical rows are not FALSE.
			filter := "AND ul.group_id = $3 AND " + resolveModelDimensionExpression(usagestats.ModelSourceMapping) +
				" = $4 AND ul.upstream_endpoint = $5 AND ul.user_id = $6 AND ul.api_key_id = $7" +
				" AND ul.account_id = $8 AND ul.billing_type = $9 AND " + tc.condition +
				" GROUP BY ul.user_id, u.email ORDER BY requests DESC LIMIT 50"
			mock.ExpectQuery(regexp.QuoteMeta(filter)).
				WithArgs(start, end, int64(42), "public -> provider", "/v1/responses", int64(7), int64(8), int64(9), billingType).
				WillReturnRows(sqlmock.NewRows([]string{
					"user_id", "email", "requests", "input_tokens", "output_tokens",
					"cache_tokens", "total_tokens", "cost", "actual_cost", "account_cost",
				}).AddRow(int64(7), "user@example.test", int64(2), int64(10), int64(20), int64(3), int64(33), 0.2, 0.1, 0.08))

			rows, err := repo.GetUserBreakdownStats(context.Background(), start, end, usagestats.UserBreakdownDimension{
				GroupID: 42, Model: "public -> provider", ModelType: usagestats.ModelSourceMapping,
				Endpoint: "/v1/responses", EndpointType: "upstream", UserID: 7, APIKeyID: 8, AccountID: 9,
				BillingType: &billingType, UpstreamModelMismatch: &tc.mismatch, SortBy: "requests",
			}, 50)

			require.NoError(t, err)
			require.Len(t, rows, 1)
			require.Equal(t, int64(7), rows[0].UserID)
			require.Equal(t, int64(2), rows[0].Requests)
			require.NoError(t, mock.ExpectationsWereMet())
		})
	}
}

func TestGetUserBreakdownStatsEndpointBillingModeKeepsLegacyUsageSemantics(t *testing.T) {
	for _, tc := range []struct {
		mode, condition string
	}{
		{mode: "image", condition: "(ul.billing_mode = $5 OR ((ul.billing_mode IS NULL OR ul.billing_mode = '') AND COALESCE(ul.image_count, 0) > 0))"},
		{mode: "token", condition: "(ul.billing_mode = $5 OR ((ul.billing_mode IS NULL OR ul.billing_mode = '') AND COALESCE(ul.image_count, 0) <= 0))"},
		{mode: "video", condition: "ul.billing_mode = $5"},
		{mode: "per_request", condition: "ul.billing_mode = $5"},
		{mode: "custom-mode", condition: "ul.billing_mode = $5"},
	} {
		t.Run(tc.mode, func(t *testing.T) {
			db, mock := newSQLMock(t)
			repo := &usageLogRepository{sql: db}
			start := time.Date(2026, 7, 1, 0, 0, 0, 0, time.UTC)
			end := start.Add(24 * time.Hour)
			mismatch := true
			filter := "AND " + resolveModelDimensionExpression(usagestats.ModelSourceRequested) +
				" = $3 AND ul.inbound_endpoint = $4 AND " + tc.condition +
				" AND ul.upstream_model_mismatch IS TRUE GROUP BY ul.user_id, u.email ORDER BY actual_cost DESC"
			mock.ExpectQuery(regexp.QuoteMeta(filter)).
				WithArgs(start, end, "public-model", "/v1/responses", tc.mode).
				WillReturnRows(sqlmock.NewRows([]string{
					"user_id", "email", "requests", "input_tokens", "output_tokens",
					"cache_tokens", "total_tokens", "cost", "actual_cost", "account_cost",
				}))

			rows, err := repo.GetUserBreakdownStats(context.Background(), start, end, usagestats.UserBreakdownDimension{
				Model: "public-model", ModelType: usagestats.ModelSourceRequested,
				Endpoint: "/v1/responses", EndpointType: "inbound", BillingMode: tc.mode,
				UpstreamModelMismatch: &mismatch,
			}, 0)

			require.NoError(t, err)
			require.Empty(t, rows)
			require.NoError(t, mock.ExpectationsWereMet())
		})
	}
}
