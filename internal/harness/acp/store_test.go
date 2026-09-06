package acp

import (
	"path/filepath"
	"sync"
	"testing"
)

func TestConcurrentTurnAllocationIsUniqueAndRestartSafe(t *testing.T) {
	path := filepath.Join(t.TempDir(), "sessions.json")
	store, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	session, err := store.Create(filepath.Clean(t.TempDir()))
	if err != nil {
		t.Fatal(err)
	}
	const count = 16
	ids := make(chan string, count)
	var wg sync.WaitGroup
	for i := 0; i < count; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			id, err := store.NextTurn(session.ID)
			if err != nil {
				t.Error(err)
				return
			}
			ids <- id
		}()
	}
	wg.Wait()
	close(ids)
	seen := map[string]bool{}
	for id := range ids {
		if seen[id] {
			t.Fatalf("duplicate %q", id)
		}
		seen[id] = true
	}
	if len(seen) != count {
		t.Fatalf("turn IDs=%d", len(seen))
	}
	reopened, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	next, err := reopened.NextTurn(session.ID)
	if err != nil {
		t.Fatal(err)
	}
	if seen[next] {
		t.Fatalf("restart reused %q", next)
	}
}

func TestForkLineageHashChainSurvivesReplay(t *testing.T) {
	path := filepath.Join(t.TempDir(), "sessions.json")
	store, _ := Open(path)
	parent, _ := store.Create(filepath.Clean(t.TempDir()))
	_ = store.Advance(parent.ID, 9)
	child, err := store.Fork(parent.ID, parent.Cwd, 9)
	if err != nil {
		t.Fatal(err)
	}
	reopened, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	values := reopened.List("")
	if len(values) != 2 {
		t.Fatalf("sessions=%#v", values)
	}
	var got Session
	for _, value := range values {
		if value.ID == child.ID {
			got = value
		}
	}
	if got.ParentID != parent.ID || got.ForkSequence != 9 {
		t.Fatalf("child=%#v", got)
	}
	lineage := reopened.Lineage()
	if len(lineage) != 2 || lineage[1].PreviousDigest != lineage[0].Digest {
		t.Fatalf("lineage=%#v", lineage)
	}
}
