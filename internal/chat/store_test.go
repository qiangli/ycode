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
