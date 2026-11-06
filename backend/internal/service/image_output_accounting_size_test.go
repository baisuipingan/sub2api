package service

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestOpenAIImageOutputCounter_ImagesDataSizeSources(t *testing.T) {
	actual4K := encodeOpenAIImageTestWebPVP8X(4096, 4096)
	actual1K := encodeOpenAIImageTestWebPVP8X(1024, 1024)

	tests := []struct {
		name  string
		body  string
		count int
		sizes []string
	}{
		{
			name:  "image bytes override conflicting item and top-level metadata",
			body:  fmt.Sprintf("{\"size\":\"1024x1024\",\"data\":[{\"size\":\"2048x2048\",\"b64_json\":%q}]}", actual4K),
			count: 1,
			sizes: []string{"4096x4096"},
		},
		{
			name:  "item metadata overrides top-level metadata when bytes cannot be decoded",
			body:  "{\"size\":\"1024x1024\",\"data\":[{\"size\":\"2048x2048\",\"b64_json\":\"invalid-image\"}]}",
			count: 1,
			sizes: []string{"2048x2048"},
		},
		{
			name:  "top-level metadata applies to each image without its own size",
			body:  "{\"size\":\"4096x4096\",\"data\":[{\"url\":\"https://example.test/a.png\"},{\"url\":\"https://example.test/b.png\"}]}",
			count: 2,
			sizes: []string{"4096x4096", "4096x4096"},
		},
		{
			name:  "mixed sources stay aligned with the output images",
			body:  fmt.Sprintf("{\"size\":\"2048x2048\",\"data\":[{\"b64_json\":%q},{\"size\":\"1024x1024\",\"url\":\"https://example.test/b.png\"},{\"b64_json\":%q}]}", actual4K, actual1K),
			count: 3,
			sizes: []string{"4096x4096", "1024x1024", "1024x1024"},
		},
		{
			name:  "remote URL without size remains unknown",
			body:  "{\"data\":[{\"url\":\"https://example.test/a.png\"}]}",
			count: 1,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			counter := newOpenAIImageOutputCounter()
			counter.AddJSONResponse([]byte(tt.body))
			require.Equal(t, tt.count, counter.Count())
			require.Equal(t, tt.sizes, counter.Sizes())
		})
	}
}

func TestOpenAIImageOutputCounter_ImagesSSEDataSizesAndRepeatedCompletion(t *testing.T) {
	actual4K := encodeOpenAIImageTestWebPVP8X(4096, 4096)
	first := fmt.Sprintf("{\"type\":\"image_generation.completed\",\"size\":\"1024x1024\",\"data\":[{\"b64_json\":%q}]}", actual4K)
	second := fmt.Sprintf("{\"type\":\"response.completed\",\"size\":\"2048x2048\",\"data\":[{\"b64_json\":%q}]}", actual4K)

	counter := newOpenAIImageOutputCounter()
	counter.AddSSEData([]byte(first))
	counter.AddSSEData([]byte(second))
	require.Equal(t, 1, counter.Count())
	require.Equal(t, []string{"4096x4096"}, counter.Sizes())

	result := &OpenAIForwardResult{ImageCount: counter.Count(), ImageOutputSizes: counter.Sizes()}
	ApplyOpenAIImageBillingResolution(result)
	require.Equal(t, ImageBillingSize4K, result.ImageSize)
	require.Equal(t, ImageSizeSourceOutput, result.ImageSizeSource)
}

func TestOpenAIImageOutputCounter_DoesNotDowngradeDecodedSizeInLaterSSESnapshot(t *testing.T) {
	actual4K := encodeOpenAIImageTestWebPVP8X(4096, 4096)
	counter := newOpenAIImageOutputCounter()
	counter.AddSSEData([]byte(fmt.Sprintf("{\"type\":\"image_generation.completed\",\"data\":[{\"id\":\"same-image\",\"b64_json\":%q}]}", actual4K)))
	counter.AddSSEData([]byte("{\"type\":\"response.completed\",\"size\":\"2048x2048\",\"data\":[{\"id\":\"same-image\",\"url\":\"https://example.test/a.png\"}]}"))

	require.Equal(t, 1, counter.Count())
	require.Equal(t, []string{"4096x4096"}, counter.Sizes())
}

func TestOpenAIImageOutputCounter_DoesNotDoubleCountDataAndOutput(t *testing.T) {
	actual4K := encodeOpenAIImageTestWebPVP8X(4096, 4096)
	body := fmt.Sprintf("{\"data\":[{\"b64_json\":%q}],\"output\":[{\"id\":\"same-image\",\"type\":\"image_generation_call\",\"result\":%q,\"size\":\"2048x2048\"}]}", actual4K, actual4K)
	counter := newOpenAIImageOutputCounter()
	counter.AddJSONResponse([]byte(body))

	require.Equal(t, 1, counter.Count())
	require.Equal(t, []string{"4096x4096"}, counter.Sizes())
}

func TestOpenAIImageOutputCounter_CompletedSSEActualSizeShapes(t *testing.T) {
	actual4K := encodeOpenAIImageTestWebPVP8X(4096, 4096)
	for _, shape := range []string{"root", "item", "output"} {
		for _, field := range []string{"b64_json", "result"} {
			for _, size := range []string{"", "2048x2048"} {
				t.Run(shape+"/"+field+"/"+size, func(t *testing.T) {
					item := map[string]any{"id": "same-image", field: actual4K}
					if size != "" {
						item["size"] = size
					}
					root := map[string]any{"type": "image_generation.completed"}
					if shape == "root" {
						for key, value := range item {
							root[key] = value
						}
					} else {
						root[shape] = item
					}
					body, err := json.Marshal(root)
					require.NoError(t, err)
					counter := newOpenAIImageOutputCounter()
					counter.AddSSEData(body)
					require.Equal(t, 1, counter.Count())
					require.Equal(t, []string{"4096x4096"}, counter.Sizes())
				})
			}
		}
	}
}

func TestOpenAIImageOutputCounter_SeenIDUpgradesToActualSizeWithoutDowngrading(t *testing.T) {
	actual4K := encodeOpenAIImageTestWebPVP8X(4096, 4096)
	counter := newOpenAIImageOutputCounter()
	counter.AddSSEData([]byte("{\"type\":\"image_generation.completed\",\"id\":\"same-image\",\"url\":\"https://example.test/a.png\",\"size\":\"2048x2048\"}"))
	counter.AddSSEData([]byte(fmt.Sprintf("{\"type\":\"image_generation.completed\",\"id\":\"same-image\",\"b64_json\":%q}", actual4K)))
	counter.AddSSEData([]byte("{\"type\":\"image_generation.completed\",\"id\":\"same-image\",\"url\":\"https://example.test/a.png\",\"size\":\"1024x1024\"}"))

	require.Equal(t, 1, counter.Count())
	require.Equal(t, []string{"4096x4096"}, counter.Sizes())
}

func TestOpenAIImageOutputCounter_CompletedSSEInheritsWrapperSize(t *testing.T) {
	for _, shape := range []string{"item", "output"} {
		t.Run(shape, func(t *testing.T) {
			body := fmt.Sprintf("{\"type\":\"image_generation.completed\",\"size\":\"4096x4096\",%q:{\"url\":\"https://example.test/a.png\"}}", shape)
			counter := newOpenAIImageOutputCounter()
			counter.AddSSEData([]byte(body))
			require.Equal(t, 1, counter.Count())
			require.Equal(t, []string{"4096x4096"}, counter.Sizes())
		})
	}
}

func TestOpenAIImageOutputCounter_ReorderedDataKeepsEachImagesSize(t *testing.T) {
	actual4K := encodeOpenAIImageTestWebPVP8X(4096, 4096)
	actual1K := encodeOpenAIImageTestWebPVP8X(1024, 1024)
	counter := newOpenAIImageOutputCounter()
	counter.AddJSONResponse([]byte(fmt.Sprintf("{\"data\":[{\"id\":\"a\",\"b64_json\":%q},{\"id\":\"b\",\"b64_json\":%q}]}", actual4K, actual1K)))
	counter.AddJSONResponse([]byte("{\"size\":\"2048x2048\",\"data\":[{\"id\":\"b\",\"url\":\"https://example.test/b.png\"},{\"id\":\"a\",\"url\":\"https://example.test/a.png\"}]}"))
	require.Equal(t, 2, counter.Count())
	require.Equal(t, []string{"1024x1024", "4096x4096"}, counter.Sizes())
}

func TestOpenAIImageOutputCounter_MergesDataAndOutputOnlyByIdentity(t *testing.T) {
	actual4K := encodeOpenAIImageTestWebPVP8X(4096, 4096)
	actual1K := encodeOpenAIImageTestWebPVP8X(1024, 1024)
	body := fmt.Sprintf("{\"data\":[{\"id\":\"a\",\"b64_json\":%q},{\"id\":\"b\",\"b64_json\":%q}],\"output\":[{\"id\":\"b\",\"type\":\"image_generation_call\",\"result\":\"not-image-b\",\"size\":\"2048x2048\"},{\"id\":\"a\",\"type\":\"image_generation_call\",\"result\":\"not-image-a\",\"size\":\"2048x2048\"}]}", actual4K, actual1K)
	counter := newOpenAIImageOutputCounter()
	counter.AddJSONResponse([]byte(body))
	require.Equal(t, 2, counter.Count())
	require.Equal(t, []string{"1024x1024", "4096x4096"}, counter.Sizes())

	unrelated := fmt.Sprintf("{\"data\":[{\"id\":\"a\",\"b64_json\":%q}],\"output\":[{\"id\":\"b\",\"type\":\"image_generation_call\",\"result\":\"not-image-b\",\"size\":\"1024x1024\"}]}", actual4K)
	counter = newOpenAIImageOutputCounter()
	counter.AddJSONResponse([]byte(unrelated))
	require.Equal(t, 1, counter.Count())
	require.Equal(t, []string{"1024x1024"}, counter.Sizes())
}

func TestForwardOpenAIImagesAPIKey_UsesActualOutputDimensionsForBilling(t *testing.T) {
	encoded := encodeOpenAIImageTestWebPVP8X(4096, 4096)
	for _, shape := range []string{"non-streaming", "data", "root", "item", "output"} {
		stream := shape != "non-streaming"
		contentType := "application/json"
		upstreamBody := fmt.Sprintf("{\"size\":\"2048x2048\",\"data\":[{\"size\":\"1024x1024\",\"b64_json\":%q}]}", encoded)
		if stream {
			contentType = "text/event-stream"
			event := fmt.Sprintf("{\"type\":\"image_generation.completed\",\"size\":\"2048x2048\",\"b64_json\":%q}", encoded)
			if shape == "data" {
				event = fmt.Sprintf("{\"type\":\"image_generation.completed\",\"size\":\"2048x2048\",\"data\":[{\"size\":\"1024x1024\",\"b64_json\":%q}]}", encoded)
			} else if shape != "root" {
				event = fmt.Sprintf("{\"type\":\"image_generation.completed\",%q:{\"b64_json\":%q}}", shape, encoded)
			}
			upstreamBody = "data: " + event + "\n\ndata: [DONE]\n\n"
		}
		t.Run(shape, func(t *testing.T) {
			body := []byte(fmt.Sprintf("{\"model\":\"gpt-image-2\",\"prompt\":\"draw a square\",\"stream\":%t}", stream))
			c, rec := newOpenAIImagesTestContext(t, body)
			upstream := &httpUpstreamRecorder{resp: &http.Response{
				StatusCode: http.StatusOK,
				Header:     http.Header{"Content-Type": []string{contentType}},
				Body:       io.NopCloser(strings.NewReader(upstreamBody)),
			}}
			svc := newOpenAIImagesTestService(upstream)
			parsed, err := svc.ParseOpenAIImagesRequest(c, body)
			require.NoError(t, err)
			result, err := svc.ForwardImages(context.Background(), c, newOpenAIImagesAPIKeyAccount(), body, parsed, "")
			require.NoError(t, err)
			require.Equal(t, 1, result.ImageCount)
			require.Equal(t, []string{"4096x4096"}, result.ImageOutputSizes)
			require.Equal(t, upstreamBody, rec.Body.String(), "API Key responses must remain unchanged")

			ApplyOpenAIImageBillingResolution(result)
			require.Equal(t, ImageBillingSize4K, result.ImageSize)
			require.Equal(t, "4096x4096", result.ImageOutputSize)
			require.Equal(t, ImageSizeSourceOutput, result.ImageSizeSource)
		})
	}
}
