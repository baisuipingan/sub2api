package service

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestStudioGatewayBusinessErrorPreservesTypedFailureAfterKeepalive(t *testing.T) {
	for _, status := range []int{200, 400} {
		path := filepath.Join(t.TempDir(), "response.json")
		require.NoError(t, os.WriteFile(path, []byte(" \n\t{\"error\":{\"code\":\"moderation_blocked\",\"type\":\"image_generation_user_error\",\"message\":\"Rejected by the safety system. safety_violations=[sexual].\"}}"), 0600))
		code, message, found := studioGatewayBusinessError(path, status)
		require.True(t, found)
		require.Equal(t, "MODERATION_BLOCKED", code)
		require.Contains(t, message, "safety system")
		require.Contains(t, message, "safety_violations=[sexual]")
		require.Contains(t, message, "[MODERATION_BLOCKED]")
	}
}

func TestStudioGatewayBusinessErrorIgnoresImagePayloadAndMalformedBody(t *testing.T) {
	for _, body := range []string{`{"data":[{"url":"https://private.test/image"}]}`, `{"error":null}`, `not json`, `{"error":{"code":"moderation_blocked"},"data":[{"url":"https://image.test/out.png"}]}`, `{"error":{"code":"moderation_blocked"},"data":[{"b64_json":"aW1hZ2U="}]}`, `{"error":{"code":"moderation_blocked"},"candidates":[{"content":{"parts":[{"inlineData":{"data":"aW1hZ2U="}}]}}]}`, `{"error":{"code":"bad code with spaces","message":"try 2K"}}`} {
		path := filepath.Join(t.TempDir(), "response.json")
		require.NoError(t, os.WriteFile(path, []byte(body), 0600))
		code, message, found := studioGatewayBusinessError(path, 200)
		if strings.Contains(body, "try 2K") {
			require.True(t, found)
			require.Equal(t, "UPSTREAM_ERROR", code)
			require.Contains(t, message, "try 2K")
		} else {
			require.False(t, found)
		}
	}
}

func TestStudioErrorDetailsRedactBeforeTruncationAndReadback(t *testing.T) {
	message := "Use 2K. Authorization: Bearer private-bearer-value access_token=private-token api_key=\"private key with spaces\" sk-privateapikey123456 https://private-bucket.test/image?sig=private-signature data:image/png;base64," + strings.Repeat("a", 1000)
	got := SanitizeStudioErrorMessage(message)
	require.Contains(t, got, "Use 2K")
	for _, secret := range []string{"private-bearer-value", "private-token", "private key with spaces", "sk-privateapikey", "private-bucket", "private-signature", "data:image", strings.Repeat("a", 160)} {
		require.NotContains(t, got, secret)
	}
	require.LessOrEqual(t, len(got), 1000)
	require.Equal(t, "upstream returned an HTML error page", SanitizeStudioErrorMessage("<html>secret page</html>"))
}

func TestStudioGatewayBusinessErrorPreservesGeminiNumericCode(t *testing.T) {
	path := filepath.Join(t.TempDir(), "response.json")
	require.NoError(t, os.WriteFile(path, []byte(`{"error":{"code":400,"status":"INVALID_ARGUMENT","message":"This image size is unsupported; choose 2K."}}`), 0600))
	code, message, found := studioGatewayBusinessError(path, 400)
	require.True(t, found)
	require.Equal(t, "INVALID_ARGUMENT", code)
	require.Contains(t, message, "choose 2K")
}

func TestStudioErrorDetailsRemoveWholeCookieHeader(t *testing.T) {
	got := SanitizeStudioErrorMessage("Retry later. Cookie: session=first-private; auth=second-private")
	require.Contains(t, got, "Retry later")
	require.NotContains(t, got, "first-private")
	require.NotContains(t, got, "second-private")
}
