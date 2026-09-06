package frontend

import (
	"bytes"
	"context"
	"errors"
	"reflect"
	"strings"
	"testing"

	"github.com/qiangli/ycode/internal/harness/event"
	"github.com/qiangli/ycode/internal/harness/spec"
)

func localSurfaceDocument() *spec.Document {
	config := spec.Frontend{HITL: true, Resume: true, Limits: spec.FrontendLimits{MaxInputBytes: 64}}
	frontends := map[string]spec.Frontend{}
	for _, kind := range []string{"one-shot", "stdin", "repl", "tui"} {
		value := config
		value.Kind = kind
		frontends[kind] = value
	}
	return &spec.Document{Spec: spec.Spec{Frontends: frontends}}
}

func TestOneShotStdinREPLAndTUIHaveCanonicalParity(t *testing.T) {
	wantEvents := []event.Event{{Sequence: 1, Type: "text.delta"}, {Sequence: 2, Type: "approval.requested"}, {Sequence: 3, Type: "turn.completed"}}
	defaults := Defaults{SessionID: "session", AgentRef: "coder", Principal: "local-user", IdempotencyKey: "request-1"}
	type runCase struct {
		name string
		run  func(Controller, Renderer) error
	}
	doc := localSurfaceDocument()
	cases := []runCase{
		{"one-shot", func(c Controller, r Renderer) error {
			s, err := NewOneShot(doc, "one-shot", c)
			if err != nil {
				return err
			}
			return s.Run(context.Background(), []byte("hello"), defaults, r)
		}},
		{"stdin", func(c Controller, r Renderer) error {
			s, err := NewStdin(doc, "stdin", c)
			if err != nil {
				return err
			}
			return s.Run(context.Background(), bytes.NewBufferString("hello\n"), defaults, r)
		}},
		{"repl", func(c Controller, r Renderer) error {
			s, err := NewREPL(doc, "repl", c)
			if err != nil {
				return err
			}
			return s.Run(context.Background(), strings.NewReader("hello\n"), defaults, r)
		}},
		{"tui", func(c Controller, r Renderer) error {
			s, err := NewTUI(doc, "tui", c)
			if err != nil {
				return err
			}
			return s.Run(context.Background(), []byte("hello"), defaults, r)
		}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			controller := &fakeController{events: wantEvents}
			var got []event.Event
			err := tc.run(controller, RenderFunc(func(e event.Event) error { got = append(got, e); return nil }))
			if err != nil {
				t.Fatal(err)
			}
			if !reflect.DeepEqual(got, wantEvents) {
				t.Fatalf("events=%#v", got)
			}
			if len(controller.inputs) != 1 {
				t.Fatalf("inputs=%#v", controller.inputs)
			}
			input := controller.inputs[0]
			if string(input.Body) != "hello" || input.SessionID != defaults.SessionID || input.AgentRef != defaults.AgentRef || input.Principal != defaults.Principal || input.IdempotencyKey != defaults.IdempotencyKey || input.FrontendRef != tc.name {
				t.Fatalf("canonical input=%#v", input)
			}
		})
	}
}

type cancellationController struct{}

func (cancellationController) Submit(ctx context.Context, _ Input) (<-chan event.Event, error) {
	stream := make(chan event.Event)
	go func() { stream <- event.Event{Sequence: 1, Type: "text.delta"}; <-ctx.Done() }()
	return stream, nil
}
func (cancellationController) Resume(context.Context, Resume) (<-chan event.Event, error) {
	return nil, errors.New("unused")
}
func (cancellationController) Fork(context.Context, Fork) (<-chan event.Event, error) {
	return nil, errors.New("unused")
}

func TestStreamingCancellationPropagatesForEveryLocalSurface(t *testing.T) {
	doc := localSurfaceDocument()
	defaults := Defaults{SessionID: "session", AgentRef: "coder"}
	for _, kind := range []string{"one-shot", "stdin", "repl", "tui"} {
		t.Run(kind, func(t *testing.T) {
			ctx, cancel := context.WithCancel(context.Background())
			renderer := RenderFunc(func(event.Event) error { cancel(); return nil })
			var err error
			switch kind {
			case "one-shot":
				s, _ := NewOneShot(doc, kind, cancellationController{})
				err = s.Run(ctx, []byte("hello"), defaults, renderer)
			case "stdin":
				s, _ := NewStdin(doc, kind, cancellationController{})
				err = s.Run(ctx, strings.NewReader("hello"), defaults, renderer)
			case "repl":
				s, _ := NewREPL(doc, kind, cancellationController{})
				err = s.Run(ctx, strings.NewReader("hello\n"), defaults, renderer)
			case "tui":
				s, _ := NewTUI(doc, kind, cancellationController{})
				err = s.Run(ctx, []byte("hello"), defaults, renderer)
			}
			if !errors.Is(err, context.Canceled) {
				t.Fatalf("error=%v", err)
			}
		})
	}
}

func TestInteractiveApprovalResumeUsesSameEventStream(t *testing.T) {
	doc := localSurfaceDocument()
	want := []event.Event{{Sequence: 4, Type: "approval.resolved"}, {Sequence: 5, Type: "turn.completed"}}
	for _, kind := range []string{"repl", "tui"} {
		t.Run(kind, func(t *testing.T) {
			controller := &fakeController{events: want}
			var got []event.Event
			renderer := RenderFunc(func(e event.Event) error { got = append(got, e); return nil })
			request := Resume{SessionID: "session", Token: "approval-1", Resolution: []byte(`{"decision":"approve"}`)}
			var err error
			if kind == "repl" {
				surface, _ := NewREPL(doc, kind, controller)
				err = surface.Resume(context.Background(), request, renderer)
			} else {
				surface, _ := NewTUI(doc, kind, controller)
				err = surface.Resume(context.Background(), request, renderer)
			}
			if err != nil {
				t.Fatal(err)
			}
			if !reflect.DeepEqual(got, want) {
				t.Fatalf("events=%#v", got)
			}
			if len(controller.resumes) != 1 || controller.resumes[0].FrontendRef != kind || !bytes.Equal(controller.resumes[0].Resolution, request.Resolution) {
				t.Fatalf("resume=%#v", controller.resumes)
			}
		})
	}
}

func TestStdinAndREPLBoundsAreCompiledConfiguration(t *testing.T) {
	doc := localSurfaceDocument()
	doc.Spec.Frontends["stdin"] = spec.Frontend{Kind: "stdin", Limits: spec.FrontendLimits{MaxInputBytes: 4}}
	stdin, _ := NewStdin(doc, "stdin", &fakeController{})
	if err := stdin.Run(context.Background(), strings.NewReader("12345"), Defaults{}, RenderFunc(func(event.Event) error { return nil })); err == nil {
		t.Fatal("oversized stdin accepted")
	}
	repl, _ := NewREPL(doc, "repl", &fakeController{})
	if err := repl.Run(context.Background(), strings.NewReader(strings.Repeat("x", 65)+"\n"), Defaults{}, RenderFunc(func(event.Event) error { return nil })); err == nil {
		t.Fatal("oversized REPL line accepted")
	}
}
