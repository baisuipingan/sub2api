package ticketrequest

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"mime/multipart"
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	infraerrors "github.com/Wei-Shaw/sub2api/internal/pkg/errors"
	pkghttputil "github.com/Wei-Shaw/sub2api/internal/pkg/httputil"
	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

func multipartContext(t *testing.T, files int, size int, key string) *gin.Context {
	t.Helper()
	var body bytes.Buffer
	w := multipart.NewWriter(&body)
	require.NoError(t, w.WriteField("content", " A screenshot "))
	for i := 0; i < files; i++ {
		p, err := w.CreateFormFile("images", "screen.png")
		require.NoError(t, err)
		_, err = p.Write(bytes.Repeat([]byte("x"), size))
		require.NoError(t, err)
	}
	require.NoError(t, w.Close())
	c, _ := gin.CreateTestContext(httptest.NewRecorder())
	c.Request = httptest.NewRequest(http.MethodPost, "/tickets", &body)
	c.Request.Header.Set("Content-Type", w.FormDataContentType())
	c.Request.Header.Set("Idempotency-Key", key)
	return c
}

func TestMultipartLimitsAndKey(t *testing.T) {
	gin.SetMode(gin.TestMode)
	for _, tc := range []struct {
		name        string
		files, size int
		key         string
		status      int
	}{
		{"valid", 3, 100, "a-key", 0},
		{"four screenshots", 4, 100, "a-key", http.StatusBadRequest},
		{"oversized screenshot", 1, maxFileBytes + 1, "a-key", http.StatusRequestEntityTooLarge},
		{"missing key", 1, 100, "", http.StatusBadRequest},
	} {
		t.Run(tc.name, func(t *testing.T) {
			req, err := Parse(multipartContext(t, tc.files, tc.size, tc.key))
			if tc.status == 0 {
				require.NoError(t, err)
				require.Len(t, req.Uploads, 3)
				return
			}
			require.Error(t, err)
			require.Equal(t, tc.status, infraerrors.Code(err))
		})
	}
}

func TestMultipartFingerprintIgnoresBoundaryIncludesTargetAndBytes(t *testing.T) {
	one, err := Parse(multipartContext(t, 1, 100, "key"))
	require.NoError(t, err)
	two, err := Parse(multipartContext(t, 1, 100, "key"))
	require.NoError(t, err)
	encode := func(v any) string { b, err := json.Marshal(v); require.NoError(t, err); return string(b) }
	require.Equal(t, encode(one.fingerprint(1)), encode(two.fingerprint(1)))
	require.NotEqual(t, encode(one.fingerprint(1)), encode(two.fingerprint(2)))
	two.Uploads[0].Data[0] = 'y'
	require.NotEqual(t, encode(one.fingerprint(1)), encode(two.fingerprint(1)))
}

func TestJSONCompatibilityAndBound(t *testing.T) {
	for _, body := range []string{`{"title":"Help","content":"Text"}`, `{"content":"` + strings.Repeat("x", maxJSONBytes) + `"}`} {
		c, _ := gin.CreateTestContext(httptest.NewRecorder())
		c.Request = httptest.NewRequest(http.MethodPost, "/tickets", strings.NewReader(body))
		c.Request.Header.Set("Content-Type", "application/json")
		req, err := Parse(c)
		if len(body) > maxJSONBytes {
			require.Error(t, err)
			require.Equal(t, http.StatusRequestEntityTooLarge, infraerrors.Code(err))
		} else {
			require.NoError(t, err)
			require.Empty(t, req.Uploads)
			require.Equal(t, "Help", req.Title)
		}
	}
}

func TestJSONLegacyContentType(t *testing.T) {
	for _, contentType := range []string{"", "application/x-www-form-urlencoded", "application/json"} {
		c, _ := gin.CreateTestContext(httptest.NewRecorder())
		c.Request = httptest.NewRequest(http.MethodPost, "/tickets", strings.NewReader(`{"content":"legacy"}`))
		c.Request.Header.Set("Content-Type", contentType)
		req, err := Parse(c)
		require.NoError(t, err)
		require.Equal(t, "legacy", req.Content)
	}
}

type blockedUploadBody struct {
	closed chan struct{}
	once   sync.Once
}

func (b *blockedUploadBody) Read([]byte) (int, error) { <-b.closed; return 0, io.ErrClosedPipe }
func (b *blockedUploadBody) Close() error             { b.once.Do(func() { close(b.closed) }); return nil }

func TestMultipartTotalDeadlineClosesSlowBody(t *testing.T) {
	c, _ := gin.CreateTestContext(httptest.NewRecorder())
	body := &blockedUploadBody{closed: make(chan struct{})}
	c.Request = httptest.NewRequest(http.MethodPost, "/tickets", nil)
	c.Request.Body = body
	c.Request.Header.Set("Content-Type", "multipart/form-data; boundary=slow")
	c.Request.Header.Set("Idempotency-Key", "slow-key")
	start := time.Now()
	_, err := parse(c, 10*time.Millisecond)
	require.Equal(t, "TICKET_UPLOAD_TIMEOUT", infraerrors.Reason(err))
	require.Less(t, time.Since(start), time.Second)
}

func TestMultipartTotalDeadlineOnRealServer(t *testing.T) {
	done := make(chan error, 1)
	router := gin.New()
	router.POST("/tickets", func(c *gin.Context) {
		_, err := parse(c, 60*time.Millisecond)
		done <- err
		c.Status(http.StatusRequestTimeout)
	})
	server := httptest.NewServer(pkghttputil.WithBodyReadTimeout(router, time.Second))
	defer server.Close()
	conn, err := net.Dial("tcp", server.Listener.Addr().String())
	require.NoError(t, err)
	defer conn.Close()
	_, err = io.WriteString(conn, "POST /tickets HTTP/1.1\r\nHost: test\r\nContent-Type: multipart/form-data; boundary=slow\r\nIdempotency-Key: slow-key\r\nContent-Length: 1024\r\n\r\n--slow\r\n")
	require.NoError(t, err)
	select {
	case err := <-done:
		require.Equal(t, "TICKET_UPLOAD_TIMEOUT", infraerrors.Reason(err))
	case <-time.After(500 * time.Millisecond):
		t.Fatal("slow upload did not release its handler")
	}
}

type operationNamespaceRepo struct {
	service.IdempotencyRepository
	records []*service.IdempotencyRecord
}

func (r *operationNamespaceRepo) CreateProcessing(_ context.Context, record *service.IdempotencyRecord) (bool, error) {
	record.ID = int64(len(r.records) + 1)
	r.records = append(r.records, record)
	return true, nil
}
func (r *operationNamespaceRepo) MarkSucceeded(context.Context, int64, int, string, time.Time) error {
	return nil
}

func TestTicketOperationKeysAreScopedByActorAndTicket(t *testing.T) {
	previous := service.DefaultIdempotencyCoordinator()
	t.Cleanup(func() { service.SetDefaultIdempotencyCoordinator(previous) })
	repo := &operationNamespaceRepo{}
	service.SetDefaultIdempotencyCoordinator(service.NewIdempotencyCoordinator(repo, service.DefaultIdempotencyConfig()))
	var operationIDs []string
	for _, input := range []struct {
		actor    string
		ticketID int64
	}{{"user:1", 1}, {"user:2", 1}, {"user:1", 2}} {
		c, _ := gin.CreateTestContext(httptest.NewRecorder())
		c.Request = httptest.NewRequest(http.MethodPost, "/tickets/messages", nil)
		Execute(c, input.actor, "ticket.reply", input.ticketID, Request{Content: "hello", key: "same-key"}, func(_ context.Context, operationID string) (any, error) {
			operationIDs = append(operationIDs, operationID)
			return map[string]any{"id": 1}, nil
		})
	}
	require.Len(t, repo.records, 3)
	for i, record := range repo.records {
		require.Equal(t, service.HashIdempotencyKey(operationIDs[i]), record.IdempotencyKeyHash)
	}
	require.NotEqual(t, operationIDs[0], operationIDs[1])
	require.NotEqual(t, operationIDs[0], operationIDs[2])
}
