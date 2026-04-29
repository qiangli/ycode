package tools

import (
	"context"

	"github.com/qiangli/ycode/internal/runtime/searxng"
)

// searxngContainerProvider adapts a searxng.Service as a SearchProvider.
type searxngContainerProvider struct {
	svc *searxng.Service
}

// NewSearXNGContainerProvider creates a SearchProvider backed by a containerized SearXNG service.
func NewSearXNGContainerProvider(svc *searxng.Service) SearchProvider {
	return &searxngContainerProvider{svc: svc}
}

func (p *searxngContainerProvider) Name() string { return "searxng-container" }

func (p *searxngContainerProvider) Search(ctx context.Context, query string, maxResults int) ([]SearchResult, error) {
	if !p.svc.Available() {
		return nil, errSearXNGUnavailable
	}

	results, err := p.svc.Search(ctx, query, maxResults)
	if err != nil {
		return nil, err
	}

	var out []SearchResult
	for _, r := range results {
		out = append(out, SearchResult{
			Title:   r.Title,
			URL:     r.URL,
			Snippet: r.Content,
			Date:    r.PublishedDate,
		})
	}
	return out, nil
}

var errSearXNGUnavailable = &searchProviderError{msg: "containerized SearXNG not available"}

type searchProviderError struct {
	msg string
}

func (e *searchProviderError) Error() string { return e.msg }
