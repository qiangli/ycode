package routing

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/qiangli/ycode/internal/api"
)

const (
	// classifyTimeout is the maximum time for a classification call.
	classifyTimeout = 3 * time.Second

	// classifyMaxTokens caps the output to minimize latency.
	classifyMaxTokens = 64

	classifySystemPrompt = `You classify user requests into tool categories. Given a user message, return ONLY a JSON array of matching categories from this list:

- "git" — git operations (commit, branch, status, diff, log, merge, rebase, stash)
- "observability" — debugging, performance analysis, metrics, traces, logs, errors
- "memory" — saving/recalling memories, memos, notes
- "file_ops" — file copy, move, rename, delete, directory creation
- "web" — fetching URLs, web search, downloading
- "agent" — delegating to subagents, task management, scheduling

Rules:
- Return [] if no categories match clearly
- Be conservative — only include categories with strong signal
- Return ONLY the JSON array, no explanation

Example: "commit the fix and check the metrics"
["git","observability"]`
)

// categoryToolBundles maps classification categories to tool names.
var categoryToolBundles = map[string][]string{
	"git":           {"git_status", "git_log", "git_commit", "git_branch", "git_stash", "view_diff"},
	"observability": {"query_metrics", "query_traces", "query_logs"},
	"memory":        {"memory_save", "memory_recall", "memory_forget", "MemosStore", "MemosSearch", "MemosList"},
	"file_ops":      {"copy_file", "move_file", "delete_file", "create_directory"},
	"web":           {"WebFetch", "WebSearch"},
	"agent":         {"Agent", "TaskCreate", "TaskList", "TaskGet"},
}

// ClassifyTools uses a lightweight LLM call to classify the user message
// into tool categories and returns the tool names to pre-activate.
// Returns nil if no classification candidates are available or the call fails.
func (r *Router) ClassifyTools(ctx context.Context, userMessage string) []string {
	// Route to the best provider for classification.
	best := r.Route(ctx, TaskClassification)
	if best == nil || best.Provider == nil {
		return nil
	}

	ctx, cancel := context.WithTimeout(ctx, classifyTimeout)
	defer cancel()

	// Make a single-shot LLM call.
	response, err := singleShot(ctx, best.Provider, best.Model, classifySystemPrompt, userMessage, classifyMaxTokens)
	if err != nil {
		r.logger.Warn("tool classification failed", "model", best.Model, "error", err)
		return nil
	}

	// Parse the JSON array response.
	categories := parseCategories(response)
	if len(categories) == 0 {
		return nil
	}

	// Map categories to tool names.
	var toolNames []string
	seen := make(map[string]bool)
	for _, cat := range categories {
		for _, tool := range categoryToolBundles[cat] {
			if !seen[tool] {
				seen[tool] = true
				toolNames = append(toolNames, tool)
			}
		}
	}

	r.logger.Info("tool classification result",
		"model", best.Model,
		"categories", categories,
		"tools", len(toolNames),
	)
	return toolNames
}

// parseCategories extracts a JSON string array from an LLM response.
// Tolerates markdown code fences and extra whitespace.
func parseCategories(response string) []string {
	response = strings.TrimSpace(response)
	// Strip markdown code fences if present.
	response = strings.TrimPrefix(response, "```json")
	response = strings.TrimPrefix(response, "```")
	response = strings.TrimSuffix(response, "```")
	response = strings.TrimSpace(response)

	var categories []string
	if err := json.Unmarshal([]byte(response), &categories); err != nil {
		return nil
	}

	// Validate against known categories.
	var valid []string
	for _, cat := range categories {
		if _, ok := categoryToolBundles[cat]; ok {
			valid = append(valid, cat)
		}
	}
	return valid
}

// singleShot sends a minimal request to a provider and returns the text response.
func singleShot(ctx context.Context, provider api.Provider, model, systemPrompt, userContent string, maxTokens int) (string, error) {
	req := &api.Request{
		Model:     model,
		MaxTokens: maxTokens,
		System:    systemPrompt,
		Messages: []api.Message{
			{
				Role: api.RoleUser,
				Content: []api.ContentBlock{
					{Type: api.ContentTypeText, Text: userContent},
				},
			},
		},
		Stream: false,
	}

	events, errc := provider.Send(ctx, req)

	var textParts []string
	for ev := range events {
		switch ev.Type {
		case "content_block_delta":
			if ev.Delta != nil {
				var delta struct {
					Text string `json:"text"`
				}
				if err := json.Unmarshal(ev.Delta, &delta); err == nil && delta.Text != "" {
					textParts = append(textParts, delta.Text)
				}
			}
		case "message_start":
			if ev.Message != nil {
				for _, block := range ev.Message.Content {
					if block.Type == api.ContentTypeText && block.Text != "" {
						textParts = append(textParts, block.Text)
					}
				}
			}
		}
	}

	if err := <-errc; err != nil {
		return "", fmt.Errorf("classification call: %w", err)
	}

	return strings.Join(textParts, ""), nil
}
