package commands

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/qiangli/ycode/internal/runtime/session"
)

func testSession() *session.Session {
	return &session.Session{
		ID:        "test-session-123",
		CreatedAt: time.Date(2026, 4, 10, 12, 0, 0, 0, time.UTC),
		Messages: []session.ConversationMessage{
			{
				UUID:      "msg-1",
				Role:      session.RoleUser,
				Timestamp: time.Date(2026, 4, 10, 12, 0, 0, 0, time.UTC),
				Content: []session.ContentBlock{
					{Type: session.ContentTypeText, Text: "Please fix the login bug"},
				},
			},
			{
				UUID:      "msg-2",
				Role:      session.RoleAssistant,
				Timestamp: time.Date(2026, 4, 10, 12, 0, 1, 0, time.UTC),
				Content: []session.ContentBlock{
					{Type: session.ContentTypeText, Text: "I'll look into the login issue."},
					{
						Type:  session.ContentTypeToolUse,
						ID:    "toolu_01AbCdEfGhIjKlMnOp",
						Name:  "Read",
						Input: json.RawMessage(`{"file_path":"/src/auth/login.go","limit":100}`),
					},
				},
				Usage: &session.TokenUsage{
					InputTokens:  1500,
					OutputTokens: 200,
					CacheReadInput: 500,
				},
			},
			{
				UUID:      "msg-3",
				Role:      session.RoleUser,
				Timestamp: time.Date(2026, 4, 10, 12, 0, 2, 0, time.UTC),
				Content: []session.ContentBlock{
					{
						Type:      session.ContentTypeToolResult,
						ToolUseID: "toolu_01AbCdEfGhIjKlMnOp",
						Name:      "Read",
						Content:   "package auth\n\nfunc Login(user string) error {\n\treturn nil\n}",
					},
				},
			},
		},
	}
}

func TestExportHandler(t *testing.T) {
	dir := t.TempDir()
	sess := testSession()
	deps := &RuntimeDeps{
		SessionID: sess.ID,
		Session:   sess,
		WorkDir:   "/home/dev/project",
	}

	handler := exportHandler(deps)

	t.Run("export with explicit path", func(t *testing.T) {
		outFile := filepath.Join(dir, "out.md")
		result, err := handler(context.Background(), outFile)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if !strings.Contains(result, "3 messages") {
			t.Errorf("expected message count in result, got: %s", result)
		}
		if !strings.Contains(result, outFile) {
			t.Errorf("expected file path in result, got: %s", result)
		}

		data, err := os.ReadFile(outFile)
		if err != nil {
			t.Fatalf("read exported file: %v", err)
		}
		content := string(data)
		if !strings.Contains(content, "# Conversation Export") {
			t.Error("missing header")
		}
		if !strings.Contains(content, sess.ID) {
			t.Error("missing session ID")
		}
		if !strings.Contains(content, "## 1. User") {
			t.Error("missing user message header")
		}
		if !strings.Contains(content, "## 2. Assistant") {
			t.Error("missing assistant message header")
		}
		if !strings.Contains(content, "**Tool call** `Read`") {
			t.Error("missing tool call")
		}
		if !strings.Contains(content, "**Tool result** `Read`") {
			t.Error("missing tool result")
		}
		if !strings.Contains(content, "_tokens: in=1500 out=200") {
			t.Error("missing token usage")
		}
	})

	t.Run("export with auto filename", func(t *testing.T) {
		// Change to temp dir so auto-generated file lands there.
		orig, _ := os.Getwd()
		os.Chdir(dir)
		defer os.Chdir(orig)

		result, err := handler(context.Background(), "")
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if !strings.Contains(result, "please-fix-the-login-bug.md") {
			t.Errorf("expected auto-generated filename, got: %s", result)
		}
	})

	t.Run("export adds extension", func(t *testing.T) {
		outFile := filepath.Join(dir, "notes")
		result, err := handler(context.Background(), outFile)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if !strings.Contains(result, outFile+".md") {
			t.Errorf("expected .md extension added, got: %s", result)
		}
	})

	t.Run("no session", func(t *testing.T) {
		emptyDeps := &RuntimeDeps{Session: &session.Session{}}
		h := exportHandler(emptyDeps)
		_, err := h(context.Background(), "out.md")
		if err == nil {
			t.Error("expected error for empty session")
		}
	})
}

func TestResolveExportPath(t *testing.T) {
	sess := &session.Session{
		Messages: []session.ConversationMessage{
			{
				Role: session.RoleUser,
				Content: []session.ContentBlock{
					{Type: session.ContentTypeText, Text: "Hello world"},
				},
			},
		},
	}

	tests := []struct {
		arg  string
		want string
	}{
		{"notes.md", "notes.md"},
		{"notes", "notes.md"},
		{"output.txt", "output.txt"},
		{"", "hello-world.md"},
	}
	for _, tt := range tests {
		got := resolveExportPath(tt.arg, sess)
		if got != tt.want {
			t.Errorf("resolveExportPath(%q) = %q, want %q", tt.arg, got, tt.want)
		}
	}
}

func TestSanitizeFilename(t *testing.T) {
	tests := []struct {
		input string
		want  string
	}{
		{"Please fix the login bug", "please-fix-the-login-bug"},
		{"Hello! What's up?", "hello-whats-up"},
		{"a b c d e f g h i j k", "a-b-c-d-e-f-g-h"},
		{"", "conversation"},
		{"!!!???", "conversation"},
	}
	for _, tt := range tests {
		got := sanitizeFilename(tt.input)
		if got != tt.want {
			t.Errorf("sanitizeFilename(%q) = %q, want %q", tt.input, got, tt.want)
		}
	}
}

func TestSummarizeToolPayload(t *testing.T) {
	t.Run("compacts JSON", func(t *testing.T) {
		input := `{  "file_path":  "/src/main.go",  "limit": 100  }`
		got := summarizeToolPayload(input)
		if strings.Contains(got, "  ") {
			t.Errorf("expected compacted JSON, got: %s", got)
		}
	})

	t.Run("truncates long payload", func(t *testing.T) {
		long := strings.Repeat("x", 500)
		got := summarizeToolPayload(long)
		if len(got) > toolSummaryLimit+3 { // +3 for "…" (3 bytes in UTF-8)
			t.Errorf("expected truncated payload, got len=%d", len(got))
		}
		if !strings.HasSuffix(got, "…") {
			t.Error("expected ellipsis suffix")
		}
	})
}

func TestTruncateForSummary(t *testing.T) {
	if got := truncateForSummary("short", 10); got != "short" {
		t.Errorf("expected no truncation, got %q", got)
	}
	if got := truncateForSummary("longer text", 6); got != "longer…" {
		t.Errorf("expected truncation, got %q", got)
	}
}
