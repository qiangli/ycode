package pipeline

import (
	"context"
	"errors"
	"reflect"
	"sync"
	"testing"

	"github.com/qiangli/ycode/internal/harness/spec"
)

func lit(v any) spec.Operand         { return spec.Operand{Literal: v} }
func fld(v string) spec.Operand      { return spec.Operand{Field: v} }
func slotOf(t string) spec.StateSlot { return spec.StateSlot{Type: t, Writer: "single"} }

func TestStateTypedVersionAndCAS(t *testing.T) {
	s := NewState()
	if err := s.SetTyped("job", "job/v1", map[string]any{"n": 1}); err != nil {
		t.Fatal(err)
	}
	if err := s.CompareAndSwap("job", 0, nil); err == nil {
		t.Fatal("stale CAS succeeded")
	}
	if err := s.CompareAndSwap("job", 1, map[string]any{"n": 2}); err != nil {
		t.Fatal(err)
	}
	got, typ, version, ok := s.Read("job.n")
	if !ok || got != 2 || typ != "job/v1" || version != 2 {
		t.Fatalf("read=(%v,%s,%d,%v)", got, typ, version, ok)
	}
}

func TestClosedRegistryExplicitBindingsAndDAG(t *testing.T) {
	reg := NewRegistry()
	var mu sync.Mutex
	var order []string
	err := reg.Register(Definition{Name: "copy", Handler: func(_ context.Context, in Invocation) Outcome {
		if _, ok := in.Inputs["hidden"]; ok {
			t.Fatal("implicit state exposed")
		}
		mu.Lock()
		order = append(order, in.StageID)
		mu.Unlock()
		return Success(map[string]any{"value": in.Inputs["value"]})
	}})
	if err != nil {
		t.Fatal(err)
	}
	if err := reg.Register(Definition{Name: "copy", Handler: func(context.Context, Invocation) Outcome { return Success(nil) }}); err == nil {
		t.Fatal("duplicate accepted")
	}
	p := spec.Pipeline{Inputs: map[string]string{"input": "x"}, State: map[string]spec.StateSlot{"left": slotOf("x"), "right": slotOf("x"), "out": slotOf("x")}, Concurrency: 2, Nodes: []spec.Stage{
		{ID: "left", Run: spec.Run{Stage: "copy", In: map[string]string{"value": "input"}, Out: map[string]string{"value": "left"}}},
		{ID: "right", Run: spec.Run{Stage: "copy", In: map[string]string{"value": "input"}, Out: map[string]string{"value": "right"}}},
		{ID: "join", Needs: []string{"left", "right"}, Run: spec.Run{Stage: "copy", In: map[string]string{"value": "left"}, Out: map[string]string{"value": "out"}}},
	}}
	s := NewState()
	_ = s.SetTyped("input", "x", 7)
	_ = s.SetTyped("hidden", "x", "secret")
	if err := NewRunner(reg).WithPipelines(map[string]spec.Pipeline{"p": p}).RunPipeline(context.Background(), "p", s); err != nil {
		t.Fatal(err)
	}
	if got, _, _, _ := s.Read("out"); got != 7 {
		t.Fatalf("out=%v", got)
	}
	if order[len(order)-1] != "join" {
		t.Fatalf("order=%v", order)
	}
	p.Nodes = []spec.Stage{{ID: "bad", Run: spec.Run{Stage: "undeclared"}}}
	if err := NewRunner(reg).WithPipelines(map[string]spec.Pipeline{"p": p}).RunPipeline(context.Background(), "p", NewState()); err == nil {
		t.Fatal("unregistered stage executed")
	}
}

func TestOutcomeRoutingRunsExplicitCleanup(t *testing.T) {
	reg := NewRegistry()
	cleaned := false
	_ = reg.Register(Definition{Name: "fail", Handler: func(context.Context, Invocation) Outcome {
		return Failure("denied", false, errors.New("denied"))
	}})
	_ = reg.Register(Definition{Name: "cleanup", Handler: func(context.Context, Invocation) Outcome {
		cleaned = true
		return Success(nil)
	}})
	p := spec.Pipeline{Concurrency: 1, Nodes: []spec.Stage{
		{ID: "work", Run: spec.Run{Stage: "fail"}},
		{ID: "cleanup", Needs: []string{"work"}, RunOn: []string{OutcomeFailed}, Run: spec.Run{Stage: "cleanup"}},
	}}
	err := NewRunner(reg).WithPipelines(map[string]spec.Pipeline{"p": p}).RunPipeline(context.Background(), "p", NewState())
	if err == nil || !cleaned {
		t.Fatalf("err=%v cleaned=%v", err, cleaned)
	}
}

func TestFailFastSkipsNewWorkButRunsExplicitCleanup(t *testing.T) {
	reg := NewRegistry()
	var work, cleanup int
	_ = reg.Register(Definition{Name: "fail", Handler: func(context.Context, Invocation) Outcome { return Failure("failed", false, errors.New("failed")) }})
	_ = reg.Register(Definition{Name: "work", Handler: func(context.Context, Invocation) Outcome { work++; return Success(nil) }})
	_ = reg.Register(Definition{Name: "cleanup", Handler: func(context.Context, Invocation) Outcome { cleanup++; return Success(nil) }})
	p := spec.Pipeline{Concurrency: 1, FailFast: true, Nodes: []spec.Stage{
		{ID: "fail", Run: spec.Run{Stage: "fail"}},
		{ID: "later", Needs: []string{"fail"}, RunOn: []string{OutcomeFailed}, Run: spec.Run{Stage: "cleanup"}},
		{ID: "ordinary", Needs: []string{"fail"}, Run: spec.Run{Stage: "work"}},
	}}
	out := NewRunner(reg).WithPipelines(map[string]spec.Pipeline{"p": p}).RunPipelineOutcome(context.Background(), "p", NewState())
	if out.Class != OutcomeFailed || work != 0 || cleanup != 1 {
		t.Fatalf("out=%#v work=%d cleanup=%d", out, work, cleanup)
	}
}

func TestStructuredControlForms(t *testing.T) {
	reg := NewRegistry()
	attempts := 0
	_ = reg.Register(Definition{Name: "flaky", Handler: func(context.Context, Invocation) Outcome {
		attempts++
		if attempts == 1 {
			return Failure("temporary", true, errors.New("retry"))
		}
		return Success(map[string]any{"value": 1})
	}})
	_ = reg.Register(Definition{Name: "inc", Handler: func(_ context.Context, in Invocation) Outcome {
		return Success(map[string]any{"value": in.Inputs["value"].(int) + 1})
	}})
	_ = reg.Register(Definition{Name: "double", Handler: func(_ context.Context, in Invocation) Outcome {
		return Success(map[string]any{"value": in.Inputs["value"].(int) * 2})
	}})
	_ = reg.Register(Definition{Name: "fail", Handler: func(context.Context, Invocation) Outcome { return Failure("offline", false, errors.New("offline")) }})
	unit := func(stage string) spec.Pipeline {
		return spec.Pipeline{Inputs: map[string]string{"value": "n"}, Outputs: map[string]string{"value": "n"}, Concurrency: 1, Nodes: []spec.Stage{{ID: "one", Run: spec.Run{Stage: stage, In: map[string]string{"value": "value"}, Out: map[string]string{"value": "value"}}}}}
	}
	fail := spec.Pipeline{Inputs: map[string]string{"value": "n"}, Outputs: map[string]string{"value": "n"}, Concurrency: 1, Nodes: []spec.Stage{{ID: "fail", Run: spec.Run{Stage: "fail"}}}}
	main := spec.Pipeline{Inputs: map[string]string{"items": "list"}, State: map[string]spec.StateSlot{"n": {Type: "n", Writer: "loop-carried"}, "choice": slotOf("n"), "results": {Type: "list", Writer: "single", Merge: "input-order"}, "fallback": slotOf("n")}, Concurrency: 1, Nodes: []spec.Stage{
		{ID: "retry", RetryV1: &spec.RetryPolicy{MaxAttempts: 2, When: spec.Expression{Eq: []spec.Operand{fld("outcome.retryable"), lit(true)}}}, Run: spec.Run{Stage: "flaky", Out: map[string]string{"value": "n"}}},
		{ID: "repeat", Needs: []string{"retry"}, Run: spec.Run{Repeat: &spec.RepeatRun{PipelineRef: "inc", MaxIterations: 3, Carry: map[string]string{"value": "n"}, Until: spec.Expression{GTE: []spec.Operand{fld("n"), lit(3)}}, Out: map[string]string{"value": "n"}}}},
		{ID: "switch", Needs: []string{"repeat"}, Run: spec.Run{Switch: &spec.SwitchRun{Cases: []spec.SwitchRunCase{{When: spec.Expression{Eq: []spec.Operand{fld("n"), lit(3)}}, PipelineRef: "double", In: map[string]string{"value": "n"}}}, NoMatch: "fail", Out: map[string]string{"value": "choice"}}}},
		{ID: "each", Needs: []string{"switch"}, Run: spec.Run{ForEach: &spec.ForEachRun{Items: spec.FieldOperand{Field: "items"}, As: "item", MaxItems: 4, MaxParallel: 2, Ordered: true, PipelineRef: "double", In: map[string]string{"value": "item"}, Collect: map[string]string{"value": "results"}}}},
		{ID: "fallback", Needs: []string{"each"}, Run: spec.Run{Fallback: &spec.FallbackRun{MaxAttempts: 2, Attempts: []spec.FallbackAttempt{{PipelineRef: "fail", On: []string{"failed"}, In: map[string]string{"value": "n"}}, {PipelineRef: "double", On: []string{"failed"}, In: map[string]string{"value": "n"}}}, NoMatch: "fail", Out: map[string]string{"value": "fallback"}}}},
	}}
	r := NewRunner(reg).WithPipelines(map[string]spec.Pipeline{"main": main, "inc": unit("inc"), "double": unit("double"), "fail": fail})
	s := NewState()
	_ = s.SetTyped("items", "list", []any{3, 1, 2})
	if err := r.RunPipeline(context.Background(), "main", s); err != nil {
		t.Fatal(err)
	}
	choice, _, _, _ := s.Read("choice")
	results, _, _, _ := s.Read("results")
	fallback, _, _, _ := s.Read("fallback")
	if choice != 6 || fallback != 6 || !reflect.DeepEqual(results, []any{6, 2, 4}) {
		t.Fatalf("choice=%v results=%v fallback=%v", choice, results, fallback)
	}
}

func TestHookInvocationLimitsAndFailureMode(t *testing.T) {
	reg := NewRegistry()
	_ = reg.Register(Definition{Name: "fail", Handler: func(context.Context, Invocation) Outcome { return Failure("hook-failed", false, errors.New("boom")) }})
	hookPipeline := spec.Pipeline{Concurrency: 1, Nodes: []spec.Stage{{ID: "body", Run: spec.Run{Stage: "fail"}}}}
	main := spec.Pipeline{Concurrency: 1, Nodes: []spec.Stage{
		{ID: "first", Run: spec.Run{HookInvoke: "audit"}},
		{ID: "second", Needs: []string{"first"}, RunOn: []string{OutcomeObserved}, Run: spec.Run{HookInvoke: "audit"}},
	}}
	runner := NewRunner(reg).WithPipelines(map[string]spec.Pipeline{"main": main, "hook": hookPipeline}).WithHooks(map[string]spec.Hook{"audit": {PipelineRef: "hook", MaxInvocations: 1, Failure: "continue"}})
	out := runner.RunPipelineOutcome(context.Background(), "main", NewState())
	if out.Class != OutcomeFailed || out.Code != "hook-limit" {
		t.Fatalf("outcome = %#v", out)
	}
}
