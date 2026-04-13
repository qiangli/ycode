package server

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/gorilla/websocket"

	"github.com/qiangli/ycode/internal/bus"
	"github.com/qiangli/ycode/internal/runtime/config"
	"github.com/qiangli/ycode/internal/service"
)

// mockService implements service.Service for testing.
type mockService struct {
	b bus.Bus
}

func (m *mockService) Bus() bus.Bus { return m.b }
func (m *mockService) CreateSession(ctx context.Context) (*service.SessionInfo, error) {
	return &service.SessionInfo{ID: "test-session", MessageCount: 0}, nil
}
func (m *mockService) GetSession(ctx context.Context, id string) (*service.SessionInfo, error) {
	return &service.SessionInfo{ID: id, MessageCount: 5}, nil
}
func (m *mockService) ListSessions(ctx context.Context) ([]service.SessionInfo, error) {
	return []service.SessionInfo{{ID: "test-session"}}, nil
}
func (m *mockService) GetMessages(ctx context.Context, sessionID string) ([]json.RawMessage, error) {
	return nil, nil
}
func (m *mockService) SendMessage(ctx context.Context, sessionID string, input service.MessageInput) error {
	// Simulate a turn with events.
	m.b.Publish(bus.Event{
		Type:      bus.EventTurnStart,
		SessionID: sessionID,
		Data:      json.RawMessage(`{}`),
	})
	m.b.Publish(bus.Event{
		Type:      bus.EventTextDelta,
		SessionID: sessionID,
		Data:      json.RawMessage(`{"text":"Hello from mock"}`),
	})
	m.b.Publish(bus.Event{
		Type:      bus.EventTurnComplete,
		SessionID: sessionID,
		Data:      json.RawMessage(`{"status":"complete"}`),
	})
	return nil
}
func (m *mockService) CancelTurn(ctx context.Context, sessionID string) error { return nil }
func (m *mockService) RespondPermission(ctx context.Context, requestID string, allowed bool) error {
	return nil
}
func (m *mockService) GetConfig(ctx context.Context) (*config.Config, error) {
	return &config.Config{Model: "test-model"}, nil
}
func (m *mockService) SwitchModel(ctx context.Context, model string) error { return nil }
func (m *mockService) GetStatus(ctx context.Context) (*service.StatusInfo, error) {
	return &service.StatusInfo{Model: "test-model", Version: "test"}, nil
}
func (m *mockService) ExecuteCommand(ctx context.Context, name string, args string) (string, error) {
	return "ok", nil
}

func newTestServer(t *testing.T) (*Server, *httptest.Server) {
	t.Helper()
	memBus := bus.NewMemoryBus()
	t.Cleanup(func() { memBus.Close() })

	svc := &mockService{b: memBus}
	srv := New(Config{Token: "test-token"}, svc)

	ts := httptest.NewServer(srv.Mux())
	t.Cleanup(ts.Close)

	return srv, ts
}

func TestHealthEndpoint(t *testing.T) {
	_, ts := newTestServer(t)

	resp, err := http.Get(ts.URL + "/api/health")
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		t.Errorf("got status %d, want %d", resp.StatusCode, http.StatusOK)
	}
}

func TestAuthMiddleware(t *testing.T) {
	_, ts := newTestServer(t)

	// Without token — should be 401.
	resp, err := http.Get(ts.URL + "/api/config")
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusUnauthorized {
		t.Errorf("got status %d, want %d", resp.StatusCode, http.StatusUnauthorized)
	}

	// With token — should be 200.
	req, _ := http.NewRequest("GET", ts.URL+"/api/config", nil)
	req.Header.Set("Authorization", "Bearer test-token")
	resp, err = http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Errorf("got status %d, want %d", resp.StatusCode, http.StatusOK)
	}

	// With query param token — should be 200.
	resp, err = http.Get(ts.URL + "/api/config?token=test-token")
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Errorf("got status %d, want %d", resp.StatusCode, http.StatusOK)
	}
}

func TestListSessions(t *testing.T) {
	_, ts := newTestServer(t)

	req, _ := http.NewRequest("GET", ts.URL+"/api/sessions", nil)
	req.Header.Set("Authorization", "Bearer test-token")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		t.Fatalf("got status %d", resp.StatusCode)
	}

	var sessions []service.SessionInfo
	if err := json.NewDecoder(resp.Body).Decode(&sessions); err != nil {
		t.Fatal(err)
	}
	if len(sessions) != 1 {
		t.Errorf("got %d sessions, want 1", len(sessions))
	}
}

func TestWebSocket(t *testing.T) {
	_, ts := newTestServer(t)

	// Convert http:// to ws://
	wsURL := "ws" + strings.TrimPrefix(ts.URL, "http") + "/api/sessions/test-session/ws?token=test-token"

	conn, _, err := websocket.DefaultDialer.Dial(wsURL, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close()

	// Send a message via WebSocket.
	msg := wsMessage{
		Type: string(bus.EventMessageSend),
		Data: json.RawMessage(`{"text":"hello"}`),
	}
	if err := conn.WriteJSON(msg); err != nil {
		t.Fatal(err)
	}

	// Read events from WebSocket.
	var events []bus.Event
	conn.SetReadDeadline(time.Now().Add(2 * time.Second))
	for {
		var ev bus.Event
		if err := conn.ReadJSON(&ev); err != nil {
			break
		}
		events = append(events, ev)
		if ev.Type == bus.EventTurnComplete {
			break
		}
	}

	if len(events) < 3 {
		t.Errorf("got %d events, want at least 3 (turn.start, text.delta, turn.complete)", len(events))
	}

	// Verify event types.
	expectedTypes := []bus.EventType{bus.EventTurnStart, bus.EventTextDelta, bus.EventTurnComplete}
	for i, expected := range expectedTypes {
		if i < len(events) && events[i].Type != expected {
			t.Errorf("event[%d].Type = %q, want %q", i, events[i].Type, expected)
		}
	}
}

func TestWebSocketAuth(t *testing.T) {
	_, ts := newTestServer(t)

	// Without token — should fail.
	wsURL := "ws" + strings.TrimPrefix(ts.URL, "http") + "/api/sessions/test-session/ws"
	_, resp, err := websocket.DefaultDialer.Dial(wsURL, nil)
	if err == nil {
		t.Error("expected WebSocket connection to fail without token")
	}
	if resp != nil && resp.StatusCode != http.StatusUnauthorized {
		t.Errorf("got status %d, want %d", resp.StatusCode, http.StatusUnauthorized)
	}
}

func TestGetStatus(t *testing.T) {
	_, ts := newTestServer(t)

	req, _ := http.NewRequest("GET", ts.URL+"/api/status", nil)
	req.Header.Set("Authorization", "Bearer test-token")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()

	var status service.StatusInfo
	if err := json.NewDecoder(resp.Body).Decode(&status); err != nil {
		t.Fatal(err)
	}
	if status.Model != "test-model" {
		t.Errorf("got model %q, want %q", status.Model, "test-model")
	}
}
