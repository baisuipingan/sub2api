package service

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"github.com/stretchr/testify/require"
	"image"
	"image/png"
	"net/http"
	"strings"
	"testing"
)

type officeStreamTestRepo struct {
	*officeBridgeTestRepo
	events []OfficeRunEvent
	err    error
}

func (r *officeStreamTestRepo) AppendEvent(_ context.Context, _ OfficeRunLease, id, kind string, data json.RawMessage) (*OfficeRunEvent, error) {
	if r.err != nil {
		return nil, r.err
	}
	e := OfficeRunEvent{ID: id, Type: kind, Payload: data}
	r.events = append(r.events, e)
	return &e, nil
}
func TestOfficeLiveStreamFragmentationAndPrivateFields(t *testing.T) {
	svc, base := officeBridgeFixture(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {}))
	repo := &officeStreamTestRepo{officeBridgeTestRepo: base}
	svc.repo = repo
	stream := &officeLiveStream{ctx: context.Background(), service: svc, lease: base.lease, operation: "model-1"}
	input := "data: {\"choices\":[{\"delta\":{\"content\":\"你好😀\",\"reasoning_content\":\"不公开\"}}]}\r\n\r\ndata: {\"choices\":[{\"delta\":{\"content\":\"世界\",\"tool_calls\":[{\"function\":{\"arguments\":\"secret\"}}]}}]}\n\ndata: [DONE]\n\n"
	for _, b := range []byte(input) {
		require.NoError(t, stream.write([]byte{b}))
	}
	require.NoError(t, stream.flush())
	var out strings.Builder
	for _, e := range repo.events {
		var p map[string]string
		require.NoError(t, json.Unmarshal(e.Payload, &p))
		out.WriteString(p["text"])
		require.Equal(t, "assistant.delta", e.Type)
		require.NotContains(t, string(e.Payload), "secret")
		require.NotContains(t, string(e.Payload), "不公开")
	}
	require.Equal(t, "你好😀世界", out.String())
	// Several complete frames in one network write can use different newline styles.
	repo.events = nil
	stream = &officeLiveStream{ctx: context.Background(), service: svc, lease: base.lease, operation: "model-2"}
	require.NoError(t, stream.write([]byte(input)))
	require.NoError(t, stream.flush())
	out.Reset()
	for _, e := range repo.events {
		var p map[string]string
		require.NoError(t, json.Unmarshal(e.Payload, &p))
		out.WriteString(p["text"])
	}
	require.Equal(t, "你好😀世界", out.String())
}
func TestOfficeLiveStreamBoundedUTF8AndLeaseFailure(t *testing.T) {
	svc, base := officeBridgeFixture(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {}))
	repo := &officeStreamTestRepo{officeBridgeTestRepo: base}
	svc.repo = repo
	stream := &officeLiveStream{ctx: context.Background(), service: svc, lease: base.lease, operation: "model-1"}
	text := strings.Repeat("中文😀", 9000)
	frame, _ := json.Marshal(map[string]any{"choices": []any{map[string]any{"delta": map[string]string{"content": text}}}})
	require.NoError(t, stream.write(append(append([]byte("data: "), frame...), []byte("\n\n")...)))
	var got strings.Builder
	for _, e := range repo.events {
		require.Less(t, len(e.Payload), 32<<10)
		var p map[string]string
		require.NoError(t, json.Unmarshal(e.Payload, &p))
		got.WriteString(p["text"])
	}
	require.Equal(t, text, got.String())
	repo.err = ErrOfficeLeaseLost
	stream.text.WriteString("late")
	require.ErrorIs(t, stream.flush(), ErrOfficeLeaseLost)
	stream.pending = strings.Repeat("x", 256<<10)
	require.ErrorIs(t, stream.write([]byte("x")), ErrOfficeBudgetExceeded)
}
func TestOfficeVisionBridgeAcceptsOnlyBoundedInlineImages(t *testing.T) {
	var data bytes.Buffer
	require.NoError(t, png.Encode(&data, image.NewRGBA(image.Rect(0, 0, 2, 2))))
	run := OfficeRun{Model: "vision", Mode: "office", Parameters: json.RawMessage(`{"supports_vision":true}`), Limits: OfficeRunLimits{MaxOutputTokens: 100}}
	body := func(url string) []byte {
		b, _ := json.Marshal(map[string]any{"model": "vision", "messages": []any{map[string]any{"role": "user", "content": []any{map[string]string{"type": "text", "text": "检查"}, map[string]any{"type": "image_url", "image_url": map[string]string{"url": url}}}}}, "max_tokens": 100})
		return b
	}
	valid := "data:image/png;base64," + base64.StdEncoding.EncodeToString(data.Bytes())
	_, e := normalizeOfficeModelRequest(body(valid), run)
	require.NoError(t, e)
	for _, url := range []string{"http://127.0.0.1/secret", "https://example.com/img.png", "data:image/jpeg;base64," + base64.StdEncoding.EncodeToString(data.Bytes()), "data:image/png;base64,bad"} {
		_, e = normalizeOfficeModelRequest(body(url), run)
		require.ErrorIs(t, e, ErrOfficeInvalid)
	}
	run.Mode = "chat"
	_, e = normalizeOfficeModelRequest(body(valid), run)
	require.ErrorIs(t, e, ErrOfficeInvalid)
	run.Mode = "office"
	run.Parameters = nil
	_, e = normalizeOfficeModelRequest(body(valid), run)
	require.ErrorIs(t, e, ErrOfficeInvalid)
}
func TestOfficeMessageTextLegacyAndResourceProjection(t *testing.T) {
	for _, raw := range []string{`"你好"`, `{"text":"你好","artifacts":[{"version_id":"a"}]}`} {
		value, e := officeMessageText(json.RawMessage(raw))
		require.NoError(t, e)
		require.Equal(t, "你好", value)
	}
	_, e := officeMessageText(json.RawMessage(`{"artifacts":[]}`))
	require.True(t, errors.Is(e, ErrOfficeInvalid))
}
