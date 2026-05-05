package memory

import (
	"context"
	"path/filepath"
	"testing"
)

func TestNewFileProvider(t *testing.T) {
	dir := t.TempDir()
	fp, err := NewFileProvider(dir)
	if err != nil {
		t.Fatalf("NewFileProvider: %v", err)
	}
	if fp == nil {
		t.Fatal("expected non-nil FileProvider")
	}
	if fp.Dir() != dir {
		t.Errorf("Dir() = %q, want %q", fp.Dir(), dir)
	}
}

func TestFileProvider_SaveLoadRoundtrip(t *testing.T) {
	dir := t.TempDir()
	fp, err := NewFileProvider(dir)
	if err != nil {
		t.Fatalf("NewFileProvider: %v", err)
	}

	mem := &Memory{
		Name:        "provider-test",
		Description: "testing the provider interface",
		Type:        TypeUser,
		Content:     "Provider round-trip content",
		FilePath:    filepath.Join(dir, "provider-test.md"),
	}
	if err := fp.Save(mem); err != nil {
		t.Fatalf("Save: %v", err)
	}

	loaded, err := fp.Load(mem.FilePath)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if loaded.Name != mem.Name {
		t.Errorf("Name: got %q, want %q", loaded.Name, mem.Name)
	}
	if loaded.Description != mem.Description {
		t.Errorf("Description: got %q, want %q", loaded.Description, mem.Description)
	}
	if loaded.Type != mem.Type {
		t.Errorf("Type: got %q, want %q", loaded.Type, mem.Type)
	}
	if loaded.Content != mem.Content {
		t.Errorf("Content: got %q, want %q", loaded.Content, mem.Content)
	}
}

func TestFileProvider_List(t *testing.T) {
	dir := t.TempDir()
	fp, err := NewFileProvider(dir)
	if err != nil {
		t.Fatalf("NewFileProvider: %v", err)
	}

	// Empty store.
	memories, err := fp.List()
	if err != nil {
		t.Fatalf("List empty: %v", err)
	}
	if len(memories) != 0 {
		t.Errorf("expected 0 memories, got %d", len(memories))
	}

	// Save two memories.
	for _, name := range []string{"alpha", "beta"} {
		if err := fp.Save(&Memory{
			Name:    name,
			Type:    TypeProject,
			Content: name + " content",
		}); err != nil {
			t.Fatalf("Save %s: %v", name, err)
		}
	}

	memories, err = fp.List()
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	if len(memories) != 2 {
		t.Errorf("expected 2 memories, got %d", len(memories))
	}
}

func TestFileProvider_Delete(t *testing.T) {
	dir := t.TempDir()
	fp, err := NewFileProvider(dir)
	if err != nil {
		t.Fatalf("NewFileProvider: %v", err)
	}

	mem := &Memory{
		Name:     "to-delete",
		Type:     TypeFeedback,
		Content:  "ephemeral",
		FilePath: filepath.Join(dir, "to-delete.md"),
	}
	if err := fp.Save(mem); err != nil {
		t.Fatalf("Save: %v", err)
	}

	if err := fp.Delete(mem.FilePath); err != nil {
		t.Fatalf("Delete: %v", err)
	}

	memories, err := fp.List()
	if err != nil {
		t.Fatalf("List after delete: %v", err)
	}
	if len(memories) != 0 {
		t.Errorf("expected 0 memories after delete, got %d", len(memories))
	}
}

func TestFileProvider_Search(t *testing.T) {
	dir := t.TempDir()
	fp, err := NewFileProvider(dir)
	if err != nil {
		t.Fatalf("NewFileProvider: %v", err)
	}

	mems := []Memory{
		{Name: "go-preferences", Description: "User prefers Go", Type: TypeUser, Content: "Always use Go"},
		{Name: "deploy-config", Description: "Deployment settings", Type: TypeProject, Content: "k8s cluster info"},
		{Name: "api-notes", Description: "API design notes", Type: TypeReference, Content: "REST endpoints"},
	}
	for i := range mems {
		if err := fp.Save(&mems[i]); err != nil {
			t.Fatalf("Save %s: %v", mems[i].Name, err)
		}
	}

	ctx := context.Background()

	// Search by name keyword.
	results, err := fp.Search(ctx, "deploy", 10)
	if err != nil {
		t.Fatalf("Search: %v", err)
	}
	if len(results) != 1 {
		t.Fatalf("expected 1 result for 'deploy', got %d", len(results))
	}
	if results[0].Name != "deploy-config" {
		t.Errorf("expected deploy-config, got %q", results[0].Name)
	}

	// Search by content keyword.
	results, err = fp.Search(ctx, "REST", 10)
	if err != nil {
		t.Fatalf("Search REST: %v", err)
	}
	if len(results) != 1 {
		t.Fatalf("expected 1 result for 'REST', got %d", len(results))
	}

	// Search by description keyword.
	results, err = fp.Search(ctx, "prefers", 10)
	if err != nil {
		t.Fatalf("Search prefers: %v", err)
	}
	if len(results) != 1 {
		t.Fatalf("expected 1 result for 'prefers', got %d", len(results))
	}

	// Search respects maxResults.
	results, err = fp.Search(ctx, "o", 2)
	if err != nil {
		t.Fatalf("Search 'o': %v", err)
	}
	if len(results) > 2 {
		t.Errorf("expected at most 2 results, got %d", len(results))
	}
}

func TestFileProvider_SearchNoMatches(t *testing.T) {
	dir := t.TempDir()
	fp, err := NewFileProvider(dir)
	if err != nil {
		t.Fatalf("NewFileProvider: %v", err)
	}

	fp.Save(&Memory{Name: "alpha", Type: TypeUser, Content: "content"})

	results, err := fp.Search(context.Background(), "zzzznonexistent", 10)
	if err != nil {
		t.Fatalf("Search: %v", err)
	}
	if len(results) != 0 {
		t.Errorf("expected 0 results, got %d", len(results))
	}
}

func TestFileProvider_LifecycleHooksNoOp(t *testing.T) {
	dir := t.TempDir()
	fp, err := NewFileProvider(dir)
	if err != nil {
		t.Fatalf("NewFileProvider: %v", err)
	}

	ctx := context.Background()
	mem := &Memory{Name: "test", Type: TypeUser, Content: "content"}

	if err := fp.OnTurnStart(ctx, 1000); err != nil {
		t.Errorf("OnTurnStart: %v", err)
	}
	if err := fp.OnPreCompress(ctx); err != nil {
		t.Errorf("OnPreCompress: %v", err)
	}
	if err := fp.OnMemoryWrite(ctx, mem); err != nil {
		t.Errorf("OnMemoryWrite: %v", err)
	}
	if err := fp.OnDelegation(ctx, "research", "some result"); err != nil {
		t.Errorf("OnDelegation: %v", err)
	}
	if err := fp.OnSessionEnd(ctx); err != nil {
		t.Errorf("OnSessionEnd: %v", err)
	}
}

// Verify FileProvider satisfies the MemoryProvider interface at compile time.
var _ MemoryProvider = (*FileProvider)(nil)
