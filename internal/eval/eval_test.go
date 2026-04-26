package eval

import (
	"context"
	"fmt"
	"testing"
	"time"
)

func TestScenarioDefaults(t *testing.T) {
	s := &Scenario{Tier: TierSmoke, Policy: UsuallyPasses}

	if got := s.EffectiveTrials(); got != 3 {
		t.Errorf("EffectiveTrials() = %d, want 3", got)
	}
	if got := s.EffectivePassThreshold(); got != 2 {
		t.Errorf("EffectivePassThreshold() = %d, want 2", got)
	}
	if got := s.EffectiveTimeout(); got != 60*time.Second {
		t.Errorf("EffectiveTimeout() = %v, want 60s", got)
	}
	if got := s.EffectiveMaxTurns(); got != 20 {
		t.Errorf("EffectiveMaxTurns() = %d, want 20", got)
	}
}

func TestScenarioContractDefaults(t *testing.T) {
	s := &Scenario{Tier: TierContract, Policy: AlwaysPasses}

	if got := s.EffectiveTrials(); got != 1 {
		t.Errorf("EffectiveTrials() = %d, want 1", got)
	}
	if got := s.EffectivePassThreshold(); got != 1 {
		t.Errorf("EffectivePassThreshold() = %d, want 1 (AlwaysPasses)", got)
	}
	if got := s.EffectiveTimeout(); got != 10*time.Second {
		t.Errorf("EffectiveTimeout() = %v, want 10s", got)
	}
}

func TestRunResultToolNames(t *testing.T) {
	r := &RunResult{
		ToolCalls: []ToolCall{
			{Name: "read_file"},
			{Name: "edit_file"},
			{Name: "bash"},
		},
	}

	names := r.ToolNames()
	if len(names) != 3 {
		t.Fatalf("expected 3 tool names, got %d", len(names))
	}
	if names[0] != "read_file" || names[1] != "edit_file" || names[2] != "bash" {
		t.Errorf("unexpected tool names: %v", names)
	}
}

func TestRunResultTotalTokens(t *testing.T) {
	r := &RunResult{InputTokens: 500, OutputTokens: 200}
	if got := r.TotalTokens(); got != 700 {
		t.Errorf("TotalTokens() = %d, want 700", got)
	}
}

func TestTierString(t *testing.T) {
	tests := []struct {
		tier Tier
		want string
	}{
		{TierContract, "contract"},
		{TierSmoke, "smoke"},
		{TierBehavioral, "behavioral"},
		{TierE2E, "e2e"},
	}
	for _, tt := range tests {
		if got := tt.tier.String(); got != tt.want {
			t.Errorf("Tier(%d).String() = %q, want %q", int(tt.tier), got, tt.want)
		}
	}
}

func TestPolicyString(t *testing.T) {
	if got := AlwaysPasses.String(); got != "always_passes" {
		t.Errorf("AlwaysPasses.String() = %q", got)
	}
	if got := UsuallyPasses.String(); got != "usually_passes" {
		t.Errorf("UsuallyPasses.String() = %q", got)
	}
}

func TestRunnerAllPass(t *testing.T) {
	scenario := &Scenario{
		Name:   "test_all_pass",
		Tier:   TierSmoke,
		Policy: AlwaysPasses,
		Prompt: "test",
		Trials: 3,
		Assertions: []Assertion{
			ResponseContains{Substring: "hello"},
		},
	}

	runner := NewRunner(RunConfig{Provider: "test", Model: "test"}, func(ctx context.Context, s *Scenario) (*RunResult, error) {
		return &RunResult{
			Response: "hello world",
			Duration: 100 * time.Millisecond,
		}, nil
	})

	result, err := runner.Run(context.Background(), scenario)
	if err != nil {
		t.Fatal(err)
	}

	if len(result.Trials) != 3 {
		t.Fatalf("expected 3 trials, got %d", len(result.Trials))
	}

	for i, tr := range result.Trials {
		if !tr.Passed {
			t.Errorf("trial %d should have passed", i+1)
		}
	}

	if !almostEqual(result.Metrics.PassAtK, 1.0, 0.01) {
		t.Errorf("pass@k = %.4f, want 1.0", result.Metrics.PassAtK)
	}
	if !almostEqual(result.Metrics.PassPowK, 1.0, 0.01) {
		t.Errorf("pass^k = %.4f, want 1.0", result.Metrics.PassPowK)
	}
	if result.Metrics.Flakiness != 0 {
		t.Errorf("flakiness = %.4f, want 0", result.Metrics.Flakiness)
	}
}

func TestRunnerPartialFail(t *testing.T) {
	callCount := 0
	scenario := &Scenario{
		Name:   "test_partial",
		Tier:   TierSmoke,
		Policy: UsuallyPasses,
		Prompt: "test",
		Trials: 3,
		Assertions: []Assertion{
			ResponseContains{Substring: "pass"},
		},
	}

	runner := NewRunner(RunConfig{Provider: "test", Model: "test"}, func(ctx context.Context, s *Scenario) (*RunResult, error) {
		callCount++
		if callCount == 2 {
			return &RunResult{
				Response: "fail",
				Duration: 50 * time.Millisecond,
			}, nil
		}
		return &RunResult{
			Response: "pass",
			Duration: 50 * time.Millisecond,
		}, nil
	})

	result, err := runner.Run(context.Background(), scenario)
	if err != nil {
		t.Fatal(err)
	}

	passed := 0
	for _, tr := range result.Trials {
		if tr.Passed {
			passed++
		}
	}
	if passed != 2 {
		t.Errorf("expected 2 passes, got %d", passed)
	}

	// pass@3 with 2 of 3 correct should be 1.0 (guaranteed at least 1 pass in 3 draws).
	if !almostEqual(result.Metrics.PassAtK, 1.0, 0.01) {
		t.Errorf("pass@k = %.4f, want 1.0", result.Metrics.PassAtK)
	}
}

func TestRunnerWithError(t *testing.T) {
	scenario := &Scenario{
		Name:   "test_error",
		Tier:   TierSmoke,
		Policy: UsuallyPasses,
		Prompt: "test",
		Trials: 1,
	}

	runner := NewRunner(RunConfig{Provider: "test", Model: "test"}, func(ctx context.Context, s *Scenario) (*RunResult, error) {
		return &RunResult{Duration: 10 * time.Millisecond}, fmt.Errorf("provider unavailable")
	})

	result, err := runner.Run(context.Background(), scenario)
	if err != nil {
		t.Fatal(err)
	}

	if result.Trials[0].Passed {
		t.Error("trial should have failed")
	}
	if result.Trials[0].Error == "" {
		t.Error("trial should have error message")
	}
}

func TestRunnerTrajectoryScoring(t *testing.T) {
	scenario := &Scenario{
		Name:   "test_trajectory",
		Tier:   TierBehavioral,
		Policy: UsuallyPasses,
		Prompt: "test",
		Trials: 1,
		TrajectoryAssertions: []TrajectoryAssertion{
			ExpectedToolSequence{Expected: []string{"read_file", "edit_file", "bash"}},
		},
	}

	runner := NewRunner(RunConfig{Provider: "test", Model: "test"}, func(ctx context.Context, s *Scenario) (*RunResult, error) {
		return &RunResult{
			Response: "done",
			Duration: 100 * time.Millisecond,
			ToolCalls: []ToolCall{
				{Name: "read_file"},
				{Name: "edit_file"},
				{Name: "bash"},
			},
		}, nil
	})

	result, err := runner.Run(context.Background(), scenario)
	if err != nil {
		t.Fatal(err)
	}

	if !result.Trials[0].Passed {
		t.Errorf("trial should have passed, error: %s", result.Trials[0].Error)
	}
	if !almostEqual(result.Trials[0].TrajectoryScore, 1.0, 0.01) {
		t.Errorf("trajectory score = %.4f, want 1.0", result.Trials[0].TrajectoryScore)
	}
}
