package session

import (
	"bufio"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/google/uuid"
)

const (
	// MaxSessionFileSize is the rotation threshold.
	MaxSessionFileSize = 256 * 1024 // 256 KB
	// MaxRotatedFiles is the number of rotated files to keep.
	MaxRotatedFiles = 3
)

// Session manages a conversation's persisted state.
type Session struct {
	ID        string                `json:"id"`
	CreatedAt time.Time             `json:"created_at"`
	Messages  []ConversationMessage `json:"messages"`
	Dir       string                `json:"-"` // session directory
	Summary   string                `json:"summary,omitempty"`
	Title     string                `json:"title,omitempty"` // human-readable session title

	sqlWriter     *SQLWriter     `json:"-"` // optional SQLite dual-writer
	searchIndexer *SearchIndexer `json:"-"` // optional Bleve indexer
}

// New creates a new session with a generated UUID.
func New(dir string) (*Session, error) {
	return NewWithID(dir, uuid.New().String())
}

// NewWithID creates a new session with the given ID.
// The session directory is created at dir/id/.
func NewWithID(dir string, id string) (*Session, error) {
	sessionDir := filepath.Join(dir, id)
	if err := os.MkdirAll(sessionDir, 0o755); err != nil {
		return nil, fmt.Errorf("create session dir: %w", err)
	}
	return &Session{
		ID:        id,
		CreatedAt: time.Now(),
		Dir:       sessionDir,
	}, nil
}

// Load reads a session from its JSONL file.
func Load(dir string, id string) (*Session, error) {
	sessionDir := filepath.Join(dir, id)
	path := filepath.Join(sessionDir, "messages.jsonl")

	f, err := os.Open(path)
	if err != nil {
		return nil, fmt.Errorf("open session: %w", err)
	}
	defer f.Close()

	s := &Session{
		ID:  id,
		Dir: sessionDir,
	}

	scanner := bufio.NewScanner(f)
	scanner.Buffer(make([]byte, 0, 1024*1024), 1024*1024) // 1MB max line
	for scanner.Scan() {
		var msg ConversationMessage
		if err := json.Unmarshal(scanner.Bytes(), &msg); err != nil {
			return nil, fmt.Errorf("parse message: %w", err)
		}
		s.Messages = append(s.Messages, msg)
	}
	if err := scanner.Err(); err != nil {
		return nil, fmt.Errorf("read session: %w", err)
	}

	if len(s.Messages) > 0 {
		s.CreatedAt = s.Messages[0].Timestamp
	}

	return s, nil
}

// AddMessage appends a message to the session and persists it.
func (s *Session) AddMessage(msg ConversationMessage) error {
	if msg.UUID == "" {
		msg.UUID = uuid.New().String()
	}
	if msg.Timestamp.IsZero() {
		msg.Timestamp = time.Now()
	}

	s.Messages = append(s.Messages, msg)

	if err := s.appendToFile(msg); err != nil {
		return err
	}

	// Best-effort dual-write to SQLite.
	if s.sqlWriter != nil {
		s.sqlWriter.WriteMessage(msg)
	}
	return nil
}

// appendToFile writes a single message as a JSONL line.
func (s *Session) appendToFile(msg ConversationMessage) error {
	path := filepath.Join(s.Dir, "messages.jsonl")

	// Check if rotation is needed.
	if info, err := os.Stat(path); err == nil && info.Size() >= MaxSessionFileSize {
		if err := s.rotate(path); err != nil {
			return fmt.Errorf("rotate session: %w", err)
		}
	}

	f, err := os.OpenFile(path, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o644)
	if err != nil {
		return fmt.Errorf("open session file: %w", err)
	}
	defer f.Close()

	data, err := json.Marshal(msg)
	if err != nil {
		return fmt.Errorf("marshal message: %w", err)
	}
	data = append(data, '\n')

	_, err = f.Write(data)
	return err
}

// rotate moves the current file and keeps MaxRotatedFiles backups.
func (s *Session) rotate(path string) error {
	// Remove the oldest rotated file.
	oldest := fmt.Sprintf("%s.%d", path, MaxRotatedFiles)
	os.Remove(oldest)

	// Shift existing rotated files.
	for i := MaxRotatedFiles - 1; i >= 1; i-- {
		from := fmt.Sprintf("%s.%d", path, i)
		to := fmt.Sprintf("%s.%d", path, i+1)
		os.Rename(from, to)
	}

	// Rotate current file.
	return os.Rename(path, fmt.Sprintf("%s.1", path))
}

// SetSQLWriter attaches a SQLite dual-writer for session persistence.
// The writer is best-effort: JSONL remains the primary persistence layer.
func (s *Session) SetSQLWriter(w *SQLWriter) {
	s.sqlWriter = w
}

// SetSearchIndexer attaches a Bleve indexer for session search.
func (s *Session) SetSearchIndexer(idx *SearchIndexer) {
	s.searchIndexer = idx
}

// SearchIndexer returns the attached search indexer, if any.
func (s *Session) SearchIndexer() *SearchIndexer {
	return s.searchIndexer
}

// ToolOutputDir returns the directory for saving full tool outputs.
// The directory is created lazily under the session directory.
func (s *Session) ToolOutputDir() string {
	return filepath.Join(s.Dir, "tool-output")
}

// MessageCount returns the number of messages in the session.
func (s *Session) MessageCount() int {
	return len(s.Messages)
}

// RemoveLastTurn removes the last assistant turn from the session.
// It removes messages from the end until it hits (and removes) a user message
// with actual text content (not tool results), effectively undoing the last
// exchange. Returns the number of messages removed.
func (s *Session) RemoveLastTurn() int {
	if len(s.Messages) == 0 {
		return 0
	}

	removed := 0
	for len(s.Messages) > 0 {
		last := s.Messages[len(s.Messages)-1]
		s.Messages = s.Messages[:len(s.Messages)-1]
		removed++
		if last.Role == RoleUser && hasTextContent(last) {
			break
		}
	}
	return removed
}

// hasTextContent returns true if the message contains a text content block
// (as opposed to only tool_result blocks).
func hasTextContent(msg ConversationMessage) bool {
	for _, block := range msg.Content {
		if block.Type == ContentTypeText && block.Text != "" {
			return true
		}
	}
	return false
}

// SetTitle sets the session title.
func (s *Session) SetTitle(title string) {
	s.Title = title
}

// GenerateDefaultTitle creates a title from the first user message.
// Returns the generated title or empty if no user messages exist.
func (s *Session) GenerateDefaultTitle() string {
	for _, msg := range s.Messages {
		if msg.Role == RoleUser && hasTextContent(msg) {
			for _, block := range msg.Content {
				if block.Type == ContentTypeText && block.Text != "" {
					title := block.Text
					// Truncate to 50 chars.
					if len(title) > 50 {
						title = title[:47] + "..."
					}
					// Strip newlines.
					for i, c := range title {
						if c == '\n' || c == '\r' {
							title = title[:i]
							break
						}
					}
					s.Title = title
					return title
				}
			}
		}
	}
	return ""
}

// TitlePrompt is the prompt sent to a cheap model for title generation.
const TitlePrompt = "Generate a concise 3-6 word title for this conversation. Reply with ONLY the title, no quotes, no punctuation at the end. Focus on the main topic or task."

// GenerateLLMTitle creates a title from the first few messages using an LLM.
// firstMessages should be the first 1-3 messages of the conversation.
// Returns the generated title or falls back to GenerateDefaultTitle on error.
func GenerateLLMTitle(firstMessages []string) string {
	// Build a brief context from messages for title generation.
	var context string
	for i, msg := range firstMessages {
		if i >= 3 {
			break
		}
		if len(msg) > 200 {
			msg = msg[:200] + "..."
		}
		context += msg + "\n"
	}
	if context == "" {
		return "New conversation"
	}
	// This returns the formatted prompt that callers send to a cheap model.
	// The actual LLM call happens in the caller since we don't have provider access here.
	return strings.TrimSpace(context)
}

// FormatTitlePrompt returns the full prompt for LLM title generation.
func FormatTitlePrompt(conversationContext string) string {
	return TitlePrompt + "\n\nConversation:\n" + conversationContext
}

// LastUserMessage returns the text of the most recent user message, or empty.
func (s *Session) LastUserMessage() string {
	for i := len(s.Messages) - 1; i >= 0; i-- {
		if s.Messages[i].Role == RoleUser {
			for _, block := range s.Messages[i].Content {
				if block.Type == ContentTypeText && block.Text != "" {
					return block.Text
				}
			}
		}
	}
	return ""
}

// RecentContext returns a text summary of the last N user/assistant text
// exchanges, formatted as "ROLE: message" lines. This provides conversation
// context for commit message generation (similar to aider's context parameter).
// Tool-use and tool-result blocks are omitted to keep the context concise.
func (s *Session) RecentContext(maxTurns int) string {
	if len(s.Messages) == 0 || maxTurns <= 0 {
		return ""
	}

	// Collect the last maxTurns messages that have text content.
	type entry struct {
		role string
		text string
	}
	var entries []entry

	for i := len(s.Messages) - 1; i >= 0 && len(entries) < maxTurns; i-- {
		msg := s.Messages[i]
		if msg.Role != RoleUser && msg.Role != RoleAssistant {
			continue
		}

		for _, block := range msg.Content {
			if block.Type == ContentTypeText && block.Text != "" {
				label := "USER"
				if msg.Role == RoleAssistant {
					label = "ASSISTANT"
				}
				entries = append(entries, entry{role: label, text: block.Text})
				break // one text block per message is enough
			}
		}
	}

	if len(entries) == 0 {
		return ""
	}

	// Reverse to chronological order.
	for i, j := 0, len(entries)-1; i < j; i, j = i+1, j-1 {
		entries[i], entries[j] = entries[j], entries[i]
	}

	var b strings.Builder
	for _, e := range entries {
		// Truncate very long messages to keep context compact.
		text := e.text
		if len(text) > 500 {
			text = text[:500] + "..."
		}
		fmt.Fprintf(&b, "%s: %s\n", e.role, text)
	}
	return strings.TrimSpace(b.String())
}
