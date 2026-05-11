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

	"github.com/qiangli/ycode/internal/api"
	"github.com/qiangli/ycode/internal/bus"
	"github.com/qiangli/ycode/internal/runtime/config"
	"github.com/qiangli/ycode/internal/service"
	"github.com/qiangli/ycode/pkg/ycode/actor"
)

// mockService implements service.Service for testing.
type mockService struct {
	b              bus.Bus
	lastCreateOpts *service.SessionOptions // for round-trip assertions
}

func (m *mockService) Bus() bus.Bus { return m.b }
func (m *mockService) CreateSession(ctx context.Context) (*service.SessionInfo, error) {
	info := &service.SessionInfo{ID: "test-session", MessageCount: 0}
	if opts, ok := ctx.Value(service.CtxSessionOptions).(service.SessionOptions); ok && !opts.IsZero() {
		copy := opts
		info.Options = &copy
		m.lastCreateOpts = &copy
	}
	return info, nil
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
func (m *mockService) ListModels(ctx context.Context) ([]api.ModelInfo, error) {
	return []api.ModelInfo{
		{ID: "claude-sonnet-4-6-20250514", Alias: "sonnet", Provider: "anthropic", Source: "builtin"},
		{ID: "gpt-4.1", Provider: "openai", Source: "env"},
		{ID: "llama3.2:3b", Provider: "ollama", Source: "ollama", Size: "2.0 GB"},
	}, nil
}
func (m *mockService) ExecuteCommand(ctx context.Context, name string, args string) (string, error) {
	return "ok", nil
}
func (m *mockService) LookupApp(ctx context.Context, workDir string) (service.AppBackend, error) {
	return nil, nil
}

func newTestServer(t *testing.T) (*Server, *httptest.Server) {
	t.Helper()
	// Token left empty so existing tests can hit endpoints without a
	// header. Auth-specific tests construct their own server with a token.
	return newTestServerWithToken(t, "")
}

func newTestServerWithToken(t *testing.T, token string) (*Server, *httptest.Server) {
	t.Helper()
	memBus := bus.NewMemoryBus()
	t.Cleanup(func() { memBus.Close() })

	svc := &mockService{b: memBus}
	srv := New(Config{Token: token}, svc)

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

func TestAuthMiddlewareNoToken(t *testing.T) {
	// Token == "" → middleware is permissive, no header required.
	_, ts := newTestServerWithToken(t, "")

	resp, err := http.Get(ts.URL + "/api/config")
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Errorf("permissive mode: got status %d, want %d", resp.StatusCode, http.StatusOK)
	}
}

func TestAuthMiddlewareWithToken(t *testing.T) {
	const token = "secret-token-xyz"
	_, ts := newTestServerWithToken(t, token)

	cases := []struct {
		name       string
		header     string
		wantStatus int
	}{
		{"NoHeader", "", http.StatusUnauthorized},
		{"WrongToken", "Bearer nope", http.StatusUnauthorized},
		{"WrongScheme", "Basic " + token, http.StatusUnauthorized},
		{"CorrectToken", "Bearer " + token, http.StatusOK},
		{"CaseInsensitiveScheme", "bearer " + token, http.StatusOK},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			req, _ := http.NewRequest("GET", ts.URL+"/api/config", nil)
			if tc.header != "" {
				req.Header.Set("Authorization", tc.header)
			}
			resp, err := http.DefaultClient.Do(req)
			if err != nil {
				t.Fatal(err)
			}
			resp.Body.Close()
			if resp.StatusCode != tc.wantStatus {
				t.Errorf("got status %d, want %d", resp.StatusCode, tc.wantStatus)
			}
		})
	}

	// /api/health is always reachable, even without a token.
	resp, err := http.Get(ts.URL + "/api/health")
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Errorf("health: got status %d, want %d", resp.StatusCode, http.StatusOK)
	}
}

// TestActorHeaderDecoding verifies that X-Actor-* headers are decoded into
// an actor.User on the request context, but only after the bearer check
// passes.
func TestActorHeaderDecoding(t *testing.T) {
	const token = "abc123"

	memBus := bus.NewMemoryBus()
	t.Cleanup(func() { memBus.Close() })
	svc := &mockService{b: memBus}
	srv := New(Config{Token: token}, svc)

	// Install a probe handler that captures the actor.User off the
	// request context. It rides the same authMiddleware as production
	// handlers because we register it through registerRoutes — to keep
	// this self-contained, register it directly with authMiddleware here.
	var captured actor.User
	var ok bool
	srv.mux.HandleFunc("GET /api/_probe_actor", srv.authMiddleware(func(w http.ResponseWriter, r *http.Request) {
		captured, ok = actor.UserFromContext(r.Context())
		w.WriteHeader(http.StatusOK)
	}))

	ts := httptest.NewServer(srv.Mux())
	t.Cleanup(ts.Close)

	req, _ := http.NewRequest("GET", ts.URL+"/api/_probe_actor", nil)
	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set("X-Actor-User", "parent_47")
	req.Header.Set("X-Actor-Email", "parent@example.com")
	req.Header.Set("X-Actor-Roles", "parent, reader")
	req.Header.Set("X-Actor-Extra-Tenant", "school-12")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("got status %d, want %d", resp.StatusCode, http.StatusOK)
	}
	if !ok {
		t.Fatal("expected actor.User on request context")
	}
	if captured.ID != "parent_47" {
		t.Errorf("ID = %q, want %q", captured.ID, "parent_47")
	}
	if captured.Email != "parent@example.com" {
		t.Errorf("Email = %q, want %q", captured.Email, "parent@example.com")
	}
	if len(captured.Roles) != 2 || captured.Roles[0] != "parent" || captured.Roles[1] != "reader" {
		t.Errorf("Roles = %v, want [parent reader]", captured.Roles)
	}
	if captured.Extra["tenant"] != "school-12" {
		t.Errorf("Extra[tenant] = %q, want %q", captured.Extra["tenant"], "school-12")
	}
}

// TestActorHeaderRequiresBearer verifies that an unauthenticated request
// cannot stamp identity onto the context — auth must precede identity.
func TestActorHeaderRequiresBearer(t *testing.T) {
	const token = "abc123"
	_, ts := newTestServerWithToken(t, token)

	// Bearer missing — the X-Actor-User header should be ignored because
	// the request is rejected with 401 before any handler can read it.
	req, _ := http.NewRequest("GET", ts.URL+"/api/config", nil)
	req.Header.Set("X-Actor-User", "spoof_user")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusUnauthorized {
		t.Errorf("got status %d, want %d", resp.StatusCode, http.StatusUnauthorized)
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
	const token = "ws-token"
	_, ts := newTestServerWithToken(t, token)
	wsBase := "ws" + strings.TrimPrefix(ts.URL, "http") + "/api/sessions/test-session/ws"

	t.Run("RejectedWithoutToken", func(t *testing.T) {
		conn, resp, err := websocket.DefaultDialer.Dial(wsBase, nil)
		if err == nil {
			conn.Close()
			t.Fatal("expected WebSocket dial without token to fail")
		}
		if resp == nil || resp.StatusCode != http.StatusUnauthorized {
			t.Errorf("got resp=%v, want 401", resp)
		}
	})

	t.Run("AcceptedWithQueryToken", func(t *testing.T) {
		conn, _, err := websocket.DefaultDialer.Dial(wsBase+"?token="+token, nil)
		if err != nil {
			t.Fatalf("expected WebSocket dial with valid query token to succeed, got: %v", err)
		}
		conn.Close()
	})

	t.Run("AcceptedWithBearerHeader", func(t *testing.T) {
		conn, _, err := websocket.DefaultDialer.Dial(wsBase, http.Header{
			"Authorization": []string{"Bearer " + token},
		})
		if err != nil {
			t.Fatalf("expected WebSocket dial with Bearer header to succeed, got: %v", err)
		}
		conn.Close()
	})
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

func TestListModels(t *testing.T) {
	_, ts := newTestServer(t)

	t.Run("ReturnsJSONArray", func(t *testing.T) {
		resp, err := http.Get(ts.URL + "/api/models")
		if err != nil {
			t.Fatal(err)
		}
		defer resp.Body.Close()

		if resp.StatusCode != http.StatusOK {
			t.Fatalf("got status %d, want %d", resp.StatusCode, http.StatusOK)
		}
		if ct := resp.Header.Get("Content-Type"); !strings.Contains(ct, "application/json") {
			t.Errorf("got Content-Type %q, want application/json", ct)
		}

		var models []api.ModelInfo
		if err := json.NewDecoder(resp.Body).Decode(&models); err != nil {
			t.Fatalf("decode response: %v", err)
		}
		if len(models) != 3 {
			t.Fatalf("got %d models, want 3", len(models))
		}
	})

	t.Run("ContainsExpectedFields", func(t *testing.T) {
		resp, err := http.Get(ts.URL + "/api/models")
		if err != nil {
			t.Fatal(err)
		}
		defer resp.Body.Close()

		var models []api.ModelInfo
		json.NewDecoder(resp.Body).Decode(&models)

		for _, m := range models {
			if m.ID == "" {
				t.Error("model.id must not be empty")
			}
			if m.Provider == "" {
				t.Error("model.provider must not be empty")
			}
			if m.Source == "" {
				t.Error("model.source must not be empty")
			}
		}
	})

	t.Run("IncludesAllSources", func(t *testing.T) {
		resp, err := http.Get(ts.URL + "/api/models")
		if err != nil {
			t.Fatal(err)
		}
		defer resp.Body.Close()

		var models []api.ModelInfo
		json.NewDecoder(resp.Body).Decode(&models)

		sources := make(map[string]bool)
		for _, m := range models {
			sources[m.Source] = true
		}
		for _, want := range []string{"builtin", "env", "ollama"} {
			if !sources[want] {
				t.Errorf("expected source %q in response, got sources: %v", want, sources)
			}
		}
	})

	t.Run("OllamaModelHasSize", func(t *testing.T) {
		resp, err := http.Get(ts.URL + "/api/models")
		if err != nil {
			t.Fatal(err)
		}
		defer resp.Body.Close()

		var models []api.ModelInfo
		json.NewDecoder(resp.Body).Decode(&models)

		for _, m := range models {
			if m.Source == "ollama" && m.Size == "" {
				t.Errorf("ollama model %q should have a size", m.ID)
			}
		}
	})

	t.Run("BuiltinModelHasAlias", func(t *testing.T) {
		resp, err := http.Get(ts.URL + "/api/models")
		if err != nil {
			t.Fatal(err)
		}
		defer resp.Body.Close()

		var models []api.ModelInfo
		json.NewDecoder(resp.Body).Decode(&models)

		for _, m := range models {
			if m.Source == "builtin" && m.Alias == "" {
				t.Errorf("builtin model %q should have an alias", m.ID)
			}
		}
	})
}
