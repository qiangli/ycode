package session

import (
	"fmt"
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

// MaskOldObservationsBudget is an enhanced masking approach that uses token budgets
// instead of simple count-based windows. It implements a "Hybrid Backward Scanned FIFO":
//  1. Protection window: newest tool outputs totaling up to protectionBudget tokens are safe
//  2. Batch threshold: only mask if total prunable tokens > minPrunable
//  3. Exempt tools: outputs from certain tools are never masked
//
// Returns a new slice and the count of masked results.
func MaskOldObservationsBudget(messages []ConversationMessage, protectionBudget, minPrunable int) ([]ConversationMessage, int) {
	if protectionBudget <= 0 {
		protectionBudget = ToolMaskingProtectionBudget
	}
	if minPrunable <= 0 {
		minPrunable = ToolMaskingMinPrunable
	}

	// Phase 1: Backward scan to identify protected vs prunable tool results.
	type toolResultLoc struct {
		msgIdx   int
		blockIdx int
		tokens   int
		exempt   bool
	}
	var locs []toolResultLoc

	for i := len(messages) - 1; i >= 0; i-- {
		for j := len(messages[i].Content) - 1; j >= 0; j-- {
			block := messages[i].Content[j]
			if block.Type != ContentTypeToolResult {
				continue
			}
			tokens := EstimateTextTokens(block.Content)
			exempt := ExemptFromMasking[block.Name]
			locs = append(locs, toolResultLoc{i, j, tokens, exempt})
		}
	}

	// Phase 2: Classify as protected or prunable (backwards = newest first).
	protectedTokens := 0
	prunableTokens := 0
	isPrunable := make(map[int]map[int]bool) // msgIdx → blockIdx → prunable

	for _, loc := range locs {
		if loc.exempt {
			continue // Never prunable.
		}
		if protectedTokens+loc.tokens <= protectionBudget {
			protectedTokens += loc.tokens
		} else {
			prunableTokens += loc.tokens
			if isPrunable[loc.msgIdx] == nil {
				isPrunable[loc.msgIdx] = make(map[int]bool)
			}
			isPrunable[loc.msgIdx][loc.blockIdx] = true
		}
	}

	// Phase 3: Only mask if prunable tokens exceed batch threshold.
	if prunableTokens < minPrunable {
		return messages, 0
	}

	// Phase 4: Apply masking.
	masked := deepCopyMessages(messages)
	maskedCount := 0
	for msgIdx, blocks := range isPrunable {
		for blockIdx := range blocks {
			block := &masked[msgIdx].Content[blockIdx]
			if block.Content != maskedPlaceholder && len(block.Content) > len(maskedPlaceholder) {
				block.Content = maskedPlaceholder
				maskedCount++
			}
		}
	}

	return masked, maskedCount
}

// MaskOldObservations replaces old tool result content with a short placeholder.
// This is Layer 0 — the lightest possible compaction, inspired by OpenHands'
// ObservationMaskingCondenser. Only tool results outside the attention window
// are masked. Returns a new slice (original not modified).
// The window parameter controls how many recent tool results remain unmasked.
// Use ObservationMaskingWindow (10) for caching providers or
// ObservationMaskingWindowAggressive (6) for non-caching providers.
func MaskOldObservations(messages []ConversationMessage, window int) ([]ConversationMessage, int) {
	if window <= 0 {
		window = ObservationMaskingWindow
	}

	// Count tool results from the end to find the masking boundary.
	toolResultCount := 0
	for i := len(messages) - 1; i >= 0; i-- {
		for _, b := range messages[i].Content {
			if b.Type == ContentTypeToolResult {
				toolResultCount++
			}
		}
	}

	if toolResultCount <= window {
		return messages, 0 // Nothing to mask.
	}

	masked := deepCopyMessages(messages)
	maskedCount := 0
	seenFromEnd := 0

	// Walk backwards, protecting the last N tool results within the window.
	for i := len(masked) - 1; i >= 0; i-- {
		for j := len(masked[i].Content) - 1; j >= 0; j-- {
			if masked[i].Content[j].Type != ContentTypeToolResult {
				continue
			}
			seenFromEnd++
			if seenFromEnd > window {
				// This tool result is outside the window — mask it.
				if masked[i].Content[j].Content != maskedPlaceholder && len(masked[i].Content[j].Content) > len(maskedPlaceholder) {
					masked[i].Content[j].Content = maskedPlaceholder
					maskedCount++
				}
			}
		}
	}

	return masked, maskedCount
}
