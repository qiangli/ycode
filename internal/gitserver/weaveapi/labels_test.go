package weaveapi

import "testing"

func TestPriorityValue(t *testing.T) {
	cases := map[string]int{
		LabelPriorityP0: 0,
		LabelPriorityP1: 1,
		LabelPriorityP2: 2,
		LabelPriorityP3: 3,
		"":              2,
		"loom:p9":       2,
		"random":        2,
	}
	for label, want := range cases {
		if got := PriorityValue(label); got != want {
			t.Errorf("PriorityValue(%q)=%d want %d", label, got, want)
		}
	}
}

func TestIsPriorityLabel(t *testing.T) {
	for _, name := range []string{LabelPriorityP0, LabelPriorityP1, LabelPriorityP2, LabelPriorityP3} {
		if !IsPriorityLabel(name) {
			t.Errorf("IsPriorityLabel(%q)=false want true", name)
		}
	}
	for _, name := range []string{"", "loom:p4", "p0", LabelStateWorking, LabelSourceHuman} {
		if IsPriorityLabel(name) {
			t.Errorf("IsPriorityLabel(%q)=true want false", name)
		}
	}
}

func TestIsStateLabel(t *testing.T) {
	want := []string{
		LabelStateTodo, LabelStateWorking, LabelStateSubmitted,
		LabelStateCIFailed, LabelStateConflict, LabelStateMerged,
		LabelStateAbandoned, LabelProposed,
	}
	for _, name := range want {
		if !IsStateLabel(name) {
			t.Errorf("IsStateLabel(%q)=false want true", name)
		}
	}
	for _, name := range []string{"", "todo", LabelPriorityP0, LabelSourceHuman, "loom:working-extra"} {
		if IsStateLabel(name) {
			t.Errorf("IsStateLabel(%q)=true want false", name)
		}
	}
}

func TestAllLabelSpecs_Count(t *testing.T) {
	specs := AllLabelSpecs()
	// 8 state (incl. proposed) + 4 priority + 2 source = 14.
	if len(specs) != 14 {
		t.Errorf("AllLabelSpecs returned %d, want 14", len(specs))
	}
	// No duplicates.
	seen := map[string]bool{}
	for _, s := range specs {
		if seen[s.Name] {
			t.Errorf("duplicate label spec %q", s.Name)
		}
		seen[s.Name] = true
		if s.Color == "" {
			t.Errorf("label %q has no color", s.Name)
		}
	}
}

func TestLabelCache_PutGet(t *testing.T) {
	c := newLabelCache()
	c.put("admin", "myapp", "loom:p0", 42)
	if id, ok := c.get("admin", "myapp", "loom:p0"); !ok || id != 42 {
		t.Errorf("cache.get returned (%d, %v); want (42, true)", id, ok)
	}
	if _, ok := c.get("admin", "myapp", "loom:p1"); ok {
		t.Errorf("cache.get for unknown label returned ok=true")
	}
	if _, ok := c.get("admin", "other", "loom:p0"); ok {
		t.Errorf("cache.get crossed repo boundary")
	}
}
