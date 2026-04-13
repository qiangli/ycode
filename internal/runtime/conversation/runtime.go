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
	"github.com/qiangli/ycode/internal/runtime/permission"
	"github.com/qiangli/ycode/internal/runtime/prompt"
	"github.com/qiangli/ycode/internal/runtime/session"
	"github.com/qiangli/ycode/internal/runtime/taskqueue"
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

	// Differential context injection for non-caching providers.
	cachingSupported bool
	contextBaseline  *prompt.ContextBaseline

	// Optional LLM-based summarizer for compaction. If nil, heuristic is used.
	llmSummarizer *session.LLMSummarizer

	// Plan mode — when true, write tools are filtered out and plan-mode
	// instructions are injected into the system prompt.
	planMode bool

	// Optional OTEL instrumentation.
	otel *OTELConfig

	// Optional streaming event callback. Called for each text/thinking delta
	// and tool call event as they arrive from the LLM provider.
	onEvent func(eventType string, data map[string]any)
}

// NewRuntime creates a new conversation runtime.
func NewRuntime(
	cfg *config.Config,
	provider api.Provider,
	sess *session.Session,
	registry *tools.Registry,
	promptCtx *prompt.ProjectContext,
) *Runtime {
	caps := api.DetectCapabilities(provider.Kind(), cfg.Model)
	cachingSupported := caps.CachingSupported
	// Allow config override for caching detection.
	if cfg.ProviderCapabilities != nil && cfg.ProviderCapabilities.CachingSupported != nil {
		cachingSupported = *cfg.ProviderCapabilities.CachingSupported
	}
	return &Runtime{
		config:           cfg,
		provider:         provider,
		session:          sess,
		registry:         registry,
		promptCtx:        promptCtx,
		logger:           slog.Default(),
		cachingSupported: cachingSupported,
		contextBaseline:  prompt.NewContextBaseline(),
	}
}

// SetLLMSummarizer enables LLM-based compaction summarization.
// When set, compaction will use the LLM for higher-fidelity summaries,
// falling back to heuristic extraction on failure.
func (r *Runtime) SetLLMSummarizer(s *session.LLMSummarizer) {
	r.llmSummarizer = s
}

// SetPlanMode enables or disables plan mode for this runtime.
func (r *Runtime) SetPlanMode(enabled bool) {
	r.planMode = enabled
}

// SetEventCallback sets a callback that receives streaming events as they
// arrive from the LLM provider. The callback receives an event type string
// and a data map. This allows the service layer to publish bus events
// without the runtime depending on the bus package.
func (r *Runtime) SetEventCallback(fn func(eventType string, data map[string]any)) {
	r.onEvent = fn
}

// emitEvent calls the event callback if set.
func (r *Runtime) emitEvent(eventType string, data map[string]any) {
	if r.onEvent != nil {
		r.onEvent(eventType, data)
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
	systemPrompt := prompt.BuildDefault(r.promptCtx, r.planMode, r.cachingSupported, r.contextBaseline)

	// Build tool definitions — in plan mode, exclude tools requiring write access.
	var toolSpecs []*tools.ToolSpec
	if r.planMode {
		toolSpecs = r.registry.AlwaysAvailableForMode(permission.ReadOnly)
	} else {
		toolSpecs = r.registry.AlwaysAvailable()
	}
	var toolDefs []api.ToolDefinition
	for _, spec := range toolSpecs {
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
						r.emitEvent("text.delta", map[string]any{"text": delta.Text})
					}
					if delta.Thinking != "" {
						thinkingParts = append(thinkingParts, delta.Thinking)
						r.emitEvent("thinking.delta", map[string]any{"text": delta.Thinking})
					}
					if currentBlock != nil && currentBlock.Type == api.ContentTypeToolUse && delta.PartialJSON != "" {
						currentBlock.Input = append(currentBlock.Input, []byte(delta.PartialJSON)...)
					}
				}
			}
		case "content_block_stop":
			if currentBlock != nil {
				if currentBlock.Type == api.ContentTypeToolUse {
					tc := ToolCall{
						ID:    currentBlock.ID,
						Name:  currentBlock.Name,
						Input: currentBlock.Input,
					}
					result.ToolCalls = append(result.ToolCalls, tc)
					r.emitEvent("tool_use.start", map[string]any{
						"id":   tc.ID,
						"tool": tc.Name,
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
// If parallel execution is enabled and there are multiple calls, they run
// concurrently within per-category limits. Progress events are sent to the
// progress channel if non-nil; the caller must close it after this returns.
func (r *Runtime) ExecuteTools(ctx context.Context, calls []ToolCall, progress chan<- taskqueue.TaskEvent) []api.ContentBlock {
	if !r.config.Parallel.Enabled || len(calls) <= 1 {
		return r.executeToolsSequential(ctx, calls, progress)
	}
	return r.executeToolsParallel(ctx, calls, progress)
}

// executeToolsSequential runs tool calls one at a time (original behavior).
func (r *Runtime) executeToolsSequential(ctx context.Context, calls []ToolCall, progress chan<- taskqueue.TaskEvent) []api.ContentBlock {
	n := len(calls)
	results := make([]api.ContentBlock, 0, n)
	for i, call := range calls {
		if progress != nil {
			progress <- taskqueue.TaskEvent{Index: i, Name: call.Name, Status: taskqueue.StatusRunning, Total: n}
		}
		output, err := r.registry.Invoke(ctx, call.Name, call.Input)
		block := api.ContentBlock{
			Type:      api.ContentTypeToolResult,
			ToolUseID: call.ID,
		}
		if err != nil {
			block.Content = fmt.Sprintf("Error: %v", err)
			block.IsError = true
			if progress != nil {
				progress <- taskqueue.TaskEvent{Index: i, Name: call.Name, Status: taskqueue.StatusFailed, Total: n}
			}
		} else {
			block.Content = output
			if progress != nil {
				progress <- taskqueue.TaskEvent{Index: i, Name: call.Name, Status: taskqueue.StatusCompleted, Total: n}
			}
		}
		results = append(results, block)
		r.logger.Info("tool executed", "tool", call.Name, "error", err != nil)
	}
	return results
}

// executeToolsParallel runs tool calls concurrently using the task queue.
func (r *Runtime) executeToolsParallel(ctx context.Context, calls []ToolCall, progress chan<- taskqueue.TaskEvent) []api.ContentBlock {
	qCalls := make([]taskqueue.Call, len(calls))
	for i, call := range calls {
		spec, _ := r.registry.Get(call.Name)
		cat := taskqueue.CatStandard
		if spec != nil {
			switch spec.Category {
			case tools.CategoryLLM:
				cat = taskqueue.CatLLM
			case tools.CategoryInteractive:
				cat = taskqueue.CatInteractive
			}
		}
		c := call
		qCalls[i] = taskqueue.Call{
			Index:    i,
			Name:     call.Name,
			Detail:   call.Name,
			Category: cat,
			Invoke: func(ctx context.Context) (string, error) {
				return r.registry.Invoke(ctx, c.Name, c.Input)
			},
		}
	}

	exec := taskqueue.NewExecutor(r.config.Parallel.MaxStandard, r.config.Parallel.MaxLLM)
	taskResults := exec.Run(ctx, qCalls, progress)

	blocks := make([]api.ContentBlock, len(calls))
	for i, res := range taskResults {
		block := api.ContentBlock{
			Type:      api.ContentTypeToolResult,
			ToolUseID: calls[i].ID,
		}
		if res.Err != nil {
			block.Content = fmt.Sprintf("Error: %v", res.Err)
			block.IsError = true
		} else {
			block.Content = res.Output
		}
		blocks[i] = block
		r.logger.Info("tool executed", "tool", calls[i].Name, "error", res.Err != nil)
	}
	return blocks
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
		compactResult := r.proactiveCompactCtx(ctx, sessionMsgs)
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
	compactResult := r.proactiveCompactCtx(ctx, sessionMsgs)
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
	return r.proactiveCompactCtx(context.Background(), sessionMsgs)
}

// proactiveCompactCtx attempts to compact messages with a context for LLM calls.
func (r *Runtime) proactiveCompactCtx(ctx context.Context, sessionMsgs []session.ConversationMessage) *session.CompactionResult {
	if len(sessionMsgs) <= session.PreserveLastMessages {
		return nil
	}

	// Determine which messages will be compacted (for search indexing).
	compactedPrefixLen := 0
	if len(sessionMsgs) > 0 && session.HasCompactedPrefix(sessionMsgs[0]) {
		compactedPrefixLen = 1
	}

	var compactResult *session.CompactionResult
	if r.llmSummarizer != nil {
		compactResult = session.CompactWithLLM(ctx, sessionMsgs, r.session.Summary, r.llmSummarizer)
	} else {
		compactResult = session.Compact(sessionMsgs, r.session.Summary)
	}
	if compactResult == nil {
		return nil
	}

	// Update session summary.
	r.session.Summary = compactResult.Summary

	// Index compacted messages in Bleve for search (best-effort).
	if indexer := r.session.SearchIndexer(); indexer != nil {
		keepFrom := len(sessionMsgs) - compactResult.PreservedCount
		compactedMessages := sessionMsgs[compactedPrefixLen:keepFrom]
		indexer.IndexCompaction(compactResult, compactedMessages)
	}

	// Reset differential context baseline — next turn must send full context.
	r.contextBaseline.Reset()

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

	// Reset differential context baseline — next turn must send full context.
	r.contextBaseline.Reset()

	sanitizedUserMsg := sanitizeUserMessageForFlush(*lastUserMsg)

	flushMessages := []api.Message{
		{
			Role: api.RoleUser,
			Content: []api.ContentBlock{
				{Type: api.ContentTypeText, Text: continuationText},
			},
		},
		sanitizedUserMsg,
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

// sanitizeUserMessageForFlush removes tool_result blocks from a user message
// to prevent orphaned tool_call_id references after emergency flush discards
// the assistant messages that contained the matching tool_use blocks.
func sanitizeUserMessageForFlush(msg api.Message) api.Message {
	var filtered []api.ContentBlock
	for _, b := range msg.Content {
		if b.Type != api.ContentTypeToolResult {
			filtered = append(filtered, b)
		}
	}
	if len(filtered) == 0 {
		filtered = []api.ContentBlock{
			{Type: api.ContentTypeText, Text: "Please continue from where we left off."},
		}
	}
	return api.Message{
		Role:    msg.Role,
		Content: filtered,
	}
}

// CompactNow triggers an immediate compaction of the current session messages,
// regardless of token count. Returns the compaction result summary.
// This is used by the compact_context tool to allow the agent to request compaction.
func (r *Runtime) CompactNow(ctx context.Context, messages []api.Message) (*session.CompactionResult, error) {
	sessionMsgs := r.apiMessagesToSession(messages)
	if len(sessionMsgs) <= session.PreserveLastMessages {
		return nil, fmt.Errorf("too few messages to compact (have %d, need >%d)", len(sessionMsgs), session.PreserveLastMessages)
	}

	result := r.proactiveCompactCtx(ctx, sessionMsgs)
	if result == nil {
		return nil, fmt.Errorf("compaction produced no result")
	}

	return result, nil
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
