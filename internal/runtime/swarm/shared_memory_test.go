package swarm

import (
	"strings"
	"testing"
)

func TestAddAndAll(t *testing.T) {
	sv := NewSharedMemoryView(10)

	sv.Add(SharedMemoryEntry{Name: "e1", Type: "fact", Content: "content1", Source: "agent-a"})
	sv.Add(SharedMemoryEntry{Name: "e2", Type: "fact", Content: "content2", Source: "agent-b"})

	all := sv.All()
	if len(all) != 2 {
		t.Fatalf("all = %d, want 2", len(all))
	}
	if all[0].Name != "e1" || all[1].Name != "e2" {
		t.Fatalf("entries = %v", all)
	}
}

func TestDeduplicationByName(t *testing.T) {
	sv := NewSharedMemoryView(10)

	sv.Add(SharedMemoryEntry{Name: "key", Content: "old"})
	sv.Add(SharedMemoryEntry{Name: "key", Content: "new"})

	if sv.Len() != 1 {
		t.Fatalf("len = %d, want 1 (dedup)", sv.Len())
	}
	all := sv.All()
	if all[0].Content != "new" {
		t.Fatalf("content = %q, want new", all[0].Content)
	}
}

func TestEvictionOverCapacity(t *testing.T) {
	sv := NewSharedMemoryView(3)

	sv.Add(SharedMemoryEntry{Name: "a", Content: "1"})
	sv.Add(SharedMemoryEntry{Name: "b", Content: "2"})
	sv.Add(SharedMemoryEntry{Name: "c", Content: "3"})
	sv.Add(SharedMemoryEntry{Name: "d", Content: "4"})

	if sv.Len() != 3 {
		t.Fatalf("len = %d, want 3 after eviction", sv.Len())
	}
	all := sv.All()
	// "a" should have been evicted (oldest).
	for _, e := range all {
		if e.Name == "a" {
			t.Fatal("oldest entry 'a' should have been evicted")
		}
	}
}

func TestSearch(t *testing.T) {
	sv := NewSharedMemoryView(10)
	sv.Add(SharedMemoryEntry{Name: "golang", Content: "Go is a programming language"})
	sv.Add(SharedMemoryEntry{Name: "python", Content: "Python is a scripting language"})
	sv.Add(SharedMemoryEntry{Name: "rust", Content: "Rust is a systems language"})

	results := sv.Search("go")
	if len(results) != 1 {
		t.Fatalf("search results = %d, want 1", len(results))
	}
	if results[0].Name != "golang" {
		t.Fatalf("result name = %q", results[0].Name)
	}

	// Search by name.
	results = sv.Search("python")
	if len(results) != 1 {
		t.Fatalf("search by name = %d, want 1", len(results))
	}

	// Case insensitive.
	results = sv.Search("RUST")
	if len(results) != 1 {
		t.Fatalf("case insensitive search = %d, want 1", len(results))
	}

	// No match.
	results = sv.Search("java")
	if len(results) != 0 {
		t.Fatalf("no match search = %d, want 0", len(results))
	}
}

func TestSearchBroadMatch(t *testing.T) {
	sv := NewSharedMemoryView(10)
	sv.Add(SharedMemoryEntry{Name: "item1", Content: "language features"})
	sv.Add(SharedMemoryEntry{Name: "item2", Content: "language design"})

	results := sv.Search("language")
	if len(results) != 2 {
		t.Fatalf("broad search = %d, want 2", len(results))
	}
}

func TestLen(t *testing.T) {
	sv := NewSharedMemoryView(10)
	if sv.Len() != 0 {
		t.Fatal("empty len should be 0")
	}
	sv.Add(SharedMemoryEntry{Name: "a"})
	if sv.Len() != 1 {
		t.Fatalf("len = %d, want 1", sv.Len())
	}
}

func TestNewSharedMemoryViewZeroCapacity(t *testing.T) {
	sv := NewSharedMemoryView(0)
	// Should default to 50.
	for i := 0; i < 55; i++ {
		sv.Add(SharedMemoryEntry{Name: string(rune('A' + i))})
	}
	if sv.Len() != 50 {
		t.Fatalf("len = %d, want 50 (default capacity)", sv.Len())
	}
}

func TestFormatForPrompt(t *testing.T) {
	sv := NewSharedMemoryView(10)

	// Empty returns empty.
	if sv.FormatForPrompt() != "" {
		t.Fatal("empty should return empty string")
	}

	sv.Add(SharedMemoryEntry{Name: "finding", Content: "important data", Source: "agent-1"})

	output := sv.FormatForPrompt()
	if !strings.Contains(output, "## Shared Swarm Memory") {
		t.Fatal("should contain header")
	}
	if !strings.Contains(output, "finding") {
		t.Fatal("should contain entry name")
	}
	if !strings.Contains(output, "important data") {
		t.Fatal("should contain content")
	}
	if !strings.Contains(output, "agent-1") {
		t.Fatal("should contain source")
	}
}

func TestFormatForPromptNoSource(t *testing.T) {
	sv := NewSharedMemoryView(10)
	sv.Add(SharedMemoryEntry{Name: "item", Content: "data"})

	output := sv.FormatForPrompt()
	if strings.Contains(output, "(from:") {
		t.Fatal("should not contain source annotation when empty")
	}
}
