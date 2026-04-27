package swarm

import (
	"encoding/json"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"sort"
	"time"

	"github.com/google/uuid"
)

// MailboxMessage is an async message in the file-based queue.
type MailboxMessage struct {
	ID        string    `json:"id"`
	From      string    `json:"from"`
	Type      string    `json:"type"` // "task", "result", "shutdown", "ping"
	Content   string    `json:"content"`
	CreatedAt time.Time `json:"created_at"`
}

// Mailbox provides file-based async messaging for cross-process agent communication.
type Mailbox struct {
	dir string // e.g., ~/.agents/ycode/teams/<team>/agents/<id>/inbox/
}

// NewMailbox creates a mailbox at the given directory.
func NewMailbox(dir string) (*Mailbox, error) {
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return nil, fmt.Errorf("create mailbox dir: %w", err)
	}
	return &Mailbox{dir: dir}, nil
}

// Send writes a message to the mailbox atomically.
func (m *Mailbox) Send(msg MailboxMessage) error {
	slog.Debug("swarm.mailbox.send",
		"from", msg.From,
		"type", msg.Type,
		"id", msg.ID,
	)
	if msg.ID == "" {
		msg.ID = uuid.New().String()
	}
	if msg.CreatedAt.IsZero() {
		msg.CreatedAt = time.Now()
	}
	data, err := json.Marshal(msg)
	if err != nil {
		return fmt.Errorf("marshal message: %w", err)
	}
	// Write to temp file, then atomic rename.
	tmpPath := filepath.Join(m.dir, ".tmp-"+msg.ID+".json")
	finalPath := filepath.Join(m.dir, msg.ID+".json")
	if err := os.WriteFile(tmpPath, data, 0o644); err != nil {
		return fmt.Errorf("write temp: %w", err)
	}
	return os.Rename(tmpPath, finalPath)
}

// Receive reads and removes the oldest message.
// Returns nil if the mailbox is empty.
func (m *Mailbox) Receive() (*MailboxMessage, error) {
	messages, err := m.list()
	if err != nil || len(messages) == 0 {
		return nil, err
	}
	// Read the oldest message.
	msg := messages[0]
	path := filepath.Join(m.dir, msg.ID+".json")
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("read message: %w", err)
	}
	var result MailboxMessage
	if err := json.Unmarshal(data, &result); err != nil {
		return nil, fmt.Errorf("unmarshal message: %w", err)
	}
	// Remove after reading.
	os.Remove(path)

	slog.Debug("swarm.mailbox.receive",
		"from", result.From,
		"type", result.Type,
		"id", result.ID,
	)
	return &result, nil
}

// Peek returns the oldest message without removing it.
func (m *Mailbox) Peek() (*MailboxMessage, error) {
	messages, err := m.list()
	if err != nil || len(messages) == 0 {
		return nil, err
	}
	path := filepath.Join(m.dir, messages[0].ID+".json")
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	var result MailboxMessage
	if err := json.Unmarshal(data, &result); err != nil {
		return nil, err
	}
	return &result, nil
}

// Count returns the number of pending messages.
func (m *Mailbox) Count() int {
	msgs, _ := m.list()
	return len(msgs)
}

type msgEntry struct {
	ID        string
	CreatedAt time.Time
}

func (m *Mailbox) list() ([]msgEntry, error) {
	entries, err := os.ReadDir(m.dir)
	if err != nil {
		return nil, nil
	}
	var msgs []msgEntry
	for _, e := range entries {
		if e.IsDir() || !isMessageFile(e.Name()) {
			continue
		}
		info, err := e.Info()
		if err != nil {
			continue
		}
		id := e.Name()[:len(e.Name())-5] // strip .json
		msgs = append(msgs, msgEntry{ID: id, CreatedAt: info.ModTime()})
	}
	sort.Slice(msgs, func(i, j int) bool {
		return msgs[i].CreatedAt.Before(msgs[j].CreatedAt)
	})
	return msgs, nil
}

func isMessageFile(name string) bool {
	return len(name) > 5 && name[len(name)-5:] == ".json" && name[0] != '.'
}
