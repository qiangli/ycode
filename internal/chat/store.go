package chat

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"github.com/google/uuid"
	_ "modernc.org/sqlite"

	"github.com/qiangli/ycode/internal/chat/channel"
)

// Store provides persistence for chat rooms, bindings, messages, and users.
type Store struct {
	db *sql.DB
}

// NewStore opens (or creates) a SQLite database at the given directory
// and runs migrations.
func NewStore(dataDir string) (*Store, error) {
	if err := os.MkdirAll(dataDir, 0o755); err != nil {
		return nil, fmt.Errorf("chat store: create dir: %w", err)
	}
	dbPath := filepath.Join(dataDir, "chat.db")
	db, err := sql.Open("sqlite", dbPath+"?_journal_mode=WAL&_busy_timeout=5000")
	if err != nil {
		return nil, fmt.Errorf("chat store: open db: %w", err)
	}
	s := &Store{db: db}
	if err := s.migrate(); err != nil {
		db.Close()
		return nil, fmt.Errorf("chat store: migrate: %w", err)
	}
	return s, nil
}

func (s *Store) migrate() error {
	const ddl = `
CREATE TABLE IF NOT EXISTS chat_rooms (
    id         TEXT PRIMARY KEY,
    name       TEXT NOT NULL DEFAULT '',
    created_at TEXT NOT NULL DEFAULT (datetime('now')),
    updated_at TEXT NOT NULL DEFAULT (datetime('now'))
);

CREATE TABLE IF NOT EXISTS chat_bindings (
    id         INTEGER PRIMARY KEY AUTOINCREMENT,
    room_id    TEXT NOT NULL REFERENCES chat_rooms(id) ON DELETE CASCADE,
    channel_id TEXT NOT NULL,
    account_id TEXT NOT NULL DEFAULT 'default',
    chat_id    TEXT NOT NULL,
    UNIQUE(channel_id, account_id, chat_id)
);
CREATE INDEX IF NOT EXISTS idx_chat_bindings_room ON chat_bindings(room_id);
CREATE INDEX IF NOT EXISTS idx_chat_bindings_lookup ON chat_bindings(channel_id, account_id, chat_id);

CREATE TABLE IF NOT EXISTS chat_messages (
    id          TEXT PRIMARY KEY,
    room_id     TEXT NOT NULL REFERENCES chat_rooms(id) ON DELETE CASCADE,
    sender_id   TEXT NOT NULL,
    sender_name TEXT NOT NULL DEFAULT '',
    channel_id  TEXT NOT NULL,
    platform_id TEXT NOT NULL DEFAULT '',
    content     TEXT NOT NULL,
    reply_to    TEXT,
    thread_id   TEXT,
    timestamp   TEXT NOT NULL DEFAULT (datetime('now'))
);
CREATE INDEX IF NOT EXISTS idx_chat_messages_room ON chat_messages(room_id, timestamp);

CREATE TABLE IF NOT EXISTS chat_users (
    id           TEXT PRIMARY KEY,
    display_name TEXT NOT NULL,
    channel_id   TEXT NOT NULL,
    platform_id  TEXT NOT NULL,
    UNIQUE(channel_id, platform_id)
);
`
	_, err := s.db.Exec(ddl)
	return err
}

// Close closes the database.
func (s *Store) Close() error {
	return s.db.Close()
}

// Room represents a chat room in the store.
type Room struct {
	ID        string    `json:"id"`
	Name      string    `json:"name"`
	CreatedAt time.Time `json:"created_at"`
	UpdatedAt time.Time `json:"updated_at"`
}

// Binding connects a room to a platform conversation.
type Binding struct {
	ID        int64             `json:"id"`
	RoomID    string            `json:"room_id"`
	ChannelID channel.ChannelID `json:"channel_id"`
	AccountID string            `json:"account_id"`
	ChatID    string            `json:"chat_id"`
}

// User represents a chat user.
type User struct {
	ID          string            `json:"id"`
	DisplayName string            `json:"display_name"`
	ChannelID   channel.ChannelID `json:"channel_id"`
	PlatformID  string            `json:"platform_id"`
}

// CreateRoom creates a new room and returns it.
func (s *Store) CreateRoom(name string) (*Room, error) {
	id := uuid.New().String()
	now := time.Now().UTC().Format(time.RFC3339)
	_, err := s.db.Exec(
		"INSERT INTO chat_rooms (id, name, created_at, updated_at) VALUES (?, ?, ?, ?)",
		id, name, now, now,
	)
	if err != nil {
		return nil, err
	}
	return &Room{ID: id, Name: name}, nil
}

// GetRoom retrieves a room by ID.
func (s *Store) GetRoom(id string) (*Room, error) {
	var r Room
	var createdAt, updatedAt string
	err := s.db.QueryRow(
		"SELECT id, name, created_at, updated_at FROM chat_rooms WHERE id = ?", id,
	).Scan(&r.ID, &r.Name, &createdAt, &updatedAt)
	if err != nil {
		return nil, err
	}
	r.CreatedAt, _ = time.Parse(time.RFC3339, createdAt)
	r.UpdatedAt, _ = time.Parse(time.RFC3339, updatedAt)
	return &r, nil
}

// ListRooms returns all rooms.
func (s *Store) ListRooms() ([]*Room, error) {
	rows, err := s.db.Query("SELECT id, name, created_at, updated_at FROM chat_rooms ORDER BY updated_at DESC")
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var rooms []*Room
	for rows.Next() {
		var r Room
		var createdAt, updatedAt string
		if err := rows.Scan(&r.ID, &r.Name, &createdAt, &updatedAt); err != nil {
			return nil, err
		}
		r.CreatedAt, _ = time.Parse(time.RFC3339, createdAt)
		r.UpdatedAt, _ = time.Parse(time.RFC3339, updatedAt)
		rooms = append(rooms, &r)
	}
	return rooms, rows.Err()
}

// AddBinding adds a platform binding to a room.
func (s *Store) AddBinding(roomID string, channelID channel.ChannelID, accountID, chatID string) error {
	_, err := s.db.Exec(
		"INSERT OR IGNORE INTO chat_bindings (room_id, channel_id, account_id, chat_id) VALUES (?, ?, ?, ?)",
		roomID, string(channelID), accountID, chatID,
	)
	return err
}

// FindRoomByBinding looks up a room by its platform binding.
func (s *Store) FindRoomByBinding(channelID channel.ChannelID, accountID, chatID string) (*Room, error) {
	var r Room
	var createdAt, updatedAt string
	err := s.db.QueryRow(`
		SELECT r.id, r.name, r.created_at, r.updated_at
		FROM chat_rooms r
		JOIN chat_bindings b ON b.room_id = r.id
		WHERE b.channel_id = ? AND b.account_id = ? AND b.chat_id = ?`,
		string(channelID), accountID, chatID,
	).Scan(&r.ID, &r.Name, &createdAt, &updatedAt)
	if err != nil {
		return nil, err
	}
	r.CreatedAt, _ = time.Parse(time.RFC3339, createdAt)
	r.UpdatedAt, _ = time.Parse(time.RFC3339, updatedAt)
	return &r, nil
}

// GetBindings returns all bindings for a room.
func (s *Store) GetBindings(roomID string) ([]*Binding, error) {
	rows, err := s.db.Query(
		"SELECT id, room_id, channel_id, account_id, chat_id FROM chat_bindings WHERE room_id = ?",
		roomID,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var bindings []*Binding
	for rows.Next() {
		var b Binding
		var chID string
		if err := rows.Scan(&b.ID, &b.RoomID, &chID, &b.AccountID, &b.ChatID); err != nil {
			return nil, err
		}
		b.ChannelID = channel.ChannelID(chID)
		bindings = append(bindings, &b)
	}
	return bindings, rows.Err()
}

// SaveMessage persists a message.
func (s *Store) SaveMessage(msg *Message) error {
	contentJSON, err := json.Marshal(msg.Content)
	if err != nil {
		return err
	}
	ts := msg.Timestamp.UTC().Format(time.RFC3339Nano)
	_, err = s.db.Exec(`
		INSERT INTO chat_messages (id, room_id, sender_id, sender_name, channel_id, platform_id, content, reply_to, thread_id, timestamp)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		msg.ID, msg.RoomID, msg.Sender.ID, msg.Sender.DisplayName,
		string(msg.Origin.ChannelID), msg.Origin.PlatformID,
		string(contentJSON), msg.ReplyTo, msg.ThreadID, ts,
	)
	// Update room's updated_at.
	if err == nil {
		s.db.Exec("UPDATE chat_rooms SET updated_at = ? WHERE id = ?", ts, msg.RoomID)
	}
	return err
}

// GetMessages returns messages for a room, ordered by timestamp, with pagination.
func (s *Store) GetMessages(roomID string, limit, offset int) ([]*Message, error) {
	if limit <= 0 {
		limit = 50
	}
	rows, err := s.db.Query(`
		SELECT id, room_id, sender_id, sender_name, channel_id, platform_id, content, reply_to, thread_id, timestamp
		FROM chat_messages
		WHERE room_id = ?
		ORDER BY timestamp ASC
		LIMIT ? OFFSET ?`,
		roomID, limit, offset,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var messages []*Message
	for rows.Next() {
		var m Message
		var chID, contentJSON, ts string
		var replyTo, threadID sql.NullString
		if err := rows.Scan(&m.ID, &m.RoomID, &m.Sender.ID, &m.Sender.DisplayName,
			&chID, &m.Origin.PlatformID, &contentJSON, &replyTo, &threadID, &ts); err != nil {
			return nil, err
		}
		m.Origin.ChannelID = channel.ChannelID(chID)
		m.Sender.ChannelID = m.Origin.ChannelID
		m.Timestamp, _ = time.Parse(time.RFC3339Nano, ts)
		if replyTo.Valid {
			m.ReplyTo = replyTo.String
		}
		if threadID.Valid {
			m.ThreadID = threadID.String
		}
		_ = json.Unmarshal([]byte(contentJSON), &m.Content)
		messages = append(messages, &m)
	}
	return messages, rows.Err()
}

// RoomStats holds aggregate stats for a room.
type RoomStats struct {
	RoomID       string    `json:"room_id"`
	MessageCount int       `json:"message_count"`
	LastActivity time.Time `json:"last_activity"`
	UserCount    int       `json:"user_count"`
}

// GetRoomStats returns aggregate stats for a room.
func (s *Store) GetRoomStats(roomID string) (*RoomStats, error) {
	var stats RoomStats
	stats.RoomID = roomID

	var lastTS sql.NullString
	err := s.db.QueryRow(
		"SELECT COUNT(*), MAX(timestamp) FROM chat_messages WHERE room_id = ?", roomID,
	).Scan(&stats.MessageCount, &lastTS)
	if err != nil {
		return nil, err
	}
	if lastTS.Valid {
		stats.LastActivity, _ = time.Parse(time.RFC3339Nano, lastTS.String)
	}

	err = s.db.QueryRow(
		"SELECT COUNT(DISTINCT sender_id) FROM chat_messages WHERE room_id = ?", roomID,
	).Scan(&stats.UserCount)
	if err != nil {
		return nil, err
	}
	return &stats, nil
}

// RenameRoom updates a room's name.
func (s *Store) RenameRoom(roomID, name string) error {
	now := time.Now().UTC().Format(time.RFC3339)
	_, err := s.db.Exec("UPDATE chat_rooms SET name = ?, updated_at = ? WHERE id = ?", name, now, roomID)
	return err
}

// RemoveBinding removes a binding by its ID.
func (s *Store) RemoveBinding(bindingID int64) error {
	_, err := s.db.Exec("DELETE FROM chat_bindings WHERE id = ?", bindingID)
	return err
}

// FindOrCreateUser finds a user by channel+platform ID, or creates one.
func (s *Store) FindOrCreateUser(channelID channel.ChannelID, platformID, displayName string) (*User, error) {
	var u User
	var chID string
	err := s.db.QueryRow(
		"SELECT id, display_name, channel_id, platform_id FROM chat_users WHERE channel_id = ? AND platform_id = ?",
		string(channelID), platformID,
	).Scan(&u.ID, &u.DisplayName, &chID, &u.PlatformID)
	if err == nil {
		u.ChannelID = channel.ChannelID(chID)
		// Update display name if changed.
		if u.DisplayName != displayName && displayName != "" {
			s.db.Exec("UPDATE chat_users SET display_name = ? WHERE id = ?", displayName, u.ID)
			u.DisplayName = displayName
		}
		return &u, nil
	}

	// Create new user.
	u = User{
		ID:          uuid.New().String(),
		DisplayName: displayName,
		ChannelID:   channelID,
		PlatformID:  platformID,
	}
	_, err = s.db.Exec(
		"INSERT INTO chat_users (id, display_name, channel_id, platform_id) VALUES (?, ?, ?, ?)",
		u.ID, u.DisplayName, string(u.ChannelID), u.PlatformID,
	)
	if err != nil {
		return nil, err
	}
	return &u, nil
}
