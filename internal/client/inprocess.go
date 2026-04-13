package client

import (
	"context"
	"encoding/json"

	"github.com/qiangli/ycode/internal/bus"
	"github.com/qiangli/ycode/internal/runtime/config"
	"github.com/qiangli/ycode/internal/service"
)

// InProcessClient implements Client by directly delegating to a Service
// and subscribing to its Bus. Zero network overhead — used when the TUI
// and server run in the same process.
type InProcessClient struct {
	svc service.Service
}

// NewInProcessClient creates a client backed by a local service.
func NewInProcessClient(svc service.Service) *InProcessClient {
	return &InProcessClient{svc: svc}
}

func (c *InProcessClient) CreateSession(ctx context.Context) (*service.SessionInfo, error) {
	return c.svc.CreateSession(ctx)
}

func (c *InProcessClient) GetSession(ctx context.Context, id string) (*service.SessionInfo, error) {
	return c.svc.GetSession(ctx, id)
}

func (c *InProcessClient) ListSessions(ctx context.Context) ([]service.SessionInfo, error) {
	return c.svc.ListSessions(ctx)
}

func (c *InProcessClient) GetMessages(ctx context.Context, sessionID string) ([]json.RawMessage, error) {
	return c.svc.GetMessages(ctx, sessionID)
}

func (c *InProcessClient) SendMessage(ctx context.Context, sessionID string, input service.MessageInput) error {
	return c.svc.SendMessage(ctx, sessionID, input)
}

func (c *InProcessClient) CancelTurn(ctx context.Context, sessionID string) error {
	return c.svc.CancelTurn(ctx, sessionID)
}

func (c *InProcessClient) RespondPermission(ctx context.Context, requestID string, allowed bool) error {
	return c.svc.RespondPermission(ctx, requestID, allowed)
}

func (c *InProcessClient) GetConfig(ctx context.Context) (*config.Config, error) {
	return c.svc.GetConfig(ctx)
}

func (c *InProcessClient) SwitchModel(ctx context.Context, model string) error {
	return c.svc.SwitchModel(ctx, model)
}

func (c *InProcessClient) GetStatus(ctx context.Context) (*service.StatusInfo, error) {
	return c.svc.GetStatus(ctx)
}

func (c *InProcessClient) ExecuteCommand(ctx context.Context, name string, args string) (string, error) {
	return c.svc.ExecuteCommand(ctx, name, args)
}

func (c *InProcessClient) Bus() bus.Bus {
	return c.svc.Bus()
}

// Events subscribes to the local bus and returns a channel of events.
func (c *InProcessClient) Events(ctx context.Context, filter ...bus.EventType) (<-chan bus.Event, error) {
	ch, unsub := c.svc.Bus().Subscribe(filter...)

	// Auto-unsubscribe when context is done.
	go func() {
		<-ctx.Done()
		unsub()
	}()

	return ch, nil
}

// Close is a no-op for the in-process client.
func (c *InProcessClient) Close() error {
	return nil
}
