package observability

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
)

func TestPortAllocator(t *testing.T) {
	dir := t.TempDir()
	pa := NewPortAllocator(dir)

	port1, err := pa.Allocate("test-component")
	if err != nil {
		t.Fatal(err)
	}
	if port1 == 0 {
		t.Error("expected non-zero port")
	}

	port2, err := pa.Allocate("another-component")
	if err != nil {
		t.Fatal(err)
	}
	if port2 == 0 || port2 == port1 {
		t.Errorf("expected unique port, got %d (same as %d)", port2, port1)
	}

	// Verify Get.
	if got := pa.Get("test-component"); got != port1 {
		t.Errorf("Get(test-component) = %d, want %d", got, port1)
	}
	if got := pa.Get("nonexistent"); got != 0 {
		t.Errorf("Get(nonexistent) = %d, want 0", got)
	}

	// Verify persistence.
	portsFile := filepath.Join(dir, "ports.json")
	data, err := os.ReadFile(portsFile)
	if err != nil {
		t.Fatal(err)
	}
	var ports map[string]int
	if err := json.Unmarshal(data, &ports); err != nil {
		t.Fatal(err)
	}
	if ports["test-component"] != port1 {
		t.Errorf("persisted port = %d, want %d", ports["test-component"], port1)
	}

	// Verify All().
	all := pa.All()
	if len(all) != 2 {
		t.Errorf("All() len = %d, want 2", len(all))
	}

	// Verify Release.
	pa.Release("test-component")
	if got := pa.Get("test-component"); got != 0 {
		t.Errorf("after Release, Get = %d, want 0", got)
	}

	// Verify ReleaseAll.
	pa.ReleaseAll()
	if _, err := os.Stat(portsFile); !os.IsNotExist(err) {
		t.Error("ports.json should be removed after ReleaseAll")
	}
}

func TestPortAllocatorReload(t *testing.T) {
	dir := t.TempDir()

	pa1 := NewPortAllocator(dir)
	port, _ := pa1.Allocate("test")

	// Create a new allocator from the same dir — should load existing ports.
	pa2 := NewPortAllocator(dir)
	if got := pa2.Get("test"); got != port {
		t.Errorf("reloaded port = %d, want %d", got, port)
	}
}
