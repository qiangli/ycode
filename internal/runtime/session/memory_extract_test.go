package session

import (
	"context"
	"testing"
)

func TestHeuristicExtract_Corrections(t *testing.T) {
	messages := []ConversationMessage{
		{Role: RoleUser, Content: []ContentBlock{
			{Type: ContentTypeText, Text: "no, don't use that approach"},
		}},
		{Role: RoleAssistant, Content: []ContentBlock{
			{Type: ContentTypeText, Text: "understood, I'll change it"},
		}},
	}

	memories := HeuristicExtract(messages)
	if len(memories) == 0 {
		t.Fatal("should detect correction")
	}
	if memories[0].Type != "feedback" {
		t.Errorf("expected feedback type, got %s", memories[0].Type)
	}
}

func TestHeuristicExtract_Confirmations(t *testing.T) {
	messages := []ConversationMessage{
		{Role: RoleUser, Content: []ContentBlock{
			{Type: ContentTypeText, Text: "yes exactly, that's the right way to do it"},
		}},
	}

	memories := HeuristicExtract(messages)
	if len(memories) == 0 {
		t.Fatal("should detect confirmation")
	}
	if memories[0].Type != "feedback" {
		t.Errorf("expected feedback type, got %s", memories[0].Type)
	}
}

func TestHeuristicExtract_IgnoresShortConfirmations(t *testing.T) {
	messages := []ConversationMessage{
		{Role: RoleUser, Content: []ContentBlock{
			{Type: ContentTypeText, Text: "yes"},
		}},
	}

	memories := HeuristicExtract(messages)
	if len(memories) != 0 {
		t.Error("short confirmations should be ignored")
	}
}

func TestHeuristicExtract_SkipsAssistant(t *testing.T) {
	messages := []ConversationMessage{
		{Role: RoleAssistant, Content: []ContentBlock{
			{Type: ContentTypeText, Text: "no, I won't do that"},
		}},
	}

	memories := HeuristicExtract(messages)
	if len(memories) != 0 {
		t.Error("should only extract from user messages")
	}
}

func TestFormatTranscriptForExtraction(t *testing.T) {
	messages := []ConversationMessage{
		{Role: RoleUser, Content: []ContentBlock{
			{Type: ContentTypeText, Text: "fix the bug"},
		}},
		{Role: RoleAssistant, Content: []ContentBlock{
			{Type: ContentTypeText, Text: "looking into it"},
			{Type: ContentTypeToolUse, Name: "read_file"},
		}},
		{Role: RoleUser, Content: []ContentBlock{
			{Type: ContentTypeToolResult, Name: "read_file", Content: "code", IsError: false},
		}},
	}

	transcript := formatTranscriptForExtraction(messages)
	if transcript == "" {
		t.Fatal("should produce transcript")
	}
	if !contains(transcript, "User: fix the bug") {
		t.Error("should include user message")
	}
	if !contains(transcript, "[tool: read_file]") {
		t.Error("should include tool use")
	}
}

func TestMemoryExtractor_RespectsTurnGap(t *testing.T) {
	// Create a real extractor with noop functions.
	me := NewMemoryExtractor(
		func(_ context.Context, _ string) ([]ExtractableMemory, error) {
			return nil, nil
		},
		func(_ context.Context, _ ExtractableMemory) error {
			return nil
		},
	)

	messages := []ConversationMessage{
		{Role: RoleUser, Content: []ContentBlock{{Type: ContentTypeText, Text: "test"}}},
	}

	// First call at turn 0 should trigger.
	me.MaybeExtract(0, messages)
	// Call at turn 2 should NOT trigger (gap < ExtractorMinTurnGap).
	me.MaybeExtract(2, messages)
	// Call at turn 5 should trigger.
	me.MaybeExtract(5, messages)

	// We can't easily verify goroutine execution, but the turn tracking is testable.
	if me.lastTurn != 5 {
		t.Errorf("expected lastTurn=5, got %d", me.lastTurn)
	}
}
