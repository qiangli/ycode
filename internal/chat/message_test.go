package chat

import (
	"encoding/json"
	"testing"
	"time"

	"github.com/qiangli/ycode/internal/chat/channel"
)

func TestMessageJSON(t *testing.T) {
	msg := Message{
		ID:     "m-1",
		RoomID: "r-1",
		Sender: Sender{
			ID:          "u-1",
			DisplayName: "Alice",
			ChannelID:   channel.ChannelTelegram,
			PlatformID:  "tg-123",
		},
		Timestamp: time.Date(2026, 4, 15, 12, 0, 0, 0, time.UTC),
		Content: channel.MessageContent{
			Text: "hello",
			Attachments: []channel.Attachment{
				{Type: "image", URL: "https://example.com/img.png", Name: "img.png"},
			},
		},
		Origin: MessageOrigin{
			ChannelID:  channel.ChannelTelegram,
			AccountID:  "bot1",
			PlatformID: "42",
		},
	}

	data, err := json.Marshal(msg)
	if err != nil {
		t.Fatalf("Marshal: %v", err)
	}

	var decoded Message
	if err := json.Unmarshal(data, &decoded); err != nil {
		t.Fatalf("Unmarshal: %v", err)
	}

	if decoded.Content.Text != "hello" {
		t.Fatalf("text: got %q, want %q", decoded.Content.Text, "hello")
	}
	if len(decoded.Content.Attachments) != 1 {
		t.Fatalf("attachments: got %d, want 1", len(decoded.Content.Attachments))
	}
	if decoded.Origin.ChannelID != channel.ChannelTelegram {
		t.Fatalf("origin channel: got %q, want %q", decoded.Origin.ChannelID, channel.ChannelTelegram)
	}
}
