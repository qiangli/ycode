package search

import (
	"context"
	"testing"

	"github.com/qiangli/ycode/internal/storage"
)

func TestStore(t *testing.T) {
	dir := t.TempDir()

	s, err := Open(dir)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	defer s.Close()

	ctx := context.Background()

	t.Run("IndexAndSearch", func(t *testing.T) {
		docs := []storage.Document{
			{ID: "1", Content: "function handleLogin validates user credentials", Metadata: map[string]string{"path": "auth.go", "language": "go"}},
			{ID: "2", Content: "function renderDashboard displays user metrics", Metadata: map[string]string{"path": "ui.go", "language": "go"}},
			{ID: "3", Content: "database migration creates users table", Metadata: map[string]string{"path": "migrate.go", "language": "go"}},
		}

		if err := s.BatchIndex(ctx, "code", docs); err != nil {
			t.Fatalf("BatchIndex: %v", err)
		}

		results, err := s.Search(ctx, "code", "user login credentials", 10)
		if err != nil {
			t.Fatalf("Search: %v", err)
		}
		if len(results) == 0 {
			t.Fatal("Search returned no results")
		}
		// The login handler should be the top result.
		if results[0].Document.ID != "1" {
			t.Errorf("top result ID = %q, want %q", results[0].Document.ID, "1")
		}
	})

	t.Run("IndexSingle", func(t *testing.T) {
		doc := storage.Document{
			ID:      "single-1",
			Content: "unique snowflake document about quantum computing",
		}
		if err := s.Index(ctx, "singles", doc); err != nil {
			t.Fatalf("Index: %v", err)
		}
		results, err := s.Search(ctx, "singles", "quantum", 5)
		if err != nil {
			t.Fatalf("Search: %v", err)
		}
		if len(results) != 1 {
			t.Fatalf("Search results = %d, want 1", len(results))
		}
		if results[0].Document.ID != "single-1" {
			t.Errorf("result ID = %q, want %q", results[0].Document.ID, "single-1")
		}
	})

	t.Run("Delete", func(t *testing.T) {
		doc := storage.Document{ID: "del-1", Content: "deletable content about testing"}
		s.Index(ctx, "deltest", doc)

		if err := s.Delete(ctx, "deltest", "del-1"); err != nil {
			t.Fatalf("Delete: %v", err)
		}

		results, err := s.Search(ctx, "deltest", "deletable testing", 5)
		if err != nil {
			t.Fatalf("Search after delete: %v", err)
		}
		if len(results) != 0 {
			t.Errorf("results after delete = %d, want 0", len(results))
		}
	})

	t.Run("SearchWithFilter", func(t *testing.T) {
		docs := []storage.Document{
			{ID: "f1", Content: "handleAuth function validates user", Metadata: map[string]string{"path": "auth.go", "language": "go"}},
			{ID: "f2", Content: "handleAuth function validates user", Metadata: map[string]string{"path": "auth.py", "language": "py"}},
			{ID: "f3", Content: "renderPage function draws UI", Metadata: map[string]string{"path": "ui.go", "language": "go"}},
		}
		if err := s.BatchIndex(ctx, "filtered", docs); err != nil {
			t.Fatalf("BatchIndex: %v", err)
		}

		// Search with language filter = go.
		results, err := s.SearchWithFilter(ctx, "filtered", "handleAuth", map[string]string{"language": "go"}, 10)
		if err != nil {
			t.Fatalf("SearchWithFilter: %v", err)
		}
		if len(results) != 1 {
			t.Fatalf("SearchWithFilter results = %d, want 1", len(results))
		}
		if results[0].Document.ID != "f1" {
			t.Errorf("result ID = %q, want %q", results[0].Document.ID, "f1")
		}

		// Empty filters should behave like regular Search.
		results, err = s.SearchWithFilter(ctx, "filtered", "handleAuth", nil, 10)
		if err != nil {
			t.Fatalf("SearchWithFilter nil filters: %v", err)
		}
		if len(results) != 2 {
			t.Fatalf("SearchWithFilter nil filters results = %d, want 2", len(results))
		}
	})

	t.Run("DeleteIndex", func(t *testing.T) {
		doc := storage.Document{ID: "di-1", Content: "entire index deletion"}
		s.Index(ctx, "todelete", doc)

		if err := s.DeleteIndex(ctx, "todelete"); err != nil {
			t.Fatalf("DeleteIndex: %v", err)
		}

		// Searching a deleted index should create a new empty one.
		results, err := s.Search(ctx, "todelete", "deletion", 5)
		if err != nil {
			t.Fatalf("Search after DeleteIndex: %v", err)
		}
		if len(results) != 0 {
			t.Errorf("results after DeleteIndex = %d, want 0", len(results))
		}
	})
}
