package service

import (
	"context"
	"errors"
	"log/slog"
	"time"
)

var (
	ErrWorkbenchSearchInsufficientBalance = errors.New("insufficient balance for workbench search")
	ErrWorkbenchSearchPriceChanged        = errors.New("workbench search price changed")
)

type WorkbenchSearchQuote struct {
	Provider  string
	AmountUSD float64
	Billing   BillingCurrencyConfig
}

type WorkbenchSearchCharge struct {
	RunID           string    `json:"run_id"`
	OperationID     string    `json:"operation_id"`
	Provider        string    `json:"provider"`
	AmountUSD       float64   `json:"amount_usd"`
	ChargeCredits   int64     `json:"amount_credits"`
	BillingCurrency string    `json:"billing_currency"`
	USDToCNYRate    float64   `json:"usd_to_cny_rate"`
	CreatedAt       time.Time `json:"created_at"`
}

type AdminWorkbenchSearchCharge struct {
	WorkbenchSearchCharge
	UserID    int64  `json:"user_id"`
	UserEmail string `json:"user_email"`
}

type workbenchSearchBillingRepository interface {
	DispatchWorkbenchSearch(context.Context, OfficeRunLease, string, WorkbenchSearchQuote) (*OfficeOperation, error)
	CompleteWorkbenchSearch(context.Context, OfficeRunLease, string, OfficeOperationResult, bool) (*OfficeOperation, error)
	ListWorkbenchSearchCharges(context.Context, int64, int, int, time.Time, time.Time) ([]WorkbenchSearchCharge, int64, error)
	GetWorkbenchSearchCharge(context.Context, int64, string, string) (*WorkbenchSearchCharge, error)
	ReleaseExpiredWorkbenchSearchReservations(context.Context, int) ([]int64, error)
}

func (s *OfficeAgentService) ListAdminSearchCharges(ctx context.Context, userID int64, page, pageSize int, start, end time.Time) ([]AdminWorkbenchSearchCharge, int64, error) {
	repo, ok := s.repo.(interface {
		ListAdminWorkbenchSearchCharges(context.Context, int64, int, int, time.Time, time.Time) ([]AdminWorkbenchSearchCharge, int64, error)
	})
	if !ok {
		return []AdminWorkbenchSearchCharge{}, 0, nil
	}
	return repo.ListAdminWorkbenchSearchCharges(ctx, userID, page, pageSize, start, end)
}

func (s *OfficeAgentService) ListSearchCharges(ctx context.Context, userID int64, page, pageSize int, start, end time.Time) ([]WorkbenchSearchCharge, int64, error) {
	repo, ok := s.repo.(workbenchSearchBillingRepository)
	if !ok {
		return []WorkbenchSearchCharge{}, 0, nil
	}
	return repo.ListWorkbenchSearchCharges(ctx, userID, page, pageSize, start, end)
}

func (s *OfficeAgentService) invalidateSearchBalance(ctx context.Context, userID int64) {
	if s.billingCache != nil {
		if err := s.billingCache.InvalidateUserBalance(ctx, userID); err != nil {
			slog.Warn("workbench search balance cache invalidation failed", "user_id", userID)
		}
	}
}

func (s *OfficeAgentService) sweepSearchReservations() {
	repo, ok := s.repo.(workbenchSearchBillingRepository)
	if !ok {
		return
	}
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	users, err := repo.ReleaseExpiredWorkbenchSearchReservations(ctx, 100)
	if err != nil {
		slog.Warn("workbench search balance release failed", "error", err)
	}
	for _, userID := range users {
		s.invalidateSearchBalance(ctx, userID)
	}
}
