package ycodecli

// Sprint: #323; Story: #1115; Story-ID: a7f947d09851
//
// MID-TURN INPUT: a turn submitted from bashy's terminal front end owns the
// terminal while it runs.
//
// The terminal front end (bashy's agent TUI) is a line shell: every free-text
// line becomes one `ycode -- TEXT` turn, a foreground child. Before this file
// nothing read the terminal during that turn, so a steer typed mid-turn sat in
// the tty until the turn ended and ESC did nothing at all (it glued itself
// onto the next line instead). Commercial agent CLIs deliver a mid-turn line to
// the running turn and stop the turn on ESC; so does this, with the policy
// left where ycode keeps it — in YAML:
//
//   - a complete line typed during the turn is enqueued on the session's
//     compiled queue as class `steering`; the compiled loop drains that class
//     before every model call and after every tool batch, so the model reads
//     it at its next step (a STOP halts work before the next tool call);
//   - a line no turn drained (the turn ended first) is taken back when the
//     turn ends and submitted as the session's next turn — a follow-up;
//   - ESC cancels the turn: nothing after the in-flight step runs, the run
//     records its end, and the session continues at the shell prompt;
//   - a bare Enter is nothing; Ctrl-C keeps its meaning (SIGINT cancels the
//     turn, never the session).
//
// Nothing is stolen: the terminal stays in canonical mode (kernel echo and line
// editing), so a partly typed line is never read by the turn and is still in
// the tty for the shell's line editor when the turn ends. See
// turninput_unix.go for the terminal side.

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"strings"
	"time"

	harnesscli "github.com/qiangli/ycode/internal/harness/cli"
	public "github.com/qiangli/ycode/pkg/ycode"
)

// keyEvent is one thing typed during a turn: a complete line, or ESC.
type keyEvent struct {
	line      string
	interrupt bool
}

// keySource is the terminal while a turn owns it.
type keySource interface {
	// Events delivers what is typed, in order.
	Events() <-chan keyEvent
	// Stop gives the terminal back and returns what was read but not yet
	// delivered: complete lines and a consumed partial line (never lost).
	Stop() (lines []string, partial string)
}

// turnHost is the session the owned turns run against.
type turnHost interface {
	// Submit runs one turn and renders it; it returns when the turn ends.
	Submit(ctx context.Context, text string) error
	// Enqueue queues a mid-turn line for the running turn.
	Enqueue(text string) error
	// TakeQueued returns the lines no turn drained.
	TakeQueued() ([]string, error)
	// Settle waits for a cancelled turn to record its end.
	Settle(ctx context.Context) bool
}

// errTurnInterrupted marks a turn stopped by ESC or Ctrl-C.
var errTurnInterrupted = errors.New("interrupted: the turn stopped before its next step; the session continues")

// settleTimeout bounds the wait for a cancelled turn to record its end.
const settleTimeout = 10 * time.Second

// signalGrace is how long a cancelled process context waits for its SIGINT
// to arrive on the interrupt channel too: SIGINT is a turn interrupt here,
// anything else (SIGTERM) ends the turns.
const signalGrace = 200 * time.Millisecond

// runOwnedTurns runs text as a turn with the terminal owned, then every
// follow-up queued during it, until nothing is left. sigint delivers the
// process's SIGINTs (Ctrl-C), which interrupt the turn like ESC.
func runOwnedTurns(ctx context.Context, host turnHost, open func() (keySource, error), text string, notices io.Writer, sigint <-chan os.Signal) error {
	// Ctrl-C is a turn interrupt while turns own the terminal, so the turns
	// run on a context the process's own SIGINT cancellation does not end;
	// ctx still ends them for anything else.
	o := &owner{host: host, notices: notices, sigint: sigint, done: ctx.Done(), base: context.WithoutCancel(ctx)}
	var interrupted error
	for text != "" {
		keys, err := open()
		if err != nil {
			// No terminal to own (not the foreground job, not a canonical
			// tty): the turn runs exactly as before.
			return host.Submit(ctx, text)
		}
		err = o.turns(keys, text)
		lines, partial := keys.Stop()
		if errors.Is(err, errTurnInterrupted) {
			interrupted, err = err, nil
		}
		if err != nil {
			return err
		}
		if partial != "" {
			lines = append(lines, partial)
		}
		var next []string
		for _, line := range lines {
			if strings.TrimSpace(line) != "" {
				next = append(next, line)
			}
		}
		if len(next) > 0 {
			interrupted = nil
		}
		text = strings.Join(next, "\n")
	}
	return interrupted
}

type owner struct {
	host    turnHost
	notices io.Writer
	sigint  <-chan os.Signal
	done    <-chan struct{} // the process context; nil once it has ended
	base    context.Context
	// sawSIGINT: a SIGINT was taken as a turn interrupt, so the process
	// context's end it also caused is not a request to stop.
	sawSIGINT bool
}

// turns runs text and then every follow-up while keys owns the terminal. It
// returns errTurnInterrupted when the last turn was interrupted with nothing
// queued after it, or the error that ended the turns.
func (o *owner) turns(keys keySource, text string) error {
	events := keys.Events()
	for text != "" {
		turnCtx, cancel := context.WithCancel(o.base)
		done := make(chan error, 1)
		go func(text string) { done <- o.host.Submit(turnCtx, text) }(text)
		interrupted, terminated := false, false
		interrupt := func() {
			if !interrupted {
				interrupted = true
				cancel()
			}
		}
		var result error
	wait:
		for {
			select {
			case result = <-done:
				break wait
			case <-o.sigint:
				o.sawSIGINT = true
				interrupt()
			case <-o.done:
				// The process context ends on SIGINT and SIGTERM alike. A
				// SIGINT also arrives on o.sigint (before or just after):
				// that is a turn interrupt; anything else ends the turns.
				o.done = nil
				if o.sawSIGINT {
					continue
				}
				select {
				case <-o.sigint:
					o.sawSIGINT = true
					interrupt()
				case <-time.After(signalGrace):
					terminated = true
					cancel()
				}
			case ev, ok := <-events:
				if !ok {
					events = nil
				} else if ev.interrupt {
					interrupt()
				} else {
					o.enqueue(ev.line)
				}
			}
		}
		cancel()
		if interrupted || terminated {
			settle, stop := context.WithTimeout(context.Background(), settleTimeout)
			o.host.Settle(settle)
			stop()
		}
		if terminated {
			return result
		}
		// What was typed after the turn ended but before this point is
		// still in the channel: queue it with the rest.
		for drained := false; !drained; {
			select {
			case ev, ok := <-events:
				if !ok {
					events, drained = nil, true
				} else if !ev.interrupt {
					o.enqueue(ev.line)
				}
			default:
				drained = true
			}
		}
		queued, err := o.host.TakeQueued()
		if err != nil {
			return err
		}
		switch {
		case interrupted:
			if len(queued) == 0 {
				return errTurnInterrupted
			}
			fmt.Fprintf(o.notices, "\r\nycode: %v; running what was typed during it\r\n", errTurnInterrupted)
		case result != nil:
			if len(queued) == 0 {
				return result
			}
			// Keep what the user typed: it runs next, after the failure is
			// shown.
			fmt.Fprintf(o.notices, "\r\nycode: %v\r\n", result)
		}
		text = strings.Join(queued, "\n")
	}
	return nil
}

func (o *owner) enqueue(line string) {
	if err := enqueueLine(o.host, line); err != nil {
		fmt.Fprintf(o.notices, "\r\nycode: could not queue that line: %v\r\n", err)
	}
}

func enqueueLine(host turnHost, line string) error {
	if strings.TrimSpace(line) == "" {
		return nil // a bare Enter
	}
	return host.Enqueue(line)
}

// ownedTurnHost adapts the harness application to turnHost.
type ownedTurnHost struct {
	app      *harnessApplication
	session  string
	queueRef string
	submit   func(context.Context, string) error
}

func (h ownedTurnHost) Submit(ctx context.Context, text string) error { return h.submit(ctx, text) }

func (h ownedTurnHost) Enqueue(text string) error {
	return h.app.harness.Enqueue(public.QueueRequest{SessionID: h.session, QueueRef: h.queueRef, Class: "steering", Text: text})
}

func (h ownedTurnHost) TakeQueued() ([]string, error) {
	items, err := h.app.harness.TakeQueued(h.session, h.queueRef)
	if err != nil {
		return nil, err
	}
	lines := make([]string, 0, len(items))
	for _, item := range items {
		lines = append(lines, item.Text)
	}
	return lines, nil
}

func (h ownedTurnHost) Settle(ctx context.Context) bool { return h.app.harness.Settle(ctx, h.session) }

// ownsTerminalTurns reports whether an args-mode turn should own the
// terminal: it was typed in a terminal front end (the session pointer is
// exported), a human or pty is on stdin, and the agent has a compiled queue
// with a steering class to deliver mid-turn lines to.
func ownsTerminalTurns(app *harnessApplication, agentRef, frontendRef string) (string, bool) {
	if !insideTerminal() || !stdinIsTerminal() {
		return "", false
	}
	queueRef := app.queueFor(agentRef, frontendRef)
	if queueRef == "" {
		return "", false
	}
	if _, ok := app.doc.Spec.Queues[queueRef].Priorities["steering"]; !ok {
		return "", false
	}
	return queueRef, true
}

// queueFor is the queue an agent's mid-session input goes to: the agent's own
// queueRef, else the trigger route's.
func (a *harnessApplication) queueFor(agentRef, frontendRef string) string {
	trigger, err := a.triggerFor(frontendRef)
	if err != nil {
		return ""
	}
	route := a.doc.Spec.Triggers[trigger].Route
	if agentRef == "" {
		agentRef = route.AgentRef
	}
	if agent, ok := a.doc.Spec.Agents[agentRef]; ok && agent.QueueRef != "" {
		return agent.QueueRef
	}
	return route.QueueRef
}

// interruptedError is the CLI's exit for a turn stopped by ESC.
func (a *harnessApplication) interruptedError(err error) error {
	if !errors.Is(err, errTurnInterrupted) {
		return err
	}
	return &harnesscli.Error{Class: "interrupted", Code: a.doc.Spec.Interfaces.CLI.ExitCodes.Interrupted, Err: err}
}

// openStdinKeys owns the process's terminal (os.Stdin).
func openStdinKeys() (keySource, error) { return openTerminalKeys(os.Stdin) }
