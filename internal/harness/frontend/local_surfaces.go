package frontend

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"

	"github.com/qiangli/ycode/internal/harness/event"
	"github.com/qiangli/ycode/internal/harness/spec"
)

// Defaults are transport metadata supplied by the caller. Adapters never
// select an agent, session, principal, or idempotency policy themselves.
type Defaults struct {
	SessionID      string
	AgentRef       string
	Principal      string
	IdempotencyKey string
}

func (d Defaults) input(body []byte) Input {
	return Input{SessionID: d.SessionID, AgentRef: d.AgentRef, Principal: d.Principal, IdempotencyKey: d.IdempotencyKey, Body: append([]byte(nil), body...)}
}

// OneShot adapts an argv/string request onto the canonical controller stream.
type OneShot struct{ local *Local }

func NewOneShot(doc *spec.Document, ref string, controller Controller) (*OneShot, error) {
	local, err := NewLocal(doc, ref, controller)
	if err != nil {
		return nil, err
	}
	if local.config.Kind != "one-shot" {
		return nil, fmt.Errorf("frontend %q is %q, not one-shot", ref, local.config.Kind)
	}
	return &OneShot{local: local}, nil
}
func (s *OneShot) Run(ctx context.Context, body []byte, defaults Defaults, renderer Renderer) error {
	return s.local.Run(ctx, defaults.input(body), renderer)
}

// Stdin consumes one bounded stream as one canonical input. It deliberately
// does not infer turns from lines; line-oriented behavior belongs to REPL.
type Stdin struct{ local *Local }

func NewStdin(doc *spec.Document, ref string, controller Controller) (*Stdin, error) {
	local, err := NewLocal(doc, ref, controller)
	if err != nil {
		return nil, err
	}
	if local.config.Kind != "stdin" && local.config.Kind != "one-shot" {
		return nil, fmt.Errorf("frontend %q is %q, not stdin-compatible", ref, local.config.Kind)
	}
	return &Stdin{local: local}, nil
}
func (s *Stdin) Run(ctx context.Context, reader io.Reader, defaults Defaults, renderer Renderer) error {
	if reader == nil {
		return errors.New("stdin reader is required")
	}
	maximum := s.local.config.Limits.MaxInputBytes
	var source io.Reader = reader
	if maximum > 0 {
		source = io.LimitReader(reader, int64(maximum)+1)
	}
	body, err := io.ReadAll(source)
	if err != nil {
		return fmt.Errorf("read stdin: %w", err)
	}
	body = bytes.TrimSuffix(body, []byte("\n"))
	return s.local.Run(ctx, defaults.input(body), renderer)
}

// REPL is a line-oriented adapter. Every nonempty line is an independent
// canonical submission and observes the same event stream contract.
type REPL struct{ local *Local }

func NewREPL(doc *spec.Document, ref string, controller Controller) (*REPL, error) {
	local, err := NewLocal(doc, ref, controller)
	if err != nil {
		return nil, err
	}
	if local.config.Kind != "repl" {
		return nil, fmt.Errorf("frontend %q is %q, not repl", ref, local.config.Kind)
	}
	return &REPL{local: local}, nil
}
func (s *REPL) Run(ctx context.Context, reader io.Reader, defaults Defaults, renderer Renderer) error {
	return s.local.REPL(ctx, reader, defaults.input(nil), renderer)
}
func (s *REPL) Resume(ctx context.Context, request Resume, renderer Renderer) error {
	return s.local.Resume(ctx, request, renderer)
}

// TUI is a presentation adapter only. The caller owns its rendering/event
// loop and cancellation context; approval resolution returns through Resume.
type TUI struct{ local *Local }

func NewTUI(doc *spec.Document, ref string, controller Controller) (*TUI, error) {
	local, err := NewLocal(doc, ref, controller)
	if err != nil {
		return nil, err
	}
	if local.config.Kind != "tui" {
		return nil, fmt.Errorf("frontend %q is %q, not tui", ref, local.config.Kind)
	}
	return &TUI{local: local}, nil
}
func (s *TUI) Run(ctx context.Context, body []byte, defaults Defaults, renderer Renderer) error {
	return s.local.Run(ctx, defaults.input(body), renderer)
}
func (s *TUI) Resume(ctx context.Context, request Resume, renderer Renderer) error {
	return s.local.Resume(ctx, request, renderer)
}

// EventRenderer is the minimal seam a CLI, REPL, or TUI implements. It keeps
// event projection outside the controller and makes all local surfaces share
// exactly the same durable event family.
type EventRenderer interface{ Render(event.Event) error }
