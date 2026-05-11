package tools

import (
	"sort"
	"testing"
)

func TestRegisterBuiltinsFiltered_subset(t *testing.T) {
	r := NewRegistry()
	RegisterBuiltinsFiltered(r, []string{"read_file", "grep_search"})

	names := r.Names()
	sort.Strings(names)
	want := []string{"grep_search", "read_file"}
	if len(names) != len(want) {
		t.Fatalf("expected %d tools, got %d: %v", len(want), len(names), names)
	}
	for i := range want {
		if names[i] != want[i] {
			t.Errorf("at %d: want %q got %q", i, want[i], names[i])
		}
	}
}

func TestRegisterBuiltinsFiltered_emptyAllowlist(t *testing.T) {
	r := NewRegistry()
	RegisterBuiltinsFiltered(r, []string{})

	if names := r.Names(); len(names) != 0 {
		t.Errorf("expected no tools with empty allowlist, got %d: %v", len(names), names)
	}
}

func TestRegisterBuiltinsFiltered_nilFallsThrough(t *testing.T) {
	rNil := NewRegistry()
	RegisterBuiltinsFiltered(rNil, nil)

	rAll := NewRegistry()
	RegisterBuiltins(rAll)

	if len(rNil.Names()) != len(rAll.Names()) {
		t.Errorf("nil allowlist should match RegisterBuiltins; got %d vs %d",
			len(rNil.Names()), len(rAll.Names()))
	}
}

func TestRegisterBuiltinsFiltered_dropsBash(t *testing.T) {
	r := NewRegistry()
	RegisterBuiltinsFiltered(r, []string{"read_file"})

	if _, ok := r.Get("bash"); ok {
		t.Error("bash should not be registered under read_file-only allowlist")
	}
	if _, ok := r.Get("read_file"); !ok {
		t.Error("read_file should be registered")
	}
}

func TestRegisterBuiltinsExcluding_dropsBlocked(t *testing.T) {
	r := NewRegistry()
	RegisterBuiltinsExcluding(r, []string{"bash", "write_file"})

	if _, ok := r.Get("bash"); ok {
		t.Error("bash should be excluded")
	}
	if _, ok := r.Get("write_file"); ok {
		t.Error("write_file should be excluded")
	}
	// Other tools must still be present.
	if _, ok := r.Get("read_file"); !ok {
		t.Error("read_file should still be registered")
	}
}

func TestRegisterBuiltinsExcluding_emptyOrNilFallsThrough(t *testing.T) {
	rAll := NewRegistry()
	RegisterBuiltins(rAll)
	rEmpty := NewRegistry()
	RegisterBuiltinsExcluding(rEmpty, nil)
	rEmpty2 := NewRegistry()
	RegisterBuiltinsExcluding(rEmpty2, []string{})

	if len(rEmpty.Names()) != len(rAll.Names()) {
		t.Errorf("nil blocklist should equal RegisterBuiltins; got %d vs %d",
			len(rEmpty.Names()), len(rAll.Names()))
	}
	if len(rEmpty2.Names()) != len(rAll.Names()) {
		t.Errorf("empty blocklist should equal RegisterBuiltins; got %d vs %d",
			len(rEmpty2.Names()), len(rAll.Names()))
	}
}
