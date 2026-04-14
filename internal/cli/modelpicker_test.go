package cli

import "testing"

func TestDetectProviderFromModel(t *testing.T) {
	tests := []struct {
		model, want string
	}{
		{"claude-sonnet-4-6-20250514", "anthropic"},
		{"gpt-4o", "openai"},
		{"gemini-2.5-pro", "gemini"},
		{"grok-3", "xai"},
		{"qwen-plus", "dashscope"},
		{"kimi-k2.5", "moonshot"},
		{"unknown-model", "unknown"},
	}
	for _, tt := range tests {
		got := detectProviderFromModel(tt.model)
		if got != tt.want {
			t.Errorf("detectProviderFromModel(%q) = %q, want %q", tt.model, got, tt.want)
		}
	}
}

func TestModelPickerOpenClose(t *testing.T) {
	var mp modelPickerState
	mp.open("claude-sonnet-4-6-20250514")

	if !mp.visible {
		t.Error("expected picker to be visible after open")
	}
	if len(mp.filtered) == 0 {
		t.Error("expected filtered items after open")
	}

	mp.close()
	if mp.visible {
		t.Error("expected picker to be hidden after close")
	}
}

func TestModelPickerFilter(t *testing.T) {
	var mp modelPickerState
	mp.open("claude-sonnet-4-6-20250514")

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
	mp.open("claude-sonnet-4-6-20250514")

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
