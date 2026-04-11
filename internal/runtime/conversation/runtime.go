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
	config    *config.Config
	provider  api.Provider
	session   *session.Session
	registry  *tools.Registry
	promptCtx *prompt.ProjectContext
	logger    *slog.Logger
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
				result.Usage.InputTokens = ev.Message.Usage.InputTokens + ev.Message.Usage.PromptTokens
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
				result.Usage.OutputTokens = ev.Usage.OutputTokens + ev.Usage.CompletionTokens
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

// RecoveryResult contains information about context management actions taken.
type RecoveryResult struct {
	CompactedCount  int
	PreservedCount  int
	RetrySuccessful bool
	SummaryPreview  string
	Pruned          bool // Layer 1: tool results were pruned
	PrunedCount     int  // Number of tool results pruned
	Flushed         bool // Layer 3: emergency flush was performed
}

// TurnWithRecovery executes a turn with the 3-layer context defense:
//
//	Layer 1 (Prune):   Soft/hard trim old tool results when approaching threshold
//	Layer 2 (Compact): Full semantic compaction when exceeding threshold
//	Layer 3 (Flush):   Emergency minimal continuation when compaction isn't enough
//
// This is called before each API request to proactively manage context.
func (r *Runtime) TurnWithRecovery(ctx context.Context, messages []api.Message) (*TurnResult, *RecoveryResult, error) {
	sessionMsgs := r.apiMessagesToSession(messages)
	health := session.CheckContextHealth(sessionMsgs)
	recovery := &RecoveryResult{}

	r.logger.Info("context health", "tokens", health.EstimatedTokens, "level", health.Level.String())

	// --- Layer 1: Pruning (in-memory tool result trimming) ---
	if health.NeedsPruning() && !health.NeedsCompactionNow() {
		pruned, pruneResult := session.PruneMessages(sessionMsgs)
		if pruneResult != nil {
			r.logger.Info("layer 1: pruned tool results",
				"soft_trimmed", pruneResult.SoftTrimmed,
				"hard_cleared", pruneResult.HardCleared,
				"tokens_before", pruneResult.TokensBefore,
				"tokens_after", pruneResult.TokensAfter,
			)
			messages = r.sessionMessagesToAPI(pruned)
			recovery.Pruned = true
			recovery.PrunedCount = pruneResult.SoftTrimmed + pruneResult.HardCleared
		}
	}

	// --- Layer 2: Proactive compaction (before hitting API limit) ---
	if health.NeedsCompactionNow() {
		compactResult := r.proactiveCompact(sessionMsgs)
		if compactResult != nil {
			messages = r.buildCompactedMessages(messages, compactResult)
			recovery.CompactedCount = compactResult.CompactedCount
			recovery.PreservedCount = compactResult.PreservedCount
			recovery.RetrySuccessful = true
			recovery.SummaryPreview = truncateSummary(compactResult.Summary, 200)

			r.logger.Info("layer 2: proactive compaction",
				"compacted", compactResult.CompactedCount,
				"preserved", compactResult.PreservedCount,
			)
		}
	}

	// First attempt (with pruned/compacted messages).
	result, err := r.Turn(ctx, messages)
	if err == nil {
		if recovery.Pruned || recovery.CompactedCount > 0 {
			return result, recovery, nil
		}
		return result, nil, nil
	}

	// --- Reactive Layer 2: Compaction on token limit error ---
	var tokenErr *api.TokenLimitError
	if !errors.As(err, &tokenErr) {
		return nil, nil, err
	}

	r.logger.Warn("token limit exceeded, attempting reactive compaction",
		"requested", tokenErr.RequestedTokens,
		"max", tokenErr.MaxTokens,
	)

	sessionMsgs = r.apiMessagesToSession(messages)
	compactResult := r.proactiveCompact(sessionMsgs)
	if compactResult == nil {
		// --- Layer 3: Emergency flush ---
		return r.emergencyFlush(ctx, messages, err)
	}

	compactedMessages := r.buildCompactedMessages(messages, compactResult)

	result, retryErr := r.Turn(ctx, compactedMessages)
	if retryErr != nil {
		if errors.As(retryErr, &tokenErr) {
			// Still too large — try emergency flush.
			return r.emergencyFlush(ctx, messages, retryErr)
		}
		return nil, nil, retryErr
	}

	recovery.CompactedCount = compactResult.CompactedCount
	recovery.PreservedCount = compactResult.PreservedCount
	recovery.RetrySuccessful = true
	recovery.SummaryPreview = truncateSummary(compactResult.Summary, 200)
	return result, recovery, nil
}

// proactiveCompact attempts to compact messages, returning nil if not possible.
func (r *Runtime) proactiveCompact(sessionMsgs []session.ConversationMessage) *session.CompactionResult {
	if len(sessionMsgs) <= session.PreserveLastMessages {
		return nil
	}

	compactResult := session.Compact(sessionMsgs, r.session.Summary)
	if compactResult == nil {
		return nil
	}

	// Update session summary.
	r.session.Summary = compactResult.Summary
	return compactResult
}

// emergencyFlush is Layer 3: when compaction isn't enough, create a minimal
// continuation with just the summary + last user message.
func (r *Runtime) emergencyFlush(ctx context.Context, messages []api.Message, originalErr error) (*TurnResult, *RecoveryResult, error) {
	r.logger.Warn("layer 3: emergency flush — creating minimal continuation")

	// Find the last user message.
	var lastUserMsg *api.Message
	for i := len(messages) - 1; i >= 0; i-- {
		if messages[i].Role == api.RoleUser {
			lastUserMsg = &messages[i]
			break
		}
	}

	if lastUserMsg == nil {
		return nil, nil, fmt.Errorf("emergency flush failed: no user message found: %w", originalErr)
	}

	// Build minimal continuation: summary + last user message.
	summary := r.session.Summary
	if summary == "" {
		summary = "Previous conversation context was too large and has been flushed."
	}

	continuationText := session.GetCompactContinuationMessage(summary, true, false)

	// Inject post-compaction context refresh from CLAUDE.md.
	if r.promptCtx != nil {
		refresh := prompt.PostCompactionRefresh(r.promptCtx.ContextFiles)
		if refresh != "" {
			continuationText += "\n\n" + refresh
		}
	}

	flushMessages := []api.Message{
		{
			Role: api.RoleUser,
			Content: []api.ContentBlock{
				{Type: api.ContentTypeText, Text: continuationText},
			},
		},
		*lastUserMsg,
	}

	result, err := r.Turn(ctx, flushMessages)
	if err != nil {
		return nil, nil, fmt.Errorf("emergency flush retry failed: %w", err)
	}

	recovery := &RecoveryResult{
		CompactedCount:  len(messages) - 1,
		PreservedCount:  1,
		RetrySuccessful: true,
		Flushed:         true,
		SummaryPreview:  truncateSummary(summary, 200),
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

// sessionMessagesToAPI converts session messages back to API messages.
func (r *Runtime) sessionMessagesToAPI(messages []session.ConversationMessage) []api.Message {
	var result []api.Message
	for _, msg := range messages {
		var blocks []api.ContentBlock
		for _, b := range msg.Content {
			blocks = append(blocks, api.ContentBlock{
				Type:      api.ContentType(b.Type),
				Text:      b.Text,
				ID:        b.ID,
				Name:      b.Name,
				Input:     b.Input,
				ToolUseID: b.ToolUseID,
				Content:   b.Content,
				IsError:   b.IsError,
			})
		}
		result = append(result, api.Message{
			Role:    api.MessageRole(msg.Role),
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
	keepCount := min(compactResult.PreservedCount, len(original))

	// Start with a system-like message containing the summary
	var result []api.Message

	// Add the compacted summary as a user message (since most APIs don't support multiple system messages)
	// We use a special marker to indicate this is context continuation
	continuationMsg := session.GetCompactContinuationMessage(compactResult.Summary, true, true)

	// Post-compaction context refresh: re-inject critical CLAUDE.md sections.
	if r.promptCtx != nil {
		refresh := prompt.PostCompactionRefresh(r.promptCtx.ContextFiles)
		if refresh != "" {
			continuationMsg += "\n\n" + refresh
		}
	}

	result = append(result, api.Message{
		Role: api.RoleUser,
		Content: []api.ContentBlock{
			{Type: api.ContentTypeText, Text: continuationMsg},
		},
	})

	// Add the preserved recent messages
	preservedStart := max(len(original)-keepCount, 0)
	result = append(result, original[preservedStart:]...)

	return result
}

func truncateSummary(s string, maxLen int) string {
	if len(s) <= maxLen {
		return s
	}
	return s[:maxLen] + "..."
}
