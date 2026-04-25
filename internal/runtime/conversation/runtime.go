package conversation

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"path/filepath"
	"strings"
	"time"

	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/trace"

	"github.com/qiangli/ycode/internal/api"
	"github.com/qiangli/ycode/internal/runtime/config"
	"github.com/qiangli/ycode/internal/runtime/permission"
	"github.com/qiangli/ycode/internal/runtime/prompt"
	"github.com/qiangli/ycode/internal/runtime/session"
	"github.com/qiangli/ycode/internal/runtime/taskqueue"
	"github.com/qiangli/ycode/internal/tools"
)

// MaxOutputTokenCap is the safety cap for output tokens per response.
// Prevents runaway responses from wasting tokens. Matches opencode's default.
const MaxOutputTokenCap = 32_000

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

	// Tool output distillation config.
	distillCfg   session.DistillConfig
	routingCache *session.RoutingCache

	// Optional LLM-based summarizer for compaction. If nil, heuristic is used.
	llmSummarizer *session.LLMSummarizer

	// Context budget for history caps and compaction thresholds.
	contextBudget session.ContextBudget

	// Activated deferred tools — tools discovered via ToolSearch that must
	// be included in subsequent API requests so the provider accepts tool_use calls.
	// Value is the turn number when the tool was last used/activated.
	activatedTools map[string]int

	// turnCount tracks the current conversation turn for tool expiration.
	turnCount int

	// Completion cache — short-TTL cache that skips the LLM entirely
	// for identical requests (retries, error recovery).
	completionCache *api.CompletionCache

	// Cache warmer — background pings to keep prompt cache alive.
	cacheWarmer *api.CacheWarmer

	// JIT instruction discovery — discovers AGENTS.md/CLAUDE.md in subdirs
	// when tools access files, merges them before the next turn.
	jitDiscovery *prompt.JITDiscovery

	// L1 working memory — tracks the current high-level task focus
	// extracted from user messages and injected into the system prompt.
	topicTracker *prompt.TopicTracker

	// Agent mode — controls system prompt assembly and tool filtering.
	// Values: "build" (default), "plan" (read-only), "explore" (subagent).
	mode string

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
	contextBudget := session.ContextBudgetForProvider(caps.MaxContextTokens, cachingSupported)

	var distillCfg session.DistillConfig
	if cachingSupported {
		distillCfg = session.DefaultDistillConfig()
	} else {
		distillCfg = session.AggressiveDistillConfig()
	}
	if sess != nil {
		distillCfg.FullOutputDir = sess.ToolOutputDir()
	}

	// Set up completion cache directory under the session.
	var completionCacheDir string
	if sess != nil {
		completionCacheDir = filepath.Join(sess.Dir, "completion-cache")
	}

	// Initialize JIT instruction discovery.
	seen := make(map[string]bool)
	totalChars := 0
	for _, cf := range promptCtx.ContextFiles {
		if cf.Hash != "" {
			seen[cf.Hash] = true
		}
		totalChars += len(cf.Content)
	}
	projectRoot := promptCtx.ProjectRoot
	if projectRoot == "" {
		projectRoot = promptCtx.WorkDir
	}
	jit := prompt.NewJITDiscovery(projectRoot, seen, totalChars)

	r := &Runtime{
		config:           cfg,
		provider:         provider,
		session:          sess,
		registry:         registry,
		promptCtx:        promptCtx,
		logger:           slog.Default(),
		cachingSupported: cachingSupported,
		contextBaseline:  prompt.NewContextBaseline(),
		contextBudget:    contextBudget,
		distillCfg:       distillCfg,
		routingCache:     session.NewRoutingCache(),
		activatedTools:   make(map[string]int),
		completionCache:  api.NewCompletionCache(completionCacheDir, api.CompletionCacheTTL),
		jitDiscovery:     jit,
		topicTracker:     prompt.NewTopicTracker(),
	}

	// Wire file access hook so tools trigger JIT discovery.
	registry.SetFileAccessHook(func(path string) {
		jit.OnToolAccess(path)
	})

	return r
}

// SetLLMSummarizer enables LLM-based compaction summarization.
// When set, compaction will use the LLM for higher-fidelity summaries,
// falling back to heuristic extraction on failure.
func (r *Runtime) SetLLMSummarizer(s *session.LLMSummarizer) {
	r.llmSummarizer = s
}

// SetCacheWarmer enables background cache warming for prompt caching providers.
func (r *Runtime) SetCacheWarmer(cw *api.CacheWarmer) {
	r.cacheWarmer = cw
}

// SetMode sets the agent mode for this runtime ("build", "plan", or "explore").
func (r *Runtime) SetMode(mode string) {
	r.mode = mode
}

// Mode returns the current agent mode.
func (r *Runtime) Mode() string {
	if r.mode == "" {
		return "build"
	}
	return r.mode
}

// TopicTracker returns the L1 working memory topic tracker.
func (r *Runtime) TopicTracker() *prompt.TopicTracker {
	return r.topicTracker
}

// RestoreTopicFromGhost loads the active topic from the latest ghost snapshot.
// Called on session resume to restore L1 working memory state.
func (r *Runtime) RestoreTopicFromGhost() {
	if r.topicTracker == nil || r.session == nil {
		return
	}
	ghost, err := session.LoadLatestGhost(r.session.Dir)
	if err != nil {
		r.logger.Warn("failed to load ghost snapshot for topic restore", "error", err)
		return
	}
	if ghost != nil && ghost.ActiveTopic != "" {
		r.topicTracker.SetTopic(ghost.ActiveTopic)
		r.logger.Info("restored active topic from ghost", "topic", ghost.ActiveTopic)
	}
}

// SetPlanMode enables or disables plan mode for this runtime.
func (r *Runtime) SetPlanMode(enabled bool) {
	if enabled {
		r.mode = "plan"
	} else {
		r.mode = "build"
	}
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

// activatedToolTTL is the number of turns after which an activated deferred
// tool is expired if not used. Keeps the tool list lean in long sessions.
const activatedToolTTL = 3

// Turn executes one turn of the conversation: send messages, get response, execute tools.
func (r *Runtime) Turn(ctx context.Context, messages []api.Message) (*TurnResult, error) {
	r.turnCount++

	// Merge any JIT-discovered instruction files into the prompt context.
	if r.jitDiscovery != nil {
		if newFiles := r.jitDiscovery.DrainPending(); len(newFiles) > 0 {
			r.promptCtx.ContextFiles = append(r.promptCtx.ContextFiles, newFiles...)
			r.logger.Info("JIT instruction discovery", "newFiles", len(newFiles), "total", len(r.promptCtx.ContextFiles))
		}
	}

	// L1 working memory: extract active topic from the latest user message.
	if r.topicTracker != nil {
		if userMsg := lastUserText(messages); userMsg != "" {
			r.topicTracker.Update(userMsg)
		}
		r.promptCtx.ActiveTopic = r.topicTracker.CurrentTopic()
	}

	// Build system prompt.
	systemPrompt := prompt.BuildDefault(r.promptCtx, r.Mode(), r.cachingSupported, r.contextBaseline)

	// Build tool definitions — in plan/explore mode, exclude tools requiring write access.
	var toolSpecs []*tools.ToolSpec
	switch r.Mode() {
	case "plan", "explore":
		toolSpecs = r.registry.AlwaysAvailableForMode(permission.ReadOnly)
	default:
		toolSpecs = r.registry.AlwaysAvailable()
	}

	// Expire activated tools not used in the last N turns.
	for name, lastUsed := range r.activatedTools {
		if r.turnCount-lastUsed > activatedToolTTL {
			delete(r.activatedTools, name)
			r.logger.Info("expired deferred tool", "tool", name, "lastUsed", lastUsed, "turn", r.turnCount)
		}
	}

	// Include activated deferred tools (discovered via ToolSearch).
	seen := make(map[string]bool, len(toolSpecs))
	for _, s := range toolSpecs {
		seen[s.Name] = true
	}
	for name := range r.activatedTools {
		if seen[name] {
			continue
		}
		if spec, ok := r.registry.Get(name); ok {
			toolSpecs = append(toolSpecs, spec)
		}
	}

	var toolDefs []api.ToolDefinition
	for _, spec := range toolSpecs {
		toolDefs = append(toolDefs, api.ToolDefinition{
			Name:        spec.Name,
			Description: spec.Description,
			InputSchema: spec.InputSchema,
		})
	}

	// Cap output tokens to prevent runaway responses.
	maxTokens := r.config.MaxTokens
	if maxTokens <= 0 || maxTokens > MaxOutputTokenCap {
		maxTokens = MaxOutputTokenCap
	}

	// Build API request.
	req := &api.Request{
		Model:     r.config.Model,
		MaxTokens: maxTokens,
		System:    systemPrompt,
		Messages:  messages,
		Tools:     toolDefs,
		Stream:    true,
	}

	// Check completion cache — skip the LLM entirely for identical requests.
	reqHash := api.RequestHash(req)
	if cached := r.completionCache.Lookup(reqHash); cached != nil {
		result := r.responseToTurnResult(cached)
		result.Duration = 0 // Instant — from cache.
		return result, nil
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

	// Cache the completed response for short-TTL reuse.
	r.completionCache.Store(reqHash, &api.Response{
		Content:    r.turnResultToContentBlocks(result),
		StopReason: result.StopReason,
		Usage:      result.Usage,
	})

	// Update cache warmer context so keep-alive pings use current system prompt.
	if r.cacheWarmer != nil {
		r.cacheWarmer.UpdateContext(req.Model, req.System, req.Tools)
		r.cacheWarmer.Start() // no-op if already running
	}

	return result, nil
}

// responseToTurnResult converts a cached api.Response into a TurnResult.
func (r *Runtime) responseToTurnResult(resp *api.Response) *TurnResult {
	result := &TurnResult{
		Response:   resp,
		StopReason: resp.StopReason,
		Usage:      resp.Usage,
	}
	for _, block := range resp.Content {
		switch block.Type {
		case api.ContentTypeText:
			result.TextContent += block.Text
		case api.ContentTypeToolUse:
			result.ToolCalls = append(result.ToolCalls, ToolCall{
				ID:    block.ID,
				Name:  block.Name,
				Input: block.Input,
			})
		}
	}
	return result
}

// turnResultToContentBlocks converts a TurnResult into api.ContentBlocks for caching.
func (r *Runtime) turnResultToContentBlocks(result *TurnResult) []api.ContentBlock {
	var blocks []api.ContentBlock
	if result.TextContent != "" {
		blocks = append(blocks, api.ContentBlock{
			Type: api.ContentTypeText,
			Text: result.TextContent,
		})
	}
	for _, tc := range result.ToolCalls {
		blocks = append(blocks, api.ContentBlock{
			Type:  api.ContentTypeToolUse,
			ID:    tc.ID,
			Name:  tc.Name,
			Input: tc.Input,
		})
	}
	return blocks
}

// ExecuteTools runs tool calls and returns tool result messages.
// If parallel execution is enabled and there are multiple calls, they run
// concurrently within per-category limits. Progress events are sent to the
// progress channel if non-nil; the caller must close it after this returns.
func (r *Runtime) ExecuteTools(ctx context.Context, calls []ToolCall, progress chan<- taskqueue.TaskEvent) []api.ContentBlock {
	// Create parent span if OTEL is configured — child tool spans from
	// middleware become children automatically via context propagation.
	if r.otel != nil && r.otel.Tracer != nil {
		toolNames := make([]string, len(calls))
		for i, c := range calls {
			toolNames[i] = c.Name
		}
		var span trace.Span
		ctx, span = r.otel.Tracer.Start(ctx, "ycode.tools.execute",
			trace.WithAttributes(
				attribute.Int("tools.count", len(calls)),
				attribute.Bool("tools.parallel", r.config.Parallel.Enabled && len(calls) > 1),
				attribute.String("tools.names", strings.Join(toolNames, ",")),
			),
		)
		defer span.End()
	}

	var results []api.ContentBlock
	if !r.config.Parallel.Enabled || len(calls) <= 1 {
		results = r.executeToolsSequential(ctx, calls, progress)
	} else {
		results = r.executeToolsParallel(ctx, calls, progress)
	}

	// Activate deferred tools discovered via ToolSearch so they are included
	// in subsequent API requests (providers reject tool_use for unknown tools).
	for i, call := range calls {
		if call.Name == "ToolSearch" && i < len(results) && !results[i].IsError {
			r.activateToolsFromResult(results[i].Content)
		}
		// Refresh turn counter for activated tools that are actually used.
		if _, ok := r.activatedTools[call.Name]; ok {
			r.activatedTools[call.Name] = r.turnCount
		}
	}

	return r.distillResults(calls, results)
}

// activateToolsFromResult parses a ToolSearch result and activates the
// discovered tools so their schemas are included in subsequent API requests.
func (r *Runtime) activateToolsFromResult(content string) {
	// ToolSearch returns tool names in JSON objects within <function> tags.
	// Extract tool names by looking for "name":"..." patterns.
	for _, name := range r.registry.Names() {
		// Check if the tool name appears in the ToolSearch output (as a JSON field value).
		if strings.Contains(content, `"name":"`+name+`"`) || strings.Contains(content, `"name": "`+name+`"`) {
			if _, ok := r.activatedTools[name]; !ok {
				r.logger.Info("activated deferred tool", "tool", name)
			}
			r.activatedTools[name] = r.turnCount
		}
	}
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
			case tools.CategoryAgent:
				cat = taskqueue.CatAgent
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

	exec := taskqueue.NewExecutor(r.config.Parallel.MaxStandard, r.config.Parallel.MaxLLM, r.config.Parallel.MaxAgent)
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

// distillResults applies content routing and tool output distillation to reduce
// token usage. The pipeline per tool result is:
//
//  1. Route: classify as Full/Partial/Summary/Excluded based on tool type + size
//  2. If RouteFull → run through DistillToolOutput (may still truncate large content)
//     Otherwise → apply the route transformation directly
//
// Error results are never distilled — they contain critical diagnostics.
func (r *Runtime) distillResults(calls []ToolCall, results []api.ContentBlock) []api.ContentBlock {
	for i := range results {
		if results[i].IsError || results[i].Type != api.ContentTypeToolResult {
			continue
		}
		// Find the matching tool name for this result.
		toolName := ""
		for _, call := range calls {
			if call.ID == results[i].ToolUseID {
				toolName = call.Name
				break
			}
		}

		before := len(results[i].Content)

		// Step 1: Content routing — classify the result.
		route := session.RouteContent(toolName, results[i].Content, false, r.routingCache, r.distillCfg.AggressiveMode)

		// Step 2: Apply route or distillation.
		switch route {
		case session.RouteFull:
			// Full route still goes through distillation (catches byte/line limits).
			results[i].Content = session.DistillToolOutput(toolName, results[i].Content, r.distillCfg)
		default:
			// Partial/Summary/Excluded — apply the route transformation.
			results[i].Content = session.ApplyRoute(results[i].Content, route, toolName)
		}

		if after := len(results[i].Content); after < before {
			r.logger.Info("distilled tool output",
				"tool", toolName,
				"route", string(route),
				"before", before,
				"after", after,
			)
		}
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
	Masked          bool // Layer 0: old observations were masked
	MaskedCount     int  // Number of old tool results masked
	Pruned          bool // Layer 1: tool results were pruned
	PrunedCount     int  // Number of tool results pruned
	Flushed         bool // Layer 3: emergency flush was performed
}

// TurnWithRecovery executes a turn with the 4-layer context defense:
//
//	Layer 0 (Mask):    Replace old tool results outside attention window with placeholder
//	Layer 1 (Prune):   Soft/hard trim old tool results when approaching threshold
//	Layer 2 (Compact): Full semantic compaction when exceeding threshold
//	Layer 3 (Flush):   Emergency minimal continuation when compaction isn't enough
//
// This is called before each API request to proactively manage context.
func (r *Runtime) TurnWithRecovery(ctx context.Context, messages []api.Message) (*TurnResult, *RecoveryResult, error) {
	sessionMsgs := r.apiMessagesToSession(messages)
	recovery := &RecoveryResult{}

	// --- Layer 0: Observation masking (cheapest defense — always runs) ---
	// Uses token-budget-based masking: protects newest tool outputs up to a budget,
	// only masks when prunable tokens exceed a batch threshold.
	// Non-caching providers use tighter budgets.
	protectionBudget := session.ToolMaskingProtectionBudget
	minPrunable := session.ToolMaskingMinPrunable
	if !r.cachingSupported {
		protectionBudget = protectionBudget * 60 / 100 // 30K for non-caching
		minPrunable = minPrunable * 60 / 100           // 18K for non-caching
	}
	masked, maskedCount := session.MaskOldObservationsBudget(sessionMsgs, protectionBudget, minPrunable)
	if maskedCount > 0 {
		r.logger.Info("layer 0: masked old observations", "count", maskedCount)
		sessionMsgs = masked
		messages = r.sessionMessagesToAPI(masked)
		recovery.Masked = true
		recovery.MaskedCount = maskedCount
	}

	// Merge adjacent user messages to reduce per-message structural overhead.
	// This is especially valuable for non-caching providers where system reminders
	// and dynamic injections are stored as separate user messages.
	sessionMsgs = session.MergeAdjacentUserMessages(sessionMsgs)
	messages = r.sessionMessagesToAPI(sessionMsgs)

	health := session.CheckContextHealth(sessionMsgs)
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
	historyBudget := r.contextBudget.MaxChatHistoryTokens
	if r.llmSummarizer != nil {
		compactResult = session.CompactWithLLM(ctx, sessionMsgs, r.session.Summary, r.llmSummarizer, historyBudget)
	} else {
		compactResult = session.Compact(sessionMsgs, r.session.Summary, historyBudget)
	}
	if compactResult == nil {
		return nil
	}

	// Update session summary.
	r.session.Summary = compactResult.Summary

	// Save ghost snapshot with active topic before compaction completes.
	if r.session != nil && r.topicTracker != nil {
		snap := &session.GhostSnapshot{
			Timestamp:    time.Now(),
			MessageCount: len(sessionMsgs),
			Summary:      compactResult.Summary,
			ActiveTopic:  r.topicTracker.CurrentTopic(),
		}
		if err := session.SaveGhostSnapshot(r.session.Dir, snap); err != nil {
			r.logger.Warn("failed to save ghost snapshot", "error", err)
		}
	}

	// Index compacted messages in Bleve for search (best-effort).
	if indexer := r.session.SearchIndexer(); indexer != nil {
		keepFrom := len(sessionMsgs) - compactResult.PreservedCount
		compactedMessages := sessionMsgs[compactedPrefixLen:keepFrom]
		indexer.IndexCompaction(compactResult, compactedMessages)
	}

	// Reset differential context baseline — next turn must send full context.
	r.contextBaseline.Reset()

	// Clear completion cache — context has changed, old responses are invalid.
	r.completionCache.Clear()

	// Restart cache warmer — cached context has changed.
	if r.cacheWarmer != nil {
		r.cacheWarmer.Restart()
	}

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

// lastUserText returns the text content of the last user message, or "".
func lastUserText(messages []api.Message) string {
	for i := len(messages) - 1; i >= 0; i-- {
		if messages[i].Role != api.RoleUser {
			continue
		}
		for _, b := range messages[i].Content {
			if b.Type == api.ContentTypeText && b.Text != "" {
				return b.Text
			}
		}
	}
	return ""
}
