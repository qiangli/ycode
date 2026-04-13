package client

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"sync"
	"time"

	"github.com/gorilla/websocket"

	"github.com/qiangli/ycode/internal/bus"
	"github.com/qiangli/ycode/internal/runtime/config"
	"github.com/qiangli/ycode/internal/service"
)

// WSClient implements Client over WebSocket for conversation and REST for CRUD.
type WSClient struct {
	baseURL string // e.g. "http://127.0.0.1:58090"
	token   string

	sessionID string
	conn      *websocket.Conn
	connMu    sync.Mutex

	// Event fan-out to subscribers.
	busMu       sync.RWMutex
	subscribers []chan bus.Event

	logger *slog.Logger
	done   chan struct{}
}

// NewWSClient creates a WebSocket client connecting to the ycode API server.
func NewWSClient(baseURL, token, sessionID string) *WSClient {
	return &WSClient{
		baseURL:   baseURL,
		token:     token,
		sessionID: sessionID,
		logger:    slog.Default(),
		done:      make(chan struct{}),
	}
}

// Connect establishes the WebSocket connection and starts the read loop.
func (c *WSClient) Connect(ctx context.Context) error {
	wsURL := fmt.Sprintf("ws%s/api/sessions/%s/ws?token=%s",
		c.baseURL[4:], // strip "http" prefix, keep "s" if https
		c.sessionID, c.token)

	conn, _, err := websocket.DefaultDialer.DialContext(ctx, wsURL, nil)
	if err != nil {
		return fmt.Errorf("websocket connect: %w", err)
	}
	c.conn = conn

	go c.readLoop()
	return nil
}

// readLoop reads events from WebSocket and fans out to subscribers.
func (c *WSClient) readLoop() {
	defer close(c.done)

	for {
		_, raw, err := c.conn.ReadMessage()
		if err != nil {
			if websocket.IsUnexpectedCloseError(err, websocket.CloseGoingAway, websocket.CloseNormalClosure) {
				c.logger.Debug("websocket read error", "error", err)
			}
			// Attempt reconnect.
			c.reconnect()
			return
		}

		var ev bus.Event
		if err := json.Unmarshal(raw, &ev); err != nil {
			c.logger.Error("invalid event from server", "error", err)
			continue
		}

		c.fanOut(ev)
	}
}

func (c *WSClient) fanOut(ev bus.Event) {
	c.busMu.RLock()
	defer c.busMu.RUnlock()
	for _, ch := range c.subscribers {
		select {
		case ch <- ev:
		default:
		}
	}
}

func (c *WSClient) reconnect() {
	for i := range 10 {
		delay := time.Duration(1<<uint(i)) * 500 * time.Millisecond
		if delay > 10*time.Second {
			delay = 10 * time.Second
		}
		time.Sleep(delay)

		c.logger.Info("reconnecting websocket", "attempt", i+1)
		if err := c.Connect(context.Background()); err == nil {
			c.logger.Info("websocket reconnected")
			return
		}
	}
	c.logger.Error("websocket reconnection failed after 10 attempts")
}

// sendWS sends a JSON message over the WebSocket.
func (c *WSClient) sendWS(msgType string, data any) error {
	c.connMu.Lock()
	defer c.connMu.Unlock()

	payload, err := json.Marshal(data)
	if err != nil {
		return err
	}

	msg := struct {
		Type string          `json:"type"`
		Data json.RawMessage `json:"data"`
	}{
		Type: msgType,
		Data: payload,
	}
	return c.conn.WriteJSON(msg)
}

// restGet makes an authenticated GET request.
func (c *WSClient) restGet(path string, result any) error {
	req, err := http.NewRequest("GET", c.baseURL+path, nil)
	if err != nil {
		return err
	}
	req.Header.Set("Authorization", "Bearer "+c.token)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 400 {
		body, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("HTTP %d: %s", resp.StatusCode, body)
	}
	return json.NewDecoder(resp.Body).Decode(result)
}

// restPost makes an authenticated POST request.
func (c *WSClient) restPost(path string, body, result any) error {
	data, err := json.Marshal(body)
	if err != nil {
		return err
	}
	req, err := http.NewRequest("POST", c.baseURL+path, jsonReader(data))
	if err != nil {
		return err
	}
	req.Header.Set("Authorization", "Bearer "+c.token)
	req.Header.Set("Content-Type", "application/json")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 400 {
		body, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("HTTP %d: %s", resp.StatusCode, body)
	}
	if result != nil {
		return json.NewDecoder(resp.Body).Decode(result)
	}
	return nil
}

// restPut makes an authenticated PUT request.
func (c *WSClient) restPut(path string, body any) error {
	data, err := json.Marshal(body)
	if err != nil {
		return err
	}
	req, err := http.NewRequest("PUT", c.baseURL+path, jsonReader(data))
	if err != nil {
		return err
	}
	req.Header.Set("Authorization", "Bearer "+c.token)
	req.Header.Set("Content-Type", "application/json")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	return nil
}

func jsonReader(data []byte) io.Reader {
	return bytes.NewReader(data)
}

// --- Service interface ---

func (c *WSClient) CreateSession(ctx context.Context) (*service.SessionInfo, error) {
	var info service.SessionInfo
	return &info, c.restPost("/api/sessions", nil, &info)
}

func (c *WSClient) GetSession(ctx context.Context, id string) (*service.SessionInfo, error) {
	var info service.SessionInfo
	return &info, c.restGet("/api/sessions/"+id, &info)
}

func (c *WSClient) ListSessions(ctx context.Context) ([]service.SessionInfo, error) {
	var sessions []service.SessionInfo
	return sessions, c.restGet("/api/sessions", &sessions)
}

func (c *WSClient) GetMessages(ctx context.Context, sessionID string) ([]json.RawMessage, error) {
	var msgs []json.RawMessage
	return msgs, c.restGet("/api/sessions/"+sessionID+"/messages", &msgs)
}

func (c *WSClient) SendMessage(ctx context.Context, sessionID string, input service.MessageInput) error {
	return c.sendWS(string(bus.EventMessageSend), input)
}

func (c *WSClient) CancelTurn(ctx context.Context, sessionID string) error {
	return c.sendWS(string(bus.EventTurnCancel), struct{}{})
}

func (c *WSClient) RespondPermission(ctx context.Context, requestID string, allowed bool) error {
	return c.sendWS(string(bus.EventPermissionRes), map[string]any{
		"request_id": requestID,
		"allowed":    allowed,
	})
}

func (c *WSClient) GetConfig(ctx context.Context) (*config.Config, error) {
	var cfg config.Config
	return &cfg, c.restGet("/api/config", &cfg)
}

func (c *WSClient) SwitchModel(ctx context.Context, model string) error {
	return c.restPut("/api/config/model", map[string]string{"model": model})
}

func (c *WSClient) GetStatus(ctx context.Context) (*service.StatusInfo, error) {
	var status service.StatusInfo
	return &status, c.restGet("/api/status", &status)
}

func (c *WSClient) ExecuteCommand(ctx context.Context, name string, args string) (string, error) {
	var resp struct {
		Result string `json:"result"`
	}
	err := c.restPost("/api/commands/"+name, map[string]string{"args": args}, &resp)
	return resp.Result, err
}

func (c *WSClient) Bus() bus.Bus {
	// WSClient doesn't have a local bus — use Events() instead.
	return nil
}

// Events returns a channel of events from the WebSocket.
func (c *WSClient) Events(ctx context.Context, filter ...bus.EventType) (<-chan bus.Event, error) {
	ch := make(chan bus.Event, 256)

	filterSet := make(map[bus.EventType]struct{})
	for _, f := range filter {
		filterSet[f] = struct{}{}
	}

	// Create a raw subscriber channel.
	rawCh := make(chan bus.Event, 256)
	c.busMu.Lock()
	c.subscribers = append(c.subscribers, rawCh)
	c.busMu.Unlock()

	go func() {
		defer close(ch)
		for {
			select {
			case <-ctx.Done():
				return
			case ev, ok := <-rawCh:
				if !ok {
					return
				}
				if len(filterSet) > 0 {
					if _, match := filterSet[ev.Type]; !match {
						continue
					}
				}
				select {
				case ch <- ev:
				default:
				}
			}
		}
	}()

	return ch, nil
}

// Close closes the WebSocket connection.
func (c *WSClient) Close() error {
	c.busMu.Lock()
	for _, ch := range c.subscribers {
		close(ch)
	}
	c.subscribers = nil
	c.busMu.Unlock()

	if c.conn != nil {
		return c.conn.Close()
	}
	return nil
}
