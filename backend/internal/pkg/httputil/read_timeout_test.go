package httputil

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func TestAbsoluteBodyReadDeadlineLimitsTrickledHTTP1AndDrain(t *testing.T) {
	for _, idle := range []time.Duration{0, 2 * time.Second} {
		t.Run(idle.String(), func(t *testing.T) { absoluteBodyReadDeadlineHTTP1(t, idle) })
	}
}

func absoluteBodyReadDeadlineHTTP1(t *testing.T, idle time.Duration) {
	readDone := make(chan error, 1)
	closed := make(chan struct{}, 1)
	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !LimitRequestBodyReadDeadline(r, time.Now().Add(150*time.Millisecond)) {
			readDone <- errors.New("absolute body deadline unavailable behind MaxBytesReader")
			return
		}
		_, err := io.ReadAll(r.Body)
		w.WriteHeader(http.StatusRequestTimeout)
		readDone <- err
	})
	srv := httptest.NewUnstartedServer(WithBodyReadTimeout(http.MaxBytesHandler(handler, 7<<20), idle))
	srv.Config.ConnState = func(_ net.Conn, state http.ConnState) {
		if state == http.StateClosed {
			select {
			case closed <- struct{}{}:
			default:
			}
		}
	}
	srv.Start()
	defer srv.Close()
	conn, err := net.Dial("tcp", srv.Listener.Addr().String())
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = conn.Close() }()
	_, err = io.WriteString(conn, "POST / HTTP/1.1\r\nHost: test\r\nTransfer-Encoding: chunked\r\n\r\n1\r\nx\r\n")
	if err != nil {
		t.Fatal(err)
	}
	stop := make(chan struct{})
	defer close(stop)
	go func() {
		ticker := time.NewTicker(30 * time.Millisecond)
		defer ticker.Stop()
		for {
			select {
			case <-ticker.C:
				if _, err := io.WriteString(conn, "1\r\nx\r\n"); err != nil {
					return
				}
			case <-stop:
				return
			}
		}
	}()
	select {
	case err := <-readDone:
		var timeout net.Error
		if !errors.As(err, &timeout) || !timeout.Timeout() {
			t.Fatalf("want absolute upload timeout, got %v", err)
		}
	case <-time.After(time.Second):
		t.Fatal("trickled upload exceeded its absolute deadline or stalled the response drain")
	}
	select {
	case <-closed:
	case <-time.After(time.Second):
		t.Fatal("absolute timeout did not bound HTTP/1 implicit drain")
	}
}

func TestAbsoluteBodyReadDeadlineHTTP2KeepsOtherStreamsAvailable(t *testing.T) {
	readDone := make(chan error, 1)
	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/slow" {
			w.WriteHeader(http.StatusNoContent)
			return
		}
		if !LimitRequestBodyReadDeadline(r, time.Now().Add(150*time.Millisecond)) {
			readDone <- errors.New("HTTP/2 body deadline unavailable")
			return
		}
		_, err := io.ReadAll(r.Body)
		w.WriteHeader(http.StatusRequestTimeout)
		readDone <- err
	})
	srv := httptest.NewUnstartedServer(WithBodyReadTimeout(http.MaxBytesHandler(handler, 7<<20), 2*time.Second))
	srv.EnableHTTP2 = true
	srv.StartTLS()
	defer srv.Close()
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	reader, writer := io.Pipe()
	defer func() { _ = writer.Close() }()
	request, _ := http.NewRequestWithContext(ctx, http.MethodPost, srv.URL+"/slow", reader)
	clientDone := make(chan struct{})
	go func() {
		defer close(clientDone)
		resp, err := srv.Client().Do(request)
		if err == nil {
			_ = resp.Body.Close()
		}
	}()
	go func() {
		ticker := time.NewTicker(30 * time.Millisecond)
		defer ticker.Stop()
		for {
			select {
			case <-ticker.C:
				if _, err := writer.Write([]byte("x")); err != nil {
					return
				}
			case <-ctx.Done():
				return
			}
		}
	}()
	defer func() { cancel(); _ = writer.Close(); <-clientDone }()
	select {
	case err := <-readDone:
		var timeout net.Error
		if !errors.As(err, &timeout) || !timeout.Timeout() {
			t.Fatalf("want absolute HTTP/2 upload timeout, got %v", err)
		}
	case <-time.After(time.Second):
		t.Fatal("trickled HTTP/2 upload retained handler")
	}
	fast, _ := http.NewRequestWithContext(ctx, http.MethodGet, srv.URL+"/fast", nil)
	resp, err := srv.Client().Do(fast)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.ProtoMajor != 2 || resp.StatusCode != http.StatusNoContent {
		t.Fatalf("other HTTP/2 stream failed: %s %s", resp.Proto, resp.Status)
	}
}

func TestAbsoluteBodyReadDeadlineCanBeClearedForProcessing(t *testing.T) {
	handlerErr := make(chan error, 1)
	srv := httptest.NewServer(WithBodyReadTimeout(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !LimitRequestBodyReadDeadline(r, time.Now().Add(50*time.Millisecond)) {
			handlerErr <- errors.New("body deadline unavailable")
			return
		}
		if _, err := io.ReadFull(r.Body, make([]byte, 1)); err != nil {
			handlerErr <- err
			return
		}
		if !LimitRequestBodyReadDeadline(r, time.Time{}) {
			handlerErr <- errors.New("body deadline could not be cleared")
			return
		}
		rest, err := io.ReadAll(r.Body)
		if err == nil && string(rest) != "tail" {
			err = fmt.Errorf("unexpected body remainder %q", rest)
		}
		handlerErr <- err
		_, _ = io.WriteString(w, "ok")
	}), time.Second))
	defer srv.Close()
	reader, writer := io.Pipe()
	go func() {
		defer func() { _ = writer.Close() }()
		_, _ = writer.Write([]byte("x"))
		time.Sleep(100 * time.Millisecond)
		_, _ = writer.Write([]byte("tail"))
	}()
	resp, err := srv.Client().Post(srv.URL, "application/octet-stream", reader)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = resp.Body.Close() }()
	if err := <-handlerErr; err != nil {
		t.Fatalf("cleared upload deadline still applied: %v", err)
	}
}

func TestBodyReadTimeoutReleasesStalledUpload(t *testing.T) {
	readDone := make(chan error, 1)
	srv := httptest.NewServer(WithBodyReadTimeout(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, err := io.ReadAll(r.Body)
		readDone <- err
	}), 100*time.Millisecond))
	defer srv.Close()
	conn, err := net.Dial("tcp", srv.Listener.Addr().String())
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = conn.Close() }()
	if _, err := io.WriteString(conn, "POST / HTTP/1.1\r\nHost: test\r\nContent-Length: 1024\r\n\r\nx"); err != nil {
		t.Fatal(err)
	}
	select {
	case err := <-readDone:
		var timeout net.Error
		if !errors.As(err, &timeout) || !timeout.Timeout() {
			t.Fatalf("want upload timeout, got %v", err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("stalled upload retained handler")
	}
}

func TestBodyReadTimeoutClosesStalledHTTP1Connection(t *testing.T) {
	closed := make(chan struct{}, 1)
	handlerDone := make(chan struct{})
	srv := httptest.NewUnstartedServer(WithWriteTimeout(WithBodyReadTimeout(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if _, err := io.ReadAll(r.Body); err != nil {
			w.WriteHeader(http.StatusBadRequest)
		}
		close(handlerDone)
	}), 100*time.Millisecond), 100*time.Millisecond))
	srv.Config.ConnState = func(_ net.Conn, state http.ConnState) {
		if state == http.StateClosed {
			select {
			case closed <- struct{}{}:
			default:
			}
		}
	}
	srv.Start()
	defer srv.Close()
	conn, err := net.Dial("tcp", srv.Listener.Addr().String())
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = conn.Close() }()
	// Keep the remainder small enough for net/http's automatic post-handler
	// drain. Clearing the failed read's deadline would strand that drain even
	// though the application handler itself has already returned.
	if _, err := io.WriteString(conn, "POST / HTTP/1.1\r\nHost: test\r\nContent-Length: 1024\r\n\r\nx"); err != nil {
		t.Fatal(err)
	}
	select {
	case <-handlerDone:
	case <-time.After(2 * time.Second):
		t.Fatal("stalled upload retained handler")
	}
	select {
	case <-closed:
	case <-time.After(2 * time.Second):
		t.Fatal("timed-out upload retained the connection while net/http drained the unread body")
	}
}

func TestBodyReadTimeoutClosesRejectedHTTP1Upload(t *testing.T) {
	for _, partialRead := range []bool{false, true} {
		for _, response := range []string{"buffered", "flush", "large_write", "close_body"} {
			name := response + "/unread"
			if partialRead {
				name = response + "/partial_read"
			}
			t.Run(name, func(t *testing.T) {
				handlerDone := make(chan error, 1)
				closed := make(chan struct{}, 1)
				handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
					if partialRead {
						if _, err := io.ReadFull(r.Body, make([]byte, 1)); err != nil {
							handlerDone <- err
							return
						}
					}
					ctx, release := WithRequestBodyMemoryBudget(r.Context(), NewBodyMemoryBudget(1))
					defer release()
					_, err := ReadRequestBodyWithPrealloc(r.WithContext(ctx))
					if !errors.Is(err, ErrRequestBodyMemoryBudget) {
						handlerDone <- errors.New("request did not exhaust its body budget")
						return
					}
					w.WriteHeader(http.StatusServiceUnavailable)
					switch response {
					case "flush":
						_ = http.NewResponseController(w).Flush()
					case "large_write":
						_, _ = w.Write([]byte(strings.Repeat("x", 8192)))
					case "close_body":
						_ = r.Body.Close()
					default:
						_, _ = io.WriteString(w, "busy")
					}
					handlerDone <- nil
				})
				srv := httptest.NewUnstartedServer(WithWriteTimeout(WithBodyReadTimeout(handler, 50*time.Millisecond), 100*time.Millisecond))
				srv.Config.ConnState = func(_ net.Conn, state http.ConnState) {
					if state == http.StateClosed {
						select {
						case closed <- struct{}{}:
						default:
						}
					}
				}
				srv.Start()
				defer srv.Close()
				conn, err := net.Dial("tcp", srv.Listener.Addr().String())
				if err != nil {
					t.Fatal(err)
				}
				defer func() { _ = conn.Close() }()
				if _, err := io.WriteString(conn, "POST / HTTP/1.1\r\nHost: test\r\nContent-Length: 1024\r\n\r\nx"); err != nil {
					t.Fatal(err)
				}
				select {
				case err := <-handlerDone:
					if err != nil {
						t.Fatal(err)
					}
				case <-time.After(2 * time.Second):
					t.Fatal("rejection blocked while writing the response or closing the body")
				}
				select {
				case <-closed:
				case <-time.After(2 * time.Second):
					t.Fatal("rejected upload retained its HTTP/1 connection")
				}
			})
		}
	}
}

func TestBodyReadTimeoutDoesNotLimitHTTP1ResponseLifetime(t *testing.T) {
	for _, readAll := range []bool{false, true} {
		name := "implicit_drain"
		if readAll {
			name = "complete_body"
		}
		t.Run(name, func(t *testing.T) {
			handlerErr := make(chan error, 1)
			srv := httptest.NewServer(WithBodyReadTimeout(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				var err error
				if readAll {
					_, err = io.Copy(io.Discard, r.Body)
				} else {
					_, err = io.ReadFull(r.Body, make([]byte, 1))
				}
				if err != nil {
					handlerErr <- err
					return
				}
				_, _ = io.WriteString(w, "first\n")
				if err = http.NewResponseController(w).Flush(); err != nil {
					handlerErr <- err
					return
				}
				// The first flush implicitly consumes any unread body. Its read
				// deadline must not cancel the response during upstream thinking.
				timer := time.NewTimer(300 * time.Millisecond)
				defer timer.Stop()
				select {
				case <-r.Context().Done():
					handlerErr <- r.Context().Err()
					return
				case <-timer.C:
				}
				_, err = io.WriteString(w, "last\n")
				handlerErr <- err
			}), 100*time.Millisecond))
			defer srv.Close()
			client := srv.Client()
			client.Timeout = 3 * time.Second
			resp, err := client.Post(srv.URL, "application/json", strings.NewReader(`{"input":"hello"}`))
			if err != nil {
				t.Fatal(err)
			}
			defer func() { _ = resp.Body.Close() }()
			body, err := io.ReadAll(resp.Body)
			if err != nil || string(body) != "first\nlast\n" {
				t.Fatalf("response was truncated: %q, %v", body, err)
			}
			if err := <-handlerErr; err != nil {
				t.Fatalf("response lifetime was limited by the upload deadline: %v", err)
			}
		})
	}
}

func TestBodyReadTimeoutHTTP2DoesNotCancelOtherStreams(t *testing.T) {
	readDone := make(chan error, 1)
	srv := httptest.NewUnstartedServer(WithBodyReadTimeout(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/slow" {
			_, err := io.ReadAll(r.Body)
			readDone <- err
			return
		}
		w.WriteHeader(http.StatusNoContent)
	}), 100*time.Millisecond))
	srv.EnableHTTP2 = true
	srv.StartTLS()
	defer srv.Close()
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	reader, writer := io.Pipe()
	defer func() { _ = writer.Close() }()
	slow, _ := http.NewRequestWithContext(ctx, http.MethodPost, srv.URL+"/slow", reader)
	clientDone := make(chan struct{})
	go func() {
		defer close(clientDone)
		resp, err := srv.Client().Do(slow)
		if err == nil {
			_ = resp.Body.Close()
		}
	}()
	defer func() {
		cancel()
		_ = writer.Close()
		<-clientDone
	}()
	select {
	case err := <-readDone:
		if err == nil {
			t.Fatal("stalled HTTP/2 upload accepted")
		}
	case <-ctx.Done():
		t.Fatal("stalled HTTP/2 upload retained handler")
	}
	fast, _ := http.NewRequestWithContext(ctx, http.MethodGet, srv.URL+"/fast", nil)
	resp, err := srv.Client().Do(fast)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.ProtoMajor != 2 || resp.StatusCode != http.StatusNoContent {
		t.Fatalf("other stream failed: %s %s", resp.Proto, resp.Status)
	}
}
