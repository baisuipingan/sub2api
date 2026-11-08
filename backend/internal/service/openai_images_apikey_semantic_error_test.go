package service

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"github.com/tidwall/gjson"
)

func TestAPIKeyImagesNonstreamSemanticFailuresHaveNoBillableResult(t *testing.T) {
	for _, tc := range []struct {
		name, body, code string
		status           int
	}{
		{
			name: "moderation refusal inside HTTP 200",
			body: `{"error":{"message":"Your request was rejected by the safety system. safety_violations=[sexual].","type":"image_generation_user_error","code":"moderation_blocked"}}`,
			code: "moderation_blocked", status: http.StatusBadRequest,
		},
		{
			name: "invalid request inside HTTP 200",
			body: `{"error":{"type":"invalid_request_error","message":"invalid size","code":"invalid_size"}}`,
			code: "invalid_size", status: http.StatusBadRequest,
		},
		{
			name: "server error inside HTTP 200",
			body: `{"error":{"type":"server_error","message":"temporarily unavailable","code":"server_error"}}`,
			code: "server_error", status: http.StatusBadGateway,
		},
		{name: "invalid JSON", body: `not JSON`, code: "upstream_invalid_response", status: http.StatusBadGateway},
		{name: "empty body", body: ``, code: "upstream_invalid_response", status: http.StatusBadGateway},
		{name: "empty result", body: `{}`, code: "upstream_no_image", status: http.StatusBadGateway},
		{name: "empty data", body: `{"data":[]}`, code: "upstream_no_image", status: http.StatusBadGateway},
		{name: "empty image fields", body: `{"data":[{"url":"  ","b64_json":""}]}`, code: "upstream_no_image", status: http.StatusBadGateway},
		{name: "unrecognized image item", body: `{"data":[{"revised_prompt":"none"}]}`, code: "upstream_no_image", status: http.StatusBadGateway},
		{name: "usage alone cannot fabricate an image", body: `{"data":[],"usage":{"images":1,"output_tokens":12}}`, code: "upstream_no_image", status: http.StatusBadGateway},
	} {
		for _, countBilling := range []bool{false, true} {
			t.Run(fmt.Sprintf("%s/count-billing=%t", tc.name, countBilling), func(t *testing.T) {
				requestBody := []byte(`{"model":"gpt-image-2.6-flare","prompt":"draw","n":3,"size":"2048x2048"}`)
				c, rec := newOpenAIImagesTestContext(t, requestBody)
				c.Set("api_key", &APIKey{ID: 42, Group: &Group{ImageCountBilling: countBilling}})
				upstream := &httpUpstreamRecorder{resp: &http.Response{
					StatusCode: http.StatusOK,
					Header:     http.Header{"Content-Type": {"application/json"}, "X-Request-Id": {"req_semantic_error"}},
					Body:       io.NopCloser(strings.NewReader(tc.body)),
				}}
				svc := newOpenAIImagesTestService(upstream)
				parsed, err := svc.ParseOpenAIImagesRequest(c, requestBody)
				require.NoError(t, err)
				result, err := svc.ForwardImages(context.Background(), c, newOpenAIImagesAPIKeyAccount(), requestBody, parsed, "")
				require.Nil(t, result, "no result may reach RecordUsage or the requested-n fallback")
				var responseErr *OpenAIImagesUpstreamError
				require.ErrorAs(t, err, &responseErr)
				require.Equal(t, tc.status, responseErr.StatusCode)
				require.Equal(t, tc.code, responseErr.Code)
				require.Equal(t, tc.status, rec.Code)
				require.Equal(t, tc.code, gjson.Get(rec.Body.String(), "error.code").String())
				require.Len(t, upstream.requests, 1)
				var failover *UpstreamFailoverError
				require.NotErrorAs(t, err, &failover, "do not add retries or bypass moderation for a semantic failure")
				if tc.code == "moderation_blocked" {
					require.False(t, IsOpenAIImagesRetryableUpstreamError(responseErr))
				}
				raw, ok := c.Get(OpsUpstreamErrorsKey)
				require.True(t, ok)
				events, ok := raw.([]*OpsUpstreamErrorEvent)
				require.True(t, ok)
				require.Len(t, events, 1)
				require.Equal(t, "response_error", events[0].Kind)
				require.Equal(t, http.StatusOK, events[0].UpstreamStatusCode, "retain the actual transport status for diagnosis")
				require.Equal(t, tc.code, events[0].Reason)
			})
		}
	}
}

func TestAPIKeyImagesNonstreamPreservesActualImagesAndUsage(t *testing.T) {
	for _, tc := range []struct {
		name, body string
		count      int
	}{
		{name: "URL without image download", body: `{"data":[{"url":"https://images.example/result.png"}],"usage":{"input_tokens":10,"output_tokens":20}}`, count: 1},
		{name: "base64", body: `{"data":[{"b64_json":"aW1hZ2U="}],"usage":{"input_tokens":10,"output_tokens":20}}`, count: 1},
		{name: "multi-image mixed response", body: `{"data":[{"b64_json":"aW1hZ2U="},{"url":"https://images.example/result.png"}],"usage":{"input_tokens":10,"output_tokens":20}}`, count: 2},
		{name: "null error is not a rejection", body: `{"error":null,"data":[{"url":"https://images.example/result.png"}],"usage":{"input_tokens":10,"output_tokens":20}}`, count: 1},
		{name: "false error flag is not a rejection", body: `{"error":false,"data":[{"url":"https://images.example/result.png"}],"usage":{"input_tokens":10,"output_tokens":20}}`, count: 1},
		{name: "empty error object is not a rejection", body: `{"error":{},"data":[{"url":"https://images.example/result.png"}],"usage":{"input_tokens":10,"output_tokens":20}}`, count: 1},
		{name: "partial response retains actual images and cost", body: `{"error":{"type":"server_error","code":"partial_failure","message":"one image failed"},"data":[{"url":"https://images.example/result.png"}],"usage":{"input_tokens":10,"output_tokens":20}}`, count: 1},
		{name: "Responses-compatible output remains counted", body: `{"output":[{"type":"image_generation_call","result":"aW1hZ2U="}],"usage":{"input_tokens":10,"output_tokens":20}}`, count: 1},
	} {
		t.Run(tc.name, func(t *testing.T) {
			body := []byte(`{"model":"gpt-image-2.6-flare","prompt":"draw","n":3,"size":"2048x2048"}`)
			c, rec := newOpenAIImagesTestContext(t, body)
			c.Set("api_key", &APIKey{ID: 42, Group: &Group{ImageCountBilling: true}})
			upstream := &httpUpstreamRecorder{resp: &http.Response{StatusCode: http.StatusOK,
				Header: http.Header{"Content-Type": {"application/json"}}, Body: io.NopCloser(strings.NewReader(tc.body))}}
			svc := newOpenAIImagesTestService(upstream)
			parsed, err := svc.ParseOpenAIImagesRequest(c, body)
			require.NoError(t, err)
			result, err := svc.ForwardImages(context.Background(), c, newOpenAIImagesAPIKeyAccount(), body, parsed, "")
			require.NoError(t, err)
			require.Equal(t, tc.count, result.ImageCount, "actual returned count wins over requested n")
			require.Equal(t, 10, result.Usage.InputTokens)
			require.Equal(t, 20, result.Usage.OutputTokens)
			require.JSONEq(t, tc.body, rec.Body.String())
			require.Len(t, upstream.requests, 1, "valid URL responses do not introduce a download or storage dependency")
		})
	}
}

func TestAPIKeyImagesNonstreamKeepaliveCannotMakeModerationBillable(t *testing.T) {
	body := []byte(`{"model":"gpt-image-2.6-flare","prompt":"draw","n":1}`)
	c, rec := newOpenAIImagesTestContext(t, body)
	c.Set("api_key", &APIKey{ID: 42, Group: &Group{ImageCountBilling: true}})
	stop := StartOpenAIImagesJSONKeepalive(c, 5*time.Millisecond)
	defer stop()
	waitForOpenAIImagesJSONKeepalive(t, c)
	upstream := &httpUpstreamRecorder{resp: &http.Response{StatusCode: http.StatusOK,
		Header: http.Header{"Content-Type": {"application/json"}},
		Body:   io.NopCloser(strings.NewReader(`{"error":{"type":"image_generation_user_error","code":"moderation_blocked","message":"request rejected"}}`))}}
	svc := newOpenAIImagesTestService(upstream)
	parsed, err := svc.ParseOpenAIImagesRequest(c, body)
	require.NoError(t, err)
	result, err := svc.ForwardImages(context.Background(), c, newOpenAIImagesAPIKeyAccount(), body, parsed, "")
	require.Nil(t, result, "a committed HTTP 200 cannot turn an explicit refusal into a billable image")
	var responseErr *OpenAIImagesUpstreamError
	require.ErrorAs(t, err, &responseErr)
	require.Equal(t, http.StatusBadRequest, responseErr.StatusCode)
	require.False(t, IsOpenAIImagesRetryableUpstreamError(responseErr))
	require.Equal(t, http.StatusOK, rec.Code)
	require.True(t, gjson.Valid(rec.Body.String()))
	require.Equal(t, "moderation_blocked", gjson.Get(rec.Body.String(), "error.code").String())
	require.Len(t, upstream.requests, 1)
	_, found := c.Get(OpsUpstreamErrorsKey)
	require.True(t, found)
}
