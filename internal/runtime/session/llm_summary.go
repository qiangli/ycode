package session

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"strings"
	"time"

	"github.com/qiangli/ycode/internal/api"
)

const (
	// LLMSummaryTimeout is the maximum time to wait for an LLM summarization call.
	LLMSummaryTimeout = 30 * time.Second

	// LLMSummaryMaxTokens limits the output length for summarization.
	LLMSummaryMaxTokens = 1024

	llmSummaryPrompt = `You are a conversation summarizer for an AI coding assistant. Summarize the following conversation into a structured intent summary that preserves the most important context for continuing the work.

Use this exact format:

<intent_summary>
Scope: N messages compacted (user=X, assistant=Y, tool=Z).
Primary Goal: the main task being worked on
Verified Facts:
- confirmed outcomes (test results, successful builds, file modifications)
Working Set: files actively being edited
Active Blockers:
- errors or failures preventing progress
Decision Log:
- key decisions and their rationale
Key Files: all referenced file paths
Tools Used: tool names used
Pending Work:
- remaining tasks
</intent_summary>

Omit any section that has no entries. Be concise but preserve critical details — file paths, error messages, and decision rationale are especially important.

Here is the conversation to summarize:`
)

// ModelSpec identifies a model for summarization.
type ModelSpec struct {
	Provider api.Provider
	Model    string
}

// LLMSummarizer generates compaction summaries using an LLM provider.
// It supports a fallback chain: tries each model in order, using the first
// that succeeds. This allows using a cheap model (e.g., Haiku) first and
// falling back to the main model, following aider's pattern.
type LLMSummarizer struct {
	models []ModelSpec
}

// NewLLMSummarizer creates a new LLM-based summarizer with a single model.
func NewLLMSummarizer(provider api.Provider, model string) *LLMSummarizer {
	return &LLMSummarizer{
		models: []ModelSpec{{Provider: provider, Model: model}},
	}
}

// NewLLMSummarizerChain creates a summarizer with a fallback chain of models.
// Models are tried in order; the first successful response wins.
// Typical usage: [weakModel, mainModel] where weakModel is cheaper.
func NewLLMSummarizerChain(models []ModelSpec) *LLMSummarizer {
	return &LLMSummarizer{models: models}
}

// Summarize generates a structured intent summary of the given messages.
// Tries each model in the chain; returns the first successful result.
func (s *LLMSummarizer) Summarize(ctx context.Context, messages []ConversationMessage) (string, error) {
	conversationText := formatMessagesForSummary(messages)

	var lastErr error
	for _, ms := range s.models {
		summary, err := s.summarizeWith(ctx, ms, conversationText)
		if err != nil {
			slog.Info("summarization failed, trying next model", "model", ms.Model, "error", err)
			lastErr = err
			continue
		}
		slog.Info("summarization succeeded", "model", ms.Model)
		return summary, nil
	}

	return "", fmt.Errorf("all summarization models failed (last: %w)", lastErr)
}

// summarizeWith sends the summarization request to a specific model.
func (s *LLMSummarizer) summarizeWith(ctx context.Context, ms ModelSpec, conversationText string) (string, error) {
	ctx, cancel := context.WithTimeout(ctx, LLMSummaryTimeout)
	defer cancel()

	req := &api.Request{
		Model:     ms.Model,
		MaxTokens: LLMSummaryMaxTokens,
		Messages: []api.Message{
			{
				Role: api.RoleUser,
				Content: []api.ContentBlock{
					{Type: api.ContentTypeText, Text: llmSummaryPrompt + "\n\n" + conversationText},
				},
			},
		},
		Stream: true,
	}

	events, errc := ms.Provider.Send(ctx, req)

	var textParts []string
	for ev := range events {
		if ev.Delta != nil {
			var delta struct {
				Text string `json:"text"`
			}
			if err := json.Unmarshal(ev.Delta, &delta); err == nil && delta.Text != "" {
				textParts = append(textParts, delta.Text)
			}
		}
	}

	if err := <-errc; err != nil {
		return "", fmt.Errorf("llm summarization (%s): %w", ms.Model, err)
	}

	summary := strings.Join(textParts, "")
	if strings.TrimSpace(summary) == "" {
		return "", fmt.Errorf("llm summarization (%s) returned empty response", ms.Model)
	}

	// Ensure the response contains the expected format; if not, wrap it.
	if !strings.Contains(summary, "<intent_summary>") {
		summary = "<intent_summary>\n" + strings.TrimSpace(summary) + "\n</intent_summary>"
	}

	return strings.TrimSpace(summary), nil
}

// formatMessagesForSummary converts conversation messages to a readable text format
// suitable for LLM summarization.
func formatMessagesForSummary(messages []ConversationMessage) string {
	var b strings.Builder
	for _, msg := range messages {
		b.WriteString(string(msg.Role))
		b.WriteString(": ")
		for _, c := range msg.Content {
			switch c.Type {
			case ContentTypeText:
				text := c.Text
				if len(text) > 500 {
					text = text[:500] + "..."
				}
				b.WriteString(text)
			case ContentTypeToolUse:
				b.WriteString(fmt.Sprintf("[tool_use: %s]", c.Name))
			case ContentTypeToolResult:
				content := c.Content
				if len(content) > 300 {
					content = content[:300] + "..."
				}
				if c.IsError {
					b.WriteString(fmt.Sprintf("[tool_error %s: %s]", c.Name, content))
				} else {
					b.WriteString(fmt.Sprintf("[tool_result %s: %s]", c.Name, content))
				}
			}
		}
		b.WriteByte('\n')
	}
	return b.String()
}
