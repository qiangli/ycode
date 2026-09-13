package ioctx

import (
	"context"
	"encoding/json"
	"errors"
	"path/filepath"
	"reflect"
	"strings"
	"sync"
	"testing"

	"github.com/qiangli/ycode/internal/harness/event"
	"github.com/qiangli/ycode/internal/harness/spec"
)

func TestInputContextPromptOutputReplay(t *testing.T) {
	t.Parallel()
	doc := testDocument()
	engine, logPath, payloads, delivery := testEngine(t, doc)
	meta := Meta{SessionID: "session", RunID: "run", StageID: "io", ConfigDigest: "sha256:config"}

	input, err := engine.Admit(context.Background(), meta, AdmissionRequest{
		TriggerRef: "interactive", FrontendRef: "repl", Principal: "user-1", IdempotencyKey: "request-1",
		Body: []byte(" { \"request\" : \"fix it\" } \n"),
	})
	if err != nil {
		t.Fatal(err)
	}
	if string(input.Data) != `{"request":"fix it"}` {
		t.Fatalf("canonical input = %s", input.Data)
	}
	if got, err := payloads.Get(input.PayloadRef); err != nil || string(got) != string(input.Data) {
		t.Fatalf("input payload = %q, %v", got, err)
	}

	// Engine construction freezes the compiled source snapshot.
	doc.Spec.Sources["identity"] = spec.Source{Resolved: "mutated", Limits: spec.SourceLimits{MaxBytes: 100}}
	contextValue, err := engine.LoadContext(context.Background(), meta, "coding")
	if err != nil {
		t.Fatal(err)
	}
	if got := []string{contextValue.Fragments[0].ID, contextValue.Fragments[1].ID}; !reflect.DeepEqual(got, []string{"identity", "project"}) {
		t.Fatalf("fragment order = %v", got)
	}
	if contextValue.Fragments[0].Content != "identity text" {
		t.Fatalf("source snapshot drifted: %q", contextValue.Fragments[0].Content)
	}

	prompt, err := engine.AssembleStage(context.Background(), meta, spec.Run{
		Stage: "prompt.assemble", In: map[string]string{"context": "context", "knowledge": "knowledge", "input": "input"},
		With: map[string]any{"order": []any{"context", "knowledge", "input"}},
	}, PromptPorts{Context: contextValue, Input: input, Knowledge: []PromptMessage{{Role: "system", Content: "never pkill on an outpost host", Ring: "repo", Form: "page", Ref: "kb:never-pkill-on-an-outpost-host"}}})
	if err != nil {
		t.Fatal(err)
	}
	gotPorts := make([]PortName, len(prompt.Messages))
	for i := range prompt.Messages {
		gotPorts[i] = prompt.Messages[i].Port
	}
	if want := []PortName{PortContext, PortContext, PortKnowledge, PortInput}; !reflect.DeepEqual(gotPorts, want) {
		t.Fatalf("prompt port order = %v, want %v", gotPorts, want)
	}
	if got := prompt.Messages[2]; got.Ring != "repo" || got.Form != "page" || got.Ref != "kb:never-pkill-on-an-outpost-host" {
		t.Fatalf("knowledge provenance = %#v", got)
	}

	output, err := engine.EmitStage(context.Background(), meta, spec.Run{Stage: "output.emit", In: map[string]string{"messages": "loop.messages"}, With: map[string]any{"sinkRefs": []any{"primary", "audit"}}}, "event-1", "repl", []byte("answer SECRET"))
	if err != nil {
		t.Fatal(err)
	}
	if got := []int{output.Deliveries[0].Attempts, output.Deliveries[1].Attempts}; !reflect.DeepEqual(got, []int{2, 1}) {
		t.Fatalf("delivery attempts = %v", got)
	}
	if got := delivery.snapshot(); !reflect.DeepEqual(got, []string{"primary:answer [redacted]", "primary:answer [redacted]", "audit:answer [redacted]"}) {
		t.Fatalf("deliveries = %v", got)
	}

	events, err := event.Replay(logPath)
	if err != nil {
		t.Fatal(err)
	}
	var types []string
	for _, item := range events {
		types = append(types, item.Type)
		if bytesContain(item.Data, "identity text") || bytesContain(item.Data, "answer [redacted]") {
			t.Fatalf("event %q embeds replay payload: %s", item.Type, item.Data)
		}
		if item.Type == "prompt.assembled" && (!bytesContain(item.Data, `"ring":"repo"`) || !bytesContain(item.Data, `"form":"page"`) || !bytesContain(item.Data, `"ref":"kb:never-pkill-on-an-outpost-host"`)) {
			t.Fatalf("prompt.assembled lost knowledge provenance: %s", item.Data)
		}
	}
	wantTypes := []string{"input.admitted", "context.loaded", "prompt.assembled", "output.delivery.requested", "output.delivery.failed", "output.delivery.requested", "output.delivery.completed", "output.delivery.requested", "output.delivery.completed", "output.emitted"}
	if !reflect.DeepEqual(types, wantTypes) {
		t.Fatalf("event types = %v, want %v", types, wantTypes)
	}
	for _, delivered := range output.Deliveries {
		if _, err := payloads.Get(delivered.PayloadRef); err != nil {
			t.Fatalf("replay payload %s: %v", delivered.PayloadRef, err)
		}
	}
}

func TestMechanismsFailClosedOnUndeclaredBehavior(t *testing.T) {
	t.Parallel()
	t.Run("input bound", func(t *testing.T) {
		engine, _, _, _ := testEngine(t, testDocument())
		_, err := engine.Admit(context.Background(), testMeta(), AdmissionRequest{TriggerRef: "interactive", FrontendRef: "repl", Principal: "p", IdempotencyKey: "i", Body: []byte(`{"request":"this is too large for configured bound"}`)})
		if err == nil {
			t.Fatal("oversized input was admitted")
		}
	})

	t.Run("context budget", func(t *testing.T) {
		doc := testDocument()
		contextConfig := doc.Spec.Contexts["coding"]
		contextConfig.Budget.MaxTokens = 1
		doc.Spec.Contexts["coding"] = contextConfig
		engine, _, _, _ := testEngine(t, doc)
		if _, err := engine.LoadContext(context.Background(), testMeta(), "coding"); err == nil {
			t.Fatal("over-budget context loaded")
		}
	})

	t.Run("prompt order", func(t *testing.T) {
		engine, _, _, _ := testEngine(t, testDocument())
		if _, err := engine.Assemble(context.Background(), testMeta(), PromptRequest{}); err == nil {
			t.Fatal("implicit prompt order was accepted")
		}
	})

	t.Run("output route", func(t *testing.T) {
		engine, _, _, _ := testEngine(t, testDocument())
		_, err := engine.Emit(context.Background(), testMeta(), OutputRequest{EventID: "e", OriginFrontend: "http", SinkRefs: []string{"primary"}, Content: []byte("x")})
		if err == nil {
			t.Fatal("undeclared output origin was routed")
		}
	})
}

func testDocument() *spec.Document {
	primary := testSink()
	audit := testSink()
	return &spec.Document{Spec: spec.Spec{
		Sources: map[string]spec.Source{
			"identity": {Resolved: "identity text", Limits: spec.SourceLimits{MaxBytes: 100}},
			"project":  {Resolved: "project text", Limits: spec.SourceLimits{MaxBytes: 100}},
		},
		Contexts: map[string]spec.Context{"coding": {
			Fragments: []spec.ContextFragment{
				{ID: "identity", SourceRef: "identity", Role: "system", Cache: spec.ContextCache{Scope: "stable", BreakAfter: true}},
				{ID: "project", SourceRef: "project", Role: "system", Cache: spec.ContextCache{Scope: "workspace", BreakAfter: true}},
			},
			Budget: spec.ContextBudget{MaxTokens: 20, Overflow: "fail"},
		}},
		Frontends: map[string]spec.Frontend{"repl": {Kind: "repl", Limits: spec.FrontendLimits{MaxInputBytes: 32}}},
		Triggers:  map[string]spec.Trigger{"interactive": {Kind: "frontend", FrontendRefs: []string{"repl"}, InputMapping: "canonical-v1", Idempotency: "required"}},
		Sinks:     map[string]spec.Sink{"primary": primary, "audit": audit},
	}}
}

func testSink() spec.Sink {
	return spec.Sink{Kind: "frontend-response", FrontendRefs: []string{"repl"}, DestinationAllowlist: []string{"originating-frontend"}, Redact: "test", Delivery: spec.Delivery{Mode: "at-least-once", DeduplicateBy: "event-id", Retry: spec.DeliveryRetry{MaxAttempts: 2, BackoffMS: 0, MaxBackoffMS: 0}, DeadLetter: spec.ControlPath{ControlPath: "dead-letter"}}}
}

func testEngine(t *testing.T, doc *spec.Document) (*Engine, string, *event.PayloadStore, *fakeDelivery) {
	t.Helper()
	dir := t.TempDir()
	logPath := filepath.Join(dir, "events.jsonl")
	events, err := event.Open(logPath)
	if err != nil {
		t.Fatal(err)
	}
	payloads, err := event.OpenPayloadStore(filepath.Join(dir, "payloads"))
	if err != nil {
		t.Fatal(err)
	}
	delivery := &fakeDelivery{fail: map[string]int{"primary": 1}}
	engine, err := New(Config{Document: doc, Events: events, Payloads: payloads, TokenCounter: wordCounter{}, Delivery: delivery, Redactor: testRedactor{}, DeadLetter: discardDeadLetter{}})
	if err != nil {
		t.Fatal(err)
	}
	return engine, logPath, payloads, delivery
}

func testMeta() Meta {
	return Meta{SessionID: "s", RunID: "r", StageID: "stage", ConfigDigest: "sha256:c"}
}

type wordCounter struct{}

func (wordCounter) Count(text string) (int, error) { return len(strings.Fields(text)), nil }

type testRedactor struct{}

func (testRedactor) Redact(policy string, content []byte) ([]byte, error) {
	if policy != "test" {
		return nil, errors.New("unknown redaction policy")
	}
	return []byte(strings.ReplaceAll(string(content), "SECRET", "[redacted]")), nil
}

type discardDeadLetter struct{}

func (discardDeadLetter) Store(context.Context, DeadLetterRequest) error { return nil }

type fakeDelivery struct {
	mu    sync.Mutex
	fail  map[string]int
	calls []string
}

func (f *fakeDelivery) Deliver(_ context.Context, request DeliveryRequest) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.calls = append(f.calls, request.SinkRef+":"+string(request.Content))
	if f.fail[request.SinkRef] > 0 {
		f.fail[request.SinkRef]--
		return errors.New("temporary")
	}
	return nil
}

func (f *fakeDelivery) snapshot() []string {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]string(nil), f.calls...)
}

func bytesContain(data json.RawMessage, value string) bool {
	return strings.Contains(string(data), value)
}
