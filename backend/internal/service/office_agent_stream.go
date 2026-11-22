package service

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"
	"unicode/utf8"
)

// This parser exposes only assistant text, never hidden reasoning or raw tool
// arguments. Writes are synchronous and bounded, providing backpressure.
type officeLiveStream struct {
	native    *workbenchNativeStream
	ctx       context.Context
	service   *OfficeAgentService
	lease     OfficeRunLease
	operation string
	pending   string
	text      strings.Builder
	sequence  int
	last      time.Time
}

func (s *officeLiveStream) write(data []byte) error {
	if s.native != nil {
		delta, err := s.native.Write(data)
		var upstream *workbenchNativeUpstreamError
		if err != nil && !errors.As(err, &upstream) {
			return err
		}
		return s.writeNativeDelta(delta)
	}
	s.pending += string(data)
	if len(s.pending) > 256<<10 {
		return ErrOfficeBudgetExceeded
	}
	for {
		index := strings.Index(s.pending, "\n\n")
		sep := 2
		crlf := strings.Index(s.pending, "\r\n\r\n")
		if crlf >= 0 && (index < 0 || crlf < index) {
			index = crlf
			sep = 4
		}
		if index < 0 {
			break
		}
		frame := s.pending[:index]
		s.pending = s.pending[index+sep:]
		var lines []string
		for _, line := range strings.Split(strings.ReplaceAll(frame, "\r\n", "\n"), "\n") {
			if strings.HasPrefix(line, "data:") {
				lines = append(lines, strings.TrimPrefix(strings.TrimPrefix(line, "data:"), " "))
			}
		}
		value := strings.Join(lines, "\n")
		if value == "[DONE]" {
			return s.flush()
		}
		if value == "" {
			continue
		}
		var v struct {
			Choices []struct {
				Delta struct {
					Content string `json:"content"`
				} `json:"delta"`
			} `json:"choices"`
		}
		if json.Unmarshal([]byte(value), &v) != nil {
			continue
		}
		for _, c := range v.Choices {
			_, _ = s.text.WriteString(c.Delta.Content)
		}
		if s.text.Len() >= 256 || time.Since(s.last) > 100*time.Millisecond {
			if e := s.flush(); e != nil {
				return e
			}
		}
	}
	return nil
}
func (s *officeLiveStream) flush() error {
	if s.text.Len() == 0 {
		return nil
	}

	text := s.text.String()
	for len(text) > 0 {
		size := min(len(text), 16<<10)
		for size < len(text) && !utf8.RuneStart(text[size]) {
			size--
		}
		if size == 0 || !utf8.ValidString(text[:size]) {
			return ErrOfficeInvalid
		}
		payload, _ := json.Marshal(map[string]string{"text": text[:size], "operation_id": s.operation})
		id := fmt.Sprintf("delta:%s:%d", officeDigest([]byte(s.operation))[:32], s.sequence)
		if _, e := s.service.repo.AppendEvent(s.ctx, s.lease, id, "assistant.delta", payload); e != nil {
			return e
		}
		s.sequence++
		text = text[size:]
	}
	s.text.Reset()
	s.last = time.Now()
	return nil
}
