//go:build unit

package service

import (
	"context"
	"errors"
	"fmt"
	"github.com/Wei-Shaw/sub2api/internal/config"
	"github.com/stretchr/testify/require"
	"testing"
)

type desktopManagedRepoStub struct {
	APIKeyRepository
	key         *APIKey
	changed     bool
	err         error
	metadataErr error
	calls       int
	updates     int
	deletes     int
}

func (r *desktopManagedRepoStub) EnsureDesktopAgentKey(context.Context, *APIKey) (*APIKey, bool, error) {
	r.calls++
	return r.key, r.changed, r.err
}
func (r *desktopManagedRepoStub) GetByID(context.Context, int64) (*APIKey, error) { return r.key, nil }
func (r *desktopManagedRepoStub) GetKeyAndOwnerID(context.Context, int64) (string, int64, error) {
	return r.key.Key, r.key.UserID, nil
}
func (r *desktopManagedRepoStub) IsSystemKey(context.Context, int64) (bool, error) {
	return r.key.KeyType == APIKeyTypeSystem, r.metadataErr
}
func (r *desktopManagedRepoStub) Update(context.Context, *APIKey, APIKeyUpdateFields) error {
	r.updates++
	return nil
}
func (r *desktopManagedRepoStub) DeleteWithAudit(context.Context, int64) error {
	r.deletes++
	return nil
}

type desktopManagedUsers struct {
	UserRepository
	user *User
}

func (r *desktopManagedUsers) GetByID(context.Context, int64) (*User, error) { return r.user, nil }

type desktopManagedGroups struct {
	GroupRepository
	group *Group
}

func (r *desktopManagedGroups) GetByID(context.Context, int64) (*Group, error) { return r.group, nil }

type desktopManagedCache struct {
	APIKeyCache
	deleted, published []string
}

func (c *desktopManagedCache) DeleteAuthCache(_ context.Context, k string) error {
	c.deleted = append(c.deleted, k)
	return nil
}
func (c *desktopManagedCache) PublishAuthCacheInvalidation(_ context.Context, k string) error {
	c.published = append(c.published, k)
	return nil
}
func desktopManagedFixture() (*APIKeyService, *desktopManagedRepoStub, *desktopManagedUsers, *desktopManagedGroups, *desktopManagedCache) {
	gid := int64(12)
	repo := &desktopManagedRepoStub{key: &APIKey{ID: 99, UserID: 7, Key: "fixture-private-agent", KeyType: APIKeyTypeSystem, Purpose: APIKeyPurposeDesktopAgent, GroupID: &gid, Status: StatusActive}}
	users := &desktopManagedUsers{user: &User{ID: 7, Status: StatusActive}}
	groups := &desktopManagedGroups{group: &Group{ID: 12, Platform: PlatformComposite, SubscriptionType: SubscriptionTypeStandard, Status: StatusActive}}
	cache := &desktopManagedCache{}
	svc := NewAPIKeyService(repo, users, groups, nil, nil, cache, &config.Config{})
	return svc, repo, users, groups, cache
}
func TestDesktopManagedProviderInvalidatesOnlyAfterSuccessfulCommit(t *testing.T) {
	for _, tc := range []struct {
		name        string
		changed     bool
		err         error
		invalidates bool
	}{{"created", true, nil, true}, {"group synced", true, nil, true}, {"unchanged", false, nil, false}, {"rollback", true, errors.New("commit failed"), false}} {
		t.Run(tc.name, func(t *testing.T) {
			svc, repo, _, _, cache := desktopManagedFixture()
			repo.changed = tc.changed
			repo.err = tc.err
			key, scope, err := svc.GetDesktopAgentCredential(context.Background(), 7, 12)
			if tc.err != nil {
				require.Error(t, err)
				require.Nil(t, key)
			} else {
				require.NoError(t, err)
				require.Equal(t, APIKeyPurposeDesktopAgent, scope)
			}
			if tc.invalidates {
				require.Equal(t, []string{svc.authCacheKey(repo.key.Key)}, cache.deleted)
				require.Equal(t, cache.deleted, cache.published)
			} else {
				require.Empty(t, cache.deleted)
				require.Empty(t, cache.published)
			}
		})
	}
}
func TestDesktopManagedProviderPermissionFailuresNeverReachEnsure(t *testing.T) {
	for _, kind := range []string{"inactive user", "wrong owner", "exclusive group", "restricted public", "subscription", "inactive group", "unsupported platform"} {
		t.Run(kind, func(t *testing.T) {
			svc, repo, users, groups, _ := desktopManagedFixture()
			switch kind {
			case "inactive user":
				users.user.Status = StatusDisabled
			case "wrong owner":
				users.user.ID = 8
			case "exclusive group":
				groups.group.IsExclusive = true
			case "restricted public":
				users.user.RestrictPublicGroups = true
			case "subscription":
				groups.group.SubscriptionType = SubscriptionTypeSubscription
			case "inactive group":
				groups.group.Status = StatusDisabled
			case "unsupported platform":
				groups.group.Platform = PlatformAnthropic
			}
			_, _, err := svc.GetDesktopAgentCredential(context.Background(), 7, 12)
			require.Error(t, err)
			require.Zero(t, repo.calls)
		})
	}
}
func TestDesktopManagedPublicEditsAndDeletesCannotReviveOrRecreate(t *testing.T) {
	svc, repo, _, _, _ := desktopManagedFixture()
	name := "renamed"
	status := StatusActive
	zero := float64(0)
	reset := true
	for _, req := range []UpdateAPIKeyRequest{{Name: &name}, {Status: &status}, {Quota: &zero}, {ResetQuota: &reset}, {ClearExpiration: true}} {
		_, err := svc.Update(context.Background(), 99, 7, req)
		require.ErrorIs(t, err, ErrSystemKeyProtected)
	}
	require.ErrorIs(t, svc.Delete(context.Background(), 99, 7), ErrSystemKeyProtected)
	require.Zero(t, repo.updates)
	require.Zero(t, repo.deletes)
	admin := &adminServiceImpl{apiKeyRepo: repo}
	gid := int64(13)
	_, err := admin.AdminUpdateAPIKeyGroupID(context.Background(), 99, &gid)
	require.ErrorIs(t, err, ErrSystemKeyProtected)
}
func TestDesktopManagedMetadataFailureBlocksGenericMutation(t *testing.T) {
	svc, repo, _, _, _ := desktopManagedFixture()
	repo.key.Purpose = ""
	repo.key.KeyType = APIKeyTypeUser
	repo.metadataErr = errors.New("database unavailable")
	name := "rename"
	_, err := svc.Update(context.Background(), 99, 7, UpdateAPIKeyRequest{Name: &name})
	require.Error(t, err)
	require.Error(t, svc.Delete(context.Background(), 99, 7))
	require.Zero(t, repo.updates)
	require.Zero(t, repo.deletes)
}

func TestDesktopManagedProviderClearsLocalPositiveAndNegativeAuthenticationCaches(t *testing.T) {
	for _, negative := range []bool{false, true} {
		t.Run(fmt.Sprint(negative), func(t *testing.T) {
			_, repo, users, groups, cache := desktopManagedFixture()
			repo.changed = true
			cfg := &config.Config{APIKeyAuth: config.APIKeyAuthCacheConfig{L1Size: 100, L1TTLSeconds: 60, NegativeTTLSeconds: 30}}
			svc := NewAPIKeyService(repo, users, groups, nil, nil, cache, cfg)
			defer svc.authCacheL1.Close()
			defer svc.authNegativeCacheL1.Close()
			cacheKey := svc.authCacheKey(repo.key.Key)
			entry := &APIKeyAuthCacheEntry{NotFound: negative, Snapshot: &APIKeyAuthSnapshot{APIKeyID: repo.key.ID}}
			svc.setAuthCacheL1(cacheKey, entry)
			svc.authCacheL1.Wait()
			svc.authNegativeCacheL1.Wait()
			_, ok := svc.getAuthCacheEntry(context.Background(), cacheKey)
			require.True(t, ok)
			_, _, err := svc.GetDesktopAgentCredential(context.Background(), 7, 12)
			require.NoError(t, err)
			_, ok = svc.authCacheL1.Get(cacheKey)
			require.False(t, ok)
			_, ok = svc.authNegativeCacheL1.Get(cacheKey)
			require.False(t, ok)
		})
	}
}
