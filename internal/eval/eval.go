// Package eval provides an evaluation framework for measuring and tracking
// ycode's agentic capabilities across releases.
//
// The framework uses a 4-tier evaluation pyramid:
//   - Contract: no LLM, deterministic tests of agent machinery
//   - Smoke: real LLM, fast pass@k scenarios
//   - Behavioral: multi-step trajectory analysis
//   - E2E: full coding tasks in sandboxed workspaces
package eval

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"github.com/qiangli/ycode/internal/runtime/permission"
)

// Tier classifies the complexity and resource requirements of a scenario.
type Tier int

const (
	TierContract   Tier = iota // No LLM, deterministic, always runs
	TierSmoke                  // Real LLM, fast, pass@k
	TierBehavioral             // Multi-step trajectory analysis
	TierE2E                    // Full coding tasks, sandboxed
)

func (t Tier) String() string {
	switch t {
	case TierContract:
		return "contract"
	case TierSmoke:
		return "smoke"
	case TierBehavioral:
		return "behavioral"
	case TierE2E:
		return "e2e"
	default:
		return fmt.Sprintf("tier(%d)", int(t))
	}
}

// Policy controls how strictly a scenario is validated.
type Policy int

const (
	// AlwaysPasses must pass every trial. Used as a build gate.
	// Failure blocks the release.
	AlwaysPasses Policy = iota

	// UsuallyPasses must pass at least PassThreshold-of-Trials.
	// Used for scheduled nightly runs. Failure triggers an alert.
	UsuallyPasses
)

func (p Policy) String() string {
	switch p {
	case AlwaysPasses:
		return "always_passes"
	case UsuallyPasses:
		return "usually_passes"
	default:
		return fmt.Sprintf("policy(%d)", int(p))
	}
}

// ToolCall records a single tool invocation during an eval run.
type ToolCall struct {
	Name     string          `json:"name"`
	Input    json.RawMessage `json:"input"`
	Output   string          `json:"output"`
	Error    string          `json:"error,omitempty"`
	Duration time.Duration   `json:"duration_ms"`
}

// RunResult captures everything produced by a single trial execution.
type RunResult struct {
	Response     string        // Final text response from the agent
	ToolCalls    []ToolCall    // Ordered list of tool invocations
	Turns        int           // Number of conversation turns (LLM calls)
	InputTokens  int           // Total input tokens consumed
	OutputTokens int           // Total output tokens consumed
	Duration     time.Duration // Wall-clock time from prompt to completion
	Error        error         // Non-nil if the agent errored out
	WorkDir      string        // Temp workspace root for this trial
}

// TotalTokens returns the sum of input and output tokens.
func (r *RunResult) TotalTokens() int {
	return r.InputTokens + r.OutputTokens
}

// ToolNames returns the ordered list of tool names invoked.
func (r *RunResult) ToolNames() []string {
	names := make([]string, len(r.ToolCalls))
	for i, tc := range r.ToolCalls {
		names[i] = tc.Name
	}
	return names
}

// Scenario defines a single evaluation scenario.
type Scenario struct {
	Name        string // Human-readable name
	Description string // What this scenario tests
	Tier        Tier
	Policy      Policy

	// Providers restricts which providers this scenario can run against.
	// Empty means all providers.
	Providers []string

	// Prompt is the user message sent to the agent.
	Prompt string

	// PermissionMode sets the agent's permission mode for this scenario.
	// Zero value defaults to WorkspaceWrite.
	PermissionMode permission.Mode

	// Setup prepares the workspace before the agent runs.
	// It receives the temp directory path and returns a cleanup function.
	// Nil means no setup required.
	Setup func(workDir string) (cleanup func(), err error)

	// Assertions are checked against the RunResult after each trial.
	Assertions []Assertion

	// TrajectoryAssertions check the sequence of tool calls (Tier 3+).
	TrajectoryAssertions []TrajectoryAssertion

	// MaxTurns caps the agentic loop to prevent runaway. 0 means default (20).
	MaxTurns int

	// Timeout per trial. 0 means default (60s for smoke, 300s for behavioral).
	Timeout time.Duration

	// Trials is the number of times to run for pass@k. 0 means default:
	// 1 for contract, 3 for smoke/behavioral/e2e.
	Trials int

	// PassThreshold is the minimum pass count out of Trials for UsuallyPasses.
	// 0 means default (2 of 3 for UsuallyPasses, all for AlwaysPasses).
	PassThreshold int
}

// EffectiveTrials returns the number of trials, applying defaults.
func (s *Scenario) EffectiveTrials() int {
	if s.Trials > 0 {
		return s.Trials
	}
	if s.Tier == TierContract {
		return 1
	}
	return 3
}

// EffectivePassThreshold returns the minimum pass count, applying defaults.
func (s *Scenario) EffectivePassThreshold() int {
	if s.PassThreshold > 0 {
		return s.PassThreshold
	}
	if s.Policy == AlwaysPasses {
		return s.EffectiveTrials()
	}
	// UsuallyPasses: majority (2 of 3)
	return (s.EffectiveTrials() + 1) / 2
}

// EffectiveTimeout returns the timeout, applying tier-based defaults.
func (s *Scenario) EffectiveTimeout() time.Duration {
	if s.Timeout > 0 {
		return s.Timeout
	}
	switch s.Tier {
	case TierContract:
		return 10 * time.Second
	case TierSmoke:
		return 60 * time.Second
	case TierBehavioral:
		return 5 * time.Minute
	case TierE2E:
		return 10 * time.Minute
	default:
		return 60 * time.Second
	}
}

// EffectiveMaxTurns returns the max turns, applying defaults.
func (s *Scenario) EffectiveMaxTurns() int {
	if s.MaxTurns > 0 {
		return s.MaxTurns
	}
	return 20
}

// Assertion checks a single aspect of a RunResult.
type Assertion interface {
	// Check returns nil if the assertion passes, or an error describing the failure.
	Check(result *RunResult) error
	// String returns a human-readable description of the assertion.
	String() string
}

// TrajectoryAssertion checks the sequence of tool calls.
type TrajectoryAssertion interface {
	// Check returns a score in [0.0, 1.0] and an optional error.
	// A score of 1.0 means the trajectory perfectly matches expectations.
	Check(toolCalls []ToolCall) (score float64, err error)
	// String returns a human-readable description.
	String() string
}

// TrialResult captures the outcome of a single trial.
type TrialResult struct {
	Trial     int           `json:"trial"`
	Passed    bool          `json:"passed"`
	RunResult *RunResult    `json:"-"`
	Duration  time.Duration `json:"duration_ms"`
	Error     string        `json:"error,omitempty"`

	// Numerical metrics for this trial.
	EditPrecision   float64 `json:"edit_precision,omitempty"`
	ToolAccuracy    float64 `json:"tool_accuracy,omitempty"`
	TrajectoryScore float64 `json:"trajectory_score,omitempty"`
}

// ScenarioResult aggregates trial results for a scenario.
type ScenarioResult struct {
	Scenario string          `json:"scenario"`
	Tier     string          `json:"tier"`
	Policy   string          `json:"policy"`
	Trials   []TrialResult   `json:"trials"`
	Metrics  ScenarioMetrics `json:"metrics"`
}

// ScenarioMetrics holds the computed metrics for a scenario.
type ScenarioMetrics struct {
	PassAtK         float64 `json:"pass_at_k"`
	PassPowK        float64 `json:"pass_pow_k"`
	Flakiness       float64 `json:"flakiness"`
	EditPrecision   float64 `json:"edit_precision,omitempty"`
	ToolAccuracy    float64 `json:"tool_accuracy,omitempty"`
	TrajectoryScore float64 `json:"trajectory_score,omitempty"`
	MeanLatencyMS   int64   `json:"mean_latency_ms"`
	TotalTokens     int     `json:"total_tokens"`
}

// RunConfig configures an eval run.
type RunConfig struct {
	Provider string // "ollama", "anthropic", "openai"
	Model    string // model identifier
	Version  string // git SHA or version string
}

// EvalRun aggregates results across all scenarios in a single run.
type EvalRun struct {
	ID        string           `json:"id"`
	Config    RunConfig        `json:"config"`
	Timestamp time.Time        `json:"timestamp"`
	Tier      string           `json:"tier"`
	Results   []ScenarioResult `json:"results"`
	Composite float64          `json:"composite_score"`
}

// Runner executes eval scenarios.
type Runner struct {
	config  RunConfig
	execute func(ctx context.Context, scenario *Scenario) (*RunResult, error)
}

// NewRunner creates a runner with the given execution function.
// The execute function is called once per trial and should run the agent
// against the scenario's prompt in a fresh workspace.
func NewRunner(cfg RunConfig, execute func(ctx context.Context, scenario *Scenario) (*RunResult, error)) *Runner {
	return &Runner{config: cfg, execute: execute}
}

// Run executes a single scenario for all its trials and returns the aggregated result.
func (r *Runner) Run(ctx context.Context, s *Scenario) (*ScenarioResult, error) {
	trials := s.EffectiveTrials()
	result := &ScenarioResult{
		Scenario: s.Name,
		Tier:     s.Tier.String(),
		Policy:   s.Policy.String(),
		Trials:   make([]TrialResult, 0, trials),
	}

	for i := range trials {
		trialCtx, cancel := context.WithTimeout(ctx, s.EffectiveTimeout())

		rr, err := r.execute(trialCtx, s)
		cancel()

		tr := TrialResult{
			Trial:    i + 1,
			Duration: rr.Duration,
		}

		if err != nil {
			tr.Error = err.Error()
			result.Trials = append(result.Trials, tr)
			continue
		}

		tr.RunResult = rr

		// Check assertions.
		passed := true
		for _, a := range s.Assertions {
			if checkErr := a.Check(rr); checkErr != nil {
				passed = false
				tr.Error = checkErr.Error()
				break
			}
		}

		// Check trajectory assertions and collect scores.
		if len(s.TrajectoryAssertions) > 0 {
			var totalScore float64
			for _, ta := range s.TrajectoryAssertions {
				score, taErr := ta.Check(rr.ToolCalls)
				if taErr != nil {
					passed = false
					tr.Error = taErr.Error()
					break
				}
				totalScore += score
			}
			tr.TrajectoryScore = totalScore / float64(len(s.TrajectoryAssertions))
		}

		tr.Passed = passed
		result.Trials = append(result.Trials, tr)
	}

	// Compute aggregate metrics.
	n := len(result.Trials)
	c := 0
	for _, t := range result.Trials {
		if t.Passed {
			c++
		}
	}
	k := s.EffectiveTrials()

	result.Metrics = ScenarioMetrics{
		PassAtK:   PassAtK(n, c, k),
		PassPowK:  PassPowK(n, c, k),
		Flakiness: Flakiness(float64(c) / float64(n)),
	}

	// Average trajectory scores.
	var trajSum float64
	var trajCount int
	for _, t := range result.Trials {
		if t.TrajectoryScore > 0 {
			trajSum += t.TrajectoryScore
			trajCount++
		}
	}
	if trajCount > 0 {
		result.Metrics.TrajectoryScore = trajSum / float64(trajCount)
	}

	return result, nil
}
