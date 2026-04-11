package prompt

import (
	"context"
	"os"
	"path/filepath"
	"testing"
)

func TestPrewarm_DiscoversFiles(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "CLAUDE.md"), []byte("# Test"), 0o644); err != nil {
		t.Fatal(err)
	}

	ctx := context.Background()
	result := Prewarm(ctx, dir, nil)

	if len(result.Errors) > 0 {
		t.Errorf("unexpected errors: %v", result.Errors)
	}
	if len(result.ContextFiles) != 1 {
		t.Errorf("expected 1 context file, got %d", len(result.ContextFiles))
	}
}

func TestPrewarm_HandlesCancellation(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel() // Cancel immediately.

	result := Prewarm(ctx, "/nonexistent", nil)

	// Should not panic, may or may not have errors depending on timing.
	_ = result
}
