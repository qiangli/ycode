package session

import (
	"fmt"
	"sort"
	"strings"
)

const (
	// DuplicateReadWasteThreshold is the minimum token waste from duplicate
	// file reads before a diagnostic hint is generated.
	DuplicateReadWasteThreshold = 5_000

	// DiminishingReturnsContinuations is the number of consecutive continuations
	// with low token progress before signaling diminishing returns.
	DiminishingReturnsContinuations = 3

	// DiminishingReturnsMinDelta is the minimum token delta per continuation
	// to be considered "making progress."
	DiminishingReturnsMinDelta = 500
)

// DuplicateFileRead tracks a file that was read multiple times.
type DuplicateFileRead struct {
	Path       string
	ReadCount  int
	TokenWaste int // estimated tokens wasted by redundant reads
}

// DetectDuplicateFileReads scans messages for files that were read multiple
// times and calculates the token waste from redundant reads.
//
// Inspired by Claude Code's analyzeContext() which tracks duplicateFileReads.
func DetectDuplicateFileReads(messages []ConversationMessage) []DuplicateFileRead {
	type readInfo struct {
		count  int
		tokens int // tokens per read (from tool result)
	}

	fileReads := make(map[string]*readInfo)

	for i, msg := range messages {
		if msg.Role != RoleAssistant {
			continue
		}
		for _, block := range msg.Content {
			if block.Type != ContentTypeToolUse {
				continue
			}
			if block.Name != "read_file" && block.Name != "Read" {
				continue
			}
			// Extract path from input.
			path := extractPathFromJSON(string(block.Input))
			if path == "" {
				continue
			}

			// Find corresponding tool result.
			resultTokens := 0
			if i+1 < len(messages) {
				for _, rb := range messages[i+1].Content {
					if rb.Type == ContentTypeToolResult {
						resultTokens = EstimateTextTokens(rb.Content)
						break
					}
				}
			}

			info, ok := fileReads[path]
			if !ok {
				info = &readInfo{}
				fileReads[path] = info
			}
			info.count++
			if resultTokens > info.tokens {
				info.tokens = resultTokens
			}
		}
	}

	var duplicates []DuplicateFileRead
	for path, info := range fileReads {
		if info.count > 1 {
			waste := (info.count - 1) * info.tokens
			duplicates = append(duplicates, DuplicateFileRead{
				Path:       path,
				ReadCount:  info.count,
				TokenWaste: waste,
			})
		}
	}

	// Sort by waste descending.
	sort.Slice(duplicates, func(i, j int) bool {
		return duplicates[i].TokenWaste > duplicates[j].TokenWaste
	})

	return duplicates
}

// FormatDuplicateReadHint produces a diagnostic message about duplicate reads.
func FormatDuplicateReadHint(duplicates []DuplicateFileRead) string {
	if len(duplicates) == 0 {
		return ""
	}

	totalWaste := 0
	for _, d := range duplicates {
		totalWaste += d.TokenWaste
	}

	if totalWaste < DuplicateReadWasteThreshold {
		return ""
	}

	var b strings.Builder
	fmt.Fprintf(&b, "Context diagnostic: ~%dk tokens wasted on duplicate file reads:\n",
		totalWaste/1000)
	for _, d := range duplicates {
		if d.TokenWaste >= 1000 {
			fmt.Fprintf(&b, "  %s: read %d times (~%dk tokens wasted)\n",
				d.Path, d.ReadCount, d.TokenWaste/1000)
		}
	}
	return b.String()
}

// extractPathFromJSON extracts a file path from a JSON tool input string.
// Handles common patterns like {"path":"/a/b.go"} and {"file_path":"/a/b.go"}.
func extractPathFromJSON(input string) string {
	// Try extractFileCandidates first.
	candidates := extractFileCandidates(input)
	if len(candidates) > 0 {
		return candidates[0]
	}

	// Fallback: simple JSON path extraction for common patterns.
	for _, key := range []string{`"path":"`, `"file_path":"`, `"path": "`, `"file_path": "`} {
		idx := strings.Index(input, key)
		if idx < 0 {
			continue
		}
		start := idx + len(key)
		end := strings.Index(input[start:], `"`)
		if end < 0 {
			continue
		}
		return input[start : start+end]
	}
	return ""
}

// ContextAnalysis provides a comprehensive analysis of context usage.
// Inspired by Claude Code's analyzeContext().
type ContextAnalysis struct {
	ToolRequests    map[string]int // tool name -> token count
	ToolResults     map[string]int // tool name -> result token count
	HumanTokens     int
	AssistantTokens int
	TotalTokens     int
	DuplicateReads  []DuplicateFileRead
}

// AnalyzeContext performs a comprehensive analysis of context usage patterns.
func AnalyzeContext(messages []ConversationMessage) *ContextAnalysis {
	analysis := &ContextAnalysis{
		ToolRequests: make(map[string]int),
		ToolResults:  make(map[string]int),
	}

	for _, msg := range messages {
		msgTokens := EstimateMessageTokens(msg)
		analysis.TotalTokens += msgTokens

		switch msg.Role {
		case RoleUser:
			for _, block := range msg.Content {
				if block.Type == ContentTypeToolResult {
					tokens := EstimateTextTokens(block.Content)
					analysis.ToolResults[block.Name] += tokens
				} else {
					analysis.HumanTokens += EstimateTextTokens(block.Text)
				}
			}
		case RoleAssistant:
			for _, block := range msg.Content {
				if block.Type == ContentTypeToolUse {
					tokens := EstimateTextTokens(string(block.Input))
					analysis.ToolRequests[block.Name] += tokens
					analysis.AssistantTokens += tokens
				} else if block.Type == ContentTypeText {
					analysis.AssistantTokens += EstimateTextTokens(block.Text)
				}
			}
		}
	}

	analysis.DuplicateReads = DetectDuplicateFileReads(messages)

	return analysis
}

// TokenBudgetTracker monitors continuation progress and detects diminishing
// returns. When the agent is spinning without making meaningful progress,
// it signals to stop.
//
// Inspired by Claude Code's TokenBudgetTracker.
type TokenBudgetTracker struct {
	// ContinuationCount is the number of continuations so far.
	ContinuationCount int
	// LastDeltaTokens is the token delta from the last check.
	LastDeltaTokens int
	// LastGlobalTokens is the cumulative token count at last check.
	LastGlobalTokens int
	// LowDeltaStreak counts consecutive low-delta continuations.
	LowDeltaStreak int
}

// NewTokenBudgetTracker creates a new tracker.
func NewTokenBudgetTracker() *TokenBudgetTracker {
	return &TokenBudgetTracker{}
}

// Update records a new observation and returns true if the agent should
// stop due to diminishing returns.
func (t *TokenBudgetTracker) Update(currentTokens int) bool {
	t.ContinuationCount++
	delta := currentTokens - t.LastGlobalTokens
	t.LastDeltaTokens = delta
	t.LastGlobalTokens = currentTokens

	if delta < DiminishingReturnsMinDelta {
		t.LowDeltaStreak++
	} else {
		t.LowDeltaStreak = 0
	}

	return t.ContinuationCount >= DiminishingReturnsContinuations &&
		t.LowDeltaStreak >= DiminishingReturnsContinuations
}

// ShouldStop returns true if diminishing returns have been detected.
func (t *TokenBudgetTracker) ShouldStop() bool {
	return t.ContinuationCount >= DiminishingReturnsContinuations &&
		t.LowDeltaStreak >= DiminishingReturnsContinuations
}

// Reset clears the tracker state.
func (t *TokenBudgetTracker) Reset() {
	t.ContinuationCount = 0
	t.LastDeltaTokens = 0
	t.LastGlobalTokens = 0
	t.LowDeltaStreak = 0
}
