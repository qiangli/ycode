// Package loopdetect implements multi-detector tool-level loop detection
// for autonomous agent operation. Inspired by openclaw's 5-detector system
// with progressive severity and deterministic outcome hashing.
//
// Unlike autoloop's stall detection (which operates at iteration/score level),
// this package detects loops at the individual tool-call level: repeated calls
// with identical arguments, ping-pong patterns between two tools, polling
// without progress, and global repetition ceilings.
package loopdetect

import (
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"log/slog"
)

// Severity classifies how urgent a loop detection is.
type Severity int

const (
	// SeverityNone means no loop detected.
	SeverityNone Severity = iota
	// SeverityWarning means early signs of a loop; inject guidance.
	SeverityWarning
	// SeverityCritical means strong evidence of a loop; force exit or strategy change.
	SeverityCritical
)

// String returns a human-readable severity label.
func (s Severity) String() string {
	switch s {
	case SeverityNone:
		return "none"
	case SeverityWarning:
		return "warning"
	case SeverityCritical:
		return "critical"
	default:
		return "unknown"
	}
}

// DetectorKind identifies which detector fired.
type DetectorKind string

const (
	DetectorGenericRepeat  DetectorKind = "generic_repeat"
	DetectorUnknownTool    DetectorKind = "unknown_tool"
	DetectorPollNoProgress DetectorKind = "poll_no_progress"
	DetectorPingPong       DetectorKind = "ping_pong"
	DetectorGlobalCeiling  DetectorKind = "global_ceiling"
)

// Result is the outcome of running all detectors against the current history.
type Result struct {
	Severity Severity
	Detector DetectorKind
	Message  string
}

// ToolCall records a single tool invocation and its outcome for loop analysis.
type ToolCall struct {
	Name   string
	Args   string // JSON-serialized arguments
	Status string // "success", "error", "timeout"
	Output string // truncated output for hashing
}

// Digest returns a SHA-256 hash of the tool call for deduplication.
func (tc ToolCall) Digest() string {
	h := sha256.New()
	h.Write([]byte(tc.Name))
	h.Write([]byte{0})
	h.Write([]byte(tc.Args))
	return fmt.Sprintf("%x", h.Sum(nil))
}

// OutcomeDigest returns a SHA-256 hash including the result, for no-progress detection.
func (tc ToolCall) OutcomeDigest() string {
	h := sha256.New()
	h.Write([]byte(tc.Name))
	h.Write([]byte{0})
	h.Write([]byte(tc.Args))
	h.Write([]byte{0})
	h.Write([]byte(tc.Status))
	h.Write([]byte{0})
	h.Write([]byte(tc.Output))
	return fmt.Sprintf("%x", h.Sum(nil))
}

// Thresholds configures when each detector fires.
type Thresholds struct {
	// GenericRepeatWarning fires warning after N identical consecutive calls.
	GenericRepeatWarning int // default 5
	// GenericRepeatCritical fires critical after N identical consecutive calls.
	GenericRepeatCritical int // default 10
	// UnknownToolMax fires critical after N calls to non-existent tools.
	UnknownToolMax int // default 5
	// PollNoProgressWarning fires when N consecutive polls return same outcome.
	PollNoProgressWarning int // default 5
	// PollNoProgressCritical fires when N consecutive polls return same outcome.
	PollNoProgressCritical int // default 10
	// PingPongWarning fires after N alternations between two tools.
	PingPongWarning int // default 4 (pairs)
	// PingPongCritical fires after N alternations between two tools.
	PingPongCritical int // default 8 (pairs)
	// GlobalCeiling fires critical after N total repetitions of any single digest.
	GlobalCeiling int // default 20
}

// DefaultThresholds returns sensible defaults.
func DefaultThresholds() Thresholds {
	return Thresholds{
		GenericRepeatWarning:   5,
		GenericRepeatCritical:  10,
		UnknownToolMax:         5,
		PollNoProgressWarning:  5,
		PollNoProgressCritical: 10,
		PingPongWarning:        4,
		PingPongCritical:       8,
		GlobalCeiling:          20,
	}
}

// Tracker maintains tool call history and runs all detectors.
type Tracker struct {
	thresholds Thresholds
	logger     *slog.Logger

	// History of tool calls (bounded).
	history []ToolCall
	maxHist int

	// Digest → count for global ceiling.
	digestCounts map[string]int

	// Known tool names (populated externally).
	knownTools map[string]bool
}

// New creates a loop detection tracker.
func New(thresholds Thresholds, logger *slog.Logger) *Tracker {
	if logger == nil {
		logger = slog.Default()
	}
	return &Tracker{
		thresholds:   thresholds,
		logger:       logger,
		maxHist:      100,
		digestCounts: make(map[string]int),
		knownTools:   make(map[string]bool),
	}
}

// RegisterTool marks a tool name as known/valid.
func (t *Tracker) RegisterTool(name string) {
	t.knownTools[name] = true
}

// RegisterTools marks multiple tool names as known/valid.
func (t *Tracker) RegisterTools(names []string) {
	for _, n := range names {
		t.knownTools[n] = true
	}
}

// Record adds a tool call to the history and runs all detectors.
// Returns the highest-severity detection result.
func (t *Tracker) Record(call ToolCall) Result {
	t.history = append(t.history, call)
	if len(t.history) > t.maxHist {
		t.history = t.history[len(t.history)-t.maxHist:]
	}

	digest := call.Digest()
	t.digestCounts[digest]++

	// Run all detectors; return the most severe result.
	detectors := []func(ToolCall) Result{
		t.detectGlobalCeiling,
		t.detectGenericRepeat,
		t.detectUnknownTool,
		t.detectPollNoProgress,
		t.detectPingPong,
	}

	var worst Result
	for _, detect := range detectors {
		r := detect(call)
		if r.Severity > worst.Severity {
			worst = r
		}
	}

	if worst.Severity > SeverityNone {
		t.logger.Warn("loop detected",
			"detector", worst.Detector,
			"severity", worst.Severity.String(),
			"tool", call.Name,
			"message", worst.Message,
		)
	}

	return worst
}

// detectGenericRepeat checks for consecutive identical tool calls.
func (t *Tracker) detectGenericRepeat(call ToolCall) Result {
	digest := call.Digest()
	consecutive := 0
	for i := len(t.history) - 1; i >= 0; i-- {
		if t.history[i].Digest() == digest {
			consecutive++
		} else {
			break
		}
	}

	if t.thresholds.GenericRepeatCritical > 0 && consecutive >= t.thresholds.GenericRepeatCritical {
		return Result{
			Severity: SeverityCritical,
			Detector: DetectorGenericRepeat,
			Message:  fmt.Sprintf("tool %q called %d times consecutively with identical args", call.Name, consecutive),
		}
	}
	if t.thresholds.GenericRepeatWarning > 0 && consecutive >= t.thresholds.GenericRepeatWarning {
		return Result{
			Severity: SeverityWarning,
			Detector: DetectorGenericRepeat,
			Message:  fmt.Sprintf("tool %q called %d times consecutively with identical args", call.Name, consecutive),
		}
	}
	return Result{}
}

// detectUnknownTool checks for repeated calls to non-existent tools.
func (t *Tracker) detectUnknownTool(call ToolCall) Result {
	if len(t.knownTools) == 0 || t.knownTools[call.Name] {
		return Result{} // known or no registry
	}

	count := 0
	for i := len(t.history) - 1; i >= 0; i-- {
		if !t.knownTools[t.history[i].Name] {
			count++
		} else {
			break
		}
	}

	if t.thresholds.UnknownToolMax > 0 && count >= t.thresholds.UnknownToolMax {
		return Result{
			Severity: SeverityCritical,
			Detector: DetectorUnknownTool,
			Message:  fmt.Sprintf("tool %q does not exist; %d consecutive unknown tool calls", call.Name, count),
		}
	}
	return Result{}
}

// detectPollNoProgress checks for consecutive identical outcomes (same tool+args+result).
func (t *Tracker) detectPollNoProgress(call ToolCall) Result {
	outcomeDigest := call.OutcomeDigest()
	consecutive := 0
	for i := len(t.history) - 1; i >= 0; i-- {
		if t.history[i].OutcomeDigest() == outcomeDigest {
			consecutive++
		} else {
			break
		}
	}

	if t.thresholds.PollNoProgressCritical > 0 && consecutive >= t.thresholds.PollNoProgressCritical {
		return Result{
			Severity: SeverityCritical,
			Detector: DetectorPollNoProgress,
			Message:  fmt.Sprintf("tool %q polled %d times with identical outcome — no progress", call.Name, consecutive),
		}
	}
	if t.thresholds.PollNoProgressWarning > 0 && consecutive >= t.thresholds.PollNoProgressWarning {
		return Result{
			Severity: SeverityWarning,
			Detector: DetectorPollNoProgress,
			Message:  fmt.Sprintf("tool %q polled %d times with identical outcome — no progress", call.Name, consecutive),
		}
	}
	return Result{}
}

// detectPingPong checks for alternating calls between exactly two tools.
func (t *Tracker) detectPingPong(_ ToolCall) Result {
	n := len(t.history)
	if n < 4 {
		return Result{}
	}

	// Walk backward counting how many consecutive calls alternate between
	// exactly two tool names. Count individual alternations, not pairs.
	a := t.history[n-1].Name
	b := t.history[n-2].Name
	if a == b {
		return Result{} // not a ping-pong if same tool
	}

	alternations := 2 // the last two are already confirmed different
	for i := n - 3; i >= 0; i-- {
		expected := a
		if (n-1-i)%2 == 1 {
			expected = b
		}
		if t.history[i].Name != expected {
			break
		}
		alternations++
	}

	// Convert to pairs (each pair = 2 alternations).
	pairs := alternations / 2

	if t.thresholds.PingPongCritical > 0 && pairs >= t.thresholds.PingPongCritical {
		return Result{
			Severity: SeverityCritical,
			Detector: DetectorPingPong,
			Message:  fmt.Sprintf("ping-pong detected between %q and %q (%d alternations)", a, b, pairs),
		}
	}
	if t.thresholds.PingPongWarning > 0 && pairs >= t.thresholds.PingPongWarning {
		return Result{
			Severity: SeverityWarning,
			Detector: DetectorPingPong,
			Message:  fmt.Sprintf("ping-pong detected between %q and %q (%d alternations)", a, b, pairs),
		}
	}
	return Result{}
}

// detectGlobalCeiling checks if any single call digest exceeds the global max.
func (t *Tracker) detectGlobalCeiling(call ToolCall) Result {
	digest := call.Digest()
	count := t.digestCounts[digest]

	if t.thresholds.GlobalCeiling > 0 && count >= t.thresholds.GlobalCeiling {
		return Result{
			Severity: SeverityCritical,
			Detector: DetectorGlobalCeiling,
			Message:  fmt.Sprintf("tool %q exceeded global ceiling (%d/%d total calls with same args)", call.Name, count, t.thresholds.GlobalCeiling),
		}
	}
	return Result{}
}

// Reset clears all history and counters.
func (t *Tracker) Reset() {
	t.history = nil
	t.digestCounts = make(map[string]int)
}

// Stats returns current tracking statistics.
func (t *Tracker) Stats() Stats {
	return Stats{
		HistoryLen:     len(t.history),
		UniqueDigests:  len(t.digestCounts),
		KnownToolCount: len(t.knownTools),
	}
}

// Stats holds tracker statistics for observability.
type Stats struct {
	HistoryLen     int
	UniqueDigests  int
	KnownToolCount int
}

// GuidanceMessage returns corrective guidance to inject into the agent's
// context when a loop is detected. Severity determines the tone.
func GuidanceMessage(r Result) string {
	if r.Severity == SeverityNone {
		return ""
	}

	base := fmt.Sprintf("[Loop Detection — %s] %s", r.Detector, r.Message)

	switch r.Severity {
	case SeverityWarning:
		return base + "\n\nYou appear to be in a loop. Try a different approach: " +
			"change the tool arguments, use a different tool, or re-evaluate your strategy."
	case SeverityCritical:
		return base + "\n\nYou are in a confirmed loop. You MUST change strategy immediately. " +
			"Do NOT repeat the same tool call. Either: (1) try an entirely different approach, " +
			"(2) report what is blocking you and signal EXIT_SIGNAL: true, or " +
			"(3) skip this subtask and move to the next one."
	default:
		return base
	}
}

// MarshalJSON implements json.Marshaler for Result (for OTEL attributes).
func (r Result) MarshalJSON() ([]byte, error) {
	return json.Marshal(struct {
		Severity string       `json:"severity"`
		Detector DetectorKind `json:"detector"`
		Message  string       `json:"message"`
	}{
		Severity: r.Severity.String(),
		Detector: r.Detector,
		Message:  r.Message,
	})
}
