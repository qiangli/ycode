package session

import (
	"bufio"
	"context"
	"encoding/json"
	"log/slog"
	"os"
	"path/filepath"
	"time"

	"github.com/qiangli/ycode/internal/storage"
)

// Indexer scans existing JSONL session files and indexes their metadata into SQLite.
// It is designed to run once on first startup (or when new unindexed sessions are found).
type Indexer struct {
	store       storage.SQLStore
	sessionsDir string
}

// NewIndexer creates a session indexer.
func NewIndexer(store storage.SQLStore, sessionsDir string) *Indexer {
	return &Indexer{store: store, sessionsDir: sessionsDir}
}

// IndexAll scans the sessions directory and indexes any sessions not yet in SQLite.
func (idx *Indexer) IndexAll(ctx context.Context) (int, error) {
	entries, err := os.ReadDir(idx.sessionsDir)
	if err != nil {
		if os.IsNotExist(err) {
			return 0, nil
		}
		return 0, err
	}

	indexed := 0
	for _, entry := range entries {
		if ctx.Err() != nil {
			break
		}
		if !entry.IsDir() {
			continue
		}

		sessionID := entry.Name()

		// Check if already indexed.
		var count int
		row := idx.store.QueryRow(ctx, `SELECT COUNT(*) FROM sessions WHERE id = ?`, sessionID)
		if err := row.Scan(&count); err != nil {
			slog.Debug("indexer: check session", "id", sessionID, "error", err)
			continue
		}
		if count > 0 {
			continue
		}

		// Parse and index the session.
		if err := idx.indexSession(ctx, sessionID); err != nil {
			slog.Debug("indexer: index session", "id", sessionID, "error", err)
			continue
		}
		indexed++
	}

	return indexed, nil
}

// indexSession reads a single session's JSONL file and writes to SQLite.
func (idx *Indexer) indexSession(ctx context.Context, sessionID string) error {
	path := filepath.Join(idx.sessionsDir, sessionID, "messages.jsonl")
	f, err := os.Open(path)
	if err != nil {
		return err
	}
	defer f.Close()

	var messages []ConversationMessage
	scanner := bufio.NewScanner(f)
	scanner.Buffer(make([]byte, 0, 1024*1024), 1024*1024)
	for scanner.Scan() {
		var msg ConversationMessage
		if err := json.Unmarshal(scanner.Bytes(), &msg); err != nil {
			continue // skip malformed lines
		}
		messages = append(messages, msg)
	}
	if scanner.Err() != nil {
		return scanner.Err()
	}

	if len(messages) == 0 {
		return nil
	}

	// Extract session metadata from messages.
	var (
		model     string
		createdAt time.Time
		totalIn   int
		totalOut  int
	)
	createdAt = messages[0].Timestamp
	for _, msg := range messages {
		if msg.Model != "" {
			model = msg.Model
		}
		if msg.Usage != nil {
			totalIn += msg.Usage.InputTokens
			totalOut += msg.Usage.OutputTokens
		}
	}

	return idx.store.Tx(ctx, func(tx storage.SQLStore) error {
		// Insert session row.
		_, err := tx.Exec(ctx, `
			INSERT INTO sessions (id, model, created_at, updated_at, token_input, token_output)
			VALUES (?, ?, ?, ?, ?, ?)
		`, sessionID, model,
			createdAt.UTC().Format(time.RFC3339),
			messages[len(messages)-1].Timestamp.UTC().Format(time.RFC3339),
			totalIn, totalOut)
		if err != nil {
			return err
		}

		// Insert message rows.
		for _, msg := range messages {
			content, err := json.Marshal(msg.Content)
			if err != nil {
				continue
			}
			var tokenIn, tokenOut int
			if msg.Usage != nil {
				tokenIn = msg.Usage.InputTokens
				tokenOut = msg.Usage.OutputTokens
			}
			_, err = tx.Exec(ctx, `
				INSERT INTO messages (id, session_id, role, content, model, timestamp, token_input, token_output)
				VALUES (?, ?, ?, ?, ?, ?, ?, ?)
				ON CONFLICT(id) DO NOTHING
			`, msg.UUID, sessionID, string(msg.Role), string(content), msg.Model,
				msg.Timestamp.UTC().Format(time.RFC3339), tokenIn, tokenOut)
			if err != nil {
				return err
			}
		}

		return nil
	})
}
