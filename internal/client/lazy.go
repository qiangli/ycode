package client

import (
	"context"
	"fmt"
	"sync"

	"github.com/qiangli/ycode/internal/api"
	"github.com/qiangli/ycode/internal/bus"
	"github.com/qiangli/ycode/internal/service"
)

// ConnectFunc creates and connects a WSClient.
type ConnectFunc func(ctx context.Context) (*WSClient, error)

// LazyClient implements the agentClient interface with lazy connection.
// The TUI can start immediately — the connection is established in the background
// and blocks only when the first message is sent.
type LazyClient struct {
	connectFn ConnectFunc

	once   sync.Once
	mu     sync.Mutex
	client *WSClient
	err    error
	ready  chan struct{}
}

// NewLazyClient creates a client that connects lazily via connectFn.
func NewLazyClient(connectFn ConnectFunc) *LazyClient {
	return &LazyClient{
		connectFn: connectFn,
		ready:     make(chan struct{}),
	}
}

// ConnectAsync starts the connection process in the background.
func (l *LazyClient) ConnectAsync() {
	go l.connect(context.Background())
}

func (l *LazyClient) connect(ctx context.Context) {
	l.once.Do(func() {
		client, err := l.connectFn(ctx)
		l.mu.Lock()
		l.client, l.err = client, err
		l.mu.Unlock()
		close(l.ready)
	})
}

// wait blocks until the client is connected or returns an error.
func (l *LazyClient) wait(ctx context.Context) (*WSClient, error) {
	// Trigger connection if not already started.
	l.connect(ctx)

	select {
	case <-l.ready:
		l.mu.Lock()
		defer l.mu.Unlock()
		if l.err != nil {
			return nil, l.err
		}
		return l.client, nil
	case <-ctx.Done():
		return nil, ctx.Err()
	}
}

// SendMessage waits for connection then sends.
func (l *LazyClient) SendMessage(ctx context.Context, sessionID string, input bus.MessageInput) error {
	c, err := l.wait(ctx)
	if err != nil {
		return fmt.Errorf("server not ready: %w", err)
	}
	return c.SendMessage(ctx, sessionID, service.MessageInput(input))
}

// CancelTurn waits for connection then cancels.
func (l *LazyClient) CancelTurn(ctx context.Context, sessionID string) error {
	c, err := l.wait(ctx)
	if err != nil {
		return err
	}
	return c.CancelTurn(ctx, sessionID)
}

// Events waits for connection then subscribes.
func (l *LazyClient) Events(ctx context.Context, filter ...bus.EventType) (<-chan bus.Event, error) {
	c, err := l.wait(ctx)
	if err != nil {
		return nil, fmt.Errorf("server not ready: %w", err)
	}
	return c.Events(ctx, filter...)
}

// ListModels waits for connection then lists.
func (l *LazyClient) ListModels(ctx context.Context) ([]api.ModelInfo, error) {
	c, err := l.wait(ctx)
	if err != nil {
		return nil, fmt.Errorf("server not ready: %w", err)
	}
	return c.ListModels(ctx)
}

// SwitchModel waits for connection then switches.
func (l *LazyClient) SwitchModel(ctx context.Context, model string) error {
	c, err := l.wait(ctx)
	if err != nil {
		return fmt.Errorf("server not ready: %w", err)
	}
	return c.SwitchModel(ctx, model)
}

// Close closes the underlying client if connected.
func (l *LazyClient) Close() error {
	l.mu.Lock()
	defer l.mu.Unlock()
	if l.client != nil {
		return l.client.Close()
	}
	return nil
}
