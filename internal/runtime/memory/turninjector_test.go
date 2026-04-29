package memory

import (
	"context"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestTurnInjector_BasicInjection(t *testing.T) {
	dir := t.TempDir()
	mgr, err := NewManager(dir)
	if err != nil {
		t.Fatalf("new manager: %v", err)
	}

	// Save some memories.
	mgr.Save(&Memory{
		Name:        "deploy-info",
		Description: "deployment configuration for production",
		Type:        TypeProject,
		Content:     "Deploy to staging first, then promote to prod.",
		FilePath:    filepath.Join(dir, "deploy-info.md"),
		CreatedAt:   time.Now(),
		UpdatedAt:   time.Now(),
	})

	ti := NewTurnInjector(mgr, 1500)
	result := ti.InjectForTurn(context.Background(), "How do I deploy to production?")

	if result == "" {
		t.Fatal("expected non-empty injection for matching query")
	}
	if !strings.Contains(result, "<memory-context>") {
		t.Error("injection should contain memory-context tag")
	}
	if !strings.Contains(result, "deploy-info") {
		t.Error("injection should contain matching memory name")
	}
}

func TestTurnInjector_EmptyMessage(t *testing.T) {
	dir := t.TempDir()
	mgr, _ := NewManager(dir)
	ti := NewTurnInjector(mgr, 1500)

	result := ti.InjectForTurn(context.Background(), "")
	if result != "" {
		t.Errorf("empty message should return empty result, got %q", result)
	}
}

func TestTurnInjector_NoMatchingMemories(t *testing.T) {
	dir := t.TempDir()
	mgr, _ := NewManager(dir)
	ti := NewTurnInjector(mgr, 1500)

	result := ti.InjectForTurn(context.Background(), "completely unrelated query about quantum physics")
	if result != "" {
		t.Errorf("no matching memories should return empty result, got length %d", len(result))
	}
}

func TestTurnInjector_DedupSimilarQueries(t *testing.T) {
	dir := t.TempDir()
	mgr, _ := NewManager(dir)
	mgr.Save(&Memory{
		Name:        "auth-info",
		Description: "authentication configuration",
		Type:        TypeProject,
		Content:     "OAuth2 flow for login.",
		FilePath:    filepath.Join(dir, "auth-info.md"),
		CreatedAt:   time.Now(),
		UpdatedAt:   time.Now(),
	})

	ti := NewTurnInjector(mgr, 1500)

	// First query should produce results.
	result1 := ti.InjectForTurn(context.Background(), "how does authentication work?")
	if result1 == "" {
		t.Fatal("first query should produce results")
	}

	// Very similar query should be deduped.
	result2 := ti.InjectForTurn(context.Background(), "how does authentication work here?")
	if result2 != "" {
		t.Error("very similar follow-up should be deduped (empty result)")
	}

	// Different query should produce results again.
	result3 := ti.InjectForTurn(context.Background(), "what is the deploy target?")
	// This might or might not match, but it shouldn't be deduped.
	_ = result3
}

func TestTurnInjector_BudgetRespected(t *testing.T) {
	dir := t.TempDir()
	mgr, _ := NewManager(dir)

	// Save several long memories.
	for i := 0; i < 5; i++ {
		mgr.Save(&Memory{
			Name:        "long-mem",
			Description: "long memory",
			Type:        TypeProject,
			Content:     strings.Repeat("content with many words about deployment and configuration ", 20),
			CreatedAt:   time.Now(),
			UpdatedAt:   time.Now(),
		})
	}

	// Very small budget.
	ti := NewTurnInjector(mgr, 100)
	result := ti.InjectForTurn(context.Background(), "deployment configuration")
	// Should either be empty or very short.
	if len(result) > 200 { // some overhead for tags
		t.Errorf("result length %d exceeds budget expectations", len(result))
	}
}

func TestWordSet(t *testing.T) {
	set := wordSet("Hello World hello")
	if len(set) != 2 {
		t.Errorf("expected 2 unique words, got %d", len(set))
	}
	if _, ok := set["hello"]; !ok {
		t.Error("should contain 'hello' (lowercased)")
	}
	if _, ok := set["world"]; !ok {
		t.Error("should contain 'world'")
	}
}
