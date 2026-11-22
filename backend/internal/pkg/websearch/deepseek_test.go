package websearch

import (
	"encoding/json"
	"io"
	"net/http"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

type deepSeekTransportFunc func(*http.Request) (*http.Response, error)

func (f deepSeekTransportFunc) RoundTrip(req *http.Request) (*http.Response, error) {
	return f(req)
}

func TestDeepSeekProviderSearchOfficialContract(t *testing.T) {
	query := strings.Repeat("深", 128-len([]rune(" site:example.test"))) + " site:example.test"
	calls := 0
	client := &http.Client{Transport: deepSeekTransportFunc(func(req *http.Request) (*http.Response, error) {
		calls++
		require.Equal(t, "https://api.deepseek.com/v1/tools/search", req.URL.String())
		require.Equal(t, http.MethodPost, req.Method)
		require.Equal(t, "Bearer fixture-key", req.Header.Get("Authorization"))
		require.Equal(t, "application/json", req.Header.Get("Content-Type"))
		defer func() { _ = req.Body.Close() }()
		var body map[string]any
		require.NoError(t, json.NewDecoder(req.Body).Decode(&body))
		require.Equal(t, map[string]any{"query": query, "count": float64(50), "need_content": false}, body)
		return &http.Response{StatusCode: 200, Body: io.NopCloser(strings.NewReader(`{"results":[
			{"url":"https://example.test/a","title":"发布","snippet":"简短说明","summary":"长摘要","date":"2026-06-01T00:00:00+00:00"},
			{"url":"https://example.test/b","title":"文档","snippet":"","summary":"补充说明","date":null}
		]}`))}, nil
	})}
	p := NewDeepSeekProvider("fixture-key", client)
	require.Equal(t, "deepseek", p.Name())
	resp, err := p.Search(t.Context(), SearchRequest{Query: query, MaxResults: 80})
	require.NoError(t, err)
	require.Equal(t, 1, calls)
	require.Equal(t, query, resp.Query)
	require.Equal(t, []SearchResult{
		{URL: "https://example.test/a", Title: "发布", Snippet: "简短说明", PageAge: "2026-06-01T00:00:00+00:00"},
		{URL: "https://example.test/b", Title: "文档", Snippet: "补充说明"},
	}, resp.Results)
}

func TestDeepSeekProviderSearchRejectsInvalidRequestsBeforeNetwork(t *testing.T) {
	client := &http.Client{Transport: deepSeekTransportFunc(func(*http.Request) (*http.Response, error) {
		t.Fatal("invalid requests must not reach the search provider")
		return nil, nil
	})}
	for _, query := range []string{"", " \t", strings.Repeat("搜", 129), "\xff"} {
		_, err := NewDeepSeekProvider("fixture-key", client).Search(t.Context(), SearchRequest{Query: query})
		require.ErrorContains(t, err, "1–128 Unicode characters")
	}
	_, err := NewDeepSeekProvider(" ", client).Search(t.Context(), SearchRequest{Query: "资料"})
	require.ErrorContains(t, err, "API key is required")
}

func TestDeepSeekProviderSearchResponseBoundaries(t *testing.T) {
	for _, tc := range []struct {
		name, body, wantError string
		status                int
	}{
		{name: "empty results", status: 200, body: `{"results":[]}`},
		{name: "invalid response", status: 200, body: `{"error":"sensitive fixture text"}`, wantError: "invalid response"},
		{name: "response limit", status: 200, body: `{"results":[]}` + strings.Repeat(" ", maxResponseSize), wantError: "response exceeds size limit"},
		{name: "status", status: 429, body: `{"error":"sensitive fixture text"}`, wantError: "status 429"},
		{name: "redirect", status: 307, wantError: "status 307"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			calls := 0
			client := &http.Client{Transport: deepSeekTransportFunc(func(req *http.Request) (*http.Response, error) {
				calls++
				require.Equal(t, "api.deepseek.com", req.URL.Host)
				defer func() { _ = req.Body.Close() }()
				var body deepSeekSearchRequest
				require.NoError(t, json.NewDecoder(req.Body).Decode(&body))
				require.Equal(t, defaultMaxResults, body.Count)
				return &http.Response{StatusCode: tc.status, Header: http.Header{"Location": {"https://example.test/"}},
					Body: io.NopCloser(strings.NewReader(tc.body))}, nil
			})}
			resp, err := NewDeepSeekProvider("fixture-key", client).Search(t.Context(), SearchRequest{Query: "资料"})
			require.Equal(t, 1, calls)
			if tc.wantError != "" {
				require.ErrorContains(t, err, tc.wantError)
				require.NotContains(t, err.Error(), "sensitive fixture text")
				require.Nil(t, resp)
				return
			}
			require.NoError(t, err)
			require.NotNil(t, resp.Results)
			require.Empty(t, resp.Results)
		})
	}
}
