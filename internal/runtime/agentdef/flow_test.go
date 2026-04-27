package agentdef

import (
	"context"
	"fmt"
	"strings"
	"testing"
)

func TestFlowExecutor_Sequence(t *testing.T) {
	actions := []Action{
		func(_ context.Context, input string) (string, error) {
			return input + "+A", nil
		},
		func(_ context.Context, input string) (string, error) {
			return input + "+B", nil
		},
		func(_ context.Context, input string) (string, error) {
			return input + "+C", nil
		},
	}

	fe := NewFlowExecutor(FlowSequence, actions)
	result, err := fe.Run(context.Background(), "start")
	if err != nil {
		t.Fatal(err)
	}
	if result != "start+A+B+C" {
		t.Errorf("sequence result = %q, want %q", result, "start+A+B+C")
	}
}

func TestFlowExecutor_Chain(t *testing.T) {
	actions := []Action{
		func(_ context.Context, input string) (string, error) {
			return "A(" + input + ")", nil
		},
		func(_ context.Context, input string) (string, error) {
			return "B(" + input + ")", nil
		},
		func(_ context.Context, input string) (string, error) {
			return "C(" + input + ")", nil
		},
	}

	fe := NewFlowExecutor(FlowChain, actions)
	result, err := fe.Run(context.Background(), "x")
	if err != nil {
		t.Fatal(err)
	}
	// Chain: A(B(C(x)))
	if result != "A(B(C(x)))" {
		t.Errorf("chain result = %q, want %q", result, "A(B(C(x)))")
	}
}

func TestFlowExecutor_Parallel(t *testing.T) {
	actions := []Action{
		func(_ context.Context, input string) (string, error) {
			return "result-A", nil
		},
		func(_ context.Context, input string) (string, error) {
			return "result-B", nil
		},
	}

	fe := NewFlowExecutor(FlowParallel, actions)
	result, err := fe.Run(context.Background(), "input")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(result, "result-A") || !strings.Contains(result, "result-B") {
		t.Errorf("parallel result should contain both results: %q", result)
	}
}

func TestFlowExecutor_Fallback(t *testing.T) {
	actions := []Action{
		func(_ context.Context, input string) (string, error) {
			return "", fmt.Errorf("fail-A")
		},
		func(_ context.Context, input string) (string, error) {
			return "success-B", nil
		},
		func(_ context.Context, input string) (string, error) {
			return "success-C", nil
		},
	}

	fe := NewFlowExecutor(FlowFallback, actions)
	result, err := fe.Run(context.Background(), "input")
	if err != nil {
		t.Fatal(err)
	}
	if result != "success-B" {
		t.Errorf("fallback result = %q, want %q", result, "success-B")
	}
}

func TestFlowExecutor_FallbackAllFail(t *testing.T) {
	actions := []Action{
		func(_ context.Context, _ string) (string, error) {
			return "", fmt.Errorf("fail-1")
		},
		func(_ context.Context, _ string) (string, error) {
			return "", fmt.Errorf("fail-2")
		},
	}

	fe := NewFlowExecutor(FlowFallback, actions)
	_, err := fe.Run(context.Background(), "input")
	if err == nil {
		t.Error("expected error when all fallback actions fail")
	}
}

func TestFlowExecutor_Loop(t *testing.T) {
	counter := 0
	actions := []Action{
		func(_ context.Context, input string) (string, error) {
			counter++
			return fmt.Sprintf("iter-%d", counter), nil
		},
	}

	fe := NewFlowExecutor(FlowLoop, actions)
	fe.SetMaxIterations(3)
	result, err := fe.Run(context.Background(), "start")
	if err != nil {
		t.Fatal(err)
	}
	if counter != 3 {
		t.Errorf("loop ran %d times, want 3", counter)
	}
	if result != "iter-3" {
		t.Errorf("loop result = %q, want %q", result, "iter-3")
	}
}

func TestFlowExecutor_Choice(t *testing.T) {
	called := make(map[string]bool)
	actions := []Action{
		func(_ context.Context, _ string) (string, error) {
			called["A"] = true
			return "A", nil
		},
		func(_ context.Context, _ string) (string, error) {
			called["B"] = true
			return "B", nil
		},
	}

	fe := NewFlowExecutor(FlowChoice, actions)
	result, err := fe.Run(context.Background(), "input")
	if err != nil {
		t.Fatal(err)
	}
	if result != "A" && result != "B" {
		t.Errorf("choice result = %q, want A or B", result)
	}
}

func TestFlowExecutor_EmptyActions(t *testing.T) {
	fe := NewFlowExecutor(FlowSequence, nil)
	result, err := fe.Run(context.Background(), "passthrough")
	if err != nil {
		t.Fatal(err)
	}
	if result != "passthrough" {
		t.Errorf("empty flow result = %q, want %q", result, "passthrough")
	}
}

func TestFlowExecutor_DefaultSequence(t *testing.T) {
	actions := []Action{
		func(_ context.Context, input string) (string, error) {
			return input + "!", nil
		},
	}
	// Empty flow type defaults to sequence.
	fe := NewFlowExecutor("", actions)
	result, err := fe.Run(context.Background(), "hello")
	if err != nil {
		t.Fatal(err)
	}
	if result != "hello!" {
		t.Errorf("default flow result = %q, want %q", result, "hello!")
	}
}

func TestFlowExecutor_ContextCancellation(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel() // cancel immediately

	actions := []Action{
		func(ctx context.Context, _ string) (string, error) {
			return "should-not-run", nil
		},
	}

	fe := NewFlowExecutor(FlowLoop, actions)
	fe.SetMaxIterations(100)
	_, err := fe.Run(ctx, "input")
	if err == nil {
		t.Error("expected context cancellation error")
	}
}
