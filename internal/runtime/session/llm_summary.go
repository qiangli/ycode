package session

import (
	"context"
	"encoding/json"
	"fmt"
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

// LLMSummarizer generates compaction summaries using an LLM provider.
type LLMSummarizer struct {
	provider api.Provider
	model    string
}

// NewLLMSummarizer creates a new LLM-based summarizer.
func NewLLMSummarizer(provider api.Provider, model string) *LLMSummarizer {
	return &LLMSummarizer{
		provider: provider,
		model:    model,
	}
}

// Summarize generates a structured intent summary of the given messages using the LLM.
// Returns the summary text, or an error if the LLM call fails.
func (s *LLMSummarizer) Summarize(ctx context.Context, messages []ConversationMessage) (string, error) {
	conversationText := formatMessagesForSummary(messages)

	ctx, cancel := context.WithTimeout(ctx, LLMSummaryTimeout)
	defer cancel()

	req := &api.Request{
		Model:     s.model,
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

	events, errc := s.provider.Send(ctx, req)

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
		return "", fmt.Errorf("llm summarization: %w", err)
	}

	summary := strings.Join(textParts, "")
	if strings.TrimSpace(summary) == "" {
		return "", fmt.Errorf("llm summarization returned empty response")
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
