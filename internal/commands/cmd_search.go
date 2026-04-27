package commands

import (
	"context"
	"fmt"
	"strings"

	"github.com/qiangli/ycode/internal/runtime/session"
)

// SearchSessions searches across session history using text matching.
// sessionsDir is the root directory containing session subdirectories.
// Returns matching sessions with their titles and message counts.
func SearchSessions(sessionsDir string, query string, maxResults int) ([]session.SearchResult, error) {
	if maxResults <= 0 {
		maxResults = 10
	}
	return session.Search(sessionsDir, session.SearchCriteria{
		Query: query,
		Limit: maxResults,
	})
}

// FormatSearchResults formats session search results for display.
func FormatSearchResults(query string, results []session.SearchResult) string {
	if len(results) == 0 {
		return fmt.Sprintf("No sessions found matching %q.", query)
	}
	var b strings.Builder
	fmt.Fprintf(&b, "Sessions matching %q:\n\n", query)
	for i, r := range results {
		created := r.CreatedAt.Format("2006-01-02 15:04")
		title := r.Title
		if title == "" {
			title = "(untitled)"
		}
		fmt.Fprintf(&b, "%d. %s — %s (%d messages)\n",
			i+1, title, created, r.MessageCount)
	}
	return b.String()
}

// RegisterSearchCommand registers the /search command into the given registry.
func RegisterSearchCommand(r *Registry, deps *RuntimeDeps) {
	r.Register(&Spec{
		Name:        "search",
		Description: "Search across session history",
		Usage:       "/search <query>",
		Category:    "session",
		Handler:     searchHandler(deps),
	})
}

func searchHandler(deps *RuntimeDeps) HandlerFunc {
	return func(ctx context.Context, args string) (string, error) {
		query := strings.TrimSpace(args)
		if query == "" {
			return "", fmt.Errorf("usage: /search <query>")
		}

		// Use the session directory from config if available.
		sessDir := ""
		if deps.Config != nil && deps.Config.SessionDir != "" {
			sessDir = deps.Config.SessionDir
		}
		if sessDir == "" {
			return fmt.Sprintf("Session directory not configured. Query: %q", query), nil
		}

		results, err := SearchSessions(sessDir, query, 10)
		if err != nil {
			return "", fmt.Errorf("search failed: %w", err)
		}
		return FormatSearchResults(query, results), nil
	}
}
