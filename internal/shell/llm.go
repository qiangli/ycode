package shell

import (
	"context"
	"encoding/json"
	"errors"
	"strings"

	"github.com/qiangli/ycode/internal/api"
)

// ErrNoProvider is returned by the agent sentinels when no LLM provider
// has been configured for this shell session (the user did not export an
// API key, or the model was misconfigured).
var ErrNoProvider = errors.New("no LLM provider configured for this shell — set ANTHROPIC_API_KEY / OPENAI_API_KEY / OPENAI_BASE_URL or pass --model")

// OneShot sends a single user prompt to the configured provider and
// returns the accumulated text. No tools, streaming consumed internally.
// Used by the `!` (agent shot) and `?` (cheap Q&A) sentinels.
//
// system is the system prompt; userText is the user message body.
// maxTokens caps output (defaults to 1024 when ≤ 0).
func OneShot(ctx context.Context, provider api.Provider, model, system, userText string, maxTokens int) (string, error) {
	if provider == nil {
		return "", ErrNoProvider
	}
	if maxTokens <= 0 {
		maxTokens = 1024
	}
	req := &api.Request{
		Model:     api.ResolveModel(model),
		MaxTokens: maxTokens,
		System:    system,
		Messages: []api.Message{{
			Role: api.RoleUser,
			Content: []api.ContentBlock{{
				Type: api.ContentTypeText,
				Text: userText,
			}},
		}},
		Stream: true,
	}

	events, errc := provider.Send(ctx, req)
	var out strings.Builder

	for ev := range events {
		switch ev.Type {
		case "content_block_delta":
			if len(ev.Delta) > 0 {
				var delta struct {
					Type string `json:"type"`
					Text string `json:"text"`
				}
				if err := json.Unmarshal(ev.Delta, &delta); err == nil && delta.Text != "" {
					out.WriteString(delta.Text)
				}
			}
		}
	}
	if err, ok := <-errc; ok && err != nil {
		return out.String(), err
	}
	return out.String(), nil
}

// agentShotSystem is the canned system prompt for `!<text>`. Includes
// shell context: cwd, last command, last output (truncated).
func agentShotSystem(cwd, lastCmd, lastOutput string) string {
	var sb strings.Builder
	sb.WriteString("You are an agentic shell assistant inside ycode shell. ")
	sb.WriteString("The user is at an interactive terminal and has just typed an `!` query. ")
	sb.WriteString("Be concise; respond in plain text suitable for a terminal. ")
	sb.WriteString("Do not invent tool calls — you do not have tool access in this mode.\n\n")
	sb.WriteString("Shell context:\n")
	sb.WriteString("  cwd: ")
	sb.WriteString(cwd)
	sb.WriteString("\n")
	if lastCmd != "" {
		sb.WriteString("  last command: ")
		sb.WriteString(lastCmd)
		sb.WriteString("\n")
	}
	if lastOutput != "" {
		sb.WriteString("  last output (truncated to 2KB):\n")
		sb.WriteString(truncate(lastOutput, 2048))
		sb.WriteString("\n")
	}
	return sb.String()
}

// agentQASystem is the canned system prompt for `?<text>` — fastest path.
const agentQASystem = "You are a concise terminal Q&A assistant. The user has typed a `?` question at the shell prompt. Answer in 1–4 short paragraphs of plain text suitable for a terminal. No code blocks unless the question genuinely requires them."

func truncate(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n] + "…[truncated]"
}
