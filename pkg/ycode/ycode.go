package ycode

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"path/filepath"

	"github.com/qiangli/ycode/internal/api"
	"github.com/qiangli/ycode/internal/bus"
	"github.com/qiangli/ycode/internal/cli"
	"github.com/qiangli/ycode/internal/inference"
	"github.com/qiangli/ycode/internal/runtime/config"
	"github.com/qiangli/ycode/internal/runtime/session"
	"github.com/qiangli/ycode/internal/server"
	"github.com/qiangli/ycode/internal/service"
	"github.com/qiangli/ycode/internal/tools"
)

// Agent is the public API for embedding ycode as a library.
type Agent struct {
	config   *config.Config
	provider api.Provider
	session  *session.Session
	registry *tools.Registry
	app      *cli.App
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

// WithOllama configures the agent to use a local Ollama server.
// If baseURL is empty, defaults to http://127.0.0.1:11434 (or OLLAMA_HOST env).
func WithOllama(baseURL string) Option {
	return func(a *Agent) error {
		if baseURL == "" {
			baseURL = inference.DefaultOllamaURL()
		}
		a.provider = api.NewOpenAICompatClient("", baseURL+"/v1")
		return nil
	}
}

// NewAgent creates a new ycode agent with the given options.
// If no provider is specified, auto-detects from environment:
// Ollama (local) → ANTHROPIC_API_KEY → OPENAI_API_KEY → error.
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

	// Register tools.
	tools.RegisterBuiltins(a.registry)

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

// Event represents a streaming event from the agent.
type Event struct {
	Type string          `json:"type"` // "text.delta", "tool_use.start", "turn.complete", "turn.error"
	Data json.RawMessage `json:"data"`
}

// Chat sends a message to the agent and streams events back via the callback.
// The callback is called for each event (text deltas, tool use, completion).
// This runs the full agentic loop with tools, memory, and all features.
func (a *Agent) Chat(ctx context.Context, message string, onEvent func(Event)) error {
	memBus := bus.NewMemoryBus()
	svc := service.NewLocalService(a.app, memBus)

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
func (a *Agent) Handler() http.Handler {
	memBus := bus.NewMemoryBus()
	svc := service.NewLocalService(a.app, memBus)
	srv := server.New(server.Config{}, svc)
	return srv.Mux()
}

// autoDetectProvider finds a provider from available credentials.
func autoDetectProvider(model string) (api.Provider, error) {
	// 1. Try Ollama if running locally.
	ollamaURL := inference.DefaultOllamaURL()
	if inference.DetectOllamaServer(context.Background(), ollamaURL) {
		return api.NewOpenAICompatClient("", ollamaURL+"/v1"), nil
	}

	// 2. Try standard API keys.
	providerCfg, err := api.DetectProvider(model)
	if err == nil {
		return api.NewProvider(providerCfg), nil
	}

	return nil, fmt.Errorf("no LLM provider available: install Ollama locally, or set ANTHROPIC_API_KEY / OPENAI_API_KEY")
}
