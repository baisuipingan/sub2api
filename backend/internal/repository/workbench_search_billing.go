package repository

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"math"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/service"
)

const workbenchSearchReservationTTL = 10 * time.Minute

func lockWorkbenchSearchWallet(ctx context.Context, tx *sql.Tx, userID int64) error {
	var id int64
	err := tx.QueryRowContext(ctx, `SELECT id FROM users WHERE id=$1 FOR UPDATE`, userID).Scan(&id)
	return officeDBError(err)
}

func (r *officeAgentRepository) DispatchWorkbenchSearch(ctx context.Context, lease service.OfficeRunLease, operationID string, quote service.WorkbenchSearchQuote) (*service.OfficeOperation, error) {
	amount := service.USDToCredits(service.QuantizeUsageBillingAmount(quote.AmountUSD))
	if !officeToken.MatchString(operationID) || amount <= 0 || math.IsNaN(quote.AmountUSD) || math.IsInf(quote.AmountUSD, 0) {
		return nil, service.ErrOfficeInvalid
	}
	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return nil, err
	}
	defer func() { _ = tx.Rollback() }()
	if err = lockWorkbenchSearchWallet(ctx, tx, lease.Run.UserID); err != nil {
		return nil, err
	}
	run, err := checkOfficeLease(ctx, tx, lease)
	if err != nil {
		return nil, err
	}
	if !run.SearchEnabled {
		return nil, service.ErrOfficeConflict
	}
	op, err := scanOfficeOperation(tx.QueryRowContext(ctx, `SELECT `+officeOperationColumns+` FROM office_agent_operations WHERE run_id=$1 AND operation_id=$2 FOR UPDATE`, run.ID, operationID))
	if err != nil {
		return nil, err
	}
	var input struct{ Tool string }
	if op.Kind != service.OfficeOperationTool || json.Unmarshal(op.Input, &input) != nil || input.Tool != "web_search" {
		return nil, service.ErrOfficeConflict
	}
	switch op.State {
	case service.OfficeOperationPrepared:
	case service.OfficeOperationUnknown:
		return nil, service.ErrOfficeResultUnknown
	default:
		return nil, service.ErrOfficeOperationDispatched
	}

	// The wallet trigger mirrors integer units to the legacy USD representation.
	// The same balance comparison covers concurrent search and other deductions.
	var available int64
	err = tx.QueryRowContext(ctx, `UPDATE users
		SET balance_credits=balance_credits-$1, frozen_balance_credits=frozen_balance_credits+$1,
			balance_credits_version=balance_credits_version+1, updated_at=NOW()
		WHERE id=$2 AND deleted_at IS NULL AND balance_credits >= $1 RETURNING balance_credits`, amount, run.UserID).Scan(&available)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, service.ErrWorkbenchSearchInsufficientBalance
	}
	if err != nil {
		return nil, err
	}
	_, err = tx.ExecContext(ctx, `INSERT INTO workbench_search_billing
		(run_id,operation_id,user_id,provider,amount_credits,billing_engine,billing_currency,usd_to_cny,billing_version,expires_at)
		VALUES($1,$2,$3,$4,$5,$6,$7,$8,$9,NOW()+$10*INTERVAL '1 second')`,
		run.ID, operationID, run.UserID, quote.Provider, amount, quote.Billing.EffectiveEngine(),
		quote.Billing.DisplayCurrency, quote.Billing.USDToCNY, quote.Billing.Version, int64(workbenchSearchReservationTTL/time.Second))
	if err != nil {
		return nil, err
	}
	op, err = dispatchOfficeOperation(ctx, tx, run.ID, operationID, lease)
	if err != nil {
		return nil, err
	}
	return op, tx.Commit()
}

func (r *officeAgentRepository) CompleteWorkbenchSearch(ctx context.Context, lease service.OfficeRunLease, operationID string, result service.OfficeOperationResult, billable bool) (*service.OfficeOperation, error) {
	return r.recordOfficeOperationResult(ctx, lease, operationID, result, &billable)
}

func settleWorkbenchSearch(ctx context.Context, tx *sql.Tx, userID int64, op *service.OfficeOperation, receipt service.OfficeOperationResult, billable bool) error {
	var amount, version int64
	var state, provider, engine, currency string
	var fx float64
	err := tx.QueryRowContext(ctx, `SELECT amount_credits,billing_version,state,provider,billing_engine,billing_currency,usd_to_cny
		FROM workbench_search_billing WHERE run_id=$1 AND operation_id=$2 AND user_id=$3 FOR UPDATE`, op.RunID, op.ID, userID).
		Scan(&amount, &version, &state, &provider, &engine, &currency, &fx)
	if err != nil {
		return officeDBError(err)
	}
	if state == "expired" {
		// An expired hold has already been returned. The original dispatcher
		// may still deposit its receipt without imposing a later deduction.
		return nil
	}
	if state != "reserved" {
		return service.ErrOfficeConflict
	}
	if !billable {
		return releaseWorkbenchSearchHold(ctx, tx, userID, op.RunID, op.ID, amount, "released")
	}
	var result struct{ Provider string }
	if receipt.ErrorCode != "" || (receipt.ToolStatus != "" && receipt.ToolStatus != "completed") ||
		json.Unmarshal(receipt.Output, &result) != nil || result.Provider != provider {
		return service.ErrOfficeConflict
	}
	var balance int64
	err = tx.QueryRowContext(ctx, `UPDATE users SET frozen_balance_credits=frozen_balance_credits-$1, updated_at=NOW()
		WHERE id=$2 AND frozen_balance_credits >= $1 RETURNING balance_credits`, amount, userID).Scan(&balance)
	if err != nil {
		return officeDBError(err)
	}
	if err = insertUsageBillingCreditLedger(ctx, tx, &service.UsageBillingCommand{
		RequestID: "client:" + op.RequestID, UserID: userID, BalanceCredits: amount,
		BalanceCost: float64(amount) / float64(service.CreditsPerUSD), BillingEngine: engine, BillingCurrency: currency,
		CreditsPerUSD: service.CreditsPerUSD, USDToCNYRate: fx, BillingVersion: version,
	}, balance); err != nil {
		return err
	}
	_, err = tx.ExecContext(ctx, `UPDATE workbench_search_billing SET state='charged',settled_at=NOW() WHERE run_id=$1 AND operation_id=$2`, op.RunID, op.ID)
	return err
}

func releaseWorkbenchSearchHold(ctx context.Context, tx *sql.Tx, userID int64, runID, operationID string, amount int64, state string) error {
	var balance int64
	err := tx.QueryRowContext(ctx, `UPDATE users SET balance_credits=balance_credits+$1,
		frozen_balance_credits=frozen_balance_credits-$1, balance_credits_version=balance_credits_version+1, updated_at=NOW()
		WHERE id=$2 AND frozen_balance_credits >= $1 RETURNING balance_credits`, amount, userID).Scan(&balance)
	if err != nil {
		return officeDBError(err)
	}
	_, err = tx.ExecContext(ctx, `UPDATE workbench_search_billing SET state=$3,settled_at=NOW() WHERE run_id=$1 AND operation_id=$2`, runID, operationID, state)
	return err
}

type workbenchSearchHold struct {
	userID             int64
	runID, operationID string
}

func (r *officeAgentRepository) ReleaseExpiredWorkbenchSearchReservations(ctx context.Context, limit int) ([]int64, error) {
	rows, err := r.db.QueryContext(ctx, `SELECT user_id,run_id::text,operation_id FROM workbench_search_billing
		WHERE state='reserved' AND expires_at <= NOW() ORDER BY expires_at LIMIT $1`, max(1, min(limit, 200)))
	if err != nil {
		return nil, err
	}
	var expired []workbenchSearchHold
	for rows.Next() {
		var hold workbenchSearchHold
		if err = rows.Scan(&hold.userID, &hold.runID, &hold.operationID); err != nil {
			_ = rows.Close()
			return nil, err
		}
		expired = append(expired, hold)
	}
	_ = rows.Close()
	if err = rows.Err(); err != nil {
		return nil, err
	}
	users := make([]int64, 0, len(expired))
	for _, hold := range expired {
		released, err := r.releaseExpiredWorkbenchSearchReservation(ctx, hold)
		if err != nil {
			return users, err
		}
		if released {
			users = append(users, hold.userID)
		}
	}
	return users, nil
}

func (r *officeAgentRepository) releaseExpiredWorkbenchSearchReservation(ctx context.Context, hold workbenchSearchHold) (bool, error) {
	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return false, err
	}
	defer func() { _ = tx.Rollback() }()
	if err = lockWorkbenchSearchWallet(ctx, tx, hold.userID); err != nil {
		return false, err
	}
	var amount int64
	err = tx.QueryRowContext(ctx, `SELECT amount_credits FROM workbench_search_billing WHERE run_id=$1 AND operation_id=$2
		AND user_id=$3 AND state='reserved' AND expires_at <= NOW() FOR UPDATE`, hold.runID, hold.operationID, hold.userID).Scan(&amount)
	if errors.Is(err, sql.ErrNoRows) {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	if err = releaseWorkbenchSearchHold(ctx, tx, hold.userID, hold.runID, hold.operationID, amount, "expired"); err != nil {
		return false, err
	}
	return true, tx.Commit()
}

const workbenchSearchChargeColumns = `b.run_id::text,b.operation_id,b.provider,b.amount_credits,b.billing_currency,b.usd_to_cny,b.settled_at`

func scanWorkbenchSearchCharge(row officeScanner, charge *service.WorkbenchSearchCharge, extra ...any) error {
	fields := []any{&charge.RunID, &charge.OperationID, &charge.Provider, &charge.ChargeCredits,
		&charge.BillingCurrency, &charge.USDToCNYRate, &charge.CreatedAt}
	err := row.Scan(append(fields, extra...)...)
	charge.AmountUSD = float64(charge.ChargeCredits) / float64(service.CreditsPerUSD)
	return err
}

func (r *officeAgentRepository) GetWorkbenchSearchCharge(ctx context.Context, userID int64, runID, operationID string) (*service.WorkbenchSearchCharge, error) {
	if !officeUUID(runID) || !officeToken.MatchString(operationID) {
		return nil, service.ErrOfficeNotFound
	}
	var charge service.WorkbenchSearchCharge
	err := scanWorkbenchSearchCharge(r.db.QueryRowContext(ctx, `SELECT `+workbenchSearchChargeColumns+`
		FROM workbench_search_billing b WHERE user_id=$1 AND run_id=$2 AND operation_id=$3 AND state='charged'`, userID, runID, operationID), &charge)
	if err != nil {
		return nil, officeDBError(err)
	}
	return &charge, nil
}

func (r *officeAgentRepository) ListWorkbenchSearchCharges(ctx context.Context, userID int64, page, pageSize int, start, end time.Time) ([]service.WorkbenchSearchCharge, int64, error) {
	if userID <= 0 {
		return nil, 0, service.ErrOfficeInvalid
	}
	rows, total, err := r.ListAdminWorkbenchSearchCharges(ctx, userID, page, pageSize, start, end)
	if err != nil {
		return nil, 0, err
	}
	items := make([]service.WorkbenchSearchCharge, 0, len(rows))
	for _, row := range rows {
		items = append(items, row.WorkbenchSearchCharge)
	}
	return items, total, nil
}

func (r *officeAgentRepository) ListAdminWorkbenchSearchCharges(ctx context.Context, userID int64, page, pageSize int, start, end time.Time) ([]service.AdminWorkbenchSearchCharge, int64, error) {
	if userID < 0 || page < 1 || page > 10000 || pageSize < 1 || pageSize > 200 {
		return nil, 0, service.ErrOfficeInvalid
	}
	startBound, endBound := sql.NullTime{Time: start, Valid: !start.IsZero()}, sql.NullTime{Time: end, Valid: !end.IsZero()}
	const filter = `WHERE b.state='charged' AND ($1::bigint=0 OR b.user_id=$1)
		AND ($2::timestamptz IS NULL OR b.settled_at >= $2) AND ($3::timestamptz IS NULL OR b.settled_at < $3)`
	tx, err := r.db.BeginTx(ctx, &sql.TxOptions{Isolation: sql.LevelRepeatableRead, ReadOnly: true})
	if err != nil {
		return nil, 0, err
	}
	defer func() { _ = tx.Rollback() }()
	var total int64
	if err = tx.QueryRowContext(ctx, `SELECT COUNT(*) FROM workbench_search_billing b `+filter, userID, startBound, endBound).Scan(&total); err != nil {
		return nil, 0, err
	}
	rows, err := tx.QueryContext(ctx, `SELECT `+workbenchSearchChargeColumns+`,b.user_id,COALESCE(u.email,'')
		FROM workbench_search_billing b LEFT JOIN users u ON u.id=b.user_id AND u.deleted_at IS NULL `+filter+
		` ORDER BY b.settled_at DESC,b.run_id,b.operation_id LIMIT $4 OFFSET $5`, userID, startBound, endBound, pageSize, (page-1)*pageSize)
	if err != nil {
		return nil, 0, err
	}
	defer func() { _ = rows.Close() }()
	items := make([]service.AdminWorkbenchSearchCharge, 0, pageSize)
	for rows.Next() {
		var item service.AdminWorkbenchSearchCharge
		if err := scanWorkbenchSearchCharge(rows, &item.WorkbenchSearchCharge, &item.UserID, &item.UserEmail); err != nil {
			return nil, 0, err
		}
		items = append(items, item)
	}
	if err = rows.Err(); err != nil {
		return nil, 0, err
	}
	return items, total, tx.Commit()
}
