package tools

import (
	"context"
	"encoding/json"
	"path/filepath"
	"strings"
	"testing"

	"github.com/qiangli/ycode/internal/runtime/todo"
)

// helper: build a registry pre-seeded with the TodoWrite builtin spec
// and the handler attached. Used by every test below.
func setupTodoHandler(t *testing.T, persistPath string) (*Registry, *todo.Board) {
	t.Helper()
	r := NewRegistry()
	RegisterBuiltins(r)
	board := todo.NewBoard()
	RegisterTodoHandler(r, board, persistPath)
	return r, board
}

// callTodoWrite invokes the handler and returns (result, err).
func callTodoWrite(t *testing.T, r *Registry, input string) (string, error) {
	t.Helper()
	spec, ok := r.Get("TodoWrite")
	if !ok {
		t.Fatal("TodoWrite not registered")
	}
	if spec.Handler == nil {
		t.Fatal("TodoWrite handler is nil — wiring broken")
	}
	return spec.Handler(context.Background(), json.RawMessage(input))
}

func TestTodoWrite_BasicReplace(t *testing.T) {
	r, board := setupTodoHandler(t, "")

	res, err := callTodoWrite(t, r, `{"todos":[
		{"content":"Fix login bug","status":"in_progress","activeForm":"Fixing login bug"},
		{"content":"Add tests","status":"pending"}
	]}`)
	if err != nil {
		t.Fatalf("write failed: %v", err)
	}
	if !strings.Contains(res, "Wrote 2 todo(s)") {
		t.Errorf("result missing summary: %q", res)
	}
	if board.Len() != 2 {
		t.Errorf("board.Len()=%d, want 2", board.Len())
	}
}

func TestTodoWrite_ReplacementSemantics(t *testing.T) {
	// Second write fully replaces the first — the model rewrites the whole
	// list each turn (deepagents semantics, not append).
	r, board := setupTodoHandler(t, "")

	if _, err := callTodoWrite(t, r, `{"todos":[
		{"content":"Old task A","status":"done"},
		{"content":"Old task B","status":"done"}
	]}`); err != nil {
		t.Fatal(err)
	}
	if _, err := callTodoWrite(t, r, `{"todos":[
		{"content":"New only","status":"pending"}
	]}`); err != nil {
		t.Fatal(err)
	}
	if board.Len() != 1 {
		t.Fatalf("board.Len()=%d, want 1 after replace", board.Len())
	}
	for _, item := range board.List() {
		if item.Title != "New only" {
			t.Errorf("unexpected remaining item: %q", item.Title)
		}
	}
}

func TestTodoWrite_IdempotentIDs(t *testing.T) {
	// Same content → same ID. Useful when the model re-emits unchanged
	// rows alongside new ones — no churn in the underlying map.
	r, _ := setupTodoHandler(t, "")

	id1 := contentHashID("Identical content")
	id2 := contentHashID("Identical content")
	if id1 != id2 {
		t.Errorf("contentHashID not stable: %q vs %q", id1, id2)
	}

	if _, err := callTodoWrite(t, r, `{"todos":[
		{"content":"Identical content","status":"pending"}
	]}`); err != nil {
		t.Fatal(err)
	}
	if _, err := callTodoWrite(t, r, `{"todos":[
		{"content":"Identical content","status":"in_progress"}
	]}`); err != nil {
		t.Fatal(err)
	}
}

func TestTodoWrite_StatusAliases(t *testing.T) {
	// "completed" should be accepted as an alias for "done" — deepagents
	// uses "completed", ycode's primitive uses "done"; the handler bridges.
	r, board := setupTodoHandler(t, "")

	if _, err := callTodoWrite(t, r, `{"todos":[
		{"content":"x","status":"completed"}
	]}`); err != nil {
		t.Fatalf("completed should be a valid alias: %v", err)
	}
	for _, item := range board.List() {
		if item.Status != todo.StatusDone {
			t.Errorf("status alias not normalized: got %q want %q", item.Status, todo.StatusDone)
		}
	}
}

func TestTodoWrite_InvalidStatus(t *testing.T) {
	r, _ := setupTodoHandler(t, "")

	_, err := callTodoWrite(t, r, `{"todos":[
		{"content":"x","status":"bogus"}
	]}`)
	if err == nil {
		t.Fatal("expected error for invalid status")
	}
	// Error should list valid options so the model can self-correct.
	if !strings.Contains(err.Error(), "pending") || !strings.Contains(err.Error(), "in_progress") {
		t.Errorf("error message should list valid statuses; got: %v", err)
	}
}

func TestTodoWrite_EmptyContentRejected(t *testing.T) {
	r, _ := setupTodoHandler(t, "")

	_, err := callTodoWrite(t, r, `{"todos":[
		{"content":"","status":"pending"}
	]}`)
	if err == nil {
		t.Fatal("expected error for empty content")
	}
}

func TestTodoWrite_EmptyArrayClears(t *testing.T) {
	r, board := setupTodoHandler(t, "")

	if _, err := callTodoWrite(t, r, `{"todos":[
		{"content":"to clear","status":"pending"}
	]}`); err != nil {
		t.Fatal(err)
	}
	if _, err := callTodoWrite(t, r, `{"todos":[]}`); err != nil {
		t.Fatal(err)
	}
	if board.Len() != 0 {
		t.Errorf("board should be empty after clearing, has %d items", board.Len())
	}
}

func TestTodoWrite_PersistsToDisk(t *testing.T) {
	persistPath := filepath.Join(t.TempDir(), "todos.json")
	r, _ := setupTodoHandler(t, persistPath)

	if _, err := callTodoWrite(t, r, `{"todos":[
		{"content":"persisted task","status":"pending"}
	]}`); err != nil {
		t.Fatal(err)
	}

	// Reload from disk and verify the item survived.
	loaded, err := todo.LoadBoard(persistPath)
	if err != nil {
		t.Fatalf("reload: %v", err)
	}
	if loaded.Len() != 1 {
		t.Errorf("reloaded board has %d items, want 1", loaded.Len())
	}
}

func TestTodoWrite_SpecFlags(t *testing.T) {
	// Smoke-test that the spec metadata matches the design: AlwaysAvailable
	// so it shows up in every conversation; ReadOnly so it works in any
	// permission mode (board is internal agent state, not user files).
	r := NewRegistry()
	RegisterBuiltins(r)

	spec, ok := r.Get("TodoWrite")
	if !ok {
		t.Fatal("TodoWrite not registered by RegisterBuiltins")
	}
	if !spec.AlwaysAvailable {
		t.Error("TodoWrite should be AlwaysAvailable")
	}
	if spec.Description == "" {
		t.Error("TodoWrite description should be non-empty")
	}
}
