package cli

import (
	"testing"

	"github.com/qiangli/ycode/internal/api"
)

// testModels returns a fixed set of models for testing.
func testModels() []api.ModelInfo {
	return []api.ModelInfo{
		{ID: "claude-sonnet-4-6-20250514", Alias: "sonnet", Provider: "anthropic", Source: "builtin"},
		{ID: "claude-opus-4-6-20250415", Alias: "opus", Provider: "anthropic", Source: "builtin"},
		{ID: "gemini-2.5-pro", Alias: "gemini-pro", Provider: "gemini", Source: "builtin"},
		{ID: "gpt-4.1", Provider: "openai", Source: "env"},
		{ID: "llama3.2:3b", Provider: "ollama", Source: "ollama", Size: "2.0 GB"},
	}
}

func TestModelPickerOpenClose(t *testing.T) {
	var mp modelPickerState
	mp.open("claude-sonnet-4-6-20250514", testModels())

	if !mp.visible {
		t.Error("expected picker to be visible after open")
	}
	if len(mp.filtered) == 0 {
		t.Error("expected filtered items after open")
	}
	if len(mp.filtered) != 5 {
		t.Errorf("expected 5 items, got %d", len(mp.filtered))
	}

	mp.close()
	if mp.visible {
		t.Error("expected picker to be hidden after close")
	}
}

func TestModelPickerFilter(t *testing.T) {
	var mp modelPickerState
	mp.open("claude-sonnet-4-6-20250514", testModels())

	total := len(mp.filtered)
	mp.typeChar('g')
	mp.typeChar('e')
	mp.typeChar('m')

	if len(mp.filtered) >= total {
		t.Error("expected filter to reduce items")
	}

	// All filtered items should contain "gem".
	for _, item := range mp.filtered {
		if !contains(item.ID, "gem") && !contains(item.Alias, "gem") {
			t.Errorf("filtered item %q should contain 'gem'", item.ID)
		}
	}
}

func TestModelPickerFilterBySource(t *testing.T) {
	var mp modelPickerState
	mp.open("claude-sonnet-4-6-20250514", testModels())

	mp.typeChar('o')
	mp.typeChar('l')
	mp.typeChar('l')
	mp.typeChar('a')
	mp.typeChar('m')
	mp.typeChar('a')

	if len(mp.filtered) != 1 {
		t.Errorf("expected 1 ollama item, got %d", len(mp.filtered))
	}
	if len(mp.filtered) > 0 && mp.filtered[0].Source != "ollama" {
		t.Errorf("expected ollama source, got %q", mp.filtered[0].Source)
	}
}

func contains(s, sub string) bool {
	return len(s) >= len(sub) && (s == sub || len(sub) == 0 ||
		findSubstring(s, sub))
}

func findSubstring(s, sub string) bool {
	for i := 0; i <= len(s)-len(sub); i++ {
		if s[i:i+len(sub)] == sub {
			return true
		}
	}
	return false
}

func TestModelPickerNavigation(t *testing.T) {
	var mp modelPickerState
	mp.open("claude-sonnet-4-6-20250514", testModels())

	if len(mp.filtered) < 2 {
		t.Skip("need at least 2 items for navigation test")
	}

	initial := mp.selected
	mp.moveDown()
	if mp.selected == initial {
		t.Error("expected selected to change after moveDown")
	}
	mp.moveUp()
	if mp.selected != initial {
		t.Error("expected selected to return to initial after moveUp")
	}
}

func TestModelPickerCurrentModelSelected(t *testing.T) {
	var mp modelPickerState
	mp.open("gemini-2.5-pro", testModels())

	if mp.selected < 0 || mp.selected >= len(mp.filtered) {
		t.Fatal("invalid selection")
	}
	if !mp.filtered[mp.selected].Current {
		t.Error("expected current model to be selected")
	}
	if mp.filtered[mp.selected].ID != "gemini-2.5-pro" {
		t.Errorf("expected gemini-2.5-pro selected, got %q", mp.filtered[mp.selected].ID)
	}
}

func TestBuildModelPickerItems(t *testing.T) {
	models := testModels()
	items := buildModelPickerItems("llama3.2:3b", models)

	if len(items) != 5 {
		t.Fatalf("expected 5 items, got %d", len(items))
	}

	// Check that the ollama model is marked current.
	var currentCount int
	for _, item := range items {
		if item.Current {
			currentCount++
			if item.ID != "llama3.2:3b" {
				t.Errorf("expected llama3.2:3b as current, got %q", item.ID)
			}
		}
	}
	if currentCount != 1 {
		t.Errorf("expected 1 current model, got %d", currentCount)
	}
}
