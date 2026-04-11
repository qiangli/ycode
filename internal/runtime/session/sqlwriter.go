package session

import (
	"context"
	"encoding/json"
	"log/slog"
	"time"

	"github.com/qiangli/ycode/internal/storage"
)

// SQLWriter writes session metadata and messages to a SQLite store.
// It runs as a best-effort secondary persistence layer alongside JSONL.
type SQLWriter struct {
	store     storage.SQLStore
	sessionID string
}

// NewSQLWriter creates a writer that persists session data to SQLite.
func NewSQLWriter(store storage.SQLStore, sessionID string) *SQLWriter {
	return &SQLWriter{store: store, sessionID: sessionID}
}

// EnsureSession creates or updates the session row in SQLite.
func (w *SQLWriter) EnsureSession(model string) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	_, err := w.store.Exec(ctx, `
		INSERT INTO sessions (id, model, created_at, updated_at)
		VALUES (?, ?, datetime('now'), datetime('now'))
		ON CONFLICT(id) DO UPDATE SET
			model = excluded.model,
			updated_at = datetime('now')
	`, w.sessionID, model)
	if err != nil {
		slog.Debug("sqlwriter: ensure session", "error", err)
	}
}

// WriteMessage persists a conversation message to the messages table.
func (w *SQLWriter) WriteMessage(msg ConversationMessage) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	// Serialize content blocks to JSON for the content column.
	content, err := json.Marshal(msg.Content)
	if err != nil {
		slog.Debug("sqlwriter: marshal content", "error", err)
		return
	}

	var tokenIn, tokenOut int
	if msg.Usage != nil {
		tokenIn = msg.Usage.InputTokens
		tokenOut = msg.Usage.OutputTokens
	}

	_, err = w.store.Exec(ctx, `
		INSERT INTO messages (id, session_id, role, content, model, timestamp, token_input, token_output)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?)
		ON CONFLICT(id) DO NOTHING
	`, msg.UUID, w.sessionID, string(msg.Role), string(content), msg.Model,
		msg.Timestamp.UTC().Format(time.RFC3339), tokenIn, tokenOut)
	if err != nil {
		slog.Debug("sqlwriter: write message", "error", err)
	}

	// Update session token totals and timestamp.
	_, err = w.store.Exec(ctx, `
		UPDATE sessions SET
			token_input = token_input + ?,
			token_output = token_output + ?,
			updated_at = datetime('now')
		WHERE id = ?
	`, tokenIn, tokenOut, w.sessionID)
	if err != nil {
		slog.Debug("sqlwriter: update session tokens", "error", err)
	}
}
