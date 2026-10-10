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

// compileWithFormatErrorNudge compiles the canonical fixture and opts its
// finish-step into the text-only format-error nudge exactly the way
// examples/genie/agent.yaml declares it (with.onTextOnly naming the
// format-error source). The fixture itself stays untouched: the YAML change
// is scoped to the genie agent, so the injection mirrors that declaration
// for behavior tests — except with.maxTextOnlyContinuations, which is left
// unset here so the tests exercise the default of 1.
func compileWithFormatErrorNudge(t *testing.T, root string) (*spec.Document, string) {
	t.Helper()
	raw, err := os.ReadFile(filepath.Join("..", "..", "..", "examples", "agent.yaml"))
	if err != nil {
		t.Fatal(err)
	}
	doc, err := spec.Compile(filepath.Join(root, "agent.yaml"), raw)
	if err != nil {
		t.Fatal(err)
	}
	const formatError = "Your last reply contained no tool call, so nothing was executed. If the task is complete, reply with exactly DONE and nothing else. Otherwise continue with a bashy tool call."
	doc.Spec.Sources["format-error"] = spec.Source{Text: formatError, Resolved: formatError, Limits: spec.SourceLimits{MaxBytes: 4096}}
	finish, ok := doc.Spec.Pipelines["finish-step"]
	if !ok {
		t.Fatal("fixture has no finish-step pipeline")
	}
	if len(finish.Nodes) == 0 {
		t.Fatal("finish-step has no nodes")
	}
	if finish.Nodes[0].Run.With == nil {
		finish.Nodes[0].Run.With = map[string]any{}
	}
	finish.Nodes[0].Run.With["onTextOnly"] = "format-error"
	doc.Spec.Pipelines["finish-step"] = finish
	return doc, formatError
}

// runScriptedHeadlessTurn wires a turn runtime over doc with a scripted
// provider and runs one headless one-shot turn to completion.
func runScriptedHeadlessTurn(t *testing.T, doc *spec.Document, root, session string, script [][]provider.Event) (*scriptedProvider, *recordingDelivery, string) {
	t.Helper()
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
	bashy := fakeBashy{}
	hitlController, err := hitl.New(hitl.Config{Document: doc, Events: events, Payloads: payloads, CheckpointPath: filepath.Join(root, session+"-hitl.json"), Preflighter: bashy})
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
		SessionID: session, RunID: "run-1", OriginFrontend: "one-shot", HumanAvailable: false,
		Input: ioctx.CanonicalInput{SchemaVersion: ioctx.SchemaVersion, TriggerRef: "interactive-input", FrontendRef: "one-shot", Principal: "test", IdempotencyKey: session + "-run-1", Data: input, PayloadRef: inputRef},
	})
	if err != nil {
		t.Fatalf("run: %v", err)
	}
	if len(output.Deliveries) != 1 {
		t.Fatalf("output = %#v", output)
	}
	return fake, delivery, eventPath
}

func countContinued(t *testing.T, eventPath string) int {
	t.Helper()
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
	return continued
}

// TestHeadlessOneShotTreatsMidTaskTextOnlyAsFormatError reproduces Sprint
// 412 story 7dcd4d1c (Sprint 322 django-15280): after tool calls the model
// answered text-only ("I can't continue ...") and genie ended the turn with
// no diff. With with.onTextOnly set, that declarative reply must not finish:
// the loop appends the format-error source text and continues, and the turn
// ends on the later DONE reply.
func TestHeadlessOneShotTreatsMidTaskTextOnlyAsFormatError(t *testing.T) {
	scratchStores(t)
	root, err := os.MkdirTemp(".", ".turn-test-")
	if err != nil {
		t.Fatal(err)
	}
	defer os.RemoveAll(root)
	doc, formatError := compileWithFormatErrorNudge(t, root)

	stalled := "I can't continue because the tool interface isn't available in this turn."
	final := "DONE: applied the fix and verified it."
	fake, delivery, eventPath := runScriptedHeadlessTurn(t, doc, root, "text-only", [][]provider.Event{
		toolCallEvents("call-1", "printf one > change.txt"),
		completedText(stalled),
		toolCallEvents("call-2", "printf two >> change.txt"),
		completedText(final),
	})
	if got := fake.callCount(); got != 4 {
		t.Fatalf("model calls = %d, want tool, text-only, tool, DONE", got)
	}
	if delivery.last != final {
		t.Fatalf("delivered %q, want the DONE completion, not the stalled text-only reply", delivery.last)
	}
	if got := countContinued(t, eventPath); got != 1 {
		t.Fatalf("loop.continued events = %d, want exactly one format-error nudge", got)
	}
	// The leading tool call shifts indices by one versus the question test:
	// the nudge follows the stalled reply, so it lands in the third request
	// (after the tool result), as a user turn right after that reply.
	third := fake.requestAt(2).Messages
	if len(third) == 0 {
		t.Fatal("third request carries no messages")
	}
	last := third[len(third)-1]
	if last.Role != "user" || len(last.Content) != 1 || last.Content[0].Text != formatError {
		t.Fatalf("third request's last message = %#v, want the format-error text", last)
	}
}

// TestHeadlessTextOnlyWithNoPriorToolCallFinishes proves the nudge is
// mid-task only: a text-only reply before any tool call ran ends the turn
// as before, so pure Q&A turns never loop.
func TestHeadlessTextOnlyWithNoPriorToolCallFinishes(t *testing.T) {
	scratchStores(t)
	root, err := os.MkdirTemp(".", ".turn-test-")
	if err != nil {
		t.Fatal(err)
	}
	defer os.RemoveAll(root)
	doc, _ := compileWithFormatErrorNudge(t, root)

	answer := "The workspace already contains the fix; nothing to change."
	fake, delivery, eventPath := runScriptedHeadlessTurn(t, doc, root, "text-only-fresh", [][]provider.Event{
		completedText(answer),
	})
	if got := fake.callCount(); got != 1 {
		t.Fatalf("model calls = %d, want exactly one: no tool ran, so there is nothing to continue", got)
	}
	if delivery.last != answer {
		t.Fatalf("delivered %q", delivery.last)
	}
	if got := countContinued(t, eventPath); got != 0 {
		t.Fatalf("loop.continued events = %d, want none", got)
	}
}

// TestHeadlessDoneAfterToolsFinishes proves a reply that declares completion
// still finishes: DONE after tool calls ends the turn with no nudge.
func TestHeadlessDoneAfterToolsFinishes(t *testing.T) {
	scratchStores(t)
	root, err := os.MkdirTemp(".", ".turn-test-")
	if err != nil {
		t.Fatal(err)
	}
	defer os.RemoveAll(root)
	doc, _ := compileWithFormatErrorNudge(t, root)

	final := "DONE: applied the fix and verified it."
	fake, delivery, eventPath := runScriptedHeadlessTurn(t, doc, root, "text-only-done", [][]provider.Event{
		toolCallEvents("call-1", "printf done > change.txt"),
		completedText(final),
	})
	if got := fake.callCount(); got != 2 {
		t.Fatalf("model calls = %d, want tool then DONE", got)
	}
	if delivery.last != final {
		t.Fatalf("delivered %q", delivery.last)
	}
	if got := countContinued(t, eventPath); got != 0 {
		t.Fatalf("loop.continued events = %d, want none: DONE finishes", got)
	}
}

// TestHeadlessTextOnlyNudgeFiresOnceThenFinishes proves the format-error
// nudge has its own bound (Sprint 412 story 7dcd4d1c follow-up): a model
// that answers text-only after the nudge — the finished-run summary without
// DONE — ends the turn instead of re-verifying until the question/menu
// budget runs out. One tool call, one stalled reply, one nudge, then the
// second identical reply finishes with no second loop.continued.
func TestHeadlessTextOnlyNudgeFiresOnceThenFinishes(t *testing.T) {
	scratchStores(t)
	root, err := os.MkdirTemp(".", ".turn-test-")
	if err != nil {
		t.Fatal(err)
	}
	defer os.RemoveAll(root)
	doc, _ := compileWithFormatErrorNudge(t, root)

	stalled := "Still working on it, no tool call in this reply."
	fake, delivery, eventPath := runScriptedHeadlessTurn(t, doc, root, "text-only-stuck", [][]provider.Event{
		toolCallEvents("call-1", "printf one > change.txt"),
		completedText(stalled),
	})
	if got := fake.callCount(); got != 3 {
		t.Fatalf("model calls = %d, want tool, stalled reply, nudge, stalled reply", got)
	}
	if delivery.last != stalled {
		t.Fatalf("delivered %q, want the model's last answer ending the turn", delivery.last)
	}
	if got := countContinued(t, eventPath); got != 1 {
		t.Fatalf("loop.continued events = %d, want exactly one text-only nudge", got)
	}
}

// TestHeadlessQuestionAndTextOnlyBudgetsAreSeparate proves the two nudges
// draw on their own counters: a turn that burns the whole question/menu
// budget (the fixture's maxContinuations of 2) still gets its one text-only
// nudge afterwards, which a shared budget would have denied.
func TestHeadlessQuestionAndTextOnlyBudgetsAreSeparate(t *testing.T) {
	scratchStores(t)
	root, err := os.MkdirTemp(".", ".turn-test-")
	if err != nil {
		t.Fatal(err)
	}
	defer os.RemoveAll(root)
	doc, _ := compileWithFormatErrorNudge(t, root)

	question := "Which approach do you want?"
	stalled := "Still working on it, no tool call in this reply."
	fake, delivery, eventPath := runScriptedHeadlessTurn(t, doc, root, "separate-budgets", [][]provider.Event{
		toolCallEvents("call-1", "printf one > change.txt"),
		completedText(question),
		completedText(question),
		completedText(stalled),
	})
	// Tool, two questions (two question nudges, exhausting that budget),
	// then the stalled reply (one text-only nudge on its own counter),
	// then the second stalled reply ends the turn.
	if got := fake.callCount(); got != 5 {
		t.Fatalf("model calls = %d, want tool, question, question, stalled, stalled", got)
	}
	if delivery.last != stalled {
		t.Fatalf("delivered %q, want the model's last answer ending the turn", delivery.last)
	}
	if got := countContinued(t, eventPath); got != 3 {
		t.Fatalf("loop.continued events = %d, want two question nudges plus one text-only nudge", got)
	}
}

// TestHeadlessBareDoneAfterToolsFinishes proves a reply that is just DONE
// finishes: the completion marker works as a standalone word on its own
// line, with no summary text around it.
func TestHeadlessBareDoneAfterToolsFinishes(t *testing.T) {
	scratchStores(t)
	root, err := os.MkdirTemp(".", ".turn-test-")
	if err != nil {
		t.Fatal(err)
	}
	defer os.RemoveAll(root)
	doc, _ := compileWithFormatErrorNudge(t, root)

	fake, delivery, eventPath := runScriptedHeadlessTurn(t, doc, root, "text-only-bare-done", [][]provider.Event{
		toolCallEvents("call-1", "printf done > change.txt"),
		completedText("DONE"),
	})
	if got := fake.callCount(); got != 2 {
		t.Fatalf("model calls = %d, want tool then bare DONE", got)
	}
	if delivery.last != "DONE" {
		t.Fatalf("delivered %q, want the bare DONE completion", delivery.last)
	}
	if got := countContinued(t, eventPath); got != 0 {
		t.Fatalf("loop.continued events = %d, want none: bare DONE finishes", got)
	}
}

// TestGenieDeclaresFormatErrorNudge pins the YAML half of the story: the
// genie agent wires with.onTextOnly to the format-error source on
// finish-step, keeping the question/menu path, with each nudge on its own
// budget (maxContinuations 2 for questions/menus, maxTextOnlyContinuations
// 1 for text-only).
func TestGenieDeclaresFormatErrorNudge(t *testing.T) {
	scratchStores(t)
	geniePath := filepath.Join("..", "..", "..", "examples", "genie", "agent.yaml")
	raw, err := os.ReadFile(geniePath)
	if err != nil {
		t.Fatal(err)
	}
	// Compile at the real path so the agent's required file sources
	// resolve; Compile itself writes nothing.
	doc, err := spec.Compile(geniePath, raw)
	if err != nil {
		t.Fatal(err)
	}
	const want = "Your last reply contained no tool call, so nothing was executed. If the task is complete, reply with exactly DONE and nothing else. Otherwise continue with a bashy tool call."
	source, ok := doc.Spec.Sources["format-error"]
	if !ok {
		t.Fatal("genie agent has no format-error source")
	}
	if source.Resolved != want {
		t.Fatalf("format-error source = %q, want the mini-swe-agent style text", source.Resolved)
	}
	finish, ok := doc.Spec.Pipelines["finish-step"]
	if !ok || len(finish.Nodes) == 0 {
		t.Fatal("genie agent has no finish-step pipeline")
	}
	with := finish.Nodes[0].Run.With
	if with["onTextOnly"] != "format-error" {
		t.Fatalf("finish-step with.onTextOnly = %#v, want %q", with["onTextOnly"], "format-error")
	}
	if with["sourceRef"] != "headless-continuation" {
		t.Fatalf("finish-step with.sourceRef = %#v, want the question/menu path kept", with["sourceRef"])
	}
	asNumber := func(key string) float64 {
		t.Helper()
		switch value := with[key].(type) {
		case int:
			return float64(value)
		case float64:
			return value
		default:
			t.Fatalf("finish-step with.%s = %#v, want a number", key, with[key])
			return 0
		}
	}
	if max := asNumber("maxContinuations"); max != 2 {
		t.Fatalf("finish-step with.maxContinuations = %v, want 2 (question/menu path alone)", max)
	}
	if max := asNumber("maxTextOnlyContinuations"); max != 1 {
		t.Fatalf("finish-step with.maxTextOnlyContinuations = %v, want 1 (text-only path alone)", max)
	}
}
