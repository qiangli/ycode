package turn

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"

	api "github.com/qiangli/ycode/internal/api"
	"github.com/qiangli/ycode/internal/harness/event"
	"github.com/qiangli/ycode/internal/harness/provider"
	"github.com/qiangli/ycode/internal/harness/spec"
	"github.com/qiangli/ycode/internal/harness/stages/hitl"
	"github.com/qiangli/ycode/internal/harness/stages/ioctx"
	memoryStage "github.com/qiangli/ycode/internal/harness/stages/memory"
	sessionStage "github.com/qiangli/ycode/internal/harness/stages/session"
)

// killingBashy runs like fakeBashy and cancels the turn once the second tool
// result is returned: the process dies mid-turn, before any commit.
type killingBashy struct {
	fakeBashy
	calls  *atomic.Int32
	cancel context.CancelFunc
}

func (b killingBashy) Execute(ctx context.Context, meta hitl.Meta, call hitl.Call, binding string) (any, error) {
	result, err := b.fakeBashy.Execute(ctx, meta, call, binding)
	if strings.Contains(call.Script, "ls -la") && b.calls.Add(1) == 2 {
		b.cancel()
	}
	return result, err
}

func TestKilledTurnLeavesToolCallsInspectable(t *testing.T) {
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
	stop, _ := json.Marshal(map[string]string{"stop_reason": api.StopReasonToolUse})
	backend := &provider.MockBackend{Events: []*api.StreamEvent{
		{Type: "content_block_start", ContentBlock: &api.ContentBlock{Type: api.ContentTypeToolUse, ID: "call", Name: provider.ToolName, Input: json.RawMessage(`{"script":"ls -la\necho second line"}`)}},
		{Type: "content_block_stop"},
		{Type: "message_delta", Delta: stop},
		{Type: "message_stop"},
	}}
	adapter, err := provider.NewMock(backend)
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
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	bashy := killingBashy{calls: &atomic.Int32{}, cancel: cancel}
	hitlController, err := hitl.New(hitl.Config{Document: doc, Events: events, Payloads: payloads, CheckpointPath: filepath.Join(root, "hitl.json"), Preflighter: bashy})
	if err != nil {
		t.Fatal(err)
	}
	runtime, err := New(Config{Document: doc, Events: events, Payloads: payloads, IO: ioEngine, Memory: memoryEngine, HITL: hitlController, Bashy: bashy, Providers: map[string]Provider{"openai": adapter}, Queue: emptyQueue{}, EventPath: eventPath, Tokens: messageCounter{}})
	if err != nil {
		t.Fatal(err)
	}
	input := []byte(`{"request":"list the workspace"}`)
	inputRef, err := payloads.Put(input)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := runtime.Run(ctx, Request{SessionID: "killed", RunID: "run-1", OriginFrontend: "embed", Input: ioctx.CanonicalInput{SchemaVersion: ioctx.SchemaVersion, TriggerRef: "interactive-input", FrontendRef: "embed", Principal: "test", IdempotencyKey: "run-1", Data: input, PayloadRef: inputRef}}); err == nil {
		t.Fatal("killed turn reported success")
	}
	replayed, err := event.Replay(eventPath)
	if err != nil {
		t.Fatal(err)
	}
	for _, item := range replayed {
		if item.Type == "session.turn-committed" {
			t.Fatal("killed turn committed")
		}
	}
	calls := sessionStage.UncommittedToolCalls(replayed, "killed")
	if len(calls) != 2 {
		for _, item := range replayed {
			t.Log(item.Type, string(item.Data))
		}
		t.Fatalf("want 2 inspectable tool calls, got %+v", calls)
	}
	for _, call := range calls {
		if call.Name != provider.ToolName || call.ExitCode == nil || *call.ExitCode != 0 {
			t.Fatalf("tool call: %+v", call)
		}
		args, err := payloads.Get(call.ArgsRef)
		if err != nil || !strings.Contains(string(args), "ls -la") {
			t.Fatalf("args payload %s: %v", args, err)
		}
		result, err := payloads.Get(call.ResultRef)
		if err != nil || !strings.Contains(string(result), "exitCode") {
			t.Fatalf("result payload %s: %v", result, err)
		}
	}
}
