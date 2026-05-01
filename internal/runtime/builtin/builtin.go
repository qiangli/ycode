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
	// DefaultSingleShotTimeout is the default timeout for short single-shot
	// LLM calls (e.g., commit message generation).
	DefaultSingleShotTimeout = 30 * time.Second

	// InitSingleShotTimeout is the timeout for /init AGENTS.md generation,
	// which sends a large prompt with full project context.
	InitSingleShotTimeout = 120 * time.Second
)

// ModelChain tries a sequence of models in order, returning the first
// successful result. Typical usage: [weakModel, mainModel] where the
// weak model is cheaper/faster.
type ModelChain struct {
	Models []session.ModelSpec
}

// SingleShotResult holds the result of a single-shot LLM call.
type SingleShotResult struct {
	Text         string
	InputTokens  int
	OutputTokens int
	CacheCreate  int
	CacheRead    int
}

// SingleShot sends a single system+user message pair to the LLM with no tools
// and no conversation history. It tries each model in the chain, returning the
// first successful response.
func (mc *ModelChain) SingleShot(ctx context.Context, systemPrompt, userContent string, maxTokens int) (string, error) {
	result, err := mc.SingleShotWithUsage(ctx, systemPrompt, userContent, maxTokens)
	if err != nil {
		return "", err
	}
	return result.Text, nil
}

// SingleShotWithUsage sends a single system+user message pair to the LLM and
// returns both the text response and token usage. It tries each model in the
// chain, returning the first successful response. Uses DefaultSingleShotTimeout.
func (mc *ModelChain) SingleShotWithUsage(ctx context.Context, systemPrompt, userContent string, maxTokens int) (*SingleShotResult, error) {
	return mc.SingleShotWithUsageAndTimeout(ctx, systemPrompt, userContent, maxTokens, DefaultSingleShotTimeout)
}

// SingleShotWithUsageAndTimeout is like SingleShotWithUsage but with a custom timeout.
// The timeout is shared across all models in the chain — if the first model
// exhausts most of the budget, later models get whatever time remains.
func (mc *ModelChain) SingleShotWithUsageAndTimeout(ctx context.Context, systemPrompt, userContent string, maxTokens int, timeout time.Duration) (*SingleShotResult, error) {
	ctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	var lastErr error
	for _, ms := range mc.Models {
		result, err := singleShotWithUsage(ctx, ms, systemPrompt, userContent, maxTokens)
		if err != nil {
			slog.Info("single-shot call failed, trying next model", "model", ms.Model, "error", err)
			lastErr = err
			continue
		}
		slog.Info("single-shot call succeeded", "model", ms.Model, "input_tokens", result.InputTokens, "output_tokens", result.OutputTokens)
		return result, nil
	}
	return nil, fmt.Errorf("all models failed (last: %w)", lastErr)
}

// SingleShotStreaming sends a single system+user message pair and calls onDelta
// for each text delta as it arrives. Returns the full result with usage.
// Uses DefaultSingleShotTimeout.
func (mc *ModelChain) SingleShotStreaming(ctx context.Context, systemPrompt, userContent string, maxTokens int, onDelta func(text string)) (*SingleShotResult, error) {
	return mc.SingleShotStreamingWithTimeout(ctx, systemPrompt, userContent, maxTokens, DefaultSingleShotTimeout, onDelta, nil)
}

// SingleShotStreamingWithTimeout is like SingleShotStreaming but with a custom timeout.
// The timeout is shared across all models in the chain — if the first model
// exhausts most of the budget, later models get whatever time remains.
// onUsage, if non-nil, is called with incremental token deltas as usage events
// arrive during streaming, enabling real-time status bar updates.
func (mc *ModelChain) SingleShotStreamingWithTimeout(ctx context.Context, systemPrompt, userContent string, maxTokens int, timeout time.Duration, onDelta func(text string), onUsage func(inputTokens, outputTokens, cacheCreate, cacheRead int)) (*SingleShotResult, error) {
	ctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	var lastErr error
	for _, ms := range mc.Models {
		result, err := singleShotStreamingImpl(ctx, ms, systemPrompt, userContent, maxTokens, onDelta, onUsage)
		if err != nil {
			slog.Info("single-shot streaming call failed, trying next model", "model", ms.Model, "error", err)
			lastErr = err
			continue
		}
		return result, nil
	}
	return nil, fmt.Errorf("all models failed (last: %w)", lastErr)
}

// singleShotStreamingImpl sends a streaming request and invokes onDelta per text chunk.
// The caller is responsible for setting an appropriate deadline on ctx.
// onUsage, if non-nil, receives incremental token deltas as usage events arrive.
func singleShotStreamingImpl(ctx context.Context, ms session.ModelSpec, systemPrompt, userContent string, maxTokens int, onDelta func(text string), onUsage func(inputTokens, outputTokens, cacheCreate, cacheRead int)) (*SingleShotResult, error) {

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
		Stream:          true,
		ReasoningEffort: "none",
	}

	events, errc := ms.Provider.Send(ctx, req)

	var textParts []string
	var usage, reported api.Usage

	for ev := range events {
		if ev.Type == "message_start" && ev.Message != nil {
			usage.InputTokens += ev.Message.Usage.InputTokens + ev.Message.Usage.PromptTokens
			usage.CacheCreationInput += ev.Message.Usage.CacheCreationInput
			usage.CacheReadInput += ev.Message.Usage.CacheReadInput
		}
		if ev.Type == "message_delta" && ev.Usage != nil {
			usage.OutputTokens += ev.Usage.OutputTokens + ev.Usage.CompletionTokens
		}
		if ev.Usage != nil {
			if ev.Usage.InputTokens > 0 || ev.Usage.PromptTokens > 0 {
				usage.InputTokens += ev.Usage.InputTokens + ev.Usage.PromptTokens
			}
			if ev.Usage.OutputTokens > 0 || ev.Usage.CompletionTokens > 0 {
				usage.OutputTokens += ev.Usage.OutputTokens + ev.Usage.CompletionTokens
			}
			usage.CacheCreationInput += ev.Usage.CacheCreationInput
			usage.CacheReadInput += ev.Usage.CacheReadInput
		}

		// Report incremental usage deltas so the caller can update the
		// status bar in real time (e.g., input tokens appear at stream start).
		if onUsage != nil {
			di := usage.InputTokens - reported.InputTokens
			do := usage.OutputTokens - reported.OutputTokens
			dc := usage.CacheCreationInput - reported.CacheCreationInput
			dr := usage.CacheReadInput - reported.CacheReadInput
			if di > 0 || do > 0 || dc > 0 || dr > 0 {
				onUsage(di, do, dc, dr)
				reported = usage
			}
		}

		if ev.Delta != nil {
			var delta struct {
				Type string `json:"type"`
				Text string `json:"text"`
			}
			if err := json.Unmarshal(ev.Delta, &delta); err == nil && delta.Text != "" {
				textParts = append(textParts, delta.Text)
				if onDelta != nil {
					onDelta(delta.Text)
				}
			}
		}
		if ev.ContentBlock != nil && ev.ContentBlock.Text != "" {
			textParts = append(textParts, ev.ContentBlock.Text)
			if onDelta != nil {
				onDelta(ev.ContentBlock.Text)
			}
		}
	}

	if err := <-errc; err != nil {
		return nil, fmt.Errorf("single-shot streaming (%s): %w", ms.Model, err)
	}

	text := strings.TrimSpace(strings.Join(textParts, ""))
	if text == "" {
		return nil, fmt.Errorf("single-shot streaming (%s) returned empty response", ms.Model)
	}

	return &SingleShotResult{
		Text:         text,
		InputTokens:  usage.InputTokens,
		OutputTokens: usage.OutputTokens,
		CacheCreate:  usage.CacheCreationInput,
		CacheRead:    usage.CacheReadInput,
	}, nil
}

// singleShotWith sends the request to a specific model.
// The caller is responsible for setting an appropriate deadline on ctx.
func singleShotWith(ctx context.Context, ms session.ModelSpec, systemPrompt, userContent string, maxTokens int) (string, error) {
	result, err := singleShotWithUsage(ctx, ms, systemPrompt, userContent, maxTokens)
	if err != nil {
		return "", err
	}
	return result.Text, nil
}

// singleShotWithUsage sends the request to a specific model and returns usage.
// The caller is responsible for setting an appropriate deadline on ctx.
func singleShotWithUsage(ctx context.Context, ms session.ModelSpec, systemPrompt, userContent string, maxTokens int) (*SingleShotResult, error) {

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
	var usage api.Usage

	for ev := range events {
		// Capture usage from events.
		if ev.Type == "message_start" && ev.Message != nil {
			usage.InputTokens += ev.Message.Usage.InputTokens + ev.Message.Usage.PromptTokens
			usage.CacheCreationInput += ev.Message.Usage.CacheCreationInput
			usage.CacheReadInput += ev.Message.Usage.CacheReadInput
		}
		if ev.Type == "message_delta" && ev.Usage != nil {
			usage.OutputTokens += ev.Usage.OutputTokens + ev.Usage.CompletionTokens
		}
		if ev.Usage != nil {
			// Fallback: capture any usage we see.
			if ev.Usage.InputTokens > 0 || ev.Usage.PromptTokens > 0 {
				usage.InputTokens += ev.Usage.InputTokens + ev.Usage.PromptTokens
			}
			if ev.Usage.OutputTokens > 0 || ev.Usage.CompletionTokens > 0 {
				usage.OutputTokens += ev.Usage.OutputTokens + ev.Usage.CompletionTokens
			}
			usage.CacheCreationInput += ev.Usage.CacheCreationInput
			usage.CacheReadInput += ev.Usage.CacheReadInput
		}

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
		return nil, fmt.Errorf("single-shot (%s): %w", ms.Model, err)
	}

	// Prefer text content; fall back to extracting the answer from thinking
	// content for reasoning models that put everything in reasoning_content
	// and leave content empty (e.g., kimi-k2.5).
	text := strings.TrimSpace(strings.Join(textParts, ""))
	if text == "" && len(thinkingParts) > 0 {
		text = extractFromThinking(strings.Join(thinkingParts, ""))
	}
	if text == "" {
		return nil, fmt.Errorf("single-shot (%s) returned empty response", ms.Model)
	}

	return &SingleShotResult{
		Text:         text,
		InputTokens:  usage.InputTokens,
		OutputTokens: usage.OutputTokens,
		CacheCreate:  usage.CacheCreationInput,
		CacheRead:    usage.CacheReadInput,
	}, nil
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
