package cli

import "testing"

func TestFuzzyMatch(t *testing.T) {
	tests := []struct {
		text, pattern string
		want          bool
	}{
		{"help", "hlp", true},
		{"command", "cmd", true},
		{"status", "sts", true},
		{"help", "xyz", false},
		{"", "", true},
		{"hello", "", true},
		{"", "a", false},
		{"model", "model", true},
	}
	for _, tt := range tests {
		got := fuzzyMatch(tt.text, tt.pattern)
		if got != tt.want {
			t.Errorf("fuzzyMatch(%q, %q) = %v, want %v", tt.text, tt.pattern, got, tt.want)
		}
	}
}

func TestCommandPaletteOpenClose(t *testing.T) {
	var cp commandPaletteState
	items := []paletteItem{
		{Name: "/help", Description: "Show help", Category: "session"},
		{Name: "/status", Description: "Show status", Category: "session"},
		{Name: "/model", Description: "Switch model", Category: "workspace"},
	}

	cp.open(items)
	if !cp.visible {
		t.Error("expected palette to be visible after open")
	}
	if len(cp.filtered) != 3 {
		t.Errorf("expected 3 items, got %d", len(cp.filtered))
	}

	cp.close()
	if cp.visible {
		t.Error("expected palette to be hidden after close")
	}
}

func TestCommandPaletteFilter(t *testing.T) {
	var cp commandPaletteState
	items := []paletteItem{
		{Name: "/help", Description: "Show help"},
		{Name: "/status", Description: "Show status"},
		{Name: "/model", Description: "Switch model"},
	}

	cp.open(items)
	cp.typeChar('h')
	cp.typeChar('l')

	// Should match /help via fuzzy "hl"
	found := false
	for _, item := range cp.filtered {
		if item.Name == "/help" {
			found = true
		}
	}
	if !found {
		t.Error("expected /help to match fuzzy filter 'hl'")
	}
}

func TestCommandPaletteNavigation(t *testing.T) {
	var cp commandPaletteState
	items := []paletteItem{
		{Name: "/a", Description: "First"},
		{Name: "/b", Description: "Second"},
		{Name: "/c", Description: "Third"},
	}

	cp.open(items)
	if cp.selected != 0 {
		t.Errorf("expected initial selected=0, got %d", cp.selected)
	}

	cp.moveDown()
	if cp.selected != 1 {
		t.Errorf("expected selected=1 after moveDown, got %d", cp.selected)
	}

	cp.moveDown()
	cp.moveDown() // wrap around
	if cp.selected != 0 {
		t.Errorf("expected selected=0 after wrap, got %d", cp.selected)
	}
}
