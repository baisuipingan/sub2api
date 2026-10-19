//go:build unit

package service

import (
	"context"
	"errors"
	"strconv"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

type updateServiceCacheStub struct {
	data string
}

func (s *updateServiceCacheStub) GetUpdateInfo(context.Context) (string, error) {
	if s.data == "" {
		return "", errors.New("cache miss")
	}
	return s.data, nil
}

func (s *updateServiceCacheStub) SetUpdateInfo(_ context.Context, data string, _ time.Duration) error {
	s.data = data
	return nil
}

type updateServiceGitHubClientStub struct {
	release        *GitHubRelease
	latestErr      error
	recentReleases []*GitHubRelease
	recentErr      error
	latestCalls    int
	recentCalls    int
}

func (s *updateServiceGitHubClientStub) FetchLatestRelease(context.Context, string) (*GitHubRelease, error) {
	s.latestCalls++
	return s.release, s.latestErr
}

func (s *updateServiceGitHubClientStub) FetchRecentReleases(context.Context, string, int) ([]*GitHubRelease, error) {
	s.recentCalls++
	return s.recentReleases, s.recentErr
}

func (s *updateServiceGitHubClientStub) DownloadFile(context.Context, string, string, int64) error {
	panic("DownloadFile should not be called when no update is available")
}

func (s *updateServiceGitHubClientStub) FetchChecksumFile(context.Context, string) ([]byte, error) {
	panic("FetchChecksumFile should not be called when no update is available")
}

func TestUpdateServicePerformUpdateNoUpdateReturnsSentinel(t *testing.T) {
	svc := NewUpdateService(
		&updateServiceCacheStub{},
		&updateServiceGitHubClientStub{
			release: &GitHubRelease{
				TagName: "v0.1.132",
				Name:    "v0.1.132",
			},
		},
		"0.1.132",
		"release",
	)

	err := svc.PerformUpdate(context.Background())

	require.Error(t, err)
	require.True(t, errors.Is(err, ErrNoUpdateAvailable))
	require.ErrorIs(t, err, ErrNoUpdateAvailable)
}

func newRollbackTestService(current string, releases []*GitHubRelease) *UpdateService {
	return NewUpdateService(
		&updateServiceCacheStub{},
		&updateServiceGitHubClientStub{recentReleases: releases},
		current,
		"release",
	)
}

func TestUpdateServiceListRollbackVersionsFiltersAndCaps(t *testing.T) {
	releases := []*GitHubRelease{
		{TagName: "v0.1.148", PublishedAt: "2026-07-09T00:00:00Z"},                       // newer than current: excluded
		{TagName: "v0.1.147", PublishedAt: "2026-07-08T00:00:00Z"},                       // current: excluded
		{TagName: "v0.1.146-rc1", PublishedAt: "2026-07-07T12:00:00Z", Prerelease: true}, // prerelease: excluded
		{TagName: "v0.1.146", PublishedAt: "2026-07-07T00:00:00Z"},
		{TagName: "v0.1.145", PublishedAt: "2026-07-06T00:00:00Z", Draft: true}, // draft: excluded
		{TagName: "v0.1.144", PublishedAt: "2026-07-05T00:00:00Z"},
		{TagName: "v0.1.144", PublishedAt: "2026-07-05T00:00:00Z"}, // duplicate: excluded
		{TagName: "v0.1.143", PublishedAt: "2026-07-04T00:00:00Z"},
		{TagName: "v0.1.142", PublishedAt: "2026-07-03T00:00:00Z"}, // beyond cap of 3: excluded
	}
	svc := newRollbackTestService("0.1.147", releases)

	versions, err := svc.ListRollbackVersions(context.Background())

	require.NoError(t, err)
	require.Len(t, versions, 3)
	require.Equal(t, "0.1.146", versions[0].Version)
	require.Equal(t, "0.1.144", versions[1].Version)
	require.Equal(t, "0.1.143", versions[2].Version)
}

func TestUpdateServiceListRollbackVersionsSortsUnorderedInput(t *testing.T) {
	releases := []*GitHubRelease{
		{TagName: "v0.1.144"},
		{TagName: "v0.1.146"},
		{TagName: "v0.1.145"},
	}
	svc := newRollbackTestService("0.1.147", releases)

	versions, err := svc.ListRollbackVersions(context.Background())

	require.NoError(t, err)
	require.Len(t, versions, 3)
	require.Equal(t, "0.1.146", versions[0].Version)
	require.Equal(t, "0.1.145", versions[1].Version)
	require.Equal(t, "0.1.144", versions[2].Version)
}

func TestUpdateServiceListRollbackVersionsEmptyWhenNoneOlder(t *testing.T) {
	releases := []*GitHubRelease{
		{TagName: "v0.1.147"},
		{TagName: "v0.1.148"},
	}
	svc := newRollbackTestService("0.1.147", releases)

	versions, err := svc.ListRollbackVersions(context.Background())

	require.NoError(t, err)
	require.Empty(t, versions)
}

func TestUpdateServiceListRollbackVersionsPropagatesFetchError(t *testing.T) {
	svc := NewUpdateService(
		&updateServiceCacheStub{},
		&updateServiceGitHubClientStub{recentErr: errors.New("github unavailable")},
		"0.1.147",
		"release",
	)

	_, err := svc.ListRollbackVersions(context.Background())

	require.Error(t, err)
	require.Contains(t, err.Error(), "github unavailable")
}

func TestUpdateServiceRollbackToVersionRejectsDisallowedTargets(t *testing.T) {
	releases := []*GitHubRelease{
		{TagName: "v0.1.148"},
		{TagName: "v0.1.147"},
		{TagName: "v0.1.146"},
		{TagName: "v0.1.145"},
		{TagName: "v0.1.144"},
		{TagName: "v0.1.143"},
		{TagName: "v0.1.142"},
	}
	svc := newRollbackTestService("0.1.147", releases)

	for _, target := range []string{
		"",         // empty
		"0.1.147",  // current version
		"v0.1.147", // current version with prefix
		"0.1.148",  // newer than current
		"0.1.142",  // older than the 3 most recent
		"9.9.9",    // nonexistent
	} {
		err := svc.RollbackToVersion(context.Background(), target)
		require.ErrorIs(t, err, ErrRollbackVersionNotAllowed, "target %q should be rejected", target)
	}
}

func TestUpdateServiceRollbackToVersionAcceptsVPrefix(t *testing.T) {
	// No platform asset in the release: the target passes the allowlist check
	// and fails later at asset lookup, proving the version itself was accepted.
	releases := []*GitHubRelease{
		{TagName: "v0.1.147"},
		{TagName: "v0.1.146"},
	}
	svc := newRollbackTestService("0.1.147", releases)

	err := svc.RollbackToVersion(context.Background(), "v0.1.146")

	require.Error(t, err)
	require.NotErrorIs(t, err, ErrRollbackVersionNotAllowed)
	require.Contains(t, err.Error(), "no compatible release found")
}

func newForkUpdateService(current, upstream string, release *GitHubRelease) (*UpdateService, *updateServiceCacheStub, *updateServiceGitHubClientStub) {
	cache := &updateServiceCacheStub{}
	client := &updateServiceGitHubClientStub{release: release}
	svc := NewUpdateService(cache, client, current, "fork").WithUpstreamVersion(upstream)
	return svc, cache, client
}

func TestUpdateServiceForkComparesOfficialBaseline(t *testing.T) {
	svc, _, _ := newForkUpdateService("0.2.17-ds.1", "0.2.17", &GitHubRelease{
		TagName: "v0.2.18",
		Name:    "v0.2.18",
	})

	info, err := svc.CheckUpdate(context.Background(), true)

	require.NoError(t, err)
	require.Equal(t, "0.2.17-ds.1", info.CurrentVersion)
	require.Equal(t, "0.2.17", info.UpstreamVersion)
	require.Equal(t, "0.2.18", info.LatestVersion)
	require.True(t, info.HasUpdate)
	require.Equal(t, "fork", info.BuildType)
	require.Equal(t, "external", info.UpdateMode)
	require.False(t, info.CanSelfUpdate)
	require.Empty(t, info.Warning)
}

func TestUpdateServiceForkClearsUpdateWhenBaselineMatches(t *testing.T) {
	svc, _, _ := newForkUpdateService("0.2.18-ds.1", "0.2.18", &GitHubRelease{TagName: "v0.2.18"})

	info, err := svc.CheckUpdate(context.Background(), true)

	require.NoError(t, err)
	require.False(t, info.HasUpdate)
}

func TestUpdateServiceForkCachedReleaseStillUsesCurrentBaseline(t *testing.T) {
	cache := &updateServiceCacheStub{data: `{"latest":"0.2.18","release_info":{"name":"v0.2.18"},"timestamp":` + formatUnix(time.Now()) + `}`}
	client := &updateServiceGitHubClientStub{}
	svc := NewUpdateService(cache, client, "0.2.18-ds.1", "fork").WithUpstreamVersion("0.2.18")

	info, err := svc.CheckUpdate(context.Background(), false)

	require.NoError(t, err)
	require.True(t, info.Cached)
	require.False(t, info.HasUpdate)
	require.Equal(t, 0, client.latestCalls)
}

func TestUpdateServiceForkMissingUpstreamReturnsDiagnostic(t *testing.T) {
	svc, _, _ := newForkUpdateService("0.2.17-ds.1", "", &GitHubRelease{TagName: "v0.2.18"})

	info, err := svc.CheckUpdate(context.Background(), true)

	require.NoError(t, err)
	require.False(t, info.HasUpdate)
	require.Contains(t, info.Warning, "upstream_version is required")
}

func TestUpdateServiceForkBlocksInPlaceWrites(t *testing.T) {
	svc, _, client := newForkUpdateService("0.2.17-ds.1", "0.2.17", &GitHubRelease{TagName: "v0.2.18"})

	require.ErrorIs(t, svc.PerformUpdate(context.Background()), ErrUpdateManagedExternally)
	require.ErrorIs(t, svc.Rollback(), ErrUpdateManagedExternally)
	require.ErrorIs(t, svc.RollbackToVersion(context.Background(), "0.2.16"), ErrUpdateManagedExternally)
	require.Equal(t, 0, client.latestCalls)

	versions, err := svc.ListRollbackVersions(context.Background())
	require.NoError(t, err)
	require.Empty(t, versions)
	require.Equal(t, 0, client.recentCalls)
}

func formatUnix(t time.Time) string {
	return strconv.FormatInt(t.Unix(), 10)
}

func TestUpdateServiceForkDoesNotDowngradeForOlderOfficialRelease(t *testing.T) {
	svc, _, _ := newForkUpdateService("0.2.18-ds.3", "0.2.18", &GitHubRelease{TagName: "v0.2.17"})

	info, err := svc.CheckUpdate(context.Background(), true)

	require.NoError(t, err)
	require.False(t, info.HasUpdate)
	require.Equal(t, "0.2.17", info.LatestVersion)
}

func TestUpdateServiceForkUsesUpstreamBaselineInsteadOfForkVersion(t *testing.T) {
	svc, _, _ := newForkUpdateService("9.9.9-ds.1", "0.2.17", &GitHubRelease{TagName: "v0.2.18"})

	info, err := svc.CheckUpdate(context.Background(), true)

	require.NoError(t, err)
	require.True(t, info.HasUpdate)
}

func TestUpdateServiceForkMalformedUpstreamReturnsDiagnostic(t *testing.T) {
	svc, _, _ := newForkUpdateService("0.2.17-ds.1", "not-a-version", &GitHubRelease{TagName: "v0.2.18"})

	info, err := svc.CheckUpdate(context.Background(), true)

	require.NoError(t, err)
	require.False(t, info.HasUpdate)
	require.Contains(t, info.Warning, "upstream_version is required")
}
