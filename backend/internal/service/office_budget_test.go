package service

import (
	"context"
	"errors"
	"net/http"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

type officeSettlementTestRepo struct {
	*officeBridgeTestRepo
	attempts         int
	settleAfter      int
	rejection        error
	suspendedCode    string
	suspendedMessage string
}

func (r *officeSettlementTestRepo) SuspendRun(_ context.Context, _ OfficeRunLease, code, message string) (*OfficeRun, error) {
	r.suspendedCode, r.suspendedMessage = code, message
	r.active = false
	run := r.lease.Run
	run.Status = OfficeRunNeedsConfirmation
	return &run, nil
}

func (r *officeSettlementTestRepo) PrepareOperation(ctx context.Context, lease OfficeRunLease, in OfficeOperationPrepare) (*OfficeOperation, bool, error) {
	r.attempts++
	if r.rejection != nil {
		return nil, false, r.rejection
	}
	if r.attempts <= r.settleAfter {
		return nil, false, ErrOfficeSettlementPending
	}
	return r.officeBridgeTestRepo.PrepareOperation(ctx, lease, in)
}

func TestOfficeBudgetWaitsOnlyForUnsentAdmissionAndNeverRepeatsProviderCall(t *testing.T) {
	var calls atomic.Int32
	svc, base := officeBridgeFixture(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		calls.Add(1)
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"choices":[{"message":{"content":"ok"}}]}`))
	}))
	repo := &officeSettlementTestRepo{officeBridgeTestRepo: base, settleAfter: 2}
	svc.repo = repo
	first, err := svc.InvokeModel(context.Background(), base.lease, "model-after-settlement", officeBridgeBody("hello"))
	require.NoError(t, err)
	require.NotNil(t, first)
	require.Equal(t, 3, repo.attempts)
	require.EqualValues(t, 1, calls.Load())
	_, err = svc.InvokeModel(context.Background(), base.lease, "model-after-settlement", officeBridgeBody("hello"))
	require.NoError(t, err)
	require.EqualValues(t, 1, calls.Load(), "completed inbox replay never re-enters the provider router")
}

func TestOfficeBudgetSettlementWaitIsBoundedAndOnlyRetriesPending(t *testing.T) {
	svc, base := officeBridgeFixture(http.HandlerFunc(func(http.ResponseWriter, *http.Request) { t.Fatal("no supplier call expected") }))
	repo := &officeSettlementTestRepo{officeBridgeTestRepo: base, settleAfter: 10000}
	svc.repo = repo
	in := OfficeOperationPrepare{ID: "new-step", Kind: OfficeOperationModel}
	start := time.Now()
	_, err := svc.prepareOfficeModelOperation(context.Background(), base.lease, in, 20*time.Millisecond)
	require.ErrorIs(t, err, ErrOfficeSettlementPending)
	require.Less(t, time.Since(start), time.Second)
	require.Greater(t, repo.attempts, 1)
	require.Empty(t, base.ops, "waiting for the original ledger must not reserve or dispatch another call")
	for _, rejection := range []error{ErrOfficeBudgetExceeded, ErrOfficeCapacity, ErrOfficeResultUnknown, errors.New("database unavailable")} {
		repo.attempts, repo.rejection = 0, rejection
		_, err = svc.prepareOfficeModelOperation(context.Background(), base.lease, in, time.Second)
		require.ErrorIs(t, err, rejection)
		require.Equal(t, 1, repo.attempts, "ordinary gateway failures and unknown results are never retried")
	}
	repo.attempts, repo.rejection = 0, nil
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	_, err = svc.prepareOfficeModelOperation(ctx, base.lease, in, time.Second)
	require.ErrorIs(t, err, context.Canceled)
}

func TestOfficeBudgetUnsettledLedgerSuspendsInGoBeforeReply(t *testing.T) {
	svc, base := officeBridgeFixture(http.HandlerFunc(func(http.ResponseWriter, *http.Request) { t.Fatal("no supplier call expected") }))
	repo := &officeSettlementTestRepo{officeBridgeTestRepo: base, settleAfter: 10000}
	svc.repo = repo
	_, err := svc.InvokeModel(context.Background(), base.lease, "blocked-new-call", officeBridgeBody("hello"))
	require.ErrorIs(t, err, ErrOfficeSettlementPending)
	require.Equal(t, "BILLING_PENDING", repo.suspendedCode)
	require.Contains(t, repo.suspendedMessage, "本步骤尚未调用")
	require.False(t, repo.active, "Go fences the run immediately; it must not wait for worker lease expiry")
	require.Empty(t, base.ops)
}

func TestOfficeSuspensionReplacesUntrustedReasonWithFixedSafeCopy(t *testing.T) {
	svc, base := officeBridgeFixture(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {}))
	repo := &officeSettlementTestRepo{officeBridgeTestRepo: base}
	svc.repo = repo
	_, err := svc.Suspend(context.Background(), base.lease, "provider-secret-token-and-raw-error")
	require.NoError(t, err)
	require.Equal(t, "RUN_RESULT_UNCONFIRMED", repo.suspendedCode)
	require.NotContains(t, repo.suspendedMessage, "provider-secret")
}
