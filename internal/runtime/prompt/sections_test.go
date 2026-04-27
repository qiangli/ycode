package prompt

import (
	"strings"
	"testing"
)

func TestProjectSection_InitialGitStatusLabel(t *testing.T) {
	ctx := &ProjectContext{
		WorkDir:   "/tmp/project",
		GitStatus: "## main\n M file.go\n?? new.txt",
	}

	result := ProjectSection(ctx)

	if !strings.Contains(result, "Initial git status:") {
		t.Error("should label git status")
	}
	if !strings.Contains(result, "M file.go") {
		t.Error("should include the actual git status content")
	}
}

func TestProjectSection_InitialGitDiffLabel(t *testing.T) {
	ctx := &ProjectContext{
		WorkDir: "/tmp/project",
		GitDiff: "diff --git a/main.go b/main.go\n+added line",
	}

	result := ProjectSection(ctx)

	if !strings.Contains(result, "Initial git diff:") {
		t.Error("should label git diff")
	}
	if !strings.Contains(result, "+added line") {
		t.Error("should include the actual git diff content")
	}
}

func TestProjectSection_OmitsEmptyGitFields(t *testing.T) {
	ctx := &ProjectContext{
		WorkDir: "/tmp/project",
	}

	result := ProjectSection(ctx)

	if strings.Contains(result, "Initial git status") {
		t.Error("should not include git status label when status is empty")
	}
	if strings.Contains(result, "Initial git diff") {
		t.Error("should not include git diff label when diff is empty")
	}
}

func TestProjectSection_RecentCommitsCappedAt3(t *testing.T) {
	ctx := &ProjectContext{
		WorkDir:       "/tmp/project",
		RecentCommits: []string{"a fix: one", "b feat: two", "c docs: three", "d test: four", "e ci: five"},
	}

	result := ProjectSection(ctx)

	if !strings.Contains(result, "Recent commits (3):") {
		t.Error("should include recent commits section capped at 3")
	}
	if !strings.Contains(result, "a fix: one") {
		t.Error("should include first commit")
	}
	if !strings.Contains(result, "c docs: three") {
		t.Error("should include third commit")
	}
	if strings.Contains(result, "d test: four") {
		t.Error("should NOT include fourth commit (capped at 3)")
	}
}

func TestProjectSection_GitDiffTruncation(t *testing.T) {
	ctx := &ProjectContext{
		WorkDir: "/tmp/project",
		GitDiff: strings.Repeat("x", 2000),
	}

	result := ProjectSection(ctx)

	if !strings.Contains(result, "diff truncated") {
		t.Error("large git diff should be truncated")
	}
}

func TestProjectSection_AllGitFieldsTogether(t *testing.T) {
	ctx := &ProjectContext{
		WorkDir:       "/tmp/project",
		GitStatus:     "## main\n M app.go",
		GitDiff:       "diff --git a/app.go b/app.go",
		RecentCommits: []string{"abc fix: test"},
	}

	result := ProjectSection(ctx)

	statusIdx := strings.Index(result, "Initial git status")
	commitsIdx := strings.Index(result, "Recent commits")
	diffIdx := strings.Index(result, "Initial git diff")

	if statusIdx < 0 || commitsIdx < 0 || diffIdx < 0 {
		t.Fatal("all three git sections should be present")
	}

	// Verify ordering: status, then commits, then diff.
	if statusIdx > commitsIdx {
		t.Error("git status should appear before recent commits")
	}
	if commitsIdx > diffIdx {
		t.Error("recent commits should appear before git diff")
	}
}

func TestProjectSection_NoDuplicateDateOrWorkDir(t *testing.T) {
	ctx := &ProjectContext{
		WorkDir:     "/tmp/project",
		CurrentDate: "2026-04-17",
	}

	result := ProjectSection(ctx)

	// Date and working directory should NOT be in ProjectSection (they're in EnvironmentSection).
	if strings.Contains(result, "2026-04-17") {
		t.Error("should not duplicate date (already in EnvironmentSection)")
	}
	if strings.Contains(result, "Working directory:") {
		t.Error("should not duplicate working directory (already in EnvironmentSection)")
	}
}

func TestGitSection_BranchAndUser(t *testing.T) {
	ctx := &ProjectContext{
		IsGitRepo:  true,
		GitBranch:  "feature/test",
		MainBranch: "main",
		GitUser:    "Test User",
	}

	result := GitSection(ctx)

	if !strings.Contains(result, "feature/test") {
		t.Error("should include current branch")
	}
	if !strings.Contains(result, "Main branch: main") {
		t.Error("should include main branch")
	}
	if !strings.Contains(result, "Git user: Test User") {
		t.Error("should include git user")
	}
}

func TestGitSection_NotARepo(t *testing.T) {
	ctx := &ProjectContext{
		IsGitRepo: false,
	}

	result := GitSection(ctx)

	if result != "" {
		t.Errorf("should return empty string for non-repo, got %q", result)
	}
}

// --- DiagnosticsSection tests ---

func TestDiagnosticsSection_NilReturnsEmpty(t *testing.T) {
	result := DiagnosticsSection(nil)
	if result != "" {
		t.Errorf("nil diagnostics should return empty string, got %q", result)
	}
}

func TestDiagnosticsSection_NoDiagnosticsReturnsEmpty(t *testing.T) {
	diag := &DiagnosticsInfo{}
	result := DiagnosticsSection(diag)
	if result != "" {
		t.Errorf("empty diagnostics should return empty string, got %q", result)
	}
}

func TestDiagnosticsSection_DegradedTools(t *testing.T) {
	diag := &DiagnosticsInfo{
		DegradedTools: []DegradedTool{
			{Name: "bash", SuccessRate: 0.4, TotalCalls: 10, FailureCount: 6},
		},
	}

	result := DiagnosticsSection(diag)

	if !strings.Contains(result, "# Runtime diagnostics") {
		t.Error("should contain diagnostics header")
	}
	if !strings.Contains(result, `"bash"`) {
		t.Error("should mention the degraded tool name")
	}
	if !strings.Contains(result, "failed 6/10") {
		t.Error("should show failure count and total calls")
	}
	if !strings.Contains(result, "40% success") {
		t.Error("should show success rate percentage")
	}
}

func TestDiagnosticsSection_ContextHealthWarning(t *testing.T) {
	diag := &DiagnosticsInfo{
		ContextHealthPct:   72,
		ContextHealthLevel: "warning",
	}

	result := DiagnosticsSection(diag)

	if !strings.Contains(result, "Context usage: 72%") {
		t.Error("should show context usage percentage")
	}
	if !strings.Contains(result, "compact_context") {
		t.Error("should suggest compact_context for warning level")
	}
}

func TestDiagnosticsSection_ContextHealthCritical(t *testing.T) {
	diag := &DiagnosticsInfo{
		ContextHealthPct:   85,
		ContextHealthLevel: "critical",
	}

	result := DiagnosticsSection(diag)

	if !strings.Contains(result, "Compact immediately") {
		t.Error("should urge immediate compaction for critical level")
	}
}

func TestDiagnosticsSection_ContextHealthHealthy(t *testing.T) {
	diag := &DiagnosticsInfo{
		ContextHealthPct:   40,
		ContextHealthLevel: "healthy",
	}

	result := DiagnosticsSection(diag)

	if result != "" {
		t.Errorf("healthy context should not produce diagnostics, got %q", result)
	}
}

func TestDiagnosticsSection_PriorSessionSummary(t *testing.T) {
	diag := &DiagnosticsInfo{
		PriorSessionSummary: "Fixed auth middleware; 3 test failures remaining.",
	}

	result := DiagnosticsSection(diag)

	if !strings.Contains(result, "Prior session context:") {
		t.Error("should show prior session context label")
	}
	if !strings.Contains(result, "Fixed auth middleware") {
		t.Error("should include the summary content")
	}
}

func TestDiagnosticsSection_CombinedDiagnostics(t *testing.T) {
	diag := &DiagnosticsInfo{
		DegradedTools: []DegradedTool{
			{Name: "grep_search", SuccessRate: 0.5, TotalCalls: 8, FailureCount: 4},
		},
		ContextHealthPct:    75,
		ContextHealthLevel:  "warning",
		PriorSessionSummary: "Refactoring config parser.",
	}

	result := DiagnosticsSection(diag)

	if !strings.Contains(result, "grep_search") {
		t.Error("should show degraded tool")
	}
	if !strings.Contains(result, "Context usage: 75%") {
		t.Error("should show context health")
	}
	if !strings.Contains(result, "Refactoring config parser") {
		t.Error("should show prior session summary")
	}
}

func TestContextHealthAdvice(t *testing.T) {
	tests := []struct {
		level    string
		contains string
	}{
		{"warning", "compact_context"},
		{"critical", "Compact immediately"},
		{"overflow", "imminent"},
		{"healthy", ""},
	}

	for _, tt := range tests {
		result := contextHealthAdvice(tt.level)
		if tt.contains == "" {
			if result != "" {
				t.Errorf("level %q: expected empty advice, got %q", tt.level, result)
			}
		} else if !strings.Contains(result, tt.contains) {
			t.Errorf("level %q: expected advice containing %q, got %q", tt.level, tt.contains, result)
		}
	}
}
