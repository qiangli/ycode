package session

import (
	"context"
	"strings"
	"time"
)

const (
	// ExtractorTimeout is the maximum time for background memory extraction.
	ExtractorTimeout = 10 * time.Second

	// ExtractorMinTurnGap is the minimum number of turns between extractions
	// to avoid excessive extraction overhead.
	ExtractorMinTurnGap = 5

	// ExtractorMaxWindowSize is the number of recent messages to scan for
	// extractable knowledge.
	ExtractorMaxWindowSize = 20
)

// ExtractableMemory represents a piece of knowledge extracted from conversation.
type ExtractableMemory struct {
	Type        string // "user", "feedback", "project", "reference"
	Name        string
	Description string
	Content     string
}

// MemoryExtractFunc is the function signature for LLM-based memory extraction.
// The function should analyze the transcript and return extractable memories.
type MemoryExtractFunc func(ctx context.Context, transcript string) ([]ExtractableMemory, error)

// MemoryStoreFunc persists an extracted memory.
type MemoryStoreFunc func(ctx context.Context, memory ExtractableMemory) error

// MemoryExtractor runs background memory extraction after assistant turns.
// It scans recent conversation for knowledge the main agent didn't explicitly
// save, including user corrections, confirmed approaches, project decisions,
// and external references.
//
// Inspired by Claude Code's extractMemories.ts post-turn agent.
type MemoryExtractor struct {
	extractFn MemoryExtractFunc
	storeFn   MemoryStoreFunc
	lastTurn  int
}

// NewMemoryExtractor creates a new background memory extractor.
func NewMemoryExtractor(extractFn MemoryExtractFunc, storeFn MemoryStoreFunc) *MemoryExtractor {
	return &MemoryExtractor{
		extractFn: extractFn,
		storeFn:   storeFn,
	}
}

// MaybeExtract checks if extraction should run based on turn gap, and if so,
// runs extraction in the background. Returns immediately; extraction happens
// asynchronously.
func (me *MemoryExtractor) MaybeExtract(currentTurn int, messages []ConversationMessage) {
	if currentTurn-me.lastTurn < ExtractorMinTurnGap {
		return
	}
	me.lastTurn = currentTurn

	// Take a snapshot of recent messages.
	window := messages
	if len(window) > ExtractorMaxWindowSize {
		window = window[len(window)-ExtractorMaxWindowSize:]
	}

	transcript := formatTranscriptForExtraction(window)

	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), ExtractorTimeout)
		defer cancel()

		memories, err := me.extractFn(ctx, transcript)
		if err != nil {
			return // Silently fail — extraction is best-effort.
		}

		for _, m := range memories {
			_ = me.storeFn(ctx, m) // Best-effort store.
		}
	}()
}

// formatTranscriptForExtraction converts messages to a readable transcript
// for the extraction LLM. Focuses on user corrections and confirmations.
func formatTranscriptForExtraction(messages []ConversationMessage) string {
	var b strings.Builder

	for _, msg := range messages {
		switch msg.Role {
		case RoleUser:
			b.WriteString("User: ")
		case RoleAssistant:
			b.WriteString("Assistant: ")
		default:
			continue
		}

		for _, block := range msg.Content {
			switch block.Type {
			case ContentTypeText:
				text := block.Text
				if len(text) > 300 {
					text = text[:300] + "..."
				}
				b.WriteString(text)
			case ContentTypeToolUse:
				b.WriteString("[tool: " + block.Name + "]")
			case ContentTypeToolResult:
				if block.IsError {
					content := block.Content
					if len(content) > 100 {
						content = content[:100] + "..."
					}
					b.WriteString("[error: " + content + "]")
				}
			}
		}
		b.WriteByte('\n')
	}

	return b.String()
}

// HeuristicExtract performs simple heuristic-based extraction without an LLM.
// It looks for common patterns that indicate extractable knowledge:
//   - User corrections ("no", "don't", "stop", "instead")
//   - User confirmations ("yes", "exactly", "perfect", "correct")
//   - External references (URLs, tool names, project names)
//
// This can be used as a fallback when no LLM is available for extraction.
func HeuristicExtract(messages []ConversationMessage) []ExtractableMemory {
	var memories []ExtractableMemory

	correctionMarkers := []string{"no ", "don't ", "stop ", "instead ", "not that", "wrong"}
	confirmationMarkers := []string{"yes ", "exactly", "perfect", "correct", "that's right"}

	for _, msg := range messages {
		if msg.Role != RoleUser {
			continue
		}
		text := firstTextBlock(msg)
		if text == "" {
			continue
		}

		lowered := strings.ToLower(text)

		// Check for corrections.
		for _, marker := range correctionMarkers {
			if strings.Contains(lowered, marker) {
				memories = append(memories, ExtractableMemory{
					Type:        "feedback",
					Name:        "user_correction",
					Description: truncateSummary(text, 80),
					Content:     text,
				})
				break
			}
		}

		// Check for confirmations of non-obvious approaches.
		for _, marker := range confirmationMarkers {
			if strings.Contains(lowered, marker) && len(text) > 20 {
				memories = append(memories, ExtractableMemory{
					Type:        "feedback",
					Name:        "user_confirmation",
					Description: truncateSummary(text, 80),
					Content:     text,
				})
				break
			}
		}
	}

	return memories
}
