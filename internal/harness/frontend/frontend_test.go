package frontend

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/qiangli/ycode/internal/harness/event"
	"github.com/qiangli/ycode/internal/harness/spec"
)

type fakeController struct {
	inputs  []Input
	resumes []Resume
	events  []event.Event
}

func (f *fakeController) Submit(_ context.Context, input Input) (<-chan event.Event, error) {
	f.inputs = append(f.inputs, input)
	return eventStream(f.events), nil
}
func (f *fakeController) Resume(_ context.Context, input Resume) (<-chan event.Event, error) {
	f.resumes = append(f.resumes, input)
	return eventStream(f.events), nil
}
func (f *fakeController) Fork(context.Context, Fork) (<-chan event.Event, error) {
	return nil, errors.New("unused")
}
func eventStream(events []event.Event) <-chan event.Event {
	ch := make(chan event.Event, len(events))
	for _, e := range events {
		ch <- e
	}
	close(ch)
	return ch
}

func TestLocalSurfacesRenderIdenticalCanonicalEvents(t *testing.T) {
	want := []event.Event{{Sequence: 1, Type: "text.delta"}, {Sequence: 2, Type: "turn.completed"}}
	controller := &fakeController{events: want}
	doc := &spec.Document{Spec: spec.Spec{Frontends: map[string]spec.Frontend{
		"one":  {Kind: "one-shot", HITL: true, Resume: true, Limits: spec.FrontendLimits{MaxInputBytes: 16}},
		"repl": {Kind: "repl", HITL: true, Resume: true, Limits: spec.FrontendLimits{MaxInputBytes: 16}},
	}}}
	collect := func() (Renderer, *[]event.Event) {
		var got []event.Event
		return RenderFunc(func(e event.Event) error { got = append(got, e); return nil }), &got
	}
	one, _ := NewLocal(doc, "one", controller)
	renderer, got := collect()
	if err := one.Run(context.Background(), Input{Body: []byte("hello")}, renderer); err != nil {
		t.Fatal(err)
	}
	if len(*got) != len(want) || (*got)[1].Type != want[1].Type {
		t.Fatalf("events = %#v", *got)
	}
	repl, _ := NewLocal(doc, "repl", controller)
	renderer, got = collect()
	if err := repl.REPL(context.Background(), strings.NewReader("hello\nagain\n"), Input{}, renderer); err != nil {
		t.Fatal(err)
	}
	if len(*got) != 2*len(want) || len(controller.inputs) != 3 {
		t.Fatalf("events=%d inputs=%d", len(*got), len(controller.inputs))
	}
	if err := one.Resume(context.Background(), Resume{Token: "once"}, RenderFunc(func(event.Event) error { return nil })); err != nil {
		t.Fatal(err)
	}
	if len(controller.resumes) != 1 || controller.resumes[0].FrontendRef != "one" {
		t.Fatalf("resumes = %#v", controller.resumes)
	}
}

func TestLocalEnforcesCompiledLimitsAndCancellation(t *testing.T) {
	doc := &spec.Document{Spec: spec.Spec{Frontends: map[string]spec.Frontend{"one": {Kind: "one-shot", Limits: spec.FrontendLimits{MaxInputBytes: 2}}}}}
	local, _ := NewLocal(doc, "one", &fakeController{})
	if err := local.Run(context.Background(), Input{Body: []byte("toolong")}, RenderFunc(func(event.Event) error { return nil })); err == nil {
		t.Fatal("oversized input accepted")
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if err := local.Run(ctx, Input{Body: []byte("ok")}, RenderFunc(func(event.Event) error { return nil })); !errors.Is(err, context.Canceled) {
		t.Fatalf("error = %v", err)
	}
}
