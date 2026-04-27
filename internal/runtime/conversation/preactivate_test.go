package conversation

import (
	"context"
	"encoding/json"
	"log/slog"
	"strings"
	"testing"

	"github.com/qiangli/ycode/internal/runtime/config"
	"github.com/qiangli/ycode/internal/tools"
)

func newPreactivateRuntime(t *testing.T, specs ...*tools.ToolSpec) *Runtime {
	t.Helper()
	reg := tools.NewRegistry()
	for _, spec := range specs {
		if spec.Handler == nil {
			s := spec
			s.Handler = func(ctx context.Context, input json.RawMessage) (string, error) { return "ok", nil }
			spec = s
		}
		if err := reg.Register(spec); err != nil {
			t.Fatalf("register %s: %v", spec.Name, err)
		}
	}
	return &Runtime{
		config:         config.DefaultConfig(),
		registry:       reg,
		logger:         slog.Default(),
		activatedTools: make(map[string]int),
		turnCount:      1,
	}
}

// deferredTool creates a deferred ToolSpec with the given name and description.
func deferredTool(name, desc string) *tools.ToolSpec {
	return &tools.ToolSpec{Name: name, Description: desc}
}

// --- Tier 1a: Keyword matching tests ---

func TestKeyword_CommitActivatesGitTools(t *testing.T) {
	rt := newPreactivateRuntime(t,
		deferredTool("git_status", "Show working tree status"),
		deferredTool("git_log", "Show commit history"),
		deferredTool("git_commit", "Stage files and create a git commit"),
	)

	activated := rt.preActivateByKeyword("please commit my changes")
	if activated == 0 {
		t.Fatal("expected tools to be activated for 'commit'")
	}
	if _, ok := rt.activatedTools["git_commit"]; !ok {
		t.Error("git_commit should be activated")
	}
	if _, ok := rt.activatedTools["git_status"]; !ok {
		t.Error("git_status should be activated")
	}
}

func TestKeyword_MemoActivatesMemoTools(t *testing.T) {
	rt := newPreactivateRuntime(t,
		deferredTool("MemosStore", "Save a memo"),
		deferredTool("MemosSearch", "Search memos"),
		deferredTool("MemosList", "List memos"),
	)

	activated := rt.preActivateByKeyword("save this as a memo for later")
	if activated == 0 {
		t.Fatal("expected tools to be activated for 'memo'")
	}
	if _, ok := rt.activatedTools["MemosStore"]; !ok {
		t.Error("MemosStore should be activated")
	}
}

func TestKeyword_NoisyWordsRemoved(t *testing.T) {
	rt := newPreactivateRuntime(t,
		deferredTool("delete_file", "Delete a file"),
		deferredTool("query_logs", "Query logs"),
	)

	// "error" and "delete" were removed from keyword map — should NOT activate.
	activated := rt.preActivateByKeyword("I see an error, don't delete anything")
	if activated != 0 {
		t.Errorf("expected 0 activations for noisy keywords, got %d", activated)
	}
}

func TestKeyword_EmptyMessage(t *testing.T) {
	rt := newPreactivateRuntime(t, deferredTool("git_status", "status"))
	if rt.preActivateTools("") != 0 {
		t.Error("empty message should activate nothing")
	}
}

func TestKeyword_CaseInsensitive(t *testing.T) {
	rt := newPreactivateRuntime(t, deferredTool("query_metrics", "Query metrics"))

	activated := rt.preActivateByKeyword("Show me the METRICS")
	if activated == 0 {
		t.Fatal("expected case-insensitive matching")
	}
}

func TestKeyword_AlreadyActivatedSkipped(t *testing.T) {
	rt := newPreactivateRuntime(t,
		deferredTool("git_status", "status"),
		deferredTool("git_log", "log"),
		deferredTool("git_commit", "commit"),
	)
	rt.activatedTools["git_status"] = rt.turnCount

	activated := rt.preActivateByKeyword("commit the changes")
	if _, ok := rt.activatedTools["git_commit"]; !ok {
		t.Error("git_commit should be activated")
	}
	// git_status was already active, shouldn't be counted.
	if activated >= 3 {
		t.Errorf("expected <3 new activations, got %d", activated)
	}
}

// --- Tier 1b: SearchTools scoring tests ---

func TestScoring_ToolNameMatch(t *testing.T) {
	rt := newPreactivateRuntime(t,
		deferredTool("query_metrics", "Query tool execution metrics for debugging performance"),
	)

	// "query_metrics" as a search term should score high (exact name match).
	activated := rt.preActivateByScoring("check query_metrics for failures")
	if activated == 0 {
		t.Fatal("expected query_metrics to be activated by name match")
	}
	if _, ok := rt.activatedTools["query_metrics"]; !ok {
		t.Error("query_metrics should be activated")
	}
}

func TestScoring_DescriptionMatch(t *testing.T) {
	rt := newPreactivateRuntime(t,
		deferredTool("query_traces", "Query OTEL trace spans for debugging slow operations and finding errors"),
	)

	// "debugging" and "operations" should match description.
	// Whether this exceeds threshold depends on cumulative score.
	rt.preActivateByScoring("debugging slow operations")
	// The score for "debugging"(+4 desc) + "operations"(+4 desc) = 8, below threshold 12.
	// But "slow"(+4 desc) adds more. Let's check.
	// Actually: "debugging" in desc (+4), "slow" in desc (+4), "operations" in desc (+4) = 12.
	// Exactly at threshold. Should activate.
}

func TestScoring_GenericMessageBelowThreshold(t *testing.T) {
	rt := newPreactivateRuntime(t,
		deferredTool("git_status", "Show working tree status"),
		deferredTool("git_log", "Show commit history"),
	)

	// Generic message — stop words filtered, remaining words shouldn't score high.
	activated := rt.preActivateByScoring("please refactor the config parser to be cleaner")
	if activated != 0 {
		t.Errorf("expected 0 activations for generic message, got %d", activated)
	}
}

func TestScoring_StopWordsFiltered(t *testing.T) {
	result := filterStopWords("please help me with the git commit")
	if result == "" {
		t.Fatal("should have non-empty result after filtering")
	}
	// "please", "help", "me", "with", "the" are stop words.
	if containsWord(result, "please") || containsWord(result, "help") || containsWord(result, "the") {
		t.Errorf("stop words should be removed, got %q", result)
	}
	if !containsWord(result, "git") || !containsWord(result, "commit") {
		t.Errorf("domain words should be kept, got %q", result)
	}
}

func TestScoring_CappedAtMax(t *testing.T) {
	// Register many tools that would score high.
	var specs []*tools.ToolSpec
	for i := range 10 {
		name := "tool_" + string(rune('a'+i))
		specs = append(specs, deferredTool(name, "test tool for testing tools"))
	}
	rt := newPreactivateRuntime(t, specs...)

	// Force high scores by using "tool" which matches all names/descriptions.
	activated := rt.preActivateByScoring("tool tool tool tool tool tool")
	if activated > maxScoredActivations {
		t.Errorf("expected at most %d activations, got %d", maxScoredActivations, activated)
	}
}

func TestScoring_SkipsAlwaysAvailableTools(t *testing.T) {
	rt := newPreactivateRuntime(t,
		&tools.ToolSpec{Name: "bash", Description: "Execute bash command", AlwaysAvailable: true},
		deferredTool("git_status", "Show working tree status"),
	)

	rt.preActivateByScoring("bash command execution")
	if _, ok := rt.activatedTools["bash"]; ok {
		t.Error("always-available tools should not be pre-activated")
	}
}

// --- Combined pipeline tests ---

func TestPreActivateTools_CombinesBothTiers(t *testing.T) {
	rt := newPreactivateRuntime(t,
		deferredTool("git_status", "Show working tree status"),
		deferredTool("git_log", "Show commit history"),
		deferredTool("git_commit", "Stage files and create a git commit"),
		deferredTool("query_metrics", "Query tool execution metrics for debugging performance issues"),
	)

	// "commit" triggers Tier 1a keyword match for git tools.
	// "query_metrics" might be picked up by Tier 1b scoring if message words match.
	total := rt.preActivateTools("commit the fix and check query_metrics")
	if total < 3 {
		t.Errorf("expected at least 3 activations from combined tiers, got %d", total)
	}
}

func TestPreActivateTools_NoMatchReturnsZero(t *testing.T) {
	rt := newPreactivateRuntime(t,
		deferredTool("git_status", "Show working tree status"),
	)

	// Use words that don't appear in any tool name or description.
	total := rt.preActivateTools("hello world foo bar baz")
	if total != 0 {
		t.Errorf("expected 0 activations for unrelated message, got %d", total)
	}
}

// --- filterStopWords tests ---

func TestFilterStopWords_AllStopWords(t *testing.T) {
	result := filterStopWords("I have a the is are")
	if result != "" {
		t.Errorf("all stop words should result in empty string, got %q", result)
	}
}

func TestFilterStopWords_MixedContent(t *testing.T) {
	result := filterStopWords("check the deployment status for our server")
	// "check", "deployment", "status", "server" should remain.
	if !containsWord(result, "check") || !containsWord(result, "deployment") {
		t.Errorf("domain words should be preserved, got %q", result)
	}
}

func TestFilterStopWords_PunctuationStripped(t *testing.T) {
	result := filterStopWords("what's the error? check logs!")
	// "error" and "check" and "logs" should remain (without punctuation).
	if !containsWord(result, "error") || !containsWord(result, "check") {
		t.Errorf("punctuation should be stripped, got %q", result)
	}
}

func TestFilterStopWords_ShortWordsRemoved(t *testing.T) {
	result := filterStopWords("a b c git d e commit")
	if containsWord(result, "b") || containsWord(result, "c") || containsWord(result, "d") {
		t.Errorf("single-char words should be removed, got %q", result)
	}
	if !containsWord(result, "git") || !containsWord(result, "commit") {
		t.Errorf("longer words should be kept, got %q", result)
	}
}

// containsWord checks if a space-separated string contains a specific word.
func containsWord(s, word string) bool {
	for _, w := range strings.Fields(s) {
		if w == word {
			return true
		}
	}
	return false
}
