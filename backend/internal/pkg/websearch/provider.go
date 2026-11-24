package websearch

import "context"

// Provider is the interface every search backend must implement.
type Provider interface {
	// Name returns the provider identifier.
	Name() string
	// Search executes a web search and returns results.
	Search(ctx context.Context, req SearchRequest) (*SearchResponse, error)
}

// HTTPStatusError records a non-success response from a search provider.
// Error may include a provider response excerpt. Use StatusCode for public reporting.
type HTTPStatusError struct {
	StatusCode int
	detail     string
}

func (e *HTTPStatusError) Error() string { return e.detail }
