//go:build unit

package service

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/Wei-Shaw/sub2api/internal/pkg/xai"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
	"github.com/tidwall/gjson"
)

func grokEditParametersBody(t *testing.T, model string, count int) []byte {
	t.Helper()
	images := make([]map[string]string, count)
	for i := range images {
		images[i] = map[string]string{"url": fmt.Sprintf("https://example.com/source-%d.png", i), "type": "image_url"}
	}
	body, err := json.Marshal(map[string]any{
		"model": model, "prompt": "combine", "images": images,
		"resolution": "2k", "aspect_ratio": "21:9", "quality": "medium", "response_format": "b64_json", "n": 2,
	})
	require.NoError(t, err)
	return body
}

func TestGrokImageEditTwoPointZeroHasModelSpecificReferenceLimit(t *testing.T) {
	for _, tc := range []struct {
		model string
		count int
		allow bool
	}{
		{"grok-imagine-image-2.0", 4, true},
		{"grok-imagine-image-2.0", 5, true},
		{"grok-imagine-image-2.0", 6, false},
		{"grok-imagine-image-2.0-2026-11-05", 4, false},
		{"grok-imagine-image-2.00", 4, false},
		{"grok-imagine-image-2.0-private", 4, false},
		{"grok-imagine-image", 3, true},
		{"grok-imagine-image", 4, false},
		{"grok-imagine-image-quality", 4, false},
		{"provider-image", 4, false},
	} {
		t.Run(fmt.Sprintf("%s/%d", tc.model, tc.count), func(t *testing.T) {
			body := grokEditParametersBody(t, tc.model, tc.count)
			out, _, err := prepareGrokMediaForwardBody(GrokMediaEndpointImagesEdits, body, "application/json", tc.model)
			if !tc.allow {
				require.Error(t, err)
				require.Nil(t, out)
				return
			}
			require.NoError(t, err)
			require.JSONEq(t, string(body), string(out), "official JSON fields must survive reference normalization")
		})
	}
}

func TestGrokImageEditMultipartUsesMutuallyExclusiveImageFields(t *testing.T) {
	for _, count := range []int{1, 5} {
		t.Run(fmt.Sprint(count), func(t *testing.T) {
			var body bytes.Buffer
			writer := multipart.NewWriter(&body)
			require.NoError(t, writer.WriteField("model", "grok-imagine-image-2.0"))
			for i := 0; i < count; i++ {
				part, err := writer.CreateFormFile("image[]", fmt.Sprintf("source-%d.png", i))
				require.NoError(t, err)
				_, err = part.Write([]byte("image"))
				require.NoError(t, err)
			}
			require.NoError(t, writer.Close())
			out, contentType, err := prepareGrokMediaForwardBody(GrokMediaEndpointImagesEdits, body.Bytes(), writer.FormDataContentType(), "grok-imagine-image-2.0")
			require.NoError(t, err)
			require.Equal(t, "application/json", contentType)
			require.Equal(t, count == 1, gjson.GetBytes(out, "image").Exists())
			require.Equal(t, count > 1, gjson.GetBytes(out, "images").Exists())
		})
	}
}

func TestForwardGrokImageEditReferenceLimitUsesActualMappedModel(t *testing.T) {
	t.Setenv(xai.EnvAllowUnsafeURLOverrides, "true")
	gin.SetMode(gin.TestMode)
	for _, tc := range []struct {
		public, upstream string
		allow            bool
	}{
		{"grok-imagine-image-2.0", "grok-imagine-image-2.0", true},
		{"grok-imagine-image", "grok-imagine-image-2.0", true},
		{"grok-imagine-image-2.0", "grok-imagine-image", false},
	} {
		t.Run(tc.public+"/"+tc.upstream, func(t *testing.T) {
			body := grokEditParametersBody(t, tc.public, 5)
			c, _ := gin.CreateTestContext(httptest.NewRecorder())
			c.Request = httptest.NewRequest(http.MethodPost, "/v1/images/edits", bytes.NewReader(body))
			upstream := &httpUpstreamRecorder{resp: &http.Response{StatusCode: 200, Header: http.Header{"Content-Type": {"application/json"}}, Body: io.NopCloser(strings.NewReader(`{"data":[{"b64_json":"aW1hZ2U="}]}`))}}
			svc := &OpenAIGatewayService{httpUpstream: upstream}
			account := &Account{ID: 66, Platform: PlatformGrok, Type: AccountTypeAPIKey, Credentials: map[string]any{
				"api_key": "key", "base_url": "https://xai.test/v1", "model_mapping": map[string]any{tc.public: tc.upstream},
			}}
			_, err := svc.ForwardGrokMedia(context.Background(), c, account, GrokMediaEndpointImagesEdits, "", body, "application/json")
			if !tc.allow {
				require.ErrorContains(t, err, "maximum of 3 source images")
				require.Nil(t, upstream.lastReq)
				return
			}
			require.NoError(t, err)
			require.Equal(t, tc.upstream, gjson.GetBytes(upstream.lastBody, "model").String())
			require.Equal(t, int64(5), gjson.GetBytes(upstream.lastBody, "images.#").Int())
			require.False(t, gjson.GetBytes(upstream.lastBody, "image").Exists())
			for key, value := range map[string]string{"resolution": "2k", "aspect_ratio": "21:9", "quality": "medium", "response_format": "b64_json"} {
				require.Equal(t, value, gjson.GetBytes(upstream.lastBody, key).String())
			}
		})
	}
}
