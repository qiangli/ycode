package service

import (
	"context"

	"github.com/qiangli/ycode/internal/api"
	"github.com/qiangli/ycode/internal/runtime/config"
	"github.com/qiangli/ycode/internal/runtime/conversation"
	"github.com/qiangli/ycode/internal/runtime/session"
	"github.com/qiangli/ycode/internal/runtime/taskqueue"
	"github.com/qiangli/ycode/internal/runtime/usage"
)

// AppBackend is the interface that LocalService requires from the application.
// It breaks the import cycle between cli and service packages.
type AppBackend interface {
	// Session and message access.
	Session() *session.Session
	SessionID() string
	SessionMessages() []api.Message
	MessageCount() int

	// Conversation runtime.
	ConversationRuntime() *conversation.Runtime
	RunTurnWithRecovery(ctx context.Context, messages []api.Message) (*conversation.TurnResult, *conversation.RecoveryResult, error)
	ExecuteTools(ctx context.Context, calls []conversation.ToolCall, progress chan<- taskqueue.TaskEvent) []api.ContentBlock

	// Configuration and state.
	Config() *config.Config
	Model() string
	ProviderKind() string
	Version() string
	WorkDir() string
	InPlanMode() bool
	SwitchModel(name string) (string, error)

	// Usage tracking.
	UsageTracker() *usage.Tracker
	NextTurnIndex() int

	// Command execution.
	ExecuteCommand(ctx context.Context, name string, args string) (string, error)
	HasCommand(name string) bool

	// Progress callbacks for streaming command output.
	SetProgressFunc(fn func(string))
	SetDeltaFunc(fn func(string))
	SetUsageFunc(fn func(inputTokens, outputTokens, cacheCreate, cacheRead int))
	SetAgentEventFunc(fn func(eventType string, data map[string]any))

	// InstallRemotePermissionPrompter wires a service-level requester into
	// the App's tool registry so permission checks for elevated tools
	// publish a permission.request bus event and wait for the client's
	// permission.response, instead of prompting an in-process TUI. Used by
	// the `ycode serve` API stack when the consumer is a remote client
	// (web UI, VS Code extension, ...).
	InstallRemotePermissionPrompter(requester PermissionRequester)

	// Lifecycle.
	Close() error
}
