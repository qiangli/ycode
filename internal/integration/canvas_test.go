//go:build integration

package integration

import (
	"encoding/json"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/gorilla/websocket"
)

// canvasBearer reads the server bearer token from ~/.agents/ycode/server.token,
// honoring the YCODE_TOKEN env var override. Empty string when neither is set
// (a server in no-auth mode accepts that fine).
func canvasBearer(t *testing.T) string {
	t.Helper()
	if v := os.Getenv("YCODE_TOKEN"); v != "" {
		return v
	}
	home, _ := os.UserHomeDir()
	data, err := os.ReadFile(filepath.Join(home, ".agents", "ycode", "server.token"))
	if err != nil {
		return ""
	}
	return strings.TrimSpace(string(data))
}

// canvasWSURL builds the canvas WS URL with the bearer token appended as the
// `?token=` query param (the WS handshake can't carry an Authorization header
// in-browser, so the server accepts the token via query string).
func canvasWSURL(t *testing.T, sessionID string) string {
	t.Helper()
	base := "ws" + strings.TrimPrefix(baseURL(t), "http") + "/ycode/api/sessions/" + sessionID + "/ws"
	if tok := canvasBearer(t); tok != "" {
		return base + "?token=" + tok
	}
	return base
}

// canvasMCPCall posts a JSONRPC tool call to the composite /mcp/ endpoint with
// the bearer token if one is available. Returns the parsed response.
func canvasMCPCall(t *testing.T, req jsonrpcRequest) jsonrpcResponse {
	t.Helper()
	tok := canvasBearer(t)
	body, err := json.Marshal(req)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	httpReq, err := http.NewRequest("POST", baseURL(t)+"/mcp/", strings.NewReader(string(body)))
	if err != nil {
		t.Fatal(err)
	}
	httpReq.Header.Set("Content-Type", "application/json")
	if tok != "" {
		httpReq.Header.Set("Authorization", "Bearer "+tok)
	}
	resp, err := httpClient().Do(httpReq)
	if err != nil {
		t.Fatalf("mcp POST: %v", err)
	}
	defer resp.Body.Close()
	rb, _ := readBody(resp)
	if resp.StatusCode != 200 {
		t.Fatalf("mcp POST returned %d; body: %s", resp.StatusCode, rb)
	}
	var out jsonrpcResponse
	if err := json.Unmarshal([]byte(rb), &out); err != nil {
		t.Fatalf("unmarshal: %v; body: %s", err, rb)
	}
	return out
}

// TestCanvas_WidgetRoundTrip verifies the full agent-OS canvas chain
// against a running ycode serve instance:
//
//  1. Open a WebSocket on /api/sessions/canvas-default/ws.
//  2. Call the agent_render_widget MCP tool over JSONRPC at /mcp/.
//  3. Assert a state.update event arrives over the WS within timeout,
//     carrying the same widget_id and html the call sent.
//
// Run prerequisite: `make deploy` (or `bin/ycode serve` running locally).
// Skips cleanly if the server isn't reachable.
func TestCanvas_WidgetRoundTrip(t *testing.T) {
	requireConnectivity(t)

	// 1. Open WS first so we don't race the MCP publish.
	wsURL := canvasWSURL(t, "canvas-default")
	dialer := websocket.Dialer{HandshakeTimeout: 5 * time.Second}
	conn, resp, err := dialer.Dial(wsURL, nil)
	if err != nil {
		t.Fatalf("dial %s: %v (resp=%v)", wsURL, err, resp)
	}
	defer conn.Close()

	// Read pump — surface every text message on a channel.
	type wsMsg struct {
		Type      string          `json:"type"`
		SessionID string          `json:"session_id"`
		Data      json.RawMessage `json:"data"`
	}
	recv := make(chan wsMsg, 32)
	go func() {
		defer close(recv)
		for {
			_, raw, err := conn.ReadMessage()
			if err != nil {
				return
			}
			var m wsMsg
			if err := json.Unmarshal(raw, &m); err == nil {
				recv <- m
			}
		}
	}()

	// 2. Call agent_render_widget via the composite MCP JSONRPC endpoint.
	const widgetID = "canvas-it-roundtrip"
	const htmlBody = `<p>hello canvas</p>`
	mcpURL := baseURL(t) + "/mcp/"
	req := jsonrpcRequest{
		JSONRPC: "2.0",
		ID:      1,
		Method:  "tools/call",
		Params: map[string]any{
			"name": "agent_render_widget",
			"arguments": map[string]any{
				"widget_id": widgetID,
				"html":      htmlBody,
				// Omit session_id — handler defaults to canvas-default,
				// which is exactly the WS we're subscribed on.
			},
		},
	}
	mcpResp := canvasMCPCall(t, req)
	_ = mcpURL // kept for readability; canvasMCPCall constructs its own URL
	if mcpResp.Error != nil {
		t.Fatalf("mcp tools/call returned error: %d %s", mcpResp.Error.Code, mcpResp.Error.Message)
	}

	// 3. Wait for the state.update event carrying our payload.
	deadline := time.After(5 * time.Second)
	for {
		select {
		case <-deadline:
			t.Fatalf("no state.update for widget %q within 5s", widgetID)
		case m, ok := <-recv:
			if !ok {
				t.Fatal("WS closed before state.update arrived")
			}
			if m.Type != "state.update" {
				continue // ignore other event types (ping, session.update, etc.)
			}
			var payload struct {
				Format   string `json:"format"`
				WidgetID string `json:"widget_id"`
				HTML     string `json:"html"`
				Origin   string `json:"origin,omitempty"`
			}
			if err := json.Unmarshal(m.Data, &payload); err != nil {
				t.Fatalf("decode payload: %v; raw=%s", err, m.Data)
			}
			if payload.WidgetID != widgetID {
				// Could be a leftover event from a parallel test — keep reading.
				continue
			}
			if payload.Format != "iframe" {
				t.Errorf("format = %q want iframe", payload.Format)
			}
			if payload.HTML != htmlBody {
				t.Errorf("html not preserved verbatim: got %q want %q", payload.HTML, htmlBody)
			}
			if m.SessionID != "canvas-default" {
				t.Errorf("session_id = %q want canvas-default", m.SessionID)
			}
			return // happy path
		}
	}
}

// TestCanvas_A2UIRoundTrip mirrors the widget test for A2UI ops.
// Asserts that agent_render_a2ui publishes a state.update with the
// expected format discriminator and the original op array preserved.
func TestCanvas_A2UIRoundTrip(t *testing.T) {
	requireConnectivity(t)

	wsURL := canvasWSURL(t, "canvas-default")
	dialer := websocket.Dialer{HandshakeTimeout: 5 * time.Second}
	conn, _, err := dialer.Dial(wsURL, nil)
	if err != nil {
		t.Fatalf("dial: %v", err)
	}
	defer conn.Close()

	type wsMsg struct {
		Type string          `json:"type"`
		Data json.RawMessage `json:"data"`
	}
	recv := make(chan wsMsg, 32)
	go func() {
		defer close(recv)
		for {
			_, raw, err := conn.ReadMessage()
			if err != nil {
				return
			}
			var m wsMsg
			if json.Unmarshal(raw, &m) == nil {
				recv <- m
			}
		}
	}()

	surfaceID := "it-test-surface"
	mcpURL := baseURL(t) + "/mcp/"
	req := jsonrpcRequest{
		JSONRPC: "2.0",
		ID:      2,
		Method:  "tools/call",
		Params: map[string]any{
			"name": "agent_render_a2ui",
			"arguments": map[string]any{
				"ops": []map[string]any{
					{"version": "v0.9", "createSurface": map[string]any{"surfaceId": surfaceID, "catalogId": "test"}},
					{"version": "v0.9", "updateDataModel": map[string]any{"surfaceId": surfaceID, "path": "/", "value": map[string]any{"x": 1}}},
				},
			},
		},
	}
	if mcpResp := canvasMCPCall(t, req); mcpResp.Error != nil {
		t.Fatalf("mcp error: %d %s", mcpResp.Error.Code, mcpResp.Error.Message)
	}
	_ = mcpURL // kept for readability; canvasMCPCall builds its own URL

	deadline := time.After(5 * time.Second)
	for {
		select {
		case <-deadline:
			t.Fatalf("no a2ui state.update for surface %q within 5s", surfaceID)
		case m, ok := <-recv:
			if !ok {
				t.Fatal("WS closed before a2ui event")
			}
			if m.Type != "state.update" {
				continue
			}
			s := string(m.Data)
			if !strings.Contains(s, `"format":"a2ui"`) {
				continue // unrelated state.update
			}
			if !strings.Contains(s, surfaceID) {
				continue
			}
			// A2UI body should carry the OperationsKey container.
			if !strings.Contains(s, "a2ui_operations") {
				t.Errorf("a2ui payload missing operations wrapper: %s", s)
			}
			return
		}
	}
}
