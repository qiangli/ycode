package main

import (
	"bytes"
	"context"
	"encoding/json"
	"path/filepath"
	"sync/atomic"
	"testing"

	"github.com/qiangli/ycode/internal/api"
	public "github.com/qiangli/ycode/pkg/ycode"
)

type applicationProvider struct{ calls atomic.Int32 }

func (*applicationProvider) Kind() api.ProviderKind { return api.ProviderOpenAI }
func (p *applicationProvider) Send(_ context.Context, request *api.Request) (<-chan *api.StreamEvent, <-chan error) {
	p.calls.Add(1)
	events := make(chan *api.StreamEvent, 3)
	errors := make(chan error, 1)
	go func() {
		defer close(events)
		defer close(errors)
		text, _ := json.Marshal(map[string]string{"type": "text_delta", "text": "configured-output"})
		stop, _ := json.Marshal(map[string]string{"stop_reason": api.StopReasonEndTurn})
		events <- &api.StreamEvent{Type: "content_block_delta", Delta: text}
		events <- &api.StreamEvent{Type: "message_delta", Delta: stop}
		events <- &api.StreamEvent{Type: "message_stop"}
	}()
	return events, errors
}

func TestCompositionRootRoutesOneShotStdinREPLAndTUIThroughHarness(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	fixture, err := filepath.Abs(filepath.Join("..", "..", "examples", "agent.yaml"))
	if err != nil {
		t.Fatal(err)
	}
	provider := &applicationProvider{}
	app, err := openHarnessApplication(fixture, public.WithHarnessProvider("openai", provider))
	if err != nil {
		t.Fatal(err)
	}
	defer app.Close()

	var output bytes.Buffer
	if err := app.RunText(context.Background(), "one-shot", "", "one", &output); err != nil {
		t.Fatal(err)
	}
	if err := app.RunReader(context.Background(), "one-shot", "", bytes.NewBufferString("two\n"), &output); err != nil {
		t.Fatal(err)
	}
	if err := app.RunREPL(context.Background(), "repl", "", bytes.NewBufferString("three\n\n"), &output); err != nil {
		t.Fatal(err)
	}
	if err := app.RunREPL(context.Background(), "tui", "", bytes.NewBufferString("four\n"), &output); err != nil {
		t.Fatal(err)
	}
	if provider.calls.Load() != 4 {
		t.Fatalf("provider calls = %d, want 4", provider.calls.Load())
	}
	if got := bytes.Count(output.Bytes(), []byte("configured-output")); got != 4 {
		t.Fatalf("rendered outputs = %d, want 4: %q", got, output.String())
	}
}

func TestCompositionRootRejectsUndeclaredFrontend(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	fixture, _ := filepath.Abs(filepath.Join("..", "..", "examples", "agent.yaml"))
	app, err := openHarnessApplication(fixture, public.WithHarnessProvider("openai", &applicationProvider{}))
	if err != nil {
		t.Fatal(err)
	}
	defer app.Close()
	if err := app.RunText(context.Background(), "invented", "", "no", &bytes.Buffer{}); err == nil {
		t.Fatal("undeclared frontend was accepted")
	}
}
