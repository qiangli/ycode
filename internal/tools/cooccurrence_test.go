package tools

import (
	"testing"
)

func TestCoOccurrence_BasicClustering(t *testing.T) {
	co := NewCoOccurrence()

	// Simulate 5 sessions where grep_search → read_file → edit_file.
	for range 5 {
		co.RecordSession([]string{"grep_search", "read_file", "edit_file"})
	}

	// grep_search should co-activate read_file and edit_file.
	result := co.CoActivate("grep_search", 5)
	if len(result) != 2 {
		t.Errorf("expected 2 co-activated tools, got %d: %v", len(result), result)
	}
	if !sliceContains(result, "read_file") {
		t.Error("expected read_file to be co-activated with grep_search")
	}
	if !sliceContains(result, "edit_file") {
		t.Error("expected edit_file to be co-activated with grep_search")
	}

	// edit_file is terminal — used last in all 5 sessions.
	if !co.IsTerminal("edit_file") {
		t.Error("edit_file should be terminal (always used last)")
	}

	// grep_search is not terminal.
	if co.IsTerminal("grep_search") {
		t.Error("grep_search should NOT be terminal")
	}
}

func TestCoOccurrence_DirectionalRelationship(t *testing.T) {
	co := NewCoOccurrence()

	// A → B → C pattern (5 times).
	for range 5 {
		co.RecordSession([]string{"git_status", "git_add", "git_commit"})
	}

	// git_status should lead to git_add and git_commit.
	result := co.CoActivate("git_status", 5)
	if !sliceContains(result, "git_add") {
		t.Error("git_status → git_add missing")
	}
	if !sliceContains(result, "git_commit") {
		t.Error("git_status → git_commit missing")
	}

	// git_commit is terminal — should NOT co-activate anything.
	result = co.CoActivate("git_commit", 5)
	if len(result) != 0 {
		t.Errorf("terminal tool git_commit should not co-activate others, got: %v", result)
	}
}

func TestCoOccurrence_TerminalToolDoesNotExpand(t *testing.T) {
	co := NewCoOccurrence()

	// B is always terminal (used alone).
	for range 5 {
		co.RecordSession([]string{"read_file"})
	}

	// read_file should be terminal.
	if !co.IsTerminal("read_file") {
		t.Error("read_file should be terminal when always used alone")
	}

	// Should not co-activate anything.
	result := co.CoActivate("read_file", 5)
	if len(result) != 0 {
		t.Errorf("terminal tool should not co-activate, got: %v", result)
	}
}

func TestCoOccurrence_MinSupport(t *testing.T) {
	co := NewCoOccurrence()

	// Only 2 sessions of A→B (below default minSupport=3).
	co.RecordSession([]string{"tool_a", "tool_b"})
	co.RecordSession([]string{"tool_a", "tool_b"})

	result := co.CoActivate("tool_a", 5)
	if len(result) != 0 {
		t.Errorf("should not co-activate with only 2 occurrences (below minSupport=3), got: %v", result)
	}

	// Add one more — now meets threshold.
	co.RecordSession([]string{"tool_a", "tool_b"})
	result = co.CoActivate("tool_a", 5)
	if len(result) != 1 || result[0] != "tool_b" {
		t.Errorf("expected [tool_b] after meeting minSupport, got: %v", result)
	}
}

func TestCoOccurrence_MinConfidence(t *testing.T) {
	co := NewCoOccurrence()

	// A→B in 3 out of 10 sessions (30% confidence, right at threshold).
	for range 3 {
		co.RecordSession([]string{"tool_a", "tool_b"})
	}
	for range 7 {
		co.RecordSession([]string{"tool_a", "tool_c"})
	}

	result := co.CoActivate("tool_a", 5)

	// tool_b at 30% confidence should be included (>= threshold).
	if !sliceContains(result, "tool_b") {
		t.Error("tool_b should be included at 30% confidence (= threshold)")
	}
	// tool_c at 70% confidence should be included.
	if !sliceContains(result, "tool_c") {
		t.Error("tool_c should be included at 70% confidence")
	}

	// tool_c should rank higher (70% > 30%).
	if len(result) >= 2 && result[0] != "tool_c" {
		t.Errorf("expected tool_c first (higher confidence), got: %v", result)
	}
}

func TestCoOccurrence_DeduplicateConsecutive(t *testing.T) {
	co := NewCoOccurrence()

	// Consecutive duplicates should be collapsed: A,A,B,B,C → A,B,C.
	for range 5 {
		co.RecordSession([]string{"tool_a", "tool_a", "tool_b", "tool_b", "tool_c"})
	}

	result := co.CoActivate("tool_a", 5)
	if !sliceContains(result, "tool_b") || !sliceContains(result, "tool_c") {
		t.Errorf("expected tool_b and tool_c, got: %v", result)
	}

	// tool_a should appear only once in totalSessions.
	stats := co.Stats("tool_a")
	if stats["tool_b"] != 5 {
		t.Errorf("expected tool_b count=5 (deduped), got: %d", stats["tool_b"])
	}
}

func TestCoOccurrence_MaxResults(t *testing.T) {
	co := NewCoOccurrence()

	// A leads to B, C, D, E, F.
	for range 10 {
		co.RecordSession([]string{"trigger", "b", "c", "d", "e", "f"})
	}

	result := co.CoActivate("trigger", 2)
	if len(result) != 2 {
		t.Errorf("expected 2 results (maxResults=2), got %d: %v", len(result), result)
	}
}

func TestCoOccurrence_EmptySession(t *testing.T) {
	co := NewCoOccurrence()

	// Empty and single-tool sessions should not crash.
	co.RecordSession(nil)
	co.RecordSession([]string{})
	co.RecordSession([]string{"single_tool"})

	result := co.CoActivate("single_tool", 5)
	if len(result) != 0 {
		t.Errorf("single-tool session should not co-activate, got: %v", result)
	}
}

func TestCoOccurrence_AllClusters(t *testing.T) {
	co := NewCoOccurrence()

	for range 5 {
		co.RecordSession([]string{"grep_search", "read_file", "edit_file"})
		co.RecordSession([]string{"git_status", "git_add", "git_commit"})
	}

	clusters := co.AllClusters()

	// grep_search and git_status should have clusters.
	if _, ok := clusters["grep_search"]; !ok {
		t.Error("expected cluster for grep_search")
	}
	if _, ok := clusters["git_status"]; !ok {
		t.Error("expected cluster for git_status")
	}

	// Terminal tools (edit_file, git_commit) should NOT have clusters.
	if _, ok := clusters["edit_file"]; ok {
		t.Error("terminal tool edit_file should not have a cluster")
	}
	if _, ok := clusters["git_commit"]; ok {
		t.Error("terminal tool git_commit should not have a cluster")
	}
}

func TestCoOccurrence_SetThresholds(t *testing.T) {
	co := NewCoOccurrence()
	co.SetThresholds(5, 0.5)

	// 3 sessions of A→B (below new minSupport=5).
	for range 3 {
		co.RecordSession([]string{"tool_a", "tool_b"})
	}

	result := co.CoActivate("tool_a", 5)
	if len(result) != 0 {
		t.Errorf("should not co-activate below new minSupport=5, got: %v", result)
	}

	// Add 2 more — now meets threshold.
	for range 2 {
		co.RecordSession([]string{"tool_a", "tool_b"})
	}

	result = co.CoActivate("tool_a", 5)
	if len(result) != 1 {
		t.Errorf("expected 1 result after meeting minSupport=5, got: %v", result)
	}
}

func TestCoOccurrence_MixedPatterns(t *testing.T) {
	co := NewCoOccurrence()

	// Pattern 1: explore workflow (5 times).
	for range 5 {
		co.RecordSession([]string{"grep_search", "read_file", "semantic_search"})
	}

	// Pattern 2: commit workflow (5 times).
	for range 5 {
		co.RecordSession([]string{"git_status", "git_add", "git_commit"})
	}

	// Pattern 3: read_file used alone (5 times — makes it also terminal).
	for range 5 {
		co.RecordSession([]string{"read_file"})
	}

	// grep_search → read_file, semantic_search.
	result := co.CoActivate("grep_search", 5)
	if !sliceContains(result, "read_file") {
		t.Error("expected read_file co-activated with grep_search")
	}

	// read_file is both a follower AND used alone. IsTerminal checks terminal rate.
	// Terminal count: 5, Total sessions: 10 → 50% < 70% → NOT terminal.
	if co.IsTerminal("read_file") {
		t.Error("read_file should NOT be terminal (only 50% terminal rate)")
	}

	// git_commit is terminal (100% terminal rate).
	if !co.IsTerminal("git_commit") {
		t.Error("git_commit should be terminal")
	}
}

func sliceContains(slice []string, item string) bool {
	for _, s := range slice {
		if s == item {
			return true
		}
	}
	return false
}
