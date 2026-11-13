//go:build unit

package repository

import (
	"context"
	"database/sql/driver"
	"errors"
	"testing"
	"time"

	"github.com/DATA-DOG/go-sqlmock"
	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/stretchr/testify/require"
)

func TestDesktopAgentCredentialLifecyclePreservesLimitsAndRequiresPurpose(t *testing.T) {
	for _, kind := range []string{
		"owner rotate active", "owner revoke active", "owner revoke disabled", "owner cannot restore disabled",
		"admin restore disabled", "admin rotate active", "admin revoke inactive user", "admin reset inactive user",
		"wrong id", "missing", "expired", "exhausted", "expired status", "quota exhausted status",
		"actor disabled", "actor deleted", "admin demoted", "target deleted", "write failure", "commit failure",
	} {
		t.Run(kind, func(t *testing.T) {
			db, mock, err := sqlmock.New()
			require.NoError(t, err)
			defer db.Close()
			r := &apiKeyRepository{sql: db}
			caller := service.DesktopAgentCredentialCaller{UserID: 7}
			admin := kind == "admin restore disabled" || kind == "admin rotate active" || kind == "admin revoke inactive user" || kind == "admin reset inactive user" || kind == "admin demoted" || kind == "actor deleted" || kind == "target deleted"
			if admin {
				caller = service.DesktopAgentCredentialCaller{UserID: 8, Administrator: true, StepUpAuthorized: true}
			}
			replacement := "replacement-secret"
			if kind == "owner revoke active" || kind == "owner revoke disabled" || kind == "admin revoke inactive user" {
				replacement = ""
			}
			mock.ExpectBegin()
			userRows := sqlmock.NewRows([]string{"id", "role", "status"})
			targetStatus := service.StatusActive
			if kind == "admin revoke inactive user" || kind == "admin reset inactive user" || kind == "actor disabled" {
				targetStatus = "disabled"
			}
			if kind != "target deleted" {
				userRows.AddRow(7, service.RoleUser, targetStatus)
			}
			if admin && kind != "actor deleted" {
				role := service.RoleAdmin
				if kind == "admin demoted" {
					role = service.RoleUser
				}
				userRows.AddRow(8, role, service.StatusActive)
			}
			// Authorization and current status are read inside the mutation's
			// transaction. Lock ordering also serializes with bootstrap/revoke.
			mock.ExpectQuery("SELECT id,role,status FROM users .* ORDER BY id FOR UPDATE").WithArgs(caller.UserID, int64(7)).WillReturnRows(userRows)
			permissionFailure := kind == "actor disabled" || kind == "actor deleted" || kind == "admin demoted" || kind == "admin reset inactive user"
			if permissionFailure || kind == "target deleted" {
				mock.ExpectRollback()
			} else {
				cols := []string{"id", "user_id", "key", "name", "group_id", "status", "quota", "quota_used", "expires_at", "created_at", "updated_at"}
				rows := sqlmock.NewRows(cols)
				id := int64(99)
				if kind == "wrong id" {
					id = 100
				}
				var expiry driver.Value
				if kind == "expired" {
					expiry = time.Now().Add(-time.Hour)
				}
				quota, used := float64(20), float64(5)
				if kind == "exhausted" {
					used = 20
				}
				status := service.StatusAPIKeyActive
				if kind == "owner revoke disabled" || kind == "owner cannot restore disabled" || kind == "admin restore disabled" {
					status = service.StatusAPIKeyDisabled
				}
				if kind == "expired status" {
					status = service.StatusAPIKeyExpired
				}
				if kind == "quota exhausted status" {
					status = service.StatusAPIKeyQuotaExhausted
				}
				if kind != "missing" {
					rows.AddRow(id, 7, "old-secret", "Desktop Agent (managed)", 12, status, quota, used, expiry, time.Now(), time.Now())
				}
				mock.ExpectQuery("SELECT id,user_id,key,name,group_id,status,quota,quota_used,expires_at,created_at,updated_at.*purpose='desktop_agent'.*FOR UPDATE").WithArgs(int64(7)).WillReturnRows(rows)
				invalid := kind == "wrong id" || kind == "missing" || kind == "expired" || kind == "exhausted" || kind == "expired status" || kind == "quota exhausted status" || kind == "owner cannot restore disabled"
				if invalid {
					mock.ExpectRollback()
				} else {
					var exec *sqlmock.ExpectedExec
					// Exact UPDATE shape ensures quota/usage/expiry/rate-limit
					// fields and all unrelated keys remain untouched.
					if replacement == "" {
						exec = mock.ExpectExec("UPDATE api_keys SET status='disabled',updated_at=NOW\\(\\) WHERE id=\\$1 AND user_id=\\$2 AND key_type='system' AND purpose='desktop_agent' AND deleted_at IS NULL").WithArgs(int64(99), int64(7))
					} else {
						exec = mock.ExpectExec("UPDATE api_keys SET key=\\$1,status='active',updated_at=NOW\\(\\) WHERE id=\\$2 AND user_id=\\$3 AND key_type='system' AND purpose='desktop_agent' AND deleted_at IS NULL").WithArgs(replacement, int64(99), int64(7))
					}
					if kind == "write failure" {
						exec.WillReturnError(errors.New("write failed"))
						mock.ExpectRollback()
					} else {
						exec.WillReturnResult(sqlmock.NewResult(0, 1))
						if kind == "commit failure" {
							mock.ExpectCommit().WillReturnError(errors.New("commit failed"))
						} else {
							mock.ExpectCommit()
						}
					}
				}
			}
			old, next, err := r.MutateDesktopAgentCredential(context.Background(), caller, 7, 99, replacement)
			success := kind == "owner rotate active" || kind == "owner revoke active" || kind == "owner revoke disabled" || kind == "admin restore disabled" || kind == "admin rotate active" || kind == "admin revoke inactive user"
			if success {
				require.NoError(t, err)
				require.Equal(t, "old-secret", old)
				require.Equal(t, replacement, next)
			} else {
				require.Error(t, err)
				require.Empty(t, old)
				require.Empty(t, next)
				if permissionFailure {
					require.ErrorIs(t, err, service.ErrInsufficientPerms)
				}
				if kind == "owner cannot restore disabled" {
					require.ErrorIs(t, err, service.ErrDesktopAgentRestoreAdminRequired)
				}
			}
			require.NoError(t, mock.ExpectationsWereMet())
		})
	}
}

func TestDesktopAgentCredentialLifecycleRejectsUntrustedCallerBeforeTransaction(t *testing.T) {
	for _, caller := range []service.DesktopAgentCredentialCaller{
		{},
		{UserID: 8},
		{UserID: 8, Administrator: true},
		{UserID: 7, Administrator: true},
	} {
		db, mock, err := sqlmock.New()
		require.NoError(t, err)
		r := &apiKeyRepository{sql: db}
		_, _, err = r.MutateDesktopAgentCredential(context.Background(), caller, 7, 99, "replacement")
		require.ErrorIs(t, err, service.ErrInsufficientPerms)
		require.NoError(t, mock.ExpectationsWereMet())
		mock.ExpectClose()
		require.NoError(t, db.Close())
	}
}
