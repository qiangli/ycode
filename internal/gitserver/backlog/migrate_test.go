package backlog

import (
	"log/slog"
	"os"
	"path/filepath"
	"testing"
)

func TestMigrateLegacy_PopulatedOldEmptyNew(t *testing.T) {
	old := t.TempDir()
	newD := t.TempDir()
	mustWriteString(t, filepath.Join(old, "task-a.md"), "---\ntitle: A\n---\nbody A")
	mustWriteString(t, filepath.Join(old, "task-b.md"), "---\ntitle: B\n---\nbody B")
	mustWriteString(t, filepath.Join(old, "not-a-task.txt"), "ignore me")

	if err := MigrateLegacy(old, newD, slog.Default()); err != nil {
		t.Fatalf("migrate: %v", err)
	}
	gotA, err := os.ReadFile(filepath.Join(newD, "task-a.md"))
	if err != nil {
		t.Fatalf("read task-a: %v", err)
	}
	if string(gotA) != "---\ntitle: A\n---\nbody A" {
		t.Fatalf("task-a content wrong: %q", gotA)
	}
	if _, err := os.Stat(filepath.Join(newD, "not-a-task.txt")); !os.IsNotExist(err) {
		t.Fatal("non-md file should not have been migrated")
	}
	// Legacy file must remain.
	if _, err := os.Stat(filepath.Join(old, "task-a.md")); err != nil {
		t.Fatalf("legacy file should remain: %v", err)
	}
}

func TestMigrateLegacy_EmptyOldNoOp(t *testing.T) {
	old := t.TempDir()
	newD := t.TempDir()
	if err := MigrateLegacy(old, newD, slog.Default()); err != nil {
		t.Fatalf("migrate: %v", err)
	}
	entries, _ := os.ReadDir(newD)
	if len(entries) != 0 {
		t.Fatalf("new dir should be untouched, got %d entries", len(entries))
	}
}

func TestMigrateLegacy_PopulatedNewLeavesNewUntouched(t *testing.T) {
	old := t.TempDir()
	newD := t.TempDir()
	mustWriteString(t, filepath.Join(old, "task-a.md"), "from old")
	mustWriteString(t, filepath.Join(newD, "task-a.md"), "from new (do not overwrite)")

	if err := MigrateLegacy(old, newD, slog.Default()); err != nil {
		t.Fatalf("migrate: %v", err)
	}
	got, err := os.ReadFile(filepath.Join(newD, "task-a.md"))
	if err != nil {
		t.Fatalf("read: %v", err)
	}
	if string(got) != "from new (do not overwrite)" {
		t.Fatalf("new content was overwritten: %q", got)
	}
}

func TestMigrateLegacy_MissingOldDir(t *testing.T) {
	newD := t.TempDir()
	if err := MigrateLegacy("/does/not/exist", newD, slog.Default()); err != nil {
		t.Fatalf("missing old dir should not error: %v", err)
	}
}

func TestMigrateLegacy_Idempotent(t *testing.T) {
	old := t.TempDir()
	newD := t.TempDir()
	mustWriteString(t, filepath.Join(old, "task.md"), "v1")
	if err := MigrateLegacy(old, newD, slog.Default()); err != nil {
		t.Fatalf("migrate (first): %v", err)
	}
	// Modify the new dir between runs; second call should not clobber.
	mustWriteString(t, filepath.Join(newD, "task.md"), "edited")
	if err := MigrateLegacy(old, newD, slog.Default()); err != nil {
		t.Fatalf("migrate (second): %v", err)
	}
	got, _ := os.ReadFile(filepath.Join(newD, "task.md"))
	if string(got) != "edited" {
		t.Fatalf("second migrate clobbered edited file: %q", got)
	}
}

func mustWriteString(t *testing.T, path, body string) {
	t.Helper()
	if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
		t.Fatalf("write %s: %v", path, err)
	}
}
