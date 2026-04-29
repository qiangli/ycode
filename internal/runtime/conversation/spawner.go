package conversation

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"time"

	"github.com/google/uuid"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/codes"
	"go.opentelemetry.io/otel/trace"

	"github.com/qiangli/ycode/internal/api"
	"github.com/qiangli/ycode/internal/runtime/agentpool"
	"github.com/qiangli/ycode/internal/runtime/lanes"
	"github.com/qiangli/ycode/internal/runtime/memory"
	"github.com/qiangli/ycode/internal/runtime/prompt"
	"github.com/qiangli/ycode/internal/runtime/taskqueue"
	"github.com/qiangli/ycode/internal/tools"
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
}

// NewAgentSpawner creates a spawner function that can be passed to
// RegisterAgentHandler. Each invocation creates a child runtime with
// mode-specific system prompt and filtered tool access, runs a bounded
// agentic loop, and returns the text result.
func NewAgentSpawner(sc *SpawnerConfig) func(ctx context.Context, manifest *tools.AgentManifest) (string, error) {
	return func(ctx context.Context, manifest *tools.AgentManifest) (string, error) {
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
				span.RecordError(err)
				span.SetStatus(codes.Error, err.Error())
				return "", fmt.Errorf("subagent turn %d: %w", i+1, err)
			}

			textContent := joinParts(textParts)

			// No tool calls — subagent is done.
			if len(toolCalls) == 0 {
				logger.Info("subagent completed", "turns", i+1)
				completeAgent(false)
				span.SetAttributes(attribute.Int("agent.turns", i+1))
				recordEpisodicMemory(sc, manifest, agentID, startTime, toolsUsed, true)
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
			toolResults := executeSubagentTools(ctx, sc, filtered, toolCalls, logger)

			messages = append(messages, api.Message{
				Role:    api.RoleUser,
				Content: toolResults,
			})
		}

		completeAgent(true)
		recordEpisodicMemory(sc, manifest, agentID, startTime, toolsUsed, false)
		err := fmt.Errorf("subagent exceeded maximum iterations (%d)", maxSubagentIterations)
		span.RecordError(err)
		span.SetStatus(codes.Error, err.Error())
		return "", err
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
