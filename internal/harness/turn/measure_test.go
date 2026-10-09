package turn

import (
	"context"
	"path/filepath"
	"testing"

	"github.com/qiangli/ycode/internal/harness/event"
	"github.com/qiangli/ycode/internal/harness/message"
	"github.com/qiangli/ycode/internal/harness/pipeline"
	"github.com/qiangli/ycode/internal/harness/spec"
	memoryStage "github.com/qiangli/ycode/internal/harness/stages/memory"
)

// measureTestDocument declares two models behind one route: "small" (the
// route's first/default attempt) and "big" (a far larger window), reachable
// only when a session selects it by ref.
func measureTestDocument() *spec.Document {
	doc := &spec.Document{ConfigDigest: "sha256:test", Spec: spec.Spec{
		Agents: map[string]spec.Agent{"coder": {ModelRouteRef: "main-route"}},
		Routes: map[string]spec.Route{"main-route": {
			Attempts: []spec.RouteAttempt{{ModelRef: "small"}, {ModelRef: "big"}},
			Budget:   spec.TokenBudget{MaxOutputTokens: 8},
		}},
		Models: map[string]spec.Model{
			"small": {Limits: spec.ModelLimits{ContextTokens: 1000, MaxOutputTokens: 20}},
			"big":   {Limits: spec.ModelLimits{ContextTokens: 100000, MaxOutputTokens: 20}},
		},
		Memories: map[string]spec.Memory{"main": {
			Provider:   spec.MemoryProviderBashyKB,
			Compaction: spec.CompactionPolicy{ReserveTokens: 8, RouteRef: "main-route", PromptSourceRef: "compact", UpdatePromptSourceRef: "update", OnFailure: "preserve-original"},
		}},
	}}
	return doc
}

func newMeasureRuntime(t *testing.T, doc *spec.Document) *Runtime {
	t.Helper()
	dir := t.TempDir()
	events, err := event.Open(filepath.Join(dir, "events.jsonl"))
	if err != nil {
		t.Fatal(err)
	}
	payloads, err := event.OpenPayloadStore(filepath.Join(dir, "payloads"))
	if err != nil {
		t.Fatal(err)
	}
	memory, err := memoryStage.New(memoryStage.Config{Document: doc, Events: events, Payloads: payloads, Tokens: messageCounter{}})
	if err != nil {
		t.Fatal(err)
	}
	return &Runtime{doc: doc, memory: memory}
}

// Sprint 379 Story #53 (c88f66baf976): the compiled agent-step pipeline's
// context.measure node must honor a session's `model use` selection the same
// way callModel/routeProvider already does, or a session on a model with a
// far larger window still measures against the route's default attempt and
// starts truncating tool results after a handful of turns.
func TestMeasureStageHonorsSessionSelectedModel(t *testing.T) {
	doc := measureTestDocument()
	rt := newMeasureRuntime(t, doc)
	in := pipeline.Invocation{
		StageID: "measure",
		Inputs:  map[string]any{"messages": []message.Message{}},
		With:    map[string]any{"memoryRef": "main", "routeRef": "main-route", "safetyMargin": 1.0},
	}

	ctx := context.WithValue(context.Background(), runKey{}, runContext{sessionID: "s", runID: "r", agentRef: "coder"})
	out := rt.measure(ctx, in)
	if out.Err != nil {
		t.Fatal(out.Err)
	}
	if got := out.Outputs["contextBudget"].(int); got != 984 {
		t.Fatalf("contextBudget without a session model = %d, want the route default (small, 984)", got)
	}

	ctx = context.WithValue(context.Background(), runKey{}, runContext{sessionID: "s", runID: "r", agentRef: "coder", modelRef: "big"})
	out = rt.measure(ctx, in)
	if out.Err != nil {
		t.Fatal(out.Err)
	}
	if got := out.Outputs["contextBudget"].(int); got != 99984 {
		t.Fatalf("contextBudget with session model %q = %d, want the selected model's window (99984)", "big", got)
	}
}
