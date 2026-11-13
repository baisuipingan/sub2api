package service

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"encoding/xml"
	"errors"
	"io"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/pkg/httpclient"
	"github.com/Wei-Shaw/sub2api/internal/util/urlvalidator"
	"golang.org/x/sync/singleflight"
)

const desktopDownloadFeedMaxBytes = 1 << 20
const desktopDownloadCacheTTL = time.Minute

type DesktopDownload struct {
	Platform  string `json:"platform"`
	Available bool   `json:"available"`
	Version   string `json:"version,omitempty"`
	URL       string `json:"url,omitempty"`
	Size      int64  `json:"size,omitempty"`
	Format    string `json:"format,omitempty"`
}

type DesktopDownloads struct {
	Platforms []DesktopDownload `json:"platforms"`
}

type desktopDownloadCache struct {
	mu      sync.Mutex
	origin  string
	expires time.Time
	value   DesktopDownloads
	flight  singleflight.Group
}

func emptyDesktopDownloads() DesktopDownloads {
	return DesktopDownloads{Platforms: []DesktopDownload{{Platform: "win32-x64"}, {Platform: "darwin-arm64"}}}
}

func (c *desktopDownloadCache) get(origin string) (DesktopDownloads, bool) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.origin != origin || !time.Now().Before(c.expires) {
		return DesktopDownloads{}, false
	}
	return cloneDesktopDownloads(c.value), true
}

func cloneDesktopDownloads(value DesktopDownloads) DesktopDownloads {
	return DesktopDownloads{Platforms: append([]DesktopDownload(nil), value.Platforms...)}
}

// PublicDownloads only reads public, bounded metadata. Download traffic and
// updater signature verification stay at their existing, separate boundaries.
func (s *DesktopReleaseStorageService) PublicDownloads(ctx context.Context) (*DesktopDownloads, error) {
	cfg, err := s.load(ctx)
	if err != nil {
		return nil, ErrDesktopReleaseStorageUnavailable
	}
	if cfg.PublicBaseURL == "" || cfg.Bucket == "" {
		value := emptyDesktopDownloads()
		return &value, nil
	}
	if value, ok := s.downloads.get(cfg.PublicBaseURL); ok {
		return &value, nil
	}
	result := s.downloads.flight.DoChan(cfg.PublicBaseURL, func() (any, error) {
		if value, ok := s.downloads.get(cfg.PublicBaseURL); ok {
			return value, nil
		}
		// One caller leaving the page must not cancel the shared read for others.
		fetchCtx, cancel := context.WithTimeout(context.Background(), 8*time.Second)
		defer cancel()
		value := emptyDesktopDownloads()
		var wg sync.WaitGroup
		for i, feed := range []string{"milocode/stable/win32-x64/latest.json", "milocode/stable/darwin-arm64/appcast.xml"} {
			wg.Add(1)
			go func(index int, path string) {
				defer wg.Done()
				if s.downloadFeed == nil {
					return
				}
				raw, fetchErr := s.downloadFeed(fetchCtx, cfg.PublicBaseURL+"/"+path)
				if fetchErr != nil || len(raw) > desktopDownloadFeedMaxBytes {
					return
				}
				var parsed DesktopDownload
				if index == 0 {
					parsed, fetchErr = parseWindowsDesktopDownload(raw, cfg.PublicBaseURL)
				} else {
					parsed, fetchErr = parseMacDesktopDownload(raw, cfg.PublicBaseURL)
				}
				if fetchErr == nil {
					value.Platforms[index] = parsed
				}
			}(i, feed)
		}
		wg.Wait()
		s.downloads.mu.Lock()
		s.downloads.origin, s.downloads.expires, s.downloads.value = cfg.PublicBaseURL, time.Now().Add(desktopDownloadCacheTTL), cloneDesktopDownloads(value)
		s.downloads.mu.Unlock()
		return value, nil
	})
	select {
	case <-ctx.Done():
		return nil, ctx.Err()
	case result := <-result:
		if result.Err != nil {
			return nil, ErrDesktopReleaseStorageUnavailable
		}
		value := cloneDesktopDownloads(result.Val.(DesktopDownloads))
		return &value, nil
	}
}

func fetchDesktopDownloadFeed(ctx context.Context, raw string) ([]byte, error) {
	validated, err := urlvalidator.ValidateHTTPSURL(raw, urlvalidator.ValidationOptions{AllowPrivate: false})
	if err != nil {
		return nil, err
	}
	shared, err := httpclient.GetClient(httpclient.Options{Timeout: 6 * time.Second, ValidateResolvedIP: true})
	if err != nil {
		return nil, err
	}
	return fetchDesktopDownloadFeedWithClient(ctx, validated, shared)
}

func fetchDesktopDownloadFeedWithClient(ctx context.Context, raw string, shared *http.Client) ([]byte, error) {
	client := *shared
	client.CheckRedirect = func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, raw, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Accept", "application/json, application/xml, text/xml")
	resp, err := client.Do(req)
	if err != nil {
		return nil, err
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusOK || resp.ContentLength > desktopDownloadFeedMaxBytes {
		return nil, errors.New("download metadata unavailable")
	}
	body, err := io.ReadAll(io.LimitReader(resp.Body, desktopDownloadFeedMaxBytes+1))
	if err != nil || len(body) > desktopDownloadFeedMaxBytes {
		return nil, errors.New("download metadata too large")
	}
	return body, nil
}

func desktopDownloadAsset(raw, origin, version, platform, suffix string, size int64) (DesktopDownload, error) {
	u, err := url.Parse(raw)
	if err != nil || u.Scheme != "https" || u.User != nil || u.RawQuery != "" || u.ForceQuery || u.Fragment != "" || u.RawPath != "" || strings.Contains(raw, "#") || len(version) > 64 || !desktopReleaseVersionPattern.MatchString(version) || size <= 0 || size > desktopReleaseMaxBytes {
		return DesktopDownload{}, ErrDesktopReleaseStorageInvalid
	}
	base, err := url.Parse(origin)
	if err != nil || u.Host != base.Host || u.Scheme != base.Scheme {
		return DesktopDownload{}, ErrDesktopReleaseStorageInvalid
	}
	key := strings.TrimPrefix(u.Path, "/")
	pathVersion, _, _, err := desktopReleaseKey(key)
	if err != nil || pathVersion != version || !strings.HasPrefix(key, "milocode/releases/"+version+"/"+platform+"/") || !strings.HasSuffix(key, suffix) {
		return DesktopDownload{}, ErrDesktopReleaseStorageInvalid
	}
	return DesktopDownload{Platform: platform, Available: true, Version: version, URL: u.String(), Size: size, Format: strings.TrimPrefix(suffix, ".")}, nil
}

func decodeDesktopDownloadJSON(raw []byte, target any) error {
	decoder := json.NewDecoder(bytes.NewReader(raw))
	if decoder.Decode(target) != nil || decoder.Decode(new(any)) != io.EOF {
		return ErrDesktopReleaseStorageInvalid
	}
	return nil
}

func parseWindowsDesktopDownload(raw []byte, origin string) (DesktopDownload, error) {
	var envelope struct {
		SchemaVersion int    `json:"schemaVersion"`
		KeyID         string `json:"keyId"`
		Payload       string `json:"payload"`
		Signature     string `json:"signature"`
	}
	if decodeDesktopDownloadJSON(raw, &envelope) != nil || envelope.SchemaVersion != 1 || envelope.KeyID == "" || len(envelope.KeyID) > 64 {
		return DesktopDownload{}, ErrDesktopReleaseStorageInvalid
	}
	signature, err := base64.StdEncoding.DecodeString(envelope.Signature)
	if err != nil || len(signature) != 64 {
		return DesktopDownload{}, ErrDesktopReleaseStorageInvalid
	}
	payload, err := base64.StdEncoding.DecodeString(envelope.Payload)
	if err != nil {
		return DesktopDownload{}, ErrDesktopReleaseStorageInvalid
	}
	var manifest struct {
		SchemaVersion int    `json:"schemaVersion"`
		ProductID     string `json:"productId"`
		Platform      string `json:"platform"`
		Channel       string `json:"channel"`
		Version       string `json:"version"`
		Files         []struct {
			URL  string `json:"url"`
			Size int64  `json:"size"`
		} `json:"files"`
	}
	if decodeDesktopDownloadJSON(payload, &manifest) != nil || manifest.SchemaVersion != 1 || manifest.ProductID != "app.milo.code" || manifest.Platform != "win32-x64" || manifest.Channel != "stable" || len(manifest.Files) != 1 {
		return DesktopDownload{}, ErrDesktopReleaseStorageInvalid
	}
	return desktopDownloadAsset(manifest.Files[0].URL, origin, manifest.Version, manifest.Platform, ".exe", manifest.Files[0].Size)
}

func parseMacDesktopDownload(raw []byte, origin string) (DesktopDownload, error) {
	var feed struct {
		XMLName xml.Name `xml:"rss"`
		Items   []struct {
			Version   string `xml:"http://www.andymatuschak.org/xml-namespaces/sparkle version"`
			Enclosure struct {
				URL       string `xml:"url,attr"`
				Length    int64  `xml:"length,attr"`
				Version   string `xml:"http://www.andymatuschak.org/xml-namespaces/sparkle version,attr"`
				Signature string `xml:"http://www.andymatuschak.org/xml-namespaces/sparkle edSignature,attr"`
			} `xml:"enclosure"`
		} `xml:"channel>item"`
	}
	if xml.Unmarshal(raw, &feed) != nil {
		return DesktopDownload{}, ErrDesktopReleaseStorageInvalid
	}
	var latest DesktopDownload
	for _, item := range feed.Items {
		version := item.Version
		if version == "" {
			version = item.Enclosure.Version
		}
		signature, err := base64.StdEncoding.DecodeString(item.Enclosure.Signature)
		if err != nil || len(signature) != 64 {
			continue
		}
		candidate, err := desktopDownloadAsset(item.Enclosure.URL, origin, version, "darwin-arm64", ".zip", item.Enclosure.Length)
		if err == nil && (!latest.Available || desktopDownloadNewer(candidate.Version, latest.Version)) {
			latest = candidate
		}
	}
	if !latest.Available {
		return DesktopDownload{}, ErrDesktopReleaseStorageInvalid
	}
	return latest, nil
}

func desktopDownloadNewer(left, right string) bool {
	a, b := strings.Split(left, "."), strings.Split(right, ".")
	for i := range a {
		if len(a[i]) != len(b[i]) {
			return len(a[i]) > len(b[i])
		}
		if a[i] != b[i] {
			return a[i] > b[i]
		}
	}
	return false
}
