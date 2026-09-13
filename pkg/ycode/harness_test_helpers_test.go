package ycode

import (
	"context"

	"github.com/qiangli/ycode/internal/api"
)

type stubProvider struct {
	kind       api.ProviderKind
	lastReq    *api.Request
	requests   []*api.Request
	streamFunc func(*api.Request) []*api.StreamEvent
	errFunc    func(*api.Request) error
}

func (p *stubProvider) Kind() api.ProviderKind { return p.kind }

func (p *stubProvider) Send(_ context.Context, request *api.Request) (<-chan *api.StreamEvent, <-chan error) {
	p.lastReq = request
	p.requests = append(p.requests, request)
	events := make(chan *api.StreamEvent, 8)
	errors := make(chan error, 1)
	go func() {
		defer close(events)
		defer close(errors)
		if p.streamFunc != nil {
			for _, item := range p.streamFunc(request) {
				events <- item
			}
		}
		if p.errFunc != nil {
			if err := p.errFunc(request); err != nil {
				errors <- err
				return
			}
		}
		events <- &api.StreamEvent{Type: "message_stop"}
	}()
	return events, errors
}

func newStubProvider(kind api.ProviderKind) *stubProvider { return &stubProvider{kind: kind} }
