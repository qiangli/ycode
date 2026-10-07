package ycode

import (
	"context"
	"errors"
	"github.com/qiangli/ycode/internal/harness/event"
	"sync"
)

// pauseGate stops between graph stages, never halfway through a tool call.
type pauseGate struct {
	mu              sync.Mutex
	reached, resume chan struct{}
	arrived         bool
	active          int
}

func (p *pauseGate) request() <-chan struct{} {
	p.mu.Lock()
	defer p.mu.Unlock()
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

// Continue releases a cooperative pause. It is distinct from typed HITL
// Resume and cannot approve a pending tool decision.
func (h *Harness) Continue(ctx context.Context, sessionID string) error {
	if err := h.Validate(); err != nil {
		return err
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	h.mu.Lock()
	defer h.mu.Unlock()
	for key, run := range h.active {
		if sessionOf(key) == sessionID {
			run.pause.mu.Lock()
			paused := run.pause.resume != nil
			run.pause.mu.Unlock()
			if !paused {
				continue
			}
			if _, err := h.events.Append(event.Draft{SessionID: sessionID, RunID: key[len(sessionID)+1:], StageID: "session.continue", Type: "session.continued", ConfigDigest: h.doc.ConfigDigest, Data: map[string]any{"approval_consumed": false}}); err != nil {
				return err
			}
			run.pause.release()
			return nil
		}
	}
	return errors.New("continue: no paused live turn")
}
