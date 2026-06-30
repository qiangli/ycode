package tools

import (
	"context"
)

// searxngContainerProvider adapts a searxng.Service as a SearchProvider.
type searxngContainerProvider struct {
	svc any
}

// NewSearXNGContainerProvider creates a SearchProvider backed by a containerized SearXNG service.
func NewSearXNGContainerProvider(svc any) SearchProvider {
	return &searxngContainerProvider{svc: svc}
}

func (p *searxngContainerProvider) Name() string { return "searxng-container" }

func (p *searxngContainerProvider) Search(_ context.Context, _ string, _ int) ([]SearchResult, error) {
	return nil, errSearXNGUnavailable
}

var errSearXNGUnavailable = &searchProviderError{msg: "containerized SearXNG not available"}

type searchProviderError struct {
	msg string
}

func (e *searchProviderError) Error() string { return e.msg }
