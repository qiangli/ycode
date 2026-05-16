package loom

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestMemoryStore_PutGetDelete(t *testing.T) {
	s := NewMemoryStore()
	l := Lease{ID: "loom-abc", Branch: "agent/x/free-1"}
	if err := s.Put(l); err != nil {
		t.Fatalf("Put: %v", err)
	}
	got, ok := s.Get("loom-abc")
	if !ok || got.Branch != l.Branch {
		t.Fatalf("Get: ok=%v got=%+v", ok, got)
	}
	if err := s.Delete("loom-abc"); err != nil {
		t.Fatalf("Delete: %v", err)
	}
	if _, ok := s.Get("loom-abc"); ok {
		t.Fatal("Get after Delete should miss")
	}
}

func TestMemoryStore_PutEmptyID(t *testing.T) {
	s := NewMemoryStore()
	if err := s.Put(Lease{}); err == nil {
		t.Fatal("expected error on empty ID")
	}
}

func TestMemoryStore_ListSorted(t *testing.T) {
	s := NewMemoryStore()
	for _, id := range []string{"loom-c", "loom-a", "loom-b"} {
		if err := s.Put(Lease{ID: id}); err != nil {
			t.Fatalf("Put %s: %v", id, err)
		}
	}
	got := s.List()
	if len(got) != 3 {
		t.Fatalf("len=%d", len(got))
	}
	if got[0].ID != "loom-a" || got[1].ID != "loom-b" || got[2].ID != "loom-c" {
		t.Errorf("not sorted: %+v", got)
	}
}

func TestFileStore_RoundTrip(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "leases.json")

	fs1, err := NewFileStore(path)
	if err != nil {
		t.Fatalf("NewFileStore (empty): %v", err)
	}
	now := time.Now().UTC().Round(time.Second)
	want := Lease{
		ID:            "loom-deadbeef",
		Path:          "/tmp/sandbox",
		Branch:        "agent/agent-loom-test-aabbccdd/free-112233",
		CloneURL:      "http://127.0.0.1:1/admin/repo.git",
		AuthorName:    "agent-loom-test-aabbccdd",
		AuthorEmail:   "agent-loom-test-aabbccdd@ycode.local",
		Slug:          "repo",
		SubAgentLabel: "test",
		AgentID:       "agent-loom-test-aabbccdd",
		CreatedAt:     now,
		LastSeenAt:    now,
		ExpiresAt:     now.Add(time.Hour),
	}
	if err := fs1.Put(want); err != nil {
		t.Fatalf("Put: %v", err)
	}

	// Reload from disk in a fresh store; lease should round-trip.
	fs2, err := NewFileStore(path)
	if err != nil {
		t.Fatalf("NewFileStore (reload): %v", err)
	}
	got, ok := fs2.Get("loom-deadbeef")
	if !ok {
		t.Fatal("lease not found after reload")
	}
	if got.ID != want.ID || got.Branch != want.Branch || got.Slug != want.Slug {
		t.Errorf("lease mismatch: got=%+v want=%+v", got, want)
	}
	if !got.ExpiresAt.Equal(want.ExpiresAt) {
		t.Errorf("ExpiresAt mismatch: got=%v want=%v", got.ExpiresAt, want.ExpiresAt)
	}

	// Delete persists too.
	if err := fs2.Delete("loom-deadbeef"); err != nil {
		t.Fatalf("Delete: %v", err)
	}
	fs3, err := NewFileStore(path)
	if err != nil {
		t.Fatalf("NewFileStore (reload after delete): %v", err)
	}
	if _, ok := fs3.Get("loom-deadbeef"); ok {
		t.Fatal("delete did not persist")
	}
}

func TestFileStore_LoadCorrupt(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "leases.json")
	if err := writeFile(path, []byte("{not json")); err != nil {
		t.Fatal(err)
	}
	if _, err := NewFileStore(path); err == nil {
		t.Fatal("expected decode error on corrupt store")
	}
}

func TestFileStore_EmptyPath(t *testing.T) {
	if _, err := NewFileStore(""); err == nil {
		t.Fatal("expected error on empty path")
	}
}

func writeFile(path string, b []byte) error {
	return os.WriteFile(path, b, 0o644)
}
