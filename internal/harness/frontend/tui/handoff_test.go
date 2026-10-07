package tui

import (
	"context"
	"io"
	"testing"
)

type handoffCommand struct{ calls int }

func (c *handoffCommand) Run() error        { c.calls++; return nil }
func (*handoffCommand) SetStdin(io.Reader)  {}
func (*handoffCommand) SetStdout(io.Writer) {}
func (*handoffCommand) SetStderr(io.Writer) {}

type handoffShell struct{ command *handoffCommand }

func (*handoffShell) Known(name string) bool       { return name == "echo" }
func (*handoffShell) Names() []string              { return []string{"echo"} }
func (s *handoffShell) Command(string) ExecCommand { return s.command }

// Terminal ownership moves outside Program.Run, while queued input and the
// session's host binding survive the next Program's Init unchanged.
func TestLiteralHandoffPreservesBindingAndTypeAhead(t *testing.T) {
	host := &raceHost{}
	command := &handoffCommand{}
	m := newModel(context.Background(), Options{Session: "bound-session", Host: host, Shell: &handoffShell{command}})
	m.status = Status{Model: "chosen-model", Mode: "plan"}
	m.epoch = 7
	m.history = []string{"earlier input"}
	m.submit("echo literal")
	if m.handoff != command || command.calls != 0 || m.busy != "command" {
		t.Fatalf("literal ran before terminal release: handoff=%v calls=%d busy=%q", m.handoff, command.calls, m.busy)
	}
	m.submit("next request")
	if len(host.turns) != 0 || len(m.pending) != 1 {
		t.Fatalf("type-ahead escaped handoff: turns=%v pending=%v", host.turns, m.pending)
	}
	// Run has finished the previous Program and executed the command; the
	// next Init resumes the same model rather than replaying its transcript.
	m.handoff = nil
	m.returning = true
	m.Init()
	if m.returning || m.busy != "" || len(m.pending) != 0 {
		t.Fatalf("handoff did not settle: returning=%v busy=%q pending=%v", m.returning, m.busy, m.pending)
	}
	if m.session != "bound-session" || m.opts.Host != host || m.status.Model != "chosen-model" || m.status.Mode != "plan" || m.epoch != 7 || len(m.history) != 1 {
		t.Fatalf("handoff changed the session binding or history: %+v", m)
	}
	if len(host.turns) != 1 || host.turns[0] != "bound-session|next request" || !m.running {
		t.Fatalf("queued input did not retain its binding: turns=%v running=%v", host.turns, m.running)
	}
	m.cancel()
}
