package service

import (
	"context"
	"encoding/json"

	"github.com/qiangli/ycode/internal/api"
	"github.com/qiangli/ycode/internal/bus"
	"github.com/qiangli/ycode/internal/runtime/config"
)

// MessageInput is an alias for bus.MessageInput to avoid import cycles.
type MessageInput = bus.MessageInput

// SessionInfo describes a session for API responses.
type SessionInfo struct {
	ID           string `json:"id"`
	WorkDir      string `json:"work_dir,omitempty"`
	CreatedAt    string `json:"created_at"`
	MessageCount int    `json:"message_count"`
	Summary      string `json:"summary,omitempty"`
}

// StatusInfo describes the current server state.
type StatusInfo struct {
	Model        string `json:"model"`
	ProviderKind string `json:"provider_kind"`
	SessionID    string `json:"session_id"`
	WorkDir      string `json:"work_dir,omitempty"`
	PlanMode     bool   `json:"plan_mode"`
	Version      string `json:"version"`
}

// Service is the backend contract shared by all transports
// (in-process, WebSocket, NATS). Both the TUI and web clients
// consume this interface — the only difference is the transport.
type Service interface {
	// Session lifecycle.
	CreateSession(ctx context.Context) (*SessionInfo, error)
	GetSession(ctx context.Context, id string) (*SessionInfo, error)
	ListSessions(ctx context.Context) ([]SessionInfo, error)
	GetMessages(ctx context.Context, sessionID string) ([]json.RawMessage, error)

	// Conversation — async, results arrive via bus events.
	SendMessage(ctx context.Context, sessionID string, input MessageInput) error
	CancelTurn(ctx context.Context, sessionID string) error

	// Permission prompt response.
	RespondPermission(ctx context.Context, requestID string, allowed bool) error

	// Config and state.
	GetConfig(ctx context.Context) (*config.Config, error)
	SwitchModel(ctx context.Context, model string) error
	GetStatus(ctx context.Context) (*StatusInfo, error)
	ListModels(ctx context.Context) ([]api.ModelInfo, error)

	// Slash commands.
	ExecuteCommand(ctx context.Context, name string, args string) (string, error)

	// Event bus access (for wiring transports).
	Bus() bus.Bus
}
