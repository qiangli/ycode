package memory

import (
	"testing"
)

func TestExtractEntities_FilePaths(t *testing.T) {
	content := "Check the file /internal/runtime/memory/store.go and also ./cmd/ycode/main.go for details."
	entities := ExtractEntities(content)

	found := make(map[string]bool)
	for _, e := range entities {
		found[e.Name] = true
		if e.Type != "file_path" {
			t.Errorf("expected file_path type for %q, got %q", e.Name, e.Type)
		}
	}

	if !found["/internal/runtime/memory/store.go"] {
		t.Error("should extract /internal/runtime/memory/store.go")
	}
	if !found["./cmd/ycode/main.go"] {
		t.Error("should extract ./cmd/ycode/main.go")
	}
}

func TestExtractEntities_URLs(t *testing.T) {
	content := "See https://github.com/qiangli/ycode and http://localhost:8080/api for details."
	entities := ExtractEntities(content)

	found := make(map[string]bool)
	for _, e := range entities {
		if e.Type == "reference" {
			found[e.Name] = true
		}
	}

	if !found["https://github.com/qiangli/ycode"] {
		t.Error("should extract GitHub URL")
	}
	if !found["http://localhost:8080/api"] {
		t.Error("should extract localhost URL")
	}
}

func TestExtractEntities_GoPackages(t *testing.T) {
	content := `Import "github.com/qiangli/ycode/internal/tools" for tool registration.`
	entities := ExtractEntities(content)

	found := false
	for _, e := range entities {
		if e.Type == "technology" && e.Name == "github.com/qiangli/ycode/internal/tools" {
			found = true
		}
	}
	if !found {
		t.Error("should extract Go package path")
	}
}

func TestExtractEntities_NoDuplicates(t *testing.T) {
	content := "Use /internal/memory/store.go and /internal/memory/store.go again."
	entities := ExtractEntities(content)

	names := make(map[string]int)
	for _, e := range entities {
		names[e.Name]++
	}
	for name, count := range names {
		if count > 1 {
			t.Errorf("entity %q extracted %d times, should be unique", name, count)
		}
	}
}

func TestExtractEntities_EmptyContent(t *testing.T) {
	entities := ExtractEntities("")
	if len(entities) != 0 {
		t.Errorf("expected 0 entities from empty content, got %d", len(entities))
	}
}

func TestEntityIndex_LinkAndFind(t *testing.T) {
	ei := NewEntityIndex()

	ei.Link(Entity{Name: "ycode", Type: "project"}, "mem-1")
	ei.Link(Entity{Name: "ycode", Type: "project"}, "mem-2")
	ei.Link(Entity{Name: "Go", Type: "technology"}, "mem-1")

	// ycode should link to both memories.
	refs := ei.FindMemories("ycode")
	if len(refs) != 2 {
		t.Errorf("expected 2 refs for ycode, got %d", len(refs))
	}

	// Go should link to one memory.
	refs = ei.FindMemories("Go")
	if len(refs) != 1 {
		t.Errorf("expected 1 ref for Go, got %d", len(refs))
	}

	// Case insensitive lookup.
	refs = ei.FindMemories("YCODE")
	if len(refs) != 2 {
		t.Errorf("case-insensitive: expected 2 refs, got %d", len(refs))
	}
}

func TestEntityIndex_Unlink(t *testing.T) {
	ei := NewEntityIndex()

	ei.Link(Entity{Name: "ycode", Type: "project"}, "mem-1")
	ei.Link(Entity{Name: "ycode", Type: "project"}, "mem-2")

	ei.Unlink("mem-1")

	refs := ei.FindMemories("ycode")
	if len(refs) != 1 {
		t.Errorf("after unlink: expected 1 ref, got %d", len(refs))
	}
	if refs[0] != "mem-2" {
		t.Errorf("expected mem-2, got %q", refs[0])
	}
}

func TestEntityIndex_UnlinkRemovesOrphan(t *testing.T) {
	ei := NewEntityIndex()

	ei.Link(Entity{Name: "orphan", Type: "concept"}, "mem-1")
	ei.Unlink("mem-1")

	refs := ei.FindMemories("orphan")
	if len(refs) != 0 {
		t.Errorf("orphan entity should be removed, got %d refs", len(refs))
	}
}

func TestEntityIndex_FindRelated(t *testing.T) {
	ei := NewEntityIndex()

	ei.Link(Entity{Name: "auth", Type: "concept"}, "mem-1")
	ei.Link(Entity{Name: "auth", Type: "concept"}, "mem-2")
	ei.Link(Entity{Name: "auth", Type: "concept"}, "mem-3")
	ei.Link(Entity{Name: "api", Type: "concept"}, "mem-1")
	ei.Link(Entity{Name: "api", Type: "concept"}, "mem-3")

	related := ei.FindRelated("mem-1")
	// mem-3 shares both entities, mem-2 shares one.
	if len(related) < 2 {
		t.Fatalf("expected at least 2 related memories, got %d", len(related))
	}
	// mem-3 should rank first (2 shared entities).
	if related[0] != "mem-3" {
		t.Errorf("expected mem-3 first (most shared entities), got %q", related[0])
	}
}

func TestEntityIndex_SearchMemories(t *testing.T) {
	ei := NewEntityIndex()

	memByName := map[string]*Memory{
		"mem-1": {Name: "mem-1", Description: "auth config"},
		"mem-2": {Name: "mem-2", Description: "deploy config"},
		"mem-3": {Name: "mem-3", Description: "auth flow"},
	}

	ei.Link(Entity{Name: "authentication", Type: "concept"}, "mem-1")
	ei.Link(Entity{Name: "authentication", Type: "concept"}, "mem-3")
	ei.Link(Entity{Name: "deployment", Type: "concept"}, "mem-2")

	results := ei.SearchMemories("authentication", memByName, 10)
	if len(results) != 2 {
		t.Fatalf("expected 2 results for 'authentication', got %d", len(results))
	}

	// Both should be linked to auth entity.
	names := make(map[string]bool)
	for _, r := range results {
		names[r.Memory.Name] = true
		if r.Source != "entity" {
			t.Errorf("source should be 'entity', got %q", r.Source)
		}
	}
	if !names["mem-1"] || !names["mem-3"] {
		t.Error("should find mem-1 and mem-3 via authentication entity")
	}
}

func TestEntityIndex_SearchMemories_EmptyQuery(t *testing.T) {
	ei := NewEntityIndex()
	results := ei.SearchMemories("", nil, 10)
	if len(results) != 0 {
		t.Errorf("empty query should return 0 results, got %d", len(results))
	}
}

func TestEntityIndex_DuplicateLinkIgnored(t *testing.T) {
	ei := NewEntityIndex()

	ei.Link(Entity{Name: "test", Type: "concept"}, "mem-1")
	ei.Link(Entity{Name: "test", Type: "concept"}, "mem-1") // duplicate

	refs := ei.FindMemories("test")
	if len(refs) != 1 {
		t.Errorf("duplicate link should be ignored, got %d refs", len(refs))
	}
}

func TestEntityIntegration_SaveExtractsEntities(t *testing.T) {
	dir := t.TempDir()
	mgr, err := NewManager(dir)
	if err != nil {
		t.Fatalf("new manager: %v", err)
	}

	ei := NewEntityIndex()
	mgr.SetEntityIndex(ei)

	mem := &Memory{
		Name:        "entity-test",
		Description: "memory with file paths",
		Type:        TypeProject,
		Content:     "Check /internal/runtime/memory/store.go for the implementation.",
	}
	if err := mgr.Save(mem); err != nil {
		t.Fatalf("save: %v", err)
	}

	// Entity should be extracted and linked.
	refs := ei.FindMemories("/internal/runtime/memory/store.go")
	if len(refs) == 0 {
		t.Error("entity should be linked to memory after save")
	}

	// Memory should have cached entities.
	if len(mem.Entities) == 0 {
		t.Error("memory should have cached entities after save")
	}
}
