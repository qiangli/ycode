package cli

import "testing"

func TestFilterCompletions(t *testing.T) {
	all := []completionItem{
		{Name: "build", Description: "Build binary"},
		{Name: "clear", Description: "Clear conversation"},
		{Name: "claude", Description: "skill", IsSkill: true},
		{Name: "cost", Description: "Show cost"},
		{Name: "help", Description: "Show help"},
		{Name: "model", Description: "Switch model"},
		{Name: "status", Description: "Show status"},
	}

	tests := []struct {
		prefix string
		want   []string
	}{
		{"", []string{"build", "clear", "claude", "cost", "help", "model", "status"}},
		{"c", []string{"clear", "claude", "cost"}},
		{"cl", []string{"clear", "claude"}},
		{"cla", []string{"claude"}},
		{"h", []string{"help"}},
		{"z", nil},
		{"mo", []string{"model"}},
	}

	for _, tt := range tests {
		got := filterCompletions(all, tt.prefix)
		var names []string
		for _, item := range got {
			names = append(names, item.Name)
		}
		if len(names) != len(tt.want) {
			t.Errorf("filterCompletions(%q): got %v, want %v", tt.prefix, names, tt.want)
			continue
		}
		for i := range names {
			if names[i] != tt.want[i] {
				t.Errorf("filterCompletions(%q)[%d]: got %q, want %q", tt.prefix, i, names[i], tt.want[i])
			}
		}
	}
}

func TestCompletionStateUpdate(t *testing.T) {
	all := []completionItem{
		{Name: "build", Description: "Build binary"},
		{Name: "help", Description: "Show help"},
		{Name: "status", Description: "Show status"},
	}

	var cs completionState

	// Typing "/" shows all commands.
	cs.update(all, "/")
	if !cs.visible {
		t.Error("expected visible after /")
	}
	if len(cs.items) != 3 {
		t.Errorf("expected 3 items, got %d", len(cs.items))
	}

	// Typing "/h" filters to help.
	cs.update(all, "/h")
	if len(cs.items) != 1 {
		t.Errorf("expected 1 item for /h, got %d", len(cs.items))
	}
	if cs.items[0].Name != "help" {
		t.Errorf("expected 'help', got %q", cs.items[0].Name)
	}

	// Typing "/help " (with space) dismisses — command already typed.
	cs.update(all, "/help ")
	if cs.visible {
		t.Error("expected not visible after /help (space)")
	}

	// Regular text doesn't show completion.
	cs.update(all, "hello")
	if cs.visible {
		t.Error("expected not visible for regular text")
	}

	// Empty input.
	cs.update(all, "")
	if cs.visible {
		t.Error("expected not visible for empty input")
	}
}

func TestCompletionNavigation(t *testing.T) {
	all := []completionItem{
		{Name: "a"}, {Name: "b"}, {Name: "c"},
	}

	var cs completionState
	cs.update(all, "/")

	if cs.selected != 0 {
		t.Errorf("expected selected=0, got %d", cs.selected)
	}
	if cs.selectedName() != "a" {
		t.Errorf("expected 'a', got %q", cs.selectedName())
	}

	cs.moveDown()
	if cs.selectedName() != "b" {
		t.Errorf("expected 'b' after moveDown, got %q", cs.selectedName())
	}

	cs.moveDown()
	if cs.selectedName() != "c" {
		t.Errorf("expected 'c' after second moveDown, got %q", cs.selectedName())
	}

	// Wrap around.
	cs.moveDown()
	if cs.selectedName() != "a" {
		t.Errorf("expected 'a' after wrap, got %q", cs.selectedName())
	}

	cs.moveUp()
	if cs.selectedName() != "c" {
		t.Errorf("expected 'c' after moveUp wrap, got %q", cs.selectedName())
	}

	cs.dismiss()
	if cs.visible {
		t.Error("expected not visible after dismiss")
	}
}

func TestRenderCompletion(t *testing.T) {
	cs := &completionState{
		items:    []completionItem{{Name: "help", Description: "Show help"}},
		selected: 0,
		visible:  true,
	}

	output := renderCompletion(cs, 60)
	if output == "" {
		t.Error("expected non-empty render output")
	}

	// Not visible returns empty.
	cs.visible = false
	output = renderCompletion(cs, 60)
	if output != "" {
		t.Error("expected empty render when not visible")
	}
}
