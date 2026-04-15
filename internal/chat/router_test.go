package chat

import (
	"testing"

	"github.com/qiangli/ycode/internal/chat/channel"
)

func TestResolveRoom_AutoCreate(t *testing.T) {
	s := newTestStore(t)
	r := NewRouter(s)

	room, err := r.ResolveRoom(channel.ChannelTelegram, "bot1", "chat-456")
	if err != nil {
		t.Fatalf("ResolveRoom: %v", err)
	}
	if room.ID == "" {
		t.Fatal("expected non-empty room ID")
	}

	// Second resolve should return the same room.
	room2, err := r.ResolveRoom(channel.ChannelTelegram, "bot1", "chat-456")
	if err != nil {
		t.Fatalf("ResolveRoom (2nd): %v", err)
	}
	if room2.ID != room.ID {
		t.Fatalf("expected same room, got %q vs %q", room2.ID, room.ID)
	}
}

func TestFanOutTargets(t *testing.T) {
	s := newTestStore(t)
	r := NewRouter(s)

	room, _ := s.CreateRoom("fanout-room")
	s.AddBinding(room.ID, channel.ChannelTelegram, "bot1", "tg-chat")
	s.AddBinding(room.ID, channel.ChannelDiscord, "bot2", "dc-chan")
	s.AddBinding(room.ID, channel.ChannelWeb, "default", room.ID)

	// Fan out from telegram should return discord and web.
	targets, err := r.FanOutTargets(room.ID, channel.ChannelTelegram, "bot1", "tg-chat")
	if err != nil {
		t.Fatalf("FanOutTargets: %v", err)
	}
	if len(targets) != 2 {
		t.Fatalf("got %d targets, want 2", len(targets))
	}

	// Verify the origin is excluded.
	for _, tgt := range targets {
		if tgt.ChannelID == channel.ChannelTelegram && tgt.ChatID == "tg-chat" {
			t.Fatal("origin should be excluded from fan-out targets")
		}
	}
}
