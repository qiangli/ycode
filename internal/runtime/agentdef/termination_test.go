package agentdef

import (
	"testing"
	"time"
)

func TestMaxTurns(t *testing.T) {
	c := MaxTurns(3)
	event := AgentEvent{Type: "turn_end"}

	// First two turns: no termination.
	if r := c.Check(event); r != nil {
		t.Errorf("turn 1: unexpected termination: %s", r.Reason)
	}
	if r := c.Check(event); r != nil {
		t.Errorf("turn 2: unexpected termination: %s", r.Reason)
	}

	// Third turn: terminate.
	r := c.Check(event)
	if r == nil {
		t.Fatal("turn 3: expected termination")
	}
	if r.Condition != "MaxTurns" {
		t.Errorf("condition = %q, want MaxTurns", r.Condition)
	}

	// Reset and verify.
	c.Reset()
	if r := c.Check(event); r != nil {
		t.Error("after reset: unexpected termination")
	}
}

func TestMaxTurns_IgnoresNonTurnEvents(t *testing.T) {
	c := MaxTurns(1)
	// Non-turn event should not count.
	if r := c.Check(AgentEvent{Type: "message", Text: "hello"}); r != nil {
		t.Error("non-turn event should not trigger termination")
	}
	// Turn event should trigger.
	if r := c.Check(AgentEvent{Type: "turn_end"}); r == nil {
		t.Error("turn event should trigger termination")
	}
}

func TestTextMatch(t *testing.T) {
	c := TextMatch(`(?i)task\s+complete`)

	if r := c.Check(AgentEvent{Text: "working on it"}); r != nil {
		t.Error("should not match")
	}
	r := c.Check(AgentEvent{Text: "The Task Complete signal was sent"})
	if r == nil {
		t.Fatal("expected match")
	}
	if r.Condition != "TextMatch" {
		t.Errorf("condition = %q, want TextMatch", r.Condition)
	}
}

func TestStopMessage(t *testing.T) {
	c := StopMessage()

	if r := c.Check(AgentEvent{Type: "message"}); r != nil {
		t.Error("should not match")
	}
	r := c.Check(AgentEvent{Type: "stop"})
	if r == nil {
		t.Fatal("expected termination on stop")
	}
	if r.Condition != "StopMessage" {
		t.Errorf("condition = %q, want StopMessage", r.Condition)
	}
}

func TestTimeout(t *testing.T) {
	c := Timeout(50 * time.Millisecond)

	start := time.Now()
	if r := c.Check(AgentEvent{Timestamp: start}); r != nil {
		t.Error("should not terminate immediately")
	}

	time.Sleep(60 * time.Millisecond)

	r := c.Check(AgentEvent{Timestamp: time.Now()})
	if r == nil {
		t.Fatal("expected timeout")
	}
	if r.Condition != "Timeout" {
		t.Errorf("condition = %q, want Timeout", r.Condition)
	}
}

func TestOr(t *testing.T) {
	c := Or(MaxTurns(5), TextMatch("done"))

	// Text match fires first.
	r := c.Check(AgentEvent{Text: "done"})
	if r == nil {
		t.Fatal("Or: expected termination from TextMatch")
	}
	if r.Condition != "TextMatch" {
		t.Errorf("condition = %q, want TextMatch", r.Condition)
	}
}

func TestAnd(t *testing.T) {
	c := And(MaxTurns(2), TextMatch("ready"))

	// Turn 1: MaxTurns not yet satisfied.
	c.Check(AgentEvent{Type: "turn_end"})

	// Turn 2: MaxTurns satisfied, but TextMatch not yet.
	r := c.Check(AgentEvent{Type: "turn_end"})
	if r != nil {
		t.Error("And: should not terminate — TextMatch not satisfied")
	}

	// TextMatch fires: now both are satisfied.
	r = c.Check(AgentEvent{Text: "ready"})
	if r == nil {
		t.Fatal("And: expected termination when both conditions satisfied")
	}
	if r.Condition != "And" {
		t.Errorf("condition = %q, want And", r.Condition)
	}
}

func TestAnd_Reset(t *testing.T) {
	c := And(MaxTurns(1), TextMatch("go"))

	c.Check(AgentEvent{Type: "turn_end"})
	c.Check(AgentEvent{Text: "go"})
	// Should be terminated now. Reset.
	c.Reset()

	// After reset, should not terminate on text alone.
	if r := c.Check(AgentEvent{Text: "go"}); r != nil {
		t.Error("after reset: should not terminate — MaxTurns not yet satisfied")
	}
}

func TestOr_Reset(t *testing.T) {
	c := Or(MaxTurns(1), StopMessage())
	c.Reset() // should not panic
}
