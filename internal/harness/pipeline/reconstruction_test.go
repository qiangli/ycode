package pipeline

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	api "github.com/qiangli/ycode/internal/api"
	"github.com/qiangli/ycode/internal/harness/event"
	"github.com/qiangli/ycode/internal/harness/provider"
	"github.com/qiangli/ycode/internal/harness/spec"
)

type reconstructionTrace struct {
	mu     sync.Mutex
	events []traceEvent
}
type traceEvent struct {
	Type      string `json:"type"`
	Stage     string `json:"stage,omitempty"`
	Mechanism string `json:"mechanism,omitempty"`
	Tool      string `json:"tool,omitempty"`
}

func (r *reconstructionTrace) add(event traceEvent) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.events = append(r.events, event)
}
func (r *reconstructionTrace) jsonl() string {
	r.mu.Lock()
	defer r.mu.Unlock()
	var out strings.Builder
	for _, e := range r.events {
		data, _ := json.Marshal(e)
		out.Write(data)
		out.WriteByte('\n')
	}
	return out.String()
}

func TestReconstructionFixturesCompileAndMatchGoldenTraces(t *testing.T) {
	root := filepath.Join("..", "..", "..")
	profiles := []string{"ycode", "codex-like", "opencode-like", "openclaw-like", "hermes-like"}
	for _, profile := range profiles {
		profile := profile
		t.Run(profile, func(t *testing.T) {
			t.Parallel()
			path := filepath.Join(root, "examples", "harness-reconstructions", profile+".yaml")
			doc, err := spec.Load(path)
			if err != nil {
				t.Fatal(err)
			}
			if doc.Metadata.Annotations["dhnt.io/compatibility"] != "behavioral" || doc.Metadata.Annotations["dhnt.io/bashy-command-kit"] != "portable-v1" {
				t.Fatalf("fixture must explicitly declare behavioral compatibility and portable Bashy kit: %#v", doc.Metadata.Annotations)
			}
			if doc.Spec.Bashy.Contract != "bashy-run-v1" || len(doc.Spec.Bashy.Operations) != 1 || doc.Spec.Bashy.Operations[0] != "execute" || doc.Spec.Bashy.Execution.CommandResolution != "embedded-only" {
				t.Fatalf("fixture exposes a tool surface other than embedded Bashy execute: %#v", doc.Spec.Bashy)
			}
			trace := &reconstructionTrace{}
			registry := reconstructionRegistry(t, trace)
			state := NewState()
			if err := state.SetTyped("request", doc.Spec.Pipelines["turn"].Inputs["request"], map[string]any{"text": "hello"}); err != nil {
				t.Fatal(err)
			}
			runner := NewRunner(registry).WithPipelines(doc.Spec.Pipelines).WithHooks(doc.Spec.Hooks)
			if err := runner.RunPipeline(context.Background(), "turn", state); err != nil {
				t.Fatal(err)
			}
			golden, err := os.ReadFile(filepath.Join(root, "testdata", "harness-reconstructions", profile+".golden.jsonl"))
			if err != nil {
				t.Fatal(err)
			}
			if got := trace.jsonl(); got != string(golden) {
				t.Fatalf("trace mismatch\n--- got ---\n%s--- want ---\n%s", got, golden)
			}
		})
	}
}

func TestNeutralKernelHasNoReconstructionBranches(t *testing.T) {
	root := filepath.Join("..", "..", "..", "internal", "harness")
	products := []string{"codex", "opencode", "openclaw", "hermes"}
	err := filepath.WalkDir(root, func(path string, entry os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if entry.IsDir() || !strings.HasSuffix(path, ".go") || strings.HasSuffix(path, "_test.go") {
			return nil
		}
		data, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		lower := strings.ToLower(string(data))
		for _, product := range products {
			if strings.Contains(lower, product) {
				t.Fatalf("product-specific runtime branch %q in %s", product, path)
			}
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
}

func TestReconstructionRestartResumeMatchesGoldenTrace(t *testing.T) {
	root := filepath.Join("..", "..", "..")
	want, err := os.ReadFile(filepath.Join(root, "testdata", "harness-reconstructions", "restart-resume.golden.jsonl"))
	if err != nil {
		t.Fatal(err)
	}
	for _, profile := range []string{"ycode", "codex-like", "opencode-like", "openclaw-like", "hermes-like"} {
		profile := profile
		t.Run(profile, func(t *testing.T) {
			path := filepath.Join(t.TempDir(), "events.jsonl")
			store, err := event.Open(path)
			if err != nil {
				t.Fatal(err)
			}
			for _, kind := range []string{"checkpoint.saved", "approval.requested"} {
				if _, err := store.Append(event.Draft{SessionID: "fixture", RunID: "before-restart", Type: kind, Data: map[string]any{"profile": profile}}); err != nil {
					t.Fatal(err)
				}
			}
			reopened, err := event.Open(path)
			if err != nil {
				t.Fatal(err)
			}
			for _, kind := range []string{"approval.resolved", "turn.completed"} {
				if _, err := reopened.Append(event.Draft{SessionID: "fixture", RunID: "after-restart", Type: kind, Data: map[string]any{"profile": profile}}); err != nil {
					t.Fatal(err)
				}
			}
			events, err := event.Replay(path)
			if err != nil {
				t.Fatal(err)
			}
			var got strings.Builder
			for index, persisted := range events {
				if persisted.Sequence != uint64(index+1) {
					t.Fatalf("sequence[%d]=%d", index, persisted.Sequence)
				}
				if index > 0 && persisted.PreviousDigest != events[index-1].Digest {
					t.Fatalf("digest chain breaks at sequence %d", persisted.Sequence)
				}
				encoded, _ := json.Marshal(traceEvent{Type: persisted.Type})
				got.Write(encoded)
				got.WriteByte('\n')
			}
			if got.String() != string(want) {
				t.Fatalf("restart trace mismatch\n--- got ---\n%s--- want ---\n%s", got.String(), want)
			}
		})
	}
}

func TestReconstructionDifferentialCoverageIsComplete(t *testing.T) {
	root := filepath.Join("..", "..", "..")
	var corpus strings.Builder
	for _, profile := range []string{"ycode", "codex-like", "opencode-like", "openclaw-like", "hermes-like"} {
		for _, path := range []string{
			filepath.Join(root, "examples", "harness-reconstructions", profile+".yaml"),
			filepath.Join(root, "testdata", "harness-reconstructions", profile+".golden.jsonl"),
		} {
			data, err := os.ReadFile(path)
			if err != nil {
				t.Fatal(err)
			}
			corpus.Write(data)
		}
	}
	restart, err := os.ReadFile(filepath.Join(root, "testdata", "harness-reconstructions", "restart-resume.golden.jsonl"))
	if err != nil {
		t.Fatal(err)
	}
	corpus.Write(restart)
	required := map[string]string{
		"context assembly": "prompt.assemble", "prompt/cache boundary": "breakAfter:",
		"memory": "memory.recall", "compaction": "memory.compact", "queue/steering": "queue.drain",
		"provider": "provider.outcome", "tool": "bashy.execute", "HITL": "hitl.review",
		"retry": "retryProbe", "subagent": "agent.invoke", "lifecycle": "lifecycle.transition",
		"output": "output.emit", "restart/resume": "approval.resolved",
	}
	for capability, marker := range required {
		if !strings.Contains(corpus.String(), marker) {
			t.Errorf("differential suite does not cover %s (missing %q)", capability, marker)
		}
	}
}

func reconstructionRegistry(t *testing.T, trace *reconstructionTrace) *Registry {
	t.Helper()
	registry := NewRegistry()
	register := func(name string, handler func(context.Context, Invocation) Outcome) {
		if err := registry.Register(Definition{Name: name, Handler: func(ctx context.Context, in Invocation) Outcome {
			trace.add(traceEvent{Type: "stage", Stage: in.StageID, Mechanism: name})
			return handler(ctx, in)
		}}); err != nil {
			t.Fatal(err)
		}
	}
	forward := func(_ context.Context, in Invocation) Outcome {
		for _, v := range in.Inputs {
			return Success(map[string]any{"value": v})
		}
		return Success(map[string]any{"value": map[string]any{}})
	}
	register("input.normalize", func(_ context.Context, in Invocation) Outcome {
		return Success(map[string]any{"input": in.Inputs["request"]})
	})
	register("lifecycle.transition", func(context.Context, Invocation) Outcome { return Success(nil) })
	register("context.load", func(context.Context, Invocation) Outcome {
		return Success(map[string]any{"context": map[string]any{"cacheBoundary": "run"}})
	})
	register("memory.recall", func(context.Context, Invocation) Outcome {
		return Success(map[string]any{"items": []any{}})
	})
	register("prompt.assemble", func(_ context.Context, in Invocation) Outcome {
		return Success(map[string]any{"state": in.Inputs})
	})
	register("state.forward", forward)
	register("event.annotate", forward)
	register("queue.drain", func(context.Context, Invocation) Outcome { return Success(map[string]any{"items": []any{}}) })
	register("context.measure", func(context.Context, Invocation) Outcome { return Success(map[string]any{"tokens": 1}) })
	register("checkpoint.save", func(_ context.Context, in Invocation) Outcome {
		return Success(map[string]any{"checkpoint": map[string]any{"state": in.Inputs["state"]}})
	})
	register("memory.compact", func(_ context.Context, in Invocation) Outcome {
		return Success(map[string]any{"state": in.Inputs["state"]})
	})
	register("policy.evaluate", func(context.Context, Invocation) Outcome {
		return Success(map[string]any{
			"decision":      map[string]any{"result": "ask"},
			"authorization": map[string]any{"scope": "fixture"},
		})
	})
	register("hitl.review", func(context.Context, Invocation) Outcome {
		return Success(map[string]any{"resolution": map[string]any{"decision": "approve"}})
	})
	register("agent.invoke", func(_ context.Context, in Invocation) Outcome {
		return Success(map[string]any{"output": in.Inputs["input"]})
	})
	var attemptMu sync.Mutex
	attempts := make(map[string]int)
	register("llm.call", func(ctx context.Context, in Invocation) Outcome {
		if retryProbe, _ := in.With["retryProbe"].(bool); retryProbe {
			attemptMu.Lock()
			attempts[in.StageID]++
			attempt := attempts[in.StageID]
			attemptMu.Unlock()
			if attempt == 1 {
				return Failure("transient", true, errors.New("deterministic retry probe"))
			}
		}
		backend := &provider.MockBackend{Events: reconstructionWireEvents()}
		adapter, err := provider.NewMock(backend)
		if err != nil {
			return Failure("provider", false, err)
		}
		for event := range adapter.Send(ctx, provider.Request{Model: "deterministic-v1", Messages: []api.Message{{Role: api.RoleUser, Content: []api.ContentBlock{{Type: api.ContentTypeText, Text: "hello"}}}}, MaxTokens: 64, Stream: true}) {
			trace.add(traceEvent{Type: "provider." + string(event.Type), Tool: toolName(event)})
		}
		wire := backend.LastRequest()
		if wire == nil || len(wire.Tools) != 1 || wire.Tools[0].Name != provider.ToolName {
			t.Fatalf("provider tools=%#v", wire)
		}
		return Success(map[string]any{"response": map[string]any{"tool": "bashy"}, "providerSession": map[string]any{"scope": "turn"}})
	})
	register("bashy.preflight", func(context.Context, Invocation) Outcome {
		return Success(map[string]any{"report": map[string]any{"complete": true}})
	})
	register("bashy.execute", func(context.Context, Invocation) Outcome {
		return Success(map[string]any{"result": []any{"one", "two"}})
	})
	register("output.emit", func(_ context.Context, in Invocation) Outcome {
		return Success(map[string]any{"output": in.Inputs["messages"]})
	})
	return registry
}

func toolName(event provider.Event) string {
	if event.ToolCall != nil {
		return event.ToolCall.Name
	}
	return ""
}
func reconstructionWireEvents() []*api.StreamEvent {
	text, _ := json.Marshal(map[string]string{"type": "text_delta", "text": "working"})
	partial, _ := json.Marshal(map[string]string{"type": "input_json_delta", "partial_json": `{"script":"echo fixture"}`})
	stop, _ := json.Marshal(map[string]string{"stop_reason": api.StopReasonToolUse})
	return []*api.StreamEvent{{Type: "message_start", Message: &api.Response{Usage: api.Usage{InputTokens: 1}}}, {Type: "content_block_delta", Delta: text}, {Type: "content_block_start", Index: 1, ContentBlock: &api.ContentBlock{Type: api.ContentTypeToolUse, ID: "fixture-call", Name: provider.ToolName, Input: json.RawMessage(`{}`)}}, {Type: "content_block_delta", Index: 1, Delta: partial}, {Type: "content_block_stop", Index: 1}, {Type: "message_delta", Usage: &api.Usage{OutputTokens: 1}, Delta: stop}, {Type: "message_stop"}}
}
