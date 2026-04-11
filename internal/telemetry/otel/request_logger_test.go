package otel

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestRequestLogger(t *testing.T) {
	dir := t.TempDir()

	rl, err := NewRequestLogger(dir, RequestLoggerConfig{
		RetentionDays:  3,
		LogToolDetails: true,
	})
	if err != nil {
		t.Fatal(err)
	}
	defer rl.Close()

	record := &ConversationRecord{
		Timestamp:    time.Now(),
		SessionID:    "test-session",
		TurnIndex:    1,
		Provider:     "anthropic",
		Model:        "claude-sonnet-4-20250514",
		SystemPrompt: "You are a helpful assistant.",
		MaxTokens:    8192,
		ResponseText: "Hello world",
		StopReason:   "end_turn",
		TokensIn:     100,
		TokensOut:    50,
		DurationMs:   1234,
		Success:      true,
		ToolCalls: []ToolCallLog{
			{
				Name:       "Read",
				Source:     "builtin",
				Input:      json.RawMessage(`{"file_path":"/tmp/test.txt"}`),
				Output:     "file contents here",
				Success:    true,
				DurationMs: 10,
			},
		},
	}

	if err := rl.Log(record); err != nil {
		t.Fatal(err)
	}

	// Verify the file was created.
	today := time.Now().Format("2006-01-02")
	logFile := filepath.Join(dir, "logs", "conversations-"+today+".jsonl")
	data, err := os.ReadFile(logFile)
	if err != nil {
		t.Fatal(err)
	}

	var parsed ConversationRecord
	if err := json.Unmarshal(data, &parsed); err != nil {
		t.Fatalf("unmarshal log: %v", err)
	}
	if parsed.SessionID != "test-session" {
		t.Errorf("SessionID = %q, want %q", parsed.SessionID, "test-session")
	}
	if parsed.Model != "claude-sonnet-4-20250514" {
		t.Errorf("Model = %q, want %q", parsed.Model, "claude-sonnet-4-20250514")
	}
	if len(parsed.ToolCalls) != 1 {
		t.Errorf("ToolCalls len = %d, want 1", len(parsed.ToolCalls))
	}
	if parsed.ToolCalls[0].Name != "Read" {
		t.Errorf("ToolCall name = %q, want %q", parsed.ToolCalls[0].Name, "Read")
	}
	if string(parsed.ToolCalls[0].Input) != `{"file_path":"/tmp/test.txt"}` {
		t.Errorf("ToolCall input = %q, want %q", string(parsed.ToolCalls[0].Input), `{"file_path":"/tmp/test.txt"}`)
	}
}

func TestRequestLoggerNoToolDetails(t *testing.T) {
	dir := t.TempDir()

	rl, err := NewRequestLogger(dir, RequestLoggerConfig{
		RetentionDays:  3,
		LogToolDetails: false,
	})
	if err != nil {
		t.Fatal(err)
	}
	defer rl.Close()

	record := &ConversationRecord{
		Timestamp: time.Now(),
		SessionID: "test",
		ToolCalls: []ToolCallLog{
			{
				Name:   "Read",
				Input:  json.RawMessage(`{"file_path":"/tmp/test.txt"}`),
				Output: "content",
			},
		},
	}

	if err := rl.Log(record); err != nil {
		t.Fatal(err)
	}

	today := time.Now().Format("2006-01-02")
	logFile := filepath.Join(dir, "logs", "conversations-"+today+".jsonl")
	data, err := os.ReadFile(logFile)
	if err != nil {
		t.Fatal(err)
	}

	var parsed ConversationRecord
	if err := json.Unmarshal(data, &parsed); err != nil {
		t.Fatal(err)
	}
	if len(parsed.ToolCalls[0].Input) > 0 && string(parsed.ToolCalls[0].Input) != "null" {
		t.Errorf("ToolCall input should be nil when LogToolDetails=false, got %q", string(parsed.ToolCalls[0].Input))
	}
	if parsed.ToolCalls[0].Output != "" {
		t.Errorf("ToolCall output should be empty when LogToolDetails=false, got %q", parsed.ToolCalls[0].Output)
	}
}
