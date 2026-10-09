package turn

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/qiangli/ycode/internal/harness/event"
	"github.com/qiangli/ycode/internal/harness/message"
	"github.com/qiangli/ycode/internal/harness/provider"
	"github.com/qiangli/ycode/internal/harness/spec"
	"github.com/qiangli/ycode/internal/harness/stages/hitl"
	"github.com/qiangli/ycode/internal/harness/stages/ioctx"
	memoryStage "github.com/qiangli/ycode/internal/harness/stages/memory"
)

// newRouteTextRuntime wires the canonical fixture to a mock provider that
// always returns plain text, so each test only has to assert on what request
// the runtime sent, not on how the provider answered it.
func newRouteTextRuntime(t *testing.T, backend *provider.MockBackend) *Runtime {
	t.Helper()
	scratchStores(t)
	root, err := os.MkdirTemp(".", ".routetext-test-")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.RemoveAll(root) })
	raw, err := os.ReadFile(filepath.Join("..", "..", "..", "examples", "agent.yaml"))
	if err != nil {
		t.Fatal(err)
	}
	doc, err := spec.Compile(filepath.Join(root, "agent.yaml"), raw)
	if err != nil {
		t.Fatal(err)
	}
	eventPath := filepath.Join(root, "events.jsonl")
	events, err := event.Open(eventPath)
	if err != nil {
		t.Fatal(err)
	}
	payloads, err := event.OpenPayloadStore(filepath.Join(root, "payloads"))
	if err != nil {
		t.Fatal(err)
	}
	ioEngine, err := ioctx.New(ioctx.Config{Document: doc, Events: events, Payloads: payloads, TokenCounter: wordCounter{}, Delivery: &recordingDelivery{}, Redactor: identityRedactor{}, DeadLetter: rejectingDeadLetter{}})
	if err != nil {
		t.Fatal(err)
	}
	memoryEngine, err := memoryStage.New(memoryStage.Config{Document: doc, Events: events, Payloads: payloads, Tokens: messageCounter{}})
	if err != nil {
		t.Fatal(err)
	}
	adapter, err := provider.NewMock(backend)
	if err != nil {
		t.Fatal(err)
	}
	bashy := fakeBashy{}
	hitlController, err := hitl.New(hitl.Config{Document: doc, Events: events, Payloads: payloads, CheckpointPath: filepath.Join(root, "hitl.json"), Preflighter: bashy})
	if err != nil {
		t.Fatal(err)
	}
	runtime, err := New(Config{
		Document: doc, Events: events, Payloads: payloads, IO: ioEngine, Memory: memoryEngine, HITL: hitlController,
		Bashy: bashy, Providers: map[string]Provider{"openai": adapter}, Queue: emptyQueue{}, EventPath: eventPath, Tokens: messageCounter{},
	})
	if err != nil {
		t.Fatal(err)
	}
	return runtime
}

// Sprint 379 Story #51 (a87716307f79): a weave yoke run against glm-5.3 hit
// memory.compaction.fallback_deterministic on nearly every compaction. The
// compaction route offered the model-visible tool exactly like a normal chat
// turn (model.capabilities.toolCalls, unconditionally), so a tool-capable
// model asked to "summarize this" could answer with a bashy call instead of
// text — RouteText treats anything but a completed/limit outcome as a failed
// summarization and compaction falls back. memory.compact must ask for text
// only, the same way a plan turn already does.
func TestRouteTextForCompactionNeverOffersTheTool(t *testing.T) {
	backend := &provider.MockBackend{Events: completedWireEvents("a tight summary")}
	runtime := newRouteTextRuntime(t, backend)
	ctx := bashyRunContext()
	messages := []message.Message{{Role: message.RoleUser, Content: []message.ContentBlock{{Type: message.ContentTypeText, Text: "turns to summarize"}}}}
	summary, err := runtime.RouteText(ctx, "main", "summarize", messages)
	if err != nil {
		t.Fatalf("RouteText: %v", err)
	}
	if summary != "a tight summary" {
		t.Fatalf("summary = %q", summary)
	}
	if tools := backend.LastRequest().Tools; len(tools) != 0 {
		t.Fatalf("compaction offered the model-visible tool: %#v", tools)
	}
}

// The fix must be scoped to memory.compact: an ordinary chat turn on the same
// route still needs the tool to call bashy at all.
func TestRouteProviderOffersTheToolOutsideCompaction(t *testing.T) {
	backend := &provider.MockBackend{Events: completedWireEvents("hello")}
	runtime := newRouteTextRuntime(t, backend)
	ctx := bashyRunContext()
	messages := []message.Message{{Role: message.RoleUser, Content: []message.ContentBlock{{Type: message.ContentTypeText, Text: "hi"}}}}
	_, outcome := runtime.routeProvider(ctx, "model", "main", "", messages, nil)
	if outcome.Error != "" {
		t.Fatalf("routeProvider: %v", outcome.Error)
	}
	if tools := backend.LastRequest().Tools; len(tools) == 0 {
		t.Fatal("an ordinary chat turn must still offer the model-visible tool")
	}
}
