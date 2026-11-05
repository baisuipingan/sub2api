//go:build unit

package service

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/Wei-Shaw/sub2api/internal/config"
	"github.com/Wei-Shaw/sub2api/internal/pkg/xai"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
	"github.com/tidwall/gjson"
)

func TestCompatibleImagesNativeResolutionProvidesInternalTierWithoutChangingBody(t *testing.T) {
	for _, tc := range []struct {
		fields, size, tier string
	}{
		{`"resolution":"1k"`, "1024x1024", "1K"},
		{`"resolution":" 2K "`, "2048x2048", "2K"},
		{`"resolution":"4k"`, "", "2K"},
		{`"resolution":"1.5k"`, "", "2K"},
		{`"size":"3840x2160","resolution":"1k"`, "3840x2160", "4K"},
		{`"size":"auto","resolution":"1k"`, "auto", "2K"},
	} {
		t.Run(tc.fields, func(t *testing.T) {
			body := []byte(`{"model":"grok-imagine-image-2.0","prompt":"draw",` + tc.fields + `}`)
			c, _ := gin.CreateTestContext(httptest.NewRecorder())
			c.Request = httptest.NewRequest(http.MethodPost, openAIImagesGenerationsEndpoint, bytes.NewReader(body))
			c.Request.Header.Set("Content-Type", "application/json")
			parsed, err := (&OpenAIGatewayService{}).ParseOpenAIImagesRequest(c, body)
			require.NoError(t, err)
			require.Equal(t, tc.size, parsed.Size)
			require.Equal(t, tc.tier, parsed.SizeTier)
			require.Equal(t, body, parsed.Body)
		})
	}
}

func TestCompatibleImagesGrokJSONEditParsesAndForwardsUnchangedProviderFields(t *testing.T) {
	gin.SetMode(gin.TestMode)
	for _, tc := range []struct {
		name, public, imageFields string
		count                     int
	}{
		{"single", "grok-imagine-image-2.0", `"image":{"url":"data:image/png;base64,aW1hZ2U=","type":"image_url"}`, 1},
		{"multiple", "grok-imagine-image-2.0", `"images":[{"url":"https://example.com/one.png"},{"url":"https://example.com/two.png"}]`, 2},
		{"legacy GPT image_url", "gpt-image-2", `"images":[{"image_url":"https://example.com/one.png"}],"mask":{"image_url":"https://example.com/mask.png"}`, 1},
		{"composite public alias", "my-painter", `"images":[{"url":"https://example.com/one.png"}]`, 1},
	} {
		t.Run(tc.name, func(t *testing.T) {
			body := []byte(`{"model":"` + tc.public + `","prompt":"edit","resolution":"1k","aspect_ratio":"21:9","quality":"medium","response_format":"b64_json",` + tc.imageFields + `}`)
			c, _ := gin.CreateTestContext(httptest.NewRecorder())
			c.Request = httptest.NewRequest(http.MethodPost, openAIImagesEditsEndpoint, bytes.NewReader(body))
			c.Request.Header.Set("Content-Type", "application/json")
			requestModel := tc.public
			if tc.public == "my-painter" {
				requestModel = "grok-imagine-image-2.0"
				ctx := WithCompositeRouteDecision(c.Request.Context(), CompositeRouteDecision{Matched: true, PublicModel: tc.public, UpstreamModel: requestModel, TargetPlatform: PlatformOpenAI})
				ctx = WithResolvedTargetPlatform(ctx, PlatformOpenAI)
				c.Request = c.Request.WithContext(ctx)
			}
			upstream := &httpUpstreamRecorder{resp: &http.Response{StatusCode: 200, Header: http.Header{"Content-Type": {"application/json"}}, Body: io.NopCloser(strings.NewReader(`{"data":[{"b64_json":"aW1hZ2U="}]}`))}}
			svc := &OpenAIGatewayService{cfg: &config.Config{}, httpUpstream: upstream}
			parsed, err := svc.ParseOpenAIImagesRequest(c, body)
			require.NoError(t, err)
			require.Len(t, parsed.InputImageURLs, tc.count)
			require.Equal(t, ImageBillingSize1K, parsed.SizeTier)
			moderation := ExtractContentModerationInput(ContentModerationProtocolOpenAIImages, parsed.ModerationBody())
			require.GreaterOrEqual(t, len(moderation.Images), tc.count)
			account := &Account{ID: 1, Platform: PlatformOpenAI, Type: AccountTypeAPIKey, Credentials: map[string]any{
				"api_key": "key", "base_url": "https://compatible.example/v1", "model_mapping": map[string]any{requestModel: "grok-imagine-image-2.0"},
			}}
			result, err := svc.ForwardImages(c.Request.Context(), c, account, body, parsed, "")
			require.NoError(t, err)
			require.Equal(t, "grok-imagine-image-2.0", result.UpstreamModel)
			require.Equal(t, ImageBillingSize1K, result.ImageSize)
			require.Equal(t, "1024x1024", result.ImageInputSize)
			require.False(t, gjson.GetBytes(upstream.lastBody, "size").Exists(), "internal representative size must not leak into the provider body")
			var expected map[string]any
			require.NoError(t, json.Unmarshal(body, &expected))
			expected["model"] = "grok-imagine-image-2.0"
			expectedBody, err := json.Marshal(expected)
			require.NoError(t, err)
			require.JSONEq(t, string(expectedBody), string(upstream.lastBody))
		})
	}
}

func TestCompatibleImagesJSONFileIDAndMaskRulesStayEnforced(t *testing.T) {
	for _, fields := range []string{
		`"image":{"file_id":"file-1"}`,
		`"images":[{"file_id":"file-1"}]`,
		`"images":[{"image_url":"https://example.com/input.png"}],"mask":{"file_id":"file-mask"}`,
	} {
		body := []byte(`{"model":"grok-imagine-image-2.0","prompt":"edit",` + fields + `}`)
		c, _ := gin.CreateTestContext(httptest.NewRecorder())
		c.Request = httptest.NewRequest(http.MethodPost, openAIImagesEditsEndpoint, bytes.NewReader(body))
		_, err := (&OpenAIGatewayService{}).ParseOpenAIImagesRequest(c, body)
		require.ErrorContains(t, err, "file_id is not supported")
	}
}

func TestForwardGrokNativeResolutionKeepsOneKTierAndOriginalJSON(t *testing.T) {
	t.Setenv(xai.EnvAllowUnsafeURLOverrides, "true")
	gin.SetMode(gin.TestMode)
	body := []byte(`{"model":"grok-imagine-image-2.0","prompt":"draw","resolution":"1k","aspect_ratio":"21:9","response_format":"b64_json"}`)
	parsed := ParseGrokMediaRequest("application/json", body)
	require.Equal(t, "1024x1024", parsed.Size)
	require.Equal(t, ImageBillingSize1K, parsed.SizeTier)
	usage := grokMediaUsageFromResponse(GrokMediaEndpointImagesGenerations, parsed, []byte(`{"data":[{"b64_json":"aW1hZ2U="}]}`))
	result := &OpenAIForwardResult{ImageCount: usage.ImageCount, ImageSize: usage.ImageSize, ImageInputSize: usage.ImageInputSize, ImageOutputSizes: usage.ImageOutputSizes}
	ApplyOpenAIImageBillingResolution(result)
	require.Equal(t, ImageBillingSize1K, result.ImageSize)
	require.Equal(t, ImageSizeSourceInput, result.ImageSizeSource)

	forwardBody, _, err := sanitizeGrokMediaForwardBody(GrokMediaEndpointImagesGenerations, body, "application/json")
	require.NoError(t, err)
	require.JSONEq(t, string(body), string(forwardBody))
	require.False(t, gjson.GetBytes(forwardBody, "size").Exists())
	upstream := &httpUpstreamRecorder{resp: &http.Response{StatusCode: 200, Header: http.Header{"Content-Type": {"application/json"}}, Body: io.NopCloser(strings.NewReader(`{"data":[{"b64_json":"aW1hZ2U="}]}`))}}
	svc := &OpenAIGatewayService{httpUpstream: upstream}
	c, _ := gin.CreateTestContext(httptest.NewRecorder())
	c.Request = httptest.NewRequest(http.MethodPost, openAIImagesGenerationsEndpoint, bytes.NewReader(body))
	account := &Account{ID: 1, Platform: PlatformGrok, Type: AccountTypeAPIKey, Credentials: map[string]any{"api_key": "key", "base_url": "https://xai.test/v1"}}
	forwarded, err := svc.ForwardGrokMedia(context.Background(), c, account, GrokMediaEndpointImagesGenerations, "", body, "application/json")
	require.NoError(t, err)
	require.Equal(t, ImageBillingSize1K, forwarded.ImageSize)
	require.Equal(t, "1024x1024", forwarded.ImageInputSize)
	require.JSONEq(t, string(body), string(upstream.lastBody))
}
