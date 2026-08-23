package cli

import (
	"bufio"
	"fmt"
	"io"
	"strings"

	"github.com/qiangli/ycode/internal/api"
	"github.com/qiangli/ycode/internal/runtime/session"
	"github.com/qiangli/ycode/internal/wireevents"
)

// StartSteerReader begins collecting injected steering for a headless run.
//
// An orchestrator that launched `ycode prompt "<task>"` under a pty (weave,
// CI wrappers) has exactly one channel left to reach the run: the process's
// stdin. Before this existed, lines written there were silently discarded —
// the orchestrator steered, the run kept executing its original plan, and
// nothing anywhere said the steering had been dropped.
//
// Each non-empty line read from r becomes one steering message. The contract
// is deliberately modest and honest: steering is consumed at the NEXT TURN
// BOUNDARY — after the in-flight model turn and its tool calls have finished,
// never preempting a running tool — and each consumed line is acknowledged on
// the --events wire as steer.consumed. There is no mid-turn interrupt here;
// a caller that needs to stop a run now should signal the process.
//
// Interactive sessions never call this; the TUI has its own mid-turn input
// path. Safe to call once per App; later calls are no-ops.
func (a *App) StartSteerReader(r io.Reader) {
	if r == nil || a.steerCh != nil {
		return
	}
	ch := make(chan string, 64)
	a.steerCh = ch
	go func() {
		sc := bufio.NewScanner(r)
		sc.Buffer(make([]byte, 0, 64*1024), 1024*1024)
		for sc.Scan() {
			line := strings.TrimSpace(sc.Text())
			if line == "" {
				continue
			}
			// Blocking send: if the orchestrator floods faster than turn
			// boundaries drain, back-pressure lands on the writer, not on
			// this run's memory. The goroutine ends with the process.
			ch <- line
		}
	}()
}

// consumeSteering drains pending steering at a turn boundary and appends it to
// the conversation as one user message, so the very next model turn sees it.
// Called only from the headless agentic loop, between turns — never while a
// tool call is in flight.
func (a *App) consumeSteering(messages *[]api.Message) {
	if a.steerCh == nil {
		return
	}
	var lines []string
	for {
		select {
		case line := <-a.steerCh:
			lines = append(lines, line)
			continue
		default:
		}
		break
	}
	if len(lines) == 0 {
		return
	}

	text := "[User steering received while the agent was working — apply it from this point on]\n" +
		strings.Join(lines, "\n")
	*messages = append(*messages, api.Message{
		Role:    api.RoleUser,
		Content: []api.ContentBlock{{Type: api.ContentTypeText, Text: text}},
	})
	_ = a.session.AddMessage(session.ConversationMessage{
		Role: session.RoleUser,
		Content: []session.ContentBlock{
			{Type: session.ContentTypeText, Text: text},
		},
	})
	fmt.Fprintf(a.chromeWriter(), "\n↪ Steering consumed at turn boundary (%d line(s)).\n", len(lines))
	for _, line := range lines {
		a.emitEvent(wireevents.SteerConsumed, wireevents.SteerConsumedData{
			Text: line,
			Turn: a.turnIndex + 1,
		})
	}
}
