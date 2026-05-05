package memex_test

import (
	"context"
	"strings"
	"testing"

	"github.com/qiangli/ycode/pkg/memex"
)

func TestMemoryGraph_DualWriteToBonsai(t *testing.T) {
	dir := t.TempDir()
	mx, err := memex.Open(dir)
	if err != nil {
		t.Fatalf("memex.Open: %v", err)
	}
	defer mx.Close()

	mg := mx.MemoryGraph()
	if mg == nil {
		t.Fatal("MemoryGraph is nil")
	}

	if err := mg.AddEdge("alpha-decision", "beta-feedback", "supersedes", 1.0); err != nil {
		t.Fatalf("AddEdge: %v", err)
	}
	if err := mg.AddEdge("alpha-decision", "auth-context", "related_to", 0.7); err != nil {
		t.Fatalf("AddEdge: %v", err)
	}

	body, err := mg.Query(context.Background(), `{
		q(func: eq(memory.name, "alpha-decision")) {
			memory.name
			memory.supersedes { memory.name }
			memory.related_to { memory.name }
		}
	}`, nil)
	if err != nil {
		t.Fatalf("MemoryGraph.Query: %v", err)
	}

	got := string(body)
	for _, want := range []string{
		`"alpha-decision"`,
		`"beta-feedback"`,
		`"auth-context"`,
	} {
		if !strings.Contains(got, want) {
			t.Errorf("response missing %q\nresponse: %s", want, got)
		}
	}
}
