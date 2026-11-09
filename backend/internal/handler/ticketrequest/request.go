// Package ticketrequest shares bounded ticket submission parsing between the
// user and administrator handlers. Multipart boundaries are never part of the
// idempotency fingerprint.
package ticketrequest

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"mime"
	"net"
	"net/http"
	"strconv"
	"strings"
	"sync/atomic"
	"time"

	infraerrors "github.com/Wei-Shaw/sub2api/internal/pkg/errors"
	pkghttputil "github.com/Wei-Shaw/sub2api/internal/pkg/httputil"
	"github.com/Wei-Shaw/sub2api/internal/pkg/response"
	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/gin-gonic/gin"
)

const (
	maxFileBytes    = 2 << 20
	maxRequestBytes = 7 << 20
	maxJSONBytes    = 32 << 10
)

type Request struct {
	Title    string                           `json:"title"`
	Content  string                           `json:"content"`
	Priority string                           `json:"priority"`
	Uploads  []service.TicketAttachmentUpload `json:"-"`
	key      string
}

var errInvalid = infraerrors.BadRequest("TICKET_ATTACHMENT_INVALID", "invalid ticket submission or screenshot format")
var errTooLarge = infraerrors.New(http.StatusRequestEntityTooLarge, "TICKET_ATTACHMENT_TOO_LARGE", "each screenshot must be at most 2 MiB; total attachments must be at most 6 MiB")

func IsMultipart(c *gin.Context) bool {
	mediaType, _, _ := mime.ParseMediaType(c.GetHeader("Content-Type"))
	return mediaType == "multipart/form-data"
}

// Parse streams multipart fields without creating temporary files. Images are
// bounded before decoding and text-only JSON clients remain compatible.
func Parse(c *gin.Context) (Request, error) {
	return parse(c, 30*time.Second)
}

func parse(c *gin.Context, uploadTimeout time.Duration) (req Request, err error) {
	key, err := service.NormalizeIdempotencyKey(c.GetHeader("Idempotency-Key"))
	if err != nil {
		return req, err
	}
	req.key = key
	mediaType, _, _ := mime.ParseMediaType(c.GetHeader("Content-Type"))
	// The previous handlers always decoded JSON, including clients that omitted
	// its content type. Keep that bounded compatibility for non-file requests.
	if mediaType != "multipart/form-data" {
		c.Request.Body = http.MaxBytesReader(c.Writer, c.Request.Body, maxJSONBytes)
		decoder := json.NewDecoder(c.Request.Body)
		if err := decoder.Decode(&req); err != nil {
			return req, parseError(err)
		}
		if err := decoder.Decode(new(any)); err != io.EOF {
			return req, errInvalid
		}
		return req, nil
	}
	if key == "" {
		return req, service.ErrIdempotencyKeyRequired
	}
	// Use the existing socket-read wrapper so its idle deadline cannot override
	// this total upload deadline. Close is only a fallback for custom bodies.
	body := c.Request.Body
	absolute := time.Now().Add(uploadTimeout)
	bounded := pkghttputil.LimitRequestBodyReadDeadline(c.Request, absolute)
	controller := http.NewResponseController(c.Writer)
	if !bounded {
		bounded = controller.SetReadDeadline(absolute) == nil
	}
	var state atomic.Int32 // 0 reading, 1 finished, 2 timed out
	timer := time.AfterFunc(uploadTimeout, func() {
		if state.CompareAndSwap(0, 2) {
			if !bounded {
				_ = body.Close()
			}
		}
	})
	defer func() {
		state.CompareAndSwap(0, 1)
		timer.Stop()
		var networkErr net.Error
		if state.Load() == 2 || (errors.As(err, &networkErr) && networkErr.Timeout()) {
			err = infraerrors.New(http.StatusRequestTimeout, "TICKET_UPLOAD_TIMEOUT", "screenshot upload timed out; please retry")
		} else if err == nil {
			pkghttputil.LimitRequestBodyReadDeadline(c.Request, time.Time{})
			_ = controller.SetReadDeadline(time.Time{})
		}
	}()
	c.Request.Body = http.MaxBytesReader(c.Writer, c.Request.Body, maxRequestBytes)
	reader, err := c.Request.MultipartReader()
	if err != nil {
		return req, errInvalid
	}
	seen := make(map[string]bool)
	for {
		part, err := reader.NextPart()
		if err == io.EOF {
			break
		}
		if err != nil {
			return req, parseError(err)
		}
		name := part.FormName()
		if name == "images" {
			if len(req.Uploads) >= 3 {
				return req, infraerrors.BadRequest("TICKET_ATTACHMENT_TOO_MANY", "at most 3 screenshots per message")
			}
			if part.FileName() == "" {
				return req, errInvalid
			}
			data, err := io.ReadAll(io.LimitReader(part, maxFileBytes+1))
			if err != nil {
				return req, parseError(err)
			}
			if len(data) > maxFileBytes {
				return req, errTooLarge
			}
			if len(data) == 0 {
				return req, errInvalid
			}
			req.Uploads = append(req.Uploads, service.TicketAttachmentUpload{Data: data})
		} else {
			limit := 20000
			switch name {
			case "title":
				limit = 480
			case "priority":
				limit = 32
			case "content":
			default:
				return req, errInvalid
			}
			if seen[name] || part.FileName() != "" {
				return req, errInvalid
			}
			seen[name] = true
			data, err := io.ReadAll(io.LimitReader(part, int64(limit+1)))
			if err != nil {
				return req, parseError(err)
			}
			if len(data) > limit {
				return req, errInvalid
			}
			switch name {
			case "title":
				req.Title = string(data)
			case "content":
				req.Content = string(data)
			case "priority":
				req.Priority = string(data)
			}
		}
		if err := part.Close(); err != nil {
			return req, parseError(err)
		}
	}
	return req, nil
}

func parseError(err error) error {
	var networkErr net.Error
	if errors.As(err, &networkErr) && networkErr.Timeout() {
		return infraerrors.New(http.StatusRequestTimeout, "TICKET_UPLOAD_TIMEOUT", "screenshot upload timed out; please retry")
	}
	var tooLarge *http.MaxBytesError
	if errors.As(err, &tooLarge) {
		return errTooLarge
	}
	return errInvalid
}

func (r Request) fingerprint(ticketID int64) any {
	digests := make([]string, 0, len(r.Uploads))
	for _, upload := range r.Uploads {
		sum := sha256.Sum256(upload.Data)
		digests = append(digests, hex.EncodeToString(sum[:]))
	}
	return struct {
		TicketID int64    `json:"ticket_id"`
		Title    string   `json:"title"`
		Content  string   `json:"content"`
		Priority string   `json:"priority"`
		Images   []string `json:"images"`
	}{ticketID, strings.TrimSpace(r.Title), strings.TrimSpace(r.Content), r.Priority, digests}
}

// Execute reuses the existing coordinator and also passes a stable operation ID
// to the ticket transaction, covering a crash after commit but before the
// coordinator records its response. Old JSON clients may omit a key.
func Execute(c *gin.Context, actor, scope string, ticketID int64, req Request, execute func(context.Context, string) (any, error)) {
	opID := ""
	if req.key != "" {
		opID = service.HashIdempotencyKey(actor + "\n" + scope + "\n" + strconv.FormatInt(ticketID, 10) + "\n" + req.key)
	}
	callback := func(ctx context.Context) (any, error) { return execute(ctx, opID) }
	coordinator := service.DefaultIdempotencyCoordinator()
	if coordinator == nil || req.key == "" {
		data, err := callback(c.Request.Context())
		if err != nil {
			response.ErrorFrom(c, err)
			return
		}
		response.Created(c, data)
		return
	}
	result, err := coordinator.Execute(c.Request.Context(), service.IdempotencyExecuteOptions{
		Scope: scope, ActorScope: actor, Method: c.Request.Method, Route: c.FullPath(),
		IdempotencyKey: opID, Payload: req.fingerprint(ticketID), RequireKey: true,
		TTL: service.DefaultWriteIdempotencyTTL(), ExecutionTimeout: 90 * time.Second,
	}, callback)
	if err != nil {
		if retry := service.RetryAfterSecondsFromError(err); retry > 0 {
			c.Header("Retry-After", strconv.Itoa(retry))
		}
		response.ErrorFrom(c, err)
		return
	}
	if result.Replayed {
		c.Header("X-Idempotency-Replayed", "true")
	}
	response.Created(c, result.Data)
}
