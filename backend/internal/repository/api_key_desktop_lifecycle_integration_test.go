//go:build integration

package repository

import (
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/stretchr/testify/require"
)

// A rotation queued behind a committed revoke must read the disabled status
// after obtaining the lock. An earlier HTTP/middleware snapshot cannot restore it.
func TestDesktopAgentCredentialLifecycleConcurrentRevokeBlocksOwnerRotation(t *testing.T) {
	f := newDesktopDBFixture(t)
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	key, _, err := f.repo.EnsureDesktopAgentKey(ctx, f.candidate("before-revoke"))
	require.NoError(t, err)
	blocker, err := integrationDB.BeginTx(ctx, nil)
	require.NoError(t, err)
	defer func() { _ = blocker.Rollback() }()
	_, err = blocker.ExecContext(ctx, `SELECT id FROM users WHERE id=$1 FOR UPDATE`, f.userID)
	require.NoError(t, err)
	result := make(chan error, 1)
	go func() {
		_, _, err := f.repo.MutateDesktopAgentCredential(ctx, service.DesktopAgentCredentialCaller{UserID: f.userID}, f.userID, key.ID, "must-not-be-saved")
		result <- err
	}()
	waitForDesktopCredentialRowLock(t, ctx)
	_, err = blocker.ExecContext(ctx, `UPDATE api_keys SET status='disabled' WHERE id=$1`, key.ID)
	require.NoError(t, err)
	require.NoError(t, blocker.Commit())
	require.ErrorIs(t, <-result, service.ErrDesktopAgentRestoreAdminRequired)
	var actualKey, actualStatus string
	err = integrationDB.QueryRowContext(ctx, `SELECT key,status FROM api_keys WHERE id=$1`, key.ID).Scan(&actualKey, &actualStatus)
	require.NoError(t, err)
	require.Equal(t, key.Key, actualKey)
	require.Equal(t, service.StatusAPIKeyDisabled, actualStatus)
	kept, created, err := f.repo.EnsureDesktopAgentKey(ctx, f.candidate("after-revoke"))
	require.NoError(t, err)
	require.False(t, created)
	require.Equal(t, key.Key, kept.Key)
	require.Equal(t, service.StatusAPIKeyDisabled, kept.Status)
}

func TestDesktopAgentCredentialLifecycleConcurrentAdminDemotionBlocksRestore(t *testing.T) {
	f := newDesktopDBFixture(t)
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	key, _, err := f.repo.EnsureDesktopAgentKey(ctx, f.candidate("before-restore"))
	require.NoError(t, err)
	_, err = integrationDB.ExecContext(ctx, `UPDATE api_keys SET status='disabled' WHERE id=$1`, key.ID)
	require.NoError(t, err)
	admin, err := testEntClient(t).User.Create().SetEmail(fmt.Sprintf("desktop-admin-%d@test.invalid", time.Now().UnixNano())).SetPasswordHash("fixture").SetRole(service.RoleAdmin).SetStatus(service.StatusActive).Save(ctx)
	require.NoError(t, err)
	t.Cleanup(func() {
		_, _ = integrationDB.ExecContext(context.Background(), `DELETE FROM users WHERE id=$1`, admin.ID)
	})
	blocker, err := integrationDB.BeginTx(ctx, nil)
	require.NoError(t, err)
	defer func() { _ = blocker.Rollback() }()
	_, err = blocker.ExecContext(ctx, `SELECT id FROM users WHERE id=$1 FOR UPDATE`, admin.ID)
	require.NoError(t, err)
	result := make(chan error, 1)
	go func() {
		caller := service.DesktopAgentCredentialCaller{UserID: admin.ID, Administrator: true, StepUpAuthorized: true}
		_, _, err := f.repo.MutateDesktopAgentCredential(ctx, caller, f.userID, key.ID, "must-not-be-saved")
		result <- err
	}()
	waitForDesktopCredentialRowLock(t, ctx)
	_, err = blocker.ExecContext(ctx, `UPDATE users SET role=$1 WHERE id=$2`, service.RoleUser, admin.ID)
	require.NoError(t, err)
	require.NoError(t, blocker.Commit())
	require.ErrorIs(t, <-result, service.ErrInsufficientPerms)
	var actualKey, actualStatus string
	err = integrationDB.QueryRowContext(ctx, `SELECT key,status FROM api_keys WHERE id=$1`, key.ID).Scan(&actualKey, &actualStatus)
	require.NoError(t, err)
	require.Equal(t, key.Key, actualKey)
	require.Equal(t, service.StatusAPIKeyDisabled, actualStatus)
}

func waitForDesktopCredentialRowLock(t *testing.T, ctx context.Context) {
	t.Helper()
	require.Eventually(t, func() bool {
		var waiting bool
		err := integrationDB.QueryRowContext(ctx, `SELECT EXISTS(SELECT 1 FROM pg_stat_activity WHERE datname=current_database() AND pid<>pg_backend_pid() AND wait_event_type='Lock' AND query LIKE 'SELECT id,role,status FROM users%')`).Scan(&waiting)
		return err == nil && waiting
	}, 5*time.Second, 10*time.Millisecond, "credential mutation must wait for the current user row lock")
}
