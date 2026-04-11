package session

import (
	"context"
	"encoding/json"
	"fmt"
	"testing"

	"github.com/qiangli/ycode/internal/api"
)

// mockProvider implements api.Provider for testing.
type mockProvider struct {
	response string
	err      error
}

func (m *mockProvider) Kind() api.ProviderKind { return api.ProviderAnthropic }

func (m *mockProvider) Send(ctx context.Context, req *api.Request) (<-chan *api.StreamEvent, <-chan error) {
	events := make(chan *api.StreamEvent, 4)
	errc := make(chan error, 1)

	go func() {
		defer close(events)
		defer close(errc)

		if m.err != nil {
			errc <- m.err
			return
		}

		// Send the response as a content_block_delta event.
		delta, _ := json.Marshal(struct {
			Text string `json:"text"`
		}{Text: m.response})
		events <- &api.StreamEvent{
			Type:  "content_block_delta",
			Delta: delta,
		}
	}()

	return events, errc
}

func TestLLMSummarizer_Summarize(t *testing.T) {
	provider := &mockProvider{
		response: `<intent_summary>
Scope: 5 messages compacted (user=2, assistant=2, tool=1).
Primary Goal: implement authentication
Verified Facts:
- Tests passing
Working Set: auth.go
</intent_summary>`,
	}

	summarizer := NewLLMSummarizer(provider, "test-model")
	messages := []ConversationMessage{
		{Role: RoleUser, Content: []ContentBlock{{Type: ContentTypeText, Text: "implement auth"}}},
		{Role: RoleAssistant, Content: []ContentBlock{{Type: ContentTypeText, Text: "I'll work on authentication"}}},
		{Role: RoleUser, Content: []ContentBlock{{Type: ContentTypeText, Text: "use JWT"}}},
		{Role: RoleAssistant, Content: []ContentBlock{{Type: ContentTypeText, Text: "I'll implement JWT tokens"}}},
		{Role: RoleUser, Content: []ContentBlock{{Type: ContentTypeText, Text: "add tests"}}},
	}

	summary, err := summarizer.Summarize(context.Background(), messages)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if summary == "" {
		t.Fatal("summary should not be empty")
	}
	if got := summary; got == "" {
		t.Error("summary should contain content")
	}
	if !contains(summary, "intent_summary") {
		t.Error("summary should contain intent_summary tags")
	}
	if !contains(summary, "Primary Goal") {
		t.Error("summary should contain Primary Goal")
	}
}

func TestLLMSummarizer_Error(t *testing.T) {
	provider := &mockProvider{
		err: fmt.Errorf("API unavailable"),
	}

	summarizer := NewLLMSummarizer(provider, "test-model")
	messages := []ConversationMessage{
		{Role: RoleUser, Content: []ContentBlock{{Type: ContentTypeText, Text: "hello"}}},
	}

	_, err := summarizer.Summarize(context.Background(), messages)
	if err == nil {
		t.Fatal("expected error from failed API call")
	}
}

func TestLLMSummarizer_EmptyResponse(t *testing.T) {
	provider := &mockProvider{response: ""}

	summarizer := NewLLMSummarizer(provider, "test-model")
	messages := []ConversationMessage{
		{Role: RoleUser, Content: []ContentBlock{{Type: ContentTypeText, Text: "hello"}}},
	}

	_, err := summarizer.Summarize(context.Background(), messages)
	if err == nil {
		t.Fatal("expected error from empty response")
	}
}

func TestLLMSummarizer_WrapsUnformatted(t *testing.T) {
	provider := &mockProvider{
		response: "Primary Goal: fix the bug\nWorking Set: main.go",
	}

	summarizer := NewLLMSummarizer(provider, "test-model")
	messages := []ConversationMessage{
		{Role: RoleUser, Content: []ContentBlock{{Type: ContentTypeText, Text: "fix the bug"}}},
	}

	summary, err := summarizer.Summarize(context.Background(), messages)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !contains(summary, "<intent_summary>") {
		t.Error("unformatted response should be wrapped in intent_summary tags")
	}
}

func TestCompactWithLLM_FallbackToHeuristic(t *testing.T) {
	messages := make([]ConversationMessage, 10)
	for i := 0; i < 10; i++ {
		role := RoleUser
		if i%2 == 1 {
			role = RoleAssistant
		}
		messages[i] = ConversationMessage{
			Role:    role,
			Content: []ContentBlock{{Type: ContentTypeText, Text: fmt.Sprintf("message %d", i)}},
		}
	}

	// LLM summarizer that fails — should fall back to heuristic.
	provider := &mockProvider{err: fmt.Errorf("API error")}
	summarizer := NewLLMSummarizer(provider, "test-model")

	result := CompactWithLLM(context.Background(), messages, "", summarizer)
	if result == nil {
		t.Fatal("should produce a result even when LLM fails")
	}
	if result.Summary == "" {
		t.Error("should have heuristic fallback summary")
	}
	if result.CompactedCount == 0 {
		t.Error("should have compacted messages")
	}
}

func TestCompactWithLLM_NilSummarizer(t *testing.T) {
	messages := make([]ConversationMessage, 10)
	for i := 0; i < 10; i++ {
		role := RoleUser
		if i%2 == 1 {
			role = RoleAssistant
		}
		messages[i] = ConversationMessage{
			Role:    role,
			Content: []ContentBlock{{Type: ContentTypeText, Text: fmt.Sprintf("message %d", i)}},
		}
	}

	// Nil summarizer — should use heuristic directly.
	result := CompactWithLLM(context.Background(), messages, "", nil)
	if result == nil {
		t.Fatal("should produce a result with nil summarizer")
	}
	if result.Summary == "" {
		t.Error("should have heuristic summary")
	}
}

func TestCompactWithLLM_TooFewMessages(t *testing.T) {
	messages := []ConversationMessage{
		{Role: RoleUser, Content: []ContentBlock{{Type: ContentTypeText, Text: "hello"}}},
	}

	result := CompactWithLLM(context.Background(), messages, "", nil)
	if result != nil {
		t.Error("should return nil for too few messages")
	}
}

func TestFormatMessagesForSummary(t *testing.T) {
	messages := []ConversationMessage{
		{Role: RoleUser, Content: []ContentBlock{{Type: ContentTypeText, Text: "fix the bug"}}},
		{Role: RoleAssistant, Content: []ContentBlock{
			{Type: ContentTypeToolUse, Name: "read_file"},
		}},
		{Role: RoleUser, Content: []ContentBlock{
			{Type: ContentTypeToolResult, Name: "read_file", Content: "file contents here"},
		}},
		{Role: RoleUser, Content: []ContentBlock{
			{Type: ContentTypeToolResult, Name: "bash", Content: "ERROR: test failed", IsError: true},
		}},
	}

	text := formatMessagesForSummary(messages)
	if !contains(text, "fix the bug") {
		t.Error("should contain user text")
	}
	if !contains(text, "[tool_use: read_file]") {
		t.Error("should contain tool_use marker")
	}
	if !contains(text, "[tool_result read_file:") {
		t.Error("should contain tool_result marker")
	}
	if !contains(text, "[tool_error bash:") {
		t.Error("should contain tool_error marker")
	}
}

// contains is defined in compact_test.go
