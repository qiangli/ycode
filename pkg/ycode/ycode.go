package ycode

import (
	"context"
	"fmt"
	"os"
	"path/filepath"

	"github.com/qiangli/ycode/internal/api"
	"github.com/qiangli/ycode/internal/cli"
	"github.com/qiangli/ycode/internal/runtime/config"
	"github.com/qiangli/ycode/internal/runtime/session"
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

// NewAgent creates a new ycode agent with the given options.
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

	// Default provider from env if not set.
	if a.provider == nil {
		apiKey := os.Getenv("ANTHROPIC_API_KEY")
		if apiKey == "" {
			return nil, fmt.Errorf("ANTHROPIC_API_KEY required (or use WithProvider/WithAPIKey)")
		}
		a.provider = api.NewAnthropicClient(apiKey)
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
	app, err := cli.NewApp(cfg, a.provider, sess)
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
