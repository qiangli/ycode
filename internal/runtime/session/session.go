package session

import (
	"bufio"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
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
}

// New creates a new session.
func New(dir string) (*Session, error) {
	id := uuid.New().String()
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
	return s.appendToFile(msg)
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

// MessageCount returns the number of messages in the session.
func (s *Session) MessageCount() int {
	return len(s.Messages)
}
