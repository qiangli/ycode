//go:build integration

package cli

import (
	"bytes"
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/x/exp/teatest"
)

// steerMarker is the supplementary text typed while the agent is streaming.
// It must be unique enough that it won't collide with ycode's own output or
// the mock LLM's scripted response.
const steerMarker = "ycode-steer-integration-please"

// TestPTY_MidTurnSteer is a regression test for Gate 5: ycode running under a
// PTY must accept supplementary input ("steer" text) while a turn is actively
// streaming, and must process it rather than dropping it on the floor.
//
// The test drives the ycode TUI inside a real pseudo-terminal via teatest,
// simulates an in-flight streaming LLM response, injects steer text while the
// TUI is in the WORKING state, and verifies that the TUI acknowledges the
// steer and forwards it to the mid-turn channel.
func TestPTY_MidTurnSteer(t *testing.T) {
	app := newTestApp(t)
	m := NewTUIModel(app)

	// Pre-seed the working state that an active agent turn would create.
	// This lets us exercise the "type while streaming" path without needing a
	// live LLM provider in the test harness.
	m.working = true
	m.workCancel = func() {}
	m.midTurnCh = make(chan string, 8)

	tm := teatest.NewTestModel(t, m,
		teatest.WithInitialTermSize(80, 24),
	)

	// Wait for the TUI to initialize.
	teatest.WaitFor(t, tm.Output(), func(bts []byte) bool {
		return bytes.Contains(bts, []byte("ycode"))
	}, teatest.WithDuration(3*time.Second))

	// Simulate streaming output from the LLM so the viewport shows mid-turn
	// text (the condition under which the regression matters).
	tm.Send(streamDeltaMsg{EventType: "text.delta", Text: "I will run a command for you. "})
	teatest.WaitFor(t, tm.Output(), func(bts []byte) bool {
		return bytes.Contains(bts, []byte("I will run"))
	}, teatest.WithDuration(3*time.Second))

	// Inject steer text while the agent is still working.
	tm.Type(steerMarker)
	tm.Send(tea.KeyMsg{Type: tea.KeyEnter})

	// The TUI must acknowledge that the steer was received.
	teatest.WaitFor(t, tm.Output(), func(bts []byte) bool {
		return bytes.Contains(bts, []byte("Noted"))
	}, teatest.WithDuration(3*time.Second))

	// The steer must have been forwarded to the mid-turn channel.
	select {
	case text := <-m.midTurnCh:
		if text != steerMarker {
			t.Errorf("midTurnCh got %q, want %q", text, steerMarker)
		}
	case <-time.After(time.Second):
		t.Fatal("timeout waiting for steer text on midTurnCh")
	}

	// Quit cleanly.
	tm.Send(tea.KeyMsg{Type: tea.KeyCtrlD})
	tm.WaitFinished(t, teatest.WithFinalTimeout(3*time.Second))
}
