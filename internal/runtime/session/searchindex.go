package session

import (
	"context"
	"fmt"
	"log/slog"
	"strings"

	"github.com/qiangli/ycode/internal/storage"
)

const sessionIndexName = "sessions"

// SearchIndexer indexes session messages and compaction summaries into Bleve.
type SearchIndexer struct {
	index     storage.SearchIndex
	sessionID string
}

// NewSearchIndexer creates a Bleve indexer for session content.
func NewSearchIndexer(index storage.SearchIndex, sessionID string) *SearchIndexer {
	return &SearchIndexer{index: index, sessionID: sessionID}
}

// IndexCompaction indexes a compaction result's summary and compacted messages.
func (si *SearchIndexer) IndexCompaction(result *CompactionResult, compactedMessages []ConversationMessage) {
	ctx := context.Background()

	var docs []storage.Document

	// Index the compaction summary.
	if result.FormattedSummary != "" {
		docs = append(docs, storage.Document{
			ID:      fmt.Sprintf("%s/compaction-summary", si.sessionID),
			Content: result.FormattedSummary,
			Metadata: map[string]string{
				"session_id": si.sessionID,
				"type":       "compaction_summary",
			},
		})
	}

	// Index individual compacted messages.
	for _, msg := range compactedMessages {
		text := extractTextContent(msg)
		if text == "" {
			continue
		}

		docs = append(docs, storage.Document{
			ID:      fmt.Sprintf("%s/%s", si.sessionID, msg.UUID),
			Content: text,
			Metadata: map[string]string{
				"session_id": si.sessionID,
				"role":       string(msg.Role),
				"timestamp":  msg.Timestamp.UTC().String(),
				"type":       "message",
			},
		})
	}

	if len(docs) > 0 {
		if err := si.index.BatchIndex(ctx, sessionIndexName, docs); err != nil {
			slog.Debug("session search: index compaction", "error", err)
		}
	}
}

// IndexMessage indexes a single message for search.
func (si *SearchIndexer) IndexMessage(msg ConversationMessage) {
	text := extractTextContent(msg)
	if text == "" {
		return
	}

	ctx := context.Background()
	doc := storage.Document{
		ID:      fmt.Sprintf("%s/%s", si.sessionID, msg.UUID),
		Content: text,
		Metadata: map[string]string{
			"session_id": si.sessionID,
			"role":       string(msg.Role),
			"timestamp":  msg.Timestamp.UTC().String(),
			"type":       "message",
		},
	}
	if err := si.index.Index(ctx, sessionIndexName, doc); err != nil {
		slog.Debug("session search: index message", "error", err)
	}
}

// extractTextContent extracts text content from a conversation message.
func extractTextContent(msg ConversationMessage) string {
	var parts []string
	for _, block := range msg.Content {
		switch block.Type {
		case ContentTypeText:
			if text := strings.TrimSpace(block.Text); text != "" {
				parts = append(parts, text)
			}
		case ContentTypeToolUse:
			if block.Name != "" {
				parts = append(parts, "tool:"+block.Name)
			}
		case ContentTypeToolResult:
			if content := strings.TrimSpace(block.Content); content != "" {
				// Truncate large tool results.
				if len(content) > 1024 {
					content = content[:1024]
				}
				parts = append(parts, content)
			}
		}
	}
	return strings.Join(parts, " ")
}
