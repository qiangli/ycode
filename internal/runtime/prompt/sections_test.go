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
