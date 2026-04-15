package chat

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	natsserver "github.com/nats-io/nats-server/v2/server"
	"github.com/nats-io/nats.go"

	"github.com/qiangli/ycode/internal/chat/adapters"
	"github.com/qiangli/ycode/internal/chat/channel"
)

// startTestNATS starts an in-process NATS server on a random port and returns a connection.
func startTestNATS(t *testing.T) *nats.Conn {
	t.Helper()
	opts := &natsserver.Options{
		Host:   "127.0.0.1",
		Port:   -1, // random port
		NoLog:  true,
		NoSigs: true,
	}
	srv, err := natsserver.NewServer(opts)
	if err != nil {
		t.Fatalf("create NATS: %v", err)
	}
	srv.Start()
	if !srv.ReadyForConnections(5 * time.Second) {
		t.Fatal("NATS not ready")
	}
	t.Cleanup(srv.Shutdown)

	conn, err := nats.Connect(srv.ClientURL())
	if err != nil {
		t.Fatalf("connect NATS: %v", err)
	}
	t.Cleanup(conn.Close)
	return conn
}

func newTestHub(t *testing.T) (*Hub, *adapters.MockChannel) {
	t.Helper()
	conn := startTestNATS(t)
	dir := t.TempDir()

	cfg := &HubConfig{
		Enabled: true,
		Channels: map[channel.ChannelID]ChannelConfig{
			channel.ChannelWeb: {Enabled: true},
			"mock":             {Enabled: true},
		},
	}

	hub := NewHub(conn, cfg, dir)
	mock := adapters.NewMockChannel("mock")
	hub.RegisterChannel(adapters.NewWebChannel())
	hub.RegisterChannel(mock)

	if err := hub.Start(context.Background()); err != nil {
		t.Fatalf("hub start: %v", err)
	}
	t.Cleanup(func() { hub.Stop(context.Background()) })

	return hub, mock
}

func TestHub_CreateRoom_ListRooms(t *testing.T) {
	hub, _ := newTestHub(t)

	// Create room via API.
	body := `{"name":"test-room"}`
	req := httptest.NewRequest("POST", "/api/rooms", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	hub.HTTPHandler().ServeHTTP(w, req)

	if w.Code != http.StatusCreated {
		t.Fatalf("create room: got %d, want %d", w.Code, http.StatusCreated)
	}

	var room Room
	json.Unmarshal(w.Body.Bytes(), &room)
	if room.Name != "test-room" {
		t.Fatalf("room name: got %q, want %q", room.Name, "test-room")
	}

	// List rooms.
	req2 := httptest.NewRequest("GET", "/api/rooms", nil)
	w2 := httptest.NewRecorder()
	hub.HTTPHandler().ServeHTTP(w2, req2)

	var rooms []*Room
	json.Unmarshal(w2.Body.Bytes(), &rooms)
	if len(rooms) != 1 {
		t.Fatalf("list rooms: got %d, want 1", len(rooms))
	}
}

func TestHub_Dashboard(t *testing.T) {
	hub, _ := newTestHub(t)

	// Create a room first.
	body := `{"name":"dash-room"}`
	req := httptest.NewRequest("POST", "/api/rooms", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	hub.HTTPHandler().ServeHTTP(w, req)

	// Fetch dashboard.
	req2 := httptest.NewRequest("GET", "/api/dashboard", nil)
	w2 := httptest.NewRecorder()
	hub.HTTPHandler().ServeHTTP(w2, req2)

	if w2.Code != http.StatusOK {
		t.Fatalf("dashboard: got %d, want 200", w2.Code)
	}

	var dash DashboardData
	json.Unmarshal(w2.Body.Bytes(), &dash)

	if len(dash.Rooms) != 1 {
		t.Fatalf("dashboard rooms: got %d, want 1", len(dash.Rooms))
	}
	if dash.Rooms[0].Name != "dash-room" {
		t.Fatalf("dashboard room name: got %q, want %q", dash.Rooms[0].Name, "dash-room")
	}
	if len(dash.Channels) < 2 {
		t.Fatalf("dashboard channels: got %d, want >= 2", len(dash.Channels))
	}
}

func TestHub_InboundFanOut(t *testing.T) {
	hub, mock := newTestHub(t)

	// Create a room with both web and mock bindings.
	room, err := hub.store.CreateRoom("bridge-room")
	if err != nil {
		t.Fatalf("create room: %v", err)
	}
	hub.store.AddBinding(room.ID, channel.ChannelWeb, "default", room.ID)
	hub.store.AddBinding(room.ID, "mock", "default", "mock-chat-1")

	// Simulate inbound from web channel.
	hub.inbound <- channel.InboundMessage{
		ChannelID:  channel.ChannelWeb,
		AccountID:  "default",
		SenderID:   "web-user",
		SenderName: "Alice",
		Content:    channel.MessageContent{Text: "hello from web"},
		PlatformID: "p1",
		ChatID:     room.ID,
		Timestamp:  time.Now(),
	}

	// Wait for processing.
	time.Sleep(200 * time.Millisecond)

	// Check that the mock channel received the fan-out.
	sent := mock.SentMessages()
	if len(sent) != 1 {
		t.Fatalf("mock sent: got %d, want 1", len(sent))
	}
	if sent[0].Msg.Content.Text != "hello from web" {
		t.Fatalf("mock msg: got %q, want %q", sent[0].Msg.Content.Text, "hello from web")
	}
	if sent[0].Target.ChatID != "mock-chat-1" {
		t.Fatalf("mock target: got %q, want %q", sent[0].Target.ChatID, "mock-chat-1")
	}

	// Verify message was persisted.
	msgs, err := hub.store.GetMessages(room.ID, 10, 0)
	if err != nil {
		t.Fatalf("get messages: %v", err)
	}
	if len(msgs) != 1 {
		t.Fatalf("persisted messages: got %d, want 1", len(msgs))
	}
}

func TestHub_Health(t *testing.T) {
	hub, _ := newTestHub(t)

	req := httptest.NewRequest("GET", "/api/health", nil)
	w := httptest.NewRecorder()
	hub.HTTPHandler().ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("health: got %d, want 200", w.Code)
	}

	var resp map[string]bool
	json.Unmarshal(w.Body.Bytes(), &resp)
	if !resp["healthy"] {
		t.Fatal("expected healthy=true")
	}
}

func TestHub_ChannelList(t *testing.T) {
	hub, _ := newTestHub(t)

	req := httptest.NewRequest("GET", "/api/channels", nil)
	w := httptest.NewRecorder()
	hub.HTTPHandler().ServeHTTP(w, req)

	var channels []struct {
		ID      string `json:"id"`
		Healthy bool   `json:"healthy"`
	}
	json.Unmarshal(w.Body.Bytes(), &channels)

	if len(channels) < 2 {
		t.Fatalf("channels: got %d, want >= 2", len(channels))
	}
}
