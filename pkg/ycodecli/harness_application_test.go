package ycodecli

import (
	"bytes"
	"context"
	"encoding/json"
	"os"
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

// emptyFirstProvider answers its first call with nothing (no text, no tool
// call) and every later call with text.
type emptyFirstProvider struct{ calls atomic.Int32 }

func (*emptyFirstProvider) Kind() api.ProviderKind { return api.ProviderOpenAI }
func (p *emptyFirstProvider) Send(_ context.Context, _ *api.Request) (<-chan *api.StreamEvent, <-chan error) {
	n := p.calls.Add(1)
	events := make(chan *api.StreamEvent, 3)
	errs := make(chan error, 1)
	go func() {
		defer close(events)
		defer close(errs)
		if n > 1 {
			text, _ := json.Marshal(map[string]string{"type": "text_delta", "text": "second-answer"})
			events <- &api.StreamEvent{Type: "content_block_delta", Delta: text}
		}
		stop, _ := json.Marshal(map[string]string{"stop_reason": api.StopReasonEndTurn})
		events <- &api.StreamEvent{Type: "message_delta", Delta: stop}
		events <- &api.StreamEvent{Type: "message_stop"}
	}()
	return events, errs
}

// An empty completion is its own outcome class: a route that lists
// empty-response in retryOn asks again; one that does not fails the turn.
func TestRouteRetriesAnEmptyResponse(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	source, err := os.ReadFile(filepath.Join("..", "..", "examples", "agent.yaml"))
	if err != nil {
		t.Fatal(err)
	}
	const retryOn = "retryOn: [transport, rate-limit, server, stream-before-first-event]"
	if !bytes.Contains(source, []byte(retryOn)) {
		t.Fatalf("examples/agent.yaml no longer carries %q", retryOn)
	}
	for _, tc := range []struct {
		name    string
		retry   bool
		calls   int32
		wantErr bool
	}{{"retried", true, 2, false}, {"not configured", false, 1, true}} {
		t.Run(tc.name, func(t *testing.T) {
			doc := source
			if tc.retry {
				doc = bytes.Replace(source, []byte(retryOn), []byte("retryOn: [transport, rate-limit, server, stream-before-first-event, empty-response]"), 1)
			}
			fixture := filepath.Join(t.TempDir(), "agent.yaml")
			if err := os.WriteFile(fixture, doc, 0o600); err != nil {
				t.Fatal(err)
			}
			provider := &emptyFirstProvider{}
			app, err := openHarnessApplication(fixture, public.WithHarnessProvider("openai", provider))
			if err != nil {
				t.Fatal(err)
			}
			defer app.Close()
			var output bytes.Buffer
			err = app.RunText(context.Background(), "one-shot", "", "hello", &output)
			if (err != nil) != tc.wantErr {
				t.Fatalf("err = %v, want error %v; output %q", err, tc.wantErr, output.String())
			}
			if got := provider.calls.Load(); got != tc.calls {
				t.Fatalf("provider calls = %d, want %d", got, tc.calls)
			}
			if !tc.wantErr && !bytes.Contains(output.Bytes(), []byte("second-answer")) {
				t.Fatalf("output %q", output.String())
			}
		})
	}
}
