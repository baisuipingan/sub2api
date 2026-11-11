package repository

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"strings"

	"github.com/Wei-Shaw/sub2api/internal/service"
)

// attachAPIKeyManagementMetadata 不使用 best-effort 标记：缺失元数据时不得向普通 API 返回秘密。
// 保持认证热路径不变；用途仅控制管理与脱敏，不增加网关协议授权规则。
func attachAPIKeyManagementMetadata(ctx context.Context, q sqlExecutor, keys []*service.APIKey) error {
	if len(keys) == 0 {
		return nil
	}
	if q == nil {
		return errors.New("API key management metadata unavailable")
	}
	ids := make([]any, 0, len(keys))
	placeholders := make([]string, 0, len(keys))
	byID := make(map[int64][]*service.APIKey, len(keys))
	for _, k := range keys {
		if k == nil {
			continue
		}
		ids = append(ids, k.ID)
		placeholders = append(placeholders, fmt.Sprintf("$%d", len(ids)))
		byID[k.ID] = append(byID[k.ID], k)
	}
	rows, err := q.QueryContext(ctx, `SELECT id, key_type, COALESCE(purpose, '') FROM api_keys WHERE id IN (`+strings.Join(placeholders, ",")+`)`, ids...)
	if err != nil {
		return fmt.Errorf("load API key management metadata: %w", err)
	}
	defer rows.Close()
	for rows.Next() {
		var id int64
		var typ, purpose string
		if err = rows.Scan(&id, &typ, &purpose); err != nil {
			return err
		}
		if typ != service.APIKeyTypeUser && typ != service.APIKeyTypeSystem {
			return errors.New("invalid API key management type")
		}
		for _, k := range byID[id] {
			k.KeyType = typ
			k.Purpose = purpose
		}
		delete(byID, id)
	}
	if err = rows.Err(); err != nil {
		return err
	}
	if len(byID) != 0 {
		return errors.New("missing API key management metadata")
	}
	return nil
}
func apiKeyPointers(keys []service.APIKey) []*service.APIKey {
	out := make([]*service.APIKey, len(keys))
	for i := range keys {
		out[i] = &keys[i]
	}
	return out
}

// EnsureDesktopAgentKey 用用户行锁保护“不存在的用途”，不能只锁不存在的 Key 行。
// 每个调用都由真实数据库事务串行领取；停用/过期/耗尽行保留身份，绝不重建或复活。
func (r *apiKeyRepository) EnsureDesktopAgentKey(ctx context.Context, candidate *service.APIKey) (*service.APIKey, bool, error) {
	if candidate == nil || candidate.UserID <= 0 || candidate.GroupID == nil || *candidate.GroupID <= 0 || candidate.Key == "" || candidate.Name == "" {
		return nil, false, errors.New("invalid managed key request")
	}
	db, ok := r.sql.(*sql.DB)
	if !ok {
		return nil, false, errors.New("managed key requires database transaction owner")
	}
	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		return nil, false, err
	}
	defer tx.Rollback()
	var uid int64
	var userStatus string
	var restrict bool
	err = scanSingleRow(ctx, tx, `SELECT id,status,restrict_public_groups FROM users WHERE id=$1 AND deleted_at IS NULL FOR UPDATE`, []any{candidate.UserID}, &uid, &userStatus, &restrict)
	if err != nil {
		return nil, false, err
	}
	if userStatus != service.StatusActive {
		return nil, false, service.ErrInsufficientPerms
	}
	// 锁住已验证的配置行，防止过期请求在改组/关闭之后覆盖当前组；提交后 bootstrap 再校验完整 revision。
	var raw string
	err = scanSingleRow(ctx, tx, `SELECT value FROM settings WHERE key=$1 FOR SHARE`, []any{service.SettingKeyDesktopAgentConfig}, &raw)
	if err != nil {
		return nil, false, err
	}
	var cfg struct {
		Enabled bool  `json:"enabled"`
		GroupID int64 `json:"group_id"`
	}
	if json.Unmarshal([]byte(raw), &cfg) != nil || !cfg.Enabled || candidate.GroupID == nil || cfg.GroupID != *candidate.GroupID {
		return nil, false, service.ErrDesktopAgentPolicyChanged
	}
	var status, subscription, platform string
	var exclusive bool
	err = scanSingleRow(ctx, tx, `SELECT status,subscription_type,platform,is_exclusive FROM groups WHERE id=$1 AND deleted_at IS NULL FOR SHARE`, []any{cfg.GroupID}, &status, &subscription, &platform, &exclusive)
	if err != nil {
		return nil, false, err
	}
	if status != service.StatusActive || subscription != service.SubscriptionTypeStandard || (platform != service.PlatformOpenAI && platform != service.PlatformComposite) {
		return nil, false, service.ErrGroupNotAllowed
	}
	if exclusive || restrict {
		// 锁住既有授权行，撤权与本次领取在提交处具有明确顺序。
		var allowedID int64
		err = scanSingleRow(ctx, tx, `SELECT user_id FROM user_allowed_groups WHERE user_id=$1 AND group_id=$2 FOR SHARE`, []any{uid, cfg.GroupID}, &allowedID)
		if errors.Is(err, sql.ErrNoRows) {
			return nil, false, service.ErrGroupNotAllowed
		}
		if err != nil {
			return nil, false, err
		}
	}
	key, err := readDesktopAgentKey(ctx, tx, uid)
	if err != nil {
		return nil, false, err
	}
	changed := false
	if key == nil {
		// 名称冲突只报错，不领用旧名称行；purpose 才是稳定身份。
		_, err = tx.ExecContext(ctx, `INSERT INTO api_keys(user_id,key,name,group_id,status,quota,key_type,purpose,created_at,updated_at)
   VALUES($1,$2,$3,$4,'active',0,'system','desktop_agent',NOW(),NOW()) ON CONFLICT DO NOTHING`, uid, candidate.Key, candidate.Name, cfg.GroupID)
		if err != nil {
			return nil, false, err
		}
		key, err = readDesktopAgentKey(ctx, tx, uid)
		if err != nil {
			return nil, false, err
		}
		if key == nil {
			return nil, false, service.ErrAPIKeyExists
		}
		changed = true
	} else if key.IsActive() && !key.IsExpired() && !key.IsQuotaExhausted() && (key.GroupID == nil || *key.GroupID != cfg.GroupID) {
		res, err := tx.ExecContext(ctx, `UPDATE api_keys SET group_id=$1,updated_at=NOW() WHERE id=$2 AND purpose='desktop_agent' AND key_type='system' AND deleted_at IS NULL`, cfg.GroupID, key.ID)
		if err != nil {
			return nil, false, err
		}
		n, err := res.RowsAffected()
		if err != nil {
			return nil, false, err
		}
		if n != 1 {
			return nil, false, service.ErrAPIKeyNotFound
		}
		key, err = readDesktopAgentKey(ctx, tx, uid)
		if err != nil {
			return nil, false, err
		}
		changed = true
	}
	if err = tx.Commit(); err != nil {
		return nil, false, err
	}
	return key, changed, nil
}
func readDesktopAgentKey(ctx context.Context, q sqlExecutor, userID int64) (*service.APIKey, error) {
	key := &service.APIKey{KeyType: service.APIKeyTypeSystem, Purpose: service.APIKeyPurposeDesktopAgent}
	err := scanSingleRow(ctx, q, `SELECT id,user_id,key,name,group_id,status,quota,quota_used,expires_at,created_at,updated_at
 FROM api_keys WHERE user_id=$1 AND purpose='desktop_agent' AND key_type='system' AND deleted_at IS NULL FOR UPDATE`, []any{userID},
		&key.ID, &key.UserID, &key.Key, &key.Name, &key.GroupID, &key.Status, &key.Quota, &key.QuotaUsed, &key.ExpiresAt, &key.CreatedAt, &key.UpdatedAt)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return key, nil
}
