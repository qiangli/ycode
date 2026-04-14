package session

import (
	"os"
	"path/filepath"
	"strings"
	"time"
)

// SearchCriteria defines filters for session search.
type SearchCriteria struct {
	// Text-based filters.
	Query     string // search in message text (case-insensitive)
	TitleLike string // search in session title (case-insensitive)

	// Time-based filters.
	After  time.Time // sessions created after this time
	Before time.Time // sessions created before this time

	// Pagination.
	Limit  int // max results (0 = no limit)
	Offset int // skip first N results
}

// SearchResult is a session matching the search criteria.
type SearchResult struct {
	ID           string    `json:"id"`
	Title        string    `json:"title,omitempty"`
	CreatedAt    time.Time `json:"created_at"`
	MessageCount int       `json:"message_count"`
	Dir          string    `json:"-"`
}

// Search scans the session root directory and returns sessions matching the criteria.
// It loads sessions lazily and filters them in a streaming fashion.
func Search(sessionsDir string, criteria SearchCriteria) ([]SearchResult, error) {
	entries, err := os.ReadDir(sessionsDir)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, err
	}

	var results []SearchResult
	skipped := 0

	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}
		sessionID := entry.Name()
		sessionDir := filepath.Join(sessionsDir, sessionID)

		// Quick check: does messages.jsonl exist?
		msgPath := filepath.Join(sessionDir, "messages.jsonl")
		info, err := os.Stat(msgPath)
		if err != nil {
			continue
		}

		// Time filter on file modification time (cheap check before loading).
		if !criteria.Before.IsZero() && info.ModTime().After(criteria.Before) {
			continue
		}

		// Load the session for deeper filtering.
		sess, err := Load(sessionsDir, sessionID)
		if err != nil {
			continue
		}

		// Apply time filters.
		if !criteria.After.IsZero() && sess.CreatedAt.Before(criteria.After) {
			continue
		}
		if !criteria.Before.IsZero() && sess.CreatedAt.After(criteria.Before) {
			continue
		}

		// Apply title filter.
		if criteria.TitleLike != "" {
			title := sess.Title
			if title == "" {
				title = sess.GenerateDefaultTitle()
			}
			if !containsInsensitive(title, criteria.TitleLike) {
				continue
			}
		}

		// Apply content query filter.
		if criteria.Query != "" {
			if !sessionContainsText(sess, criteria.Query) {
				continue
			}
		}

		// Apply offset.
		if skipped < criteria.Offset {
			skipped++
			continue
		}

		title := sess.Title
		if title == "" {
			title = sess.GenerateDefaultTitle()
		}

		results = append(results, SearchResult{
			ID:           sess.ID,
			Title:        title,
			CreatedAt:    sess.CreatedAt,
			MessageCount: len(sess.Messages),
			Dir:          sessionDir,
		})

		// Apply limit.
		if criteria.Limit > 0 && len(results) >= criteria.Limit {
			break
		}
	}

	return results, nil
}

// sessionContainsText checks if any message in the session contains the query.
func sessionContainsText(sess *Session, query string) bool {
	queryLower := strings.ToLower(query)
	for _, msg := range sess.Messages {
		for _, block := range msg.Content {
			if block.Type == ContentTypeText && strings.Contains(strings.ToLower(block.Text), queryLower) {
				return true
			}
		}
	}
	return false
}

// containsInsensitive checks if s contains substr (case-insensitive).
func containsInsensitive(s, substr string) bool {
	return strings.Contains(strings.ToLower(s), strings.ToLower(substr))
}
