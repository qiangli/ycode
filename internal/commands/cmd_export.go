package commands

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"time"

	"github.com/qiangli/ycode/internal/runtime/session"
)

const (
	// toolSummaryLimit is the max characters for tool payload summaries.
	toolSummaryLimit = 280
	// maxFilenameWords is the max words used to derive a filename.
	maxFilenameWords = 8
)

// exportHandler returns a handler that exports the conversation to a markdown file.
func exportHandler(deps *RuntimeDeps) HandlerFunc {
	return func(ctx context.Context, args string) (string, error) {
		if deps.Session == nil || len(deps.Session.Messages) == 0 {
			return "", fmt.Errorf("no conversation to export")
		}

		path := resolveExportPath(strings.TrimSpace(args), deps.Session)

		content := renderExportMarkdown(deps)

		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			return "", fmt.Errorf("create export directory: %w", err)
		}
		if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
			return "", fmt.Errorf("write export file: %w", err)
		}

		return fmt.Sprintf("Exported %d messages to %s", len(deps.Session.Messages), path), nil
	}
}

// resolveExportPath determines the output file path for an export.
func resolveExportPath(arg string, s *session.Session) string {
	if arg != "" {
		if !strings.Contains(filepath.Base(arg), ".") {
			arg += ".md"
		}
		return arg
	}
	return defaultExportFilename(s) + ".md"
}

// defaultExportFilename derives a filename from the first user message.
func defaultExportFilename(s *session.Session) string {
	for _, msg := range s.Messages {
		if msg.Role != session.RoleUser {
			continue
		}
		for _, block := range msg.Content {
			if block.Type == session.ContentTypeText && block.Text != "" {
				return sanitizeFilename(block.Text)
			}
		}
	}
	return "conversation"
}

// filenameUnsafe matches characters not safe for filenames.
var filenameUnsafe = regexp.MustCompile(`[^a-zA-Z0-9_-]+`)

// sanitizeFilename turns text into a safe, short filename.
func sanitizeFilename(text string) string {
	words := strings.Fields(text)
	if len(words) > maxFilenameWords {
		words = words[:maxFilenameWords]
	}
	name := strings.Join(words, "-")
	name = strings.ToLower(name)
	name = filenameUnsafe.ReplaceAllString(name, "")
	// Collapse repeated dashes and trim.
	for strings.Contains(name, "--") {
		name = strings.ReplaceAll(name, "--", "-")
	}
	name = strings.Trim(name, "-")
	if name == "" {
		return "conversation"
	}
	if len(name) > 80 {
		name = name[:80]
	}
	return name
}

// renderExportMarkdown formats the full conversation as markdown.
func renderExportMarkdown(deps *RuntimeDeps) string {
	s := deps.Session
	var b strings.Builder

	// Header.
	b.WriteString("# Conversation Export\n\n")
	fmt.Fprintf(&b, "- **Session**: %s\n", s.ID)
	fmt.Fprintf(&b, "- **Messages**: %d\n", len(s.Messages))
	if !s.CreatedAt.IsZero() {
		fmt.Fprintf(&b, "- **Created**: %s\n", s.CreatedAt.Format(time.RFC3339))
	}
	if deps.WorkDir != "" {
		fmt.Fprintf(&b, "- **Workspace**: %s\n", deps.WorkDir)
	}
	if s.Summary != "" {
		fmt.Fprintf(&b, "- **Summary**: %s\n", s.Summary)
	}
	b.WriteString("\n---\n\n")

	// Messages.
	for i, msg := range s.Messages {
		role := roleLabel(msg.Role)
		fmt.Fprintf(&b, "## %d. %s\n\n", i+1, role)

		for _, block := range msg.Content {
			switch block.Type {
			case session.ContentTypeText:
				b.WriteString(block.Text)
				b.WriteString("\n\n")

			case session.ContentTypeToolUse:
				shortID := truncateID(block.ID)
				fmt.Fprintf(&b, "**Tool call** `%s` _(id %s)_\n\n", block.Name, shortID)
				if len(block.Input) > 0 {
					summary := summarizeToolPayload(string(block.Input))
					fmt.Fprintf(&b, "> %s\n\n", summary)
				}

			case session.ContentTypeToolResult:
				shortID := truncateID(block.ToolUseID)
				status := "ok"
				if block.IsError {
					status = "error"
				}
				name := block.Name
				if name == "" {
					name = "tool"
				}
				fmt.Fprintf(&b, "**Tool result** `%s` _(id %s, %s)_\n\n", name, shortID, status)
				if block.Content != "" {
					summary := summarizeToolPayload(block.Content)
					fmt.Fprintf(&b, "> %s\n\n", summary)
				}
			}
		}

		if msg.Usage != nil {
			fmt.Fprintf(&b, "_tokens: in=%d out=%d",
				msg.Usage.InputTokens, msg.Usage.OutputTokens)
			if msg.Usage.CacheCreationInput > 0 {
				fmt.Fprintf(&b, " cache_create=%d", msg.Usage.CacheCreationInput)
			}
			if msg.Usage.CacheReadInput > 0 {
				fmt.Fprintf(&b, " cache_read=%d", msg.Usage.CacheReadInput)
			}
			b.WriteString("_\n\n")
		}
	}

	return b.String()
}

// roleLabel returns a display label for a message role.
func roleLabel(role session.MessageRole) string {
	switch role {
	case session.RoleUser:
		return "User"
	case session.RoleAssistant:
		return "Assistant"
	case session.RoleSystem:
		return "System"
	default:
		return string(role)
	}
}

// truncateID returns a short form of a tool use ID.
func truncateID(id string) string {
	if len(id) <= 12 {
		return id
	}
	return id[:12]
}

// summarizeToolPayload compacts and truncates a tool payload for display.
func summarizeToolPayload(payload string) string {
	// Try to compact JSON.
	var raw json.RawMessage
	if err := json.Unmarshal([]byte(payload), &raw); err == nil {
		if compacted, err := json.Marshal(raw); err == nil {
			payload = string(compacted)
		}
	} else {
		// Normalize whitespace for non-JSON.
		payload = strings.Join(strings.Fields(payload), " ")
	}
	return truncateForSummary(payload, toolSummaryLimit)
}

// truncateForSummary truncates a string to maxLen, appending "…" if truncated.
func truncateForSummary(s string, maxLen int) string {
	if len(s) <= maxLen {
		return s
	}
	return s[:maxLen] + "…"
}
