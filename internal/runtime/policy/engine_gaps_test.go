package policy

import (
	"testing"
)

func TestCommandPatternMatching(t *testing.T) {
	engine := NewEngine()

	// Rule: allow "git *" commands for bash.
	engine.AddRule(Rule{
		Name:            "allow-git",
		Tools:           []string{"bash"},
		CommandPatterns: []string{"git *"},
		Decision:        DecisionAllow,
		Priority:        10,
	})

	// Rule: deny "rm *" commands for bash.
	engine.AddRule(Rule{
		Name:            "deny-rm",
		Tools:           []string{"bash"},
		CommandPatterns: []string{"rm *"},
		Decision:        DecisionDeny,
		Priority:        10,
	})

	tests := []struct {
		tool   string
		detail string
		want   Decision
	}{
		{"bash", "git commit -m 'test'", DecisionAllow},
		{"bash", "git push", DecisionAllow},
		{"bash", "rm -rf /", DecisionDeny},
		{"bash", "rm file.txt", DecisionDeny},
		{"bash", "ls -la", DecisionAsk},        // no matching command pattern
		{"edit_file", "anything", DecisionAsk}, // no matching tool
	}

	for _, tt := range tests {
		got, _ := engine.Evaluate(tt.tool, tt.detail)
		if got != tt.want {
			t.Errorf("Evaluate(%q, %q) = %s, want %s", tt.tool, tt.detail, got, tt.want)
		}
	}
}

func TestMatchPattern(t *testing.T) {
	tests := []struct {
		patterns []string
		value    string
		want     bool
	}{
		{[]string{"*"}, "anything", true},
		{[]string{"git *"}, "git commit", true},
		{[]string{"git *"}, "git push", true},
		{[]string{"git *"}, "npm install", false},
		{[]string{"npm test"}, "npm test", true},
		{[]string{"npm test"}, "npm install", false},
		{[]string{"go *", "npm *"}, "go test", true},
		{[]string{"go *", "npm *"}, "npm run", true},
		{[]string{"go *", "npm *"}, "cargo build", false},
	}

	for _, tt := range tests {
		got := MatchPattern(tt.patterns, tt.value)
		if got != tt.want {
			t.Errorf("MatchPattern(%v, %q) = %v, want %v", tt.patterns, tt.value, got, tt.want)
		}
	}
}

func TestRuleWithoutCommandPatterns(t *testing.T) {
	engine := NewEngine()
	engine.AddRule(Rule{
		Name:     "allow-all-read",
		Tools:    []string{"read_file"},
		Decision: DecisionAllow,
		Priority: 5,
	})

	got, _ := engine.Evaluate("read_file", "")
	if got != DecisionAllow {
		t.Errorf("expected allow for tool-only rule, got %s", got)
	}
}

func TestRulePriority(t *testing.T) {
	engine := NewEngine()

	// Lower priority: deny all bash.
	engine.AddRule(Rule{
		Name:     "deny-all-bash",
		Tools:    []string{"bash"},
		Decision: DecisionDeny,
		Priority: 5,
	})
	// Higher priority: allow git commands.
	engine.AddRule(Rule{
		Name:            "allow-git",
		Tools:           []string{"bash"},
		CommandPatterns: []string{"git *"},
		Decision:        DecisionAllow,
		Priority:        10,
	})

	got, _ := engine.Evaluate("bash", "git commit")
	if got != DecisionAllow {
		t.Errorf("expected higher-priority allow rule to win, got %s", got)
	}

	// Non-git command should hit the lower-priority deny.
	got2, _ := engine.Evaluate("bash", "ls -la")
	if got2 != DecisionDeny {
		t.Errorf("expected deny for non-git command, got %s", got2)
	}
}
