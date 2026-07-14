// Package wireevents is the EXTERNAL event contract: the NDJSON stream ycode
// writes when it is given --events.
//
// # Why this package exists at all
//
// ycode had two event vocabularies for the same three facts, inside one binary:
//
//	the one-shot path (App.RunPrompt)   emitted  turn.start / tool.call / turn.end
//	the internal bus  (the TUI's path)  carries  turn.start / tool_use.start / turn.complete
//
// So --events worked from `ycode prompt` and did nothing at all from the TUI —
// and the TUI is what an orchestrator launches when it wants a session it can
// steer. bashy would ask for a turn boundary, receive silence, and fall back to
// guessing that a turn had ended after 25 seconds of quiet.
//
// # The fix, and the layering it implies
//
// The bus is ycode's INTERNAL nervous system: sixteen event types, several
// subscribers, and no reason to care what anyone outside thinks. Renaming
// EventTurnComplete to please an external consumer would be the tail wagging the
// dog.
//
// So the wire is its own thing, and it has exactly three words:
//
//	{"type":"turn.start","data":{"prompt":"..."}}
//	{"type":"tool.call","data":{"name":"read_file","input":{...}}}
//	{"type":"turn.end","data":{"status":"ok","text":"the answer"}}
//
// This package OWNS those three words. Both paths go through it, so the vocabulary
// cannot drift apart again — which is exactly what happened when each path was
// allowed to name things for itself.
//
// # Where this does NOT reach, stated plainly
//
// SERVER MODE. When `ycode serve` is running, the TUI becomes a thin client and
// the agent loop runs in the SERVER process — which never sees the client's
// --events flag. So an orchestrator that starts a server first gets no wire.
//
// This is a real gap and it is left open rather than papered over. I wrote a bus
// bridge for it (subscribe to the internal bus, translate, write the wire), got it
// building, and then deleted it: it hung off the CLIENT's App, so it would have
// subscribed to the client's bus, which carries nothing. It compiled, it read like
// a feature, and it did nothing — which is the exact failure this file exists to
// stop. Closing this properly means a sink on the SERVER side, and that is a
// different design, not a missing call.
//
// # What turn.end is for
//
// It is the thing an orchestrator cannot get from any third-party CLI: a turn's
// end as a FACT THE AGENT REPORTED, rather than a silence somebody interpreted.
// `text` is the assistant's final answer, and it must be exactly what --print
// writes to stdout — a consumer compares them, and if they disagree one of us is
// lying.
package wireevents

import (
	"encoding/json"
	"fmt"
	"io"
	"os"
	"sync"
	"time"
)

// The three words. This is the whole vocabulary; resist adding a fourth without a
// consumer that needs it.
const (
	TurnStart = "turn.start"
	ToolCall  = "tool.call"
	TurnEnd   = "turn.end"
)

// Event is one line of the wire.
type Event struct {
	Type      string    `json:"type"`
	Timestamp time.Time `json:"timestamp"`
	Data      any       `json:"data,omitempty"`
}

// Writer serializes events to an NDJSON stream, one object per line.
//
// Safe for concurrent use: the TUI's bus bridge and the one-shot path can both be
// live in a single process, and a torn line on this stream is not a cosmetic
// problem — the consumer tails the file while it is being written, so a half-object
// is a parse error at the other end.
type Writer struct {
	mu  sync.Mutex
	w   io.WriteCloser
	enc *json.Encoder
}

// NewFileWriter opens an NDJSON event stream at path.
func NewFileWriter(path string) (*Writer, error) {
	f, err := os.OpenFile(path, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0o600)
	if err != nil {
		return nil, fmt.Errorf("events file: %w", err)
	}
	return &Writer{w: f, enc: json.NewEncoder(f)}, nil
}

// Emit writes one event. Errors are returned, never swallowed: an event channel
// that silently stops working is worse than one that was never opened, because a
// consumer is waiting on it.
func (w *Writer) Emit(typ string, data any) error {
	if w == nil {
		return nil
	}
	w.mu.Lock()
	defer w.mu.Unlock()
	return w.enc.Encode(Event{Type: typ, Timestamp: time.Now(), Data: data})
}

// Close flushes and closes the stream.
func (w *Writer) Close() error {
	if w == nil {
		return nil
	}
	w.mu.Lock()
	defer w.mu.Unlock()
	return w.w.Close()
}

// TurnStartData / ToolCallData / TurnEndData are the payloads, named so the shape
// of the wire is legible from one place rather than inferred from map literals
// scattered across two subsystems.
type TurnStartData struct {
	Prompt string `json:"prompt,omitempty"`
}

type ToolCallData struct {
	Name  string `json:"name"`
	Input any    `json:"input,omitempty"`
}

type TurnEndData struct {
	Status string `json:"status"`
	Text   string `json:"text"`
}
