package conversation

// ConvergenceAction is the policy's verdict after observing a completed turn
// that requested tool calls.
type ConvergenceAction int

const (
	// ConvergeContinue — within budget, or the turn made measurable progress.
	ConvergeContinue ConvergenceAction = iota
	// ConvergeGrace — the exploration budget is exhausted. The caller must
	// inject one final-answer instruction (GraceMessage) and give the model
	// exactly one more turn to answer in text.
	ConvergeGrace
	// ConvergeStop — tool use continued after the grace instruction. The
	// caller must terminate the run as NOT successful.
	ConvergeStop
)

// ConvergencePolicy is the progress-aware grace-then-hard-stop contract for
// headless agent loops.
//
// The flat iteration ceiling (config.MaxToolIterations, default 100) is a
// runaway backstop, not a convergence policy: a model that reads and searches
// with VARIED inputs every turn — never repeating an exact call signature, so
// the loop detectors stay silent, and never writing, testing, or answering —
// walks all the way to that ceiling. This policy bounds something different:
// CONSECUTIVE turns without measurable progress.
//
//   - A turn that only explores (reads/searches) consumes one unit of budget.
//   - A turn that makes progress (a write, an edit, a test/build run — as
//     judged by the caller, who knows its tools) RESETS the budget, so a long
//     productive task is never cut short by this policy.
//   - When the budget runs dry, the model gets ONE grace turn: a final-answer
//     instruction. Answering in text ends the run successfully.
//   - If tool use continues past the grace turn, the run is stopped and
//     reported as not successful.
//
// It reuses IterationBudget for the grace bookkeeping so the grace contract
// here and in the service loop cannot drift apart.
type ConvergencePolicy struct {
	budget *IterationBudget
}

// NewConvergencePolicy creates a policy allowing explorationTurns consecutive
// exploration-only turns; the next exploration-only turn after that triggers
// the grace instruction. explorationTurns <= 0 is clamped to 1.
func NewConvergencePolicy(explorationTurns int) *ConvergencePolicy {
	return &ConvergencePolicy{budget: NewIterationBudget(explorationTurns)}
}

// ObserveToolTurn records one completed turn that REQUESTED TOOL CALLS
// (turns with no tool calls end the run and never reach the policy) and
// returns the action the caller must take. progress reports whether any of
// the turn's tool calls can advance task state (write/edit/execute) rather
// than merely explore it. Nil-safe: a nil policy always continues.
func (p *ConvergencePolicy) ObserveToolTurn(progress bool) ConvergenceAction {
	if p == nil {
		return ConvergeContinue
	}
	// The grace instruction was already issued, and the model called tools
	// again instead of answering. The contract is one grace turn, not two.
	if p.budget.IsGrace() {
		return ConvergeStop
	}
	if progress {
		p.budget.Used = 0
		return ConvergeContinue
	}
	p.budget.Consume()
	if p.budget.IsGrace() {
		return ConvergeGrace
	}
	return ConvergeContinue
}

// NoProgressTurns returns the current count of consecutive exploration-only
// turns (including the one that triggered grace, once it has).
func (p *ConvergencePolicy) NoProgressTurns() int {
	if p == nil {
		return 0
	}
	return p.budget.Used
}

// Budget returns the configured number of exploration-only turns allowed
// before the grace instruction.
func (p *ConvergencePolicy) Budget() int {
	if p == nil {
		return 0
	}
	return p.budget.Total
}

// GraceMessage is the final-answer instruction injected on the grace turn.
func (p *ConvergencePolicy) GraceMessage() string {
	return "You have spent many consecutive turns reading and searching without " +
		"a write, an edit, a test run, or a final answer. This is your FINAL turn: " +
		"deliver your best answer now as plain text, based on what you have already " +
		"learned. State plainly anything you could not determine. Do not call any " +
		"more tools — further tool use will end this run unsuccessfully."
}
