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

	if !strings.Contains(result, "Initial git status (captured at session start):") {
		t.Error("should label git status as initial snapshot captured at session start")
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

	if !strings.Contains(result, "Initial git diff (captured at session start):") {
		t.Error("should label git diff as initial snapshot captured at session start")
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

func TestProjectSection_RecentCommits(t *testing.T) {
	ctx := &ProjectContext{
		WorkDir:       "/tmp/project",
		RecentCommits: []string{"abc1234 fix: something", "def5678 feat: another"},
	}

	result := ProjectSection(ctx)

	if !strings.Contains(result, "Recent commits (last 5):") {
		t.Error("should include recent commits section")
	}
	if !strings.Contains(result, "abc1234 fix: something") {
		t.Error("should include commit entries")
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
