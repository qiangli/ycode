package indexer

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/qiangli/ycode/internal/storage/kv"
)

func TestRefGraph_GoFile(t *testing.T) {
	dir := t.TempDir()

	// Create a Go file with function calls.
	src := `package main

import "fmt"

func main() {
	result := compute(42)
	fmt.Println(result)
}

func compute(x int) int {
	return helper(x) + 1
}

func helper(x int) int {
	return x * 2
}
`
	path := filepath.Join(dir, "main.go")
	if err := os.WriteFile(path, []byte(src), 0o644); err != nil {
		t.Fatal(err)
	}

	// Open a KV store.
	kvStore, err := kv.Open(filepath.Join(dir, "test.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer kvStore.Close()

	g := NewRefGraph(kvStore)
	g.IndexFileReferences(path, "main.go")

	// Test FindCallers: who calls compute?
	callers := g.FindCallers("compute")
	if len(callers) == 0 {
		t.Error("expected callers of compute, got none")
	}
	found := false
	for _, c := range callers {
		if c == "main.main" {
			found = true
		}
	}
	if !found {
		t.Errorf("expected main.main to call compute, callers: %v", callers)
	}

	// Test FindCallees: what does compute call?
	callees := g.FindCallees("main.compute")
	if len(callees) == 0 {
		t.Error("expected callees of main.compute, got none")
	}
	found = false
	for _, c := range callees {
		if c == "helper" {
			found = true
		}
	}
	if !found {
		t.Errorf("expected compute to call helper, callees: %v", callees)
	}

	// Test FindImpact: if I change helper, what breaks?
	impact := g.FindImpact("helper", 3)
	if len(impact) == 0 {
		t.Error("expected impact from changing helper, got none")
	}
}

func TestRefGraph_NilSafe(t *testing.T) {
	var g *RefGraph

	// All methods should be nil-safe.
	g.IndexFileReferences("", "")
	callers := g.FindCallers("foo")
	if callers != nil {
		t.Error("expected nil callers from nil graph")
	}
	impact := g.FindImpact("foo", 3)
	if impact != nil {
		t.Error("expected nil impact from nil graph")
	}
}
