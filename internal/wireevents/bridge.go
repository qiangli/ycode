package wireevents

import (
	"encoding/json"

	"github.com/qiangli/ycode/internal/bus"
)

// Bridge translates ycode's internal bus into the external wire.
//
// This is what makes --events work from the TUI. The one-shot path (RunPrompt)
// calls Writer directly because it does not use the bus at all; the TUI runs the
// agent loop through the service layer, which publishes to the bus, and nothing
// was listening. So --events was accepted, ignored, and produced an empty file —
// a flag that looked supported and did nothing, which is the worst kind.
//
// The translation is deliberately LOSSY and deliberately small. The bus carries
// sixteen event types including text deltas, thinking deltas, LLM request/response
// snapshots and permission round-trips. An orchestrator does not want a firehose;
// it wants to know when a turn started, what tools ran, and when the turn ended
// with what answer. Three words. Anything else belongs on the bus, where it
// already is.
//
//	bus turn.start      -> wire turn.start   (the prompt)
//	bus tool_use.start  -> wire tool.call    (the CALL, not the result)
//	bus turn.complete   -> wire turn.end     (status + the final text)
//
// tool_use.start rather than tool.result on purpose: the consumer wants to know
// the agent DECIDED to call something. Whether it succeeded is the agent's problem
// and shows up in the answer.
type Bridge struct {
	w      *Writer
	unsub  func()
	events <-chan bus.Event
	done   chan struct{}
}

// StartBridge subscribes to the bus and writes the wire until Stop is called.
// A nil Writer (no --events) makes this a no-op, so callers need no branch.
func StartBridge(b bus.Bus, w *Writer) *Bridge {
	if b == nil || w == nil {
		return nil
	}
	ch, unsub := b.Subscribe(
		bus.EventTurnStart,
		bus.EventToolUseStart,
		bus.EventTurnComplete,
	)
	br := &Bridge{w: w, unsub: unsub, events: ch, done: make(chan struct{})}
	go br.run()
	return br
}

func (b *Bridge) run() {
	defer close(b.done)
	for ev := range b.events {
		switch ev.Type {
		case bus.EventTurnStart:
			var d struct {
				Text string `json:"text"`
			}
			_ = json.Unmarshal(ev.Data, &d)
			_ = b.w.Emit(TurnStart, TurnStartData{Prompt: d.Text})

		case bus.EventToolUseStart:
			// The runtime publishes {"tool": name, "input": ...}. The wire says
			// "name", because that is what the wire has always said and a consumer
			// should not have to know which of ycode's subsystems produced the line.
			var d struct {
				Tool  string          `json:"tool"`
				Input json.RawMessage `json:"input"`
			}
			_ = json.Unmarshal(ev.Data, &d)
			var input any
			if len(d.Input) > 0 {
				_ = json.Unmarshal(d.Input, &input)
			}
			_ = b.w.Emit(ToolCall, ToolCallData{Name: d.Tool, Input: input})

		case bus.EventTurnComplete:
			// The bus reports status "complete" or "loop_break"; the wire reports
			// "ok" or the raw status. A consumer waiting for a turn to END does not
			// care WHY it ended — but it does care that it ended, and a loop_break
			// is a turn that ended.
			var d struct {
				Status string `json:"status"`
				Text   string `json:"text"`
			}
			_ = json.Unmarshal(ev.Data, &d)
			status := d.Status
			if status == "complete" {
				status = "ok"
			}
			_ = b.w.Emit(TurnEnd, TurnEndData{Status: status, Text: d.Text})
		}
	}
}

// Stop unsubscribes and waits for the last event to be written.
//
// It waits on purpose. A turn.end that is still in flight when the process exits
// is a turn.end the consumer never sees — and the consumer is BLOCKED on it,
// waiting to learn that the turn is over.
func (b *Bridge) Stop() {
	if b == nil {
		return
	}
	b.unsub()
	<-b.done
}
