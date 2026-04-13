package client

import (
	"context"

	"github.com/qiangli/ycode/internal/bus"
	"github.com/qiangli/ycode/internal/service"
)

// Client extends the Service interface with event streaming and lifecycle.
// All frontends (TUI, web, remote) use this interface.
type Client interface {
	service.Service

	// Events returns a channel of bus events, optionally filtered by type.
	Events(ctx context.Context, filter ...bus.EventType) (<-chan bus.Event, error)

	// Close releases resources.
	Close() error
}
