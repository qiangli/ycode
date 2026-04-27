package ralph

import (
	"os"
	"path/filepath"
	"testing"
)

func TestArchiveRunAndList(t *testing.T) {
	runDir := t.TempDir()
	archiveDir := t.TempDir()

	// Create some files in the run directory.
	if err := os.WriteFile(filepath.Join(runDir, "state.json"), []byte(`{"iteration":5}`), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(runDir, "progress.txt"), []byte("log content"), 0o644); err != nil {
		t.Fatal(err)
	}
	// prd.json is missing — should be skipped gracefully.

	archivePath, err := ArchiveRun(runDir, archiveDir, "test run")
	if err != nil {
		t.Fatalf("ArchiveRun: %v", err)
	}

	// Verify files were copied.
	data, err := os.ReadFile(filepath.Join(archivePath, "state.json"))
	if err != nil {
		t.Fatalf("read archived state: %v", err)
	}
	if string(data) != `{"iteration":5}` {
		t.Fatalf("state content = %q", string(data))
	}

	data, err = os.ReadFile(filepath.Join(archivePath, "progress.txt"))
	if err != nil {
		t.Fatalf("read archived progress: %v", err)
	}
	if string(data) != "log content" {
		t.Fatalf("progress content = %q", string(data))
	}

	// prd.json should not exist since it was missing.
	if _, err := os.Stat(filepath.Join(archivePath, "prd.json")); !os.IsNotExist(err) {
		t.Fatal("prd.json should not exist in archive")
	}

	// List archives.
	archives, err := ListArchives(archiveDir)
	if err != nil {
		t.Fatalf("ListArchives: %v", err)
	}
	if len(archives) != 1 {
		t.Fatalf("archives = %d, want 1", len(archives))
	}
	if archives[0].Path != archivePath {
		t.Fatalf("path = %q, want %q", archives[0].Path, archivePath)
	}
}

func TestListArchivesNonexistent(t *testing.T) {
	archives, err := ListArchives("/nonexistent/dir")
	if err != nil {
		t.Fatalf("ListArchives: %v", err)
	}
	if len(archives) != 0 {
		t.Fatalf("expected empty, got %d", len(archives))
	}
}

func TestRestoreArchive(t *testing.T) {
	archiveDir := t.TempDir()
	runDir := t.TempDir()

	// Create archived files.
	if err := os.WriteFile(filepath.Join(archiveDir, "state.json"), []byte(`{"iteration":3}`), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(archiveDir, "progress.txt"), []byte("old log"), 0o644); err != nil {
		t.Fatal(err)
	}

	if err := RestoreArchive(archiveDir, runDir); err != nil {
		t.Fatalf("RestoreArchive: %v", err)
	}

	data, err := os.ReadFile(filepath.Join(runDir, "state.json"))
	if err != nil {
		t.Fatal(err)
	}
	if string(data) != `{"iteration":3}` {
		t.Fatalf("restored state = %q", string(data))
	}

	data, err = os.ReadFile(filepath.Join(runDir, "progress.txt"))
	if err != nil {
		t.Fatal(err)
	}
	if string(data) != "old log" {
		t.Fatalf("restored progress = %q", string(data))
	}
}

func TestSanitizeLabel(t *testing.T) {
	tests := []struct {
		input string
		want  string
	}{
		{"hello world", "hello-world"},
		{"test!@#run", "testrun"},
		{"a-b_c", "a-b_c"},
		{"", ""},
	}
	for _, tt := range tests {
		got := sanitizeLabel(tt.input)
		if got != tt.want {
			t.Errorf("sanitizeLabel(%q) = %q, want %q", tt.input, got, tt.want)
		}
	}
}
