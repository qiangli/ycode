package prompt

import (
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
)

func TestDiscoverInstructionFiles_StopsAtCeiling(t *testing.T) {
	// Structure:
	// parent/
	//   AGENTS.md  ← outside ceiling, should NOT be found
	//   project/   ← ceiling
	//     CLAUDE.md ← should be found
	//     sub/
	//       (startDir)
	parent := t.TempDir()
	project := filepath.Join(parent, "project")
	sub := filepath.Join(project, "sub")
	if err := os.MkdirAll(sub, 0o755); err != nil {
		t.Fatal(err)
	}

	if err := os.WriteFile(filepath.Join(parent, "AGENTS.md"), []byte("# Outside"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(project, "CLAUDE.md"), []byte("# Project"), 0o644); err != nil {
		t.Fatal(err)
	}

	files := DiscoverInstructionFiles(sub, project)
	if len(files) != 1 {
		t.Fatalf("expected 1 file (project CLAUDE.md only), got %d", len(files))
	}
	if filepath.Base(files[0].Path) != "CLAUDE.md" {
		t.Errorf("expected CLAUDE.md, got %s", files[0].Path)
	}
}

func TestDiscoverInstructionFiles_EmptyCeilingDefaultsToStartDir(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "AGENTS.md"), []byte("# Test"), 0o644); err != nil {
		t.Fatal(err)
	}

	files := DiscoverInstructionFiles(dir, "")
	if len(files) != 1 {
		t.Fatalf("expected 1 file, got %d", len(files))
	}
}

func TestDiscoverInstructionFiles_FindsSubdirFiles(t *testing.T) {
	// Structure:
	// root/         ← ceiling
	//   AGENTS.md
	//   src/
	//     AGENTS.md  ← also discovered
	root := t.TempDir()
	src := filepath.Join(root, "src")
	if err := os.MkdirAll(src, 0o755); err != nil {
		t.Fatal(err)
	}

	if err := os.WriteFile(filepath.Join(root, "AGENTS.md"), []byte("# Root"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(src, "AGENTS.md"), []byte("# Src"), 0o644); err != nil {
		t.Fatal(err)
	}

	files := DiscoverInstructionFiles(src, root)
	if len(files) != 2 {
		t.Fatalf("expected 2 files (src + root), got %d", len(files))
	}
}

func TestDiscoverGlobalInstructionFiles(t *testing.T) {
	dir := t.TempDir()

	if err := os.WriteFile(filepath.Join(dir, "AGENTS.md"), []byte("# Global agents"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "CLAUDE.md"), []byte("# Global claude"), 0o644); err != nil {
		t.Fatal(err)
	}

	files := DiscoverGlobalInstructionFiles(dir)
	if len(files) != 2 {
		t.Fatalf("expected 2 global files, got %d", len(files))
	}
}

func TestDiscoverGlobalInstructionFiles_EmptyDir(t *testing.T) {
	files := DiscoverGlobalInstructionFiles("")
	if len(files) != 0 {
		t.Errorf("expected 0 files for empty dir, got %d", len(files))
	}
}

func TestDiscoverGlobalInstructionFiles_NoFiles(t *testing.T) {
	dir := t.TempDir()
	files := DiscoverGlobalInstructionFiles(dir)
	if len(files) != 0 {
		t.Errorf("expected 0 files, got %d", len(files))
	}
}

func TestLoadConfiguredInstructions_AbsolutePath(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "custom.md")
	if err := os.WriteFile(path, []byte("# Custom instructions"), 0o644); err != nil {
		t.Fatal(err)
	}

	files := LoadConfiguredInstructions([]string{path}, dir)
	if len(files) != 1 {
		t.Fatalf("expected 1 file, got %d", len(files))
	}
	if files[0].Path != path {
		t.Errorf("expected path %s, got %s", path, files[0].Path)
	}
}

func TestLoadConfiguredInstructions_RelativePath(t *testing.T) {
	root := t.TempDir()
	docsDir := filepath.Join(root, "docs")
	if err := os.MkdirAll(docsDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(docsDir, "INSTRUCTIONS.md"), []byte("# Docs"), 0o644); err != nil {
		t.Fatal(err)
	}

	files := LoadConfiguredInstructions([]string{"docs/INSTRUCTIONS.md"}, root)
	if len(files) != 1 {
		t.Fatalf("expected 1 file, got %d", len(files))
	}
}

func TestLoadConfiguredInstructions_URL(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		w.Write([]byte("# Remote instructions"))
	}))
	defer srv.Close()

	files := LoadConfiguredInstructions([]string{srv.URL + "/instructions.md"}, "")
	if len(files) != 1 {
		t.Fatalf("expected 1 file from URL, got %d", len(files))
	}
	if files[0].Content != "# Remote instructions" {
		t.Errorf("unexpected content: %q", files[0].Content)
	}
}

func TestLoadConfiguredInstructions_MissingFile(t *testing.T) {
	files := LoadConfiguredInstructions([]string{"/nonexistent/file.md"}, "")
	if len(files) != 0 {
		t.Errorf("expected 0 files for missing path, got %d", len(files))
	}
}

func TestLoadConfiguredInstructions_Deduplication(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "inst.md")
	if err := os.WriteFile(path, []byte("# Same content"), 0o644); err != nil {
		t.Fatal(err)
	}

	// Pass same path twice — should deduplicate.
	files := LoadConfiguredInstructions([]string{path, path}, dir)
	if len(files) != 1 {
		t.Errorf("expected 1 file (dedup), got %d", len(files))
	}
}

func TestLoadConfiguredInstructions_Empty(t *testing.T) {
	files := LoadConfiguredInstructions(nil, "")
	if len(files) != 0 {
		t.Errorf("expected 0 files for nil paths, got %d", len(files))
	}
}
