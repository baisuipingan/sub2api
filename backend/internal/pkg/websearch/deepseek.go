package websearch

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
	"unicode/utf8"
)

// https://api-docs.deepseek.com/api/search/
const (
	deepSeekSearchEndpoint = "https://api.deepseek.com/v1/tools/search"
	deepSeekMaxQueryLength = 128
	deepSeekMaxResults     = 50
)

// DeepSeekProvider uses the standalone Search API, independently of the model.
type DeepSeekProvider struct {
	apiKey string
	client *http.Client
}

func NewDeepSeekProvider(apiKey string, client *http.Client) *DeepSeekProvider {
	if client == nil {
		client = http.DefaultClient
	}
	// Keep the caller's transport and timeout. This adapter sends the key
	// only to the fixed official endpoint.
	searchClient := *client
	searchClient.CheckRedirect = func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }
	return &DeepSeekProvider{apiKey: strings.TrimSpace(apiKey), client: &searchClient}
}

func (p *DeepSeekProvider) Name() string { return ProviderTypeDeepSeek }

func (p *DeepSeekProvider) Search(ctx context.Context, req SearchRequest) (*SearchResponse, error) {
	if p.apiKey == "" {
		return nil, errors.New("deepseek search: API key is required")
	}
	if strings.TrimSpace(req.Query) == "" || !utf8.ValidString(req.Query) || utf8.RuneCountInString(req.Query) > deepSeekMaxQueryLength {
		return nil, errors.New("deepseek search: query must contain 1–128 Unicode characters")
	}
	count := req.MaxResults
	if count <= 0 {
		count = defaultMaxResults
	}
	count = min(count, deepSeekMaxResults)
	body, err := json.Marshal(deepSeekSearchRequest{Query: req.Query, Count: count})
	if err != nil {
		return nil, fmt.Errorf("deepseek search: encode request: %w", err)
	}
	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, deepSeekSearchEndpoint, bytes.NewReader(body))
	if err != nil {
		return nil, fmt.Errorf("deepseek search: build request: %w", err)
	}
	httpReq.Header.Set("Authorization", "Bearer "+p.apiKey)
	httpReq.Header.Set("Content-Type", "application/json")
	httpReq.Header.Set("Accept", "application/json")
	resp, err := p.client.Do(httpReq)
	if err != nil {
		return nil, fmt.Errorf("deepseek search: request failed: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("deepseek search: status %d", resp.StatusCode)
	}
	response, err := io.ReadAll(io.LimitReader(resp.Body, maxResponseSize+1))
	if err != nil {
		return nil, fmt.Errorf("deepseek search: read response: %w", err)
	}
	if len(response) > maxResponseSize {
		return nil, errors.New("deepseek search: response exceeds size limit")
	}
	var raw deepSeekSearchResponse
	if err := json.Unmarshal(response, &raw); err != nil || raw.Results == nil {
		return nil, errors.New("deepseek search: invalid response")
	}
	count = min(len(raw.Results), count)
	results := make([]SearchResult, 0, count)
	for _, result := range raw.Results[:count] {
		snippet := result.Snippet
		if strings.TrimSpace(snippet) == "" {
			snippet = result.Summary
		}
		results = append(results, SearchResult{URL: result.URL, Title: result.Title, Snippet: snippet, PageAge: result.Date})
	}
	return &SearchResponse{Results: results, Query: req.Query}, nil
}

type deepSeekSearchRequest struct {
	Query       string `json:"query"`
	Count       int    `json:"count"`
	NeedContent bool   `json:"need_content"`
}

type deepSeekSearchResponse struct {
	Results []struct {
		Title   string `json:"title"`
		URL     string `json:"url"`
		Snippet string `json:"snippet"`
		Summary string `json:"summary"`
		Date    string `json:"date"`
	} `json:"results"`
}
