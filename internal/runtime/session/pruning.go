package session

import (
	"fmt"
)

const (
	// SoftTrimRatio is the fraction of CompactionThreshold at which soft trim activates.
	// At 60K estimated tokens, old tool results are truncated.
	SoftTrimRatio = 0.60

	// HardClearRatio is the fraction of CompactionThreshold at which hard clear activates.
	// At 80K estimated tokens, old tool results are replaced with placeholders.
	HardClearRatio = 0.80

	// SoftTrimHeadChars is the number of leading characters to keep in soft-trimmed results.
	SoftTrimHeadChars = 400

	// SoftTrimTailChars is the number of trailing characters to keep in soft-trimmed results.
	SoftTrimTailChars = 200

	// RecentMessagesProtected is the number of recent messages never pruned.
	RecentMessagesProtected = 6

	// hardClearPlaceholder replaces pruned tool results.
	hardClearPlaceholder = "[Tool output pruned to save context. Re-run the tool if needed.]"
)

// PruneResult describes what pruning did.
type PruneResult struct {
	SoftTrimmed  int
	HardCleared  int
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
func PruneMessages(messages []ConversationMessage) ([]ConversationMessage, *PruneResult) {
	totalTokens := estimateAllTokens(messages)

	softThreshold := int(float64(CompactionThreshold) * SoftTrimRatio)
	hardThreshold := int(float64(CompactionThreshold) * HardClearRatio)

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

// CheckContextHealth evaluates the context health of a message set.
func CheckContextHealth(messages []ConversationMessage) ContextHealth {
	tokens := estimateAllTokens(messages)
	ratio := float64(tokens) / float64(CompactionThreshold)

	var level ContextLevel
	switch {
	case ratio > 1.0:
		level = ContextOverflow
	case ratio >= HardClearRatio:
		level = ContextCritical
	case ratio >= SoftTrimRatio:
		level = ContextWarning
	default:
		level = ContextHealthy
	}

	return ContextHealth{
		EstimatedTokens: tokens,
		Threshold:       CompactionThreshold,
		Ratio:           ratio,
		Level:           level,
	}
}

// FormatContextHealth returns a human-readable context health string.
func (h ContextHealth) String() string {
	pct := int(h.Ratio * 100)
	return fmt.Sprintf("Context: %dk/%dk tokens (%d%%, %s)",
		h.EstimatedTokens/1000, h.Threshold/1000, pct, h.Level)
}

// NeedsPruning returns true if the context is at or above the soft trim threshold.
func (h ContextHealth) NeedsPruning() bool {
	return h.Level >= ContextWarning
}

// NeedsCompactionNow returns true if the context is at or above the compaction threshold.
func (h ContextHealth) NeedsCompactionNow() bool {
	return h.Level >= ContextOverflow
}
