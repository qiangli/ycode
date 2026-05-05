// Command memex_full is a smoke test that exercises the full memex umbrella
// (Open, Memory, Memos, VFS) from outside the ycode tree. Builds and runs
// without importing any internal/ packages.
package main

import (
	"context"
	"fmt"
	"os"

	"github.com/qiangli/ycode/pkg/memex"
	"github.com/qiangli/ycode/pkg/memex/memory"
	"github.com/qiangli/ycode/pkg/memex/memos"
)

func main() {
	dir, err := os.MkdirTemp("", "memex-full-")
	if err != nil {
		fail(err)
	}
	defer os.RemoveAll(dir)

	mx, err := memex.Open(dir)
	if err != nil {
		fail(err)
	}
	defer mx.Close()

	ctx := context.Background()

	// Memory round-trip.
	if err := mx.Memory().Save(&memory.Memory{
		Name:        "demo-memory",
		Type:        memory.TypeReference,
		Scope:       memory.ScopeGlobal,
		Content:     "memex umbrella smoke memory",
		Description: "external import test",
	}); err != nil {
		fail(err)
	}
	hits, err := mx.Memory().Recall("smoke memory", 5)
	if err != nil {
		fail(err)
	}
	if len(hits) == 0 {
		fail(fmt.Errorf("memory recall returned no hits"))
	}

	// Memo round-trip.
	if mx.Memos() == nil {
		fail(fmt.Errorf("memos store is nil"))
	}
	memo := &memos.Memo{Content: "external #smoke memo"}
	if err := mx.Memos().Create(ctx, memo); err != nil {
		fail(err)
	}
	got, err := mx.Memos().Get(ctx, memo.ID)
	if err != nil {
		fail(err)
	}
	if got.Content != memo.Content {
		fail(fmt.Errorf("memo round-trip content mismatch: %q vs %q", got.Content, memo.Content))
	}

	// VFS round-trip.
	roots, err := mx.VFS().List(ctx, "/")
	if err != nil {
		fail(err)
	}
	if len(roots) < 2 {
		fail(fmt.Errorf("vfs root listing: got %d entries, want >=2", len(roots)))
	}

	// Read the memory back through the VFS.
	memPath := memex.MemoryPath(memory.Memory{
		Name:  "demo-memory",
		Type:  memory.TypeReference,
		Scope: memory.ScopeGlobal,
	})
	body, _, err := mx.VFS().Read(ctx, memPath)
	if err != nil {
		fail(err)
	}
	if string(body) != "memex umbrella smoke memory" {
		fail(fmt.Errorf("vfs memory read mismatch: %q", body))
	}

	fmt.Println("pkg/memex: umbrella + Memory + Memos + VFS round-trip OK")
}

func fail(err error) {
	fmt.Fprintln(os.Stderr, "memex-full smoke:", err)
	os.Exit(1)
}
