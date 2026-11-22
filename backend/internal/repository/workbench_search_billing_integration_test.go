//go:build integration

package repository

import (
	"context"
	"encoding/json"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/pkg/pagination"
	"github.com/Wei-Shaw/sub2api/internal/pkg/usagestats"
	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
)

func workbenchSearchBillingFixture(t *testing.T, funding int64) (*officeAgentRepository, service.OfficeRunLease) {
	t.Helper()
	ctx := context.Background()
	base, input := officeTestAdmission(t)
	repo := base.(*officeAgentRepository)
	input.SearchEnabled, input.BrowserDriven, input.Mode = true, true, "chat"
	input.Limits.MaxToolCalls = 8
	_, err := integrationDB.ExecContext(ctx, `UPDATE users SET balance_credits=$2 WHERE id=$1`, input.UserID, funding)
	require.NoError(t, err)
	t.Cleanup(func() {
		_, err := integrationDB.ExecContext(ctx, `DELETE FROM workbench_search_billing WHERE user_id=$1`, input.UserID)
		require.NoError(t, err)
		_, err = integrationDB.ExecContext(ctx, `DELETE FROM billing_ledger WHERE user_id=$1`, input.UserID)
		require.NoError(t, err)
	})
	_, _, err = repo.AdmitRun(ctx, input)
	require.NoError(t, err)
	lease, _, err := repo.ClaimBrowserStep(ctx, input.UserID, input.RunID, time.Minute)
	require.NoError(t, err)
	require.NotNil(t, lease)
	return repo, *lease
}

func prepareWorkbenchSearchBilling(t *testing.T, repo *officeAgentRepository, lease service.OfficeRunLease, id string) *service.OfficeOperation {
	t.Helper()
	op, created, err := repo.PrepareOperation(context.Background(), lease, service.OfficeOperationPrepare{
		ID: id, Kind: service.OfficeOperationTool, InputHash: strings.Repeat("d", 64),
		Input: json.RawMessage(`{"tool":"web_search","query":"交付"}`), RequestID: uuid.NewString(),
	})
	require.NoError(t, err)
	require.True(t, created)
	return op
}

func assertWorkbenchSearchWallet(t *testing.T, userID, available, frozen int64) {
	t.Helper()
	var balance, held, legacy, legacyHeld int64
	err := integrationDB.QueryRowContext(context.Background(), `SELECT balance_credits,frozen_balance_credits,
		ROUND(balance*100000000)::bigint,ROUND(frozen_balance*100000000)::bigint FROM users WHERE id=$1`, userID).
		Scan(&balance, &held, &legacy, &legacyHeld)
	require.NoError(t, err)
	require.Equal(t, []int64{available, frozen, available, frozen}, []int64{balance, held, legacy, legacyHeld})
}

func TestWorkbenchSearchBillingReservesAndSettlesOnce(t *testing.T) {
	ctx := context.Background()
	for _, engine := range []string{service.BillingEngineCredits, service.BillingEngineLegacy} {
		t.Run(engine, func(t *testing.T) {
			const each = int64(75_000)
			repo, lease := workbenchSearchBillingFixture(t, 2*each)
			before, err := repo.AdminOverview(ctx, 7)
			require.NoError(t, err)
			quote := service.WorkbenchSearchQuote{Provider: "deepseek", AmountUSD: .00075, Billing: service.DefaultBillingCurrencyConfig()}
			quote.Billing.Engine = engine
			op := prepareWorkbenchSearchBilling(t, repo, lease, "browser:tool:0:0")
			_, err = repo.DispatchWorkbenchSearch(ctx, lease, op.ID, quote)
			require.NoError(t, err)
			_, err = repo.DispatchWorkbenchSearch(ctx, lease, op.ID, quote)
			require.ErrorIs(t, err, service.ErrOfficeOperationDispatched)
			assertWorkbenchSearchWallet(t, lease.Run.UserID, each, each)

			out := service.OfficeOperationResult{Output: json.RawMessage(`{"provider":"deepseek","results":[{"title":"结果"}]}`)}
			for range 2 {
				completed, err := repo.CompleteWorkbenchSearch(ctx, lease, op.ID, out, true)
				require.NoError(t, err)
				require.Equal(t, service.OfficeOperationCompleted, completed.State)
			}
			assertWorkbenchSearchWallet(t, lease.Run.UserID, each, 0)
			var entries int
			var delta int64
			require.NoError(t, integrationDB.QueryRowContext(ctx, `SELECT COUNT(*),SUM(delta_credits) FROM billing_ledger WHERE user_id=$1 AND request_id=$2`,
				lease.Run.UserID, "client:"+op.RequestID).Scan(&entries, &delta))
			require.Equal(t, 1, entries)
			require.Equal(t, -each, delta)
			charge, err := repo.GetWorkbenchSearchCharge(ctx, lease.Run.UserID, lease.Run.ID, op.ID)
			require.NoError(t, err)
			require.Equal(t, each, charge.ChargeCredits)
			require.Equal(t, .00075, charge.AmountUSD)
			_, err = repo.GetWorkbenchSearchCharge(ctx, lease.Run.UserID+100000, lease.Run.ID, op.ID)
			require.ErrorIs(t, err, service.ErrOfficeNotFound)
			charges, total, err := repo.ListWorkbenchSearchCharges(ctx, lease.Run.UserID, 1, 10, charge.CreatedAt, charge.CreatedAt.Add(time.Second))
			require.NoError(t, err)
			require.EqualValues(t, 1, total)
			require.Equal(t, []service.WorkbenchSearchCharge{*charge}, charges)
			charges, total, err = repo.ListWorkbenchSearchCharges(ctx, lease.Run.UserID, 1, 10, time.Time{}, charge.CreatedAt)
			require.NoError(t, err)
			require.Zero(t, total)
			require.Empty(t, charges)

			usage := NewUsageLogRepository(integrationEntClient, integrationDB)
			filters := usagestats.UsageLogFilters{
				UserID: lease.Run.UserID, APIKeyID: lease.Run.APIKeyID, GroupID: lease.Run.GroupID,
				Model: "workbench-web-search", BillingMode: string(service.BillingModePerRequest), ExactTotal: true,
			}
			params := pagination.PaginationParams{Page: 1, PageSize: 1, SortBy: "created_at"}
			logs, paging, err := usage.ListWithFilters(ctx, params, filters)
			require.NoError(t, err)
			require.EqualValues(t, 1, paging.Total)
			require.Len(t, logs, 1)
			log := logs[0]
			require.Equal(t, "client:"+op.RequestID, log.RequestID)
			require.Equal(t, charge.CreatedAt, log.CreatedAt)
			require.Equal(t, charge.AmountUSD, log.ActualCost)
			require.Equal(t, charge.BillingCurrency, log.BillingCurrency)
			require.Equal(t, charge.USDToCNYRate, log.USDToCNYRate)
			require.Equal(t, charge.ChargeCredits, log.ChargeCredits)
			require.Equal(t, service.RequestTypeSync, log.RequestType)
			require.NotNil(t, log.DurationMs)
			require.Equal(t, charge.Provider, *log.SearchProvider)
			require.Zero(t, log.AccountID)
			require.Zero(t, *log.AccountStatsCost)
			calls, err := repo.RunUsage(ctx, lease.Run.UserID, lease.Run.ID, true)
			require.NoError(t, err)
			require.Len(t, calls, 1)
			require.Equal(t, charge.AmountUSD, calls[0].Usage.ActualCost)
			require.Empty(t, calls[0].Usage.Upstreams)

			// Re-run the migration's projection over the historical charge and check idempotence.
			require.NoError(t, usage.Delete(ctx, log.ID))
			overview, err := repo.AdminOverview(ctx, 7)
			require.NoError(t, err)
			require.Equal(t, before.SearchCalls+1, overview.SearchCalls)
			require.Equal(t, before.SearchChargeCredits+each, overview.SearchChargeCredits)
			require.Equal(t, before.ChargeCredits+each, overview.ChargeCredits)
			for range 2 {
				_, err := integrationDB.ExecContext(ctx, `SELECT record_workbench_search_usage(b) FROM workbench_search_billing b WHERE run_id=$1 AND state='charged'`, lease.Run.ID)
				require.NoError(t, err)
			}
			logs, paging, err = usage.ListWithFilters(ctx, params, filters)
			require.NoError(t, err)
			require.EqualValues(t, 1, paging.Total)
			require.Equal(t, charge.CreatedAt, logs[0].CreatedAt)
			assertWorkbenchSearchWallet(t, lease.Run.UserID, each, 0)
			filters.UserID += 100000
			logs, paging, err = usage.ListWithFilters(ctx, params, filters)
			require.NoError(t, err)
			require.Zero(t, paging.Total)
			require.Empty(t, logs)
		})
	}
}

func TestWorkbenchSearchBillingReleaseAndExpiry(t *testing.T) {
	ctx := context.Background()
	const each = int64(75_000)
	repo, lease := workbenchSearchBillingFixture(t, each)
	quote := service.WorkbenchSearchQuote{Provider: "deepseek", AmountUSD: .00075, Billing: service.DefaultBillingCurrencyConfig()}
	first := prepareWorkbenchSearchBilling(t, repo, lease, "browser:tool:0:0")
	second := prepareWorkbenchSearchBilling(t, repo, lease, "browser:tool:0:1")
	var wg sync.WaitGroup
	type dispatch struct {
		id  string
		err error
	}
	results := make(chan dispatch, 2)
	for _, op := range []*service.OfficeOperation{first, second} {
		wg.Go(func() {
			_, err := repo.DispatchWorkbenchSearch(ctx, lease, op.ID, quote)
			results <- dispatch{op.ID, err}
		})
	}
	wg.Wait()
	close(results)
	var reserved, unreserved string
	for result := range results {
		if result.err != nil {
			require.ErrorIs(t, result.err, service.ErrWorkbenchSearchInsufficientBalance)
			unreserved = result.id
		} else {
			reserved = result.id
		}
	}
	require.NotEmpty(t, reserved)
	require.NotEmpty(t, unreserved)
	assertWorkbenchSearchWallet(t, lease.Run.UserID, 0, each)
	empty := service.OfficeOperationResult{Output: json.RawMessage(`{"provider":"deepseek","results":[]}`)}
	_, err := repo.CompleteWorkbenchSearch(ctx, lease, reserved, empty, false)
	require.NoError(t, err)
	assertWorkbenchSearchWallet(t, lease.Run.UserID, each, 0)
	_, err = repo.DispatchWorkbenchSearch(ctx, lease, unreserved, quote)
	require.NoError(t, err)
	_, err = repo.CancelRun(ctx, lease.Run.UserID, lease.Run.ID)
	require.NoError(t, err)
	assertWorkbenchSearchWallet(t, lease.Run.UserID, 0, each)
	_, err = integrationDB.ExecContext(ctx, `UPDATE workbench_search_billing SET expires_at=NOW() WHERE run_id=$1 AND operation_id=$2`, lease.Run.ID, unreserved)
	require.NoError(t, err)
	owners, err := repo.ReleaseExpiredWorkbenchSearchReservations(ctx, 10)
	require.NoError(t, err)
	require.Equal(t, []int64{lease.Run.UserID}, owners)
	owners, err = repo.ReleaseExpiredWorkbenchSearchReservations(ctx, 10)
	require.NoError(t, err)
	require.Empty(t, owners)
	out := service.OfficeOperationResult{Output: json.RawMessage(`{"provider":"deepseek","results":[{"title":"late"}]}`)}
	_, err = repo.CompleteWorkbenchSearch(ctx, lease, unreserved, out, true)
	require.NoError(t, err)
	assertWorkbenchSearchWallet(t, lease.Run.UserID, each, 0)
	charges, total, err := repo.ListWorkbenchSearchCharges(ctx, lease.Run.UserID, 1, 10, time.Time{}, time.Time{})
	require.NoError(t, err)
	require.Zero(t, total)
	require.Empty(t, charges)
	var usageCount int
	require.NoError(t, integrationDB.QueryRowContext(ctx, `SELECT COUNT(*) FROM usage_logs WHERE user_id=$1`, lease.Run.UserID).Scan(&usageCount))
	require.Zero(t, usageCount)
}

func TestWorkbenchSearchBillingAdminListsSettledChargesAcrossUsers(t *testing.T) {
	ctx := context.Background()
	const each = int64(75_000)
	repo, first := workbenchSearchBillingFixture(t, 2*each)
	_, second := workbenchSearchBillingFixture(t, each)
	quote := service.WorkbenchSearchQuote{Provider: "deepseek", AmountUSD: .00075, Billing: service.DefaultBillingCurrencyConfig()}
	start := time.Now().Add(-time.Second)
	for _, lease := range []service.OfficeRunLease{first, second} {
		op := prepareWorkbenchSearchBilling(t, repo, lease, "browser:tool:0:0")
		_, err := repo.DispatchWorkbenchSearch(ctx, lease, op.ID, quote)
		require.NoError(t, err)
		out := service.OfficeOperationResult{Output: json.RawMessage(`{"provider":"deepseek","results":[{"title":"confirmed"}]}`)}
		_, err = repo.CompleteWorkbenchSearch(ctx, lease, op.ID, out, true)
		require.NoError(t, err)
	}
	released := prepareWorkbenchSearchBilling(t, repo, first, "browser:tool:0:1")
	_, err := repo.DispatchWorkbenchSearch(ctx, first, released.ID, quote)
	require.NoError(t, err)
	_, err = repo.CompleteWorkbenchSearch(ctx, first, released.ID, service.OfficeOperationResult{Output: json.RawMessage(`{"provider":"deepseek","results":[]}`)}, false)
	require.NoError(t, err)
	_, err = integrationDB.ExecContext(ctx, `UPDATE users SET deleted_at=NOW() WHERE id=$1`, second.Run.UserID)
	require.NoError(t, err)

	adminCharges, total, err := repo.ListAdminWorkbenchSearchCharges(ctx, 0, 1, 1, start, time.Time{})
	require.NoError(t, err)
	require.EqualValues(t, 2, total)
	require.Len(t, adminCharges, 1)
	require.Equal(t, second.Run.UserID, adminCharges[0].UserID)
	require.Empty(t, adminCharges[0].UserEmail)
	adminCharges, total, err = repo.ListAdminWorkbenchSearchCharges(ctx, first.Run.UserID, 1, 10, start, time.Time{})
	require.NoError(t, err)
	require.EqualValues(t, 1, total)
	require.Len(t, adminCharges, 1)
	require.Equal(t, first.Run.UserID, adminCharges[0].UserID)
	require.True(t, strings.HasSuffix(adminCharges[0].UserEmail, "@office.test"))
	require.Equal(t, each, adminCharges[0].ChargeCredits)
	personalCharges, total, err := repo.ListWorkbenchSearchCharges(ctx, first.Run.UserID, 1, 10, start, time.Time{})
	require.NoError(t, err)
	require.EqualValues(t, 1, total)
	require.Equal(t, []service.WorkbenchSearchCharge{adminCharges[0].WorkbenchSearchCharge}, personalCharges)
}

func TestWorkbenchSearchBillingSettlesSuccessfulResponseAfterCancel(t *testing.T) {
	ctx := context.Background()
	const each = int64(75_000)
	repo, lease := workbenchSearchBillingFixture(t, each)
	quote := service.WorkbenchSearchQuote{Provider: "deepseek", AmountUSD: .00075, Billing: service.DefaultBillingCurrencyConfig()}
	op := prepareWorkbenchSearchBilling(t, repo, lease, "browser:tool:0:0")
	_, err := repo.DispatchWorkbenchSearch(ctx, lease, op.ID, quote)
	require.NoError(t, err)
	run, err := repo.CancelRun(ctx, lease.Run.UserID, lease.Run.ID)
	require.NoError(t, err)
	require.Equal(t, service.OfficeRunCancelled, run.Status)
	forged := lease
	forged.Token = uuid.NewString()
	out := service.OfficeOperationResult{Output: json.RawMessage(`{"provider":"deepseek","results":[{"title":"confirmed"}]}`)}
	_, err = repo.CompleteWorkbenchSearch(ctx, forged, op.ID, out, true)
	require.ErrorIs(t, err, service.ErrOfficeLeaseLost)
	completed, err := repo.CompleteWorkbenchSearch(ctx, lease, op.ID, out, true)
	require.NoError(t, err)
	require.Equal(t, service.OfficeOperationCompleted, completed.State)
	assertWorkbenchSearchWallet(t, lease.Run.UserID, 0, 0)
	charge, err := repo.GetWorkbenchSearchCharge(ctx, lease.Run.UserID, lease.Run.ID, op.ID)
	require.NoError(t, err)
	require.Equal(t, each, charge.ChargeCredits)
	events, err := repo.ListEvents(ctx, lease.Run.UserID, lease.Run.ID, 0, 100)
	require.NoError(t, err)
	for _, event := range events {
		require.NotEqual(t, "tool.completed", event.Type)
	}
}
