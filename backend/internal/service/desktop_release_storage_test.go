//go:build unit

package service

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/config"
	"github.com/stretchr/testify/require"
)

type desktopReleaseStoreStub struct {
	puts, gets, deletes          int
	body                         []byte
	failPut, failGet, failDelete bool
	request                      DesktopReleaseUploadRequest
	cache                        string
	expiry                       time.Duration
}

func (s *desktopReleaseStoreStub) Head(context.Context, string) (*DesktopReleaseObjectHead, error) {
	return &DesktopReleaseObjectHead{Exists: false}, nil
}
func (s *desktopReleaseStoreStub) PresignUpload(_ context.Context, in DesktopReleaseUploadRequest, cache string, expiry time.Duration) (*DesktopReleaseUpload, error) {
	s.request = in
	s.cache = cache
	s.expiry = expiry
	return &DesktopReleaseUpload{URL: "https://r2.example.test/upload"}, nil
}
func (s *desktopReleaseStoreStub) PutProbe(_ context.Context, _ string, body []byte) error {
	s.puts++
	s.body = body
	if s.failPut {
		return errors.New("private-provider-response")
	}
	return nil
}
func (s *desktopReleaseStoreStub) GetProbe(context.Context, string) ([]byte, error) {
	s.gets++
	if s.failGet {
		return nil, errors.New("private-provider-response")
	}
	return s.body, nil
}
func (s *desktopReleaseStoreStub) DeleteProbe(context.Context, string) error {
	s.deletes++
	if s.failDelete {
		return errors.New("private-provider-response")
	}
	return nil
}

func desktopReleaseFixture(t *testing.T) (*DesktopReleaseStorageService, *stubSettingRepo, *desktopReleaseStoreStub, *BackupS3Config) {
	t.Helper()
	repo := newStubSettingRepo()
	seedBackupS3(t, repo, BackupS3Config{Endpoint: "https://account.r2.cloudflarestorage.com", Region: "auto", Bucket: "private-backups", AccessKeyID: "secret-access-id", SecretAccessKey: "secret-credential"})
	backup := NewBackupService(repo, &config.Config{}, reversibleEncryptor{}, nil, nil)
	store := &desktopReleaseStoreStub{}
	// Return a stable pointer to observe the resolved credential copy.
	observed := &BackupS3Config{}
	svc := NewDesktopReleaseStorageService(repo, backup, func(_ context.Context, cfg *BackupS3Config) (DesktopReleaseObjectStore, error) {
		*observed = *cfg
		return store, nil
	})
	svc.publicProbe = func(_ context.Context, _ string, _ []byte) error { return nil }
	return svc, repo, store, observed
}

func TestDesktopReleaseStorageIndependentConfig(t *testing.T) {
	ctx := context.Background()
	svc, repo, _, observed := desktopReleaseFixture(t)
	view, err := svc.Get(ctx)
	require.NoError(t, err)
	require.False(t, view.Configured)
	require.True(t, view.BackupS3Configured)
	view, err = svc.Update(ctx, DesktopReleaseStorageConfig{Bucket: "milocode-releases", PublicBaseURL: "https://download.editor.vip/"})
	require.NoError(t, err)
	require.True(t, view.Configured)
	require.Equal(t, "https://download.editor.vip/milocode/", view.ReleaseBaseURL)
	stored := repo.values[settingKeyDesktopReleaseStorage]
	require.NotContains(t, stored, "secret")
	require.NotContains(t, stored, "endpoint")
	var persisted map[string]any
	require.NoError(t, json.Unmarshal([]byte(stored), &persisted))
	require.Len(t, persisted, 2)
	_, err = svc.Head(ctx, "milocode/stable/win32-x64/latest.json")
	require.NoError(t, err)
	require.Equal(t, "milocode-releases", observed.Bucket)
	require.Equal(t, "secret-credential", observed.SecretAccessKey)
	backup, err := svc.backup.loadS3Config(ctx)
	require.NoError(t, err)
	require.Equal(t, "private-backups", backup.Bucket)
	view, err = svc.Update(ctx, DesktopReleaseStorageConfig{})
	require.NoError(t, err)
	require.False(t, view.Configured)
	require.Empty(t, view.ReleaseBaseURL)
}

func TestDesktopReleaseStorageRejectsUnsafeDestination(t *testing.T) {
	for _, cfg := range []DesktopReleaseStorageConfig{
		{Bucket: "private-backups", PublicBaseURL: "https://download.editor.vip"},
		{Bucket: "", PublicBaseURL: "https://download.editor.vip"},
		{Bucket: "releases", PublicBaseURL: "http://download.editor.vip"},
		{Bucket: "releases", PublicBaseURL: "https://user:password@download.editor.vip"},
		{Bucket: "releases", PublicBaseURL: "https://download.editor.vip/path"},
		{Bucket: "releases", PublicBaseURL: "https://download.editor.vip?"},
		{Bucket: "releases", PublicBaseURL: "https://download.editor.vip/#"},
		{Bucket: "releases", PublicBaseURL: "https://127.0.0.1"},
		{Bucket: "releases", PublicBaseURL: "https://localhost"},
		{Bucket: "../backups", PublicBaseURL: "https://download.editor.vip"},
	} {
		t.Run(cfg.PublicBaseURL+cfg.Bucket, func(t *testing.T) {
			svc, repo, _, _ := desktopReleaseFixture(t)
			_, err := svc.Update(context.Background(), cfg)
			require.ErrorIs(t, err, ErrDesktopReleaseStorageInvalid)
			require.Empty(t, repo.values[settingKeyDesktopReleaseStorage])
		})
	}
}

func validDesktopReleaseUpload() DesktopReleaseUploadRequest {
	return DesktopReleaseUploadRequest{Key: "milocode/releases/1.2.3/darwin-arm64/MiloCode-1.2.3.zip", Size: 123, ContentType: "application/zip", SHA256: strings.Repeat("a", 64), SHA512: base64.StdEncoding.EncodeToString(make([]byte, 64)), Version: "1.2.3", IfNoneMatch: "*"}
}
func TestDesktopReleaseUploadAllowlistAndConditionalWrites(t *testing.T) {
	valid := validDesktopReleaseUpload()
	cache, err := validateDesktopReleaseUpload(valid)
	require.NoError(t, err)
	require.Equal(t, DesktopReleaseImmutableCacheControl, cache)
	for name, change := range map[string]func(*DesktopReleaseUploadRequest){
		"mismatched-version": func(in *DesktopReleaseUploadRequest) { in.Version = "1.2.4" },
		"prerelease":         func(in *DesktopReleaseUploadRequest) { in.Version = "1.2.3-beta.1" },
		"oversized":          func(in *DesktopReleaseUploadRequest) { in.Size = desktopReleaseMaxBytes + 1 },
		"zero":               func(in *DesktopReleaseUploadRequest) { in.Size = 0 },
		"bad-sha256":         func(in *DesktopReleaseUploadRequest) { in.SHA256 = strings.Repeat("A", 64) },
		"bad-sha512":         func(in *DesktopReleaseUploadRequest) { in.SHA512 = "a" },
		"wrong-mime":         func(in *DesktopReleaseUploadRequest) { in.ContentType = "application/octet-stream" },
		"overwrite-version":  func(in *DesktopReleaseUploadRequest) { in.IfNoneMatch = ""; in.IfMatch = `"etag"` },
		"no-condition":       func(in *DesktopReleaseUploadRequest) { in.IfNoneMatch = "" },
		"two-conditions":     func(in *DesktopReleaseUploadRequest) { in.IfMatch = `"etag"` },
		"traversal":          func(in *DesktopReleaseUploadRequest) { in.Key = "milocode/releases/1.2.3/darwin-arm64/../escape.zip" },
		"encoded":            func(in *DesktopReleaseUploadRequest) { in.Key = "milocode/releases/1.2.3/darwin-arm64/%2fescape.zip" },
		"wrong-platform":     func(in *DesktopReleaseUploadRequest) { in.Key = "milocode/releases/1.2.3/win32-x64/file.zip" },
		"notes":              func(in *DesktopReleaseUploadRequest) { in.Key = "milocode/releases/1.2.3/darwin-arm64/notes.md" },
		"extra-slash":        func(in *DesktopReleaseUploadRequest) { in.Key = "milocode//releases/1.2.3/darwin-arm64/file.zip" },
	} {
		t.Run(name, func(t *testing.T) {
			in := valid
			change(&in)
			_, err := validateDesktopReleaseUpload(in)
			require.ErrorIs(t, err, ErrDesktopReleaseStorageInvalid)
		})
	}
	for _, condition := range []struct {
		none, match string
		ok          bool
	}{{"*", "", true}, {"", `"etag-value"`, true}, {"", "", false}, {"*", `"etag"`, false}, {"", "W/\"etag\"", false}, {"", "*", false}} {
		in := valid
		in.Key = "milocode/stable/win32-x64/latest.json"
		in.ContentType = "application/json"
		in.IfNoneMatch, in.IfMatch = condition.none, condition.match
		cache, err := validateDesktopReleaseUpload(in)
		if condition.ok {
			require.NoError(t, err)
			require.Equal(t, DesktopReleaseStableCacheControl, cache)
		} else {
			require.Error(t, err)
		}
	}
}

func TestDesktopReleaseProbeAlwaysCleansAndHidesProviderErrors(t *testing.T) {
	for _, failure := range []string{"", "put", "get", "public", "delete"} {
		t.Run(failure, func(t *testing.T) {
			svc, _, store, _ := desktopReleaseFixture(t)
			store.failPut = failure == "put"
			store.failGet = failure == "get"
			store.failDelete = failure == "delete"
			svc.publicProbe = func(_ context.Context, raw string, body []byte) error {
				require.True(t, strings.HasPrefix(raw, "https://download.editor.vip/milocode/.probe/"))
				require.Less(t, len(body), 128)
				if failure == "public" {
					return errors.New("private-response")
				}
				return nil
			}
			err := svc.TestConnection(context.Background(), DesktopReleaseStorageConfig{Bucket: "milocode-releases", PublicBaseURL: "https://download.editor.vip"})
			if failure == "" {
				require.NoError(t, err)
			} else {
				require.ErrorIs(t, err, ErrDesktopReleaseStorageTestFailed)
				require.NotContains(t, err.Error(), "private-response")
			}
			require.Equal(t, 1, store.deletes)
		})
	}
}

type desktopReleaseRoundTrip func(*http.Request) (*http.Response, error)

func (f desktopReleaseRoundTrip) RoundTrip(request *http.Request) (*http.Response, error) {
	return f(request)
}

func TestDesktopReleasePublicProbeDoesNotFollowRedirectOrReadUnboundedBody(t *testing.T) {
	for _, test := range []struct {
		status int
		body   string
		ok     bool
	}{{http.StatusOK, "expected", true}, {http.StatusFound, "", false}, {http.StatusOK, strings.Repeat("a", 5000), false}, {http.StatusNotFound, "expected", false}} {
		calls := 0
		client := &http.Client{Transport: desktopReleaseRoundTrip(func(request *http.Request) (*http.Response, error) {
			calls++
			require.Empty(t, request.Header.Get("Authorization"))
			require.Empty(t, request.Header.Get("Cookie"))
			return &http.Response{StatusCode: test.status, Header: http.Header{"Location": []string{"https://attacker.example.test/"}}, Body: io.NopCloser(strings.NewReader(test.body)), Request: request}, nil
		})}
		err := desktopReleaseProbeWithClient(context.Background(), "https://download.example.test/milocode/.probe/random.txt", []byte("expected"), client)
		if test.ok {
			require.NoError(t, err)
		} else {
			require.Error(t, err)
		}
		require.Equal(t, 1, calls, "redirects must not be followed")
	}
	for _, raw := range []string{"http://example.test/file", "https://127.0.0.1/file", "https://localhost/file", "https://10.0.0.1/file"} {
		require.Error(t, probeDesktopReleasePublicObject(context.Background(), raw, []byte("expected")))
	}
}
