package eval

import (
	"os"
	"path/filepath"
	"testing"
)

func TestResponseContains(t *testing.T) {
	result := &RunResult{Response: "The answer is 4."}

	a := ResponseContains{Substring: "4"}
	if err := a.Check(result); err != nil {
		t.Errorf("expected pass: %v", err)
	}

	a = ResponseContains{Substring: "42"}
	if err := a.Check(result); err == nil {
		t.Error("expected failure for missing substring")
	}
}

func TestResponseMatches(t *testing.T) {
	result := &RunResult{Response: "The answer is 42."}

	a := &ResponseMatches{Pattern: `\d+`}
	if err := a.Check(result); err != nil {
		t.Errorf("expected pass: %v", err)
	}

	a = &ResponseMatches{Pattern: `^exact$`}
	if err := a.Check(result); err == nil {
		t.Error("expected failure for non-matching pattern")
	}
}

func TestNoError(t *testing.T) {
	a := NoError{}

	if err := a.Check(&RunResult{}); err != nil {
		t.Errorf("expected pass for nil error: %v", err)
	}

	if err := a.Check(&RunResult{Error: os.ErrNotExist}); err == nil {
		t.Error("expected failure for non-nil error")
	}
}

func TestToolUsed(t *testing.T) {
	result := &RunResult{
		ToolCalls: []ToolCall{
			{Name: "read_file"},
			{Name: "edit_file"},
		},
	}

	a := ToolUsed{ToolName: "read_file"}
	if err := a.Check(result); err != nil {
		t.Errorf("expected pass: %v", err)
	}

	a = ToolUsed{ToolName: "bash"}
	if err := a.Check(result); err == nil {
		t.Error("expected failure for unused tool")
	}
}

func TestToolNotUsed(t *testing.T) {
	result := &RunResult{
		ToolCalls: []ToolCall{
			{Name: "read_file"},
		},
	}

	a := ToolNotUsed{ToolName: "bash"}
	if err := a.Check(result); err != nil {
		t.Errorf("expected pass: %v", err)
	}

	a = ToolNotUsed{ToolName: "read_file"}
	if err := a.Check(result); err == nil {
		t.Error("expected failure for used tool")
	}
}

func TestFileExists(t *testing.T) {
	dir := t.TempDir()
	testFile := filepath.Join(dir, "test.txt")
	if err := os.WriteFile(testFile, []byte("hello"), 0o644); err != nil {
		t.Fatal(err)
	}

	result := &RunResult{WorkDir: dir}

	a := FileExists{Path: "test.txt"}
	if err := a.Check(result); err != nil {
		t.Errorf("expected pass: %v", err)
	}

	a = FileExists{Path: "missing.txt"}
	if err := a.Check(result); err == nil {
		t.Error("expected failure for missing file")
	}
}

func TestFileContains(t *testing.T) {
	dir := t.TempDir()
	testFile := filepath.Join(dir, "test.txt")
	if err := os.WriteFile(testFile, []byte("hello world"), 0o644); err != nil {
		t.Fatal(err)
	}

	result := &RunResult{WorkDir: dir}

	a := FileContains{Path: "test.txt", Substring: "world"}
	if err := a.Check(result); err != nil {
		t.Errorf("expected pass: %v", err)
	}

	a = FileContains{Path: "test.txt", Substring: "missing"}
	if err := a.Check(result); err == nil {
		t.Error("expected failure for missing substring")
	}
}

func TestMaxTurns(t *testing.T) {
	a := MaxTurns{Max: 5}

	if err := a.Check(&RunResult{Turns: 3}); err != nil {
		t.Errorf("expected pass: %v", err)
	}

	if err := a.Check(&RunResult{Turns: 10}); err == nil {
		t.Error("expected failure for exceeding max turns")
	}
}

func TestMaxTokens(t *testing.T) {
	a := MaxTokens{Max: 1000}

	if err := a.Check(&RunResult{InputTokens: 300, OutputTokens: 200}); err != nil {
		t.Errorf("expected pass: %v", err)
	}

	if err := a.Check(&RunResult{InputTokens: 800, OutputTokens: 500}); err == nil {
		t.Error("expected failure for exceeding max tokens")
	}
}

func TestToolCount(t *testing.T) {
	result := &RunResult{
		ToolCalls: []ToolCall{
			{Name: "read_file"},
			{Name: "edit_file"},
			{Name: "read_file"},
		},
	}

	a := ToolCount{ToolName: "read_file", Count: 2}
	if err := a.Check(result); err != nil {
		t.Errorf("expected pass: %v", err)
	}

	a = ToolCount{ToolName: "read_file", Count: 1}
	if err := a.Check(result); err == nil {
		t.Error("expected failure for wrong count")
	}
}

func TestExpectedToolSequence(t *testing.T) {
	toolCalls := []ToolCall{
		{Name: "read_file"},
		{Name: "edit_file"},
		{Name: "bash"},
	}

	// Perfect match.
	a := ExpectedToolSequence{Expected: []string{"read_file", "edit_file", "bash"}}
	score, err := a.Check(toolCalls)
	if err != nil {
		t.Errorf("expected pass: %v", err)
	}
	if !almostEqual(score, 1.0, 0.01) {
		t.Errorf("expected score 1.0, got %.4f", score)
	}

	// Completely wrong sequence should fail.
	a = ExpectedToolSequence{Expected: []string{"bash", "write_file", "glob_search"}}
	score, err = a.Check(toolCalls)
	if err == nil {
		t.Error("expected failure for bad sequence")
	}
	if score >= 0.5 {
		t.Errorf("expected score < 0.5, got %.4f", score)
	}
}

func TestMinToolAccuracy(t *testing.T) {
	toolCalls := []ToolCall{
		{Name: "read_file"},
		{Name: "edit_file"},
	}

	a := MinToolAccuracy{
		ExpectedTools: []string{"read_file", "edit_file"},
		MinAccuracy:   0.8,
	}
	score, err := a.Check(toolCalls)
	if err != nil {
		t.Errorf("expected pass: %v", err)
	}
	if !almostEqual(score, 1.0, 0.01) {
		t.Errorf("expected score 1.0, got %.4f", score)
	}

	a = MinToolAccuracy{
		ExpectedTools: []string{"read_file", "write_file", "bash"},
		MinAccuracy:   0.9,
	}
	_, err = a.Check(toolCalls)
	if err == nil {
		t.Error("expected failure for low accuracy")
	}
}
