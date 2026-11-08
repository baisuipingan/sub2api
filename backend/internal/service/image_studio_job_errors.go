package service

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"regexp"
	"strings"
	"unicode"

	"github.com/Wei-Shaw/sub2api/internal/util/logredact"
)

var (
	studioErrorCodePattern             = regexp.MustCompile(`^[A-Za-z0-9_.-]{1,64}$`)
	studioErrorURLPattern              = regexp.MustCompile(`(?i)https?://[^\s<>"']+`)
	studioErrorMediaPattern            = regexp.MustCompile(`(?i)data:[^\s"']+|[A-Za-z0-9+/=_-]{160,}`)
	studioErrorCredentialPattern       = regexp.MustCompile(`(?i)\b(?:sk|rk)-[A-Za-z0-9_-]{8,}|\b(?:bearer|basic)\s+[^\s,;"']+|\beyJ[A-Za-z0-9_-]+\.[A-Za-z0-9_-]+\.[A-Za-z0-9_-]+`)
	studioErrorCookiePattern           = regexp.MustCompile(`(?im)\b(?:cookie|set-cookie)\s*[:=]\s*[^\r\n]+`)
	studioErrorSecretAssignmentPattern = regexp.MustCompile(`(?i)(["']?(?:api[_ -]?key|x-api-key|x-goog-api-key|access[_-]?token|refresh[_-]?token|id[_-]?token|client[_-]?secret|secret|password|passwd|token)["']?\s*[:=]\s*)(?:"[^"]*"|'[^']*'|[^\s,;}\]&]+)`)
)

// SanitizeStudioErrorMessage is used both at persistence and at the admin read
// boundary, including old records. Only a short error message is exposed, never
// a provider response body, URL, inline image, or authentication material.
func SanitizeStudioErrorMessage(message string) string {
	message = strings.TrimSpace(message)
	if strings.Contains(strings.ToLower(message), "<html") || strings.Contains(strings.ToLower(message), "<!doctype") || strings.Contains(strings.ToLower(message), "<script") {
		return "upstream returned an HTML error page"
	}
	message = studioErrorCookiePattern.ReplaceAllString(message, "[cookie redacted]")
	message = studioErrorSecretAssignmentPattern.ReplaceAllString(message, "${1}***")
	message = studioErrorCredentialPattern.ReplaceAllString(message, "[credential redacted]")
	message = logredact.RedactText(message, "authorization", "api_key", "apikey", "x-api-key", "x-goog-api-key", "token", "secret", "cookie", "set-cookie", "prompt", "b64_json", "account_name", "upstream_model")
	message = studioErrorURLPattern.ReplaceAllString(message, "[URL redacted]")
	message = studioErrorMediaPattern.ReplaceAllString(message, "[image or token redacted]")
	message = studioErrorCredentialPattern.ReplaceAllString(message, "[credential redacted]")
	message = strings.Map(func(r rune) rune {
		if unicode.IsControl(r) {
			return ' '
		}
		return r
	}, message)
	return truncateString(strings.Join(strings.Fields(message), " "), 1000)
}

// studioGatewayBusinessError recognizes error JSON even after keepalive has
// already committed HTTP 200. It never mistakes an error envelope for an image.
func studioGatewayBusinessError(path string, status int) (code, message string, found bool) {
	file, err := os.Open(path)
	if err != nil {
		return "", "", false
	}
	defer func() { _ = file.Close() }()
	var payload struct {
		Error json.RawMessage `json:"error"`
	}
	data, err := io.ReadAll(io.LimitReader(file, (16<<10)+1))
	if err != nil || len(data) > 16<<10 {
		return "", "", false
	}
	counter := newOpenAIImageOutputCounter()
	counter.AddJSONResponse(data)
	if counter.Count() > 0 {
		return "", "", false
	}
	// Gemini native responses may contain successful image parts alongside a
	// secondary error. Existing image processing and billing must remain intact.
	var mixed struct {
		Candidates []struct {
			Content struct {
				Parts []struct {
					InlineData struct {
						Data string `json:"data"`
					} `json:"inlineData"`
				} `json:"parts"`
			} `json:"content"`
		} `json:"candidates"`
	}
	if json.Unmarshal(data, &mixed) == nil {
		for _, candidate := range mixed.Candidates {
			for _, part := range candidate.Content.Parts {
				if strings.TrimSpace(part.InlineData.Data) != "" {
					return "", "", false
				}
			}
		}
	}
	decoder := json.NewDecoder(bytes.NewReader(data))
	if decoder.Decode(&payload) != nil || len(payload.Error) == 0 || string(payload.Error) == "null" {
		return "", "", false
	}
	var detail struct {
		Code    any    `json:"code"`
		Type    string `json:"type"`
		Message string `json:"message"`
		Status  string `json:"status"`
	}
	if json.Unmarshal(payload.Error, &detail) != nil {
		if json.Unmarshal(payload.Error, &detail.Message) != nil || detail.Message == "" {
			return "", "", false
		}
	}
	code, _ = detail.Code.(string)
	if code == "" {
		code = detail.Status
	}
	if !studioErrorCodePattern.MatchString(code) || studioErrorCredentialPattern.MatchString(code) {
		code = "UPSTREAM_ERROR"
	}
	code = strings.ToUpper(code)
	if code == "UPSTREAM_ERROR" && detail.Type == "image_generation_user_error" {
		code = "IMAGE_GENERATION_REJECTED"
	}
	message = SanitizeStudioErrorMessage(detail.Message)
	if message == "" {
		message = "upstream rejected the image request"
	}
	if status < 200 || status >= 300 {
		message = fmt.Sprintf("HTTP %d [%s]: %s", status, code, message)
	} else {
		message = fmt.Sprintf("[%s]: %s", code, message)
	}
	return code, message, true
}
