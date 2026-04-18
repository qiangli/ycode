// Package builtin provides structured operations that bypass the full
// conversation runtime. Each operation runs git commands directly via os/exec
// and makes at most one LLM call with minimal context, yielding 90-95% token
// savings compared to the agentic loop.
package builtin

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"strings"
	"time"

	"github.com/qiangli/ycode/internal/api"
	"github.com/qiangli/ycode/internal/runtime/config"
	"github.com/qiangli/ycode/internal/runtime/session"
)

const (
	// singleShotTimeout is the maximum time for a single-shot LLM call.
	singleShotTimeout = 30 * time.Second
)

// ModelChain tries a sequence of models in order, returning the first
// successful result. Typical usage: [weakModel, mainModel] where the
// weak model is cheaper/faster.
type ModelChain struct {
	Models []session.ModelSpec
}

// SingleShot sends a single system+user message pair to the LLM with no tools
// and no conversation history. It tries each model in the chain, returning the
// first successful response.
func (mc *ModelChain) SingleShot(ctx context.Context, systemPrompt, userContent string, maxTokens int) (string, error) {
	var lastErr error
	for _, ms := range mc.Models {
		text, err := singleShotWith(ctx, ms, systemPrompt, userContent, maxTokens)
		if err != nil {
			slog.Info("single-shot call failed, trying next model", "model", ms.Model, "error", err)
			lastErr = err
			continue
		}
		slog.Info("single-shot call succeeded", "model", ms.Model)
		return text, nil
	}
	return "", fmt.Errorf("all models failed (last: %w)", lastErr)
}

// singleShotWith sends the request to a specific model.
func singleShotWith(ctx context.Context, ms session.ModelSpec, systemPrompt, userContent string, maxTokens int) (string, error) {
	ctx, cancel := context.WithTimeout(ctx, singleShotTimeout)
	defer cancel()

	req := &api.Request{
		Model:     ms.Model,
		MaxTokens: maxTokens,
		System:    systemPrompt,
		Messages: []api.Message{
			{
				Role: api.RoleUser,
				Content: []api.ContentBlock{
					{Type: api.ContentTypeText, Text: userContent},
				},
			},
		},
		Stream: true,
	}

	events, errc := ms.Provider.Send(ctx, req)

	var parts []string
	for ev := range events {
		if ev.Delta != nil {
			var delta struct {
				Text string `json:"text"`
			}
			if err := json.Unmarshal(ev.Delta, &delta); err == nil && delta.Text != "" {
				parts = append(parts, delta.Text)
			}
		}
	}

	if err := <-errc; err != nil {
		return "", fmt.Errorf("single-shot (%s): %w", ms.Model, err)
	}

	text := strings.TrimSpace(strings.Join(parts, ""))
	if text == "" {
		return "", fmt.Errorf("single-shot (%s) returned empty response", ms.Model)
	}

	return text, nil
}

// ResolveModelChain builds a ModelChain from config, using the weak model
// first (if configured) and the main model as fallback. The weak model may
// use a different provider than the main model (e.g., Haiku via Anthropic
// while the main model is GPT-4o via OpenAI).
func ResolveModelChain(cfg *config.Config, mainProvider api.Provider) *ModelChain {
	var specs []session.ModelSpec

	if cfg.WeakModel != "" {
		resolved := api.ResolveModelWithAliases(cfg.WeakModel, cfg.Aliases)
		weakProvider := resolveProviderForModel(resolved, mainProvider)
		specs = append(specs, session.ModelSpec{Provider: weakProvider, Model: resolved})
	}

	// Always include main model as final fallback.
	specs = append(specs, session.ModelSpec{Provider: mainProvider, Model: cfg.Model})

	return &ModelChain{Models: specs}
}

// SkillExecutor is a function that directly executes a builtin operation
// when the LLM invokes the Skill tool with a matching name.
type SkillExecutor func(ctx context.Context, args string) (string, error)

// skillExecutors maps skill names to builtin executors.
var skillExecutors = map[string]SkillExecutor{}

// RegisterSkillExecutor registers a function that directly executes a builtin
// operation when the LLM invokes the Skill tool with the given name. This
// short-circuits SKILL.md loading and runs the optimized path instead.
func RegisterSkillExecutor(name string, fn SkillExecutor) {
	skillExecutors[strings.ToLower(name)] = fn
}

// GetSkillExecutor returns the builtin executor for a skill name, if one exists.
func GetSkillExecutor(name string) (SkillExecutor, bool) {
	fn, ok := skillExecutors[strings.ToLower(name)]
	return fn, ok
}

// resolveProviderForModel attempts to detect the correct provider for a model.
// Falls back to the given default provider if detection fails.
func resolveProviderForModel(model string, fallback api.Provider) api.Provider {
	cfg, err := api.DetectProvider(model)
	if err != nil {
		return fallback
	}
	return api.NewProvider(cfg)
}
