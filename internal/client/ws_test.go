package client

import (
	"context"
	"encoding/json"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/qiangli/ycode/internal/bus"
	"github.com/qiangli/ycode/internal/runtime/config"
	"github.com/qiangli/ycode/internal/server"
	"github.com/qiangli/ycode/internal/service"
)

// mockService for testing.
type mockService struct {
	b bus.Bus
}

func (m *mockService) Bus() bus.Bus { return m.b }
func (m *mockService) CreateSession(ctx context.Context) (*service.SessionInfo, error) {
	return &service.SessionInfo{ID: "test-session"}, nil
}
func (m *mockService) GetSession(ctx context.Context, id string) (*service.SessionInfo, error) {
	return &service.SessionInfo{ID: id}, nil
}
func (m *mockService) ListSessions(ctx context.Context) ([]service.SessionInfo, error) {
	return []service.SessionInfo{{ID: "test-session"}}, nil
}
func (m *mockService) GetMessages(ctx context.Context, sid string) ([]json.RawMessage, error) {
	return nil, nil
}
func (m *mockService) SendMessage(ctx context.Context, sid string, input service.MessageInput) error {
	m.b.Publish(bus.Event{Type: bus.EventTurnStart, SessionID: sid, Data: json.RawMessage(`{}`)})
	m.b.Publish(bus.Event{Type: bus.EventTextDelta, SessionID: sid, Data: json.RawMessage(`{"text":"hi"}`)})
	m.b.Publish(bus.Event{Type: bus.EventTurnComplete, SessionID: sid, Data: json.RawMessage(`{"status":"complete"}`)})
	return nil
}
func (m *mockService) CancelTurn(ctx context.Context, sid string) error { return nil }
func (m *mockService) RespondPermission(ctx context.Context, rid string, allowed bool) error {
	return nil
}
func (m *mockService) GetConfig(ctx context.Context) (*config.Config, error) {
	return &config.Config{Model: "test-model"}, nil
}
func (m *mockService) SwitchModel(ctx context.Context, model string) error { return nil }
func (m *mockService) GetStatus(ctx context.Context) (*service.StatusInfo, error) {
	return &service.StatusInfo{Model: "test-model", SessionID: "test-session"}, nil
}
func (m *mockService) ExecuteCommand(ctx context.Context, name, args string) (string, error) {
	return "ok", nil
}

func TestWSClient_SendAndReceive(t *testing.T) {
	memBus := bus.NewMemoryBus()
	t.Cleanup(func() { memBus.Close() })

	svc := &mockService{b: memBus}
	srv := server.New(server.Config{Token: "test-token"}, svc)

	ts := httptest.NewServer(srv.Mux())
	t.Cleanup(ts.Close)

	baseURL := "http" + strings.TrimPrefix(ts.URL, "http")

	wsClient := NewWSClient(baseURL, "test-token", "test-session")
	ctx := context.Background()
	if err := wsClient.Connect(ctx); err != nil {
		t.Fatal(err)
	}
	defer wsClient.Close()

	// Subscribe to events.
	evCh, err := wsClient.Events(ctx)
	if err != nil {
		t.Fatal(err)
	}

	// Send message.
	err = wsClient.SendMessage(ctx, "test-session", service.MessageInput{Text: "hello"})
	if err != nil {
		t.Fatal(err)
	}

	// Collect events.
	var events []bus.Event
	timeout := time.After(3 * time.Second)
	for {
		select {
		case ev := <-evCh:
			events = append(events, ev)
			if ev.Type == bus.EventTurnComplete {
				goto done
			}
		case <-timeout:
			t.Fatalf("timed out, got %d events", len(events))
		}
	}
done:

	if len(events) < 3 {
		t.Errorf("got %d events, want at least 3", len(events))
	}
}

func TestWSClient_REST(t *testing.T) {
	memBus := bus.NewMemoryBus()
	t.Cleanup(func() { memBus.Close() })

	svc := &mockService{b: memBus}
	srv := server.New(server.Config{Token: "test-token"}, svc)

	ts := httptest.NewServer(srv.Mux())
	t.Cleanup(ts.Close)

	baseURL := "http" + strings.TrimPrefix(ts.URL, "http")
	wsClient := NewWSClient(baseURL, "test-token", "test-session")

	ctx := context.Background()

	// Test GetStatus.
	status, err := wsClient.GetStatus(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if status.Model != "test-model" {
		t.Errorf("got model %q, want %q", status.Model, "test-model")
	}

	// Test ListSessions.
	sessions, err := wsClient.ListSessions(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if len(sessions) != 1 {
		t.Errorf("got %d sessions, want 1", len(sessions))
	}
}
