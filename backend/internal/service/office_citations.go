package service

import (
	"encoding/json"
	"net/url"
	"strings"
)

// OfficeCitation is derived only from completed, Go-owned network-tool inboxes.
// The assistant's text is never parsed as source metadata.
type OfficeCitation struct {
	ID      string `json:"id"`
	Title   string `json:"title"`
	URL     string `json:"url"`
	Excerpt string `json:"excerpt,omitempty"`
}

func officeCitationText(text string, maximum int) string {
	runes := []rune(strings.Join(strings.Fields(text), " "))
	if len(runes) > maximum {
		runes = runes[:maximum]
	}
	return string(runes)
}

func officeCitationURL(raw string) (string, bool) {
	raw = strings.TrimSpace(raw)
	if len(raw) > 4096 || strings.ContainsAny(raw, "\r\n\x00") {
		return "", false
	}
	u, err := url.Parse(raw)
	if err != nil || u.Hostname() == "" || u.User != nil || u.Opaque != "" || (u.Scheme != "https" && u.Scheme != "http") {
		return "", false
	}
	u.Host = strings.ToLower(u.Host)
	u.Fragment, u.RawFragment = "", ""
	if u.Path == "" {
		u.Path = "/"
	}
	return u.String(), true
}

func officeCitations(operations []OfficeOperation) []OfficeCitation {
	const maximum = 24
	citations := make([]OfficeCitation, 0, maximum)
	seen := make(map[string]bool, maximum)
	add := func(raw, title, excerpt string) {
		if len(citations) >= maximum {
			return
		}
		canonical, ok := officeCitationURL(raw)
		if !ok || seen[canonical] {
			return
		}
		seen[canonical] = true
		title = officeCitationText(title, 200)
		if title == "" {
			u, _ := url.Parse(canonical)
			title = u.Hostname()
		}
		citations = append(citations, OfficeCitation{ID: "source-" + officeDigest([]byte(canonical))[:24],
			Title: title, URL: canonical, Excerpt: officeCitationText(excerpt, 600)})
	}
	for _, op := range operations {
		if len(citations) >= maximum {
			break
		}
		if op.Kind != OfficeOperationTool || op.State != OfficeOperationCompleted || op.Result == nil || op.Result.ErrorCode != "" || (op.Result.HTTPStatus != 0 && (op.Result.HTTPStatus < 200 || op.Result.HTTPStatus >= 300)) {
			continue
		}
		var input struct {
			Tool string `json:"tool"`
		}
		if json.Unmarshal(op.Input, &input) != nil || (input.Tool != "web_search" && input.Tool != "web_read") {
			continue
		}
		var output struct {
			Error   json.RawMessage `json:"error"`
			URL     string          `json:"url"`
			Title   string          `json:"title"`
			Text    string          `json:"text"`
			Results []struct {
				URL     string `json:"url"`
				Title   string `json:"title"`
				Snippet string `json:"snippet"`
			} `json:"results"`
		}
		if json.Unmarshal(op.Result.Output, &output) != nil || (len(output.Error) > 0 && string(output.Error) != "null" && string(output.Error) != `""`) {
			continue
		}
		if input.Tool == "web_read" {
			if strings.TrimSpace(output.Text) != "" {
				add(output.URL, output.Title, output.Text)
			}
		} else {
			for _, source := range output.Results {
				add(source.URL, source.Title, source.Snippet)
			}
		}
	}
	return citations
}
