package tools

import (
	"crypto/sha256"
	"fmt"
	"sync"
)

// ToolMutability classifies tools by their side-effect profile.
type ToolMutability int

const (
	// ToolIdempotent tools are safe to repeat (read-only operations).
	ToolIdempotent ToolMutability = iota
	// ToolMutating tools have side effects and are dangerous to repeat blindly.
	ToolMutating
)

// toolMutabilityMap classifies known tools. Unknown tools default to ToolMutating.
var toolMutabilityMap = map[string]ToolMutability{
	// Read-only / idempotent tools.
	"read_file":           ToolIdempotent,
	"read_multiple_files": ToolIdempotent,
	"glob_search":         ToolIdempotent,
	"grep_search":         ToolIdempotent,
	"list_files":          ToolIdempotent,
	"list_directory":      ToolIdempotent,
	"get_file_info":       ToolIdempotent,
	"tree":                ToolIdempotent,
	"list_roots":          ToolIdempotent,
	"ToolSearch":          ToolIdempotent,
	"WebSearch":           ToolIdempotent,
	"WebFetch":            ToolIdempotent,
	"AskUserQuestion":     ToolIdempotent,
	"git_status":          ToolIdempotent,
	"git_log":             ToolIdempotent,
	"git_show":            ToolIdempotent,
	"git_diff":            ToolIdempotent,
	"git_branch":          ToolIdempotent,
	"memory_recall":       ToolIdempotent,
	"memory_list":         ToolIdempotent,
	"MemosSearch":         ToolIdempotent,
	"MemosList":           ToolIdempotent,
	"LSP":                 ToolIdempotent,
	"view_image":          ToolIdempotent,
	"view_diff":           ToolIdempotent,
	"query_metrics":       ToolIdempotent,
	"query_traces":        ToolIdempotent,
	"query_logs":          ToolIdempotent,
	"TaskGet":             ToolIdempotent,
	"TaskList":            ToolIdempotent,
	"TaskOutput":          ToolIdempotent,
	"ListPlan":            ToolIdempotent,
	"GetGoal":             ToolIdempotent,
	"Think":               ToolIdempotent,
	"AgentList":           ToolIdempotent,
	"CronList":            ToolIdempotent,
	"ListMcpResources":    ToolIdempotent,
	"ReadMcpResource":     ToolIdempotent,
}

// GetToolMutability returns the mutability classification for a tool.
func GetToolMutability(toolName string) ToolMutability {
	if m, ok := toolMutabilityMap[toolName]; ok {
		return m
	}
	return ToolMutating // default: assume mutating
}

// GuardrailConfig holds configurable thresholds for tool loop detection.
type GuardrailConfig struct {
	ExactFailWarn    int // warn after N identical failures (default 2)
	ExactFailBlock   int // block after N (default 5)
	SameToolFailWarn int // warn after N same-tool failures (default 3)
	SameToolFailHalt int // halt after N (default 8)
	NoProgressWarn   int // warn after N turns without mutation (default 2)
	NoProgressBlock  int // block after N (default 5)
}

// DefaultGuardrailConfig returns sensible defaults.
func DefaultGuardrailConfig() GuardrailConfig {
	return GuardrailConfig{
		ExactFailWarn:    2,
		ExactFailBlock:   5,
		SameToolFailWarn: 3,
		SameToolFailHalt: 8,
		NoProgressWarn:   2,
		NoProgressBlock:  5,
	}
}

// GuardrailAction describes the intervention to take after a guardrail check.
type GuardrailAction int

const (
	ActionNone  GuardrailAction = iota // no intervention
	ActionWarn                         // prepend warning to error
	ActionBlock                        // return intervention message as tool result
)

// GuardrailResult holds the action and message from a guardrail check.
type GuardrailResult struct {
	Action  GuardrailAction
	Message string
}

// MistakeTracker tracks per-tool failure patterns to detect spiraling loops.
type MistakeTracker struct {
	mu                   sync.Mutex
	config               GuardrailConfig
	consecutiveFailures  map[string]int // tool name → consecutive failure count
	exactFailures        map[string]int // sha256(toolName+input) → identical call count
	turnsWithoutProgress int
}

// NewMistakeTracker creates a mistake tracker with the given config.
func NewMistakeTracker(config GuardrailConfig) *MistakeTracker {
	return &MistakeTracker{
		config:              config,
		consecutiveFailures: make(map[string]int),
		exactFailures:       make(map[string]int),
	}
}

// RecordSuccess resets consecutive failure counters for the given tool.
func (mt *MistakeTracker) RecordSuccess(toolName string) {
	mt.mu.Lock()
	defer mt.mu.Unlock()
	mt.consecutiveFailures[toolName] = 0
}

// RecordFailure records a tool failure and returns the guardrail action.
func (mt *MistakeTracker) RecordFailure(toolName string, inputHash string, toolErr *ToolError) GuardrailResult {
	mt.mu.Lock()
	defer mt.mu.Unlock()

	// Track consecutive same-tool failures.
	mt.consecutiveFailures[toolName]++
	sameToolCount := mt.consecutiveFailures[toolName]

	// Track exact identical failures.
	exactKey := hashKey(toolName, inputHash)
	mt.exactFailures[exactKey]++
	exactCount := mt.exactFailures[exactKey]

	// Check exact-failure thresholds.
	if exactCount >= mt.config.ExactFailBlock {
		return GuardrailResult{
			Action: ActionBlock,
			Message: fmt.Sprintf("BLOCKED: Identical call to %s has failed %d times. "+
				"Stop retrying the same approach. Reconsider your strategy entirely.", toolName, exactCount),
		}
	}
	if exactCount >= mt.config.ExactFailWarn {
		return GuardrailResult{
			Action: ActionWarn,
			Message: fmt.Sprintf("WARNING: Identical call to %s has failed %d times. "+
				"Consider a different approach.", toolName, exactCount),
		}
	}

	// Check same-tool-failure thresholds.
	if sameToolCount >= mt.config.SameToolFailHalt {
		return GuardrailResult{
			Action: ActionBlock,
			Message: fmt.Sprintf("BLOCKED: %s has failed %d consecutive times. "+
				"Stop using this tool and try an alternative approach.", toolName, sameToolCount),
		}
	}
	if sameToolCount >= mt.config.SameToolFailWarn {
		hint := ""
		if toolErr != nil && toolErr.RecoveryHint != "" {
			hint = " " + toolErr.RecoveryHint
		}
		return GuardrailResult{
			Action:  ActionWarn,
			Message: fmt.Sprintf("WARNING: %s has failed %d consecutive times.%s", toolName, sameToolCount, hint),
		}
	}

	return GuardrailResult{Action: ActionNone}
}

// EndTurn updates the no-progress counter.
func (mt *MistakeTracker) EndTurn(hadSuccessfulMutation bool) GuardrailResult {
	mt.mu.Lock()
	defer mt.mu.Unlock()

	if hadSuccessfulMutation {
		mt.turnsWithoutProgress = 0
		return GuardrailResult{Action: ActionNone}
	}

	mt.turnsWithoutProgress++

	if mt.turnsWithoutProgress >= mt.config.NoProgressBlock {
		return GuardrailResult{
			Action: ActionBlock,
			Message: fmt.Sprintf("BLOCKED: No successful mutations in %d turns. "+
				"You may be stuck. Re-read the relevant files and reconsider your approach.", mt.turnsWithoutProgress),
		}
	}
	if mt.turnsWithoutProgress >= mt.config.NoProgressWarn {
		return GuardrailResult{
			Action:  ActionWarn,
			Message: fmt.Sprintf("WARNING: No successful mutations in %d turns. Consider changing approach.", mt.turnsWithoutProgress),
		}
	}

	return GuardrailResult{Action: ActionNone}
}

// Reset clears all tracking state.
func (mt *MistakeTracker) Reset() {
	mt.mu.Lock()
	defer mt.mu.Unlock()
	mt.consecutiveFailures = make(map[string]int)
	mt.exactFailures = make(map[string]int)
	mt.turnsWithoutProgress = 0
}

// ConsecutiveFailures returns the consecutive failure count for a tool.
func (mt *MistakeTracker) ConsecutiveFailures(toolName string) int {
	mt.mu.Lock()
	defer mt.mu.Unlock()
	return mt.consecutiveFailures[toolName]
}

// hashKey creates a deterministic hash for tool+input dedup.
func hashKey(toolName, inputHash string) string {
	h := sha256.Sum256([]byte(toolName + ":" + inputHash))
	return fmt.Sprintf("%x", h[:8])
}
