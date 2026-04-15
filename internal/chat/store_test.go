package chat

import (
	"testing"
	"time"

	"github.com/qiangli/ycode/internal/chat/channel"
)

func newTestStore(t *testing.T) *Store {
	t.Helper()
	dir := t.TempDir()
	s, err := NewStore(dir)
	if err != nil {
		t.Fatalf("NewStore: %v", err)
	}
	t.Cleanup(func() { s.Close() })
	return s
}

func TestCreateAndGetRoom(t *testing.T) {
	s := newTestStore(t)

	room, err := s.CreateRoom("test-room")
	if err != nil {
		t.Fatalf("CreateRoom: %v", err)
	}
	if room.ID == "" || room.Name != "test-room" {
		t.Fatalf("unexpected room: %+v", room)
	}

	got, err := s.GetRoom(room.ID)
	if err != nil {
		t.Fatalf("GetRoom: %v", err)
	}
	if got.Name != "test-room" {
		t.Fatalf("got name %q, want %q", got.Name, "test-room")
	}
}

func TestListRooms(t *testing.T) {
	s := newTestStore(t)

	s.CreateRoom("room-a")
	s.CreateRoom("room-b")

	rooms, err := s.ListRooms()
	if err != nil {
		t.Fatalf("ListRooms: %v", err)
	}
	if len(rooms) != 2 {
		t.Fatalf("got %d rooms, want 2", len(rooms))
	}
}

func TestBindingLookup(t *testing.T) {
	s := newTestStore(t)

	room, _ := s.CreateRoom("bound-room")
	if err := s.AddBinding(room.ID, channel.ChannelTelegram, "bot1", "chat123"); err != nil {
		t.Fatalf("AddBinding: %v", err)
	}

	found, err := s.FindRoomByBinding(channel.ChannelTelegram, "bot1", "chat123")
	if err != nil {
		t.Fatalf("FindRoomByBinding: %v", err)
	}
	if found.ID != room.ID {
		t.Fatalf("got room %q, want %q", found.ID, room.ID)
	}

	// Not found case.
	_, err = s.FindRoomByBinding(channel.ChannelDiscord, "x", "y")
	if err == nil {
		t.Fatal("expected error for missing binding")
	}
}

func TestSaveAndGetMessages(t *testing.T) {
	s := newTestStore(t)

	room, _ := s.CreateRoom("msg-room")

	msg := &Message{
		ID:     "msg-1",
		RoomID: room.ID,
		Sender: Sender{
			ID:          "user-1",
			DisplayName: "Alice",
			ChannelID:   channel.ChannelWeb,
			PlatformID:  "web-alice",
		},
		Timestamp: time.Now(),
		Content:   channel.MessageContent{Text: "hello world"},
		Origin: MessageOrigin{
			ChannelID: channel.ChannelWeb,
		},
	}

	if err := s.SaveMessage(msg); err != nil {
		t.Fatalf("SaveMessage: %v", err)
	}

	messages, err := s.GetMessages(room.ID, 10, 0)
	if err != nil {
		t.Fatalf("GetMessages: %v", err)
	}
	if len(messages) != 1 {
		t.Fatalf("got %d messages, want 1", len(messages))
	}
	if messages[0].Content.Text != "hello world" {
		t.Fatalf("got text %q, want %q", messages[0].Content.Text, "hello world")
	}
}

func TestRenameRoom(t *testing.T) {
	s := newTestStore(t)
	room, _ := s.CreateRoom("old-name")

	if err := s.RenameRoom(room.ID, "new-name"); err != nil {
		t.Fatalf("RenameRoom: %v", err)
	}

	got, _ := s.GetRoom(room.ID)
	if got.Name != "new-name" {
		t.Fatalf("got name %q, want %q", got.Name, "new-name")
	}
}

func TestRemoveBinding(t *testing.T) {
	s := newTestStore(t)
	room, _ := s.CreateRoom("rm-bind-room")
	s.AddBinding(room.ID, channel.ChannelTelegram, "bot1", "chat1")

	bindings, _ := s.GetBindings(room.ID)
	if len(bindings) != 1 {
		t.Fatalf("expected 1 binding, got %d", len(bindings))
	}

	if err := s.RemoveBinding(bindings[0].ID); err != nil {
		t.Fatalf("RemoveBinding: %v", err)
	}

	bindings2, _ := s.GetBindings(room.ID)
	if len(bindings2) != 0 {
		t.Fatalf("expected 0 bindings after remove, got %d", len(bindings2))
	}
}

func TestGetRoomStats(t *testing.T) {
	s := newTestStore(t)
	room, _ := s.CreateRoom("stats-room")

	// Empty stats.
	stats, err := s.GetRoomStats(room.ID)
	if err != nil {
		t.Fatalf("GetRoomStats: %v", err)
	}
	if stats.MessageCount != 0 {
		t.Fatalf("expected 0 messages, got %d", stats.MessageCount)
	}

	// Add a message.
	msg := &Message{
		ID:     "stat-msg-1",
		RoomID: room.ID,
		Sender: Sender{ID: "u1", DisplayName: "Alice", ChannelID: channel.ChannelWeb},
		Timestamp: time.Now(),
		Content:   channel.MessageContent{Text: "hi"},
		Origin:    MessageOrigin{ChannelID: channel.ChannelWeb},
	}
	s.SaveMessage(msg)

	stats2, _ := s.GetRoomStats(room.ID)
	if stats2.MessageCount != 1 {
		t.Fatalf("expected 1 message, got %d", stats2.MessageCount)
	}
	if stats2.UserCount != 1 {
		t.Fatalf("expected 1 user, got %d", stats2.UserCount)
	}
}

func TestFindOrCreateUser(t *testing.T) {
	s := newTestStore(t)

	u1, err := s.FindOrCreateUser(channel.ChannelTelegram, "tg-123", "Bob")
	if err != nil {
		t.Fatalf("FindOrCreateUser: %v", err)
	}
	if u1.DisplayName != "Bob" {
		t.Fatalf("got name %q, want Bob", u1.DisplayName)
	}

	// Same user, should return existing.
	u2, err := s.FindOrCreateUser(channel.ChannelTelegram, "tg-123", "Bob")
	if err != nil {
		t.Fatalf("FindOrCreateUser (2nd): %v", err)
	}
	if u2.ID != u1.ID {
		t.Fatalf("expected same user ID, got %q vs %q", u2.ID, u1.ID)
	}
}
