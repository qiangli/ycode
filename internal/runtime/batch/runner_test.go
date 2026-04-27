package batch

import (
	"os"
	"path/filepath"
	"testing"
)

func TestLoadPrompts(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "prompts.jsonl")

	content := `{"id":"p1","prompt":"Hello world"}
{"id":"p2","prompt":"Summarize this","model":"gpt-4"}
{"id":"p3","prompt":"Translate to French"}
`
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}

	prompts, err := LoadPrompts(path)
	if err != nil {
		t.Fatalf("LoadPrompts: %v", err)
	}
	if len(prompts) != 3 {
		t.Fatalf("got %d prompts, want 3", len(prompts))
	}
	if prompts[0].ID != "p1" || prompts[0].Prompt != "Hello world" {
		t.Errorf("prompt 0 = %+v", prompts[0])
	}
	if prompts[1].Model != "gpt-4" {
		t.Errorf("prompt 1 model = %q, want gpt-4", prompts[1].Model)
	}
}

func TestLoadPrompts_FileNotFound(t *testing.T) {
	_, err := LoadPrompts("/nonexistent/path.jsonl")
	if err == nil {
		t.Fatal("expected error for missing file")
	}
}

func TestLoadPrompts_InvalidJSON(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "bad.jsonl")
	if err := os.WriteFile(path, []byte("not json\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	_, err := LoadPrompts(path)
	if err == nil {
		t.Fatal("expected error for invalid JSON")
	}
}

func TestNewRunner_Defaults(t *testing.T) {
	r := NewRunner(RunnerConfig{})
	if r.config.Concurrency != 4 {
		t.Errorf("default concurrency = %d, want 4", r.config.Concurrency)
	}
	if r.config.MaxRetries != 2 {
		t.Errorf("default max retries = %d, want 2", r.config.MaxRetries)
	}
	if r.stats.ToolUsage == nil {
		t.Error("ToolUsage map not initialized")
	}
}

func TestNewRunner_CustomConfig(t *testing.T) {
	r := NewRunner(RunnerConfig{
		Concurrency: 8,
		MaxRetries:  5,
	})
	if r.config.Concurrency != 8 {
		t.Errorf("concurrency = %d, want 8", r.config.Concurrency)
	}
	if r.config.MaxRetries != 5 {
		t.Errorf("max retries = %d, want 5", r.config.MaxRetries)
	}
}
