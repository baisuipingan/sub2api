package service

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"strings"
	"time"

	"golang.org/x/net/html"
)

var (
	ErrOfficeSearchUnavailable      = errors.New("workbench search is unavailable")
	ErrOfficeSearchModelUnsupported = errors.New("workbench model does not support search tools")
)

type WorkbenchSearchProviderStatus string

const (
	workbenchSearchUnconfigured WorkbenchSearchProviderStatus = "unconfigured"
	workbenchSearchUnavailable  WorkbenchSearchProviderStatus = "unavailable"
	workbenchSearchConfigured   WorkbenchSearchProviderStatus = "configured"
)

func (s *SettingService) workbenchSearchProviderStatus(ctx context.Context) WorkbenchSearchProviderStatus {
	cfg, err := s.loadWorkbenchSearchConfig(ctx)
	if err != nil {
		return workbenchSearchUnavailable
	}
	if cfg.Provider == "" {
		return workbenchSearchUnconfigured
	}
	if _, err := s.workbenchSearchAPIKey(cfg); err != nil {
		return workbenchSearchUnavailable
	}
	return workbenchSearchConfigured
}

func officeReadURL(raw string) error {
	u, e := url.Parse(raw)
	if e != nil || u.Hostname() == "" || u.User != nil || u.Fragment != "" || len(raw) > 4096 || (u.Scheme != "https" && u.Scheme != "http") || (u.Port() != "" && u.Port() != "80" && u.Port() != "443") {
		return ErrOfficeInvalid
	}
	return nil
}
func (s *OfficeAgentService) Search(ctx context.Context, lease OfficeRunLease, id, query string, maximum int) (json.RawMessage, error) {
	if !lease.Run.SearchEnabled || len(query) > 2000 || strings.TrimSpace(query) == "" {
		return nil, ErrOfficeInvalid
	}
	if s.settings == nil {
		return nil, ErrOfficeSearchUnavailable
	}
	maximum = max(1, min(maximum, 8))
	input, _ := json.Marshal(map[string]any{"tool": "web_search", "query": query, "max_results": maximum})
	op, err := s.prepareToolOperation(ctx, lease, id, input)
	if err != nil {
		return nil, err
	}
	if op.State == OfficeOperationCompleted {
		if op.Result == nil {
			return nil, ErrOfficeResultUnknown
		}
		return op.Result.Output, nil
	}

	cfg, searchErr := s.settings.loadWorkbenchSearchConfig(ctx)
	if searchErr != nil {
		searchErr = ErrWorkbenchSearchUnavailable
	}
	var turn struct {
		SearchPrice float64 `json:"search_price_per_1000_usd"`
	}
	_ = json.Unmarshal(lease.Run.Parameters, &turn)
	if searchErr == nil && cfg.pricePer1000USD != turn.SearchPrice {
		searchErr = ErrWorkbenchSearchPriceChanged
	}
	billingRepo, hasBilling := s.repo.(workbenchSearchBillingRepository)
	paid := searchErr == nil && cfg.pricePer1000USD > 0
	if paid {
		if !hasBilling {
			return nil, ErrWorkbenchSearchUnavailable
		}
		quote := WorkbenchSearchQuote{
			Provider: cfg.Provider, AmountUSD: cfg.pricePer1000USD / 1000,
			Billing: s.settings.GetBillingCurrencyConfig(ctx),
		}
		if _, err = billingRepo.DispatchWorkbenchSearch(ctx, lease, id, quote); err != nil {
			if !errors.Is(err, ErrWorkbenchSearchInsufficientBalance) {
				return nil, err
			}
			searchErr, paid = err, false
		} else {
			if s.billingCache != nil {
				invalidate, stop := context.WithTimeout(context.WithoutCancel(ctx), time.Second)
				s.invalidateSearchBalance(invalidate, lease.Run.UserID)
				stop()
			}
		}
	}
	if !paid {
		if _, err = s.repo.DispatchOperation(ctx, lease, id); err != nil {
			return nil, err
		}
	}
	callCtx := ctx
	if paid {
		// Once dispatched, let the bounded provider call settle even if the client disconnects.
		callCtx = context.WithoutCancel(ctx)
	}
	toolCtx, cancel := context.WithTimeout(callCtx, 25*time.Second)
	defer cancel()
	var search *WorkbenchSearchResult
	if searchErr == nil {
		search, searchErr = s.settings.searchWorkbenchWithConfig(toolCtx, cfg, query, maximum, workbenchSearchClient)
	}
	receipt, err := officeNetworkReceipt("web_search", search, searchErr, toolCtx.Err())
	if err != nil {
		return nil, err
	}
	store, stop := context.WithTimeout(context.WithoutCancel(ctx), 5*time.Second)
	defer stop()
	if paid {
		billable := searchErr == nil && search != nil && search.supplierResultCount > 0
		_, err = billingRepo.CompleteWorkbenchSearch(store, lease, id, receipt, billable)
		if err == nil {
			s.invalidateSearchBalance(store, lease.Run.UserID)
		}
	} else {
		_, err = s.CompleteTool(store, lease, id, receipt)
	}
	return receipt.Output, err
}
func (s *OfficeAgentService) ReadWeb(ctx context.Context, lease OfficeRunLease, id, raw string) (json.RawMessage, error) {
	if !lease.Run.SearchEnabled {
		return nil, ErrOfficeInvalid
	}
	if e := officeReadURL(raw); e != nil {
		return nil, e
	}
	input, _ := json.Marshal(map[string]string{"tool": "web_read", "url": raw})
	return s.officeNetworkTool(ctx, lease, id, "web_read", input, func(call context.Context) (any, error) {
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
func (s *OfficeAgentService) officeNetworkTool(ctx context.Context, lease OfficeRunLease, id, tool string, input json.RawMessage, call func(context.Context) (any, error)) (json.RawMessage, error) {
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
	receipt, e := officeNetworkReceipt(tool, result, e, toolCtx.Err())
	if e != nil {
		return nil, e
	}
	store, stop := context.WithTimeout(context.Background(), 5*time.Second)
	defer stop()
	if _, e = s.CompleteTool(store, lease, id, receipt); e != nil {
		return nil, e
	}
	return receipt.Output, nil
}

func officeNetworkReceipt(tool string, result any, err, contextErr error) (OfficeOperationResult, error) {
	receipt := OfficeOperationResult{}
	if err != nil {
		receipt.ErrorCode, receipt.ToolStatus = workbenchNetworkOutcome(tool, err, contextErr)
		receipt.ErrorMessage = workbenchToolErrorMessage(tool, receipt, receipt.ToolStatus)
		failure := map[string]string{"error": receipt.ErrorCode, "message": receipt.ErrorMessage}
		if search, ok := result.(*WorkbenchSearchResult); ok && search != nil {
			failure["provider"] = search.Provider
		}
		result = failure
	}
	receipt.Output, err = json.Marshal(result)
	return receipt, err
}

func workbenchNetworkOutcome(tool string, err, contextErr error) (code, status string) {
	if errors.Is(contextErr, context.Canceled) || errors.Is(err, context.Canceled) {
		return "TOOL_CANCELLED", "cancelled"
	}
	var timeout net.Error
	timedOut := errors.Is(contextErr, context.DeadlineExceeded) || errors.Is(err, context.DeadlineExceeded) || errors.As(err, &timeout) && timeout.Timeout()
	if tool == "web_read" {
		if timedOut {
			return "WEB_READ_TIMEOUT", "failed"
		}
		return "WEB_READ_FAILED", "failed"
	}
	switch {
	case timedOut:
		return "SEARCH_TIMEOUT", "failed"
	case errors.Is(err, ErrWorkbenchSearchUnavailable):
		return "SEARCH_UNAVAILABLE", "failed"
	case errors.Is(err, ErrWorkbenchSearchQuery):
		return "SEARCH_QUERY_INVALID", "failed"
	case errors.Is(err, ErrWorkbenchSearchInsufficientBalance):
		return "SEARCH_INSUFFICIENT_BALANCE", "failed"
	case errors.Is(err, ErrWorkbenchSearchPriceChanged):
		return "SEARCH_PRICE_CHANGED", "failed"
	default:
		return "SEARCH_FAILED", "failed"
	}
}
