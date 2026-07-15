package conversation

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"time"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/trace"

	"github.com/qiangli/coreutils/pkg/telemetry"

	"github.com/qiangli/ycode/internal/api"
	"github.com/qiangli/ycode/internal/runtime/config"
	"github.com/qiangli/ycode/internal/runtime/permission"
	"github.com/qiangli/ycode/internal/runtime/prompt"
	"github.com/qiangli/ycode/internal/runtime/routing"
	"github.com/qiangli/ycode/internal/runtime/session"
	"github.com/qiangli/ycode/internal/runtime/taskqueue"
	"github.com/qiangli/ycode/internal/tools"
	"github.com/qiangli/ycode/pkg/memex/memory"
	"github.com/qiangli/ycode/pkg/memex/qacache"
)

// MaxOutputTokenCap is the safety cap for output tokens per response.
// Prevents runaway responses from wasting tokens. Matches opencode's default.
const MaxOutputTokenCap = 32_000

// Runtime manages the conversation turn loop.
type Runtime struct {
	// lastContextChars is the size of the conversation as of the current turn.
	// Content routing consults it so that it only mutilates a tool result when the
	// window is ACTUALLY under pressure — see contextUnderPressure.
	lastContextChars int

	// preActivatedFor is the user message we last ran tool preactivation for.
	// The agentic loop re-enters Turn() once per LLM round-trip with the SAME user
	// message (only tool results are appended), and preactivation was re-deriving
	// the identical answer every time: measured at 41.4s of a 111.7s run — 37% of
	// wall — across 25 turns, all with msg_len=461.
	preActivatedFor string

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

	// Context budget for history caps and compaction thresholds.
	contextBudget session.ContextBudget

	// responseTokens is the max_tokens we actually SEND, and the exact amount
	// contextBudget reserves room for. One number, so the two cannot disagree.
	responseTokens int

	// What the PROVIDER said the last request cost, and how many messages were in
	// flight when it said it.
	//
	// This is held here, in memory, rather than recovered from the message list —
	// because it CANNOT be recovered from the message list. TurnWithRecovery is handed
	// []api.Message, and api.Message is {Role, Content}: it has no Usage field and never
	// did. So apiMessagesToSession could not populate ConversationMessage.Usage no
	// matter what was persisted, MeasureTokens read nil on every single turn, and the
	// context gate fell back to the 4-chars-per-token estimator — silently, forever.
	//
	// I built a mechanism to stop guessing and then fed it a type that cannot carry the
	// answer. The log said `from_provider=false` on every turn and I had to be looking
	// at it to notice. Which is the whole argument for printing the provenance of a
	// number next to the number.
	lastReportedTokens int
	lastReportedAtMsgs int

	// Activated deferred tools — tools discovered via ToolSearch that must
	// be included in subsequent API requests so the provider accepts tool_use calls.
	// Value is the turn number when the tool was last used/activated.
	activatedTools map[string]int

	// turnCount tracks the current conversation turn for tool expiration.
	turnCount int

	// Completion cache — short-TTL cache that skips the LLM entirely
	// for identical requests (retries, error recovery).
	completionCache *api.CompletionCache

	// Q→A injector — populates Diagnostics.RecentAnswer pre-LLM and
	// records the assistant's answer post-LLM. Nil-safe: when unset,
	// the runtime never touches the cache.
	qaInjector *qacache.Injector

	// Memory manager — when set, compaction intent-summaries are
	// promoted to TypeEpisodic memories so post-session recall finds
	// them via the time-bucket index. Nil-safe.
	memoryManager *memory.Manager

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

	// lastContextHealth caches the most recent context health check result
	// from TurnWithRecovery, so collectDiagnostics can inject it into the prompt.
	lastContextHealth *session.ContextHealth

	// Optional inference router for multi-factor model selection.
	// Used by Tier 2 tool pre-activation (LLM classification).
	inferenceRouter *routing.Router

	// Optional smart tool router for semantic pre-activation (Tier 1c).
	smartRouter *tools.SmartRouter

	// Optional co-occurrence tracker for cluster-based tool expansion.
	coOccurrence *tools.CoOccurrence

	// Prompt cache tracking — detects cache hits/misses/breaks for observability.
	promptCache *api.PromptCache

	// Optional OTEL instrumentation.
	otel *OTELConfig

	// Optional streaming event callback. Called for each text/thinking delta
	// and tool call event as they arrive from the LLM provider.
	onEvent func(eventType string, data map[string]any)

	// Persona — resolved user model for tailored responses.
	// Updated per-turn with behavioral signals from the observer.
	currentPersona *memory.Persona

	// modelOverride takes precedence over r.config.Model when non-empty.
	// Set by SetModelOverride for per-turn / per-session model selection
	// without mutating the shared config (which is per-workDir, so a
	// mutation would race across concurrent multi-tenant sessions).
	modelOverride string
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
	// Two halves of one rule, and they have to agree: ask for a reply that FITS in
	// this model's window (MaxResponseTokens), and then RESERVE room for exactly that
	// reply (WithResponseReserve). Reserving for a reply too big to fit is impossible;
	// asking for one and not reserving for it overflows the window.
	ceiling := cfg.MaxTokens
	if ceiling <= 0 || ceiling > MaxOutputTokenCap {
		ceiling = MaxOutputTokenCap
	}

	// The window comes from a hardcoded table, which goes stale every time a provider
	// ships a model. cfg.ContextWindow is the escape hatch — one number, no rebuild.
	window := caps.MaxContextTokens
	if cfg.ContextWindow > 0 {
		window = cfg.ContextWindow
	}

	baseBudget := session.ContextBudgetForProvider(window, cachingSupported)
	responseTokens := baseBudget.MaxResponseTokens(ceiling)
	contextBudget := baseBudget.WithResponseReserve(responseTokens)
	if cfg.ContextReserved > 0 {
		contextBudget = contextBudget.WithReserved(cfg.ContextReserved)
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
		responseTokens:   responseTokens,
		activatedTools:   make(map[string]int),
		completionCache:  api.NewCompletionCache(completionCacheDir, api.CompletionCacheTTL),
		promptCache:      api.NewPromptCache(),
		jitDiscovery:     jit,
		topicTracker:     prompt.NewTopicTracker(),
	}

	// Wire file access hook so tools trigger JIT discovery.
	registry.SetFileAccessHook(func(path string) {
		jit.OnToolAccess(path)
	})

	// A RESUMED session already knows what it cost — recover it, or the first request
	// after a resume is unmeasured and a large history sails into the window.
	if sess != nil {
		r.seedReportedTokens(sess.Messages)
	}

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

// SetInferenceRouter attaches an inference router for multi-factor model selection.
// When set, Tier 2 tool pre-activation uses the router to classify user messages
// via a lightweight LLM call (local Ollama or cheap remote model).
func (r *Runtime) SetInferenceRouter(router *routing.Router) {
	r.inferenceRouter = router
}

// SetSmartRouter sets the semantic tool router for Tier 1c pre-activation.
func (r *Runtime) SetSmartRouter(sr *tools.SmartRouter) {
	r.smartRouter = sr
}

// SetCoOccurrence sets the co-occurrence tracker for cluster-based tool expansion.
func (r *Runtime) SetCoOccurrence(co *tools.CoOccurrence) {
	r.coOccurrence = co
}

// SetPersona sets the resolved persona for tailored responses.
// When set, persona signals are collected per-turn and the persona
// context is injected into the system prompt.
// SetQAInjector attaches a Q→A cache injector. Lookup runs pre-LLM and
// surfaces a hit through Diagnostics.RecentAnswer; Record runs post-LLM
// to store the assistant's response. Nil disables the path.
func (r *Runtime) SetQAInjector(i *qacache.Injector) {
	r.qaInjector = i
}

// SetMemoryManager wires the memex memory manager. When set, compaction
// intent-summaries are promoted to TypeEpisodic memories so post-
// session queries like "what did we do this week" can hit the memex
// time-bucket index without re-deriving from raw sources.
func (r *Runtime) SetMemoryManager(mgr *memory.Manager) {
	r.memoryManager = mgr
}

func (r *Runtime) SetPersona(p *memory.Persona) {
	r.currentPersona = p
	if r.promptCtx != nil {
		r.promptCtx.Persona = p
	}
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

// RestoreSessionDiagnostics loads the latest ghost snapshot and injects
// a prior-session summary into the prompt context. Called once on session
// resume so the agent has warm-start context about what happened previously.
func (r *Runtime) RestoreSessionDiagnostics() {
	if r.session == nil {
		return
	}
	ghost, err := session.LoadLatestGhost(r.session.Dir)
	if err != nil {
		r.logger.Warn("failed to load ghost for session diagnostics", "error", err)
		return
	}
	if ghost == nil || ghost.Summary == "" {
		return
	}

	// Build a compact summary from the ghost snapshot.
	summary := ghost.Summary
	if len(summary) > 300 {
		summary = summary[:300] + "..."
	}

	if r.promptCtx.Diagnostics == nil {
		r.promptCtx.Diagnostics = &prompt.DiagnosticsInfo{}
	}
	r.promptCtx.Diagnostics.PriorSessionSummary = summary
	r.logger.Info("restored session diagnostics from ghost", "summary_len", len(summary))
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

// SetModelOverride installs a per-Runtime model override. When set, the
// next Turn() uses this model instead of r.config.Model. Used by
// per-session model selection (G-G) so multi-tenant deployments can route
// different sessions to different models without mutating the per-workDir
// shared config.
//
// Pass "" to clear the override and revert to r.config.Model.
func (r *Runtime) SetModelOverride(model string) {
	r.modelOverride = model
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
// tool is expired if not used. Set to 8 to reduce re-discovery overhead in
// multi-phase tasks (build→test→fix cycles) while still expiring unused tools.
const activatedToolTTL = 8

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

	// Persona: observe behavioral signals from the user's message.
	if r.currentPersona != nil && r.currentPersona.SessionContext != nil {
		if userMsg := lastUserText(messages); userMsg != "" {
			sig := memory.ObserveTurn(userMsg, nil, r.turnCount)
			r.currentPersona.SessionContext.Update(sig)
		}
	}

	// Collect runtime diagnostics for the system prompt.
	r.collectDiagnostics()

	// Clear prior session summary after the first turn — it's a one-time warm-start signal.
	if r.turnCount > 1 && r.promptCtx.Diagnostics != nil {
		r.promptCtx.Diagnostics.PriorSessionSummary = ""
	}

	// Q→A cache lookup: if the current user message matches a recent
	// question, surface the cached answer as a one-shot diagnostics
	// block. The LLM still runs and decides whether to reuse or refine.
	if r.qaInjector != nil {
		if userMsg := lastUserText(messages); userMsg != "" {
			if block := r.qaInjector.Lookup(userMsg, time.Now()); block != "" {
				if r.promptCtx.Diagnostics == nil {
					r.promptCtx.Diagnostics = &prompt.DiagnosticsInfo{}
				}
				r.promptCtx.Diagnostics.RecentAnswer = block
			}
		}
	}

	// Build system prompt. Wrapped in a span so the assembly phase
	// (which can be expensive — discovers AGENTS.md files, gathers env
	// snapshot, formats memories) is visible in trace timelines.
	promptCtx, promptSpan := otel.Tracer("ycode.conversation").Start(ctx, "prompt.build",
		trace.WithAttributes(
			attribute.String("mode", r.Mode()),
			attribute.Bool("caching_supported", r.cachingSupported),
		))
	systemPrompt := prompt.BuildDefault(r.promptCtx, r.Mode(), r.cachingSupported, r.contextBaseline)
	promptSpan.SetAttributes(attribute.Int("system_prompt.size", len(systemPrompt)))
	promptSpan.End()
	_ = promptCtx

	// (r.lastContextChars is measured below, once the FULL request exists — it has to
	// count the system prompt and the tool schemas too, and those are built further
	// down. See the comment at the assignment.)

	// Pre-activate deferred tools based on intent signals in the user message.
	// Tier 1a: high-precision keywords. Tier 1b: SearchTools() scoring.
	// This eliminates the 2-turn ToolSearch overhead for common operations.
	//
	// INSTRUMENTED (temporary): an L4 review named this as the suspected per-turn
	// cost — on a continuation turn the user text is UNCHANGED (only tool results
	// were appended), the cheap tiers find everything already active, total==0, and
	// that fires the Tier-2 LLM classifier (preactivate.go:137) plus a semantic
	// vector query. Both have timeouts (3s and 2s) but NOBODY HAS MEASURED THE
	// ACTUAL DURATION. A timeout is a ceiling, not a number. Measure it.
	if userMsg := lastUserText(messages); userMsg != "" {
		preStart := time.Now()
		r.preActivateTools(userMsg)
		if os.Getenv("YCODE_PERF") != "" {
			fmt.Fprintf(os.Stderr, "YCODE_PERF preactivate=%.2fs msg_len=%d\n",
				time.Since(preStart).Seconds(), len(userMsg))
		}
	}

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
	// Ask for a reply that FITS. r.responseTokens is the model-aware cap the budget
	// was built around — using MaxOutputTokenCap here instead would ask for a reply
	// bigger than the room we reserved for it, which is how a "reserve" becomes a
	// decoration.
	maxTokens := r.responseTokens
	if maxTokens <= 0 {
		maxTokens = MaxOutputTokenCap
	}

	// Build API request.
	model := r.config.Model
	if r.modelOverride != "" {
		model = r.modelOverride
	}
	req := &api.Request{
		Model:     model,
		MaxTokens: maxTokens,
		System:    systemPrompt,
		Messages:  messages,
		Tools:     toolDefs,
		Stream:    true,
	}

	// How full is the window, really? contextUnderPressure and distillResults read
	// this to decide whether damaging a tool result buys anything.
	//
	// Measure the REQUEST, not the message list. The system prompt and the tool
	// schemas are not free — they ride in every single request and they occupy the
	// same window the messages do. The old version counted only messages, which
	// understates pressure by a near-constant amount: largest, proportionally,
	// exactly when the conversation is smallest.
	r.lastContextChars = requestChars(req)

	// Track prompt cache fingerprint for hit/miss/break detection.
	fp := api.Fingerprint(req)
	if r.promptCache.Check(fp) {
		r.logger.Debug("prompt cache: hit (static parts unchanged)")
	}

	// Check completion cache — skip the LLM entirely for identical requests.
	reqHash := api.RequestHash(req)
	if cached := r.completionCache.Lookup(reqHash); cached != nil {
		result := r.responseToTurnResult(cached)
		result.Duration = 0 // Instant — from cache.
		return result, nil
	}

	// Emit the full outbound request as a debug snapshot. The chat UI
	// renders this as a collapsible "raw request" panel so users can see
	// every byte exchanged with the LLM.
	r.emitEvent("llm.request", map[string]any{
		"model":            req.Model,
		"max_tokens":       req.MaxTokens,
		"system":           req.System,
		"messages":         req.Messages,
		"tools":            req.Tools,
		"stream":           req.Stream,
		"temperature":      req.Temperature,
		"top_p":            req.TopP,
		"reasoning_effort": req.ReasoningEffort,
	})

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
						"id":    tc.ID,
						"tool":  tc.Name,
						"input": tc.Input,
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

	// Emit the assembled response as a debug snapshot — paired with the
	// llm.request emitted above. The UI uses this to render the "raw
	// response" panel under each turn.
	r.emitEvent("llm.response", map[string]any{
		"model":       req.Model,
		"text":        result.TextContent,
		"thinking":    result.ThinkingContent,
		"tool_calls":  result.ToolCalls,
		"stop_reason": result.StopReason,
		"usage":       result.Usage,
		"duration_ms": result.Duration.Milliseconds(),
	})

	// Cache the completed response for short-TTL reuse.
	r.completionCache.Store(reqHash, &api.Response{
		Content:    r.turnResultToContentBlocks(result),
		StopReason: result.StopReason,
		Usage:      result.Usage,
	})

	// Update prompt cache fingerprint and detect breaks.
	r.promptCache.Update(fp)
	if r.promptCache.DetectBreak(result.Usage.CacheReadInput) {
		r.logger.Warn("prompt cache: unexpected break detected",
			"cache_read_tokens", result.Usage.CacheReadInput,
			"breaks", r.promptCache.Breaks,
		)
	}

	// Update cache warmer context so keep-alive pings use current system prompt.
	if r.cacheWarmer != nil {
		r.cacheWarmer.UpdateContext(req.Model, req.System, req.Tools)
		r.cacheWarmer.Start() // no-op if already running
	}

	// Q→A cache record: store the assistant's text answer so a repeat
	// of this question hits the cache. Skipped when the response is
	// tool-call-only (no user-facing text) or when there's no question.
	if r.qaInjector != nil && result.TextContent != "" {
		if userMsg := lastUserText(messages); userMsg != "" {
			r.qaInjector.Record(userMsg, result.TextContent, nil, nil, time.Now())
		}
	}

	// Clear the one-shot recent-answer hint so it doesn't leak into the
	// next turn (where lookup will recompute it from the new question).
	if r.promptCtx.Diagnostics != nil {
		r.promptCtx.Diagnostics.RecentAnswer = ""
	}

	// Remember what the provider said this request cost. This is the number the whole
	// context layer runs on — see MeasureTokens.
	//
	// Anthropic reports input_tokens and cache_read_input_tokens separately, so summing
	// is correct; the OpenAI-compatible path leaves the cache fields at zero, so it
	// cannot double-count either.
	if total := result.Usage.InputTokens + result.Usage.OutputTokens +
		result.Usage.CacheReadInput + result.Usage.CacheCreationInput; total > 0 {
		r.lastReportedTokens = total
		// NOTE: the tail-estimate baseline (lastReportedAtMsgs) is anchored in
		// TurnWithRecovery against the DURABLE message list, NOT here against `messages`.
		// `messages` is the list we SENT, which compaction may have shrunk; measureTokens
		// reads the durable history, so an index taken from the compacted send-list drifts
		// and estimates a phantom tail. See TurnWithRecovery + measureTokens.
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

	// Record batch results on parent span if OTEL is configured.
	if r.otel != nil && r.otel.Tracer != nil {
		succeeded, failed, totalSize := 0, 0, 0
		for _, res := range results {
			if res.IsError {
				failed++
			} else {
				succeeded++
			}
			totalSize += len(res.Content)
		}
		span := trace.SpanFromContext(ctx)
		span.SetAttributes(
			attribute.Int("tools.succeeded", succeeded),
			attribute.Int("tools.failed", failed),
			attribute.Int("tools.output_size", totalSize),
		)
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

	return r.distillResults(ctx, calls, results)
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
			// Name is what every downstream policy keys on — ExemptFromMasking, the
			// routing exemptions, the pressure logic. It was never set here, so
			// ExemptFromMasking[block.Name] was ALWAYS false and the exemption list
			// protected nothing. A safety list nobody can match is not a safety list.
			Name: call.Name,
		}
		if err != nil {
			block.Content = fmt.Sprintf("Error: %v", err)
			block.IsError = true
			if progress != nil {
				progress <- taskqueue.TaskEvent{Index: i, Name: call.Name, Status: taskqueue.StatusFailed, Total: n}
			}
			r.recordError(ctx, "tool", "execution_failure", call.Name, err)
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
			Name:      calls[i].Name, // see the note at the serial path
		}
		if res.Err != nil {
			block.Content = fmt.Sprintf("Error: %v", res.Err)
			block.IsError = true
			r.recordError(ctx, "tool", "execution_failure", calls[i].Name, res.Err)
		} else {
			block.Content = res.Output
		}
		blocks[i] = block
		r.logger.Info("tool executed", "tool", calls[i].Name, "error", res.Err != nil)
	}
	return blocks
}

// distillResults keeps its name and no longer distills anything. See below.
func (r *Runtime) distillResults(ctx context.Context, calls []ToolCall, results []api.ContentBlock) []api.ContentBlock {
	// VERBATIM. The model sees exactly what the tool returned.
	//
	// This function used to run a two-stage cascade over every tool result:
	//
	//   session.RouteContent  -> classify, and for a read_file over 2000 chars,
	//                            keep the head, keep the tail, DELETE THE MIDDLE
	//   session.DistillToolOutput -> head/tail again at 1000 chars for a non-caching
	//                            provider
	//
	// Measured on the wire: the model asked to read the 2.4KB test file it had been
	// told to implement against, and got it back with the TEST CASES cut out of the
	// middle. It then spent SEVENTEEN turns on cat, sed ranges, python, awk, base64
	// and finally xxd, trying to rebuild the specification we had deleted. It was not
	// confused; it was doing what anyone does when handed a document with the middle
	// torn out.
	//
	// Both stages have been deleted. Not gated -- deleted. A tool result is an
	// OBSERVATION, and the answer to "how big is this string" is never a reason to
	// edit what the agent saw.
	//
	// The one limit that survives is an absolute safety cap: a core dump or a 200MB
	// log must not be inlined whatever the pressure. It fires at 256KB, on output no
	// human would paste either, and it TELLS the model -- naming the size and how to
	// read the middle. A cut the model knows about costs one follow-up call. A cut it
	// does not know about cost us seventeen.
	//
	// Pressure is now handled where pressure belongs: once, before the request, in
	// TurnWithRecovery, against the token count the PROVIDER reported.
	for i := range results {
		if results[i].Type != api.ContentTypeToolResult {
			continue
		}
		if before := len(results[i].Content); before > session.AbsoluteToolOutputCap {
			tool := toolNameFor(calls, results[i].ToolUseID)
			results[i].Content = session.CapToolOutput(results[i].Content)
			r.logger.Warn("tool result exceeded the absolute safety cap",
				"tool", tool,
				"bytes", before,
				"cap", session.AbsoluteToolOutputCap,
			)

			// The only unconditional cut left in the system. It fires on output no human
			// would paste either — but every OTHER cut we made here was invisible, and
			// each one cost hours. This one says so.
			telemetry.BoundHit(ctx, "bytes", int64(session.AbsoluteToolOutputCap), int64(before),
				"tool result truncated: "+tool)
		}
	}
	return results
}

// toolNameFor resolves a tool result back to the call that produced it.
func toolNameFor(calls []ToolCall, toolUseID string) string {
	for _, c := range calls {
		if c.ID == toolUseID {
			return c.Name
		}
	}
	return ""
}

// collectDiagnostics gathers runtime diagnostics (degraded tools, context health)
// and attaches them to the prompt context for system prompt injection.
func (r *Runtime) collectDiagnostics() {
	info := &prompt.DiagnosticsInfo{}
	hasContent := false

	// Degraded tools from QualityMonitor.
	if qm := r.registry.QualityMonitor(); qm != nil {
		for _, d := range qm.DegradedTools() {
			info.DegradedTools = append(info.DegradedTools, prompt.DegradedTool{
				Name:         d.Name,
				SuccessRate:  d.SuccessRate,
				TotalCalls:   d.TotalCalls,
				FailureCount: d.FailureCount,
			})
			hasContent = true
		}
	}

	// Context health — populated by TurnWithRecovery before Turn is called.
	if r.lastContextHealth != nil {
		info.ContextHealthPct = int(r.lastContextHealth.Ratio * 100)
		info.ContextHealthLevel = r.lastContextHealth.Level.String()
		if info.ContextHealthPct >= 60 {
			hasContent = true
		}
	}

	// Preserve prior session summary if it was set externally (e.g., by RestoreSessionDiagnostics).
	if r.promptCtx.Diagnostics != nil && r.promptCtx.Diagnostics.PriorSessionSummary != "" {
		info.PriorSessionSummary = r.promptCtx.Diagnostics.PriorSessionSummary
		hasContent = true
	}

	if hasContent {
		r.promptCtx.Diagnostics = info
	} else {
		r.promptCtx.Diagnostics = nil
	}
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
	TokensSaved     int  // Approximate tokens reclaimed by Layer 1 pruning (before - after)
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

	// ONE measurement, from the PROVIDER, not from a guess.
	//
	// Everything below used to be decided by estimateAllTokens() — 4 chars ≈ 1 token —
	// compared against absolute constants unrelated to the model. That estimate was too
	// crude to act on once, so the code acted on it FIVE times, gently: mask, soft-trim,
	// hard-clear, route, distill. Five gentle wrong answers, and on a small model all of
	// their thresholds sat OUTSIDE the window, so none of them ever fired and the log
	// said "healthy" right up to the API rejecting the request.
	//
	// MeasureTokens asks the provider. Its count covers the whole request — system
	// prompt, tool schemas, every prior message — and only the tool results appended
	// since the last response have to be estimated. The estimator survives, confined to
	// the tail of one turn, sitting on top of a number that is exact.
	//
	// It also makes a RESUMED session safe: usage is persisted per message, so a
	// conversation loaded from disk arrives with its size already known. Without that,
	// the first request after a resume has no count, "nothing to compact" is the answer,
	// and the request blows the window — a success state reached by the ABSENCE of a
	// measurement.
	used := r.measureTokens(sessionMsgs)
	r.logger.Info("context",
		"reported_tokens", used.Reported,
		"unreported_estimate", used.Unreported,
		"total", used.Total(),
		"window", r.contextBudget.ContextWindow,
		"compact_at", r.contextBudget.CompactionThreshold,
		"from_provider", used.HasReport,
	)

	// THE PROVENANCE OF THE NUMBER, RECORDED NEXT TO THE NUMBER.
	//
	// A token count of 6482 tells you nothing. 6482 that the PROVIDER REPORTED tells you
	// the context gate is running on fact; 6482 that we ESTIMATED tells you it is running
	// on a guess — and every context bug in this codebase was a decision made on a guess
	// that nobody could see was a guess.
	//
	// `from_provider=false` on the log line above is the ONLY reason the dead
	// usage-plumbing bug was ever caught: MeasureTokens was reading ConversationMessage
	// .Usage out of []api.Message, a type with no Usage field, so it returned nil on every
	// single turn and the whole "ask the provider, do not guess" mechanism fell back to
	// the estimator it exists to replace. Silently. For every model.
	//
	// As a log line that took a human staring at stderr. As a span attribute it is a
	// QUERY: "show me every turn where the context gate ran on an estimate."
	source := "estimate"
	if used.HasReport {
		source = "provider"
	}
	telemetry.Provenance(ctx, "context.tokens", int64(used.Total()), source)
	telemetry.Provenance(ctx, "context.window", int64(r.contextBudget.ContextWindow), "capabilities-table")
	r.lastContextHealth = &session.ContextHealth{
		EstimatedTokens: used.Total(),
		Threshold:       r.contextBudget.CompactionThreshold,
	}

	// Merge adjacent user messages to reduce per-message structural overhead.
	// This is especially valuable for non-caching providers where system reminders
	// and dynamic injections are stored as separate user messages.
	sessionMsgs = session.MergeAdjacentUserMessages(sessionMsgs)
	messages = r.sessionMessagesToAPI(sessionMsgs)

	// --- The cheap answer to real pressure: drop STALE tool observations ---
	//
	// This is the one trimming layer that survives, and it survives because compaction
	// is an LLM CALL. If the only response to pressure is to summarize, then every
	// pressure event on a long non-caching session — deepseek, kimi, the whole API-key
	// lane — costs a full summarization round-trip. Dropping an old, superseded tool
	// result costs nothing. (This is codex's objection to deleting it, and it is right.)
	//
	// What makes it safe is that it only ever touches OLD observations, it only runs
	// when the window is ACTUALLY filling up (against the provider's count, not a
	// guess), and it names the tool and says the result can be re-run.
	//
	// Observation masking used to run here too, unconditionally, doing the same job with
	// a different placeholder — and its exemption list keyed on a field production never
	// populated, so it protected nothing. Two mechanisms, one job, one of them blind.
	// Deleted.
	if r.contextBudget.NeedsTrim(used) && !r.contextBudget.NeedsCompaction(used) {
		pruned, pruneResult := session.PruneMessages(sessionMsgs, r.contextBudget)
		if pruneResult != nil {
			r.logger.Info("trimmed stale tool observations",
				"soft_trimmed", pruneResult.SoftTrimmed,
				"hard_cleared", pruneResult.HardCleared,
				"tokens_before", pruneResult.TokensBefore,
				"tokens_after", pruneResult.TokensAfter,
			)
			messages = r.sessionMessagesToAPI(pruned)
			recovery.Pruned = true
			recovery.PrunedCount = pruneResult.SoftTrimmed + pruneResult.HardCleared
			if saved := pruneResult.TokensBefore - pruneResult.TokensAfter; saved > 0 {
				recovery.TokensSaved = saved
			}
		}
	}

	// --- Compaction: the honest, expensive answer when trimming is not enough ---
	if r.contextBudget.NeedsCompaction(used) {
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
		// Anchor the tail-estimate baseline to the DURABLE list.
		//
		// Turn just recorded the provider's token count (r.lastReportedTokens), but the
		// baseline INDEX must live here, because only here do we hold the durable history
		// (sessionMsgs). Turn holds `messages` — the list it SENT, which compaction may
		// have shrunk to a fraction of the durable size. Recording the index from that
		// short list and then reading it against the long durable list in measureTokens
		// makes the two diverge: msgs[shortIndex:longLen] is a phantom tail.
		//
		// Observed, live: GLM Reported=16k, tail ESTIMATED at 230k, total 246k tripping a
		// 56k threshold EVERY turn — so compaction fired 119 times, discarding the files
		// the agent had just read, and the agent re-read them and never converged (40 min,
		// 0 files written).
		//
		// The report accounts for the whole request through this response; the only
		// genuinely-unaccounted tokens are what gets appended AFTER it (next turn's tool
		// results). +1 skips the assistant message, whose output tokens are already in
		// the report.
		r.lastReportedAtMsgs = len(sessionMsgs) + 1
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

	// Promote the intent summary into the memex as an episodic memory so
	// post-session "what did we do this week" queries hit the time
	// bucket without re-deriving from raw sources. Best-effort; a save
	// failure logs and continues — compaction itself must not fail.
	r.promoteCompactionToMemex(compactResult)

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
// promoteCompactionToMemex saves the intent summary from a compaction
// pass as a TypeEpisodic memory with a 14-day TTL and a session-window
// validity. The memory's name is keyed by session ID so repeated
// compactions in the same session overwrite rather than duplicate.
//
// Best-effort: failures are logged but never block compaction. When no
// memory manager is wired, returns without doing work.
func (r *Runtime) promoteCompactionToMemex(result *session.CompactionResult) {
	if r.memoryManager == nil || result == nil || result.Summary == "" {
		return
	}
	now := time.Now().UTC()
	validUntil := now.AddDate(0, 0, 14)
	sessionID := ""
	if r.session != nil {
		sessionID = r.session.ID
	}
	desc := compactionDescription(result)
	content := result.Summary
	// Bake the project's recent-commit list into the episodic content so
	// "what did we do this week" returns commits, not just a summary.
	// promptCtx.RecentCommits is pre-rendered as a list of subject lines.
	if r.promptCtx != nil && len(r.promptCtx.RecentCommits) > 0 {
		content = appendGitActivity(content, r.promptCtx.RecentCommits)
	}
	mem := &memory.Memory{
		Name:        compactionMemoryName(sessionID, now),
		Description: desc,
		Type:        memory.TypeEpisodic,
		Scope:       memory.ScopeProject,
		Content:     content,
		Importance:  0.6,
		TTLMinutes:  14 * 24 * 60,
		ValidUntil:  &validUntil,
		CreatedAt:   now,
		UpdatedAt:   now,
	}
	if err := r.memoryManager.Save(mem); err != nil {
		r.logger.Warn("compaction promote: save failed", "name", mem.Name, "error", err)
	}
}

// compactionMemoryName returns a stable name per session+compaction. The
// minute-granular suffix keeps repeated compactions within the same
// minute on the same session from churning; subsequent compactions an
// hour later produce a distinct memory.
func compactionMemoryName(sessionID string, t time.Time) string {
	if sessionID == "" {
		sessionID = "anon"
	}
	return fmt.Sprintf("compaction-%s-%s", sessionID, t.Format("20060102T1504"))
}

// compactionDescription returns a short label for the index entry. The
// counts and length are stable across runs and make it easy to scan a
// list of compactions in /memex list.
func compactionDescription(result *session.CompactionResult) string {
	return fmt.Sprintf("Session compaction: %d msgs → %d-char summary",
		result.CompactedCount, len(result.Summary))
}

// appendGitActivity appends a "Git activity" section listing the
// commit subjects active during this session. Each line is rendered as
// a markdown bullet. Empty commit lists yield the original content
// unchanged.
func appendGitActivity(content string, commits []string) string {
	if len(commits) == 0 {
		return content
	}
	var b strings.Builder
	b.WriteString(content)
	if !strings.HasSuffix(content, "\n") {
		b.WriteString("\n")
	}
	b.WriteString("\n## Git activity\n")
	for _, c := range commits {
		c = strings.TrimSpace(c)
		if c == "" {
			continue
		}
		fmt.Fprintf(&b, "- %s\n", c)
	}
	return b.String()
}

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

// contextUnderPressure reports whether the conversation is large enough that
// throwing away part of a tool result is a trade worth making.
//
// It exists because content routing was making that trade UNCONDITIONALLY. On turn
// 1 of an empty conversation — 64K-token window, ~600 tokens used — a read_file
// result over 2000 characters had its MIDDLE DELETED (session/routing.go:84-93).
//
// The model asked for the test file it had been told to implement against, and got
// it back with the test cases cut out. It then spent seventeen turns trying to
// recover them with sed, base64 and xxd. It was not confused; it was doing what
// anyone would do when handed a document with the middle torn out.
//
// Context management is a response to a context PROBLEM. Below the soft threshold
// there is no problem, and saving 800 characters costs the agent its ability to
// read.
// requestChars measures everything in the request that occupies the model's context
// window: the system prompt, the tool schemas, and the messages.
//
// The tool schemas matter more than they look. A counting proxy on the wire measured
// ycode sending ~6.2KB of them on EVERY request — small next to a full conversation,
// large next to an empty one, and present in both.
func requestChars(req *api.Request) int {
	n := len(req.System)
	for _, t := range req.Tools {
		n += len(t.Name) + len(t.Description) + len(t.InputSchema)
	}
	for _, m := range req.Messages {
		for _, b := range m.Content {
			n += len(b.Text) + len(b.Content)
		}
	}
	return n
}

// The budget is the MODEL'S. The first version of this function was gated on the
// package-level session.CompactionThreshold — a flat 100_000 — which on a 64K model
// puts the soft-trim line at 60K tokens against a usable window of 48K. It could
// never fire. The gate would have said "no pressure" right up to the API error.
//
// It gave the right answer on the benchmark for the wrong reason: those runs were
// short, so "never under pressure" and "not yet under pressure" look identical. A
// long conversation would have sailed past the window instead of compacting.
func (r *Runtime) contextUnderPressure() bool {
	// ~4 chars per token is the usual rule of thumb; the exactness does not matter,
	// only the order of magnitude — we are distinguishing "the window is basically
	// empty" from "the window is filling up".
	const charsPerToken = 4

	budget := r.contextBudget
	if budget.CompactionThreshold <= 0 {
		// An UNSET budget is not a full window. Without this, SoftTrimAt() is 0, the
		// comparison is `0 >= 0`, and an EMPTY conversation reports pressure — so a
		// missing budget would silently turn content damage back on for everything.
		//
		// Damaging the model's observations is the aggressive act. Do not reach it by
		// the ABSENCE of a number.
		budget = session.DefaultContextBudget()
	}
	return r.lastContextChars/charsPerToken >= budget.SoftTrimAt()
}

// measureTokens reports how full the window is, using what the PROVIDER said the last
// request cost plus an estimate of only what has been appended since.
//
// The count is held on the Runtime, not read back out of the message list, because it
// CANNOT be read out of the message list: TurnWithRecovery is handed []api.Message, and
// api.Message is {Role, Content}. It has no Usage field and never had one. So
// session.MeasureTokens read nil on every turn, `from_provider=false` on every line of
// the log, and the gate quietly fell back to the 4-chars-per-token estimator it was
// built to replace.
//
// I built the mechanism to stop guessing and then fed it a type that cannot carry the
// answer. It took a log line that PRINTS THE PROVENANCE of the number — from_provider —
// to see it. A number without its provenance would have looked perfectly healthy.
func (r *Runtime) measureTokens(msgs []session.ConversationMessage) session.TokensUsed {
	if r.lastReportedTokens <= 0 {
		// No response has come back yet. Estimate the lot — it is all we have, and a
		// conversation with no assistant turn in it is small. (A RESUMED session is
		// seeded from its persisted usage at construction; see seedReportedTokens.)
		return session.MeasureTokens(msgs)
	}

	// Everything up to the message count that produced the last report is exactly
	// accounted for by that report. Estimate only the tail appended since.
	unreported := 0
	if r.lastReportedAtMsgs < len(msgs) {
		for _, m := range msgs[r.lastReportedAtMsgs:] {
			unreported += session.EstimateMessageTokens(m)
		}
	}
	return session.TokensUsed{
		Reported:   r.lastReportedTokens,
		Unreported: unreported,
		HasReport:  true,
	}
}

// seedReportedTokens recovers the provider's count for a RESUMED conversation.
//
// Usage IS persisted per message (ConversationMessage.Usage), so a session loaded from
// disk still knows what it cost. Without this, the first request after a resume would
// have no count, "nothing to compact" would be the answer, and a 95k-token history
// would sail straight into the window — a success state reached by the ABSENCE of a
// measurement.
func (r *Runtime) seedReportedTokens(msgs []session.ConversationMessage) {
	used := session.MeasureTokens(msgs)
	if used.HasReport {
		r.lastReportedTokens = used.Reported
		r.lastReportedAtMsgs = len(msgs)
	}
}
