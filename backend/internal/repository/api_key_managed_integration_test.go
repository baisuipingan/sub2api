//go:build integration

package repository

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"sync"
	"testing"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/config"
	"github.com/Wei-Shaw/sub2api/internal/handler/dto"
	"github.com/Wei-Shaw/sub2api/internal/pkg/pagination"
	"github.com/Wei-Shaw/sub2api/internal/service"
	dbmigrations "github.com/Wei-Shaw/sub2api/migrations"
	"github.com/redis/go-redis/v9"
	"github.com/stretchr/testify/require"
)

type desktopDBFixture struct {
	repo                          *apiKeyRepository
	userID, groupID, otherGroupID int64
}

func newDesktopDBFixture(t *testing.T) *desktopDBFixture {
	t.Helper()
	ctx := context.Background()
	client := testEntClient(t)
	u, err := client.User.Create().SetEmail(fmt.Sprintf("desktop-%d@test.invalid", time.Now().UnixNano())).SetPasswordHash("fixture").SetRole(service.RoleUser).SetStatus(service.StatusActive).Save(ctx)
	require.NoError(t, err)
	g, err := client.Group.Create().SetName(fmt.Sprintf("desktop-%d", u.ID)).SetPlatform(service.PlatformComposite).SetSubscriptionType(service.SubscriptionTypeStandard).SetStatus(service.StatusActive).Save(ctx)
	require.NoError(t, err)
	other, err := client.Group.Create().SetName(fmt.Sprintf("desktop-other-%d", u.ID)).SetPlatform(service.PlatformOpenAI).SetSubscriptionType(service.SubscriptionTypeStandard).SetStatus(service.StatusActive).Save(ctx)
	require.NoError(t, err)
	f := &desktopDBFixture{repo: newAPIKeyRepositoryWithSQL(client, integrationDB), userID: u.ID, groupID: g.ID, otherGroupID: other.ID}
	f.configure(t, g.ID, true)
	t.Cleanup(func() {
		_, _ = integrationDB.ExecContext(ctx, `DELETE FROM api_keys WHERE user_id=$1`, u.ID)
		_, _ = integrationDB.ExecContext(ctx, `DELETE FROM users WHERE id=$1`, u.ID)
		_, _ = integrationDB.ExecContext(ctx, `DELETE FROM groups WHERE id=ANY($1)`, fmt.Sprintf("{%d,%d}", g.ID, other.ID))
		_, _ = integrationDB.ExecContext(ctx, `DELETE FROM settings WHERE key=$1`, service.SettingKeyDesktopAgentConfig)
	})
	return f
}
func (f *desktopDBFixture) configure(t *testing.T, gid int64, enabled bool) {
	t.Helper()
	raw, _ := json.Marshal(map[string]any{"enabled": enabled, "group_id": gid})
	_, err := integrationDB.ExecContext(context.Background(), `INSERT INTO settings(key,value)VALUES($1,$2)ON CONFLICT(key)DO UPDATE SET value=EXCLUDED.value`, service.SettingKeyDesktopAgentConfig, string(raw))
	require.NoError(t, err)
}
func (f *desktopDBFixture) candidate(suffix string) *service.APIKey {
	return &service.APIKey{UserID: f.userID, GroupID: &f.groupID, Key: fmt.Sprintf("fixture-agent-%d-%s", f.userID, suffix), Name: "Desktop Agent (managed)"}
}
func TestDesktopAgentManagedKeyConcurrentEnsureAndMetadata(t *testing.T) {
	f := newDesktopDBFixture(t)
	ctx := context.Background()
	// Existing image key retains exact secret/name/group/purpose across migration and Agent ensure.
	image := &service.APIKey{UserID: f.userID, Key: fmt.Sprintf("fixture-image-%d", f.userID), Name: service.ImageStudioKeyName, GroupID: &f.groupID, Status: service.StatusActive}
	require.NoError(t, f.repo.CreateSystemKey(ctx, image))
	migration, err := dbmigrations.FS.ReadFile("256_desktop_agent_key_purpose.sql")
	require.NoError(t, err)
	_, err = integrationDB.ExecContext(ctx, string(migration))
	require.NoError(t, err)
	const n = 32
	results := make([]*service.APIKey, n)
	errs := make([]error, n)
	var wg sync.WaitGroup
	for i := range n {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			results[i], _, errs[i] = f.repo.EnsureDesktopAgentKey(ctx, f.candidate(fmt.Sprint(i)))
		}(i)
	}
	wg.Wait()
	for i := range n {
		require.NoError(t, errs[i])
		require.Equal(t, results[0].ID, results[i].ID)
		require.Equal(t, results[0].Key, results[i].Key)
	}
	agent := results[0]
	var count int
	require.NoError(t, integrationDB.QueryRowContext(ctx, `SELECT COUNT(*) FROM api_keys WHERE user_id=$1 AND purpose='desktop_agent'`, f.userID).Scan(&count))
	require.Equal(t, 1, count)
	_, err = integrationDB.ExecContext(ctx, `UPDATE api_keys SET name='renamed for display' WHERE id=$1`, agent.ID)
	require.NoError(t, err)
	again, changed, err := f.repo.EnsureDesktopAgentKey(ctx, f.candidate("renamed"))
	require.NoError(t, err)
	require.False(t, changed)
	require.Equal(t, agent.ID, again.ID)
	require.Equal(t, agent.Key, again.Key)
	// Database index enforces purpose independently of mutable names.
	_, err = integrationDB.ExecContext(ctx, `INSERT INTO api_keys(user_id,key,name,key_type,purpose)VALUES($1,$2,'different name','system','desktop_agent')`, f.userID, fmt.Sprintf("duplicate-%d", f.userID))
	require.Error(t, err)
	gotImage, err := f.repo.GetByID(ctx, image.ID)
	require.NoError(t, err)
	require.Empty(t, gotImage.Purpose)
	require.Equal(t, image.Key, gotImage.Key)
	require.Equal(t, image.Name, gotImage.Name)
	require.Equal(t, image.GroupID, gotImage.GroupID)
	detail, err := f.repo.GetByID(ctx, agent.ID)
	require.NoError(t, err)
	require.Equal(t, agent.Key, dto.APIKeyFromService(detail).Key)
	require.True(t, dto.APIKeyFromService(detail).IsSystem)
	svc := service.NewAPIKeyService(f.repo, nil, nil, nil, nil, nil, nil)
	for _, sort := range []string{"id", "group", "current_concurrency"} {
		keys, _, err := svc.List(ctx, f.userID, pagination.PaginationParams{Page: 1, PageSize: 50, SortBy: sort}, service.APIKeyListFilters{})
		require.NoError(t, err)
		require.Len(t, keys, 2)
		for i := range keys {
			if keys[i].ID == agent.ID {
				require.Equal(t, service.APIKeyPurposeDesktopAgent, keys[i].Purpose)
				require.Equal(t, agent.Key, dto.APIKeyFromService(&keys[i]).Key)
				require.True(t, dto.APIKeyFromService(&keys[i]).IsSystem)
			}
		}
	}
	usageRepo := newUsageLogRepositoryWithSQL(testEntClient(t), integrationDB)
	nested, err := usageRepo.loadAPIKeys(ctx, []int64{agent.ID, image.ID})
	require.NoError(t, err)
	require.Equal(t, agent.Key, dto.UsageLogFromService(&service.UsageLog{APIKey: nested[agent.ID]}).APIKey.Key)
	require.True(t, dto.UsageLogFromService(&service.UsageLog{APIKey: nested[agent.ID]}).APIKey.IsSystem)
	require.Equal(t, image.Key, dto.APIKeyFromService(nested[image.ID]).Key)
}
func TestDesktopAgentManagedKeyBlockedRowsNeverRecreatedOrReset(t *testing.T) {
	for _, kind := range []string{"disabled", "expired timestamp", "expired status", "quota counter", "quota status"} {
		t.Run(kind, func(t *testing.T) {
			f := newDesktopDBFixture(t)
			ctx := context.Background()
			key, _, err := f.repo.EnsureDesktopAgentKey(ctx, f.candidate("first"))
			require.NoError(t, err)
			var query string
			switch kind {
			case "disabled":
				query = `UPDATE api_keys SET status='disabled',quota=20,quota_used=2 WHERE id=$1`
			case "expired timestamp":
				query = `UPDATE api_keys SET expires_at=NOW()-INTERVAL '1 day',quota=20,quota_used=2 WHERE id=$1`
			case "expired status":
				query = `UPDATE api_keys SET status='expired',quota=20,quota_used=2 WHERE id=$1`
			case "quota counter":
				query = `UPDATE api_keys SET quota=2,quota_used=2 WHERE id=$1`
			case "quota status":
				query = `UPDATE api_keys SET status='quota_exhausted',quota=20,quota_used=2 WHERE id=$1`
			}
			_, err = integrationDB.ExecContext(ctx, query, key.ID)
			require.NoError(t, err)
			before, err := f.repo.GetByID(ctx, key.ID)
			require.NoError(t, err)
			f.configure(t, f.otherGroupID, true)
			candidate := f.candidate("second")
			candidate.GroupID = &f.otherGroupID
			again, changed, err := f.repo.EnsureDesktopAgentKey(ctx, candidate)
			require.NoError(t, err)
			require.False(t, changed)
			require.Equal(t, key.ID, again.ID)
			require.Equal(t, key.Key, again.Key)
			require.Equal(t, before.Status, again.Status)
			require.Equal(t, before.Quota, again.Quota)
			require.Equal(t, before.QuotaUsed, again.QuotaUsed)
			require.Equal(t, before.ExpiresAt, again.ExpiresAt)
			require.Equal(t, before.GroupID, again.GroupID)
		})
	}
}
func TestDesktopAgentManagedKeyGroupSyncAndStalePolicy(t *testing.T) {
	f := newDesktopDBFixture(t)
	ctx := context.Background()
	key, _, err := f.repo.EnsureDesktopAgentKey(ctx, f.candidate("first"))
	require.NoError(t, err)
	f.configure(t, f.otherGroupID, true)
	_, _, err = f.repo.EnsureDesktopAgentKey(ctx, f.candidate("stale"))
	require.ErrorIs(t, err, service.ErrDesktopAgentPolicyChanged)
	candidate := f.candidate("new group")
	candidate.GroupID = &f.otherGroupID
	again, changed, err := f.repo.EnsureDesktopAgentKey(ctx, candidate)
	require.NoError(t, err)
	require.True(t, changed)
	require.Equal(t, key.ID, again.ID)
	require.Equal(t, key.Key, again.Key)
	require.Equal(t, f.otherGroupID, *again.GroupID)
	f.configure(t, f.otherGroupID, false)
	_, _, err = f.repo.EnsureDesktopAgentKey(ctx, candidate)
	require.ErrorIs(t, err, service.ErrDesktopAgentPolicyChanged)
}
func TestDesktopAgentManagedKeyUnauthorizedOrReservedNameNeverCreates(t *testing.T) {
	for _, kind := range []string{"restricted user", "exclusive group", "disabled user", "disabled group", "subscription group", "unsupported platform", "reserved name"} {
		t.Run(kind, func(t *testing.T) {
			f := newDesktopDBFixture(t)
			ctx := context.Background()
			var err error
			switch kind {
			case "restricted user":
				_, err = integrationDB.ExecContext(ctx, `UPDATE users SET restrict_public_groups=true WHERE id=$1`, f.userID)
			case "exclusive group":
				_, err = integrationDB.ExecContext(ctx, `UPDATE groups SET is_exclusive=true WHERE id=$1`, f.groupID)
			case "disabled user":
				_, err = integrationDB.ExecContext(ctx, `UPDATE users SET status='disabled' WHERE id=$1`, f.userID)
			case "disabled group":
				_, err = integrationDB.ExecContext(ctx, `UPDATE groups SET status='disabled' WHERE id=$1`, f.groupID)
			case "subscription group":
				_, err = integrationDB.ExecContext(ctx, `UPDATE groups SET subscription_type='subscription' WHERE id=$1`, f.groupID)
			case "unsupported platform":
				_, err = integrationDB.ExecContext(ctx, `UPDATE groups SET platform='anthropic' WHERE id=$1`, f.groupID)
			case "reserved name":
				err = f.repo.CreateSystemKey(ctx, &service.APIKey{UserID: f.userID, Key: fmt.Sprintf("legacy-collision-%d", f.userID), Name: "Desktop Agent (managed)", GroupID: &f.groupID, Status: service.StatusActive})
			}
			require.NoError(t, err)
			_, _, err = f.repo.EnsureDesktopAgentKey(ctx, f.candidate("denied"))
			require.Error(t, err)
			var count int
			require.NoError(t, integrationDB.QueryRowContext(ctx, `SELECT COUNT(*) FROM api_keys WHERE user_id=$1 AND purpose='desktop_agent'`, f.userID).Scan(&count))
			require.Zero(t, count)
		})
	}
}

func TestDesktopAgentRealProviderBootstrapUsesIndependentPurpose(t *testing.T) {
	f := newDesktopDBFixture(t)
	ctx := context.Background()
	client := testEntClient(t)
	users := NewUserRepository(client, integrationDB)
	groups := NewGroupRepository(client, integrationDB)
	keys := service.NewAPIKeyService(f.repo, users, groups, nil, nil, nil, &config.Config{})
	accounts := newAccountRepositoryWithSQL(client, integrationDB, nil)
	account := &service.Account{
		Name: fmt.Sprintf("desktop-bootstrap-%d", f.userID), Platform: service.PlatformOpenAI,
		Type: service.AccountTypeAPIKey, Status: service.StatusActive, Schedulable: true,
		Credentials: map[string]any{"model_mapping": map[string]any{"fixture-model": "fixture-upstream"}}, Extra: map[string]any{},
	}
	require.NoError(t, accounts.CreateWithAccountGroups(ctx, account, []service.AccountGroup{{GroupID: f.groupID}}))
	t.Cleanup(func() {
		_, _ = integrationDB.ExecContext(ctx, "DELETE FROM scheduler_outbox WHERE account_id=$1", account.ID)
		_, _ = integrationDB.ExecContext(ctx, "DELETE FROM account_groups WHERE account_id=$1", account.ID)
		_, _ = integrationDB.ExecContext(ctx, "DELETE FROM accounts WHERE id=$1", account.ID)
	})
	catalog := service.NewDesktopAgentModelCatalog(accounts, NewCompositeModelRouteRepository(client))
	svc := service.NewDesktopAgentService(service.NewSettingService(NewSettingRepository(client), nil), users, groups, keys, catalog)
	boolp := func(v bool) *bool { return &v }
	cfg := service.DesktopAgentConfig{Enabled: true, GroupID: f.groupID, BaseURL: "https://gateway.example.test/v1", Model: &service.DesktopAgentModel{
		ID: "fixture-model", DisplayName: "Fixture", ContextWindow: 100000, MaxOutputTokens: 20000, DefaultMaxOutputTokens: 10000, ReasoningLevels: []string{"none"}, DefaultReasoning: "none", SupportsReasoning: boolp(false), SupportsStreaming: boolp(true), RequiresMFJSToolSchema: boolp(false),
		Input: &service.DesktopAgentInput{Text: boolp(true), Image: boolp(false), Video: boolp(false), Audio: boolp(false), PDF: boolp(false)}, Output: &service.DesktopAgentOutput{Text: boolp(true)}, SupportsToolCall: boolp(true), SupportsJSONSchemaOutput: boolp(false), SupportsNativeWebSearch: boolp(false), SupportsMidConversationSystem: boolp(true)}}
	_, err := svc.UpdateConfig(ctx, cfg)
	// This synthetic public alias has no official capability identity. Saving
	// it must fail until the administrator explicitly binds its test policy.
	require.ErrorIs(t, err, service.ErrDesktopAgentInvalid)
	var keyCount int
	require.NoError(t, integrationDB.QueryRowContext(ctx, `SELECT COUNT(*) FROM api_keys WHERE user_id=$1 AND purpose='desktop_agent'`, f.userID).Scan(&keyCount))
	require.Zero(t, keyCount)
	// Keep the fixture's non-reasoning policy, rather than silently adopting
	// official capabilities or weakening the provider/purpose assertions below.
	cfg.ModelProfiles = map[string]service.DesktopAgentModelProfileBinding{
		"fixture-model": {ProfileID: "gpt-6.1-sol", Override: true},
	}
	_, err = svc.UpdateConfig(ctx, cfg)
	require.NoError(t, err)
	first, err := svc.Bootstrap(ctx, f.userID)
	require.NoError(t, err)
	require.Equal(t, "ready", first.ServiceStatus)
	require.NotNil(t, first.Credential)
	require.Equal(t, service.APIKeyPurposeDesktopAgent, first.Credential.Scope)
	again, err := svc.Bootstrap(ctx, f.userID)
	require.NoError(t, err)
	require.Equal(t, first.Credential, again.Credential)
	_, err = integrationDB.ExecContext(ctx, `UPDATE api_keys SET status='disabled' WHERE user_id=$1 AND purpose='desktop_agent'`, f.userID)
	require.NoError(t, err)
	blocked, err := svc.Bootstrap(ctx, f.userID)
	require.NoError(t, err)
	require.Equal(t, "credential_disabled", blocked.ReasonCode)
	require.Nil(t, blocked.Credential)
}

func TestDesktopAgentManagedKeyAllowedGroupThenRevoked(t *testing.T) {
	for _, restricted := range []bool{false, true} {
		t.Run(fmt.Sprint(restricted), func(t *testing.T) {
			f := newDesktopDBFixture(t)
			ctx := context.Background()
			_, err := integrationDB.ExecContext(ctx, `UPDATE users SET restrict_public_groups=$2 WHERE id=$1`, f.userID, restricted)
			require.NoError(t, err)
			_, err = integrationDB.ExecContext(ctx, `UPDATE groups SET is_exclusive=$2 WHERE id=$1`, f.groupID, !restricted)
			require.NoError(t, err)
			users := NewUserRepository(testEntClient(t), integrationDB)
			require.NoError(t, users.AddGroupToAllowedGroups(ctx, f.userID, f.groupID))
			key, _, err := f.repo.EnsureDesktopAgentKey(ctx, f.candidate("allowed"))
			require.NoError(t, err)
			require.NotNil(t, key)
			require.NoError(t, users.RemoveGroupFromUserAllowedGroups(ctx, f.userID, f.groupID))
			_, _, err = f.repo.EnsureDesktopAgentKey(ctx, f.candidate("revoked"))
			require.ErrorIs(t, err, service.ErrGroupNotAllowed)
		})
	}
}
func TestDesktopAgentManagedKeyExcludedFromBulkGroupReplacement(t *testing.T) {
	f := newDesktopDBFixture(t)
	ctx := context.Background()
	agent, _, err := f.repo.EnsureDesktopAgentKey(ctx, f.candidate("agent"))
	require.NoError(t, err)
	image := &service.APIKey{UserID: f.userID, Key: fmt.Sprintf("bulk-image-%d", f.userID), Name: service.ImageStudioKeyName, GroupID: &f.groupID, Status: service.StatusActive}
	require.NoError(t, f.repo.CreateSystemKey(ctx, image))
	user := &service.APIKey{UserID: f.userID, Key: fmt.Sprintf("bulk-user-%d", f.userID), Name: "user key", GroupID: &f.groupID, Status: service.StatusActive}
	require.NoError(t, f.repo.Create(ctx, user))
	n, err := f.repo.UpdateGroupIDByUserAndGroup(ctx, f.userID, f.groupID, f.otherGroupID)
	require.NoError(t, err)
	require.EqualValues(t, 2, n)
	for _, expected := range []struct{ id, group int64 }{{agent.ID, f.groupID}, {image.ID, f.otherGroupID}, {user.ID, f.otherGroupID}} {
		key, err := f.repo.GetByID(ctx, expected.id)
		require.NoError(t, err)
		require.Equal(t, expected.group, *key.GroupID)
	}
}
func TestMigration256PreservesLegacySystemAndUserKeyRows(t *testing.T) {
	tx := testEntTx(t)
	ctx := context.Background()
	client := tx.Client()
	u, err := client.User.Create().SetEmail(fmt.Sprintf("migration256-%d@test.invalid", time.Now().UnixNano())).SetPasswordHash("fixture").Save(ctx)
	require.NoError(t, err)
	_, err = tx.ExecContext(ctx, `ALTER TABLE api_keys DROP COLUMN purpose CASCADE`)
	require.NoError(t, err)
	_, err = tx.ExecContext(ctx, `INSERT INTO api_keys(user_id,key,name,key_type,status,quota,quota_used,expires_at)VALUES($1,$2,'legacy image','system','disabled',8,3,NOW()+INTERVAL '1 day'),($1,$3,'legacy user','user','active',5,2,NULL)`, u.ID, fmt.Sprintf("migration-image-%d", u.ID), fmt.Sprintf("migration-user-%d", u.ID))
	require.NoError(t, err)
	var before, after string
	query := `SELECT jsonb_agg(to_jsonb(k)-'purpose' ORDER BY id)::text FROM api_keys k WHERE user_id=$1`
	require.NoError(t, scanSingleRow(ctx, tx, query, []any{u.ID}, &before))
	migration, err := dbmigrations.FS.ReadFile("256_desktop_agent_key_purpose.sql")
	require.NoError(t, err)
	_, err = tx.ExecContext(ctx, string(migration))
	require.NoError(t, err)
	require.NoError(t, scanSingleRow(ctx, tx, query, []any{u.ID}, &after))
	require.JSONEq(t, before, after)
	var filled int
	require.NoError(t, scanSingleRow(ctx, tx, `SELECT COUNT(*) FROM api_keys WHERE user_id=$1 AND purpose IS NOT NULL`, []any{u.ID}, &filled))
	require.Zero(t, filled)
}

func TestDesktopAgentManagedKeyGroupSyncInvalidatesRealRedisAuthCache(t *testing.T) {
	f := newDesktopDBFixture(t)
	ctx := context.Background()
	client := testEntClient(t)
	rdb := testRedis(t)
	cache := NewAPIKeyCache(rdb)
	users := NewUserRepository(client, integrationDB)
	groups := NewGroupRepository(client, integrationDB)
	svc := service.NewAPIKeyService(f.repo, users, groups, nil, nil, cache, &config.Config{APIKeyAuth: config.APIKeyAuthCacheConfig{L2TTLSeconds: 60}})
	key, _, err := svc.GetDesktopAgentCredential(ctx, f.userID, f.groupID)
	require.NoError(t, err)
	auth, err := svc.GetByKey(ctx, key.Key)
	require.NoError(t, err)
	require.Equal(t, f.groupID, *auth.GroupID)
	hash := sha256.Sum256([]byte(key.Key))
	cacheID := hex.EncodeToString(hash[:])
	cached, err := cache.GetAuthCache(ctx, cacheID)
	require.NoError(t, err)
	require.Equal(t, f.groupID, *cached.Snapshot.GroupID)
	subscription := rdb.Subscribe(ctx, authCacheInvalidateChannel)
	defer subscription.Close()
	_, err = subscription.Receive(ctx)
	require.NoError(t, err)
	f.configure(t, f.otherGroupID, true)
	again, _, err := svc.GetDesktopAgentCredential(ctx, f.userID, f.otherGroupID)
	require.NoError(t, err)
	require.Equal(t, key.ID, again.ID)
	_, err = cache.GetAuthCache(ctx, cacheID)
	require.ErrorIs(t, err, redis.Nil)
	receiveCtx, cancel := context.WithTimeout(ctx, 3*time.Second)
	defer cancel()
	message, err := subscription.ReceiveMessage(receiveCtx)
	require.NoError(t, err)
	require.Equal(t, cacheID, message.Payload)
	auth, err = svc.GetByKey(ctx, key.Key)
	require.NoError(t, err)
	require.Equal(t, f.otherGroupID, *auth.GroupID)
}
