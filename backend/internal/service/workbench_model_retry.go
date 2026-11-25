package service

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"strings"
	"time"
)

const (
	workbenchModelMaxRetries = 5
	workbenchMaxRetryAfter   = 10 * time.Second
	workbenchMissingFinish   = "missing stream finish event"
	workbenchMissingStop     = "missing Anthropic message stop"
	workbenchInvalidCall     = "invalid function call"
)

func workbenchModelResponseFailure(writeErr, parseErr, callErr error) (code, reason string) {
	switch {
	case errors.Is(writeErr, context.Canceled), errors.Is(writeErr, ErrOfficeLeaseLost), writeErr == nil && errors.Is(callErr, context.Canceled):
		return "", ""
	case errors.Is(writeErr, io.ErrShortBuffer):
		return "MODEL_RESPONSE_TOO_LARGE", "response buffer limit"
	case errors.Is(writeErr, ErrOfficeBudgetExceeded):
		return "MODEL_PROGRESS_LIMIT", "run event budget exhausted"
	case errors.Is(writeErr, errOfficeGatewayPanic):
		return "MODEL_RESPONSE_WRITE_FAILED", "gateway handler panicked"
	case writeErr != nil:
		return "MODEL_RESPONSE_WRITE_FAILED", "could not publish model progress"
	case errors.Is(callErr, context.DeadlineExceeded):
		return "MODEL_STREAM_TIMEOUT", "model call deadline exceeded"
	}
	var parseFailure workbenchNativeParseFailure
	if !errors.As(parseErr, &parseFailure) {
		return "", ""
	}
	switch parseFailure.reason {
	case workbenchMissingFinish, workbenchMissingStop:
		return "MODEL_STREAM_FINISH_MISSING", parseFailure.reason
	case workbenchInvalidCall:
		return "MODEL_TOOL_CALL_INVALID", parseFailure.reason
	default:
		return "MODEL_STREAM_INVALID_RESPONSE", parseFailure.reason
	}
}

func isWorkbenchModelStreamReadError(code string) bool {
	switch strings.ToLower(code) {
	case OpenAIUpstreamHTTP2StreamErrorCode, OpenAIUpstreamStreamReadErrorCode, OpenAIUpstreamStreamTruncatedCode:
		return true
	}
	return false
}

func isWorkbenchRetryableModelFailure(reply *OfficeGatewayReply) bool {
	if reply.RetryAfterMillis > workbenchMaxRetryAfter.Milliseconds() {
		return false
	}
	if isWorkbenchModelStreamReadError(reply.ErrorCode) || strings.EqualFold(reply.IncompleteReason, "network_error") {
		return true
	}
	switch reply.ErrorCode {
	case "MODEL_STREAM_FINISH_MISSING", "MODEL_STREAM_TIMEOUT",
		"MODEL_UPSTREAM_RATE_LIMITED", "MODEL_UPSTREAM_UNAVAILABLE", "GATEWAY_QUEUE_FULL", "GATEWAY_CONCURRENCY_LIMIT":
		return true
	case "", "UPSTREAM_ERROR", "SERVER_ERROR", "API_ERROR", "RATE_LIMIT_ERROR":
		switch reply.Status {
		case http.StatusRequestTimeout, http.StatusTooManyRequests, http.StatusInternalServerError,
			http.StatusBadGateway, http.StatusServiceUnavailable, http.StatusGatewayTimeout:
			return true
		}
	}
	return false
}

func workbenchModelRetryAfter(header string, now time.Time) int64 {
	header = strings.TrimSpace(header)
	if header == "" {
		return 0
	}
	if seconds, err := strconv.ParseUint(header, 10, 64); err == nil {
		return int64(min(seconds, uint64(workbenchMaxRetryAfter/time.Second)+1)) * 1000
	}
	if date, err := http.ParseTime(header); err == nil {
		if date.After(now) {
			return date.Sub(now).Milliseconds()
		}
	}
	return 0
}

type workbenchModelRetry struct {
	failedOperation string
	attempt         int
	notBefore       time.Time
}

// The journal preserves one ID per billed attempt and a separate logical round
// index for tool IDs. Only a recorded failure can authorize another attempt.
func (state *workbenchBrowserState) modelAttempt(journal map[string]OfficeOperation, round int) (*OfficeGatewayReply, error) {
	state.NextModel = round
	state.ModelID = fmt.Sprintf("browser:model:%d", round)
	state.Retry = nil
	for attempt := 0; ; attempt++ {
		model := journal[state.ModelID]
		switch model.State {
		case "", OfficeOperationPrepared:
			return nil, nil
		case OfficeOperationDispatched, OfficeOperationUnknown:
			state.Uncertain = model.ID
			return nil, nil
		}
		var reply OfficeGatewayReply
		if model.Result == nil || json.Unmarshal(model.Result.Output, &reply) != nil {
			return nil, ErrOfficeResultUnknown
		}
		if !reply.Retryable || attempt >= workbenchModelMaxRetries {
			return &reply, nil
		}
		delay := time.Second << attempt
		if retryAfter := time.Duration(reply.RetryAfterMillis) * time.Millisecond; retryAfter > delay {
			delay = retryAfter
		}
		var notBefore time.Time
		if model.CompletedAt != nil {
			notBefore = model.CompletedAt.Add(delay)
		}
		state.Retry = &workbenchModelRetry{failedOperation: state.ModelID, attempt: attempt + 1, notBefore: notBefore}
		state.ModelID = fmt.Sprintf("browser:model:%d:retry:%d", round, attempt+1)
	}
}

func (s *OfficeAgentService) waitForWorkbenchModelRetry(ctx context.Context, lease OfficeRunLease, retry *workbenchModelRetry) error {
	payload, _ := json.Marshal(map[string]any{
		"operation_id": retry.failedOperation, "attempt": retry.attempt, "max_retries": workbenchModelMaxRetries,
	})
	if _, err := s.repo.AppendEvent(ctx, lease, "model-retry:"+retry.failedOperation, "model.retrying", payload); err != nil {
		return err
	}
	if delay := time.Until(retry.notBefore); delay > 0 {
		timer := time.NewTimer(delay)
		defer timer.Stop()
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-timer.C:
		}
	}
	return nil
}
