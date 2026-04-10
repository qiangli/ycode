package memory

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestMemoryManager_SaveRecallForget(t *testing.T) {
	dir := t.TempDir()
	mgr, err := NewManager(dir)
	if err != nil {
		t.Fatalf("new manager: %v", err)
	}

	// Save a memory.
	mem := &Memory{
		Name:        "test-memory",
		Description: "a test memory for unit testing",
		Type:        TypeUser,
		Content:     "User prefers Go over Rust",
		FilePath:    filepath.Join(dir, "test-memory.md"),
		CreatedAt:   time.Now(),
		UpdatedAt:   time.Now(),
	}
	if err := mgr.Save(mem); err != nil {
		t.Fatalf("save: %v", err)
	}

	// Recall.
	results, err := mgr.Recall("test", 5)
	if err != nil {
		t.Fatalf("recall: %v", err)
	}
	if len(results) == 0 {
		t.Fatal("expected at least one recall result")
	}

	// All.
	all, err := mgr.All()
	if err != nil {
		t.Fatalf("all: %v", err)
	}
	if len(all) != 1 {
		t.Fatalf("expected 1 memory, got %d", len(all))
	}

	// Forget.
	if err := mgr.Forget("test-memory"); err != nil {
		t.Fatalf("forget: %v", err)
	}

	all, _ = mgr.All()
	if len(all) != 0 {
		t.Errorf("expected 0 memories after forget, got %d", len(all))
	}
}

func TestMemoryManager_Index(t *testing.T) {
	dir := t.TempDir()
	mgr, err := NewManager(dir)
	if err != nil {
		t.Fatalf("new manager: %v", err)
	}

	mem := &Memory{
		Name:        "indexed-mem",
		Description: "memory with index entry",
		Type:        TypeProject,
		Content:     "Some project info",
		FilePath:    filepath.Join(dir, "indexed-mem.md"),
		CreatedAt:   time.Now(),
		UpdatedAt:   time.Now(),
	}
	if err := mgr.Save(mem); err != nil {
		t.Fatalf("save: %v", err)
	}

	// Check index.
	indexContent, err := mgr.ReadIndex()
	if err != nil {
		t.Fatalf("read index: %v", err)
	}
	if indexContent == "" {
		t.Error("index should not be empty after save")
	}
}

func TestStaleness(t *testing.T) {
	recent := &Memory{
		Type:      TypeProject,
		UpdatedAt: time.Now(),
	}
	if IsStale(recent) {
		t.Error("recent memory should not be stale")
	}

	old := &Memory{
		Type:      TypeProject,
		UpdatedAt: time.Now().Add(-60 * 24 * time.Hour),
	}
	if !IsStale(old) {
		t.Error("old memory should be stale")
	}
}

func TestDecayScore(t *testing.T) {
	fresh := &Memory{UpdatedAt: time.Now()}
	score := DecayScore(1.0, fresh)
	if score != 1.0 {
		t.Errorf("fresh memory should have no decay, got %f", score)
	}

	old := &Memory{UpdatedAt: time.Now().Add(-90 * 24 * time.Hour)}
	decayed := DecayScore(1.0, old)
	if decayed >= 1.0 {
		t.Errorf("old memory should have decayed score, got %f", decayed)
	}
}

func TestSearch(t *testing.T) {
	memories := []*Memory{
		{Name: "api-config", Description: "API configuration details", Type: TypeReference},
		{Name: "user-pref", Description: "user prefers dark mode", Type: TypeUser},
		{Name: "project-goal", Description: "project goal is to build a CLI tool", Type: TypeProject},
	}

	results := Search(memories, "API", 5)
	if len(results) == 0 {
		t.Fatal("expected at least one search result for 'API'")
	}
	// The API config memory should score highest.
	if results[0].Memory.Name != "api-config" {
		t.Errorf("expected api-config first, got %s", results[0].Memory.Name)
	}
}

func TestMemoryFileRoundtrip(t *testing.T) {
	dir := t.TempDir()
	store, err := NewStore(dir)
	if err != nil {
		t.Fatalf("new store: %v", err)
	}

	mem := &Memory{
		Name:        "roundtrip",
		Description: "test roundtrip",
		Type:        TypeFeedback,
		Content:     "This is test content\nwith multiple lines",
		FilePath:    filepath.Join(dir, "roundtrip.md"),
		CreatedAt:   time.Now(),
		UpdatedAt:   time.Now(),
	}

	if err := store.Save(mem); err != nil {
		t.Fatalf("save: %v", err)
	}

	// Verify file exists.
	if _, err := os.Stat(mem.FilePath); err != nil {
		t.Fatalf("file should exist: %v", err)
	}

	// List and verify.
	memories, err := store.List()
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	if len(memories) != 1 {
		t.Fatalf("expected 1 memory, got %d", len(memories))
	}
	if memories[0].Name != "roundtrip" {
		t.Errorf("expected name 'roundtrip', got %q", memories[0].Name)
	}
}
