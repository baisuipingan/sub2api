//go:build unit

package service

import (
	"testing"
	"time"

	dbent "github.com/Wei-Shaw/sub2api/ent"
	"github.com/Wei-Shaw/sub2api/internal/payment"
	"github.com/stretchr/testify/require"
)

func TestUSDTBEP20ReceiptsPreserveExactNativeAmounts(t *testing.T) {
	t.Parallel()

	today := time.Date(2026, time.November, 24, 0, 0, 0, 0, time.UTC)
	yesterday := today.Add(-time.Hour)
	orders := []*dbent.PaymentOrder{
		{PaymentType: payment.TypeUSDTBSC, PaidAt: &yesterday, ProviderSnapshot: map[string]any{"currency": "USD", "crypto_amount": "3.01"}},
		{PaymentType: payment.TypeUSDTBSC, PaidAt: &today, ProviderSnapshot: map[string]any{"currency": "CNY", "crypto_amount": "0.000000000000000001"}},
		{PaymentType: payment.TypeUSDTBSC, PaidAt: &today},
		{PaymentType: payment.TypeUSDTBSC, ProviderSnapshot: map[string]any{"currency": "USD", "crypto_amount": "10"}},
	}
	require.Equal(t, &USDTBEP20ReceiptTotals{
		TotalAmount: "3.010000000000000001", TodayAmount: "0.000000000000000001", MissingAmountCount: 1,
	}, sumUSDTBEP20Receipts(orders, today))
}

func TestPaymentOrderAccountingUsesTheOrderSnapshot(t *testing.T) {
	t.Parallel()

	completedAt := time.Date(2026, time.November, 24, 0, 0, 0, 0, time.UTC)
	order := &dbent.PaymentOrder{
		OrderType: payment.OrderTypeBalance, PaymentType: payment.TypeAlipay,
		Amount: 1.56, CreditedCredits: 155_555_556, BonusCredits: 7_407_408, UsdToCnySnapshot: 6.75,
		ProviderSnapshot: map[string]any{"recharge_pricing_version": float64(1), "platform_currency": "CNY"},
	}
	require.Equal(t, PaymentOrderAccountingDetails{
		CreditedDisplayAmount: "10.50", CreditedDisplayCurrency: "CNY", BonusDisplayAmount: "0.50", RefundBlocked: true,
	}, PaymentOrderAccounting(order))

	order.CompletedAt = &completedAt
	details := PaymentOrderAccounting(order)
	require.Equal(t, &completedAt, details.CreditedAt)

	order.ProviderSnapshot = nil
	details = PaymentOrderAccounting(order)
	require.False(t, details.RefundBlocked)
	require.Equal(t, "USD", details.CreditedDisplayCurrency)
	require.Equal(t, "1.56", details.CreditedDisplayAmount)
}

func TestComputeBasicStatsGroupsAmountsByCurrency(t *testing.T) {
	t.Parallel()

	todayStart := time.Date(2026, time.July, 25, 0, 0, 0, 0, time.UTC)
	yesterday := todayStart.Add(-time.Hour)
	today := todayStart.Add(time.Hour)
	orders := []*dbent.PaymentOrder{
		paymentStatsTestOrder(1, "alice@example.com", "CNY", 10, &today),
		paymentStatsTestOrder(2, "bob@example.com", "USD", 10, &today),
		paymentStatsTestOrder(1, "alice@example.com", "CNY", 5, &yesterday),
	}

	stats := &DashboardStats{}
	computeBasicStats(stats, orders, todayStart)

	require.Equal(t, CurrencyAmounts{"CNY": 15, "USD": 10}, stats.TotalAmount)
	require.Equal(t, CurrencyAmounts{"CNY": 10, "USD": 10}, stats.TodayAmount)
	require.Equal(t, CurrencyAmounts{"CNY": 7.5, "USD": 10}, stats.AvgAmount)
	require.Equal(t, 3, stats.TotalCount)
	require.Equal(t, 2, stats.TodayCount)
}

func TestPaymentDashboardBreakdownsGroupAmountsAndRankingsByCurrency(t *testing.T) {
	t.Parallel()

	firstDay := time.Date(2026, time.July, 24, 12, 0, 0, 0, time.UTC)
	secondDay := firstDay.AddDate(0, 0, 1)
	orders := []*dbent.PaymentOrder{
		paymentStatsTestOrder(1, "alice@example.com", "CNY", 5.555, &firstDay),
		paymentStatsTestOrder(2, "bob@example.com", "CNY", 10, &firstDay),
		paymentStatsTestOrder(1, "alice@example.com", "USD", 20, &secondDay),
		paymentStatsTestOrder(2, "bob@example.com", "USD", 10, &secondDay),
	}
	orders[0].PaymentType = "stripe"
	orders[1].PaymentType = "stripe"
	orders[2].PaymentType = "stripe"
	orders[3].PaymentType = "alipay"

	daily := buildDailySeries(orders, firstDay.AddDate(0, 0, -1), 2)
	require.Equal(t, []DailyStats{
		{Date: "2026-07-24", Amount: CurrencyAmounts{"CNY": 15.56}, Count: 2},
		{Date: "2026-07-25", Amount: CurrencyAmounts{"USD": 30}, Count: 2},
	}, daily)

	methods := buildMethodDistribution(orders)
	require.Equal(t, []PaymentMethodStat{
		{Type: "alipay", Amount: CurrencyAmounts{"USD": 10}, Count: 1},
		{Type: "stripe", Amount: CurrencyAmounts{"CNY": 15.56, "USD": 20}, Count: 3},
	}, methods)

	users := buildTopUsers(orders)
	require.Equal(t, TopUsersByCurrency{
		"CNY": {
			{UserID: 2, Email: "bob@example.com", Amount: 10},
			{UserID: 1, Email: "alice@example.com", Amount: 5.56},
		},
		"USD": {
			{UserID: 1, Email: "alice@example.com", Amount: 20},
			{UserID: 2, Email: "bob@example.com", Amount: 10},
		},
	}, users)
}

func paymentStatsTestOrder(userID int64, email, currency string, amount float64, paidAt *time.Time) *dbent.PaymentOrder {
	return &dbent.PaymentOrder{
		UserID:           userID,
		UserEmail:        email,
		PayAmount:        amount,
		PaidAt:           paidAt,
		ProviderSnapshot: map[string]any{"currency": currency},
	}
}
