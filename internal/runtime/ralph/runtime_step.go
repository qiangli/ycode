package ralph

import (
	"context"
	"fmt"
	"log/slog"
	"path/filepath"
	"strings"

	"github.com/qiangli/ycode/internal/api"
	"github.com/qiangli/ycode/internal/runtime/config"
	"github.com/qiangli/ycode/internal/runtime/conversation"
	"github.com/qiangli/ycode/internal/runtime/memory"
	"github.com/qiangli/ycode/internal/runtime/prompt"
	"github.com/qiangli/ycode/internal/runtime/session"
	"github.com/qiangli/ycode/internal/tools"
)

// maxAgenticIterations is the maximum number of tool-use round-trips per turn.
const maxAgenticIterations = 25

// RuntimeDeps holds the shared dependencies for creating per-iteration runtimes.
// These are constructed once and reused across all Ralph iterations.
type RuntimeDeps struct {
	Config        *config.Config
	Provider      api.Provider
	Registry      *tools.Registry
	PromptCtx     *prompt.ProjectContext
	MemoryManager *memory.Manager
	SessionDir    string
	Logger        *slog.Logger
}

// RuntimeStepConfig configures the runtime-backed step function.
type RuntimeStepConfig struct {
	// Deps holds shared runtime dependencies.
	Deps *RuntimeDeps

	// UserPrompt is the high-level task description.
	UserPrompt string

	// ProgressLog provides learnings from previous iterations (optional).
	ProgressLog *ProgressLog

	// StoryProvider returns the current story for an iteration (optional).
	// Called at the start of each iteration.
	StoryProvider func() *Story
}

// NewRuntimeStepFunc creates a StepFunc backed by the full conversation runtime.
// Each invocation creates a fresh session and runtime, builds messages from state,
// and runs the full agentic loop (turn → execute tools → repeat until done).
func NewRuntimeStepFunc(sc *RuntimeStepConfig) StepFunc {
	logger := sc.Deps.Logger
	if logger == nil {
		logger = slog.Default()
	}

	return func(ctx context.Context, state *State, iteration int) (string, float64, error) {
		// Create a fresh session for this iteration (prevents context bloat).
		iterSessionDir := filepath.Join(sc.Deps.SessionDir, fmt.Sprintf("ralph-iter-%d", iteration))
		sess, err := session.New(iterSessionDir)
		if err != nil {
			return "", 0, fmt.Errorf("create iteration session: %w", err)
		}

		// Create conversation runtime with full tool access.
		rt := conversation.NewRuntime(
			sc.Deps.Config,
			sc.Deps.Provider,
			sess,
			sc.Deps.Registry,
			sc.Deps.PromptCtx,
		)

		// Read progress excerpt from previous iterations.
		var progressExcerpt string
		if sc.ProgressLog != nil {
			progressExcerpt, _ = sc.ProgressLog.Read()
			// Truncate long progress logs to keep context manageable.
			if len(progressExcerpt) > 2000 {
				progressExcerpt = "...\n" + progressExcerpt[len(progressExcerpt)-2000:]
			}
		}

		// Get current story if in PRD-driven mode.
		var story *Story
		if sc.StoryProvider != nil {
			story = sc.StoryProvider()
		}

		iterPrompt := GenerateIterationPrompt(state, progressExcerpt, story)
		fullPrompt := sc.UserPrompt + "\n\n" + iterPrompt

		// Build initial messages.
		messages := []api.Message{{
			Role: api.RoleUser,
			Content: []api.ContentBlock{
				{Type: api.ContentTypeText, Text: fullPrompt},
			},
		}}

		// Run the agentic loop: send → receive → execute tools → repeat.
		var allText []string
		for i := 0; i < maxAgenticIterations; i++ {
			result, _, err := rt.TurnWithRecovery(ctx, messages)
			if err != nil {
				return strings.Join(allText, ""), 0, fmt.Errorf("turn %d: %w", i+1, err)
			}

			// Collect text output.
			if result.TextContent != "" {
				allText = append(allText, result.TextContent)
				fmt.Print(result.TextContent)
			}

			// No tool calls means the agent is done.
			if len(result.ToolCalls) == 0 {
				break
			}

			// Build assistant message with tool_use blocks.
			var assistantBlocks []api.ContentBlock
			if result.ThinkingContent != "" {
				assistantBlocks = append(assistantBlocks, api.ContentBlock{
					Type: api.ContentTypeThinking,
					Text: result.ThinkingContent,
				})
			}
			if result.TextContent != "" {
				assistantBlocks = append(assistantBlocks, api.ContentBlock{
					Type: api.ContentTypeText,
					Text: result.TextContent,
				})
			}
			for _, tc := range result.ToolCalls {
				assistantBlocks = append(assistantBlocks, api.ContentBlock{
					Type:  api.ContentTypeToolUse,
					ID:    tc.ID,
					Name:  tc.Name,
					Input: tc.Input,
				})
				logger.Info("ralph: tool call", "iteration", iteration, "tool", tc.Name)
			}
			messages = append(messages, api.Message{
				Role:    api.RoleAssistant,
				Content: assistantBlocks,
			})

			// Execute tools and append results.
			toolResults := rt.ExecuteTools(ctx, result.ToolCalls, nil)
			messages = append(messages, api.Message{
				Role:    api.RoleUser,
				Content: toolResults,
			})
		}

		output := strings.Join(allText, "")
		fmt.Println()

		return output, 0, nil
	}
}

// SaveIterationMemory persists learnings from a Ralph iteration as procedural memory.
func SaveIterationMemory(mgr *memory.Manager, iteration int, storyID, output string) {
	if mgr == nil || output == "" {
		return
	}

	name := fmt.Sprintf("ralph-iter-%d", iteration)
	if storyID != "" {
		name = fmt.Sprintf("ralph-%s-iter-%d", storyID, iteration)
	}

	mem := memory.NewProceduralMemory(&memory.ProceduralPattern{
		Name:        name,
		Description: fmt.Sprintf("Ralph iteration %d learnings", iteration),
		Steps:       nil,
		Context:     "autonomous Ralph loop iteration",
		Rationale:   output,
		Source:      "ralph",
	})

	if err := mgr.Save(mem); err != nil {
		slog.Warn("ralph: failed to save iteration memory", "error", err)
	}
}
