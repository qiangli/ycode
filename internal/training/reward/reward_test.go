package reward

import (
	"context"
	"testing"
)

func TestBinaryReward(t *testing.T) {
	ctx := context.Background()

	br := &BinaryReward{
		Check: func(result *AgentResult) bool {
			return result.Finished
		},
	}

	score, err := br.Score(ctx, &AgentResult{Finished: true})
	if err != nil {
		t.Fatal(err)
	}
	if score != 1.0 {
		t.Errorf("expected 1.0, got %f", score)
	}

	score, err = br.Score(ctx, &AgentResult{Finished: false})
	if err != nil {
		t.Fatal(err)
	}
	if score != 0.0 {
		t.Errorf("expected 0.0, got %f", score)
	}
}

func TestMultiSignalReward(t *testing.T) {
	ctx := context.Background()

	msr := &MultiSignalReward{
		Signals: []Signal{
			{Name: "always_one", Weight: 1.0, Scorer: func(_ *AgentResult) float64 { return 1.0 }},
			{Name: "always_zero", Weight: 1.0, Scorer: func(_ *AgentResult) float64 { return 0.0 }},
		},
	}

	score, err := msr.Score(ctx, &AgentResult{})
	if err != nil {
		t.Fatal(err)
	}
	if score != 0.5 {
		t.Errorf("expected 0.5, got %f", score)
	}

	// No signals.
	empty := &MultiSignalReward{}
	score, err = empty.Score(ctx, &AgentResult{})
	if err != nil {
		t.Fatal(err)
	}
	if score != 0.0 {
		t.Errorf("expected 0.0 for empty signals, got %f", score)
	}
}

func TestToolUsageSignal(t *testing.T) {
	result := &AgentResult{
		Messages: []Message{
			{Role: "assistant", ToolCalls: []ToolCall{{Name: "Bash", Arguments: `{"cmd":"ls"}`}}},
		},
	}

	sig := ToolUsageSignal("Bash", "Read")
	if score := sig(result); score != 1.0 {
		t.Errorf("expected 1.0, got %f", score)
	}

	sig2 := ToolUsageSignal("Write")
	if score := sig2(result); score != 0.0 {
		t.Errorf("expected 0.0, got %f", score)
	}
}

func TestEfficiencySignal(t *testing.T) {
	makeResult := func(numCalls int) *AgentResult {
		calls := make([]ToolCall, numCalls)
		for i := range calls {
			calls[i] = ToolCall{Name: "Bash"}
		}
		return &AgentResult{
			Messages: []Message{{Role: "assistant", ToolCalls: calls}},
		}
	}

	sig := EfficiencySignal(3, 10)

	// At or below ideal.
	if score := sig(makeResult(2)); score != 1.0 {
		t.Errorf("expected 1.0 for 2 calls, got %f", score)
	}
	if score := sig(makeResult(3)); score != 1.0 {
		t.Errorf("expected 1.0 for 3 calls, got %f", score)
	}

	// At or above max.
	if score := sig(makeResult(10)); score != 0.0 {
		t.Errorf("expected 0.0 for 10 calls, got %f", score)
	}

	// In between.
	score := sig(makeResult(6))
	// (1.0 - (6-3)/(10-3)) = 1.0 - 3/7 ≈ 0.571
	if score < 0.57 || score > 0.58 {
		t.Errorf("expected ~0.571, got %f", score)
	}
}
