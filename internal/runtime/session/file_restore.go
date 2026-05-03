package session

import (
	"fmt"
	"os"
	"strings"
)

const (
	// PostCompactMaxFilesToRestore is the maximum number of recently-edited files
	// to restore after compaction. Inspired by Claude Code's
	// POST_COMPACT_MAX_FILES_TO_RESTORE = 5.
	PostCompactMaxFilesToRestore = 5

	// PostCompactTokenBudget is the total token budget for restored file content.
	PostCompactTokenBudget = 50_000

	// PostCompactMaxTokensPerFile caps individual file restoration.
	PostCompactMaxTokensPerFile = 5_000
)

// FileTracker tracks recently-edited files for post-compaction restoration.
// It maintains an ordered list of files that were written or edited during
// the session, so their content can be re-injected after compaction.
type FileTracker struct {
	// files tracks file paths in order of last edit (most recent last).
	files []string
	// seen prevents duplicates; maps path to index in files.
	seen map[string]int
}

// NewFileTracker creates a new file tracker.
func NewFileTracker() *FileTracker {
	return &FileTracker{
		seen: make(map[string]int),
	}
}

// Track records a file as recently edited. If already tracked, moves it
// to the most-recent position.
func (ft *FileTracker) Track(path string) {
	if idx, ok := ft.seen[path]; ok {
		// Remove from current position.
		ft.files = append(ft.files[:idx], ft.files[idx+1:]...)
		// Rebuild seen map.
		ft.rebuildSeen()
	}
	ft.files = append(ft.files, path)
	ft.seen[path] = len(ft.files) - 1
}

// RecentFiles returns up to n most recently edited files (most recent first).
func (ft *FileTracker) RecentFiles(n int) []string {
	if n <= 0 || len(ft.files) == 0 {
		return nil
	}
	if n > len(ft.files) {
		n = len(ft.files)
	}
	// Return in reverse order (most recent first).
	result := make([]string, n)
	for i := 0; i < n; i++ {
		result[i] = ft.files[len(ft.files)-1-i]
	}
	return result
}

func (ft *FileTracker) rebuildSeen() {
	ft.seen = make(map[string]int, len(ft.files))
	for i, f := range ft.files {
		ft.seen[f] = i
	}
}

// RestoreRecentFiles reads recently-edited files and produces synthetic
// tool_result messages that can be injected after compaction. This ensures
// the agent has immediate access to files it was editing without re-reading.
//
// Returns messages and the total tokens consumed.
func RestoreRecentFiles(tracker *FileTracker) ([]ConversationMessage, int) {
	if tracker == nil {
		return nil, 0
	}

	recentPaths := tracker.RecentFiles(PostCompactMaxFilesToRestore)
	if len(recentPaths) == 0 {
		return nil, 0
	}

	var messages []ConversationMessage
	totalTokens := 0

	for _, path := range recentPaths {
		if totalTokens >= PostCompactTokenBudget {
			break
		}

		content, err := os.ReadFile(path)
		if err != nil {
			continue // File may have been deleted.
		}

		text := string(content)
		tokens := EstimateTextTokens(text)

		// Truncate if needed.
		if tokens > PostCompactMaxTokensPerFile {
			text = truncateToTokenBudget(text, PostCompactMaxTokensPerFile)
			tokens = PostCompactMaxTokensPerFile
		}

		if totalTokens+tokens > PostCompactTokenBudget {
			remaining := PostCompactTokenBudget - totalTokens
			text = truncateToTokenBudget(text, remaining)
			tokens = remaining
		}

		msg := ConversationMessage{
			Role: RoleUser,
			Content: []ContentBlock{
				{
					Type:    ContentTypeToolResult,
					Name:    "read_file",
					Content: fmt.Sprintf("[Post-compaction file restore: %s]\n%s", path, text),
				},
			},
		}
		messages = append(messages, msg)
		totalTokens += tokens
	}

	return messages, totalTokens
}

// truncateToTokenBudget truncates text to fit within a token budget.
// Keeps the head and tail with an omission marker.
func truncateToTokenBudget(text string, maxTokens int) string {
	estimated := EstimateTextTokens(text)
	if estimated <= maxTokens {
		return text
	}

	// Approximate character budget from token budget.
	charBudget := maxTokens * 4

	if len(text) <= charBudget {
		return text
	}

	headChars := charBudget * 2 / 3
	tailChars := charBudget / 3

	head := text[:headChars]
	tail := text[len(text)-tailChars:]
	omitted := len(text) - headChars - tailChars

	return fmt.Sprintf("%s\n[... %d characters omitted for token budget ...]\n%s", head, omitted, tail)
}

// extractPathFromToolInput extracts a file path from tool input JSON.
func extractPathFromToolInput(input string) string {
	// Try heuristic extraction first (handles whitespace-separated paths).
	candidates := extractFileCandidates(input)
	for _, c := range candidates {
		if strings.HasPrefix(c, "/") {
			return c
		}
	}
	// Fallback: simple JSON key extraction.
	for _, key := range []string{`"path":"`, `"file_path":"`, `"path": "`, `"file_path": "`} {
		idx := strings.Index(input, key)
		if idx < 0 {
			continue
		}
		start := idx + len(key)
		end := strings.Index(input[start:], `"`)
		if end > 0 {
			return input[start : start+end]
		}
	}
	return ""
}

// TrackEditedFiles scans messages for file edit/write tool calls and
// records them in the tracker. Used to populate the tracker from
// conversation history.
func TrackEditedFiles(tracker *FileTracker, messages []ConversationMessage) {
	if tracker == nil {
		return
	}

	writeTools := map[string]bool{
		"write_file": true,
		"edit_file":  true,
		"Edit":       true,
		"Write":      true,
	}

	for _, msg := range messages {
		if msg.Role != RoleAssistant {
			continue
		}
		for _, block := range msg.Content {
			if block.Type != ContentTypeToolUse || !writeTools[block.Name] {
				continue
			}
			path := extractPathFromToolInput(string(block.Input))
			if path != "" && strings.HasPrefix(path, "/") {
				tracker.Track(path)
			}
		}
	}
}
