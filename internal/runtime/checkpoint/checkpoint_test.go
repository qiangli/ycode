package checkpoint

import (
	"context"
	"encoding/json"
	"testing"
)

func TestFileStore_SaveAndLoad(t *testing.T) {
	dir := t.TempDir()
	store := NewFileStore(dir)
	ctx := context.Background()

	cp := &Checkpoint{
		ID:           "cp-1",
		WorkflowID:   "workflow-123",
		WorkflowType: "dag",
		Phase:        "layer-2",
		Outputs:      map[string]string{"node-a": "output-a", "node-b": "output-b"},
		Resumable:    true,
	}

	if err := store.Save(ctx, cp); err != nil {
		t.Fatalf("save: %v", err)
	}

	loaded, err := store.Load(ctx, "workflow-123")
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	if loaded == nil {
		t.Fatal("expected non-nil checkpoint")
	}
	if loaded.WorkflowID != "workflow-123" {
		t.Fatalf("expected workflow-123, got %s", loaded.WorkflowID)
	}
	if loaded.Phase != "layer-2" {
		t.Fatalf("expected layer-2, got %s", loaded.Phase)
	}
	if len(loaded.Outputs) != 2 {
		t.Fatalf("expected 2 outputs, got %d", len(loaded.Outputs))
	}
	if !loaded.Resumable {
		t.Fatal("expected resumable")
	}
}

func TestFileStore_LoadNonExistent(t *testing.T) {
	dir := t.TempDir()
	store := NewFileStore(dir)

	cp, err := store.Load(context.Background(), "nonexistent")
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	if cp != nil {
		t.Fatal("expected nil checkpoint for nonexistent workflow")
	}
}

func TestFileStore_List(t *testing.T) {
	dir := t.TempDir()
	store := NewFileStore(dir)
	ctx := context.Background()

	// Save two checkpoints.
	_ = store.Save(ctx, &Checkpoint{WorkflowID: "wf-1", Phase: "a"})
	_ = store.Save(ctx, &Checkpoint{WorkflowID: "wf-2", Phase: "b"})

	cps, err := store.List(ctx)
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	if len(cps) != 2 {
		t.Fatalf("expected 2 checkpoints, got %d", len(cps))
	}
}

func TestFileStore_Delete(t *testing.T) {
	dir := t.TempDir()
	store := NewFileStore(dir)
	ctx := context.Background()

	_ = store.Save(ctx, &Checkpoint{WorkflowID: "wf-del", Phase: "x"})

	if err := store.Delete(ctx, "wf-del"); err != nil {
		t.Fatalf("delete: %v", err)
	}

	cp, err := store.Load(ctx, "wf-del")
	if err != nil {
		t.Fatalf("load after delete: %v", err)
	}
	if cp != nil {
		t.Fatal("expected nil after delete")
	}
}

func TestFileStore_DeleteNonExistent(t *testing.T) {
	dir := t.TempDir()
	store := NewFileStore(dir)

	if err := store.Delete(context.Background(), "nope"); err != nil {
		t.Fatalf("delete nonexistent should not error: %v", err)
	}
}

func TestFileStore_ListEmptyDir(t *testing.T) {
	store := NewFileStore(t.TempDir() + "/nonexistent")

	cps, err := store.List(context.Background())
	if err != nil {
		t.Fatalf("list empty: %v", err)
	}
	if len(cps) != 0 {
		t.Fatalf("expected 0, got %d", len(cps))
	}
}

func TestFileStore_SaveWithState(t *testing.T) {
	dir := t.TempDir()
	store := NewFileStore(dir)
	ctx := context.Background()

	state := map[string]any{"iteration": 3, "scores": []float64{0.5, 0.7, 0.8}}
	stateBytes, _ := json.Marshal(state)

	cp := &Checkpoint{
		WorkflowID:   "wf-state",
		WorkflowType: "autoloop",
		Phase:        "evaluate",
		State:        stateBytes,
		Resumable:    true,
	}

	if err := store.Save(ctx, cp); err != nil {
		t.Fatalf("save: %v", err)
	}

	loaded, err := store.Load(ctx, "wf-state")
	if err != nil {
		t.Fatalf("load: %v", err)
	}

	var loadedState map[string]any
	if err := json.Unmarshal(loaded.State, &loadedState); err != nil {
		t.Fatalf("unmarshal state: %v", err)
	}
	if loadedState["iteration"].(float64) != 3 {
		t.Fatal("state not preserved")
	}
}
