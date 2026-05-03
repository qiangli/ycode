package agentdef

import (
	"regexp"
	"time"
)

// AgentEvent represents an event in the agent execution that termination
// conditions can inspect. Generalizes across conversation turns, tool calls,
// and workflow phase transitions.
type AgentEvent struct {
	Type      string // "message", "tool_call", "tool_result", "turn_end"
	Text      string // message text or tool output
	ToolName  string // for tool events
	TurnCount int    // current turn number
	Timestamp time.Time
}

// TerminationResult describes why execution should stop.
type TerminationResult struct {
	Reason    string
	Condition string // name of the condition that triggered
}

// TerminationCondition checks whether execution should stop.
// Inspired by AutoGen's composable termination algebra with AND/OR operators.
type TerminationCondition interface {
	// Check inspects an event and returns a result if termination should occur.
	// Returns nil if execution should continue.
	Check(event AgentEvent) *TerminationResult

	// Reset clears any internal state (turn counters, etc.).
	// Must be called between workflow runs.
	Reset()

	// Name returns a human-readable name for this condition.
	Name() string
}

// MaxTurns terminates after N turns.
func MaxTurns(n int) TerminationCondition {
	return &maxTurnsCondition{max: n}
}

type maxTurnsCondition struct {
	max  int
	seen int
}

func (c *maxTurnsCondition) Check(event AgentEvent) *TerminationResult {
	if event.Type == "turn_end" {
		c.seen++
		if c.seen >= c.max {
			return &TerminationResult{
				Reason:    "maximum turns reached",
				Condition: c.Name(),
			}
		}
	}
	return nil
}

func (c *maxTurnsCondition) Reset()       { c.seen = 0 }
func (c *maxTurnsCondition) Name() string { return "MaxTurns" }

// TextMatch terminates when a message contains the given pattern.
func TextMatch(pattern string) TerminationCondition {
	re := regexp.MustCompile(pattern)
	return &textMatchCondition{pattern: re}
}

type textMatchCondition struct {
	pattern *regexp.Regexp
}

func (c *textMatchCondition) Check(event AgentEvent) *TerminationResult {
	if event.Text != "" && c.pattern.MatchString(event.Text) {
		return &TerminationResult{
			Reason:    "text pattern matched: " + c.pattern.String(),
			Condition: c.Name(),
		}
	}
	return nil
}

func (c *textMatchCondition) Reset()       {}
func (c *textMatchCondition) Name() string { return "TextMatch" }

// StopMessage terminates when a message of type "stop" is received.
func StopMessage() TerminationCondition {
	return &stopMessageCondition{}
}

type stopMessageCondition struct{}

func (c *stopMessageCondition) Check(event AgentEvent) *TerminationResult {
	if event.Type == "stop" {
		return &TerminationResult{
			Reason:    "stop message received",
			Condition: c.Name(),
		}
	}
	return nil
}

func (c *stopMessageCondition) Reset()       {}
func (c *stopMessageCondition) Name() string { return "StopMessage" }

// Timeout terminates after a duration has elapsed since the first event.
func Timeout(d time.Duration) TerminationCondition {
	return &timeoutCondition{timeout: d}
}

type timeoutCondition struct {
	timeout time.Duration
	start   time.Time
	started bool
}

func (c *timeoutCondition) Check(event AgentEvent) *TerminationResult {
	if !c.started {
		c.start = event.Timestamp
		if c.start.IsZero() {
			c.start = time.Now()
		}
		c.started = true
	}
	if time.Since(c.start) >= c.timeout {
		return &TerminationResult{
			Reason:    "timeout exceeded",
			Condition: c.Name(),
		}
	}
	return nil
}

func (c *timeoutCondition) Reset()       { c.started = false }
func (c *timeoutCondition) Name() string { return "Timeout" }

// And returns a condition that triggers only when ALL sub-conditions trigger.
// Each sub-condition is checked independently; termination occurs when the
// last unsatisfied condition fires.
func And(conditions ...TerminationCondition) TerminationCondition {
	return &andCondition{conditions: conditions, satisfied: make([]bool, len(conditions))}
}

type andCondition struct {
	conditions []TerminationCondition
	satisfied  []bool
}

func (c *andCondition) Check(event AgentEvent) *TerminationResult {
	for i, cond := range c.conditions {
		if !c.satisfied[i] {
			if result := cond.Check(event); result != nil {
				c.satisfied[i] = true
			}
		}
	}
	// Check if all are now satisfied.
	for _, s := range c.satisfied {
		if !s {
			return nil
		}
	}
	return &TerminationResult{
		Reason:    "all conditions satisfied",
		Condition: c.Name(),
	}
}

func (c *andCondition) Reset() {
	for i, cond := range c.conditions {
		cond.Reset()
		c.satisfied[i] = false
	}
}

func (c *andCondition) Name() string { return "And" }

// Or returns a condition that triggers when ANY sub-condition triggers.
func Or(conditions ...TerminationCondition) TerminationCondition {
	return &orCondition{conditions: conditions}
}

type orCondition struct {
	conditions []TerminationCondition
}

func (c *orCondition) Check(event AgentEvent) *TerminationResult {
	for _, cond := range c.conditions {
		if result := cond.Check(event); result != nil {
			return result
		}
	}
	return nil
}

func (c *orCondition) Reset() {
	for _, cond := range c.conditions {
		cond.Reset()
	}
}

func (c *orCondition) Name() string { return "Or" }
