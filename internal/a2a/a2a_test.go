package a2a

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestAgentCard_Serialization(t *testing.T) {
	card := &AgentCard{
		Name:        "test-agent",
		Description: "A test agent",
		URL:         "http://localhost:8080",
		Version:     "1.0.0",
		Skills: []AgentSkill{
			{Name: "code-review", Description: "Reviews code for quality"},
		},
		Capabilities: Capabilities{
			Streaming:        false,
			StateTransitions: true,
		},
		InputModes:      []string{"text/plain"},
		OutputModes:     []string{"text/plain"},
		ProtocolVersion: "0.1",
	}

	data, err := json.Marshal(card)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}

	var loaded AgentCard
	if err := json.Unmarshal(data, &loaded); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}

	if loaded.Name != "test-agent" {
		t.Fatalf("expected test-agent, got %s", loaded.Name)
	}
	if len(loaded.Skills) != 1 {
		t.Fatalf("expected 1 skill, got %d", len(loaded.Skills))
	}
}

func TestHandler_AgentCard(t *testing.T) {
	card := &AgentCard{
		Name:            "test",
		Description:     "test agent",
		ProtocolVersion: "0.1",
		InputModes:      []string{"text/plain"},
		OutputModes:     []string{"text/plain"},
	}

	handler := NewHandler(card, nil)
	mux := http.NewServeMux()
	handler.RegisterRoutes(mux)

	req := httptest.NewRequest(http.MethodGet, "/.well-known/agent-card.json", nil)
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", w.Code)
	}

	var result AgentCard
	if err := json.NewDecoder(w.Body).Decode(&result); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if result.Name != "test" {
		t.Fatalf("expected test, got %s", result.Name)
	}
}

func TestHandler_TaskSend(t *testing.T) {
	executor := func(_ context.Context, message, ctx string) (string, error) {
		return "processed: " + message, nil
	}

	card := &AgentCard{Name: "test"}
	handler := NewHandler(card, executor)
	mux := http.NewServeMux()
	handler.RegisterRoutes(mux)

	taskReq := TaskRequest{
		ID:      "task-1",
		Message: "hello",
		Context: "test context",
	}
	body, _ := json.Marshal(taskReq)

	req := httptest.NewRequest(http.MethodPost, "/a2a/tasks/send", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", w.Code)
	}

	var resp TaskResponse
	if err := json.NewDecoder(w.Body).Decode(&resp); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if resp.Status != TaskStatusCompleted {
		t.Fatalf("expected completed, got %s", resp.Status)
	}
	if resp.Output != "processed: hello" {
		t.Fatalf("unexpected output: %s", resp.Output)
	}
	if resp.ID != "task-1" {
		t.Fatalf("expected task-1, got %s", resp.ID)
	}
}

func TestHandler_TaskSend_InvalidJSON(t *testing.T) {
	card := &AgentCard{Name: "test"}
	handler := NewHandler(card, nil)
	mux := http.NewServeMux()
	handler.RegisterRoutes(mux)

	req := httptest.NewRequest(http.MethodPost, "/a2a/tasks/send", strings.NewReader("not json"))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)

	if w.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d", w.Code)
	}
}

func TestClient_FetchCard(t *testing.T) {
	card := &AgentCard{
		Name:            "remote-agent",
		Description:     "A remote agent",
		ProtocolVersion: "0.1",
		InputModes:      []string{"text/plain"},
		OutputModes:     []string{"text/plain"},
	}

	// Create a test server.
	handler := NewHandler(card, nil)
	mux := http.NewServeMux()
	handler.RegisterRoutes(mux)
	server := httptest.NewServer(mux)
	defer server.Close()

	client := NewClient(server.URL, nil)
	fetched, err := client.FetchCard(context.Background())
	if err != nil {
		t.Fatalf("fetch card: %v", err)
	}
	if fetched.Name != "remote-agent" {
		t.Fatalf("expected remote-agent, got %s", fetched.Name)
	}
}

func TestClient_SendTask(t *testing.T) {
	executor := func(_ context.Context, message, _ string) (string, error) {
		return "done: " + message, nil
	}

	card := &AgentCard{Name: "worker"}
	handler := NewHandler(card, executor)
	mux := http.NewServeMux()
	handler.RegisterRoutes(mux)
	server := httptest.NewServer(mux)
	defer server.Close()

	client := NewClient(server.URL, nil)
	resp, err := client.SendTask(context.Background(), &TaskRequest{
		ID:      "t-1",
		Message: "do stuff",
	})
	if err != nil {
		t.Fatalf("send task: %v", err)
	}
	if resp.Status != TaskStatusCompleted {
		t.Fatalf("expected completed, got %s", resp.Status)
	}
	if resp.Output != "done: do stuff" {
		t.Fatalf("unexpected output: %s", resp.Output)
	}
}

func TestClient_Auth_Bearer(t *testing.T) {
	var receivedAuth string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		receivedAuth = r.Header.Get("Authorization")
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(&AgentCard{Name: "authed"})
	}))
	defer server.Close()

	client := NewClient(server.URL, &AuthConfig{
		Type:  "bearer",
		Token: "my-token",
	})

	_, err := client.FetchCard(context.Background())
	if err != nil {
		t.Fatalf("fetch: %v", err)
	}
	if receivedAuth != "Bearer my-token" {
		t.Fatalf("expected Bearer auth, got %q", receivedAuth)
	}
}

func TestClient_Auth_APIKey(t *testing.T) {
	var receivedKey string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		receivedKey = r.Header.Get("X-API-Key")
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(&AgentCard{Name: "keyed"})
	}))
	defer server.Close()

	client := NewClient(server.URL, &AuthConfig{
		Type:  "api_key",
		Token: "secret-key",
	})

	_, err := client.FetchCard(context.Background())
	if err != nil {
		t.Fatalf("fetch: %v", err)
	}
	if receivedKey != "secret-key" {
		t.Fatalf("expected api key, got %q", receivedKey)
	}
}

func TestTaskStatus_Constants(t *testing.T) {
	statuses := []TaskStatus{
		TaskStatusPending,
		TaskStatusRunning,
		TaskStatusCompleted,
		TaskStatusFailed,
		TaskStatusCancelled,
	}
	for _, s := range statuses {
		if string(s) == "" {
			t.Fatal("empty status constant")
		}
	}
}
