package repository

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/config"
)

func TestOfficeStorageProbeReadsViaPresignedURLAndDeletes(t *testing.T) {
	var mu sync.Mutex
	var body string
	var calls []string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		defer mu.Unlock()
		calls = append(calls, r.Method)
		if !strings.HasPrefix(r.URL.Path, "/office/workspace/v1/_connection-test/") {
			t.Errorf("unexpected probe key %q", r.URL.Path)
		}
		switch r.Method {
		case http.MethodPut:
			data, err := io.ReadAll(r.Body)
			if err != nil {
				t.Errorf("read probe body: %v", err)
			}
			body = string(data)
		case http.MethodGet:
			if r.URL.Query().Get("X-Amz-Signature") == "" || r.URL.Query().Get("X-Amz-Expires") != "120" {
				t.Error("probe request lacks the expected short-lived signature")
			}
			_, _ = io.WriteString(w, body)
		case http.MethodDelete:
			body = ""
			w.WriteHeader(http.StatusNoContent)
		}
	}))
	defer server.Close()
	store, err := NewS3ImageStorage(context.Background(), &config.ImageStorageConfig{
		Endpoint: server.URL, Bucket: "office", Region: "auto",
		AccessKeyID: "test", SecretAccessKey: "test", ForcePathStyle: true,
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := store.ProbeSignedOfficeRead(context.Background(), "workspace/v1/"); err != nil {
		t.Fatal(err)
	}
	mu.Lock()
	defer mu.Unlock()
	if body != "" || strings.Join(calls, " ") != "PUT GET DELETE" {
		t.Fatal("expected probe object to be deleted after signed read", calls)
	}
}

func TestPresignOfficePutBindsContentMD5AndOptionalCondition(t *testing.T) {
	ctx := context.Background()
	client, err := newS3Client(ctx, s3ClientParams{
		Endpoint: "https://storage.example.test", Region: "auto",
		AccessKeyID: "test", SecretAccessKey: "test", ForcePathStyle: true,
	})
	if err != nil {
		t.Fatal(err)
	}
	store := &s3ImageStudioGalleryStore{client: client, bucket: "workspace"}
	const checksum = "1B2M2Y8AsgTpgAmY7PhCfg=="
	for _, tc := range []struct {
		name       string
		createOnly bool
		condition  string
	}{
		{name: "conditional", createOnly: true, condition: "*"},
		{name: "legacy"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			rawURL, headers, err := store.PresignOfficePut(ctx, "version/original", "text/plain", 42, checksum, tc.createOnly, time.Minute)
			if err != nil {
				t.Fatal(err)
			}
			signedURL, err := url.Parse(rawURL)
			if err != nil {
				t.Fatal(err)
			}
			signed := ";" + signedURL.Query().Get("X-Amz-SignedHeaders") + ";"
			if headers.Get("Content-MD5") != checksum || !strings.Contains(signed, ";content-md5;") {
				t.Fatalf("content checksum was not signed: %s", signed)
			}
			if headers.Get("If-None-Match") != tc.condition || strings.Contains(signed, ";if-none-match;") != tc.createOnly {
				t.Fatalf("unexpected conditional header: %s", signed)
			}
		})
	}
}
