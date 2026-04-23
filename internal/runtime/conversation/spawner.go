package conversation

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"

	"github.com/qiangli/ycode/internal/api"
	"github.com/qiangli/ycode/internal/runtime/prompt"
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
}

// NewAgentSpawner creates a spawner function that can be passed to
// RegisterAgentHandler. Each invocation creates a child runtime with
// mode-specific system prompt and filtered tool access, runs a bounded
// agentic loop, and returns the text result.
func NewAgentSpawner(sc *SpawnerConfig) func(ctx context.Context, manifest *tools.AgentManifest) (string, error) {
	return func(ctx context.Context, manifest *tools.AgentManifest) (string, error) {
		mode := tools.AgentTypeToMode(manifest.Type)
		logger := sc.Logger
		if logger == nil {
			logger = slog.Default()
		}

		logger.Info("spawning subagent",
			"type", manifest.Type,
			"mode", mode,
			"description", manifest.Description,
		)

		// Build mode-specific system prompt.
		systemPrompt := prompt.BuildDefault(sc.PromptCtx, string(mode), sc.CachingSupported, nil)

		// Get allowed tools for this mode and create a filtered registry.
		allowed := tools.AllowedToolsForMode(mode)
		filtered := tools.NewFilteredRegistry(sc.Registry, allowed)

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

		// Build initial messages with the user prompt from the manifest.
		messages := []api.Message{
			{
				Role: api.RoleUser,
				Content: []api.ContentBlock{
					{Type: api.ContentTypeText, Text: manifest.Prompt},
				},
			},
		}

		// Agentic loop: send → receive → execute tools → repeat.
		for i := 0; i < maxSubagentIterations; i++ {
			req := &api.Request{
				Model:     sc.Model,
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
				return "", fmt.Errorf("subagent turn %d: %w", i+1, err)
			}

			textContent := joinParts(textParts)

			// No tool calls — subagent is done.
			if len(toolCalls) == 0 {
				logger.Info("subagent completed", "turns", i+1)
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

			// Execute tools through the filtered registry.
			var toolResults []api.ContentBlock
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
				toolResults = append(toolResults, block)
				logger.Info("subagent tool executed", "tool", tc.Name, "error", err != nil)
			}

			messages = append(messages, api.Message{
				Role:    api.RoleUser,
				Content: toolResults,
			})
		}

		return "", fmt.Errorf("subagent exceeded maximum iterations (%d)", maxSubagentIterations)
	}
}
