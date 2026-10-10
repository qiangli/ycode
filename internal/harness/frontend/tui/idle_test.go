package tui

import (
	"context"
	"testing"
	"time"

	"charm.land/bubbles/v2/textinput"
)

// A finished turn leaves the focused prompt visible. Simulated cursor blink
// intervals must not change it or schedule another update while it is idle.
func TestIdlePromptDoesNotBlink(t *testing.T) {
	m := newModel(context.Background(), Options{Session: "s1", Host: &raceHost{}})
	m.running, m.started = true, time.Now()
	m.Update(turnDoneMsg{gen: m.gen})
	if m.running {
		t.Fatal("turn completion left the prompt running")
	}
	want := m.View().Content
	for i := 0; i < 3; i++ {
		_, cmd := m.Update(textinput.Blink())
		if cmd != nil {
			t.Fatalf("idle blink interval %d scheduled another update", i+1)
		}
		if got := m.View().Content; got != want {
			t.Fatalf("idle blink interval %d changed the prompt: %q != %q", i+1, got, want)
		}
	}
}
