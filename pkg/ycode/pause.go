package ycode

import (
	"context"
	"sync"
)

// pauseGate stops between graph stages, never halfway through a tool call.
type pauseGate struct {
	mu              sync.Mutex
	reached, resume chan struct{}
	arrived         bool
	active          int
	finished        bool
}

func (p *pauseGate) request() <-chan struct{} {
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.finished {
		return nil
	}
	if p.resume == nil {
		p.reached = make(chan struct{})
		p.resume = make(chan struct{})
		p.arrived = false
	}
	if p.active == 0 && !p.arrived {
		p.arrived = true
		close(p.reached)
	}
	return p.reached
}
func (p *pauseGate) enter(ctx context.Context) (func(), error) {
	for {
		p.mu.Lock()
		if p.resume == nil {
			p.active++
			p.mu.Unlock()
			return func() {
				p.mu.Lock()
				p.active--
				if p.active == 0 && p.resume != nil && !p.arrived {
					p.arrived = true
					close(p.reached)
				}
				p.mu.Unlock()
			}, nil
		}
		resume := p.resume
		p.mu.Unlock()
		select {
		case <-resume:
		case <-ctx.Done():
			return nil, ctx.Err()
		}
	}
}
func (p *pauseGate) boundary(ctx context.Context) error {
	p.mu.Lock()
	resume := p.resume
	if resume != nil && !p.arrived {
		p.arrived = true
		close(p.reached)
	}
	p.mu.Unlock()
	if resume == nil {
		return ctx.Err()
	}
	select {
	case <-resume:
		return ctx.Err()
	case <-ctx.Done():
		return ctx.Err()
	}
}
func (p *pauseGate) release() bool {
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.resume == nil {
		return false
	}
	close(p.resume)
	p.resume = nil
	p.reached = nil
	p.arrived = false
	return true
}

// finish closes the pause admission window atomically with the last boundary.
// A controller cannot receive a successful pause after completion has begun.
func (p *pauseGate) finish(ctx context.Context) error {
	for {
		p.mu.Lock()
		resume := p.resume
		if resume == nil {
			p.finished = true
			p.mu.Unlock()
			return ctx.Err()
		}
		p.mu.Unlock()
		select {
		case <-resume:
		case <-ctx.Done():
			return ctx.Err()
		}
	}
}

// Continue releases a cooperative pause. It is distinct from typed HITL
// Resume and cannot approve a pending tool decision.
func (h *Harness) Continue(ctx context.Context, sessionID string) error {
	return h.controlLiveRun(ctx, sessionID, "continue")
}
