package httputil

import (
	"bufio"
	"context"
	"errors"
	"io"
	"net"
	"net/http"
	"os"
	"sync/atomic"
	"time"
)

type bodyReadTimeoutContextKey struct{}

// WithBodyReadTimeout bounds each socket read while receiving a body. It does
// not set a whole-request deadline or limit the duration of an SSE response.
func WithBodyReadTimeout(next http.Handler, timeout time.Duration) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Body != nil && r.Body != http.NoBody {
			body := &bodyReadTimeout{ReadCloser: r.Body, controller: http.NewResponseController(w), timeout: timeout}
			r.Body = body
			r = r.WithContext(context.WithValue(r.Context(), bodyReadTimeoutContextKey{}, body))
			if r.ProtoMajor == 1 {
				writer := &bodyDrainTimeoutWriter{ResponseWriter: w, body: body}
				defer func() {
					if !writer.hijacked.Load() {
						// net/http closes its original body after ServeHTTP returns,
						// bypassing our Read and Close methods.
						body.startDrain()
					}
				}()
				next.ServeHTTP(writer, r)
				return
			}
		}
		next.ServeHTTP(w, r)
	})
}

type bodyReadTimeout struct {
	io.ReadCloser
	controller       *http.ResponseController
	timeout          time.Duration
	finished         atomic.Bool
	failed           atomic.Bool
	absoluteDeadline atomic.Int64
}

// LimitRequestBodyReadDeadline adds an absolute deadline to the existing idle
// timeout, including reads made through MaxBytesReader and implicit HTTP/1
// drains. Configure it before consuming the body. A zero deadline clears the
// additional limit after a successful upload, before long-running processing.
// False means the server does not provide controllable body read deadlines.
func LimitRequestBodyReadDeadline(r *http.Request, deadline time.Time) bool {
	if r == nil {
		return false
	}
	body, _ := r.Context().Value(bodyReadTimeoutContextKey{}).(*bodyReadTimeout)
	if body == nil {
		body, _ = r.Body.(*bodyReadTimeout)
	}
	if body == nil {
		return false
	}
	if deadline.IsZero() {
		body.absoluteDeadline.Store(0)
		if body.failed.Load() {
			return true // A failed read must retain its deadline for the final drain.
		}
		return body.controller.SetReadDeadline(time.Time{}) == nil
	}
	next := deadline.UnixNano()
	for {
		previous := body.absoluteDeadline.Load()
		if previous != 0 && previous <= next {
			next = previous
			break
		}
		if body.absoluteDeadline.CompareAndSwap(previous, next) {
			break
		}
	}
	return body.controller.SetReadDeadline(time.Unix(0, next)) == nil
}

func (r *bodyReadTimeout) readDeadline() time.Time {
	var deadline time.Time
	if r.timeout > 0 {
		deadline = time.Now().Add(r.timeout)
	}
	if absolute := r.absoluteDeadline.Load(); absolute != 0 && (deadline.IsZero() || absolute < deadline.UnixNano()) {
		return time.Unix(0, absolute)
	}
	return deadline
}

func (r *bodyReadTimeout) absoluteDeadlineExpired() bool {
	absolute := r.absoluteDeadline.Load()
	return absolute != 0 && time.Now().UnixNano() >= absolute
}

func (r *bodyReadTimeout) Read(p []byte) (int, error) {
	deadline := r.readDeadline()
	if !deadline.IsZero() {
		err := r.controller.SetReadDeadline(deadline)
		if err != nil && !errors.Is(err, http.ErrNotSupported) {
			return 0, err
		}
	}
	if r.absoluteDeadlineExpired() {
		r.failed.Store(true)
		return 0, os.ErrDeadlineExceeded
	}
	n, readErr := r.ReadCloser.Read(p)
	if errors.Is(readErr, io.EOF) {
		r.finished.Store(true)
	} else if readErr != nil {
		r.failed.Store(true)
	}
	if readErr == nil || errors.Is(readErr, io.EOF) {
		if r.absoluteDeadlineExpired() {
			r.failed.Store(true)
			_ = r.controller.SetReadDeadline(r.readDeadline())
			return n, os.ErrDeadlineExceeded
		}
		if !deadline.IsZero() {
			_ = r.controller.SetReadDeadline(time.Time{})
		}
	}
	// Keep a failed read's deadline: net/http may drain an unread HTTP/1 body
	// after the handler returns. Clearing a timeout here could block that drain.
	return n, readErr
}

func (r *bodyReadTimeout) Close() error {
	armed := r.startDrain()
	err := r.ReadCloser.Close()
	if err == nil {
		r.finished.Store(true)
	} else {
		r.failed.Store(true)
	}
	r.finishDrain(armed)
	return err
}

func (r *bodyReadTimeout) startDrain() bool {
	if r.finished.Load() || r.failed.Load() {
		return false
	}
	deadline := r.readDeadline()
	return !deadline.IsZero() && r.controller.SetReadDeadline(deadline) == nil
}

func (r *bodyReadTimeout) finishDrain(armed bool) {
	if armed && !r.failed.Load() {
		if r.absoluteDeadlineExpired() {
			r.failed.Store(true)
			_ = r.controller.SetReadDeadline(r.readDeadline())
		} else {
			_ = r.controller.SetReadDeadline(time.Time{})
		}
	}
}

// HTTP/1 can drain an unread request body during its first response write or
// flush as well as after the handler returns. These reads use net/http's original
// body, so guard the response operations too. HTTP/2 has no such implicit drain.
type bodyDrainTimeoutWriter struct {
	http.ResponseWriter
	body       *bodyReadTimeout
	hijacked   atomic.Bool
	fullDuplex atomic.Bool
}

func (w *bodyDrainTimeoutWriter) Unwrap() http.ResponseWriter { return w.ResponseWriter }

func (w *bodyDrainTimeoutWriter) startDrain() bool {
	return !w.fullDuplex.Load() && w.body.startDrain()
}

func (w *bodyDrainTimeoutWriter) Write(p []byte) (int, error) {
	armed := w.startDrain()
	defer w.body.finishDrain(armed)
	return w.ResponseWriter.Write(p)
}

func (w *bodyDrainTimeoutWriter) WriteString(s string) (int, error) {
	armed := w.startDrain()
	defer w.body.finishDrain(armed)
	return io.WriteString(w.ResponseWriter, s)
}

func (w *bodyDrainTimeoutWriter) WriteHeader(status int) {
	armed := w.startDrain()
	defer w.body.finishDrain(armed)
	w.ResponseWriter.WriteHeader(status)
}

func (w *bodyDrainTimeoutWriter) FlushError() error {
	armed := w.startDrain()
	defer w.body.finishDrain(armed)
	return http.NewResponseController(w.ResponseWriter).Flush()
}

func (w *bodyDrainTimeoutWriter) Flush() { _ = w.FlushError() }

func (w *bodyDrainTimeoutWriter) Hijack() (net.Conn, *bufio.ReadWriter, error) {
	conn, rw, err := http.NewResponseController(w.ResponseWriter).Hijack()
	if err == nil {
		w.hijacked.Store(true)
		_ = conn.SetReadDeadline(time.Time{})
	}
	return conn, rw, err
}

func (w *bodyDrainTimeoutWriter) EnableFullDuplex() error {
	if err := http.NewResponseController(w.ResponseWriter).EnableFullDuplex(); err != nil {
		return err
	}
	w.fullDuplex.Store(true)
	return nil
}

func (w *bodyDrainTimeoutWriter) CloseNotify() <-chan bool {
	if notifier, ok := w.ResponseWriter.(http.CloseNotifier); ok { //nolint:staticcheck // Gin's ResponseWriter still requires legacy CloseNotify forwarding.
		return notifier.CloseNotify()
	}
	return make(chan bool)
}
