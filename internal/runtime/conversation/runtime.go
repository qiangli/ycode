package conversation

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"time"

	"github.com/qiangli/ycode/internal/api"
	"github.com/qiangli/ycode/internal/runtime/config"
	"github.com/qiangli/ycode/internal/runtime/prompt"
	"github.com/qiangli/ycode/internal/runtime/session"
	"github.com/qiangli/ycode/internal/tools"
)

// Runtime manages the conversation turn loop.
type Runtime struct {
	config      *config.Config
	provider    api.Provider
	session     *session.Session
	registry    *tools.Registry
	promptCtx   *prompt.ProjectContext
	logger      *slog.Logger
}

// NewRuntime creates a new conversation runtime.
func NewRuntime(
	cfg *config.Config,
	provider api.Provider,
	sess *session.Session,
	registry *tools.Registry,
	promptCtx *prompt.ProjectContext,
) *Runtime {
	return &Runtime{
		config:    cfg,
		provider:  provider,
		session:   sess,
		registry:  registry,
		promptCtx: promptCtx,
		logger:    slog.Default(),
	}
}

// TurnResult is the outcome of a single conversation turn.
type TurnResult struct {
	Response        *api.Response
	ToolCalls       []ToolCall
	TextContent     string
	ThinkingContent string
	StopReason      string
	Usage           api.Usage
	Duration        time.Duration
}

// ToolCall represents a tool invocation requested by the model.
type ToolCall struct {
	ID     string          `json:"id"`
	Name   string          `json:"name"`
	Input  json.RawMessage `json:"input"`
	Result string          `json:"result,omitempty"`
	Error  string          `json:"error,omitempty"`
}

// Turn executes one turn of the conversation: send messages, get response, execute tools.
func (r *Runtime) Turn(ctx context.Context, messages []api.Message) (*TurnResult, error) {
	// Build system prompt.
	systemPrompt := prompt.BuildDefault(r.promptCtx)

	// Build tool definitions.
	var toolDefs []api.ToolDefinition
	for _, spec := range r.registry.AlwaysAvailable() {
		toolDefs = append(toolDefs, api.ToolDefinition{
			Name:        spec.Name,
			Description: spec.Description,
			InputSchema: spec.InputSchema,
		})
	}

	// Build API request.
	req := &api.Request{
		Model:     r.config.Model,
		MaxTokens: r.config.MaxTokens,
		System:    systemPrompt,
		Messages:  messages,
		Tools:     toolDefs,
		Stream:    true,
	}

	// Send request and track timing.
	start := time.Now()
	events, errc := r.provider.Send(ctx, req)

	// Accumulate response.
	result := &TurnResult{}
	var currentBlock *api.ContentBlock
	var textParts []string
	var thinkingParts []string

	for ev := range events {
		switch ev.Type {
		case "message_start":
			// Capture input token usage from the initial message.
			if ev.Message != nil {
				result.Usage.InputTokens = ev.Message.Usage.InputTokens
				result.Usage.CacheCreationInput = ev.Message.Usage.CacheCreationInput
				result.Usage.CacheReadInput = ev.Message.Usage.CacheReadInput
			}
		case "content_block_start":
			if ev.ContentBlock != nil {
				block := *ev.ContentBlock
				currentBlock = &block
			} else if ev.Delta != nil {
				var block api.ContentBlock
				if err := json.Unmarshal(ev.Delta, &block); err == nil {
					currentBlock = &block
				}
			}
		case "content_block_delta":
			if ev.Delta != nil {
				var delta struct {
					Type        string `json:"type"`
					Text        string `json:"text"`
					Thinking    string `json:"thinking"`
					PartialJSON string `json:"partial_json,omitempty"`
				}
				if err := json.Unmarshal(ev.Delta, &delta); err == nil {
					if delta.Text != "" {
						textParts = append(textParts, delta.Text)
					}
					if delta.Thinking != "" {
						thinkingParts = append(thinkingParts, delta.Thinking)
					}
					if currentBlock != nil && currentBlock.Type == api.ContentTypeToolUse && delta.PartialJSON != "" {
						currentBlock.Input = append(currentBlock.Input, []byte(delta.PartialJSON)...)
					}
				}
			}
		case "content_block_stop":
			if currentBlock != nil {
				if currentBlock.Type == api.ContentTypeToolUse {
					result.ToolCalls = append(result.ToolCalls, ToolCall{
						ID:    currentBlock.ID,
						Name:  currentBlock.Name,
						Input: currentBlock.Input,
					})
				}
				currentBlock = nil
			}
		case "message_delta":
			// Capture output token usage and stop reason.
			if ev.Usage != nil {
				result.Usage.OutputTokens = ev.Usage.OutputTokens
			}
			if ev.Delta != nil {
				var delta struct {
					StopReason string `json:"stop_reason"`
				}
				if err := json.Unmarshal(ev.Delta, &delta); err == nil {
					result.StopReason = delta.StopReason
				}
			}
		}
	}

	if err := <-errc; err != nil {
		return nil, fmt.Errorf("stream: %w", err)
	}

	result.Duration = time.Since(start)
	result.TextContent = joinParts(textParts)
	result.ThinkingContent = joinParts(thinkingParts)
	return result, nil
}

// ExecuteTools runs tool calls and returns tool result messages.
func (r *Runtime) ExecuteTools(ctx context.Context, calls []ToolCall) []api.ContentBlock {
	var results []api.ContentBlock
	for _, call := range calls {
		output, err := r.registry.Invoke(ctx, call.Name, call.Input)
		block := api.ContentBlock{
			Type:      api.ContentTypeToolResult,
			ToolUseID: call.ID,
		}
		if err != nil {
			block.Content = fmt.Sprintf("Error: %v", err)
			block.IsError = true
		} else {
			block.Content = output
		}
		results = append(results, block)
		r.logger.Info("tool executed", "tool", call.Name, "error", err != nil)
	}
	return results
}

func joinParts(parts []string) string {
	s := ""
	for _, p := range parts {
		s += p
	}
	return s
}
