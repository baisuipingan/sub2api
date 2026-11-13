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
	"sync"
	"sync/atomic"
	"testing"

	"github.com/stretchr/testify/require"
)

const downloadTestOrigin = "https://download.editor.vip"

func windowsDownloadFixture(t *testing.T, asset, version string) []byte {
	t.Helper()
	payload, err := json.Marshal(map[string]any{"schemaVersion": 1, "productId": "app.milo.code", "platform": "win32-x64", "channel": "stable", "version": version, "files": []any{map[string]any{"url": asset, "size": 12345}}})
	require.NoError(t, err)
	envelope, err := json.Marshal(map[string]any{"schemaVersion": 1, "keyId": "fixture", "payload": base64.StdEncoding.EncodeToString(payload), "signature": base64.StdEncoding.EncodeToString(make([]byte, 64))})
	require.NoError(t, err)
	return envelope
}

func macDownloadFixture(items string) []byte {
	return []byte(`<?xml version="1.0" encoding="utf-8"?><!-- signed feed -->
<rss xmlns:sparkle="http://www.andymatuschak.org/xml-namespaces/sparkle"><channel>` + items + `</channel></rss>`)
}

func macDownloadItem(version string) string {
	return `<item><sparkle:version>` + version + `</sparkle:version><enclosure url="` + downloadTestOrigin + `/milocode/releases/` + version + `/darwin-arm64/MiloCode.zip" length="45678" sparkle:edSignature="` + base64.StdEncoding.EncodeToString(make([]byte, 64)) + `" /></item>`
}

func TestDesktopDownloadsParseLatestWithoutHardcodedVersion(t *testing.T) {
	url := downloadTestOrigin + "/milocode/releases/2.10.3/win32-x64/MiloCode.exe"
	got, err := parseWindowsDesktopDownload(windowsDownloadFixture(t, url, "2.10.3"), downloadTestOrigin)
	require.NoError(t, err)
	require.Equal(t, DesktopDownload{Platform: "win32-x64", Available: true, Version: "2.10.3", URL: url, Size: 12345, Format: "exe"}, got)
	got, err = parseMacDesktopDownload(macDownloadFixture(macDownloadItem("1.2.9")+macDownloadItem("1.2.10")+macDownloadItem("1.2.8")), downloadTestOrigin)
	require.NoError(t, err)
	require.Equal(t, "1.2.10", got.Version)
	require.Equal(t, "zip", got.Format)
	// Sparkle also supports version on the direct enclosure. Delta enclosures
	// must never be offered as a first-install download.
	item := strings.Replace(macDownloadItem("3.0.0"), "<sparkle:version>3.0.0</sparkle:version>", "", 1)
	item = strings.Replace(item, "<enclosure ", `<enclosure sparkle:version="3.0.0" `, 1)
	item = strings.Replace(item, "</item>", `<sparkle:deltas><enclosure url="https://attacker.test/delta.zip" /></sparkle:deltas></item>`, 1)
	got, err = parseMacDesktopDownload(macDownloadFixture(item), downloadTestOrigin)
	require.NoError(t, err)
	require.Equal(t, "3.0.0", got.Version)
	require.Equal(t, downloadTestOrigin+"/milocode/releases/3.0.0/darwin-arm64/MiloCode.zip", got.URL)
}

func TestDesktopDownloadsRejectUntrustedPathsAndMalformedFeeds(t *testing.T) {
	valid := downloadTestOrigin + "/milocode/releases/1.2.3/win32-x64/MiloCode.exe"
	for _, asset := range []string{
		strings.Replace(valid, downloadTestOrigin, "https://attacker.test", 1),
		strings.Replace(valid, "https://", "http://", 1),
		strings.Replace(valid, "https://", "https://user:pass@", 1),
		valid + "?token=secret", valid + "#fragment", valid + "#", valid + "?",
		strings.Replace(valid, "1.2.3", "1.2.4", 1),
		strings.Replace(valid, "win32-x64", "darwin-arm64", 1),
		strings.Replace(valid, "MiloCode.exe", "../MiloCode.exe", 1),
		strings.Replace(valid, "MiloCode.exe", "%4diloCode.exe", 1),
		strings.Replace(valid, "MiloCode.exe", "MiloCode.exe.blockmap", 1),
	} {
		t.Run(asset, func(t *testing.T) {
			_, err := parseWindowsDesktopDownload(windowsDownloadFixture(t, asset, "1.2.3"), downloadTestOrigin)
			require.Error(t, err)
		})
	}
	for _, raw := range [][]byte{[]byte(`null`), []byte(`{}`), []byte(`{"schemaVersion":2}`), append(windowsDownloadFixture(t, valid, "1.2.3"), []byte(` {}`)...)} {
		_, err := parseWindowsDesktopDownload(raw, downloadTestOrigin)
		require.Error(t, err)
	}
	_, err := parseMacDesktopDownload(macDownloadFixture(strings.ReplaceAll(macDownloadItem("1.2.3"), downloadTestOrigin, "https://attacker.test")), downloadTestOrigin)
	require.Error(t, err)
	_, err = parseMacDesktopDownload([]byte(`<!DOCTYPE rss [<!ENTITY x SYSTEM "file:///etc/passwd">]><rss><channel>&x;</channel></rss>`), downloadTestOrigin)
	require.Error(t, err)
}

func TestDesktopDownloadsCacheCoalescesAndKeepsPlatformsIndependent(t *testing.T) {
	svc, _, _, _ := desktopReleaseFixture(t)
	_, err := svc.Update(context.Background(), DesktopReleaseStorageConfig{Bucket: "milocode-releases", PublicBaseURL: downloadTestOrigin})
	require.NoError(t, err)
	started, release := make(chan struct{}, 2), make(chan struct{})
	var calls atomic.Int32
	feed := windowsDownloadFixture(t, downloadTestOrigin+"/milocode/releases/9.2.3/win32-x64/MiloCode.exe", "9.2.3")
	svc.downloadFeed = func(_ context.Context, url string) ([]byte, error) {
		calls.Add(1)
		started <- struct{}{}
		<-release
		if strings.HasSuffix(url, "appcast.xml") {
			return nil, errors.New("private-provider-body-with-secret")
		}
		return feed, nil
	}
	var wg sync.WaitGroup
	results := make(chan *DesktopDownloads, 12)
	for range 12 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			value, getErr := svc.PublicDownloads(context.Background())
			if getErr == nil {
				results <- value
			}
		}()
	}
	<-started
	<-started
	close(release)
	wg.Wait()
	close(results)
	require.Len(t, results, 12)
	require.EqualValues(t, 2, calls.Load())
	for result := range results {
		require.True(t, result.Platforms[0].Available)
		require.Equal(t, "9.2.3", result.Platforms[0].Version)
		require.False(t, result.Platforms[1].Available)
		// The caller cannot mutate the shared cache by retaining its result.
		result.Platforms[0].Version = "changed"
	}
	again, err := svc.PublicDownloads(context.Background())
	require.NoError(t, err)
	require.Equal(t, "9.2.3", again.Platforms[0].Version)
	require.EqualValues(t, 2, calls.Load())
	// Disabling configuration immediately bypasses previously cached feeds.
	_, err = svc.Update(context.Background(), DesktopReleaseStorageConfig{})
	require.NoError(t, err)
	again, err = svc.PublicDownloads(context.Background())
	require.NoError(t, err)
	require.False(t, again.Platforms[0].Available)
	require.EqualValues(t, 2, calls.Load())
}

func TestDesktopDownloadsNoConfigurationMakesNoOutboundRequest(t *testing.T) {
	svc, _, _, _ := desktopReleaseFixture(t)
	svc.downloadFeed = func(context.Context, string) ([]byte, error) { t.Fatal("unexpected fetch"); return nil, nil }
	value, err := svc.PublicDownloads(context.Background())
	require.NoError(t, err)
	require.Equal(t, emptyDesktopDownloads(), *value)
}

func TestDesktopDownloadsHTTPBoundaryIsBoundedAndDoesNotRedirect(t *testing.T) {
	for _, test := range []struct {
		status int
		body   string
		ok     bool
	}{{200, "small feed", true}, {302, "", false}, {404, "", false}, {200, strings.Repeat("x", desktopDownloadFeedMaxBytes+1), false}} {
		calls := 0
		client := &http.Client{Transport: desktopReleaseRoundTrip(func(request *http.Request) (*http.Response, error) {
			calls++
			require.Empty(t, request.Header.Get("Authorization"))
			require.Empty(t, request.Header.Get("Cookie"))
			return &http.Response{StatusCode: test.status, Header: http.Header{"Location": []string{"https://attacker.test/"}}, Body: io.NopCloser(strings.NewReader(test.body)), Request: request}, nil
		})}
		_, err := fetchDesktopDownloadFeedWithClient(context.Background(), downloadTestOrigin+"/milocode/stable/win32-x64/latest.json", client)
		if test.ok {
			require.NoError(t, err)
		} else {
			require.Error(t, err)
		}
		require.Equal(t, 1, calls)
	}
	for _, raw := range []string{"http://example.test/feed", "https://127.0.0.1/feed", "https://localhost/feed", "https://10.0.0.1/feed"} {
		_, err := fetchDesktopDownloadFeed(context.Background(), raw)
		require.Error(t, err)
	}
}
