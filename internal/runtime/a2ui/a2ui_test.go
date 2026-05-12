package a2ui

import (
	"encoding/json"
	"strings"
	"testing"
)

func TestNewCreateSurface_DefaultCatalog(t *testing.T) {
	op := NewCreateSurface("kanban")
	if op.Version != Version {
		t.Errorf("version = %q, want %q", op.Version, Version)
	}
	if op.CreateSurface == nil {
		t.Fatal("CreateSurface should be set")
	}
	if op.CreateSurface.SurfaceID != "kanban" {
		t.Errorf("surfaceID = %q, want kanban", op.CreateSurface.SurfaceID)
	}
	if op.CreateSurface.CatalogID != BasicCatalogID {
		t.Errorf("catalogID = %q, want default %q", op.CreateSurface.CatalogID, BasicCatalogID)
	}
}

func TestNewUpdateDataModel_DefaultsRootPath(t *testing.T) {
	op := NewUpdateDataModel("memos", "", map[string]any{"k": 1})
	if op.UpdateDataModel.Path != "/" {
		t.Errorf("empty path should default to /, got %q", op.UpdateDataModel.Path)
	}
}

func TestRender_ShapeMatchesPythonReference(t *testing.T) {
	// The Python a2ui.render() wraps ops in {"a2ui_operations": [...]}.
	// Renderers (both @a2ui/web_core and the lit reference) explicitly
	// look for that key — if we drift, they silently ignore our payload.
	ops := []Op{
		NewCreateSurface("health"),
		NewUpdateDataModel("health", "/", map[string]any{"slo": 99.9}),
	}
	data, err := Render(ops)
	if err != nil {
		t.Fatal(err)
	}

	var out map[string]any
	if err := json.Unmarshal(data, &out); err != nil {
		t.Fatalf("rendered payload is not valid JSON: %v", err)
	}
	if _, ok := out[OperationsKey]; !ok {
		t.Errorf("rendered payload missing %q key; got %s", OperationsKey, data)
	}

	// Sanity check op shape — first op should have createSurface set.
	if !strings.Contains(string(data), `"createSurface"`) {
		t.Errorf("rendered payload missing createSurface marker: %s", data)
	}
	if !strings.Contains(string(data), `"updateDataModel"`) {
		t.Errorf("rendered payload missing updateDataModel marker: %s", data)
	}
}

func TestRender_NilOpsProducesEmptyArray(t *testing.T) {
	// A foreign agent might call render() with no ops to clear a session.
	// Should produce {"a2ui_operations": []}, not null.
	data, err := Render(nil)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(data), `"a2ui_operations":[]`) {
		t.Errorf("nil ops should serialize as empty array, got %s", data)
	}
}

func TestOp_OmitsUnusedFields(t *testing.T) {
	// JSON omitempty discipline: a CreateSurface op should NOT carry
	// keys for updateComponents/updateDataModel. Important — the renderer
	// may treat a present-but-null field as a malformed op.
	op := NewCreateSurface("x")
	data, err := json.Marshal(op)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(data), "updateComponents") {
		t.Errorf("createSurface op leaked updateComponents key: %s", data)
	}
	if strings.Contains(string(data), "updateDataModel") {
		t.Errorf("createSurface op leaked updateDataModel key: %s", data)
	}
}

func TestSurface_Bootstrap(t *testing.T) {
	// A first-class surface should produce a 2-op bootstrap sequence:
	// createSurface (announces it) + updateComponents (defines its UI).
	s := &Surface{
		ID:         "health",
		Components: []json.RawMessage{json.RawMessage(`{"id":"root","component":"Text"}`)},
	}
	ops := s.Bootstrap()
	if len(ops) != 2 {
		t.Fatalf("bootstrap should produce 2 ops, got %d", len(ops))
	}
	if ops[0].CreateSurface == nil || ops[0].CreateSurface.SurfaceID != "health" {
		t.Errorf("first bootstrap op should be createSurface(health), got %+v", ops[0])
	}
	if ops[1].UpdateComponents == nil || ops[1].UpdateComponents.SurfaceID != "health" {
		t.Errorf("second bootstrap op should be updateComponents(health), got %+v", ops[1])
	}
}

func TestSurface_BootstrapDefaultsCatalog(t *testing.T) {
	s := &Surface{ID: "x"} // no CatalogID set
	ops := s.Bootstrap()
	if ops[0].CreateSurface.CatalogID != BasicCatalogID {
		t.Errorf("empty catalog should default to BasicCatalogID, got %q", ops[0].CreateSurface.CatalogID)
	}
}

func TestRegistry_AddGetAll(t *testing.T) {
	r := NewRegistry()
	if err := r.Add(&Surface{ID: "a"}); err != nil {
		t.Fatal(err)
	}
	if err := r.Add(&Surface{ID: "b"}); err != nil {
		t.Fatal(err)
	}
	if _, ok := r.Get("a"); !ok {
		t.Error("surface 'a' not found after add")
	}
	if got := len(r.All()); got != 2 {
		t.Errorf("All() len = %d, want 2", got)
	}
}

func TestRegistry_RejectsEmptyID(t *testing.T) {
	r := NewRegistry()
	if err := r.Add(&Surface{ID: ""}); err == nil {
		t.Error("Add should reject empty ID")
	}
	if err := r.Add(nil); err == nil {
		t.Error("Add should reject nil surface")
	}
}

func TestRegistry_Replaces(t *testing.T) {
	r := NewRegistry()
	_ = r.Add(&Surface{ID: "x", CatalogID: "old"})
	_ = r.Add(&Surface{ID: "x", CatalogID: "new"})
	got, _ := r.Get("x")
	if got.CatalogID != "new" {
		t.Errorf("second Add should replace; got catalog %q want new", got.CatalogID)
	}
}
