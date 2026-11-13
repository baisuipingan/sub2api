//go:build unit

package service

import (
	"context"
	"errors"
	"testing"

	"github.com/stretchr/testify/require"
)

type desktopLifecycleRepo struct {
	*desktopManagedRepoStub
	oldKey, newKey string
	err            error
	calls          int
	userID, keyID  int64
	replacement    string
	caller         DesktopAgentCredentialCaller
}

func (r *desktopLifecycleRepo) MutateDesktopAgentCredential(_ context.Context, caller DesktopAgentCredentialCaller, userID, keyID int64, replacement string) (string, string, error) {
	r.calls++
	r.caller = caller
	r.userID = userID
	r.keyID = keyID
	r.replacement = replacement
	if replacement != "" {
		r.newKey = replacement
	}
	return r.oldKey, r.newKey, r.err
}

func TestDesktopManagedCredentialMutationInvalidatesOnlyCommittedSecrets(t *testing.T) {
	for _, reset := range []bool{false, true} {
		for _, fail := range []bool{false, true} {
			svc, base, _, _, cache := desktopManagedFixture()
			repo := &desktopLifecycleRepo{desktopManagedRepoStub: base, oldKey: "old-secret"}
			if fail {
				repo.err = errors.New("transaction failed")
			}
			svc.apiKeyRepo = repo
			ctx, cancel := context.WithCancel(context.Background())
			cancel()
			result, err := svc.ManageDesktopAgentCredential(ctx, DesktopAgentCredentialCaller{UserID: 7}, 7, 99, reset)
			if fail {
				require.Error(t, err)
				require.Nil(t, result)
				require.Empty(t, cache.deleted)
			} else {
				require.NoError(t, err)
				require.Equal(t, int64(99), result.KeyID)
				require.Contains(t, cache.deleted, svc.authCacheKey("old-secret"))
				if reset {
					require.Equal(t, "active", result.Status)
					require.NotEmpty(t, repo.replacement)
					require.Contains(t, cache.deleted, svc.authCacheKey(repo.replacement))
				} else {
					require.Equal(t, "revoked", result.Status)
					require.Empty(t, repo.replacement)
				}
				require.Equal(t, cache.deleted, cache.published)
			}
			require.Equal(t, DesktopAgentCredentialCaller{UserID: 7}, repo.caller)
			require.Equal(t, int64(7), repo.userID)
			require.Equal(t, int64(99), repo.keyID)
		}
	}
}

func TestDesktopManagedCredentialActionStrictBody(t *testing.T) {
	for _, raw := range []string{`{}`, `{"key_id":0}`, `{"key_id":null}`, `{"key_id":1,"user_id":9}`, `{"key_id":1,"administrator":true}`, `{"key_id":1,"step_up_authorized":true}`, `{"key_id":1,"key_id":2}`, `{"key_id":"1"}`, `{"key_id":1}{}`, `[]`} {
		_, err := ParseDesktopAgentCredentialAction([]byte(raw))
		require.ErrorIs(t, err, ErrDesktopAgentInvalid, raw)
	}
	id, err := ParseDesktopAgentCredentialAction([]byte(`{"key_id":99}`))
	require.NoError(t, err)
	require.Equal(t, int64(99), id)
}

func TestDesktopManagedCredentialRejectsUntrustedCaller(t *testing.T) {
	for _, caller := range []DesktopAgentCredentialCaller{
		{}, {UserID: 8}, {UserID: 8, Administrator: true}, {UserID: 7, Administrator: true},
	} {
		svc, base, _, _, cache := desktopManagedFixture()
		repo := &desktopLifecycleRepo{desktopManagedRepoStub: base, oldKey: "old-secret"}
		svc.apiKeyRepo = repo
		result, err := svc.ManageDesktopAgentCredential(context.Background(), caller, 7, 99, true)
		require.ErrorIs(t, err, ErrInsufficientPerms)
		require.Nil(t, result)
		require.Zero(t, repo.calls)
		require.Empty(t, cache.deleted)
	}
}

func TestDesktopManagedCredentialAdminAuthorizationAndRestoreDenialArePreserved(t *testing.T) {
	svc, base, _, _, cache := desktopManagedFixture()
	repo := &desktopLifecycleRepo{desktopManagedRepoStub: base, oldKey: "old-secret"}
	svc.apiKeyRepo = repo
	caller := DesktopAgentCredentialCaller{UserID: 8, Administrator: true, StepUpAuthorized: true}
	result, err := svc.ManageDesktopAgentCredential(context.Background(), caller, 7, 99, true)
	require.NoError(t, err)
	require.Equal(t, "active", result.Status)
	require.Equal(t, caller, repo.caller)
	require.NotEmpty(t, cache.deleted)
	cache.deleted, cache.published = nil, nil
	repo.err = ErrDesktopAgentRestoreAdminRequired
	result, err = svc.ManageDesktopAgentCredential(context.Background(), DesktopAgentCredentialCaller{UserID: 7}, 7, 99, true)
	require.ErrorIs(t, err, ErrDesktopAgentRestoreAdminRequired)
	require.Nil(t, result)
	require.Empty(t, cache.deleted)
	require.Empty(t, cache.published)
}
