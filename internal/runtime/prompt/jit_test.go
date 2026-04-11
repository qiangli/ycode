package prompt

import (
	"os"
	"path/filepath"
	"testing"
)

func TestJITDiscovery_DiscoversSubdirectoryFiles(t *testing.T) {
	// Create temp project structure:
	// projectRoot/
	//   CLAUDE.md  (startup-discovered)
	//   sub/
	//     CLAUDE.md (JIT-discovered)
	//     deep/
	//       file.go
	root := t.TempDir()
	sub := filepath.Join(root, "sub")
	deep := filepath.Join(sub, "deep")
	if err := os.MkdirAll(deep, 0o755); err != nil {
		t.Fatal(err)
	}

	// Root CLAUDE.md (already known at startup).
	rootContent := "# Root instructions"
	if err := os.WriteFile(filepath.Join(root, "CLAUDE.md"), []byte(rootContent), 0o644); err != nil {
		t.Fatal(err)
	}

	// Sub CLAUDE.md (to be JIT-discovered).
	subContent := "# Sub instructions"
	if err := os.WriteFile(filepath.Join(sub, "CLAUDE.md"), []byte(subContent), 0o644); err != nil {
		t.Fatal(err)
	}

	// Simulate startup: discover root file, seed seen set.
	startupFiles := DiscoverInstructionFiles(root)
	seen := make(map[string]bool)
	totalChars := 0
	for _, f := range startupFiles {
		seen[f.Hash] = true
		totalChars += len(f.Content)
	}

	jit := NewJITDiscovery(root, seen, totalChars)

	// Simulate tool accessing a file in sub/deep/.
	n := jit.OnToolAccess(filepath.Join(deep, "file.go"))
	if n != 1 {
		t.Errorf("expected 1 new file discovered, got %d", n)
	}

	pending := jit.DrainPending()
	if len(pending) != 1 {
		t.Fatalf("expected 1 pending file, got %d", len(pending))
	}
	if pending[0].Content != subContent {
		t.Errorf("expected sub content, got %q", pending[0].Content)
	}
}

func TestJITDiscovery_DoesNotGoAboveProjectRoot(t *testing.T) {
	// Structure:
	// parent/
	//   CLAUDE.md  ← should NOT be discovered
	//   project/
	//     sub/
	//       file.go
	parent := t.TempDir()
	project := filepath.Join(parent, "project")
	sub := filepath.Join(project, "sub")
	if err := os.MkdirAll(sub, 0o755); err != nil {
		t.Fatal(err)
	}

	// Parent CLAUDE.md — outside project root.
	if err := os.WriteFile(filepath.Join(parent, "CLAUDE.md"), []byte("# Outside"), 0o644); err != nil {
		t.Fatal(err)
	}

	jit := NewJITDiscovery(project, nil, 0)

	n := jit.OnToolAccess(filepath.Join(sub, "file.go"))
	if n != 0 {
		t.Errorf("expected 0 files (none in project), got %d", n)
	}
}

func TestJITDiscovery_DeduplicatesOnRepeatedAccess(t *testing.T) {
	root := t.TempDir()
	sub := filepath.Join(root, "sub")
	if err := os.MkdirAll(sub, 0o755); err != nil {
		t.Fatal(err)
	}

	if err := os.WriteFile(filepath.Join(sub, "CLAUDE.md"), []byte("# Sub"), 0o644); err != nil {
		t.Fatal(err)
	}

	jit := NewJITDiscovery(root, nil, 0)

	n1 := jit.OnToolAccess(filepath.Join(sub, "a.go"))
	n2 := jit.OnToolAccess(filepath.Join(sub, "b.go"))

	if n1 != 1 {
		t.Errorf("first access should discover 1 file, got %d", n1)
	}
	if n2 != 0 {
		t.Errorf("second access should discover 0 files (dedup), got %d", n2)
	}
}

func TestJITDiscovery_DrainClearsPending(t *testing.T) {
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "CLAUDE.md"), []byte("# Test"), 0o644); err != nil {
		t.Fatal(err)
	}

	jit := NewJITDiscovery(root, nil, 0)
	jit.OnToolAccess(filepath.Join(root, "file.go"))

	if jit.PendingCount() != 1 {
		t.Fatalf("expected 1 pending, got %d", jit.PendingCount())
	}

	files := jit.DrainPending()
	if len(files) != 1 {
		t.Errorf("expected 1 drained file, got %d", len(files))
	}
	if jit.PendingCount() != 0 {
		t.Errorf("expected 0 pending after drain, got %d", jit.PendingCount())
	}
}

func TestJITDiscovery_RespectsBudget(t *testing.T) {
	root := t.TempDir()
	sub := filepath.Join(root, "sub")
	if err := os.MkdirAll(sub, 0o755); err != nil {
		t.Fatal(err)
	}

	// Create a file that would exceed budget when combined with existing chars.
	content := make([]byte, MaxTotalBudget-100)
	for i := range content {
		content[i] = 'x'
	}
	if err := os.WriteFile(filepath.Join(sub, "CLAUDE.md"), content, 0o644); err != nil {
		t.Fatal(err)
	}

	// Start with almost-full budget.
	jit := NewJITDiscovery(root, nil, MaxTotalBudget-50)
	n := jit.OnToolAccess(filepath.Join(sub, "file.go"))

	// File is larger than remaining budget (50 chars), should not be discovered.
	if n != 0 {
		t.Errorf("expected 0 (over budget), got %d", n)
	}
}
