package pipeline

// Sprint: #323; Story: #1115; Story-ID: a7f947d09851

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/qiangli/ycode/internal/harness/spec"
)

// A turn cancelled by its frontend (ESC) must start no further stage — not
// the next model call, tool call or file write — and a loop must end on the
// cancellation instead of spinning to its iteration limit. Only nodes that
// declare runOn: cancelled still run.
func TestCancelledRunStartsNoFurtherStage(t *testing.T) {
	reg := NewRegistry()
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	var tool, model, cleanup int
	_ = reg.Register(Definition{Name: "tool", Handler: func(ctx context.Context, in Invocation) Outcome {
		tool++
		cancel() // ESC arrives while the tool runs
		return Success(map[string]any{"value": in.Inputs["value"]})
	}})
	_ = reg.Register(Definition{Name: "model", Handler: func(_ context.Context, in Invocation) Outcome {
		model++
		return Success(map[string]any{"value": in.Inputs["value"]})
	}})
	_ = reg.Register(Definition{Name: "cleanup", Handler: func(context.Context, Invocation) Outcome {
		cleanup++
		return Success(nil)
	}})
	step := spec.Pipeline{Inputs: map[string]string{"value": "n"}, Outputs: map[string]string{"value": "n"}, Concurrency: 1, Nodes: []spec.Stage{
		{ID: "tool", Run: spec.Run{Stage: "tool", In: map[string]string{"value": "value"}, Out: map[string]string{"value": "value"}}},
		{ID: "model", Needs: []string{"tool"}, Run: spec.Run{Stage: "model", In: map[string]string{"value": "value"}, Out: map[string]string{"value": "value"}}},
	}}
	main := spec.Pipeline{Inputs: map[string]string{"n": "n"}, State: map[string]spec.StateSlot{"n": {Type: "n", Writer: "loop-carried"}}, Concurrency: 1, Nodes: []spec.Stage{
		{ID: "loop", Run: spec.Run{Repeat: &spec.RepeatRun{PipelineRef: "step", MaxIterations: 5, Carry: map[string]string{"value": "n"}, Until: spec.Expression{Eq: []spec.Operand{fld("n"), lit(-1)}}, Out: map[string]string{"value": "n"}}}},
		{ID: "cleanup", Needs: []string{"loop"}, RunOn: []string{OutcomeCancelled}, Run: spec.Run{Stage: "cleanup"}},
	}}
	s := NewState()
	_ = s.SetTyped("n", "n", 0)
	out := NewRunner(reg).WithPipelines(map[string]spec.Pipeline{"main": main, "step": step}).RunPipelineOutcome(ctx, "main", s)
	if out.Class != OutcomeCancelled || !errors.Is(out.Err, context.Canceled) {
		t.Fatalf("outcome = %#v, want cancelled with context.Canceled", out)
	}
	if tool != 1 || model != 0 {
		t.Fatalf("tool=%d model=%d: the cancelled run must stop before its next step", tool, model)
	}
	if cleanup != 1 {
		t.Fatalf("cleanup=%d: a runOn: cancelled node still runs", cleanup)
	}
}

// A run whose context is already cancelled when its graph starts must return
// a cancelled outcome, not wait on a scheduler that dispatches nothing.
func TestPreCancelledRunReturnsCancelled(t *testing.T) {
	reg := NewRegistry()
	var ran, cleanup int
	_ = reg.Register(Definition{Name: "work", Handler: func(context.Context, Invocation) Outcome { ran++; return Success(nil) }})
	_ = reg.Register(Definition{Name: "cleanup", Handler: func(context.Context, Invocation) Outcome { cleanup++; return Success(nil) }})
	main := spec.Pipeline{Concurrency: 2, Nodes: []spec.Stage{
		{ID: "work", Run: spec.Run{Stage: "work"}},
		{ID: "cleanup", Needs: []string{"work"}, RunOn: []string{OutcomeCancelled}, Run: spec.Run{Stage: "cleanup"}},
	}}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	done := make(chan Outcome, 1)
	go func() {
		done <- NewRunner(reg).WithPipelines(map[string]spec.Pipeline{"main": main}).RunPipelineOutcome(ctx, "main", NewState())
	}()
	select {
	case out := <-done:
		if out.Class != OutcomeCancelled || !errors.Is(out.Err, context.Canceled) {
			t.Fatalf("outcome = %#v, want cancelled", out)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("pre-cancelled run never returned")
	}
	if ran != 0 || cleanup != 1 {
		t.Fatalf("work=%d cleanup=%d, want 0 and 1", ran, cleanup)
	}
}
