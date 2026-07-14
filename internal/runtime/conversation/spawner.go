package conversation

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/google/uuid"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/codes"
	"go.opentelemetry.io/otel/trace"

	"github.com/qiangli/coreutils/pkg/telemetry"

	"github.com/qiangli/ycode/internal/api"
	"github.com/qiangli/ycode/internal/runtime/agentpool"
	"github.com/qiangli/ycode/internal/runtime/lanes"
	"github.com/qiangli/ycode/internal/runtime/prompt"
	"github.com/qiangli/ycode/internal/runtime/taskqueue"
	yotel "github.com/qiangli/ycode/internal/telemetry/otel"
	"github.com/qiangli/ycode/internal/tools"
	"github.com/qiangli/ycode/pkg/memex/memory"
)

// maxSubagentIterations caps the agentic loop for spawned subagents.
const maxSubagentIterations = 15

// SpawnerConfig holds the dependencies needed to spawn child agent runtimes.
type SpawnerConfig struct {
	Model            string // model ID to use for subagent calls
	Provider         api.Provider
	Registry         *tools.Registry
	PromptCtx        *prompt.ProjectContext
	Logger           *slog.Logger
	CachingSupported bool // whether the provider supports prompt caching

	// Parallel tool execution within subagents.
	ParallelEnabled bool // enable parallel tool execution (default false for backward compat)
	MaxStandard     int  // max concurrent standard tools (default 8)
	MaxLLM          int  // max concurrent LLM tools (default 2)
	MaxAgent        int  // max concurrent nested agent spawns (default 4)

	// Agent pool for progress tracking (optional).
	AgentPool *agentpool.Pool

	// Lane scheduler for concurrency control (optional).
	LaneScheduler *lanes.Scheduler

	// Memory manager for episodic memory recording (optional).
	MemoryManager *memory.Manager

	// Session ID for episodic memory context.
	SessionID string

	// OnEvent emits events for agent lifecycle progress (optional).
	// Event types: "agent.start", "agent.progress", "agent.complete".
	OnEvent func(eventType string, data map[string]any)
}

// NewAgentSpawner creates a spawner function that can be passed to
// RegisterAgentHandler. Each invocation creates a child runtime with
// mode-specific system prompt and filtered tool access, runs a bounded
// agentic loop, and returns the text result.
//
// A panic safety net wraps the spawn body: a panicking subagent
// converts to an error rather than killing the parent process; the
// panic is recorded in OTel via yotel.RecordPanic.
func NewAgentSpawner(sc *SpawnerConfig) func(ctx context.Context, manifest *tools.AgentManifest) (string, error) {
	body := func(ctx context.Context, manifest *tools.AgentManifest) (string, error) {
		// Acquire a subagent lane slot to bound concurrent subagent work.
		if sc.LaneScheduler != nil {
			release, err := sc.LaneScheduler.Acquire(ctx, lanes.LaneSubagent,
				fmt.Sprintf("agent:%s", manifest.Type))
			if err != nil {
				return "", fmt.Errorf("subagent lane: %w", err)
			}
			defer release()
		}

		mode := tools.AgentTypeToMode(manifest.Type)
		logger := sc.Logger
		if logger == nil {
			logger = slog.Default()
		}

		// Generate agent ID and register in pool if available.
		agentID := uuid.New().String()
		manifest.ID = agentID

		logger.Info("spawning subagent",
			"id", agentID,
			"type", manifest.Type,
			"mode", mode,
			"description", manifest.Description,
		)

		// Create OTEL span for the entire subagent lifecycle.
		tracer := otel.Tracer("ycode.subagent")
		ctx, span := tracer.Start(ctx, "ycode.subagent",
			trace.WithAttributes(
				attribute.String("agent.id", agentID),
				attribute.String("agent.type", string(manifest.Type)),
				attribute.String("agent.mode", string(mode)),
				attribute.String("agent.description", manifest.Description),
			),
		)
		defer func() {
			span.End()
		}()

		pool := sc.AgentPool
		if pool != nil {
			pool.Register(agentID, string(manifest.Type), manifest.Description)
			pool.SetRunning(agentID)
		}

		// Emit agent start event for TUI rendering.
		emitAgentEvent(sc, "agent.start", map[string]any{
			"agent_id":    agentID,
			"agent_type":  string(manifest.Type),
			"description": manifest.Description,
		})

		// completeAgent marks the agent as done in the pool.
		completeAgent := func(failed bool) {
			if pool != nil {
				pool.Complete(agentID, failed)
			}
		}

		// Resolve model: custom definition > manifest > spawner config.
		model := sc.Model
		if manifest.Model != "" {
			model = manifest.Model
		}

		// Build system prompt and tool access based on custom definition or mode.
		var systemPrompt string
		var filtered *tools.FilteredRegistry

		if def := manifest.CustomDef; def != nil {
			// Custom agent: use definition's instruction as system prompt.
			systemPrompt = def.Instruction
			if def.Context != "" {
				systemPrompt += "\n\n" + def.Context
			}
			if def.Model != "" {
				model = def.Model
			}
			// Custom tools override mode-based tools.
			if len(def.Tools) > 0 {
				allowed := tools.ApplyBlocklist(def.Tools, tools.DefaultSubagentBlocklist)
				filtered = tools.NewFilteredRegistry(sc.Registry, allowed)
			} else {
				allowed := tools.ApplyBlocklist(tools.AllowedToolsForMode(mode), tools.DefaultSubagentBlocklist)
				filtered = tools.NewFilteredRegistry(sc.Registry, allowed)
			}
		} else {
			// Built-in agent: use mode-based system prompt and tools.
			systemPrompt = prompt.BuildDefault(sc.PromptCtx, string(mode), sc.CachingSupported, nil)
			allowed := tools.AllowedToolsForMode(mode)
			allowed = tools.ApplyBlocklist(allowed, tools.DefaultSubagentBlocklist)
			if allowed != nil {
				filtered = tools.NewFilteredRegistry(sc.Registry, allowed)
			} else {
				// Unrestricted mode (e.g., build): create unfiltered registry, then hide blocked tools.
				filtered = tools.NewFilteredRegistry(sc.Registry, nil)
				for _, name := range tools.DefaultSubagentBlocklist {
					filtered.Hide(name)
				}
			}
		}

		// Build tool definitions from the filtered set.
		var toolDefs []api.ToolDefinition
		for _, spec := range filtered.AlwaysAvailable() {
			toolDefs = append(toolDefs, api.ToolDefinition{
				Name:        spec.Name,
				Description: spec.Description,
				InputSchema: spec.InputSchema,
			})
		}

		// Also include deferred tools that are in the allowlist.
		for _, spec := range filtered.Deferred() {
			toolDefs = append(toolDefs, api.ToolDefinition{
				Name:        spec.Name,
				Description: spec.Description,
				InputSchema: spec.InputSchema,
			})
		}

		// Prepend custom agent message if defined.
		var initialText string
		if def := manifest.CustomDef; def != nil && def.Message != "" {
			initialText = def.Message + "\n\n" + manifest.Prompt
		} else {
			initialText = manifest.Prompt
		}

		// Build initial messages with the user prompt from the manifest.
		messages := []api.Message{
			{
				Role: api.RoleUser,
				Content: []api.ContentBlock{
					{Type: api.ContentTypeText, Text: initialText},
				},
			},
		}

		// Determine max iterations: custom definition overrides default.
		maxIter := maxSubagentIterations
		if def := manifest.CustomDef; def != nil {
			maxIter = def.EffectiveMaxIter()
		}

		// Track metrics for episodic memory.
		startTime := time.Now()
		toolsUsed := make(map[string]bool)

		// What the subagent has ESTABLISHED so far, turn by turn.
		//
		// It used to be kept nowhere. When the iteration cap was hit, the loop returned
		// ("", error) — every finding from fifteen turns of real investigation thrown on
		// the floor, and the parent handed an error it could not distinguish from "my
		// delegate found nothing". See the cap handler below.
		var progress []string

		// Agentic loop: send → receive → execute tools → repeat.
		for i := 0; i < maxIter; i++ {
			req := &api.Request{
				Model:     model,
				MaxTokens: MaxOutputTokenCap,
				System:    systemPrompt,
				Messages:  messages,
				Tools:     toolDefs,
				Stream:    true,
			}

			events, errc := sc.Provider.Send(ctx, req)

			// Accumulate response.
			var textParts []string
			var toolCalls []ToolCall
			var currentBlock *api.ContentBlock

			for ev := range events {
				switch ev.Type {
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
							Text        string `json:"text"`
							PartialJSON string `json:"partial_json,omitempty"`
						}
						if err := json.Unmarshal(ev.Delta, &delta); err == nil {
							if delta.Text != "" {
								textParts = append(textParts, delta.Text)
							}
							if currentBlock != nil && currentBlock.Type == api.ContentTypeToolUse && delta.PartialJSON != "" {
								currentBlock.Input = append(currentBlock.Input, []byte(delta.PartialJSON)...)
							}
						}
					}
				case "content_block_stop":
					if currentBlock != nil && currentBlock.Type == api.ContentTypeToolUse {
						toolCalls = append(toolCalls, ToolCall{
							ID:    currentBlock.ID,
							Name:  currentBlock.Name,
							Input: currentBlock.Input,
						})
					}
					currentBlock = nil
				}
			}

			if err := <-errc; err != nil {
				completeAgent(true)
				emitAgentEvent(sc, "agent.complete", map[string]any{
					"agent_id":    agentID,
					"description": manifest.Description,
					"status":      "failed",
					"duration_ms": time.Since(startTime).Milliseconds(),
				})
				span.RecordError(err)
				span.SetStatus(codes.Error, err.Error())

				// An API error mid-flight — a rate limit that outlasted its retries, a
				// dropped connection — should not incinerate the work already done. It
				// used to: `return "", err`, and everything the subagent had established
				// went with it.
				//
				// But if it established NOTHING, the parent must hear a hard error and
				// not a soft "partial result with no findings" — otherwise a dead binding
				// or a bad key reads as "the delegate looked and found nothing", which is
				// a lie about the code rather than about the run.
				if len(progress) > 0 {
					logger.Warn("subagent hit an API error; returning the findings it already had",
						"agent_id", agentID, "turn", i+1, "error", err)

					// DO NOT PUT THE TRANSPORT ERROR IN THE MODEL'S CONTEXT.
					//
					// The first version of this appended err.Error() to the report, so a
					// rate limit landed in the parent's context as text. The parent —
					// reasonably, and this is measured, not hypothetical — tried to SOLVE
					// it: glm-5.2 read "rate limit" and issued `sleep 120`. Three of its
					// turns went to napping. It was doing the harness's job, badly, with
					// the operator's iteration budget.
					//
					// A rate limit is a fact about the PROVIDER, not about the work. It is
					// handled in the transport (see api.providerPacer) where the agent
					// cannot see it and cannot try to help. What the parent needs to know
					// is only that the delegate stopped early — which the partial report
					// already says.
					return partialSubagentReport(progress, toolsUsed, i+1) +
						"\nIt was stopped early by an infrastructure condition (already handled " +
						"by the harness — this is NOT something for you to work around).\n", nil
				}
				return "", fmt.Errorf("subagent turn %d: %w", i+1, err)
			}

			textContent := joinParts(textParts)
			if strings.TrimSpace(textContent) != "" {
				progress = append(progress, textContent)
			}

			// No tool calls — subagent is done.
			if len(toolCalls) == 0 {
				logger.Info("subagent completed", "turns", i+1)
				completeAgent(false)
				span.SetAttributes(attribute.Int("agent.turns", i+1))
				recordEpisodicMemory(sc, manifest, agentID, startTime, toolsUsed, true)
				emitAgentEvent(sc, "agent.complete", map[string]any{
					"agent_id":    agentID,
					"description": manifest.Description,
					"status":      "completed",
					"turns":       i + 1,
					"duration_ms": time.Since(startTime).Milliseconds(),
				})
				return textContent, nil
			}

			// Build assistant message with tool_use blocks.
			var assistantBlocks []api.ContentBlock
			if textContent != "" {
				assistantBlocks = append(assistantBlocks, api.ContentBlock{
					Type: api.ContentTypeText,
					Text: textContent,
				})
			}
			for _, tc := range toolCalls {
				assistantBlocks = append(assistantBlocks, api.ContentBlock{
					Type:  api.ContentTypeToolUse,
					ID:    tc.ID,
					Name:  tc.Name,
					Input: tc.Input,
				})
			}
			messages = append(messages, api.Message{
				Role:    api.RoleAssistant,
				Content: assistantBlocks,
			})

			// Execute tools through the filtered registry (parallel when enabled).
			// Track tool uses in the agent pool for progress reporting.
			if pool != nil {
				for _, tc := range toolCalls {
					pool.RecordToolUse(agentID, tc.Name)
				}
			}
			for _, tc := range toolCalls {
				toolsUsed[tc.Name] = true
			}

			// Emit progress event with current tool activity.
			toolNames := make([]string, len(toolCalls))
			for ti, tc := range toolCalls {
				toolNames[ti] = tc.Name
			}
			emitAgentEvent(sc, "agent.progress", map[string]any{
				"agent_id":    agentID,
				"description": manifest.Description,
				"tool_uses":   len(toolsUsed),
				"tools":       toolNames,
				"turn":        i + 1,
				"duration_ms": time.Since(startTime).Milliseconds(),
			})

			toolResults := executeSubagentTools(ctx, sc, filtered, toolCalls, logger)

			messages = append(messages, api.Message{
				Role:    api.RoleUser,
				Content: toolResults,
			})
		}

		// THE CAP IS NOT A FAILURE. It is an INTERRUPTION, and the difference is the
		// whole point.
		//
		// This used to `return "", err` — discarding everything the subagent had
		// established across every one of its turns, and handing the parent an error
		// indistinguishable from "my delegate found nothing".
		//
		// That is the absence-of-evidence bug living inside the delegation path, and it
		// is not theoretical: a live conductor run delegated correctly, had its
		// subagents killed at the cap, got back errors carrying ZERO information, had
		// nothing to reason from, announced a fallback strategy, and stopped. It burned
		// 25 turns and 169 tool calls and produced not one finding. The model was blamed.
		// It was our cap.
		//
		// So: return the WORK, say plainly that it is partial, and say what it means.
		// A parent that knows its delegate was cut off can re-delegate the remainder,
		// narrow the scope, or finish the job itself. A parent handed an opaque error
		// can only guess — and the guess it makes is "there was nothing there".
		completeAgent(true)
		recordEpisodicMemory(sc, manifest, agentID, startTime, toolsUsed, false)
		emitAgentEvent(sc, "agent.complete", map[string]any{
			"agent_id":    agentID,
			"description": manifest.Description,
			"status":      "truncated",
			"turns":       maxIter,
			"duration_ms": time.Since(startTime).Milliseconds(),
		})
		logger.Warn("subagent hit its iteration cap; returning partial findings",
			"agent_id", agentID, "max_iter", maxIter, "turns_of_findings", len(progress))

		// The delegate was INTERRUPTED, not finished. Record it as a bound, so a parent
		// that reasoned from a truncated report can be found later — the absence of a
		// finding here is not evidence there was nothing to find.
		telemetry.BoundHit(ctx, "iterations", int64(maxIter), int64(maxIter),
			"subagent cut off with "+strconv.Itoa(len(progress))+" turns of findings")
		span.SetAttributes(attribute.Bool("agent.truncated", true), attribute.Int("agent.turns", maxIter))
		return partialSubagentReport(progress, toolsUsed, maxIter), nil
	}

	return func(ctx context.Context, manifest *tools.AgentManifest) (out string, err error) {
		defer func() {
			if r := recover(); r != nil {
				detail := ""
				if manifest != nil {
					detail = string(manifest.Type)
				}
				err = yotel.RecordPanic(ctx, "subagent.spawn", detail, r)
			}
		}()
		return body(ctx, manifest)
	}
}

// executeSubagentTools runs tool calls for a subagent. When parallel execution
// is enabled in the SpawnerConfig, tools run concurrently within per-category
// limits (same bounded-parallelism model as the main runtime). Otherwise they
// execute sequentially for backward compatibility.
func executeSubagentTools(ctx context.Context, sc *SpawnerConfig, filtered *tools.FilteredRegistry, toolCalls []ToolCall, logger *slog.Logger) []api.ContentBlock {
	if !sc.ParallelEnabled || len(toolCalls) <= 1 {
		return executeSubagentToolsSequential(ctx, filtered, toolCalls, logger)
	}

	qCalls := make([]taskqueue.Call, len(toolCalls))
	for i, tc := range toolCalls {
		spec, _ := filtered.Get(tc.Name)
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
		c := tc
		qCalls[i] = taskqueue.Call{
			Index:    i,
			Name:     tc.Name,
			Detail:   tc.Name,
			Category: cat,
			Invoke: func(ctx context.Context) (string, error) {
				return filtered.Invoke(ctx, c.Name, c.Input)
			},
		}
	}

	exec := taskqueue.NewExecutor(sc.MaxStandard, sc.MaxLLM, sc.MaxAgent)
	taskResults := exec.Run(ctx, qCalls, nil)

	blocks := make([]api.ContentBlock, len(toolCalls))
	for i, res := range taskResults {
		block := api.ContentBlock{
			Type:      api.ContentTypeToolResult,
			ToolUseID: toolCalls[i].ID,
		}
		if res.Err != nil {
			block.Content = fmt.Sprintf("Error: %v", res.Err)
			block.IsError = true
		} else {
			block.Content = res.Output
		}
		blocks[i] = block
		logger.Info("subagent tool executed", "tool", toolCalls[i].Name, "error", res.Err != nil)
	}
	return blocks
}

// executeSubagentToolsSequential runs tool calls one at a time.
func executeSubagentToolsSequential(ctx context.Context, filtered *tools.FilteredRegistry, toolCalls []ToolCall, logger *slog.Logger) []api.ContentBlock {
	results := make([]api.ContentBlock, 0, len(toolCalls))
	for _, tc := range toolCalls {
		output, err := filtered.Invoke(ctx, tc.Name, tc.Input)
		block := api.ContentBlock{
			Type:      api.ContentTypeToolResult,
			ToolUseID: tc.ID,
		}
		if err != nil {
			block.Content = fmt.Sprintf("Error: %v", err)
			block.IsError = true
		} else {
			block.Content = output
		}
		results = append(results, block)
		logger.Info("subagent tool executed", "tool", tc.Name, "error", err != nil)
	}
	return results
}

// emitAgentEvent publishes an agent lifecycle event if the event callback is set.
func emitAgentEvent(sc *SpawnerConfig, eventType string, data map[string]any) {
	if sc.OnEvent != nil {
		sc.OnEvent(eventType, data)
	}
}

// recordEpisodicMemory saves an episodic memory for a completed subagent.
// This captures agent experiences for future recall — what was attempted,
// which tools were used, whether it succeeded, and how long it took.
func recordEpisodicMemory(sc *SpawnerConfig, manifest *tools.AgentManifest, agentID string, startTime time.Time, toolsUsed map[string]bool, success bool) {
	if sc.MemoryManager == nil {
		return
	}

	var toolNames []string
	for name := range toolsUsed {
		toolNames = append(toolNames, name)
	}

	meta := &memory.EpisodicMetadata{
		AgentType:   string(manifest.Type),
		AgentID:     agentID,
		TaskSummary: manifest.Description,
		ToolsUsed:   toolNames,
		Duration:    time.Since(startTime),
		Success:     success,
		SessionID:   sc.SessionID,
	}

	mem := memory.NewEpisodicMemory(meta)
	if err := sc.MemoryManager.Save(mem); err != nil {
		slog.Debug("episodic memory save failed", "agent", agentID, "error", err)
	}
}

// partialSubagentReport renders what a subagent established before its iteration cap
// stopped it — and, more importantly, tells the parent WHAT THE RESULT MEANS.
//
// The cap is an interruption, not a verdict. A parent that is told "your delegate ran
// out of budget, here is what it had, here is what it never reached" can re-delegate,
// narrow the scope, or finish the job. A parent handed an error concludes "nothing was
// found" — and that conclusion is drawn from an ABSENCE, which is the failure this whole
// codebase has spent a day stamping out.
//
// The warning is stated in the model's own channel — the text — because the model is the
// consumer. An error code it never sees cannot inform it.
func partialSubagentReport(progress []string, toolsUsed map[string]bool, maxIter int) string {
	var b strings.Builder

	fmt.Fprintf(&b, "[PARTIAL RESULT — this subagent was STOPPED at its %d-iteration budget. "+
		"It did NOT finish.]\n\n", maxIter)

	if len(progress) == 0 {
		b.WriteString("It produced no findings before it was stopped — it was still gathering context.\n\n")
	} else {
		b.WriteString("What it established before it was stopped:\n\n")
		for _, p := range progress {
			b.WriteString(strings.TrimSpace(p))
			b.WriteString("\n\n")
		}
	}

	if len(toolsUsed) > 0 {
		names := make([]string, 0, len(toolsUsed))
		for n := range toolsUsed {
			names = append(names, n)
		}
		sort.Strings(names)
		fmt.Fprintf(&b, "Tools it used: %s\n\n", strings.Join(names, ", "))
	}

	b.WriteString("IMPORTANT — how to read this:\n")
	b.WriteString("This is an INTERRUPTED investigation, not a completed one. The absence of a " +
		"finding here is NOT evidence that there is nothing to find; it is evidence that the " +
		"subagent ran out of budget.\n")
	b.WriteString("Do not conclude anything from what is missing. Either re-delegate the " +
		"remaining work with a NARROWER scope (so it fits the budget), or finish it yourself.\n")

	return b.String()
}
