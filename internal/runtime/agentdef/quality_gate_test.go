package agentdef

import (
	"context"
	"errors"
	"strings"
	"testing"
)

func TestQualityGate_PassesFirstAttempt(t *testing.T) {
	gate := &QualityGate{
		Name:       "test-gate",
		MaxRetries: 3,
		Review: func(_ context.Context, output string) (bool, string, error) {
			return true, "", nil // always pass
		},
	}

	output, result, err := gate.Apply(context.Background(), func(_ context.Context, feedback string) (string, error) {
		return "good output", nil
	})

	if err != nil {
		t.Fatal(err)
	}
	if !result.Passed {
		t.Error("expected pass")
	}
	if result.Attempts != 1 {
		t.Errorf("attempts = %d, want 1", result.Attempts)
	}
	if output != "good output" {
		t.Errorf("output = %q, want 'good output'", output)
	}
}

func TestQualityGate_PassesAfterRetries(t *testing.T) {
	attempts := 0
	gate := &QualityGate{
		Name:       "retry-gate",
		MaxRetries: 3,
		Review: func(_ context.Context, output string) (bool, string, error) {
			if strings.Contains(output, "v3") {
				return true, "", nil
			}
			return false, "needs improvement", nil
		},
	}

	output, result, err := gate.Apply(context.Background(), func(_ context.Context, feedback string) (string, error) {
		attempts++
		return "output v" + string(rune('0'+attempts)), nil
	})

	if err != nil {
		t.Fatal(err)
	}
	if !result.Passed {
		t.Error("expected pass after retries")
	}
	if result.Attempts != 3 {
		t.Errorf("attempts = %d, want 3", result.Attempts)
	}
	if output != "output v3" {
		t.Errorf("output = %q, want 'output v3'", output)
	}
}

func TestQualityGate_ExhaustsRetries(t *testing.T) {
	gate := &QualityGate{
		Name:       "exhaust-gate",
		MaxRetries: 2,
		Review: func(_ context.Context, _ string) (bool, string, error) {
			return false, "still bad", nil // always reject
		},
	}

	_, result, err := gate.Apply(context.Background(), func(_ context.Context, _ string) (string, error) {
		return "mediocre output", nil
	})

	if err != nil {
		t.Fatal(err)
	}
	if result.Passed {
		t.Error("should not pass after exhausting retries")
	}
	if result.Feedback != "still bad" {
		t.Errorf("feedback = %q, want 'still bad'", result.Feedback)
	}
}

func TestQualityGate_FeedbackPassedToExecutor(t *testing.T) {
	var receivedFeedback []string
	gate := &QualityGate{
		Name:       "feedback-gate",
		MaxRetries: 2,
		Review: func(_ context.Context, output string) (bool, string, error) {
			if output == "fixed" {
				return true, "", nil
			}
			return false, "please fix the issue", nil
		},
	}

	callCount := 0
	_, _, err := gate.Apply(context.Background(), func(_ context.Context, feedback string) (string, error) {
		receivedFeedback = append(receivedFeedback, feedback)
		callCount++
		if callCount >= 2 {
			return "fixed", nil
		}
		return "broken", nil
	})

	if err != nil {
		t.Fatal(err)
	}
	if len(receivedFeedback) < 2 {
		t.Fatalf("expected at least 2 calls, got %d", len(receivedFeedback))
	}
	if receivedFeedback[0] != "" {
		t.Errorf("first call should have empty feedback, got %q", receivedFeedback[0])
	}
	if receivedFeedback[1] != "please fix the issue" {
		t.Errorf("second call should have feedback, got %q", receivedFeedback[1])
	}
}

func TestQualityGate_ExecutorError(t *testing.T) {
	gate := &QualityGate{
		Name:       "error-gate",
		MaxRetries: 3,
		Review: func(_ context.Context, _ string) (bool, string, error) {
			return true, "", nil
		},
	}

	_, result, err := gate.Apply(context.Background(), func(_ context.Context, _ string) (string, error) {
		return "", errors.New("executor failed")
	})

	if err == nil {
		t.Fatal("expected error")
	}
	if result.Passed {
		t.Error("should not pass on executor error")
	}
	if result.Attempts != 1 {
		t.Errorf("attempts = %d, want 1", result.Attempts)
	}
}

func TestQualityGate_ReviewError(t *testing.T) {
	gate := &QualityGate{
		Name:       "review-error-gate",
		MaxRetries: 3,
		Review: func(_ context.Context, _ string) (bool, string, error) {
			return false, "", errors.New("review crashed")
		},
	}

	_, _, err := gate.Apply(context.Background(), func(_ context.Context, _ string) (string, error) {
		return "output", nil
	})

	if err == nil {
		t.Fatal("expected error from review")
	}
	if !strings.Contains(err.Error(), "review failed") {
		t.Errorf("expected review failure, got: %v", err)
	}
}

func TestQualityGate_DefaultMaxRetries(t *testing.T) {
	gate := &QualityGate{
		Review: func(_ context.Context, _ string) (bool, string, error) {
			return false, "nope", nil
		},
	}

	callCount := 0
	gate.Apply(context.Background(), func(_ context.Context, _ string) (string, error) {
		callCount++
		return "output", nil
	})

	// Default 3 retries: initial + 3 retries + 1 final = 5 executor calls
	if callCount < 4 {
		t.Errorf("expected at least 4 executor calls with default retries, got %d", callCount)
	}
}
