package commands

import (
	"context"
	"os"
	"path/filepath"
	"sort"
	"testing"

	"github.com/qiangli/ycode/internal/runtime/config"
	"github.com/qiangli/ycode/internal/runtime/session"
)

func newTestRegistry(t *testing.T) *Registry {
	workDir := t.TempDir()

	// Create a CLAUDE.md in the temp dir so /memory and /context find it.
	if err := os.WriteFile(filepath.Join(workDir, "CLAUDE.md"), []byte("# Test project\nSome instructions here."), 0o644); err != nil {
		t.Fatal(err)
	}

	cfg := config.DefaultConfig()
	cfg.Model = "test-model"

	sess := &session.Session{
		ID: "test-session-123",
		Messages: []session.ConversationMessage{
			{Role: session.RoleUser, Content: []session.ContentBlock{{Type: session.ContentTypeText, Text: "hello"}}},
			{
				Role:    session.RoleAssistant,
				Content: []session.ContentBlock{{Type: session.ContentTypeText, Text: "hi"}},
				Usage:   &session.TokenUsage{InputTokens: 100, OutputTokens: 50},
			},
		},
	}

	r := NewRegistry()
	RegisterBuiltins(r, &RuntimeDeps{
		SessionID:    sess.ID,
		MessageCount: sess.MessageCount,
		Model:        func() string { return cfg.Model },
		ProviderKind: func() string { return "anthropic" },
		CostSummary:  func() string { return "Input: 1000, Output: 500, Cost: $0.01" },
		Version:      "v0.1.0-test",
		WorkDir:      workDir,
		Config:       cfg,
		ConfigDirs: ConfigDirs{
			UserDir:    filepath.Join(workDir, "user-config"),
			ProjectDir: filepath.Join(workDir, ".agents", "ycode"),
			LocalDir:   filepath.Join(workDir, ".agents", "ycode"),
		},
		MemoryDir: filepath.Join(workDir, "memory"),
		Session:   sess,
	})
	return r
}

// TestAllCommandsRegistered verifies every expected command is in the registry.
func TestAllCommandsRegistered(t *testing.T) {
	r := newTestRegistry(t)

	expected := []string{
		// session
		"help", "status", "cost", "version", "model", "retry", "revert", "rename", "search",
		// workspace
		"clear", "compact", "config", "export", "init", "memory",
		// discovery
		"doctor", "context", "skills", "tasks",
		// automation
		"commit", "review", "advisor", "security-review", "team", "cron", "loop",
		// plugin
		"plugin",
	}

	for _, name := range expected {
		if _, ok := r.Get(name); !ok {
			t.Errorf("command %q is NOT registered", name)
		}
	}

	// Also verify total count matches so stale entries are caught.
	all := r.List()
	if len(all) != len(expected) {
		got := make([]string, len(all))
		for i, s := range all {
			got[i] = s.Name
		}
		sort.Strings(got)
		t.Errorf("expected %d commands, got %d: %v", len(expected), len(all), got)
	}
}

// TestAllCommandsExecute verifies every command can be invoked without panic.
func TestAllCommandsExecute(t *testing.T) {
	r := newTestRegistry(t)
	ctx := context.Background()

	// Commands that are expected to return errors when deps are nil.
	errExpected := map[string]bool{
		"retry":  true,
		"revert": true,
		"rename": true, // requires args
		"commit": true, // requires Provider
		"search": true, // requires args
	}

	for _, spec := range r.List() {
		t.Run(spec.Name, func(t *testing.T) {
			output, err := r.Execute(ctx, spec.Name, "")
			if errExpected[spec.Name] {
				if err == nil {
					t.Fatalf("/%s expected error (nil deps), got output: %s", spec.Name, output)
				}
				return
			}
			if err != nil {
				t.Fatalf("/%s returned error: %v", spec.Name, err)
			}
			if output == "" {
				t.Errorf("/%s returned empty output", spec.Name)
			}
		})
	}
}

// TestHelpListsAllCommands verifies /help output mentions every registered command.
func TestHelpListsAllCommands(t *testing.T) {
	r := newTestRegistry(t)
	ctx := context.Background()

	output, err := r.Execute(ctx, "help", "")
	if err != nil {
		t.Fatalf("/help error: %v", err)
	}

	for _, spec := range r.List() {
		needle := "/" + spec.Name
		if !containsStr(output, needle) {
			t.Errorf("/help output does not mention %s", needle)
		}
	}

	// Verify built-in exit commands are listed.
	for _, name := range []string{"/quit", "/exit"} {
		if !containsStr(output, name) {
			t.Errorf("/help output does not mention %s", name)
		}
	}
}

// TestStatusShowsLiveCount verifies /status calls the MessageCount function.
func TestStatusShowsLiveCount(t *testing.T) {
	r := newTestRegistry(t)
	ctx := context.Background()

	output, err := r.Execute(ctx, "status", "")
	if err != nil {
		t.Fatalf("/status error: %v", err)
	}
	if !containsStr(output, "2") {
		t.Errorf("/status should show live message count 2, got: %s", output)
	}
	if !containsStr(output, "test-session-123") {
		t.Errorf("/status should show session ID, got: %s", output)
	}
	if !containsStr(output, "test-model") {
		t.Errorf("/status should show model, got: %s", output)
	}
}

// TestCostShowsSummary verifies /cost calls the CostSummary function.
func TestCostShowsSummary(t *testing.T) {
	r := newTestRegistry(t)
	ctx := context.Background()

	output, err := r.Execute(ctx, "cost", "")
	if err != nil {
		t.Fatalf("/cost error: %v", err)
	}
	if !containsStr(output, "$0.01") {
		t.Errorf("/cost should show cost summary, got: %s", output)
	}
}

// TestVersionShowsVersion verifies /version shows the version string.
func TestVersionShowsVersion(t *testing.T) {
	r := newTestRegistry(t)
	ctx := context.Background()

	output, err := r.Execute(ctx, "version", "")
	if err != nil {
		t.Fatalf("/version error: %v", err)
	}
	if !containsStr(output, "v0.1.0-test") {
		t.Errorf("/version should show version string, got: %s", output)
	}
}

// TestCommandsWithSubcommands verifies commands that accept subcommands.
func TestCommandsWithSubcommands(t *testing.T) {
	r := newTestRegistry(t)
	ctx := context.Background()

	tests := []struct {
		name string
		args string
		want string // substring expected in output
	}{
		// /memory
		{"memory", "", "Instruction files"},
		{"memory", "", "CLAUDE.md"},
		// /config
		{"config", "", "Merged settings"},
		{"config", "model", "test-model"},
		{"config", "model", "anthropic"},
		{"config", "permissions", "ask"},
		{"config", "memory", "autoMemory"},
		{"config", "session", "autoCompact"},
		{"config", "badarg", "Unknown section"},
		// /context
		{"context", "", "test-model"},
		{"context", "", "Instruction files"},
		// /review
		{"review", "", "staged"},
		{"review", "branch", "branch"},
		// /advisor
		{"advisor", "", "general architecture"},
		{"advisor", "performance", "performance"},
		// /security-review
		{"security-review", "", "staged changes"},
		{"security-review", "src/", "src/"},
		// /team
		{"team", "", "Usage"},
		{"team", "list", "No active teams"},
		{"team", "create myteam", "myteam"},
		{"team", "delete myteam", "myteam"},
		// /cron
		{"cron", "", "Usage"},
		{"cron", "list", "No scheduled tasks"},
		{"cron", "create myjob 5m /review", "myjob"},
		{"cron", "delete myjob", "myjob"},
		// /loop
		{"loop", "", "Usage"},
		{"loop", "stop", "stopped"},
		{"loop", "5m /review", "5m"},
		// /plugin
		{"plugin", "", "Usage"},
		{"plugin", "list", "none"},
		{"plugin", "install myplugin", "myplugin"},
		{"plugin", "enable myplugin", "myplugin"},
		{"plugin", "disable myplugin", "myplugin"},
		{"plugin", "uninstall myplugin", "myplugin"},
		{"plugin", "update myplugin", "myplugin"},
		{"plugin", "update", "Updating"},
		// /skills
		{"skills", "", "No skills found"},
		{"skills", "install-bundled", "Installing bundled"},
	}

	for _, tt := range tests {
		label := tt.name
		if tt.args != "" {
			label += " " + tt.args
		}
		t.Run(label, func(t *testing.T) {
			output, err := r.Execute(ctx, tt.name, tt.args)
			if err != nil {
				t.Fatalf("/%s %s returned error: %v", tt.name, tt.args, err)
			}
			if !containsStr(output, tt.want) {
				t.Errorf("/%s %s: expected output to contain %q, got:\n%s", tt.name, tt.args, tt.want, output)
			}
		})
	}
}

// TestUnknownCommand verifies unknown commands return an error.
func TestUnknownCommand(t *testing.T) {
	r := newTestRegistry(t)
	ctx := context.Background()

	_, err := r.Execute(ctx, "nonexistent", "")
	if err == nil {
		t.Error("expected error for unknown command")
	}
}

// TestCategoriesAreNonEmpty verifies all expected categories have commands.
func TestCategoriesAreNonEmpty(t *testing.T) {
	r := newTestRegistry(t)
	cats := r.ListByCategory()

	expectedCats := []string{"session", "workspace", "discovery", "automation", "plugin"}
	for _, cat := range expectedCats {
		specs, ok := cats[cat]
		if !ok || len(specs) == 0 {
			t.Errorf("category %q is missing or empty", cat)
		}
	}
}

func containsStr(s, substr string) bool {
	return len(s) >= len(substr) && indexOf(s, substr) >= 0
}

func indexOf(s, sub string) int {
	for i := 0; i+len(sub) <= len(s); i++ {
		if s[i:i+len(sub)] == sub {
			return i
		}
	}
	return -1
}
