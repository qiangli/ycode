package turn

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	api "github.com/qiangli/ycode/internal/api"
	"github.com/qiangli/ycode/internal/harness/event"
	"github.com/qiangli/ycode/internal/harness/message"
	"github.com/qiangli/ycode/internal/harness/provider"
	"github.com/qiangli/ycode/internal/harness/spec"
	"github.com/qiangli/ycode/internal/harness/stages/hitl"
	"github.com/qiangli/ycode/internal/harness/stages/ioctx"
	memoryStage "github.com/qiangli/ycode/internal/harness/stages/memory"
	memexmemory "github.com/qiangli/ycode/pkg/memex/memory"
)

func TestCompiledStarterTurnSurvivesStoreRestart(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
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

	backend := &provider.MockBackend{Events: completedWireEvents("done from yaml")}
	adapter, err := provider.NewMock(backend)
	if err != nil {
		t.Fatal(err)
	}
	delivery := &recordingDelivery{}

	run := func(runID string) {
		events, err := event.Open(filepath.Join(root, "events.jsonl"))
		if err != nil {
			t.Fatal(err)
		}
		payloads, err := event.OpenPayloadStore(filepath.Join(root, "payloads"))
		if err != nil {
			t.Fatal(err)
		}
		ioEngine, err := ioctx.New(ioctx.Config{Document: doc, Events: events, Payloads: payloads, TokenCounter: wordCounter{}, Delivery: delivery, Redactor: identityRedactor{}, DeadLetter: rejectingDeadLetter{}})
		if err != nil {
			t.Fatal(err)
		}
		memoryEngine, err := memoryStage.New(memoryStage.Config{Document: doc, Events: events, Payloads: payloads, Facade: emptyMemory{}, Tokens: messageCounter{}})
		if err != nil {
			t.Fatal(err)
		}
		bashy := fakeBashy{}
		hitlController, err := hitl.New(hitl.Config{Document: doc, Events: events, Payloads: payloads, CheckpointPath: filepath.Join(root, "hitl.json"), Preflighter: bashy})
		if err != nil {
			t.Fatal(err)
		}
		runtime, err := New(Config{Document: doc, Events: events, Payloads: payloads, IO: ioEngine, Memory: memoryEngine, HITL: hitlController, Bashy: bashy, Providers: map[string]Provider{"openai": adapter}, Queue: emptyQueue{}, Materialize: noMemoryWrites{}})
		if err != nil {
			t.Fatal(err)
		}
		inputBytes := []byte(`{"request":"finish the starter turn"}`)
		inputRef, err := payloads.Put(inputBytes)
		if err != nil {
			t.Fatal(err)
		}
		output, err := runtime.Run(context.Background(), Request{SessionID: "session", RunID: runID, OriginFrontend: "embed", Input: ioctx.CanonicalInput{SchemaVersion: ioctx.SchemaVersion, TriggerRef: "interactive-input", FrontendRef: "embed", Principal: "test", IdempotencyKey: runID, Data: inputBytes, PayloadRef: inputRef}})
		if err != nil {
			t.Fatal(err)
		}
		if len(output.Deliveries) != 1 || output.Deliveries[0].SinkRef != "primary-output" {
			t.Fatalf("output = %#v", output)
		}
		if got := backend.LastRequest(); got == nil || len(got.Tools) != 1 || got.Tools[0].Name != provider.ToolName {
			t.Fatalf("provider request = %#v", got)
		}
	}

	run("run-1")
	first, err := event.Replay(filepath.Join(root, "events.jsonl"))
	if err != nil {
		t.Fatal(err)
	}
	if len(first) == 0 {
		t.Fatal("first run emitted no replay events")
	}
	if _, err := os.Stat(checkpointPath(doc, "session", "run-1")); err != nil {
		t.Fatalf("checkpoint: %v", err)
	}

	// Reopening every durable mechanism proves the turn carries no process-only
	// execution state and the existing hash chain remains appendable.
	run("run-2")
	second, err := event.Replay(filepath.Join(root, "events.jsonl"))
	if err != nil {
		t.Fatal(err)
	}
	if len(second) <= len(first) || second[len(first)].PreviousDigest != first[len(first)-1].Digest {
		t.Fatal("restart did not continue the durable event chain")
	}
	if delivery.last != "done from yaml" {
		t.Fatalf("delivered %q", delivery.last)
	}
}

func completedWireEvents(value string) []*api.StreamEvent {
	delta, _ := json.Marshal(map[string]string{"type": "text_delta", "text": value})
	stop, _ := json.Marshal(map[string]string{"stop_reason": api.StopReasonEndTurn})
	return []*api.StreamEvent{{Type: "content_block_delta", Delta: delta}, {Type: "message_delta", Delta: stop}, {Type: "message_stop"}}
}

type wordCounter struct{}

func (wordCounter) Count(value string) (int, error) { return len(value)/4 + 1, nil }

type messageCounter struct{}

func (messageCounter) CountMessages(messages []message.Message) (int, error) {
	raw, _ := json.Marshal(messages)
	return len(raw)/4 + 1, nil
}

type recordingDelivery struct{ last string }

func (d *recordingDelivery) Deliver(_ context.Context, request ioctx.DeliveryRequest) error {
	d.last = string(request.Content)
	return nil
}

type identityRedactor struct{}

func (identityRedactor) Redact(_ string, value []byte) ([]byte, error) {
	return append([]byte(nil), value...), nil
}

type rejectingDeadLetter struct{}

func (rejectingDeadLetter) Store(context.Context, ioctx.DeadLetterRequest) error { return nil }

type emptyMemory struct{}

func (emptyMemory) Recall(context.Context, memoryStage.RecallQuery) ([]memoryStage.RecallHit, error) {
	return nil, nil
}
func (emptyMemory) Write(context.Context, *memexmemory.Memory) error { return nil }

type noMemoryWrites struct{}

func (noMemoryWrites) Materialize(context.Context, string, []message.Message) ([]*memexmemory.Memory, error) {
	return nil, nil
}

type emptyQueue struct{}

func (emptyQueue) Drain(context.Context, string, []string) ([]QueueItem, error) { return nil, nil }

type fakeBashy struct{}

func (fakeBashy) Preflight(_ context.Context, _ hitl.Meta, call hitl.Call) (hitl.Preflight, error) {
	return hitl.Preflight{Call: call, Digest: stableID(call.ID, call.Script), Complete: true, Effects: []string{"read"}, Paths: []string{"workspace"}}, nil
}
func (fakeBashy) Execute(_ context.Context, _ hitl.Meta, call hitl.Call, _ string) (any, error) {
	return map[string]any{"call_id": call.ID, "outcome": "success"}, nil
}
