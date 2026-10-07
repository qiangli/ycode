package tui

// Sprint: #387; Story: #1625; Story-ID: 27b3dfdf7e76

import (
	"context"
	"testing"

	"github.com/qiangli/ycode/internal/harness/event"
)

// A cancelled turn's partial streamed line, its pending approval and a
// status read issued before /clear never reach the next conversation: the
// session change resets them and fences the late results.
func TestClearResetsTransientTurnState(t *testing.T) {
	host := &raceHost{}
	m := newModel(context.Background(), Options{Session: "old", Host: host})
	m.startTurn("stream something")
	gen := m.gen
	// A partial delta (no newline yet) sits in the live buffer when the
	// turn is cancelled; the turn has also printed a streamed line.
	m.live.WriteString("PARTIAL-OLD-DELTA")
	m.streamed = "old answer line\n"
	m.waiting = &Waiting{RunID: "r1"}
	m.running = false
	staleStatus := m.refreshStatus()

	m.Update(slashDoneMsg{session: "old", result: SlashResult{Session: "new", Fresh: true, Clear: true}})
	if m.session != "new" {
		t.Fatalf("session = %q", m.session)
	}
	if m.live.Len() != 0 || m.streamed != "" || m.waiting != nil || m.activity != "" {
		t.Fatalf("transient state survived /clear: live=%q streamed=%q waiting=%v activity=%q", m.live.String(), m.streamed, m.waiting, m.activity)
	}
	// A late event or turn end of the cancelled turn is dropped.
	if m.gen == gen {
		t.Fatal("the turn generation was not fenced")
	}
	m.Update(eventMsg{gen: gen, ok: true, main: true, ev: event.Event{Sequence: 9, Type: "llm.delta"}})
	if m.live.Len() != 0 {
		t.Fatalf("a late delta landed: %q", m.live.String())
	}
	// A status read issued on the old session is dropped.
	m.status = Status{Model: "current"}
	m.Update(staleStatus())
	if m.status.Model != "current" {
		t.Fatalf("a stale status read replaced the new one: %+v", m.status)
	}

	// A new turn never starts with a cancelled turn's partial text.
	m.live.WriteString("PARTIAL-CANCELLED")
	m.streamed = "x\n"
	m.startTurn("next")
	if m.live.Len() != 0 || m.streamed != "" {
		t.Fatalf("new turn inherited partial output: live=%q streamed=%q", m.live.String(), m.streamed)
	}
}
