package conversation

import (
	"context"
	"encoding/json"
	"errors"
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

// RecoveryResult contains information about a successful recovery from token limit error.
type RecoveryResult struct {
	CompactedCount   int
	PreservedCount   int
	RetrySuccessful  bool
	SummaryPreview   string
}

// TurnWithRecovery executes a turn with automatic recovery from token limit errors.
// If the request exceeds the model's token limit, it compacts older messages and retries.
func (r *Runtime) TurnWithRecovery(ctx context.Context, messages []api.Message) (*TurnResult, *RecoveryResult, error) {
	// First attempt
	result, err := r.Turn(ctx, messages)
	if err == nil {
		return result, nil, nil
	}

	// Check if this is a token limit error
	var tokenErr *api.TokenLimitError
	if !errors.As(err, &tokenErr) {
		return nil, nil, err // Not a token limit error, return as-is
	}

	r.logger.Warn("token limit exceeded, attempting recovery via compaction",
		"requested", tokenErr.RequestedTokens,
		"max", tokenErr.MaxTokens,
	)

	// Convert api.Messages to session messages for compaction
	sessionMsgs := r.apiMessagesToSession(messages)

	// Check if we have enough messages to compact
	if len(sessionMsgs) <= session.PreserveLastMessages {
		r.logger.Error("cannot compact: not enough messages to preserve minimum",
			"messages", len(sessionMsgs),
			"preserve_min", session.PreserveLastMessages,
		)
		return nil, nil, fmt.Errorf("token limit exceeded and cannot compact further: %w", err)
	}

	// Perform compaction
	compactResult := session.Compact(sessionMsgs, r.session.Summary)
	if compactResult == nil {
		return nil, nil, fmt.Errorf("token limit exceeded and compaction failed: %w", err)
	}

	r.logger.Info("compacted messages for recovery",
		"compacted_count", compactResult.CompactedCount,
		"preserved_count", compactResult.PreservedCount,
	)

	// Update session summary
	r.session.Summary = compactResult.Summary

	// Build compacted messages: summary as system-like message + preserved messages
	compactedMessages := r.buildCompactedMessages(messages, compactResult)

	// Retry with compacted messages
	result, retryErr := r.Turn(ctx, compactedMessages)
	if retryErr != nil {
		// Check if it's still a token limit error - we may need multiple compaction rounds
		if errors.As(retryErr, &tokenErr) {
			return nil, nil, fmt.Errorf("token limit exceeded even after compaction: %w", retryErr)
		}
		return nil, nil, retryErr
	}

	// Success! Return the result and recovery info
	recovery := &RecoveryResult{
		CompactedCount:  compactResult.CompactedCount,
		PreservedCount:  compactResult.PreservedCount,
		RetrySuccessful: true,
		SummaryPreview:  truncateSummary(compactResult.Summary, 200),
	}

	return result, recovery, nil
}

// apiMessagesToSession converts API messages to session messages for compaction analysis.
func (r *Runtime) apiMessagesToSession(messages []api.Message) []session.ConversationMessage {
	var result []session.ConversationMessage
	for _, msg := range messages {
		var blocks []session.ContentBlock
		for _, b := range msg.Content {
			blocks = append(blocks, session.ContentBlock{
				Type:      session.ContentType(b.Type),
				Text:      b.Text,
				ID:        b.ID,
				Name:      b.Name,
				Input:     b.Input,
				ToolUseID: b.ToolUseID,
				Content:   b.Content,
				IsError:   b.IsError,
			})
		}
		result = append(result, session.ConversationMessage{
			Role:    session.MessageRole(msg.Role),
			Content: blocks,
		})
	}
	return result
}

// buildCompactedMessages rebuilds the API message list with compacted history.
func (r *Runtime) buildCompactedMessages(original []api.Message, compactResult *session.CompactionResult) []api.Message {
	if compactResult == nil || len(original) == 0 {
		return original
	}

	// Calculate how many messages to keep from the end
	keepCount := compactResult.PreservedCount
	if keepCount > len(original) {
		keepCount = len(original)
	}

	// Start with a system-like message containing the summary
	var result []api.Message

	// Add the compacted summary as a user message (since most APIs don't support multiple system messages)
	// We use a special marker to indicate this is context continuation
	continuationMsg := session.GetCompactContinuationMessage(compactResult.Summary, true, true)
	result = append(result, api.Message{
		Role: api.RoleUser,
		Content: []api.ContentBlock{
			{Type: api.ContentTypeText, Text: continuationMsg},
		},
	})

	// Add the preserved recent messages
	preservedStart := len(original) - keepCount
	if preservedStart < 0 {
		preservedStart = 0
	}
	result = append(result, original[preservedStart:]...)

	return result
}

func truncateSummary(s string, maxLen int) string {
	if len(s) <= maxLen {
		return s
	}
	return s[:maxLen] + "..."
}
