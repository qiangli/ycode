package provider

import (
	"context"
	"sync"

	api "github.com/qiangli/ycode/internal/api"
)

// MockBackend is a deterministic in-memory api.Provider for harness tests and
// offline fixtures. It passes through the same Adapter normalization path as a
// network provider rather than maintaining a second canonical implementation.
type MockBackend struct {
	Events []*api.StreamEvent
	Err    error

	mu      sync.Mutex
	request *api.Request
}

func (m *MockBackend) Kind() api.ProviderKind { return api.ProviderLocal }

func (m *MockBackend) Send(ctx context.Context, request *api.Request) (<-chan *api.StreamEvent, <-chan error) {
	events := make(chan *api.StreamEvent)
	errs := make(chan error, 1)
	m.mu.Lock()
	m.request = cloneAPIRequest(request)
	m.mu.Unlock()
	go func() {
		defer close(events)
		defer close(errs)
		for _, event := range m.Events {
			select {
			case events <- cloneStreamEvent(event):
			case <-ctx.Done():
				return
			}
		}
		if m.Err != nil {
			errs <- m.Err
		}
	}()
	return events, errs
}

// LastRequest returns a defensive snapshot of the last provider request.
func (m *MockBackend) LastRequest() *api.Request {
	m.mu.Lock()
	defer m.mu.Unlock()
	return cloneAPIRequest(m.request)
}

func NewMock(backend *MockBackend) (*Adapter, error) { return New(KindMock, backend) }

func cloneAPIRequest(request *api.Request) *api.Request {
	if request == nil {
		return nil
	}
	cloned := *request
	cloned.Messages = cloneMessages(request.Messages)
	cloned.Tools = append([]api.ToolDefinition(nil), request.Tools...)
	for i := range cloned.Tools {
		cloned.Tools[i].InputSchema = append([]byte(nil), cloned.Tools[i].InputSchema...)
	}
	return &cloned
}

func cloneStreamEvent(event *api.StreamEvent) *api.StreamEvent {
	if event == nil {
		return nil
	}
	cloned := *event
	cloned.Delta = append([]byte(nil), event.Delta...)
	if event.ContentBlock != nil {
		block := *event.ContentBlock
		block.Input = append([]byte(nil), event.ContentBlock.Input...)
		cloned.ContentBlock = &block
	}
	if event.Usage != nil {
		usage := *event.Usage
		cloned.Usage = &usage
	}
	return &cloned
}
