package ycode

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"sync"

	"github.com/qiangli/ycode/internal/api"
	"github.com/qiangli/ycode/internal/bus"
	"github.com/qiangli/ycode/internal/cli"
	"github.com/qiangli/ycode/internal/runtime/config"
	"github.com/qiangli/ycode/internal/runtime/embedding"
	"github.com/qiangli/ycode/internal/runtime/session"
	"github.com/qiangli/ycode/internal/server"
	"github.com/qiangli/ycode/internal/service"
	"github.com/qiangli/ycode/internal/tools"
	memexgraph "github.com/qiangli/ycode/pkg/memex/graph"
	"github.com/qiangli/ycode/pkg/memex/memory"
	"github.com/qiangli/ycode/pkg/memex/store"
)

// Agent is the public API for embedding ycode as a library.
type Agent struct {
	config   *config.Config
	provider api.Provider
	session  *session.Session
	registry *tools.Registry
	app      *cli.App

	// Tool-registration control (set via experimental Options). Zero values
	// produce the default behavior: register every built-in tool.
	skipBuiltins     bool
	builtinAllowlist []string

	// Permission hooks (set via experimental Options).
	permResolver tools.PermissionResolver
	permPrompter tools.PermissionPrompter

	// Embedding provider (set via experimental Option or lazy-init).
	embedProvider embedding.Provider
	embedOnce     sync.Once

	// Shared service + bus. ensureService initializes both exactly once
	// so concurrent Chat / Handler / HandlerWithAuth use the same backing
	// state (per-session cancellation, permission channels).
	serviceOnce sync.Once
	cachedBus   bus.Bus
	cachedSvc   service.Service
}

// Option configures an Agent.
type Option func(*Agent) error

// WithModel sets the model to use.
func WithModel(model string) Option {
	return func(a *Agent) error {
		a.config.Model = model
		return nil
	}
}

// WithMaxTokens sets the maximum output tokens.
func WithMaxTokens(n int) Option {
	return func(a *Agent) error {
		a.config.MaxTokens = n
		return nil
	}
}

// WithProvider sets a custom API provider.
func WithProvider(p api.Provider) Option {
	return func(a *Agent) error {
		a.provider = p
		return nil
	}
}

// WithAPIKey sets the Anthropic API key.
func WithAPIKey(key string) Option {
	return func(a *Agent) error {
		a.provider = api.NewAnthropicClient(key)
		return nil
	}
}

// NewAgent creates a new ycode agent with the given options.
// If no provider is specified, auto-detects from environment:
// configured API provider credentials → error.
func NewAgent(opts ...Option) (*Agent, error) {
	cfg := config.DefaultConfig()

	a := &Agent{
		config:   cfg,
		registry: tools.NewRegistry(),
	}

	for _, opt := range opts {
		if err := opt(a); err != nil {
			return nil, fmt.Errorf("apply option: %w", err)
		}
	}

	// Auto-detect provider if not explicitly set.
	if a.provider == nil {
		provider, err := autoDetectProvider(a.config.Model)
		if err != nil {
			return nil, err
		}
		a.provider = provider
	}

	// Create session.
	home, _ := os.UserHomeDir()
	sessionDir := filepath.Join(home, ".local", "share", "ycode", "sessions")
	sess, err := session.New(sessionDir)
	if err != nil {
		return nil, fmt.Errorf("create session: %w", err)
	}
	a.session = sess

	// Register built-in tools — opt-out / allowlist hooks come from
	// experimental Options. The default is "register everything."
	switch {
	case a.skipBuiltins:
		// no-op: host populates a.Registry() with custom tools only.
	case a.builtinAllowlist != nil:
		tools.RegisterBuiltinsFiltered(a.registry, a.builtinAllowlist)
	default:
		tools.RegisterBuiltins(a.registry)
	}

	// Install permission hooks if provided via experimental Options.
	if a.permResolver != nil {
		a.registry.SetPermissionResolver(a.permResolver)
	}
	if a.permPrompter != nil {
		a.registry.SetPermissionPrompter(a.permPrompter)
	}

	// Create app.
	app, err := cli.NewApp(cfg, a.provider, sess, cli.AppOptions{
		ToolRegistry: a.registry,
	})
	if err != nil {
		return nil, err
	}
	a.app = app

	return a, nil
}

// Run executes a one-shot prompt and returns the response text.
func (a *Agent) Run(ctx context.Context, prompt string) error {
	return a.app.RunPrompt(ctx, prompt)
}

// RunInteractive starts the interactive REPL.
func (a *Agent) RunInteractive(ctx context.Context) error {
	return a.app.RunInteractive(ctx)
}

// Registry returns the tool registry for custom tool registration.
func (a *Agent) Registry() *tools.Registry {
	return a.registry
}

// Memory returns the underlying memex memory manager. May be nil if the
// agent was constructed without a memory subsystem (e.g. when no persistent
// store is configured).
func (a *Agent) Memory() *memory.Manager { return a.app.Memory() }

// Storage returns the underlying memex store.Manager (KV/SQL/search/vector).
// May be nil for agents that don't carry a persistent store.
func (a *Agent) Storage() *store.Manager { return a.app.Storage() }

// Graph returns the underlying memex queryable graph store (bonsai). May
// be nil if the agent was constructed without one. Callers can issue DQL
// queries directly via Graph().Query.
func (a *Agent) Graph() *memexgraph.Graph { return a.app.Graph() }

// GraphHandler returns the bonsai HTTP handler (DQL query endpoint).
// Mountable on any HTTP mux. Returns nil if no graph is configured.
func (a *Agent) GraphHandler() http.Handler {
	if g := a.Graph(); g != nil {
		return g.HTTPHandler()
	}
	return nil
}

// Event represents a streaming event from the agent.
type Event struct {
	Type string          `json:"type"` // "text.delta", "tool_use.start", "turn.complete", "turn.error"
	Data json.RawMessage `json:"data"`
}

// Chat sends a message to the agent and streams events back via the callback.
// The callback is called for each event (text deltas, tool use, completion).
// This runs the full agentic loop with tools, memory, and all features.
func (a *Agent) Chat(ctx context.Context, message string, onEvent func(Event)) error {
	svc, memBus := a.ensureService()

	evCh, unsub := memBus.Subscribe()
	defer unsub()

	// Send message asynchronously.
	errCh := make(chan error, 1)
	go func() {
		errCh <- svc.SendMessage(ctx, a.session.ID, bus.MessageInput{Text: message})
	}()

	// Stream events to callback.
	for ev := range evCh {
		onEvent(Event{Type: string(ev.Type), Data: ev.Data})
		if ev.Type == bus.EventTurnComplete || ev.Type == bus.EventTurnError {
			break
		}
	}

	return <-errCh
}

// Handler returns an http.Handler that serves the ycode REST + WebSocket API.
// Mount this on any Go HTTP server to get the full agentic API.
//
// SECURITY: This handler has no built-in authentication or authorization.
// /api/sessions is reachable to any HTTP caller that can reach the mux.
// Never expose Handler() to untrusted networks — wrap with HandlerWithAuth
// (or with your own middleware) before doing so.
func (a *Agent) Handler() http.Handler {
	svc, _ := a.ensureService()
	srv := server.New(server.Config{}, svc)
	return srv.Mux()
}

// ensureService lazily initializes a single in-memory bus + LocalService for
// this Agent, returning the same pair on every subsequent call. Sharing one
// service is what makes per-session cancellation, permission-prompt channels,
// and event ordering coherent across Chat/Handler/HandlerWithAuth.
func (a *Agent) ensureService() (service.Service, bus.Bus) {
	a.serviceOnce.Do(func() {
		a.cachedBus = bus.NewMemoryBus()
		a.cachedSvc = service.NewLocalService(a.app, a.cachedBus)
	})
	return a.cachedSvc, a.cachedBus
}

// autoDetectProvider finds a provider from available credentials.
func autoDetectProvider(model string) (api.Provider, error) {
	providerCfg, err := api.DetectProvider(model)
	if err != nil {
		return nil, fmt.Errorf("no LLM provider available: set provider credentials such as ANTHROPIC_API_KEY or OPENAI_API_KEY")
	}

	// Preflight key-health probe: a stale/invalid key fails NOW, with a clear
	// fingerprinted error, instead of mid-run behind an opaque fallback message.
	if err := api.PreflightAuthCheck(context.Background(), providerCfg); err != nil {
		return nil, err
	}

	return api.NewProvider(providerCfg), nil
}
