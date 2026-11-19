package service

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/pkg/websearch"
	"golang.org/x/net/html"
)

func officeReadURL(raw string) error {
	u, e := url.Parse(raw)
	if e != nil || u.Hostname() == "" || u.User != nil || u.Fragment != "" || len(raw) > 4096 || (u.Scheme != "https" && u.Scheme != "http") || (u.Port() != "" && u.Port() != "80" && u.Port() != "443") {
		return ErrOfficeInvalid
	}
	return nil
}
func (s *OfficeAgentService) Search(ctx context.Context, lease OfficeRunLease, id, query string, maximum int) (json.RawMessage, error) {
	if !lease.Run.SearchEnabled || len(query) > 2000 || strings.TrimSpace(query) == "" || s.settings == nil || !s.settings.IsWebSearchEmulationEnabled(ctx) {
		return nil, ErrOfficeInvalid
	}
	mgr := getWebSearchManager()
	if mgr == nil {
		return nil, ErrOfficeDisabled
	}
	maximum = max(1, min(maximum, 8))
	input, _ := json.Marshal(map[string]any{"tool": "web_search", "query": query, "max_results": maximum})
	return s.officeNetworkTool(ctx, lease, id, input, func(call context.Context) (any, error) {
		resp, provider, e := mgr.SearchWithBestProvider(call, websearch.SearchRequest{Query: query, MaxResults: maximum})
		if e != nil {
			return nil, e
		}
		return map[string]any{"query": resp.Query, "results": resp.Results, "provider": provider}, nil
	})
}
func (s *OfficeAgentService) ReadWeb(ctx context.Context, lease OfficeRunLease, id, raw string) (json.RawMessage, error) {
	if !lease.Run.SearchEnabled {
		return nil, ErrOfficeInvalid
	}
	if e := officeReadURL(raw); e != nil {
		return nil, e
	}
	input, _ := json.Marshal(map[string]string{"tool": "web_read", "url": raw})
	return s.officeNetworkTool(ctx, lease, id, input, func(call context.Context) (any, error) {
		req, e := http.NewRequestWithContext(call, http.MethodGet, raw, nil)
		if e != nil {
			return nil, e
		}
		req.Header.Set("User-Agent", "MiloRouter-Workbench/1.0")
		req.Header.Set("Accept", "text/html,text/plain")
		resp, e := officeWebClient.Do(req)
		if e != nil {
			return nil, e
		}
		defer func() { _ = resp.Body.Close() }()
		if resp.StatusCode != 200 {
			return nil, fmt.Errorf("page status %d", resp.StatusCode)
		}
		contentType := resp.Header.Get("Content-Type")
		if !strings.HasPrefix(contentType, "text/html") && !strings.HasPrefix(contentType, "text/plain") {
			return nil, ErrOfficeInvalid
		}
		body, e := io.ReadAll(io.LimitReader(resp.Body, (2<<20)+1))
		if e != nil || len(body) > 2<<20 {
			return nil, ErrOfficeBudgetExceeded
		}
		text := string(body)
		if strings.HasPrefix(contentType, "text/html") {
			var out strings.Builder
			tokenizer := html.NewTokenizer(strings.NewReader(text))
			skip := 0
			for {
				kind := tokenizer.Next()
				if kind == html.ErrorToken {
					break
				}
				token := tokenizer.Token()
				if kind == html.StartTagToken && (token.Data == "script" || token.Data == "style" || token.Data == "noscript") {
					skip++
				}
				if kind == html.EndTagToken && (token.Data == "script" || token.Data == "style" || token.Data == "noscript") {
					skip = max(0, skip-1)
				}
				if kind == html.TextToken && skip == 0 {
					t := strings.TrimSpace(token.Data)
					if t != "" {
						_, _ = out.WriteString(t)
						_ = out.WriteByte('\n')
					}
					if out.Len() > 64<<10 {
						break
					}
				}
			}
			text = out.String()
		}
		runes := []rune(text)
		if len(runes) > 16000 {
			text = string(runes[:16000])
		}
		return map[string]any{"url": resp.Request.URL.String(), "text": text}, nil
	})
}
func (s *OfficeAgentService) officeNetworkTool(ctx context.Context, lease OfficeRunLease, id string, input json.RawMessage, call func(context.Context) (any, error)) (json.RawMessage, error) {
	op, execute, e := s.PrepareTool(ctx, lease, id, input)
	if e != nil {
		return nil, e
	}
	if !execute {
		if op.Result == nil {
			return nil, ErrOfficeResultUnknown
		}
		return op.Result.Output, nil
	}
	toolCtx, cancel := context.WithTimeout(ctx, 25*time.Second)
	defer cancel()
	result, e := call(toolCtx)
	if e != nil {
		result = map[string]string{"error": "SEARCH_FAILED", "message": "检索或网页读取失败；请检查来源地址或搜索供应商配置"}
	}
	encoded, _ := json.Marshal(result)
	store, stop := context.WithTimeout(context.Background(), 5*time.Second)
	defer stop()
	if _, e = s.CompleteTool(store, lease, id, OfficeOperationResult{Output: encoded}); e != nil {
		return nil, e
	}
	return encoded, nil
}
