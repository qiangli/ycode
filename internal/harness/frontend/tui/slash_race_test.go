package tui

// Sprint: #387; Story: #27b3dfdf; Story-ID: 27b3dfdf7e76

import (
	"context"
	"testing"

	"github.com/qiangli/ycode/internal/harness/event"
)

type raceHost struct {
	turns  []string // session|text
	steers []string // session|text
}

func (h *raceHost) Turn(_ context.Context, session, text string) (<-chan event.Event, error) {
	h.turns = append(h.turns, session+"|"+text)
	return make(chan event.Event), nil
}
func (h *raceHost) Steer(session, text string) error {
	h.steers = append(h.steers, session+"|"+text)
	return nil
}
func (*raceHost) TakeQueued(string) ([]string, error) { return nil, nil }
func (*raceHost) Settle(context.Context, string) bool { return true }
func (*raceHost) Payload(string) ([]byte, error)      { return nil, nil }
func (*raceHost) Status(string) Status                { return Status{} }
func (*raceHost) Transcript(string) ([]Entry, error)  { return nil, nil }
func (*raceHost) Decide(context.Context, string, Waiting, string) (<-chan event.Event, error) {
	return nil, nil
}
func (*raceHost) Slash(context.Context, string, Slash, []string) (SlashResult, error) {
	return SlashResult{}, nil
}

// planResult is a /plan TEXT result whose turn records the session it was
// bound to when the slash ran.
func planResult(bound string, started *[]string) SlashResult {
	return SlashResult{Turn: "plan it", Start: func(context.Context) (<-chan event.Event, error) {
		*started = append(*started, bound)
		return make(chan event.Event), nil
	}}
}

// A line typed while /plan TEXT is in flight waits for it: it neither starts
// its own turn (which discarded the planning turn) nor runs before it; once
// the planning turn starts, the waiting line steers it.
func TestTypeAheadWaitsForSlashTurn(t *testing.T) {
	host := &raceHost{}
	m := newModel(context.Background(), Options{Session: "s1", Host: host})
	m.submit("/plan plan it")
	if m.busy != "/plan" {
		t.Fatalf("busy = %q", m.busy)
	}
	m.submit("next thing")
	if len(host.turns) != 0 || m.running {
		t.Fatalf("a line typed during /plan started a turn: %v", host.turns)
	}
	var started []string
	m.Update(slashDoneMsg{session: "s1", result: planResult("s1", &started)})
	if len(started) != 1 || !m.running {
		t.Fatalf("the planning turn did not start: started=%v running=%v", started, m.running)
	}
	if len(host.turns) != 0 || len(host.steers) != 1 || host.steers[0] != "s1|next thing" || len(m.pending) != 0 {
		t.Fatalf("held line: turns=%v steers=%v pending=%v", host.turns, host.steers, m.pending)
	}
}

// A slash result never acts on a session the terminal has left: its turn
// stays bound to the session it ran on and is not started on another.
func TestSlashTurnBoundToItsSession(t *testing.T) {
	host := &raceHost{}
	m := newModel(context.Background(), Options{Session: "s1", Host: host})
	m.submit("/resume s2")
	m.submit("/plan plan it")
	if len(m.pending) != 1 {
		t.Fatalf("pending = %v", m.pending)
	}
	m.Update(slashDoneMsg{session: "s1", result: SlashResult{Session: "s2"}})
	if m.session != "s2" || m.busy != "/plan" || len(m.pending) != 0 {
		t.Fatalf("after switch: session=%q busy=%q pending=%v", m.session, m.busy, m.pending)
	}
	var started []string
	m.Update(slashDoneMsg{session: "s1", result: planResult("s1", &started)})
	if len(started) != 0 || m.running {
		t.Fatalf("a result issued on s1 started on %s: %v", m.session, started)
	}
	m.Update(slashDoneMsg{session: "s2", result: planResult("s2", &started)})
	if len(started) != 1 || started[0] != "s2" || !m.running {
		t.Fatalf("the slash on s2 did not start its turn: %v", started)
	}
}
