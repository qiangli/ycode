package session

import (
	"testing"
)

func TestDetectDuplicateFileReads_NoDuplicates(t *testing.T) {
	messages := []ConversationMessage{
		{Role: RoleAssistant, Content: []ContentBlock{
			{Type: ContentTypeToolUse, Name: "read_file", Input: []byte(`{"path":"/a/b.go"}`)},
		}},
		{Role: RoleUser, Content: []ContentBlock{
			{Type: ContentTypeToolResult, Content: "file content"},
		}},
	}

	dups := DetectDuplicateFileReads(messages)
	if len(dups) != 0 {
		t.Errorf("expected no duplicates, got %d", len(dups))
	}
}

func TestDetectDuplicateFileReads_WithDuplicates(t *testing.T) {
	messages := []ConversationMessage{
		{Role: RoleAssistant, Content: []ContentBlock{
			{Type: ContentTypeToolUse, Name: "read_file", Input: []byte(`{"file_path":"/a/b.go"}`)},
		}},
		{Role: RoleUser, Content: []ContentBlock{
			{Type: ContentTypeToolResult, Content: "file content here"},
		}},
		{Role: RoleAssistant, Content: []ContentBlock{
			{Type: ContentTypeToolUse, Name: "read_file", Input: []byte(`{"file_path":"/a/b.go"}`)},
		}},
		{Role: RoleUser, Content: []ContentBlock{
			{Type: ContentTypeToolResult, Content: "file content here"},
		}},
		{Role: RoleAssistant, Content: []ContentBlock{
			{Type: ContentTypeToolUse, Name: "read_file", Input: []byte(`{"file_path":"/a/b.go"}`)},
		}},
		{Role: RoleUser, Content: []ContentBlock{
			{Type: ContentTypeToolResult, Content: "file content here"},
		}},
	}

	dups := DetectDuplicateFileReads(messages)
	if len(dups) != 1 {
		t.Fatalf("expected 1 duplicate entry, got %d", len(dups))
	}
	if dups[0].ReadCount != 3 {
		t.Errorf("expected 3 reads, got %d", dups[0].ReadCount)
	}
	if dups[0].TokenWaste <= 0 {
		t.Error("expected positive token waste")
	}
}

func TestFormatDuplicateReadHint_BelowThreshold(t *testing.T) {
	dups := []DuplicateFileRead{
		{Path: "/a.go", ReadCount: 2, TokenWaste: 100},
	}
	hint := FormatDuplicateReadHint(dups)
	if hint != "" {
		t.Error("below threshold should return empty hint")
	}
}

func TestFormatDuplicateReadHint_AboveThreshold(t *testing.T) {
	dups := []DuplicateFileRead{
		{Path: "/a.go", ReadCount: 5, TokenWaste: 10_000},
	}
	hint := FormatDuplicateReadHint(dups)
	if hint == "" {
		t.Fatal("above threshold should return a hint")
	}
	if !contains(hint, "/a.go") {
		t.Error("hint should mention the file path")
	}
}

func TestAnalyzeContext(t *testing.T) {
	messages := []ConversationMessage{
		{Role: RoleUser, Content: []ContentBlock{
			{Type: ContentTypeText, Text: "fix the bug"},
		}},
		{Role: RoleAssistant, Content: []ContentBlock{
			{Type: ContentTypeText, Text: "I'll look into it"},
			{Type: ContentTypeToolUse, Name: "read_file", Input: []byte(`{"path":"/main.go"}`)},
		}},
		{Role: RoleUser, Content: []ContentBlock{
			{Type: ContentTypeToolResult, Name: "read_file", Content: "package main"},
		}},
	}

	analysis := AnalyzeContext(messages)
	if analysis.TotalTokens <= 0 {
		t.Error("total tokens should be positive")
	}
	if analysis.HumanTokens <= 0 {
		t.Error("human tokens should be positive")
	}
	if analysis.AssistantTokens <= 0 {
		t.Error("assistant tokens should be positive")
	}
	if _, ok := analysis.ToolRequests["read_file"]; !ok {
		t.Error("should track read_file tool request")
	}
	if _, ok := analysis.ToolResults["read_file"]; !ok {
		t.Error("should track read_file tool result")
	}
}

func TestTokenBudgetTracker_NoStop(t *testing.T) {
	tracker := NewTokenBudgetTracker()

	// Making good progress.
	if tracker.Update(1000) {
		t.Error("should not stop after first update")
	}
	if tracker.Update(5000) {
		t.Error("should not stop with good delta")
	}
	if tracker.Update(10000) {
		t.Error("should not stop with good delta")
	}
}

func TestTokenBudgetTracker_DiminishingReturns(t *testing.T) {
	tracker := NewTokenBudgetTracker()

	// Making minimal progress.
	tracker.Update(100)
	tracker.Update(200) // delta 100 < 500
	tracker.Update(250) // delta 50 < 500

	shouldStop := tracker.Update(280) // delta 30 < 500, 3rd low delta
	if !shouldStop {
		t.Error("should stop after 3+ continuations with low delta")
	}
	if !tracker.ShouldStop() {
		t.Error("ShouldStop should return true")
	}
}

func TestTokenBudgetTracker_Reset(t *testing.T) {
	tracker := NewTokenBudgetTracker()

	tracker.Update(100)
	tracker.Update(200)
	tracker.Update(250)

	tracker.Reset()

	if tracker.ContinuationCount != 0 {
		t.Error("reset should clear continuation count")
	}
	if tracker.ShouldStop() {
		t.Error("should not stop after reset")
	}
}

func TestTokenBudgetTracker_RecoveryAfterProgress(t *testing.T) {
	tracker := NewTokenBudgetTracker()

	// Low delta streak.
	tracker.Update(100)
	tracker.Update(200)

	// Then makes progress.
	tracker.Update(5000) // delta 4800 > 500 — resets streak.

	// More low delta.
	shouldStop := tracker.Update(5100)
	if shouldStop {
		t.Error("should not stop — streak was broken by progress")
	}
}
