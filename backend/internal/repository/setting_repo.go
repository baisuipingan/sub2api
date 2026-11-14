package repository

import (
	"context"
	"errors"
	"time"

	"github.com/Wei-Shaw/sub2api/ent"
	"github.com/Wei-Shaw/sub2api/ent/setting"
	"github.com/Wei-Shaw/sub2api/internal/service"
)

// SetContentModerationConfig holds live group rows until the setting is written.
// Deletion takes FOR UPDATE on the group before cleaning this setting, so both
// operations follow the same lock order: groups, then settings.
func (r *settingRepository) SetContentModerationConfig(ctx context.Context, value string, groupIDs []int64) error {
	tx, err := r.client.Tx(ctx)
	if err != nil && !errors.Is(err, ent.ErrTxStarted) {
		return err
	}
	client := r.client
	if err == nil {
		defer func() { _ = tx.Rollback() }()
		client = tx.Client()
	}
	if err := lockLiveGroups(ctx, client, groupIDs); err != nil {
		return err
	}
	now := time.Now()
	if err := client.Setting.Create().SetKey(service.SettingKeyContentModerationConfig).
		SetValue(value).SetUpdatedAt(now).OnConflictColumns(setting.FieldKey).
		UpdateNewValues().Exec(ctx); err != nil {
		return err
	}
	if tx != nil {
		return tx.Commit()
	}
	return nil
}

type settingRepository struct {
	client *ent.Client
}

func NewSettingRepository(client *ent.Client) service.SettingRepository {
	return &settingRepository{client: client}
}

func (r *settingRepository) Get(ctx context.Context, key string) (*service.Setting, error) {
	m, err := r.client.Setting.Query().Where(setting.KeyEQ(key)).Only(ctx)
	if err != nil {
		if ent.IsNotFound(err) {
			return nil, service.ErrSettingNotFound
		}
		return nil, err
	}
	return &service.Setting{
		ID:        m.ID,
		Key:       m.Key,
		Value:     m.Value,
		UpdatedAt: m.UpdatedAt,
	}, nil
}

func (r *settingRepository) GetValue(ctx context.Context, key string) (string, error) {
	setting, err := r.Get(ctx, key)
	if err != nil {
		return "", err
	}
	return setting.Value, nil
}

func (r *settingRepository) Set(ctx context.Context, key, value string) error {
	now := time.Now()
	return r.client.Setting.
		Create().
		SetKey(key).
		SetValue(value).
		SetUpdatedAt(now).
		OnConflictColumns(setting.FieldKey).
		UpdateNewValues().
		Exec(ctx)
}

// ReplaceExistingAdminAPIKey 原子轮换仍存在的旧版管理员密钥。
// 受影响行数为零表示密钥已被删除或不存在，绝不能通过 upsert 重新创建。
func (r *settingRepository) ReplaceExistingAdminAPIKey(ctx context.Context, value string) (bool, error) {
	n, err := r.client.Setting.Update().
		Where(setting.KeyEQ(service.SettingKeyAdminAPIKey), setting.ValueNEQ("")).
		SetValue(value).
		SetUpdatedAt(time.Now()).
		Save(ctx)
	if err != nil {
		return false, err
	}
	return n == 1, nil
}

func (r *settingRepository) GetMultiple(ctx context.Context, keys []string) (map[string]string, error) {
	if len(keys) == 0 {
		return map[string]string{}, nil
	}
	settings, err := r.client.Setting.Query().Where(setting.KeyIn(keys...)).All(ctx)
	if err != nil {
		return nil, err
	}

	result := make(map[string]string)
	for _, s := range settings {
		result[s.Key] = s.Value
	}
	return result, nil
}

func (r *settingRepository) SetMultiple(ctx context.Context, settings map[string]string) error {
	if len(settings) == 0 {
		return nil
	}

	now := time.Now()
	builders := make([]*ent.SettingCreate, 0, len(settings))
	for key, value := range settings {
		builders = append(builders, r.client.Setting.Create().SetKey(key).SetValue(value).SetUpdatedAt(now))
	}
	return r.client.Setting.
		CreateBulk(builders...).
		OnConflictColumns(setting.FieldKey).
		UpdateNewValues().
		Exec(ctx)
}

func (r *settingRepository) GetAll(ctx context.Context) (map[string]string, error) {
	settings, err := r.client.Setting.Query().All(ctx)
	if err != nil {
		return nil, err
	}

	result := make(map[string]string)
	for _, s := range settings {
		result[s.Key] = s.Value
	}
	return result, nil
}

func (r *settingRepository) Delete(ctx context.Context, key string) error {
	_, err := r.client.Setting.Delete().Where(setting.KeyEQ(key)).Exec(ctx)
	return err
}

// CompareAndSwapDesktopAgentConfig never upserts: a stale disable cannot replace
// a newer policy or resurrect a configuration removed by another administrator.
func (r *settingRepository) CompareAndSwapDesktopAgentConfig(ctx context.Context, previous, next string) (bool, error) {
	count, err := r.client.Setting.Update().Where(setting.KeyEQ(service.SettingKeyDesktopAgentConfig), setting.ValueEQ(previous)).SetValue(next).SetUpdatedAt(time.Now()).Save(ctx)
	return count == 1, err
}

// Catalog publication is compare-and-swap: a stale refresh must not overwrite
// another instance's newer source or last-good snapshot.
func (r *settingRepository) CompareAndSwapDesktopAgentProfileCatalog(ctx context.Context, previous, next string, configGuard ...string) (bool, error) {
	tx, err := r.client.Tx(ctx)
	if err != nil {
		return false, err
	}
	defer func() { _ = tx.Rollback() }()
	row, err := lockDesktopAgentProfileCatalog(ctx, tx)
	if err != nil {
		return false, err
	}
	if row.Value != previous {
		return false, nil
	}
	if len(configGuard) > 0 {
		cfg, readErr := tx.Setting.Query().Where(setting.KeyEQ(service.SettingKeyDesktopAgentConfig)).ForShare().Only(ctx)
		raw := ""
		if readErr != nil && !ent.IsNotFound(readErr) {
			return false, readErr
		}
		if cfg != nil {
			raw = cfg.Value
		}
		if raw != configGuard[0] {
			return false, nil
		}
	}
	if _, err = tx.Setting.UpdateOne(row).SetValue(next).SetUpdatedAt(time.Now()).Save(ctx); err != nil {
		return false, err
	}
	if err = tx.Commit(); err != nil {
		return false, err
	}
	return true, nil
}

// Always lock catalog before config in both publication directions. Creating an
// empty placeholder inside this transaction also serializes first publication.
func lockDesktopAgentProfileCatalog(ctx context.Context, tx *ent.Tx) (*ent.Setting, error) {
	// Ent's single-row DO NOTHING still requests RETURNING id and reports
	// sql.ErrNoRows when the key exists. Ignore preserves existing columns while
	// returning the locked row's ID in both the insert and conflict cases.
	if err := tx.Setting.Create().SetKey(service.SettingKeyDesktopAgentProfileCatalog).SetValue("").SetUpdatedAt(time.Now()).OnConflictColumns(setting.FieldKey).Ignore().Exec(ctx); err != nil {
		return nil, err
	}
	return tx.Setting.Query().Where(setting.KeyEQ(service.SettingKeyDesktopAgentProfileCatalog)).ForUpdate().Only(ctx)
}
func (r *settingRepository) SaveDesktopAgentConfigWithCatalog(ctx context.Context, expectedCatalog, value string) (bool, error) {
	tx, err := r.client.Tx(ctx)
	if err != nil {
		return false, err
	}
	defer func() { _ = tx.Rollback() }()
	row, err := lockDesktopAgentProfileCatalog(ctx, tx)
	if err != nil {
		return false, err
	}
	if row.Value != expectedCatalog {
		return false, nil
	}
	if err = tx.Setting.Create().SetKey(service.SettingKeyDesktopAgentConfig).SetValue(value).SetUpdatedAt(time.Now()).OnConflictColumns(setting.FieldKey).UpdateNewValues().Exec(ctx); err != nil {
		return false, err
	}
	if err = tx.Commit(); err != nil {
		return false, err
	}
	return true, nil
}
