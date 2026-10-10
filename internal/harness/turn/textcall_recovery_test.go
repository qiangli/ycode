package turn

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"sync"
	"testing"

	"github.com/qiangli/ycode/internal/harness/event"
	"github.com/qiangli/ycode/internal/harness/provider"
	"github.com/qiangli/ycode/internal/harness/spec"
	"github.com/qiangli/ycode/internal/harness/stages/hitl"
	"github.com/qiangli/ycode/internal/harness/stages/ioctx"
	memoryStage "github.com/qiangli/ycode/internal/harness/stages/memory"
	sessionStage "github.com/qiangli/ycode/internal/harness/stages/session"
)

// recordingBashy runs like fakeBashy and remembers every executed script, so
// a recovery test can prove the model-written call really dispatched.
type recordingBashy struct {
	fakeBashy
	mu      sync.Mutex
	scripts []string
}

func (b *recordingBashy) Preflight(ctx context.Context, meta hitl.Meta, call hitl.Call) (hitl.Preflight, error) {
	return b.fakeBashy.Preflight(ctx, meta, call)
}

func (b *recordingBashy) Execute(ctx context.Context, meta hitl.Meta, call hitl.Call, binding string) (any, error) {
	b.mu.Lock()
	b.scripts = append(b.scripts, call.Script)
	b.mu.Unlock()
	return b.fakeBashy.Execute(ctx, meta, call, binding)
}

func (b *recordingBashy) executed() []string {
	b.mu.Lock()
	defer b.mu.Unlock()
	return append([]string(nil), b.scripts...)
}

// runScriptedRecoveryTurn wires a headless one-shot turn over the canonical
// fixture with a recording bashy boundary.
func runScriptedRecoveryTurn(t *testing.T, root, session string, script [][]provider.Event) (*scriptedProvider, *recordingBashy, *recordingDelivery, string) {
	t.Helper()
	raw, err := os.ReadFile(filepath.Join("..", "..", "..", "examples", "agent.yaml"))
	if err != nil {
		t.Fatal(err)
	}
	doc, err := spec.Compile(filepath.Join(root, "agent.yaml"), raw)
	if err != nil {
		t.Fatal(err)
	}
	fake := &scriptedProvider{script: script}
	eventPath := filepath.Join(root, "events.jsonl")
	events, err := event.Open(eventPath)
	if err != nil {
		t.Fatal(err)
	}
	payloads, err := event.OpenPayloadStore(filepath.Join(root, "payloads"))
	if err != nil {
		t.Fatal(err)
	}
	delivery := &recordingDelivery{}
	ioEngine, err := ioctx.New(ioctx.Config{Document: doc, Events: events, Payloads: payloads, TokenCounter: wordCounter{}, Delivery: delivery, Redactor: identityRedactor{}, DeadLetter: rejectingDeadLetter{}})
	if err != nil {
		t.Fatal(err)
	}
	memoryEngine, err := memoryStage.New(memoryStage.Config{Document: doc, Events: events, Payloads: payloads, Tokens: messageCounter{}})
	if err != nil {
		t.Fatal(err)
	}
	bashy := &recordingBashy{}
	hitlController, err := hitl.New(hitl.Config{Document: doc, Events: events, Payloads: payloads, CheckpointPath: filepath.Join(root, session+"-hitl.json"), Preflighter: bashy})
	if err != nil {
		t.Fatal(err)
	}
	runtime, err := New(Config{Document: doc, Events: events, Payloads: payloads, IO: ioEngine, Memory: memoryEngine, HITL: hitlController, Bashy: bashy, Providers: map[string]Provider{"openai": fake}, Queue: emptyQueue{}, EventPath: eventPath, Tokens: messageCounter{}})
	if err != nil {
		t.Fatal(err)
	}
	input := []byte(`{"request":"apply the patch"}`)
	inputRef, err := payloads.Put(input)
	if err != nil {
		t.Fatal(err)
	}
	output, err := runtime.Run(context.Background(), Request{
		SessionID: session, RunID: "run-1", OriginFrontend: "one-shot", HumanAvailable: false,
		Input: ioctx.CanonicalInput{SchemaVersion: ioctx.SchemaVersion, TriggerRef: "interactive-input", FrontendRef: "one-shot", Principal: "test", IdempotencyKey: session + "-run-1", Data: input, PayloadRef: inputRef},
	})
	if err != nil {
		t.Fatalf("run: %v", err)
	}
	if len(output.Deliveries) != 1 {
		t.Fatalf("output = %#v", output)
	}
	return fake, bashy, delivery, eventPath
}

func countEventsOfType(t *testing.T, eventPath, want string) int {
	t.Helper()
	replayed, err := event.Replay(eventPath)
	if err != nil {
		t.Fatal(err)
	}
	count := 0
	for _, item := range replayed {
		if item.Type == want {
			count++
		}
	}
	return count
}

func countRecoveryEvents(t *testing.T, eventPath string) int {
	t.Helper()
	replayed, err := event.Replay(eventPath)
	if err != nil {
		t.Fatal(err)
	}
	count := 0
	for _, item := range replayed {
		if item.Type != textToolCallRecoveredEvent {
			continue
		}
		count++
		var data map[string]any
		if err := json.Unmarshal(item.Data, &data); err != nil {
			t.Fatal(err)
		}
		if data["call_count"] != float64(1) {
			t.Fatalf("recovery event data = %#v, want call_count 1", data)
		}
	}
	return count
}

// TestTextToolCallRecoveryExecutesTheBashyCall is the Sprint 412 story #1819
// red/green test: a scripted provider returns the sample text body holding
// the bashy call, and the step executes it instead of finishing with no
// diff. The turn then ends on the model's DONE reply.
func TestTextToolCallRecoveryExecutesTheBashyCall(t *testing.T) {
	scratchStores(t)
	root, err := os.MkdirTemp(".", ".turn-test-")
	if err != nil {
		t.Fatal(err)
	}
	defer os.RemoveAll(root)
	final := "DONE: applied the fix and verified it."
	fake, bashy, delivery, eventPath := runScriptedRecoveryTurn(t, root, "text-recovery", [][]provider.Event{
		completedText(textCallSample),
		completedText(final),
	})
	if got := fake.callCount(); got != 2 {
		t.Fatalf("model calls = %d, want the text call plus DONE", got)
	}
	// The recorder also sees harness-authored bashy.run calls (kb context,
	// workspace probe, kb note), so match the recovered script by content
	// and prove the model-tool path with its tool.call.completed event,
	// which harness-authored calls never emit.
	matched := 0
	for _, script := range bashy.executed() {
		if script == textCallScript {
			matched++
		}
	}
	if matched != 1 {
		t.Fatalf("recovered script executed %d times, want exactly once", matched)
	}
	if got := countEventsOfType(t, eventPath, sessionStage.ToolCallCompleted); got != 1 {
		t.Fatalf("tool.call.completed events = %d, want the one recovered model call", got)
	}
	if delivery.last != final {
		t.Fatalf("delivered %q, want the DONE completion", delivery.last)
	}
	if got := countRecoveryEvents(t, eventPath); got != 1 {
		t.Fatalf("recovery events = %d, want exactly one %s", got, textToolCallRecoveredEvent)
	}
	// The tool result from the recovered call reaches the next model turn
	// like any structured call's does.
	second := fake.requestAt(1).Messages
	if len(second) == 0 || second[len(second)-1].Content[0].Type != "tool_result" {
		t.Fatalf("second request's last message = %#v, want the recovered call's tool result", second)
	}
}

// TestTextToolCallRecoveryRefusesProse proves prose that merely mentions the
// JSON shape never dispatches: no execution, no recovery event, and the
// text-only reply ends the turn as before.
func TestTextToolCallRecoveryRefusesProse(t *testing.T) {
	scratchStores(t)
	root, err := os.MkdirTemp(".", ".turn-test-")
	if err != nil {
		t.Fatal(err)
	}
	defer os.RemoveAll(root)
	prose := "The patch is ready for review. To call the tool, reply with {\"tool_calls\": [...]} format and include your script."
	fake, _, delivery, eventPath := runScriptedRecoveryTurn(t, root, "text-refusal", [][]provider.Event{
		completedText(prose),
	})
	if got := fake.callCount(); got != 1 {
		t.Fatalf("model calls = %d, want exactly one: prose ends the turn", got)
	}
	if got := countEventsOfType(t, eventPath, sessionStage.ToolCallCompleted); got != 0 {
		t.Fatalf("tool.call.completed events = %d, want none: prose executes nothing", got)
	}
	if delivery.last != prose {
		t.Fatalf("delivered %q, want the prose answer", delivery.last)
	}
	if got := countRecoveryEvents(t, eventPath); got != 0 {
		t.Fatalf("recovery events = %d, want none", got)
	}
}

// TestTextToolCallRecoveryRefusesPartialJSON proves a truncated tool-call
// body never dispatches either.
func TestTextToolCallRecoveryRefusesPartialJSON(t *testing.T) {
	scratchStores(t)
	root, err := os.MkdirTemp(".", ".turn-test-")
	if err != nil {
		t.Fatal(err)
	}
	defer os.RemoveAll(root)
	partial := `{"tool_calls":[{"name":"bashy","arguments":{"script":"printf hi`
	fake, _, delivery, eventPath := runScriptedRecoveryTurn(t, root, "text-partial", [][]provider.Event{
		completedText(partial),
	})
	if got := fake.callCount(); got != 1 {
		t.Fatalf("model calls = %d, want exactly one: partial JSON ends the turn", got)
	}
	if got := countEventsOfType(t, eventPath, sessionStage.ToolCallCompleted); got != 0 {
		t.Fatalf("tool.call.completed events = %d, want none: partial JSON executes nothing", got)
	}
	if delivery.last != partial {
		t.Fatalf("delivered %q, want the partial body back untouched", delivery.last)
	}
	if got := countRecoveryEvents(t, eventPath); got != 0 {
		t.Fatalf("recovery events = %d, want none", got)
	}
}
