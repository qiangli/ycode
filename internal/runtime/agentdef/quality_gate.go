package agentdef

import (
	"context"
	"fmt"
	"log/slog"
)

// QualityGate implements a review/fix feedback loop for workflow nodes.
// When attached to a DAG node or flow step, the gate reviews the output
// and can reject it with feedback, triggering a bounded retry cycle.
//
// Pattern: Execute → Review → (Accept | Reject+Feedback → Re-execute → ...)
//
// Inspired by MetaGPT's Engineer→CodeReview→FixBug pattern where code
// output is reviewed and iteratively improved until it passes quality checks.
type QualityGate struct {
	// Review inspects the output and returns whether it passes.
	// If rejected, feedback describes what needs to change.
	Review func(ctx context.Context, output string) (passed bool, feedback string, err error)

	// MaxRetries is the maximum number of fix attempts (default 3).
	// After exhausting retries, the last output is accepted with a warning.
	MaxRetries int

	// Name identifies this gate for logging and diagnostics.
	Name string
}

// DefaultMaxRetries is the default number of fix attempts for a quality gate.
const DefaultMaxRetries = 3

// QualityGateResult records the outcome of a quality gate evaluation.
type QualityGateResult struct {
	Passed     bool
	Attempts   int
	LastOutput string
	Feedback   string // feedback from the last rejection, empty if passed
}

// Apply runs the quality gate loop: execute → review → (fix → review)*.
// The executor function is called with an optional feedback string from the
// previous rejection (empty on first call). Returns the final output and
// the gate result.
func (g *QualityGate) Apply(
	ctx context.Context,
	executor func(ctx context.Context, feedback string) (output string, err error),
) (string, *QualityGateResult, error) {
	maxRetries := g.MaxRetries
	if maxRetries <= 0 {
		maxRetries = DefaultMaxRetries
	}
	gateName := g.Name
	if gateName == "" {
		gateName = "quality_gate"
	}

	var feedback string
	for attempt := 0; attempt <= maxRetries; attempt++ {
		// Execute (or re-execute with feedback).
		output, err := executor(ctx, feedback)
		if err != nil {
			return "", &QualityGateResult{
				Passed:   false,
				Attempts: attempt + 1,
				Feedback: feedback,
			}, fmt.Errorf("%s: executor failed on attempt %d: %w", gateName, attempt+1, err)
		}

		// Review.
		passed, reviewFeedback, err := g.Review(ctx, output)
		if err != nil {
			return output, &QualityGateResult{
				Passed:     false,
				Attempts:   attempt + 1,
				LastOutput: output,
				Feedback:   reviewFeedback,
			}, fmt.Errorf("%s: review failed on attempt %d: %w", gateName, attempt+1, err)
		}

		if passed {
			slog.Info("quality gate passed",
				"gate", gateName,
				"attempts", attempt+1,
			)
			return output, &QualityGateResult{
				Passed:     true,
				Attempts:   attempt + 1,
				LastOutput: output,
			}, nil
		}

		// Rejected — prepare feedback for next attempt.
		feedback = reviewFeedback
		slog.Info("quality gate rejected",
			"gate", gateName,
			"attempt", attempt+1,
			"feedback_len", len(feedback),
		)
	}

	// Exhausted retries — accept last output with warning.
	lastOutput, _ := executor(ctx, feedback)
	slog.Warn("quality gate: max retries exhausted, accepting last output",
		"gate", gateName,
		"max_retries", maxRetries,
	)
	return lastOutput, &QualityGateResult{
		Passed:     false,
		Attempts:   maxRetries + 2, // initial + retries + final
		LastOutput: lastOutput,
		Feedback:   feedback,
	}, nil
}
