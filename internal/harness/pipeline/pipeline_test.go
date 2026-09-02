package pipeline

import (
	"context"
	"errors"
	"reflect"
	"strings"
	"sync"
	"testing"

	"github.com/qiangli/ycode/internal/harness/spec"
)

func TestRunnerExecutesDeclaredOrder(t *testing.T) {
	registry := NewRegistry()
	var order []string
	for _, name := range []string{"input.read", "llm.call", "output.emit"} {
		name := name
		if err := registry.RegisterStage(name, func(context.Context, *State, map[string]any) error {
			order = append(order, name)
			return nil
		}); err != nil {
			t.Fatal(err)
		}
	}
	stages := []spec.Stage{{ID: "in", Use: "input.read"}, {ID: "model", Use: "llm.call"}, {ID: "out", Use: "output.emit"}}
	if err := NewRunner(registry).Run(context.Background(), stages, NewState()); err != nil {
		t.Fatal(err)
	}
	if want := []string{"input.read", "llm.call", "output.emit"}; !reflect.DeepEqual(order, want) {
		t.Fatalf("order = %v, want %v", order, want)
	}
}

func TestRunnerRepeatAndConditional(t *testing.T) {
	registry := NewRegistry()
	if err := registry.RegisterStage("llm.call", func(_ context.Context, state *State, _ map[string]any) error {
		value, _ := state.Get("calls")
		calls, _ := value.(int)
		state.Set("calls", calls+1)
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	if err := registry.RegisterStage("bashy.execute", func(_ context.Context, state *State, _ map[string]any) error {
		state.Set("executed", true)
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	if err := registry.RegisterPredicate("llm.finished", func(state *State) bool {
		value, _ := state.Get("calls")
		calls, _ := value.(int)
		return calls == 2
	}); err != nil {
		t.Fatal(err)
	}
	if err := registry.RegisterPredicate("llm.hasToolCalls", func(state *State) bool {
		value, _ := state.Get("calls")
		calls, _ := value.(int)
		return calls == 1
	}); err != nil {
		t.Fatal(err)
	}
	stages := []spec.Stage{{ID: "loop", Repeat: &spec.Repeat{
		Max:   3,
		Until: "llm.finished",
		Stages: []spec.Stage{
			{ID: "model", Use: "llm.call"},
			{When: "llm.hasToolCalls", Stages: []spec.Stage{{ID: "tool", Use: "bashy.execute"}}},
		},
	}}}
	state := NewState()
	if err := NewRunner(registry).Run(context.Background(), stages, state); err != nil {
		t.Fatal(err)
	}
	if value, _ := state.Get("executed"); value != true {
		t.Fatalf("executed = %#v", value)
	}
}

func TestRunnerReportsRepeatLimit(t *testing.T) {
	registry := NewRegistry()
	_ = registry.RegisterStage("llm.call", func(context.Context, *State, map[string]any) error { return nil })
	_ = registry.RegisterPredicate("llm.finished", func(*State) bool { return false })
	err := NewRunner(registry).Run(context.Background(), []spec.Stage{{ID: "loop", Repeat: &spec.Repeat{
		Max: 2, Until: "llm.finished", Stages: []spec.Stage{{Use: "llm.call"}},
	}}}, NewState())
	if err == nil || !strings.Contains(err.Error(), "repeat limit 2 reached") {
		t.Fatalf("error = %v", err)
	}
}

func TestRunnerFallbackStopsAfterSuccess(t *testing.T) {
	registry := NewRegistry()
	_ = registry.RegisterStage("memory.recall", func(context.Context, *State, map[string]any) error { return errors.New("offline") })
	_ = registry.RegisterStage("context.load", func(context.Context, *State, map[string]any) error { return nil })
	err := NewRunner(registry).Run(context.Background(), []spec.Stage{{ID: "fallback", Fallback: []spec.Stage{
		{Use: "memory.recall"}, {Use: "context.load"},
	}}}, NewState())
	if err != nil {
		t.Fatal(err)
	}
}

func TestRunnerCallRetryAndSwitch(t *testing.T) {
	registry := NewRegistry()
	attempts := 0
	_ = registry.RegisterStage("llm.call", func(context.Context, *State, map[string]any) error {
		attempts++
		if attempts == 1 {
			return errors.New("temporary")
		}
		return nil
	})
	_ = registry.RegisterStage("output.emit", func(_ context.Context, state *State, _ map[string]any) error {
		state.Set("branch", "done")
		return nil
	})
	_ = registry.RegisterPredicate("llm.finished", func(*State) bool { return true })
	pipelines := map[string]spec.Pipeline{
		"model": {Concurrency: 1, Nodes: []spec.Stage{{Retry: &spec.Retry{Max: 2, Stages: []spec.Stage{{Use: "llm.call"}}}}}},
	}
	stages := []spec.Stage{
		{ID: "model-call", Call: "model"},
		{ID: "route", Switch: &spec.Switch{Cases: []spec.Case{{When: "llm.finished", Stages: []spec.Stage{{Use: "output.emit"}}}}}},
	}
	state := NewState()
	if err := NewRunner(registry).WithPipelines(pipelines).Run(context.Background(), stages, state); err != nil {
		t.Fatal(err)
	}
	if attempts != 2 {
		t.Fatalf("attempts = %d", attempts)
	}
	if branch, _ := state.Get("branch"); branch != "done" {
		t.Fatalf("branch = %#v", branch)
	}
}

func TestRunnerPipelineUsesDAGFanOutAndFanIn(t *testing.T) {
	registry := NewRegistry()
	var mu sync.Mutex
	var order []string
	_ = registry.RegisterStage("input.read", func(_ context.Context, _ *State, with map[string]any) error {
		mu.Lock()
		defer mu.Unlock()
		order = append(order, with["name"].(string))
		return nil
	})
	p := spec.Pipeline{Concurrency: 2, FailFast: true, Nodes: []spec.Stage{
		{ID: "root", Use: "input.read", With: map[string]any{"name": "root"}},
		{ID: "left", Needs: []string{"root"}, Use: "input.read", With: map[string]any{"name": "left"}},
		{ID: "right", Needs: []string{"root"}, Use: "input.read", With: map[string]any{"name": "right"}},
		{ID: "join", Needs: []string{"left", "right"}, Use: "input.read", With: map[string]any{"name": "join"}},
	}}
	if err := NewRunner(registry).WithPipelines(map[string]spec.Pipeline{"turn": p}).RunPipeline(context.Background(), "turn", NewState()); err != nil {
		t.Fatal(err)
	}
	position := func(name string) int {
		for i, value := range order {
			if value == name {
				return i
			}
		}
		return -1
	}
	if position("root") != 0 || position("join") != 3 {
		t.Fatalf("dependency order = %v", order)
	}
}

func TestRunnerForEachCollectsInInputOrder(t *testing.T) {
	registry := NewRegistry()
	_ = registry.RegisterStage("bashy.execute", func(_ context.Context, state *State, _ map[string]any) error {
		item, _ := state.Get("call")
		state.Set("results", item.(int)*2)
		return nil
	})
	state := NewState()
	state.Set("calls", []int{3, 1, 2})
	stages := []spec.Stage{{ID: "tools", ForEach: &spec.ForEach{
		Items: "calls", As: "call", Collect: "results", MaxParallel: 2, Ordered: true,
		Stages: []spec.Stage{{Use: "bashy.execute"}},
	}}}
	if err := NewRunner(registry).Run(context.Background(), stages, state); err != nil {
		t.Fatal(err)
	}
	got, _ := state.Get("results")
	if want := []any{6, 2, 4}; !reflect.DeepEqual(got, want) {
		t.Fatalf("results = %#v, want %#v", got, want)
	}
}
