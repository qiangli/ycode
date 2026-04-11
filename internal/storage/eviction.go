package storage

import (
	"context"
	"log/slog"
	"time"
)

const (
	// DefaultEvictionInterval is how often expired prompt cache entries are cleaned up.
	DefaultEvictionInterval = 5 * time.Minute
)

// StartEviction runs a background goroutine that periodically removes expired
// prompt_cache rows from SQLite. It blocks until ctx is cancelled.
func (m *Manager) StartEviction(ctx context.Context) {
	// Wait for SQL backend to be ready.
	select {
	case <-m.sqlReady:
	case <-ctx.Done():
		return
	}

	m.mu.RLock()
	sqlStore := m.sql
	m.mu.RUnlock()
	if sqlStore == nil {
		return
	}

	ticker := time.NewTicker(DefaultEvictionInterval)
	defer ticker.Stop()

	// Run one eviction immediately.
	evictExpiredPromptCache(ctx, sqlStore)

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			evictExpiredPromptCache(ctx, sqlStore)
		}
	}
}

// evictExpiredPromptCache deletes rows from prompt_cache where expires_at < now.
func evictExpiredPromptCache(ctx context.Context, store SQLStore) {
	ctx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()

	result, err := store.Exec(ctx,
		`DELETE FROM prompt_cache WHERE expires_at < datetime('now')`,
	)
	if err != nil {
		slog.Debug("eviction: delete expired prompt cache", "error", err)
		return
	}

	if affected, err := result.RowsAffected(); err == nil && affected > 0 {
		slog.Debug("eviction: cleaned prompt cache", "deleted", affected)
	}
}
