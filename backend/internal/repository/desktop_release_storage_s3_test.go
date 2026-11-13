//go:build unit

package repository

import (
	"context"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/stretchr/testify/require"
)

func TestDesktopReleasePresignBindsFrozenPlan(t *testing.T) {
	client, err := newS3Client(context.Background(), s3ClientParams{Endpoint: "https://account.r2.cloudflarestorage.com", Region: "auto", AccessKeyID: "fixture-id", SecretAccessKey: "fixture-secret", ForcePathStyle: true})
	require.NoError(t, err)
	store := &desktopReleaseS3Store{client: client, bucket: "milocode-releases"}
	in := service.DesktopReleaseUploadRequest{Key: "milocode/releases/1.2.3/darwin-arm64/MiloCode.zip", Size: 12345, ContentType: "application/zip", SHA256: strings.Repeat("a", 64), SHA512: strings.Repeat("A", 86) + "==", Version: "1.2.3", IfNoneMatch: "*"}
	result, err := store.PresignUpload(context.Background(), in, service.DesktopReleaseImmutableCacheControl, 15*time.Minute)
	require.NoError(t, err)
	u, err := url.Parse(result.URL)
	require.NoError(t, err)
	require.Equal(t, "900", u.Query().Get("X-Amz-Expires"))
	require.Equal(t, "AWS4-HMAC-SHA256", u.Query().Get("X-Amz-Algorithm"))
	require.NotEmpty(t, u.Query().Get("X-Amz-Signature"))
	for header, value := range map[string]string{"Content-Type": in.ContentType, "Content-Length": "12345", "Cache-Control": service.DesktopReleaseImmutableCacheControl, "If-None-Match": "*", "X-Amz-Meta-Sha256": in.SHA256, "X-Amz-Meta-Sha512": in.SHA512, "X-Amz-Meta-Milocode-Version": "1.2.3"} {
		require.Equal(t, value, result.Headers[header], header)
		require.Contains(t, strings.Split(u.Query().Get("X-Amz-SignedHeaders"), ";"), strings.ToLower(header), header)
		require.Empty(t, u.Query().Get(header), "metadata must not be hoisted")
	}
	require.NotContains(t, result.Headers, "Host")
	require.NotContains(t, result.Headers, "Authorization")
	require.NotContains(t, result.URL, "fixture-secret")
}

func TestDesktopReleaseHeadPreservesMetadataAndETag(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.HasSuffix(r.URL.Path, "missing.zip") {
			w.WriteHeader(http.StatusNotFound)
			return
		}
		w.Header().Set("Content-Length", "42")
		w.Header().Set("ETag", `"opaque-etag"`)
		w.Header().Set("X-Amz-Meta-Sha512", "hash-value")
		w.Header().Set("X-Amz-Meta-Milocode-Version", "1.2.3")
	}))
	defer server.Close()
	client, err := newS3Client(context.Background(), s3ClientParams{Endpoint: server.URL, Region: "auto", AccessKeyID: "ak", SecretAccessKey: "sk", ForcePathStyle: true})
	require.NoError(t, err)
	store := &desktopReleaseS3Store{client: client, bucket: "releases"}
	got, err := store.Head(context.Background(), "artifact.zip")
	require.NoError(t, err)
	require.Equal(t, &service.DesktopReleaseObjectHead{Exists: true, Size: 42, ETag: `"opaque-etag"`, SHA512: "hash-value", Version: "1.2.3"}, got)
	got, err = store.Head(context.Background(), "missing.zip")
	require.NoError(t, err)
	require.False(t, got.Exists)
}
