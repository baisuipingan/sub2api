package repository

import (
	"context"
	"database/sql"
	"errors"

	"github.com/Wei-Shaw/sub2api/internal/service"
)

// MutateDesktopAgentCredential serializes with EnsureDesktopAgentKey using the
// same user -> purpose-row lock order. Administrator and target user rows are
// locked by ascending ID to avoid deadlocks between cross-user operations.
// It never creates or deletes a key.
func (r *apiKeyRepository) MutateDesktopAgentCredential(ctx context.Context, caller service.DesktopAgentCredentialCaller, userID, keyID int64, replacement string) (string, string, error) {
	if userID <= 0 || keyID <= 0 {
		return "", "", service.ErrDesktopAgentInvalid
	}
	if err := caller.Validate(userID, replacement != ""); err != nil {
		return "", "", err
	}
	db, ok := r.sql.(*sql.DB)
	if !ok {
		return "", "", errors.New("managed credential requires transaction owner")
	}
	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		return "", "", err
	}
	defer func() { _ = tx.Rollback() }()
	rows, err := tx.QueryContext(ctx, `SELECT id,role,status FROM users WHERE (id=$1 OR id=$2) AND deleted_at IS NULL ORDER BY id FOR UPDATE`, caller.UserID, userID)
	if err != nil {
		return "", "", err
	}
	var actorActive, actorAdmin, targetFound bool
	var status string
	for rows.Next() {
		var id int64
		var role, rowStatus string
		if err := rows.Scan(&id, &role, &rowStatus); err != nil {
			_ = rows.Close()
			return "", "", err
		}
		if id == caller.UserID {
			actorActive = rowStatus == service.StatusActive
			actorAdmin = role == service.RoleAdmin
		}
		if id == userID {
			targetFound, status = true, rowStatus
		}
	}
	if err := rows.Err(); err != nil {
		_ = rows.Close()
		return "", "", err
	}
	if err := rows.Close(); err != nil {
		return "", "", err
	}
	// These facts are read under transaction locks, rather than relying on a
	// role/status snapshot loaded by middleware before a concurrent revocation.
	if !actorActive || (caller.Administrator && !actorAdmin) {
		return "", "", service.ErrInsufficientPerms
	}
	if !targetFound {
		return "", "", service.ErrAPIKeyNotFound
	}
	// 停用账户仍可被管理员撤销；显式恢复Key不能绕过账户停用。
	if replacement != "" && status != service.StatusActive {
		return "", "", service.ErrInsufficientPerms
	}
	key, err := readDesktopAgentKey(ctx, tx, userID)
	if err != nil {
		return "", "", err
	}
	if key == nil || key.ID != keyID {
		return "", "", service.ErrAPIKeyNotFound
	}
	oldKey := key.Key
	if replacement == "" {
		_, err = tx.ExecContext(ctx, `UPDATE api_keys SET status='disabled',updated_at=NOW() WHERE id=$1 AND user_id=$2 AND key_type='system' AND purpose='desktop_agent' AND deleted_at IS NULL`, keyID, userID)
	} else {
		if key.Status == service.StatusAPIKeyDisabled && !caller.Administrator {
			return "", "", service.ErrDesktopAgentRestoreAdminRequired
		}
		// 原因：reset是换密钥，不是清额度；保留全部quota/usage/expiry/限流窗口。
		if key.IsExpired() || key.IsQuotaExhausted() || (key.Status != service.StatusAPIKeyActive && key.Status != service.StatusAPIKeyDisabled) {
			return "", "", service.ErrDesktopAgentInvalid
		}
		_, err = tx.ExecContext(ctx, `UPDATE api_keys SET key=$1,status='active',updated_at=NOW() WHERE id=$2 AND user_id=$3 AND key_type='system' AND purpose='desktop_agent' AND deleted_at IS NULL`, replacement, keyID, userID)
	}
	if err != nil {
		return "", "", err
	}
	if err = tx.Commit(); err != nil {
		return "", "", err
	}
	return oldKey, replacement, nil
}
