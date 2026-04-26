package eval

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"sync"
	"time"

	"github.com/qiangli/ycode/internal/api"
	"github.com/qiangli/ycode/internal/tools"
	"github.com/qiangli/ycode/pkg/ycode"
)

// AgentRunner creates a Runner that executes scenarios using the ycode Agent API.
// It wraps tool handlers with trajectory-capturing middleware to record all
// tool calls for assertion and scoring.
func AgentRunner(cfg RunConfig, provider api.Provider) *Runner {
	return NewRunner(cfg, func(ctx context.Context, s *Scenario) (*RunResult, error) {
		return executeWithAgent(ctx, s, cfg, provider)
	})
}

func executeWithAgent(ctx context.Context, s *Scenario, cfg RunConfig, provider api.Provider) (*RunResult, error) {
	// Create temp workspace.
	workDir, err := os.MkdirTemp("", "ycode-eval-*")
	if err != nil {
		return &RunResult{Duration: 0}, fmt.Errorf("create workspace: %w", err)
	}

	// Run scenario setup if provided.
	var cleanup func()
	if s.Setup != nil {
		cleanup, err = s.Setup(workDir)
		if err != nil {
			os.RemoveAll(workDir)
			return &RunResult{Duration: 0}, fmt.Errorf("setup: %w", err)
		}
	}
	defer func() {
		if cleanup != nil {
			cleanup()
		}
		os.RemoveAll(workDir)
	}()

	// Create agent.
	opts := []ycode.Option{
		ycode.WithProvider(provider),
	}
	if cfg.Model != "" {
		opts = append(opts, ycode.WithModel(cfg.Model))
	}

	agent, err := ycode.NewAgent(opts...)
	if err != nil {
		return &RunResult{WorkDir: workDir, Duration: 0}, fmt.Errorf("create agent: %w", err)
	}

	// Apply trajectory-capturing middleware to all registered tools.
	recorder := &trajectoryRecorder{}
	registry := agent.Registry()
	for _, name := range registry.Names() {
		toolName := name // capture loop var
		_ = registry.ApplyMiddleware(toolName, recorder.middleware(toolName))
	}

	// Execute.
	start := time.Now()
	runErr := agent.Run(ctx, s.Prompt)
	duration := time.Since(start)

	result := &RunResult{
		ToolCalls: recorder.calls(),
		Turns:     recorder.turnCount(),
		Duration:  duration,
		Error:     runErr,
		WorkDir:   workDir,
	}

	return result, nil
}

// trajectoryRecorder captures tool calls in order via middleware.
type trajectoryRecorder struct {
	mu       sync.Mutex
	recorded []ToolCall
}

func (r *trajectoryRecorder) middleware(toolName string) tools.Middleware {
	return func(next tools.ToolFunc) tools.ToolFunc {
		return func(ctx context.Context, input json.RawMessage) (string, error) {
			start := time.Now()
			output, err := next(ctx, input)
			dur := time.Since(start)

			tc := ToolCall{
				Name:     toolName,
				Input:    input,
				Output:   output,
				Duration: dur,
			}
			if err != nil {
				tc.Error = err.Error()
			}

			r.mu.Lock()
			r.recorded = append(r.recorded, tc)
			r.mu.Unlock()

			return output, err
		}
	}
}

func (r *trajectoryRecorder) calls() []ToolCall {
	r.mu.Lock()
	defer r.mu.Unlock()
	out := make([]ToolCall, len(r.recorded))
	copy(out, r.recorded)
	return out
}

// turnCount estimates conversation turns from tool calls.
// Each unique sequence of tool calls between pauses is roughly one turn.
func (r *trajectoryRecorder) turnCount() int {
	r.mu.Lock()
	defer r.mu.Unlock()
	if len(r.recorded) == 0 {
		return 1 // at minimum one turn (the prompt)
	}
	// Approximate: number of tool calls / average tools per turn, min 1.
	return max(1, len(r.recorded)/3+1)
}

// ProviderFromEnv creates an API provider from environment variables.
// Supports EVAL_PROVIDER (ollama, anthropic, openai) and EVAL_MODEL.
func ProviderFromEnv() (api.Provider, string, error) {
	providerName := os.Getenv("EVAL_PROVIDER")
	if providerName == "" {
		providerName = "anthropic" // default
	}

	model := os.Getenv("EVAL_MODEL")

	switch providerName {
	case "ollama", "local":
		baseURL := os.Getenv("OLLAMA_HOST")
		if baseURL == "" {
			baseURL = "http://127.0.0.1:11434"
		}
		p := api.NewProvider(&api.ProviderConfig{
			Kind:    api.ProviderLocal,
			BaseURL: baseURL + "/v1",
		})
		if model == "" {
			model = "qwen2.5-coder:7b"
		}
		return p, model, nil

	case "anthropic":
		key := os.Getenv("ANTHROPIC_API_KEY")
		if key == "" {
			return nil, "", fmt.Errorf("ANTHROPIC_API_KEY required for anthropic provider")
		}
		p := api.NewAnthropicClient(key)
		if model == "" {
			model = "claude-sonnet-4-6-20250514"
		}
		return p, model, nil

	case "openai":
		key := os.Getenv("OPENAI_API_KEY")
		if key == "" {
			return nil, "", fmt.Errorf("OPENAI_API_KEY required for openai provider")
		}
		baseURL := os.Getenv("OPENAI_BASE_URL")
		if baseURL == "" {
			baseURL = "https://api.openai.com/v1"
		}
		p := api.NewProvider(&api.ProviderConfig{
			Kind:    api.ProviderOpenAI,
			APIKey:  key,
			BaseURL: baseURL,
		})
		if model == "" {
			model = "gpt-4o"
		}
		return p, model, nil

	default:
		return nil, "", fmt.Errorf("unknown provider %q (use ollama, anthropic, or openai)", providerName)
	}
}
