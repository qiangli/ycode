package cli

import (
	"bufio"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"sync"
)

const (
	maxHistoryEntries = 100
	historyFileName   = "prompt-history.jsonl"
)

// promptHistory provides persistent, cross-session command history
// stored as a JSONL file in the user config directory.
type promptHistory struct {
	entries []string // submitted inputs (oldest first)
	index   int      // -1 = not browsing, 0..len-1 = current position
	draft   string   // saves current input when user starts browsing

	path string     // path to the JSONL file
	mu   sync.Mutex // guards file writes
}

// historyEntry is the JSON structure stored per line in the JSONL file.
type historyEntry struct {
	Input string `json:"input"`
}

// newPromptHistory creates a promptHistory that persists to the given directory.
// If dir is empty, history is in-memory only.
func newPromptHistory(dir string) *promptHistory {
	h := &promptHistory{
		entries: make([]string, 0, 64),
		index:   -1,
	}
	if dir != "" {
		h.path = filepath.Join(dir, historyFileName)
		h.load()
	}
	return h
}

// load reads history from the JSONL file, discarding corrupt lines.
func (h *promptHistory) load() {
	f, err := os.Open(h.path)
	if err != nil {
		return
	}
	defer f.Close()

	var entries []string
	scanner := bufio.NewScanner(f)
	// Allow long lines (up to 1MB).
	scanner.Buffer(make([]byte, 0, 64*1024), 1024*1024)
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" {
			continue
		}
		var e historyEntry
		if err := json.Unmarshal([]byte(line), &e); err != nil {
			continue // skip corrupt entries
		}
		if e.Input != "" {
			entries = append(entries, e.Input)
		}
	}

	// Keep only the most recent entries.
	if len(entries) > maxHistoryEntries {
		entries = entries[len(entries)-maxHistoryEntries:]
	}
	h.entries = entries

	// Self-heal: rewrite the file with only valid entries.
	if len(entries) > 0 {
		h.rewrite()
	}
}

// Append adds a new entry to history and persists it.
// Consecutive duplicates are suppressed.
func (h *promptHistory) Append(input string) {
	input = strings.TrimSpace(input)
	if input == "" {
		return
	}
	// Skip consecutive duplicates.
	if len(h.entries) > 0 && h.entries[len(h.entries)-1] == input {
		return
	}

	h.entries = append(h.entries, input)

	trimmed := false
	if len(h.entries) > maxHistoryEntries {
		h.entries = h.entries[len(h.entries)-maxHistoryEntries:]
		trimmed = true
	}

	// Reset navigation state.
	h.index = -1
	h.draft = ""

	if h.path == "" {
		return
	}

	h.mu.Lock()
	defer h.mu.Unlock()

	if trimmed {
		h.rewrite()
		return
	}

	// Append-only for normal case.
	data, _ := json.Marshal(historyEntry{Input: input})
	f, err := os.OpenFile(h.path, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o644)
	if err != nil {
		return
	}
	defer f.Close()
	f.Write(data)
	f.Write([]byte("\n"))
}

// Up moves backward in history. Returns the entry to display, or "" if no change.
// currentInput is the current textarea value (saved on first press).
func (h *promptHistory) Up(currentInput string) (string, bool) {
	if len(h.entries) == 0 {
		return "", false
	}
	if h.index == -1 {
		// Starting history navigation — save current input as draft.
		h.draft = currentInput
		h.index = len(h.entries) - 1
	} else if h.index > 0 {
		h.index--
	} else {
		return "", false // already at oldest
	}
	return h.entries[h.index], true
}

// Down moves forward in history. Returns the entry to display, or "" if no change.
func (h *promptHistory) Down() (string, bool) {
	if h.index == -1 {
		return "", false // not browsing history
	}
	if h.index < len(h.entries)-1 {
		h.index++
		return h.entries[h.index], true
	}
	// At end of history — restore draft.
	h.index = -1
	return h.draft, true
}

// Reset clears navigation state (call after submitting input).
func (h *promptHistory) Reset() {
	h.index = -1
	h.draft = ""
}

// rewrite replaces the file with the current entries. Caller must hold mu if path is set.
func (h *promptHistory) rewrite() {
	if h.path == "" {
		return
	}
	// Ensure parent directory exists.
	os.MkdirAll(filepath.Dir(h.path), 0o755)

	var buf strings.Builder
	for _, input := range h.entries {
		data, _ := json.Marshal(historyEntry{Input: input})
		buf.Write(data)
		buf.WriteByte('\n')
	}
	// Write atomically via temp file.
	tmp := h.path + ".tmp"
	if err := os.WriteFile(tmp, []byte(buf.String()), 0o644); err != nil {
		return
	}
	os.Rename(tmp, h.path)
}
