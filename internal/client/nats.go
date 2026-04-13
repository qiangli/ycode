package client

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"sync"
	"time"

	"github.com/nats-io/nats.go"

	natsBus "github.com/qiangli/ycode/internal/bus"
	"github.com/qiangli/ycode/internal/runtime/config"
	"github.com/qiangli/ycode/internal/service"
)

// NATSClient implements Client over NATS for remote agent interaction.
type NATSClient struct {
	conn      *nats.Conn
	sessionID string
	bus       *natsBus.NATSBus
	logger    *slog.Logger

	subMu       sync.RWMutex
	subscribers []chan natsBus.Event
}

// NewNATSClient creates a NATS client for remote agent interaction.
func NewNATSClient(url, sessionID string, opts ...nats.Option) (*NATSClient, error) {
	defaults := []nats.Option{
		nats.Name("ycode-client"),
		nats.ReconnectWait(2 * time.Second),
		nats.MaxReconnects(-1),
	}
	opts = append(defaults, opts...)

	conn, err := nats.Connect(url, opts...)
	if err != nil {
		return nil, fmt.Errorf("connect to NATS: %w", err)
	}

	b := natsBus.NewNATSBus(conn)

	c := &NATSClient{
		conn:      conn,
		sessionID: sessionID,
		bus:       b,
		logger:    slog.Default(),
	}

	// Subscribe to session events and fan out.
	go c.eventLoop()

	return c, nil
}

func (c *NATSClient) eventLoop() {
	ch, unsub := c.bus.Subscribe()
	defer unsub()

	for ev := range ch {
		if ev.SessionID != "" && ev.SessionID != c.sessionID {
			continue
		}
		c.subMu.RLock()
		for _, sub := range c.subscribers {
			select {
			case sub <- ev:
			default:
			}
		}
		c.subMu.RUnlock()
	}
}

// --- Service interface (via NATS pub/sub) ---

func (c *NATSClient) CreateSession(ctx context.Context) (*service.SessionInfo, error) {
	return &service.SessionInfo{ID: c.sessionID}, nil
}

func (c *NATSClient) GetSession(ctx context.Context, id string) (*service.SessionInfo, error) {
	return &service.SessionInfo{ID: id}, nil
}

// ListSessions is handled via NATS RPC (defined below with other RPC methods).

func (c *NATSClient) GetMessages(ctx context.Context, sessionID string) ([]json.RawMessage, error) {
	return nil, nil // Not supported over basic NATS pub/sub.
}

func (c *NATSClient) SendMessage(ctx context.Context, sessionID string, input service.MessageInput) error {
	return c.bus.PublishInput(c.sessionID, natsBus.Event{
		Type: natsBus.EventMessageSend,
		Data: mustJSON(input),
	})
}

func (c *NATSClient) CancelTurn(ctx context.Context, sessionID string) error {
	return c.bus.PublishInput(c.sessionID, natsBus.Event{
		Type: natsBus.EventTurnCancel,
		Data: json.RawMessage(`{}`),
	})
}

func (c *NATSClient) RespondPermission(ctx context.Context, requestID string, allowed bool) error {
	return c.bus.PublishInput(c.sessionID, natsBus.Event{
		Type: natsBus.EventPermissionRes,
		Data: mustJSON(map[string]any{"request_id": requestID, "allowed": allowed}),
	})
}

func (c *NATSClient) GetConfig(ctx context.Context) (*config.Config, error) {
	var cfg config.Config
	if err := c.rpcRequest("config", nil, &cfg); err != nil {
		return nil, err
	}
	return &cfg, nil
}

func (c *NATSClient) SwitchModel(ctx context.Context, model string) error {
	return fmt.Errorf("SwitchModel not supported over NATS")
}

func (c *NATSClient) GetStatus(ctx context.Context) (*service.StatusInfo, error) {
	var status service.StatusInfo
	if err := c.rpcRequest("status", nil, &status); err != nil {
		return nil, err
	}
	return &status, nil
}

func (c *NATSClient) ExecuteCommand(ctx context.Context, name string, args string) (string, error) {
	return "", fmt.Errorf("ExecuteCommand not supported over NATS")
}

// rpcRequest sends a NATS request/reply message and decodes the response.
func (c *NATSClient) rpcRequest(operation string, body any, result any) error {
	subject := "ycode.rpc." + operation
	var payload []byte
	if body != nil {
		var err error
		payload, err = json.Marshal(body)
		if err != nil {
			return err
		}
	}
	msg, err := c.conn.Request(subject, payload, 5*time.Second)
	if err != nil {
		return fmt.Errorf("NATS RPC %s: %w", operation, err)
	}
	// Check for error response.
	var errResp struct {
		Error string `json:"error"`
	}
	if json.Unmarshal(msg.Data, &errResp) == nil && errResp.Error != "" {
		return fmt.Errorf("NATS RPC %s: %s", operation, errResp.Error)
	}
	return json.Unmarshal(msg.Data, result)
}

func (c *NATSClient) ListSessions(ctx context.Context) ([]service.SessionInfo, error) {
	var sessions []service.SessionInfo
	if err := c.rpcRequest("sessions", nil, &sessions); err != nil {
		return nil, err
	}
	return sessions, nil
}

func (c *NATSClient) Bus() natsBus.Bus {
	return c.bus
}

// Events returns a channel of events from NATS, optionally filtered.
func (c *NATSClient) Events(ctx context.Context, filter ...natsBus.EventType) (<-chan natsBus.Event, error) {
	ch := make(chan natsBus.Event, 256)

	filterSet := make(map[natsBus.EventType]struct{})
	for _, f := range filter {
		filterSet[f] = struct{}{}
	}

	rawCh := make(chan natsBus.Event, 256)
	c.subMu.Lock()
	c.subscribers = append(c.subscribers, rawCh)
	c.subMu.Unlock()

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

// Close closes the NATS connection.
func (c *NATSClient) Close() error {
	c.subMu.Lock()
	for _, ch := range c.subscribers {
		close(ch)
	}
	c.subscribers = nil
	c.subMu.Unlock()

	c.bus.Close()
	c.conn.Close()
	return nil
}

func mustJSON(v any) json.RawMessage {
	data, _ := json.Marshal(v)
	return data
}
