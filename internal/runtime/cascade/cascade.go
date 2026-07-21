// Package cascade climbs a model ladder when the current model has
// demonstrably stopped making progress.
//
// A cascade agent (ycode-cascade-x4: glm-5.2 → gpt-5.6-terra → gpt-5.6-sol) is
// supposed to be cheap most of the time and expensive only when it has to be.
// What it actually did was run the BASE for the entire session: the loop
// detectors fired, the coach flagged an unresolved loop, the run failed — all
// on the cheapest rung, because nothing was wired to change the served model.
// A cascade that never escalates is not a cascade; it is a base model with a
// longer name.
//
// This package is that missing wire. It consumes the signals the runtime
// ALREADY produces (the response and tool-call loop detectors, and turns that
// end with nothing but tool failures), and answers one question: should the
// next turn run on a stronger model?
//
// Two rules, both deliberately blunt:
//
//   - A detected LOOP escalates immediately. The loop detectors only fire after
//     several near-identical turns; waiting longer just buys more of the same.
//   - Repeated STALLS (turns whose tool calls all failed, or that made no
//     progress) escalate after StallThreshold of them in a row. One bad turn is
//     noise; three in a row is a model that cannot do this task.
//
// Any real progress resets the stall counter but never DEMOTES: once a run has
// needed premium help, dropping back to the model that was already stuck just
// re-enters the loop.
//
// Unavailability is loud. If the next rung has no credentials or is otherwise
// unusable, that is reported to the caller — the failure mode this package
// exists to kill is a silent continue on the base.
package cascade

import (
	"errors"
	"fmt"
	"log/slog"
	"strings"
	"sync"
)

// Signal is what one completed turn tells the escalator.
type Signal int

const (
	// SignalProgress — the turn accomplished something. Resets the stall count.
	SignalProgress Signal = iota
	// SignalStall — the turn made no progress (all tool calls failed, or no
	// mutation happened). Escalates once StallThreshold of them stack up.
	SignalStall
	// SignalLoop — a loop detector fired. Escalates immediately.
	SignalLoop
)

// String renders the signal for logs and the escalation reason.
func (s Signal) String() string {
	switch s {
	case SignalStall:
		return "stall"
	case SignalLoop:
		return "loop"
	default:
		return "progress"
	}
}

// DefaultStallThreshold is how many consecutive no-progress turns escalate.
const DefaultStallThreshold = 3

// ErrLadderExhausted reports that the run is already on the top rung — there is
// no stronger model to call in.
var ErrLadderExhausted = errors.New("cascade ladder exhausted: already on the top rung")

// Switch describes one escalation that happened.
type Switch struct {
	From   string // model the run was on
	To     string // model subsequent turns use
	Rung   int    // index into the ladder (0 = base)
	Reason string // why: "loop", "stall_x3", ...
}

// Config configures an Escalator.
type Config struct {
	// Ladder is the ordered model list, base first. Fewer than two entries
	// disables escalation entirely (New returns nil).
	Ladder []string

	// StallThreshold is the number of consecutive no-progress turns that
	// triggers an escalation. Zero uses DefaultStallThreshold.
	StallThreshold int

	// Probe reports whether a model is usable right now (credentials, quota).
	// A non-nil error skips that rung and is reported when every rung fails.
	// nil assumes every rung is usable.
	Probe func(model string) error

	// Logger records each switch and each skipped rung. nil uses slog.Default.
	Logger *slog.Logger

	// OnSwitch, when set, is called after a successful escalation — the hook
	// for emitting an event and re-pointing the runtime at the new model.
	OnSwitch func(Switch)
}

// Escalator tracks progress signals and advances the served model.
// A nil *Escalator is a no-op: every method tolerates a nil receiver, so
// non-cascade runs wire it unconditionally.
type Escalator struct {
	mu       sync.Mutex
	ladder   []string
	rung     int
	stalls   int
	thresh   int
	reason   string
	probe    func(string) error
	log      *slog.Logger
	onSwitch func(Switch)
}

// New builds an Escalator, or nil when the ladder has nothing to climb.
func New(cfg Config) *Escalator {
	ladder := make([]string, 0, len(cfg.Ladder))
	for _, m := range cfg.Ladder {
		if m = strings.TrimSpace(m); m != "" {
			ladder = append(ladder, m)
		}
	}
	if len(ladder) < 2 {
		return nil
	}
	thresh := cfg.StallThreshold
	if thresh <= 0 {
		thresh = DefaultStallThreshold
	}
	log := cfg.Logger
	if log == nil {
		log = slog.Default()
	}
	return &Escalator{
		ladder:   ladder,
		thresh:   thresh,
		probe:    cfg.Probe,
		log:      log,
		onSwitch: cfg.OnSwitch,
	}
}

// Base returns the bottom rung — the model the run started on.
func (e *Escalator) Base() string {
	if e == nil {
		return ""
	}
	return e.ladder[0]
}

// Model returns the model subsequent turns should use.
func (e *Escalator) Model() string {
	if e == nil {
		return ""
	}
	e.mu.Lock()
	defer e.mu.Unlock()
	return e.ladder[e.rung]
}

// Escalated reports whether the run has left the base model.
func (e *Escalator) Escalated() bool {
	if e == nil {
		return false
	}
	e.mu.Lock()
	defer e.mu.Unlock()
	return e.rung > 0
}

// Reason returns why the last escalation happened ("" if none).
func (e *Escalator) Reason() string {
	if e == nil {
		return ""
	}
	e.mu.Lock()
	defer e.mu.Unlock()
	return e.reason
}

// Ladder returns a copy of the configured ladder.
func (e *Escalator) Ladder() []string {
	if e == nil {
		return nil
	}
	return append([]string(nil), e.ladder...)
}

// Observe records one turn's outcome.
//
// It returns a non-nil *Switch when the served model changed — the caller must
// re-point the runtime at Switch.To. It returns an error when escalation was
// WARRANTED but impossible (top rung already, or every remaining rung is
// unavailable); that error is meant to be surfaced loudly, not swallowed,
// because the run is now knowingly continuing on a model that is stuck.
func (e *Escalator) Observe(sig Signal) (*Switch, error) {
	if e == nil {
		return nil, nil
	}
	e.mu.Lock()
	defer e.mu.Unlock()

	switch sig {
	case SignalProgress:
		e.stalls = 0
		return nil, nil
	case SignalStall:
		e.stalls++
		if e.stalls < e.thresh {
			return nil, nil
		}
	}

	reason := sig.String()
	if sig == SignalStall {
		reason = fmt.Sprintf("stall_x%d", e.stalls)
	}
	e.stalls = 0
	return e.advanceLocked(reason)
}

// Force escalates one rung regardless of accumulated signals, for callers with
// their own stuck-detection (an operator command, a supervisor's verdict).
func (e *Escalator) Force(reason string) (*Switch, error) {
	if e == nil {
		return nil, nil
	}
	e.mu.Lock()
	defer e.mu.Unlock()
	if strings.TrimSpace(reason) == "" {
		reason = "forced"
	}
	return e.advanceLocked(reason)
}

// advanceLocked moves to the next USABLE rung. Assumes e.mu is held.
func (e *Escalator) advanceLocked(reason string) (*Switch, error) {
	from := e.ladder[e.rung]
	if e.rung >= len(e.ladder)-1 {
		return nil, fmt.Errorf("%w (model %s, reason %s)", ErrLadderExhausted, from, reason)
	}

	var unavailable []string
	for next := e.rung + 1; next < len(e.ladder); next++ {
		model := e.ladder[next]
		if e.probe != nil {
			if err := e.probe(model); err != nil {
				// LOUD: a rung we cannot reach is an operator problem
				// (missing key, exhausted quota), not a detail.
				e.log.Error("cascade: escalation tier unavailable, skipping rung",
					"model", model, "rung", next, "reason", reason, "error", err)
				unavailable = append(unavailable, fmt.Sprintf("%s (%v)", model, err))
				continue
			}
		}
		e.rung = next
		e.reason = reason
		sw := Switch{From: from, To: model, Rung: next, Reason: reason}
		e.log.Warn("cascade: escalating to premium tier",
			"from", from, "to", model, "rung", next, "reason", reason)
		if e.onSwitch != nil {
			e.onSwitch(sw)
		}
		return &sw, nil
	}

	return nil, fmt.Errorf("cascade: %s is stuck (%s) and every escalation tier is unavailable: %s",
		from, reason, strings.Join(unavailable, "; "))
}
