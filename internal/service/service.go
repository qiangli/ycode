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

// SessionOptions carries per-session overrides accepted on POST
// /api/sessions and echoed back in SessionInfo. The zero value means
// "inherit every default from the loaded config." Fields are honored
// best-effort:
//
//   - Model: applied per-turn (G-G). When set, SendMessage swaps the
//     active model for the duration of the turn and restores afterwards.
//   - ToolsAllowlist / ToolsBlocklist (G-E): wire shape is accepted and
//     echoed. Today enforcement is server-wide via `--tools-allowlist` /
//     `--tools-blocklist` flags; full per-session enforcement is gated on
//     G-I (decoupling memex/conversation namespaces).
//   - PersonaDisabled (G-F): same — server-wide via `--no-persona` today.
//
// The struct lives next to SessionInfo so JSON tags match the wire.
type SessionOptions struct {
	Model           string   `json:"model,omitempty"`
	ToolsAllowlist  []string `json:"tools_allowlist,omitempty"`
	ToolsBlocklist  []string `json:"tools_blocklist,omitempty"`
	PersonaDisabled bool     `json:"persona_disabled,omitempty"`
}

// IsZero reports whether o carries no overrides.
func (o SessionOptions) IsZero() bool {
	return o.Model == "" && len(o.ToolsAllowlist) == 0 && len(o.ToolsBlocklist) == 0 && !o.PersonaDisabled
}

// SessionInfo describes a session for API responses.
type SessionInfo struct {
	ID           string          `json:"id"`
	WorkDir      string          `json:"work_dir,omitempty"`
	CreatedAt    string          `json:"created_at"`
	MessageCount int             `json:"message_count"`
	Summary      string          `json:"summary,omitempty"`
	Options      *SessionOptions `json:"session_options,omitempty"`
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

	// LookupApp resolves the per-workDir App backend. Used by stateless
	// wire endpoints (/api/extract, /api/embed) that need the App's
	// provider/config but not the agentic conversation loop. workDir may
	// be empty when the service backs a single project (LocalService) —
	// in that case the implementation returns its sole App.
	LookupApp(ctx context.Context, workDir string) (AppBackend, error)
}
