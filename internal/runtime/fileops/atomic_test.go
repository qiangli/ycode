package fileops

import (
	"os"
	"path/filepath"
	"runtime"
	"testing"
)

func TestAtomicWriteFile_BasicWriteAndReadBack(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "test.txt")
	data := []byte("hello, atomic world")

	if err := AtomicWriteFile(path, data, 0o644); err != nil {
		t.Fatalf("AtomicWriteFile: %v", err)
	}

	got, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("ReadFile: %v", err)
	}
	if string(got) != string(data) {
		t.Errorf("got %q, want %q", got, data)
	}
}

func TestAtomicWriteFile_OverwriteExisting(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "test.txt")

	// Write initial content.
	initial := []byte("initial content")
	if err := os.WriteFile(path, initial, 0o644); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}

	// Overwrite atomically.
	updated := []byte("updated content")
	if err := AtomicWriteFile(path, updated, 0o644); err != nil {
		t.Fatalf("AtomicWriteFile: %v", err)
	}

	got, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("ReadFile: %v", err)
	}
	if string(got) != string(updated) {
		t.Errorf("got %q, want %q", got, updated)
	}
}

func TestAtomicWriteFile_DataIntegrity(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "test.txt")

	// Write a known file.
	original := []byte("precious data that must not be lost")
	if err := AtomicWriteFile(path, original, 0o644); err != nil {
		t.Fatalf("AtomicWriteFile: %v", err)
	}

	// Write new content successfully.
	newData := []byte("new data replacing the old")
	if err := AtomicWriteFile(path, newData, 0o644); err != nil {
		t.Fatalf("AtomicWriteFile: %v", err)
	}

	got, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("ReadFile: %v", err)
	}
	if string(got) != string(newData) {
		t.Errorf("got %q, want %q", got, newData)
	}
}

func TestAtomicWriteFile_PermissionPreservation(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "test.txt")

	if err := AtomicWriteFile(path, []byte("test"), 0o600); err != nil {
		t.Fatalf("AtomicWriteFile: %v", err)
	}

	info, err := os.Stat(path)
	if err != nil {
		t.Fatalf("Stat: %v", err)
	}

	// Check permission bits (mask with 0o777 to ignore OS-specific bits).
	// Windows models only the read-only attribute: a writable file reports
	// 0666 whatever owner/group/other bits were requested.
	want := os.FileMode(0o600)
	if runtime.GOOS == "windows" {
		want = 0o666
	}
	got := info.Mode().Perm()
	if got != want {
		t.Errorf("permission = %o, want %o", got, want)
	}
}
