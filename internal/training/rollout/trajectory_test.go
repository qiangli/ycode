package rollout

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestSaveLoadTrajectories(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "trajectories.jsonl")

	want := []ScoredTrajectory{
		{
			ID:        "t1",
			TaskName:  "gsm8k",
			ExampleID: "ex1",
			Messages: []Message{
				{Role: "user", Content: "What is 2+2?"},
				{Role: "assistant", Content: "4", ToolCalls: []ToolCall{
					{Name: "calculator", Arguments: `{"expr":"2+2"}`, Result: "4"},
				}},
			},
			Score:     1.0,
			TurnsUsed: 2,
			Duration:  5 * time.Second,
			Finished:  true,
		},
		{
			ID:         "t2",
			TaskName:   "gsm8k",
			ExampleID:  "ex2",
			Messages:   []Message{{Role: "user", Content: "Solve x"}},
			Score:      0.0,
			TurnsUsed:  1,
			ToolErrors: 1,
			Duration:   2 * time.Second,
			Finished:   false,
		},
	}

	if err := SaveTrajectories(path, want); err != nil {
		t.Fatalf("SaveTrajectories: %v", err)
	}

	got, err := LoadTrajectories(path)
	if err != nil {
		t.Fatalf("LoadTrajectories: %v", err)
	}

	if len(got) != len(want) {
		t.Fatalf("got %d trajectories, want %d", len(got), len(want))
	}

	for i := range want {
		if got[i].ID != want[i].ID {
			t.Errorf("trajectory[%d].ID = %q, want %q", i, got[i].ID, want[i].ID)
		}
		if got[i].Score != want[i].Score {
			t.Errorf("trajectory[%d].Score = %f, want %f", i, got[i].Score, want[i].Score)
		}
		if got[i].TurnsUsed != want[i].TurnsUsed {
			t.Errorf("trajectory[%d].TurnsUsed = %d, want %d", i, got[i].TurnsUsed, want[i].TurnsUsed)
		}
		if got[i].ToolErrors != want[i].ToolErrors {
			t.Errorf("trajectory[%d].ToolErrors = %d, want %d", i, got[i].ToolErrors, want[i].ToolErrors)
		}
		if got[i].Finished != want[i].Finished {
			t.Errorf("trajectory[%d].Finished = %v, want %v", i, got[i].Finished, want[i].Finished)
		}
		if len(got[i].Messages) != len(want[i].Messages) {
			t.Errorf("trajectory[%d] has %d messages, want %d", i, len(got[i].Messages), len(want[i].Messages))
		}
	}
}

func TestSaveTrajectories_InvalidPath(t *testing.T) {
	err := SaveTrajectories("/nonexistent/dir/file.jsonl", []ScoredTrajectory{{ID: "x"}})
	if err == nil {
		t.Fatal("expected error for invalid path")
	}
}

func TestLoadTrajectories_NotFound(t *testing.T) {
	_, err := LoadTrajectories("/nonexistent/file.jsonl")
	if err == nil {
		t.Fatal("expected error for missing file")
	}
}

func TestLoadTrajectories_Empty(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "empty.jsonl")
	if err := os.WriteFile(path, []byte{}, 0o644); err != nil {
		t.Fatal(err)
	}
	got, err := LoadTrajectories(path)
	if err != nil {
		t.Fatalf("LoadTrajectories on empty file: %v", err)
	}
	if len(got) != 0 {
		t.Fatalf("expected 0 trajectories, got %d", len(got))
	}
}

func TestComputeStats(t *testing.T) {
	trajectories := []ScoredTrajectory{
		{Score: 1.0, TurnsUsed: 3, ToolErrors: 0},
		{Score: 0.5, TurnsUsed: 5, ToolErrors: 1},
		{Score: 1.5, TurnsUsed: 2, ToolErrors: 0},
		{Score: 0.0, TurnsUsed: 10, ToolErrors: 3},
	}

	stats := ComputeStats(trajectories)

	if stats.Total != 4 {
		t.Errorf("Total = %d, want 4", stats.Total)
	}
	wantAvg := (1.0 + 0.5 + 1.5 + 0.0) / 4.0
	if diff := stats.AvgScore - wantAvg; diff > 1e-9 || diff < -1e-9 {
		t.Errorf("AvgScore = %f, want %f", stats.AvgScore, wantAvg)
	}
	if stats.MaxScore != 1.5 {
		t.Errorf("MaxScore = %f, want 1.5", stats.MaxScore)
	}
	if stats.MinScore != 0.0 {
		t.Errorf("MinScore = %f, want 0.0", stats.MinScore)
	}
	if stats.TotalErrors != 4 {
		t.Errorf("TotalErrors = %d, want 4", stats.TotalErrors)
	}
	// PassRate: 2 out of 4 have score >= 1.0 (1.0 and 1.5)
	if stats.PassRate != 0.5 {
		t.Errorf("PassRate = %f, want 0.5", stats.PassRate)
	}
	wantAvgTurns := (3.0 + 5.0 + 2.0 + 10.0) / 4.0
	if diff := stats.AvgTurns - wantAvgTurns; diff > 1e-9 || diff < -1e-9 {
		t.Errorf("AvgTurns = %f, want %f", stats.AvgTurns, wantAvgTurns)
	}
}

func TestComputeStats_Empty(t *testing.T) {
	stats := ComputeStats(nil)
	if stats.Total != 0 {
		t.Errorf("Total = %d, want 0", stats.Total)
	}
	if stats.AvgScore != 0 {
		t.Errorf("AvgScore = %f, want 0", stats.AvgScore)
	}
}

func TestResultBudget_DefaultBudget(t *testing.T) {
	b := DefaultBudget()
	if b.DefaultMaxChars != 4000 {
		t.Errorf("DefaultMaxChars = %d, want 4000", b.DefaultMaxChars)
	}
	if b.TurnBudgetChars != 16000 {
		t.Errorf("TurnBudgetChars = %d, want 16000", b.TurnBudgetChars)
	}
	if b.PreviewChars != 500 {
		t.Errorf("PreviewChars = %d, want 500", b.PreviewChars)
	}
}

func TestResultBudget_MaxCharsForTool(t *testing.T) {
	b := DefaultBudget()

	// Override tool.
	if got := b.MaxCharsForTool("terminal"); got != 10000 {
		t.Errorf("MaxCharsForTool(terminal) = %d, want 10000", got)
	}
	// Unlimited tool.
	if got := b.MaxCharsForTool("read_file"); got != 0 {
		t.Errorf("MaxCharsForTool(read_file) = %d, want 0", got)
	}
	// Default tool.
	if got := b.MaxCharsForTool("unknown_tool"); got != 4000 {
		t.Errorf("MaxCharsForTool(unknown_tool) = %d, want 4000", got)
	}
}

func TestResultBudget_TruncateResult(t *testing.T) {
	b := &ResultBudget{
		DefaultMaxChars: 20,
		PreviewChars:    5,
		Overrides:       map[string]int{"read_file": 0},
	}

	// Short result — no truncation.
	short := "hello"
	if got := b.TruncateResult("some_tool", short); got != short {
		t.Errorf("TruncateResult(short) = %q, want %q", got, short)
	}

	// Long result — should truncate.
	long := "abcdefghijklmnopqrstuvwxyz"
	got := b.TruncateResult("some_tool", long)
	want := "abcde\n[... truncated, full result persisted ...]"
	if got != want {
		t.Errorf("TruncateResult(long) = %q, want %q", got, want)
	}

	// Unlimited tool — no truncation.
	if got := b.TruncateResult("read_file", long); got != long {
		t.Errorf("TruncateResult(read_file, long) = %q, want %q", got, long)
	}
}
