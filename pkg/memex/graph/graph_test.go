package graph

import (
	"context"
	"strings"
	"testing"
)

func TestOpen_RoundTrip(t *testing.T) {
	dir := t.TempDir()
	g, err := Open(Options{Dir: dir})
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	defer g.Close()

	ctx := context.Background()

	// Insert two memory nodes with a related_to edge.
	uids, err := g.Mutate(ctx, []byte(`
		_:a <memory.name> "alpha" .
		_:a <memory.type> "feedback" .
		_:a <memory.scope> "global" .
		_:a <dgraph.type> "Memory" .
		_:b <memory.name> "beta" .
		_:b <memory.type> "reference" .
		_:b <memory.scope> "global" .
		_:b <dgraph.type> "Memory" .
		_:a <memory.related_to> _:b .
	`))
	if err != nil {
		t.Fatalf("Mutate: %v", err)
	}
	if len(uids) < 2 {
		t.Fatalf("Mutate returned %d UIDs, want >=2", len(uids))
	}

	// Query for alpha and verify the related edge resolves.
	body, err := g.Query(ctx, `{
		q(func: eq(memory.name, "alpha")) {
			memory.name
			memory.type
			memory.related_to {
				memory.name
			}
		}
	}`, nil)
	if err != nil {
		t.Fatalf("Query: %v", err)
	}
	got := string(body)
	if !strings.Contains(got, `"alpha"`) {
		t.Errorf("query missing alpha: %s", got)
	}
	if !strings.Contains(got, `"beta"`) {
		t.Errorf("query missing related beta: %s", got)
	}
}

func TestOpen_EmptyDir(t *testing.T) {
	if _, err := Open(Options{}); err == nil {
		t.Errorf("Open with empty Dir should fail")
	}
}

func TestSchema_Versioned(t *testing.T) {
	if SchemaVersion == "" {
		t.Fatal("SchemaVersion is empty")
	}
	if !strings.Contains(Schema(), "memex graph schema v"+SchemaVersion) {
		t.Errorf("Schema text missing version marker")
	}
	if !strings.Contains(Schema(), "memory.name") {
		t.Errorf("Schema missing memory predicates")
	}
	if !strings.Contains(Schema(), "code.label") {
		t.Errorf("Schema missing code predicates")
	}
}
