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
)

// scriptedProvider is a turn.Provider that answers the Nth model call of a
// turn with script[N] (clamped to the last entry), and records every
// request it saw so a test can inspect what the loop fed back to the model.
type scriptedProvider struct {
	script [][]provider.Event

	mu       sync.Mutex
	requests []provider.Request
}

func (p *scriptedProvider) Send(ctx context.Context, request provider.Request) <-chan provider.Event {
	p.mu.Lock()
	p.requests = append(p.requests, request)
	index := len(p.requests) - 1
	if index >= len(p.script) {
		index = len(p.script) - 1
	}
	script := p.script[index]
	p.mu.Unlock()
	events := make(chan provider.Event)
	go func() {
		defer close(events)
		for _, item := range script {
			select {
			case events <- item:
			case <-ctx.Done():
				return
			}
		}
	}()
	return events
}

func (p *scriptedProvider) callCount() int {
	p.mu.Lock()
	defer p.mu.Unlock()
	return len(p.requests)
}

func (p *scriptedProvider) requestAt(i int) provider.Request {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.requests[i]
}

func completedText(value string) []provider.Event {
	return []provider.Event{
		{Type: provider.EventTextDelta, Text: value},
		{Type: provider.EventOutcome, Outcome: &provider.Outcome{Class: provider.OutcomeCompleted, StopReason: "end_turn"}},
	}
}

func toolCallEvents(id, script string) []provider.Event {
	input, _ := json.Marshal(map[string]string{"script": script})
	return []provider.Event{
		{Type: provider.EventToolCall, ToolCall: &provider.ToolCall{ID: id, Name: provider.ToolName, Input: input}},
		{Type: provider.EventOutcome, Outcome: &provider.Outcome{Class: provider.OutcomeToolCall, StopReason: "tool_use"}},
	}
}

// TestHeadlessOneShotContinuesPastAQuestionThenFinishesOnAnAnswer
// reproduces docs/todo/1d4096a6c973: a headless one-shot turn (no human to
// answer) whose model ends on a question or menu instead of acting gets one
// bounded nudge instead of ending the turn there. The script is exactly the
// live failure mode: question, then (after the nudge) an edit, then a real
// completion that is not a question — which must end the turn for good.
func TestHeadlessOneShotContinuesPastAQuestionThenFinishesOnAnAnswer(t *testing.T) {
	scratchStores(t)
	root, err := os.MkdirTemp(".", ".turn-test-")
	if err != nil {
		t.Fatal(err)
	}
	defer os.RemoveAll(root)
	raw, err := os.ReadFile(filepath.Join("..", "..", "..", "examples", "agent.yaml"))
	if err != nil {
		t.Fatal(err)
	}
	doc, err := spec.Compile(filepath.Join(root, "agent.yaml"), raw)
	if err != nil {
		t.Fatal(err)
	}
	continuationText := doc.Spec.Sources["headless-continuation"].Resolved
	if continuationText == "" {
		t.Fatal("fixture does not declare the headless-continuation source")
	}

	question := "Where do you want to take this - tighten down, fix the Windows story, or add the missing live lifecycle test first?"
	finalAnswer := "Fixed the Windows story and verified it."
	fake := &scriptedProvider{script: [][]provider.Event{
		completedText(question),
		toolCallEvents("call-1", "printf done > change.txt"),
		completedText(finalAnswer),
	}}
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
	bashy := fakeBashy{}
	hitlController, err := hitl.New(hitl.Config{Document: doc, Events: events, Payloads: payloads, CheckpointPath: filepath.Join(root, "hitl.json"), Preflighter: bashy})
	if err != nil {
		t.Fatal(err)
	}
	runtime, err := New(Config{Document: doc, Events: events, Payloads: payloads, IO: ioEngine, Memory: memoryEngine, HITL: hitlController, Bashy: bashy, Providers: map[string]Provider{"openai": fake}, Queue: emptyQueue{}, EventPath: eventPath, Tokens: messageCounter{}})
	if err != nil {
		t.Fatal(err)
	}
	input := []byte(`{"request":"stop the Windows door on kill"}`)
	inputRef, err := payloads.Put(input)
	if err != nil {
		t.Fatal(err)
	}
	output, err := runtime.Run(context.Background(), Request{
		SessionID: "headless", RunID: "run-1", OriginFrontend: "one-shot", HumanAvailable: false,
		Input: ioctx.CanonicalInput{SchemaVersion: ioctx.SchemaVersion, TriggerRef: "interactive-input", FrontendRef: "one-shot", Principal: "test", IdempotencyKey: "run-1", Data: input, PayloadRef: inputRef},
	})
	if err != nil {
		t.Fatalf("run: %v", err)
	}
	if len(output.Deliveries) != 1 || output.Deliveries[0].SinkRef != "primary-output" {
		t.Fatalf("output = %#v", output)
	}
	if delivery.last != finalAnswer {
		t.Fatalf("delivered %q, want the real completion, not the question", delivery.last)
	}
	if got := fake.callCount(); got != 3 {
		t.Fatalf("model calls = %d, want question, continuation and edit, final answer", got)
	}

	// The second call is the one the nudge produced: its last message is the
	// continuation text, injected as a user turn right after the question.
	second := fake.requestAt(1).Messages
	if len(second) == 0 {
		t.Fatal("second request carries no messages")
	}
	last := second[len(second)-1]
	if last.Role != "user" || len(last.Content) != 1 || last.Content[0].Text != continuationText {
		t.Fatalf("second request's last message = %#v, want the headless-continuation text", last)
	}

	// The third call follows the executed edit: its last message is the
	// tool's result, not another nudge (the edit answered the question).
	third := fake.requestAt(2).Messages
	if len(third) == 0 || third[len(third)-1].Content[0].Type != "tool_result" {
		t.Fatalf("third request's last message = %#v, want the tool result from the edit", third[len(third)-1])
	}

	replayed, err := event.Replay(eventPath)
	if err != nil {
		t.Fatal(err)
	}
	continued := 0
	for _, item := range replayed {
		if item.Type != "loop.continued" {
			continue
		}
		continued++
		var data map[string]any
		if err := json.Unmarshal(item.Data, &data); err != nil {
			t.Fatal(err)
		}
		if data["count"] != float64(1) || data["max"] != float64(2) {
			t.Fatalf("loop.continued data = %#v", data)
		}
	}
	if continued != 1 {
		t.Fatalf("loop.continued events = %d, want exactly one bounded nudge", continued)
	}
}

// TestHeadlessOneShotGivesUpAfterMaxContinuations proves the nudge is bounded:
// a model that keeps asking never loops forever, and the turn still completes
// (on whatever the model last said) once the compiled limit is spent.
func TestHeadlessOneShotGivesUpAfterMaxContinuations(t *testing.T) {
	scratchStores(t)
	root, err := os.MkdirTemp(".", ".turn-test-")
	if err != nil {
		t.Fatal(err)
	}
	defer os.RemoveAll(root)
	raw, err := os.ReadFile(filepath.Join("..", "..", "..", "examples", "agent.yaml"))
	if err != nil {
		t.Fatal(err)
	}
	doc, err := spec.Compile(filepath.Join(root, "agent.yaml"), raw)
	if err != nil {
		t.Fatal(err)
	}

	question := "Which approach do you want?"
	fake := &scriptedProvider{script: [][]provider.Event{completedText(question)}}

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
	bashy := fakeBashy{}
	hitlController, err := hitl.New(hitl.Config{Document: doc, Events: events, Payloads: payloads, CheckpointPath: filepath.Join(root, "hitl.json"), Preflighter: bashy})
	if err != nil {
		t.Fatal(err)
	}
	runtime, err := New(Config{Document: doc, Events: events, Payloads: payloads, IO: ioEngine, Memory: memoryEngine, HITL: hitlController, Bashy: bashy, Providers: map[string]Provider{"openai": fake}, Queue: emptyQueue{}, EventPath: eventPath, Tokens: messageCounter{}})
	if err != nil {
		t.Fatal(err)
	}
	input := []byte(`{"request":"do the thing"}`)
	inputRef, err := payloads.Put(input)
	if err != nil {
		t.Fatal(err)
	}
	output, err := runtime.Run(context.Background(), Request{
		SessionID: "headless-stuck", RunID: "run-1", OriginFrontend: "one-shot", HumanAvailable: false,
		Input: ioctx.CanonicalInput{SchemaVersion: ioctx.SchemaVersion, TriggerRef: "interactive-input", FrontendRef: "one-shot", Principal: "test", IdempotencyKey: "run-1", Data: input, PayloadRef: inputRef},
	})
	if err != nil {
		t.Fatalf("run: %v", err)
	}
	if len(output.Deliveries) != 1 {
		t.Fatalf("output = %#v", output)
	}
	// Two nudges (the compiled maxContinuations), then the third identical
	// question ends the turn for good instead of nudging a third time.
	if got := fake.callCount(); got != 3 {
		t.Fatalf("model calls = %d, want exactly maxContinuations+1", got)
	}
	if delivery.last != question {
		t.Fatalf("delivered %q, want the model's last (unresolved) answer", delivery.last)
	}

	replayed, err := event.Replay(eventPath)
	if err != nil {
		t.Fatal(err)
	}
	continued := 0
	for _, item := range replayed {
		if item.Type == "loop.continued" {
			continued++
		}
	}
	if continued != 2 {
		t.Fatalf("loop.continued events = %d, want exactly the compiled maxContinuations", continued)
	}
}

// TestInteractiveFrontendNeverGetsTheHeadlessNudge proves the nudge is
// headless-only: the same question, on a run a human can answer, ends the
// turn immediately like before this change.
func TestInteractiveFrontendNeverGetsTheHeadlessNudge(t *testing.T) {
	scratchStores(t)
	root, err := os.MkdirTemp(".", ".turn-test-")
	if err != nil {
		t.Fatal(err)
	}
	defer os.RemoveAll(root)
	raw, err := os.ReadFile(filepath.Join("..", "..", "..", "examples", "agent.yaml"))
	if err != nil {
		t.Fatal(err)
	}
	doc, err := spec.Compile(filepath.Join(root, "agent.yaml"), raw)
	if err != nil {
		t.Fatal(err)
	}

	question := "Which approach do you want?"
	fake := &scriptedProvider{script: [][]provider.Event{completedText(question)}}

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
	bashy := fakeBashy{}
	hitlController, err := hitl.New(hitl.Config{Document: doc, Events: events, Payloads: payloads, CheckpointPath: filepath.Join(root, "hitl.json"), Preflighter: bashy})
	if err != nil {
		t.Fatal(err)
	}
	runtime, err := New(Config{Document: doc, Events: events, Payloads: payloads, IO: ioEngine, Memory: memoryEngine, HITL: hitlController, Bashy: bashy, Providers: map[string]Provider{"openai": fake}, Queue: emptyQueue{}, EventPath: eventPath, Tokens: messageCounter{}})
	if err != nil {
		t.Fatal(err)
	}
	input := []byte(`{"request":"do the thing"}`)
	inputRef, err := payloads.Put(input)
	if err != nil {
		t.Fatal(err)
	}
	// Same frontend ref, but a human can answer this one (REPL/TUI/ACP all
	// set HumanAvailable: true), so the question ends the turn as before.
	output, err := runtime.Run(context.Background(), Request{
		SessionID: "interactive", RunID: "run-1", OriginFrontend: "one-shot", HumanAvailable: true,
		Input: ioctx.CanonicalInput{SchemaVersion: ioctx.SchemaVersion, TriggerRef: "interactive-input", FrontendRef: "one-shot", Principal: "test", IdempotencyKey: "run-1", Data: input, PayloadRef: inputRef},
	})
	if err != nil {
		t.Fatalf("run: %v", err)
	}
	if len(output.Deliveries) != 1 {
		t.Fatalf("output = %#v", output)
	}
	if got := fake.callCount(); got != 1 {
		t.Fatalf("model calls = %d, want exactly one: a human can answer the question", got)
	}
	if delivery.last != question {
		t.Fatalf("delivered %q", delivery.last)
	}
	replayed, err := event.Replay(eventPath)
	if err != nil {
		t.Fatal(err)
	}
	for _, item := range replayed {
		if item.Type == "loop.continued" {
			t.Fatal("interactive run got the headless nudge")
		}
	}
}
