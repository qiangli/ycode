package session

import (
	"fmt"
	"strings"
)

const (
	// ObservationMaskingWindow is the number of recent tool results that remain unmasked.
	// Inspired by OpenHands' ObservationMaskingCondenser (default attention_window=100).
	// For a CLI agent with smaller conversations, 10 is appropriate.
	ObservationMaskingWindow = 10

	// ObservationMaskingWindowAggressive is used for non-caching providers where
	// every input token costs full price. A smaller window masks more old results.
	ObservationMaskingWindowAggressive = 6

	// ToolMaskingProtectionBudget is the default token budget for protecting recent
	// tool outputs from masking. Newest outputs totaling up to this budget are safe.
	ToolMaskingProtectionBudget = 50_000

	// ToolMaskingMinPrunable is the minimum prunable tokens before masking triggers.
	// Avoids overhead for small savings. Effectively masking starts at ~80K total
	// tool output tokens (50K protected + 30K prunable).
	ToolMaskingMinPrunable = 30_000

	// maskedPlaceholder replaces old tool results in observation masking (Layer 0).
	//
	// It is a PREFIX, not the whole string: maskedFor appends the tool's name and a
	// recovery instruction. A bare "<MASKED>" tells the model that something it once
	// saw is gone, but not what it was or how to get it back — which leaves it to
	// guess, and guessing is what costs turns.
	maskedPlaceholder = "<MASKED>"

	// SoftTrimRatio is the fraction of CompactionThreshold at which soft trim activates.
	// At 60K estimated tokens, old tool results are truncated.
	SoftTrimRatio = 0.60

	// HardClearRatio is the fraction of CompactionThreshold at which hard clear activates.
	// At 80K estimated tokens, old tool results are replaced with placeholders.
	HardClearRatio = 0.80

	// SoftTrimTotalChars is the total character budget for soft-trimmed results.
	SoftTrimTotalChars = 600

	// SoftTrimHeadRatio is the fraction of the budget kept from the start.
	// 15% head preserves headers/context; 85% tail preserves error messages/results.
	// Inspired by Gemini CLI's normalizationHeadRatio.
	SoftTrimHeadRatio = 0.15

	// SoftTrimHeadChars is the number of leading characters to keep in soft-trimmed results.
	SoftTrimHeadChars = int(SoftTrimTotalChars * SoftTrimHeadRatio) // 90

	// SoftTrimTailChars is the number of trailing characters to keep in soft-trimmed results.
	SoftTrimTailChars = SoftTrimTotalChars - SoftTrimHeadChars // 510

	// RecentMessagesProtected is the number of recent messages never pruned.
	RecentMessagesProtected = 6

	// hardClearPlaceholder replaces pruned tool results.
	hardClearPlaceholder = "[Tool output pruned to save context. Re-run the tool if needed.]"
)

// PruneResult describes what pruning did.
type PruneResult struct {
	SoftTrimmed  int
	HardCleared  int
	Masked       int // Layer 0: observation masking count
	TokensBefore int
	TokensAfter  int
}

// PruneMessages applies in-memory context pruning to reduce token pressure
// before compaction is needed. It returns a new slice (original is not modified).
//
// Layer 1 defense (OpenClaw pattern):
//   - Soft trim: truncate old tool results keeping head + tail
//   - Hard clear: replace old tool results with placeholder
//
// Recent messages (last RecentMessagesProtected) are never pruned.
//
// The budget is the MODEL'S, not a global constant. That distinction is the whole
// point of the parameter: see the comment on ContextHealth.Threshold.
func PruneMessages(messages []ConversationMessage, budget ContextBudget) ([]ConversationMessage, *PruneResult) {
	totalTokens := estimateAllTokens(messages)

	softThreshold := budget.SoftTrimAt()
	hardThreshold := budget.HardClearAt()

	if totalTokens < softThreshold {
		return messages, nil // No pruning needed.
	}

	// Deep copy messages so we don't modify the originals.
	pruned := deepCopyMessages(messages)
	result := &PruneResult{TokensBefore: totalTokens}

	// Determine the protected boundary.
	protectedFrom := max(len(pruned)-RecentMessagesProtected, 0)

	needHardClear := totalTokens >= hardThreshold

	for i := range protectedFrom {
		msg := &pruned[i]
		for j := range msg.Content {
			block := &msg.Content[j]
			if block.Type != ContentTypeToolResult {
				continue
			}

			content := block.Content
			if len(content) < 100 {
				continue // Don't prune tiny results.
			}

			if needHardClear && len(content) > SoftTrimHeadChars {
				block.Content = hardClearPlaceholder
				result.HardCleared++
			} else if len(content) > SoftTrimHeadChars+SoftTrimTailChars+20 {
				block.Content = softTrim(content)
				result.SoftTrimmed++
			}
		}
	}

	result.TokensAfter = estimateAllTokens(pruned)
	return pruned, result
}

// softTrim keeps the head and tail of a string with an omission marker.
func softTrim(content string) string {
	runes := []rune(content)
	if len(runes) <= SoftTrimHeadChars+SoftTrimTailChars+20 {
		return content
	}

	head := string(runes[:SoftTrimHeadChars])
	tail := string(runes[len(runes)-SoftTrimTailChars:])
	omitted := len(runes) - SoftTrimHeadChars - SoftTrimTailChars

	return fmt.Sprintf("%s\n\n[... %d characters omitted ...]\n\n%s", head, omitted, tail)
}

// estimateAllTokens returns the total estimated tokens across all messages.
func estimateAllTokens(messages []ConversationMessage) int {
	total := 0
	for _, m := range messages {
		total += EstimateMessageTokens(m)
	}
	return total
}

// deepCopyMessages creates a deep copy of a message slice.
func deepCopyMessages(messages []ConversationMessage) []ConversationMessage {
	result := make([]ConversationMessage, len(messages))
	for i, msg := range messages {
		result[i] = ConversationMessage{
			UUID:      msg.UUID,
			Role:      msg.Role,
			Timestamp: msg.Timestamp,
			Model:     msg.Model,
			Usage:     msg.Usage,
		}
		result[i].Content = make([]ContentBlock, len(msg.Content))
		copy(result[i].Content, msg.Content)
	}
	return result
}

// ContextHealth reports the current context usage state.
type ContextHealth struct {
	EstimatedTokens int
	Threshold       int
	Ratio           float64
	Level           ContextLevel
}

// ContextLevel indicates how full the context is.
type ContextLevel int

const (
	ContextHealthy  ContextLevel = iota // < 60%
	ContextWarning                      // 60-80%
	ContextCritical                     // 80-100%
	ContextOverflow                     // > 100%
)

// String returns a human-readable level name.
func (l ContextLevel) String() string {
	switch l {
	case ContextHealthy:
		return "healthy"
	case ContextWarning:
		return "warning"
	case ContextCritical:
		return "critical"
	case ContextOverflow:
		return "overflow"
	default:
		return "unknown"
	}
}

// FormatContextHealth returns a human-readable context health string.
func (h ContextHealth) String() string {
	pct := int(h.Ratio * 100)
	return fmt.Sprintf("Context: %dk/%dk tokens (%d%%, %s)",
		h.EstimatedTokens/1000, h.Threshold/1000, pct, h.Level)
}

// maskedFor renders the placeholder that replaces an old tool observation, naming
// the tool and saying plainly that the result can be obtained again.
//
// The model is not being lied to here: the observation IS gone, and it needs to know
// both that fact and what to do about it. Dropping content silently is what turns one
// masked read into six speculative ones.
func maskedFor(toolName string) string {
	if toolName == "" {
		return maskedPlaceholder + " (an older tool result was dropped to free context; re-run the tool if you still need it)"
	}
	return maskedPlaceholder + " (the older `" + toolName + "` result was dropped to free context; re-run `" + toolName + "` if you still need it)"
}

// isMasked reports whether a tool result has already been masked.
func isMasked(content string) bool {
	return strings.HasPrefix(content, maskedPlaceholder)
}

// ExemptFromMasking is the set of tool names whose outputs are always high-signal
// and should never be masked, regardless of position in conversation history.
// Inspired by Gemini CLI's EXEMPT_TOOLS.
var ExemptFromMasking = map[string]bool{
	"AskUserQuestion": true,
	"MemosStore":      true,
	"MemosSearch":     true,
	"MemosList":       true,
	"EnterPlanMode":   true,
	"ExitPlanMode":    true,
	"Skill":           true,
	"query_metrics":   true,
}
