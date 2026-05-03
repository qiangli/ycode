package session

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestFileTracker_Track(t *testing.T) {
	ft := NewFileTracker()

	ft.Track("/a/b.go")
	ft.Track("/c/d.go")
	ft.Track("/e/f.go")

	recent := ft.RecentFiles(3)
	if len(recent) != 3 {
		t.Fatalf("expected 3 files, got %d", len(recent))
	}
	// Most recent first.
	if recent[0] != "/e/f.go" {
		t.Errorf("expected /e/f.go first, got %s", recent[0])
	}
	if recent[2] != "/a/b.go" {
		t.Errorf("expected /a/b.go last, got %s", recent[2])
	}
}

func TestFileTracker_TrackDuplicate(t *testing.T) {
	ft := NewFileTracker()

	ft.Track("/a/b.go")
	ft.Track("/c/d.go")
	ft.Track("/a/b.go") // Re-edit: should move to most recent.

	recent := ft.RecentFiles(3)
	if len(recent) != 2 {
		t.Fatalf("expected 2 files (deduped), got %d", len(recent))
	}
	if recent[0] != "/a/b.go" {
		t.Errorf("expected /a/b.go most recent, got %s", recent[0])
	}
}

func TestFileTracker_RecentFilesLimit(t *testing.T) {
	ft := NewFileTracker()
	for i := 0; i < 10; i++ {
		ft.Track("/file" + string(rune('0'+i)) + ".go")
	}

	recent := ft.RecentFiles(3)
	if len(recent) != 3 {
		t.Fatalf("expected 3, got %d", len(recent))
	}
}

func TestFileTracker_RecentFilesEmpty(t *testing.T) {
	ft := NewFileTracker()
	if recent := ft.RecentFiles(5); len(recent) != 0 {
		t.Errorf("expected empty, got %d", len(recent))
	}
}

func TestRestoreRecentFiles(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping test that writes temp files")
	}

	dir := t.TempDir()

	// Create test files.
	for i := 0; i < 3; i++ {
		path := filepath.Join(dir, "file"+string(rune('0'+i))+".go")
		if err := os.WriteFile(path, []byte("package main\n// file "+string(rune('0'+i))), 0o644); err != nil {
			t.Fatal(err)
		}
	}

	tracker := NewFileTracker()
	for i := 0; i < 3; i++ {
		tracker.Track(filepath.Join(dir, "file"+string(rune('0'+i))+".go"))
	}

	messages, tokens := RestoreRecentFiles(tracker)
	if len(messages) != 3 {
		t.Fatalf("expected 3 restored messages, got %d", len(messages))
	}
	if tokens <= 0 {
		t.Error("expected positive token count")
	}

	// Verify content includes file restore marker.
	for _, msg := range messages {
		if len(msg.Content) == 0 {
			t.Fatal("expected content blocks")
		}
		if !strings.Contains(msg.Content[0].Content, "[Post-compaction file restore:") {
			t.Error("expected file restore marker")
		}
	}
}

func TestRestoreRecentFiles_Nil(t *testing.T) {
	messages, tokens := RestoreRecentFiles(nil)
	if len(messages) != 0 || tokens != 0 {
		t.Error("nil tracker should return empty")
	}
}

func TestRestoreRecentFiles_MissingFile(t *testing.T) {
	tracker := NewFileTracker()
	tracker.Track("/nonexistent/path/file.go")

	messages, tokens := RestoreRecentFiles(tracker)
	if len(messages) != 0 || tokens != 0 {
		t.Error("missing files should be skipped")
	}
}

func TestTruncateToTokenBudget(t *testing.T) {
	short := "short text"
	if truncateToTokenBudget(short, 100) != short {
		t.Error("short text should not be truncated")
	}

	long := strings.Repeat("x", 10000)
	truncated := truncateToTokenBudget(long, 100)
	if len(truncated) >= len(long) {
		t.Error("long text should be truncated")
	}
	if !strings.Contains(truncated, "characters omitted for token budget") {
		t.Error("should contain omission marker")
	}
}

func TestTrackEditedFiles(t *testing.T) {
	tracker := NewFileTracker()

	messages := []ConversationMessage{
		{Role: RoleAssistant, Content: []ContentBlock{
			{Type: ContentTypeToolUse, Name: "write_file", Input: []byte(`{"file_path": "/tmp/test/auth.go"}`)},
		}},
		{Role: RoleUser, Content: []ContentBlock{
			{Type: ContentTypeToolResult, Content: "written"},
		}},
		{Role: RoleAssistant, Content: []ContentBlock{
			{Type: ContentTypeToolUse, Name: "edit_file", Input: []byte(`{"file_path": "/tmp/test/main.go"}`)},
		}},
	}

	TrackEditedFiles(tracker, messages)

	recent := tracker.RecentFiles(5)
	if len(recent) != 2 {
		t.Fatalf("expected 2 tracked files, got %d", len(recent))
	}
}
