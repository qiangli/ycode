package conversation

import (
	"github.com/qiangli/ycode/internal/bus"
)

// DetectorType identifies the loop detection strategy that triggered.
type DetectorType string

const (
	DetectorGenericRepeat  DetectorType = "generic_repeat"  // consecutive similar responses
	DetectorPingPong       DetectorType = "ping_pong"       // alternating identical tool calls
	DetectorCircuitBreaker DetectorType = "circuit_breaker" // total tool calls per turn exceeded
	DetectorRepetitiveTool DetectorType = "repetitive_tool" // same tool called too many times per turn
)

// repetitiveToolThreshold is the number of calls to the same discovery tool in
// one turn that triggers a warning. Only applies to search/discovery tools
// (ToolSearch, glob_search, grep_search) — not to action tools like bash or read_file.
const repetitiveToolThreshold = 3

// discoveryTools are tool names that indicate search/discovery behavior.
// Repeated calls to these suggest the agent is stuck in a search loop.
var discoveryTools = map[string]bool{
	"ToolSearch":  true,
	"glob_search": true,
	"grep_search": true,
}

// LoopEvent describes a loop detection event.
type LoopEvent struct {
	Detector         DetectorType
	Status           LoopStatus
	ConsecutiveCount int
	SessionID        string
}

// EnhancedLoopDetector extends LoopDetector with additional detection strategies
// and diagnostic event emission.
type EnhancedLoopDetector struct {
	*LoopDetector

	// Ping-pong detection: tracks recent tool call sequences.
	recentToolCalls []string
	maxToolTracked  int

	// Circuit breaker: total tool calls this turn.
	totalToolCalls    int
	circuitBreakerMax int

	// Repetitive tool detection: counts per-tool calls within a turn.
	toolCallCounts map[string]int

	// Event emission.
	emitter   *bus.DiagnosticEmitter
	sessionID string
}

// EnhancedLoopDetectorConfig configures the enhanced detector.
type EnhancedLoopDetectorConfig struct {
	CircuitBreakerMax int // max tool calls per turn (default 100)
	SessionID         string
	Emitter           *bus.DiagnosticEmitter // optional
}

// NewEnhancedLoopDetector creates a detector with all strategies enabled.
func NewEnhancedLoopDetector(cfg EnhancedLoopDetectorConfig) *EnhancedLoopDetector {
	cbMax := cfg.CircuitBreakerMax
	if cbMax <= 0 {
		cbMax = 100
	}
	return &EnhancedLoopDetector{
		LoopDetector:      NewLoopDetector(),
		maxToolTracked:    10,
		circuitBreakerMax: cbMax,
		toolCallCounts:    make(map[string]int),
		emitter:           cfg.Emitter,
		sessionID:         cfg.SessionID,
	}
}

// RecordResponse checks for response-level loops (delegates to base LoopDetector)
// and emits diagnostic events.
func (d *EnhancedLoopDetector) RecordResponse(response string) LoopStatus {
	status := d.LoopDetector.Record(response)
	if status != LoopNone && d.emitter != nil {
		count := d.consecutiveSimilarCount()
		d.emitter.EmitToolLoop(d.sessionID, string(DetectorGenericRepeat), count, status.String())
	}
	return status
}

// RecordToolCall records a tool invocation for ping-pong detection, circuit breaking,
// and repetitive single-tool detection. Returns the loop status.
func (d *EnhancedLoopDetector) RecordToolCall(toolName string) LoopStatus {
	d.totalToolCalls++
	d.toolCallCounts[toolName]++
	d.recentToolCalls = append(d.recentToolCalls, toolName)
	if len(d.recentToolCalls) > d.maxToolTracked {
		d.recentToolCalls = d.recentToolCalls[len(d.recentToolCalls)-d.maxToolTracked:]
	}

	// Check circuit breaker.
	if d.totalToolCalls >= d.circuitBreakerMax {
		if d.emitter != nil {
			d.emitter.EmitToolLoop(d.sessionID, string(DetectorCircuitBreaker), d.totalToolCalls, LoopBreak.String())
		}
		return LoopBreak
	}

	// Check repetitive discovery tool: same search/discovery tool called too
	// many times in one turn. Catches search spirals (ToolSearch×3, Glob×3)
	// that the ping-pong detector misses because they aren't strictly A-B-A-B.
	// Only applies to discovery tools — action tools (bash, read_file) are normal.
	if discoveryTools[toolName] {
		if count := d.toolCallCounts[toolName]; count >= repetitiveToolThreshold {
			if d.emitter != nil {
				d.emitter.EmitToolLoop(d.sessionID, string(DetectorRepetitiveTool), count, LoopWarning.String())
			}
			return LoopWarning
		}
	}

	// Check ping-pong: alternating A-B-A-B pattern.
	if status := d.detectPingPong(); status != LoopNone {
		return status
	}

	return LoopNone
}

// detectPingPong checks for alternating tool call patterns (A-B-A-B).
func (d *EnhancedLoopDetector) detectPingPong() LoopStatus {
	calls := d.recentToolCalls
	if len(calls) < 4 {
		return LoopNone
	}

	// Check last 4+ calls for A-B-A-B pattern.
	n := len(calls)
	a, b := calls[n-2], calls[n-1]
	if a == b {
		return LoopNone // not alternating
	}

	consecutive := 1
	for i := n - 3; i >= 0; i -= 2 {
		if i-1 < 0 {
			break
		}
		if calls[i] == b && calls[i-1] == a {
			consecutive++
		} else {
			break
		}
	}

	if consecutive >= 3 {
		if d.emitter != nil {
			d.emitter.EmitToolLoop(d.sessionID, string(DetectorPingPong), consecutive, LoopBreak.String())
		}
		return LoopBreak
	}
	if consecutive >= 2 {
		if d.emitter != nil {
			d.emitter.EmitToolLoop(d.sessionID, string(DetectorPingPong), consecutive, LoopWarning.String())
		}
		return LoopWarning
	}

	return LoopNone
}

// ResetTurn resets per-turn counters (call at start of each turn).
func (d *EnhancedLoopDetector) ResetTurn() {
	d.totalToolCalls = 0
	d.recentToolCalls = nil
	d.toolCallCounts = make(map[string]int)
}

// TotalToolCalls returns the total tool calls in the current turn.
func (d *EnhancedLoopDetector) TotalToolCalls() int {
	return d.totalToolCalls
}

// consecutiveSimilarCount returns the current consecutive similar response count.
func (d *EnhancedLoopDetector) consecutiveSimilarCount() int {
	responses := d.recentResponses
	if len(responses) < 2 {
		return 1
	}
	count := 1
	latest := responses[len(responses)-1]
	for i := len(responses) - 2; i >= 0; i-- {
		if isSimilar(latest, responses[i]) {
			count++
		} else {
			break
		}
	}
	return count
}
