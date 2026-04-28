package indexer

import (
	"os"
	"path/filepath"
	"sort"
	"testing"

	"github.com/qiangli/ycode/internal/storage/kv"
)

func TestTrigramIndex_BasicSearch(t *testing.T) {
	dir := t.TempDir()

	kvStore, err := kv.Open(filepath.Join(dir, "test.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer kvStore.Close()

	ti := NewTrigramIndex(kvStore)

	// Create test files.
	files := map[string]string{
		"auth.go":    "func handleAuthentication(user string) error {\n\treturn nil\n}",
		"handler.go": "func handleRequest(req *http.Request) {\n\t// process\n}",
		"utils.go":   "func formatDate(t time.Time) string {\n\treturn t.Format(time.RFC3339)\n}",
	}

	for name, content := range files {
		path := filepath.Join(dir, name)
		if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
			t.Fatal(err)
		}
		ti.IndexFile(path, name)
	}

	// Query for "handle" — should match auth.go and handler.go.
	results := ti.QueryPattern("handle")
	sort.Strings(results)

	if len(results) != 2 {
		t.Fatalf("expected 2 results for 'handle', got %d: %v", len(results), results)
	}

	// Query for "Authentication" — should match only auth.go.
	results = ti.QueryPattern("Authentication")
	if len(results) != 1 {
		t.Fatalf("expected 1 result for 'Authentication', got %d: %v", len(results), results)
	}
	if results[0] != "auth.go" {
		t.Errorf("expected auth.go, got %s", results[0])
	}

	// Query for "zzz_nonexistent" — should match nothing.
	results = ti.QueryPattern("zzz_nonexistent")
	if len(results) != 0 {
		t.Errorf("expected 0 results for nonexistent, got %d", len(results))
	}
}

func TestTrigramIndex_Stats(t *testing.T) {
	dir := t.TempDir()

	kvStore, err := kv.Open(filepath.Join(dir, "test.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer kvStore.Close()

	ti := NewTrigramIndex(kvStore)

	path := filepath.Join(dir, "test.go")
	if err := os.WriteFile(path, []byte("func main() {}"), 0o644); err != nil {
		t.Fatal(err)
	}
	ti.IndexFile(path, "test.go")

	trigrams, entries := ti.Stats()
	if trigrams == 0 {
		t.Error("expected non-zero trigrams")
	}
	if entries == 0 {
		t.Error("expected non-zero entries")
	}
}

func TestTrigramIndex_NilSafe(t *testing.T) {
	var ti *TrigramIndex

	ti.IndexFile("", "")
	results := ti.QueryPattern("test")
	if results != nil {
		t.Error("expected nil from nil index")
	}
	tri, ent := ti.Stats()
	if tri != 0 || ent != 0 {
		t.Error("expected zero stats from nil index")
	}
}

func TestExtractTrigrams(t *testing.T) {
	trigrams := extractTrigrams("hello world")
	if len(trigrams) == 0 {
		t.Error("expected non-zero trigrams")
	}

	// "hel", "ell", "llo", "lo ", "o w", " wo", "wor", "orl", "rld"
	seen := make(map[string]bool)
	for _, tri := range trigrams {
		seen[tri] = true
	}
	if !seen["hel"] {
		t.Error("missing trigram 'hel'")
	}
	if !seen["wor"] {
		t.Error("missing trigram 'wor'")
	}
}
