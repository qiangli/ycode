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
	"regexp"
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
		// Non-streaming is more reliable across providers for short single-shot
		// calls (some OpenAI-compatible models have streaming quirks). The
		// Anthropic provider overrides this to always stream regardless.
		Stream: false,
		// Disable thinking/reasoning for single-shot calls — we want a direct
		// answer, not chain-of-thought (e.g., kimi-k2.5 puts everything in
		// reasoning_content otherwise).
		ReasoningEffort: "none",
	}

	events, errc := ms.Provider.Send(ctx, req)

	var textParts []string
	var thinkingParts []string
	for ev := range events {
		if ev.Delta != nil {
			var delta struct {
				Type     string `json:"type"`
				Text     string `json:"text"`
				Thinking string `json:"thinking"`
			}
			if err := json.Unmarshal(ev.Delta, &delta); err == nil {
				if delta.Text != "" {
					textParts = append(textParts, delta.Text)
				}
				if delta.Thinking != "" {
					thinkingParts = append(thinkingParts, delta.Thinking)
				}
			}
		}
		// Also extract text from content blocks (some providers emit these
		// instead of or in addition to deltas).
		if ev.ContentBlock != nil && ev.ContentBlock.Text != "" {
			textParts = append(textParts, ev.ContentBlock.Text)
		}
	}

	if err := <-errc; err != nil {
		return "", fmt.Errorf("single-shot (%s): %w", ms.Model, err)
	}

	// Prefer text content; fall back to extracting the answer from thinking
	// content for reasoning models that put everything in reasoning_content
	// and leave content empty (e.g., kimi-k2.5).
	text := strings.TrimSpace(strings.Join(textParts, ""))
	if text == "" && len(thinkingParts) > 0 {
		text = extractFromThinking(strings.Join(thinkingParts, ""))
	}
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

// conventionalCommitPrefix matches the start of a Conventional Commits message.
// Covers: fix, feat, build, chore, ci, docs, style, refactor, perf, test
// with optional scope in parentheses.
var conventionalCommitPrefix = regexp.MustCompile(
	`^(fix|feat|build|chore|ci|docs|style|refactor|perf|test)(\([^)]+\))?:\s`,
)

// extractFromThinking scans reasoning/thinking output for a line that looks
// like a conventional commit message. Reasoning models (e.g., kimi-k2.5) may
// put the final answer within their chain-of-thought rather than in the
// content field.
func extractFromThinking(thinking string) string {
	// Scan lines in reverse — the answer is typically near the end.
	lines := strings.Split(thinking, "\n")
	for i := len(lines) - 1; i >= 0; i-- {
		line := strings.TrimSpace(lines[i])
		// Strip markdown formatting: backticks, quotes, bold.
		line = strings.Trim(line, "`\"'*")
		line = strings.TrimSpace(line)
		if conventionalCommitPrefix.MatchString(line) {
			return line
		}
	}
	return ""
}

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
