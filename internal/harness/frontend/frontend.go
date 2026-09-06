// Package frontend projects local and remote interfaces onto one canonical
// harness request and durable event stream.
package frontend

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"io"

	"github.com/qiangli/ycode/internal/harness/event"
	"github.com/qiangli/ycode/internal/harness/spec"
)

type Input struct {
	FrontendRef    string
	SessionID      string
	AgentRef       string
	Principal      string
	IdempotencyKey string
	Body           []byte
}

type Resume struct {
	FrontendRef string
	SessionID   string
	Token       string
	Resolution  []byte
}

type Fork struct {
	FrontendRef string
	SessionID   string
	AtSequence  uint64
}

type Controller interface {
	Submit(context.Context, Input) (<-chan event.Event, error)
	Resume(context.Context, Resume) (<-chan event.Event, error)
	Fork(context.Context, Fork) (<-chan event.Event, error)
}

type Renderer interface{ Render(event.Event) error }
type RenderFunc func(event.Event) error

func (f RenderFunc) Render(value event.Event) error { return f(value) }

type Local struct {
	controller Controller
	ref        string
	config     spec.Frontend
}

func NewLocal(doc *spec.Document, frontendRef string, controller Controller) (*Local, error) {
	if doc == nil || controller == nil {
		return nil, errors.New("frontend requires compiled document and controller")
	}
	configured, ok := doc.Spec.Frontends[frontendRef]
	if !ok {
		return nil, fmt.Errorf("frontend %q is not declared", frontendRef)
	}
	switch configured.Kind {
	case "one-shot", "stdin", "repl", "tui":
	default:
		return nil, fmt.Errorf("frontend %q has non-local kind %q", frontendRef, configured.Kind)
	}
	return &Local{controller: controller, ref: frontendRef, config: configured}, nil
}

func (l *Local) Run(ctx context.Context, input Input, renderer Renderer) error {
	if renderer == nil {
		return errors.New("frontend renderer is required")
	}
	input.FrontendRef = l.ref
	if err := l.validateBody(input.Body); err != nil {
		return err
	}
	stream, err := l.controller.Submit(ctx, input)
	if err != nil {
		return err
	}
	return render(ctx, stream, renderer)
}

func (l *Local) Resume(ctx context.Context, request Resume, renderer Renderer) error {
	if !l.config.HITL || !l.config.Resume {
		return fmt.Errorf("frontend %q does not allow approval resume", l.ref)
	}
	request.FrontendRef = l.ref
	stream, err := l.controller.Resume(ctx, request)
	if err != nil {
		return err
	}
	return render(ctx, stream, renderer)
}

// REPL submits one non-empty input line at a time and renders the exact same
// event stream as Run. Presentation never changes execution semantics.
func (l *Local) REPL(ctx context.Context, reader io.Reader, base Input, renderer Renderer) error {
	if l.config.Kind != "repl" && l.config.Kind != "tui" {
		return fmt.Errorf("frontend %q is not interactive", l.ref)
	}
	scanner := bufio.NewScanner(reader)
	maximum := l.config.Limits.MaxInputBytes
	if maximum > 0 {
		scanner.Buffer(make([]byte, 4096), maximum)
	}
	for scanner.Scan() {
		if len(scanner.Bytes()) == 0 {
			continue
		}
		input := base
		input.Body = append([]byte(nil), scanner.Bytes()...)
		if err := l.Run(ctx, input, renderer); err != nil {
			return err
		}
	}
	return scanner.Err()
}

func (l *Local) validateBody(body []byte) error {
	if len(body) == 0 {
		return errors.New("frontend input is empty")
	}
	if l.config.Limits.MaxInputBytes > 0 && len(body) > l.config.Limits.MaxInputBytes {
		return fmt.Errorf("frontend input exceeds %d bytes", l.config.Limits.MaxInputBytes)
	}
	return nil
}

func render(ctx context.Context, stream <-chan event.Event, renderer Renderer) error {
	if stream == nil {
		return errors.New("frontend controller returned a nil event stream")
	}
	for {
		if err := ctx.Err(); err != nil {
			return err
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case item, ok := <-stream:
			if !ok {
				if err := ctx.Err(); err != nil {
					return err
				}
				return nil
			}
			if err := renderer.Render(item); err != nil {
				return err
			}
		}
	}
}
