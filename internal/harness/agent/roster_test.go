package agent

import (
	"context"
	"path/filepath"
	"strings"
	"testing"

	"github.com/qiangli/ycode/internal/harness/event"
	"github.com/qiangli/ycode/internal/harness/pipeline"
	"github.com/qiangli/ycode/internal/harness/spec"
)

func TestRosterUsesSamePipelineForRootAndDelegatedAgent(t *testing.T) {
	doc, runner, store, eventPath := fixture(t)
	roster, err := New(doc, runner, store)
	if err != nil {
		t.Fatal(err)
	}

	root, err := roster.Invoke(context.Background(), Request{AgentRef: "coder", Input: "root", SessionID: "s", RunID: "root"})
	if err != nil || root.Output != "root" {
		t.Fatalf("root = %#v, %v", root, err)
	}
	child, err := roster.Invoke(context.Background(), Request{AgentRef: "reviewer", CallerAgentRef: "coder", Input: "child", Depth: 1, PermissionCeiling: "read-only", EffectsCeiling: []string{"read"}, SessionID: "s", RunID: "child"})
	if err != nil || child.Output != "child" {
		t.Fatalf("child = %#v, %v", child, err)
	}

	events, err := event.Replay(filepath.Join(t.TempDir(), "unused"))
	if err == nil || events != nil {
		t.Fatal("expected missing independent event log")
	}
	logged, err := event.Replay(eventPath)
	if err != nil {
		t.Fatal(err)
	}
	want := []string{"agent.started", "agent.completed", "agent.handoff.started", "agent.handoff.completed"}
	if len(logged) != len(want) {
		t.Fatalf("events = %#v", logged)
	}
	for i := range want {
		if logged[i].Type != want[i] {
			t.Fatalf("event %d = %q", i, logged[i].Type)
		}
	}
}

func TestRosterEnforcesDeclaredDelegationAndAttenuation(t *testing.T) {
	doc, runner, store, _ := fixture(t)
	roster, _ := New(doc, runner, store)
	cases := []Request{
		{AgentRef: "reviewer", CallerAgentRef: "missing", Input: "x", Depth: 1, SessionID: "s", RunID: "r"},
		{AgentRef: "reviewer", CallerAgentRef: "coder", Input: "x", Depth: 2, SessionID: "s", RunID: "r"},
		{AgentRef: "reviewer", CallerAgentRef: "coder", Input: "x", Depth: 1, PermissionCeiling: "workspace-write", SessionID: "s", RunID: "r"},
		{AgentRef: "reviewer", CallerAgentRef: "coder", Input: "x", Depth: 1, EffectsCeiling: []string{"exec"}, SessionID: "s", RunID: "r"},
	}
	for _, request := range cases {
		if _, err := roster.Invoke(context.Background(), request); err == nil {
			t.Fatalf("accepted %#v", request)
		}
	}

	doc.Spec.Agents["reviewer"] = spec.Agent{PipelineRef: "turn", PermissionCeiling: "workspace-write", EffectsCeiling: []string{"read", "write"}}
	if _, err := roster.Invoke(context.Background(), Request{AgentRef: "reviewer", CallerAgentRef: "coder", Input: "x", Depth: 1, SessionID: "s", RunID: "r"}); err == nil || !strings.Contains(err.Error(), "authority") {
		t.Fatalf("error = %v", err)
	}
}

func TestAgentInvokeDefinitionUsesRoster(t *testing.T) {
	doc, runner, store, _ := fixture(t)
	roster, _ := New(doc, runner, store)
	definition := roster.Definition()
	out := definition.Handler(context.Background(), pipeline.Invocation{With: map[string]any{"agentRef": "reviewer"}, Inputs: map[string]any{"input": StageInput{Input: "review", CallerAgentRef: "coder", Depth: 1, SessionID: "s", RunID: "r"}}})
	if out.Class != pipeline.OutcomeSucceeded || out.Outputs["output"] != "review" {
		t.Fatalf("outcome = %#v", out)
	}
}

func fixture(t *testing.T) (*spec.Document, *pipeline.Runner, *event.Store, string) {
	t.Helper()
	path := filepath.Join(t.TempDir(), "events.jsonl")
	store, err := event.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	registry := pipeline.NewRegistry()
	if err := registry.Register(pipeline.Definition{Name: "echo", Inputs: map[string]string{"in": "text"}, Outputs: map[string]string{"out": "text"}, Handler: func(_ context.Context, in pipeline.Invocation) pipeline.Outcome {
		return pipeline.Success(map[string]any{"out": in.Inputs["in"]})
	}}); err != nil {
		t.Fatal(err)
	}
	p := spec.Pipeline{Inputs: map[string]string{"request": "text"}, State: map[string]spec.StateSlot{"output": {Type: "text", Writer: "single"}}, Outputs: map[string]string{"output": "text"}, Concurrency: 1, Nodes: []spec.Stage{{ID: "echo", Run: spec.Run{Stage: "echo", In: map[string]string{"in": "request"}, Out: map[string]string{"out": "output"}}}}}
	doc := &spec.Document{ConfigDigest: "sha256:test", Spec: spec.Spec{Pipelines: map[string]spec.Pipeline{"turn": p}, Agents: map[string]spec.Agent{
		"coder":    {PipelineRef: "turn", PermissionCeiling: "workspace-write", EffectsCeiling: []string{"read", "write"}, Delegations: []spec.Delegation{{AgentRef: "reviewer", MaxDepth: 1, MaxParallel: 2, TimeoutMS: 1000, PermissionCeiling: "read-only", EffectsCeiling: []string{"read"}}}},
		"reviewer": {PipelineRef: "turn", PermissionCeiling: "read-only", EffectsCeiling: []string{"read"}},
	}}}
	return doc, pipeline.NewRunner(registry).WithPipelines(doc.Spec.Pipelines), store, path
}
