package ralph

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
)

func TestNewBashCheckFunc_Pass(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping in short mode")
	}

	check := NewBashCheckFunc("true")
	passed, output, err := check(context.Background())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !passed {
		t.Fatalf("expected pass, got fail; output: %s", output)
	}
}

func TestNewBashCheckFunc_Fail(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping in short mode")
	}

	check := NewBashCheckFunc("echo 'test failed'; exit 1")
	passed, output, err := check(context.Background())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if passed {
		t.Fatal("expected fail, got pass")
	}
	if output == "" {
		t.Fatal("expected non-empty output")
	}
}

func TestNewGitCommitFunc(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping in short mode")
	}

	// Check git is available.
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git not found")
	}

	// Set up a temp git repo.
	dir := t.TempDir()
	run := func(args ...string) {
		t.Helper()
		cmd := exec.Command("git", append([]string{"-C", dir}, args...)...)
		cmd.Env = append(os.Environ(),
			"GIT_AUTHOR_NAME=Test",
			"GIT_AUTHOR_EMAIL=test@test.com",
			"GIT_COMMITTER_NAME=Test",
			"GIT_COMMITTER_EMAIL=test@test.com",
		)
		out, err := cmd.CombinedOutput()
		if err != nil {
			t.Fatalf("git %v: %s", args, out)
		}
	}
	run("init")
	run("commit", "--allow-empty", "-m", "initial")

	// Create a file.
	if err := os.WriteFile(filepath.Join(dir, "hello.txt"), []byte("hello"), 0o644); err != nil {
		t.Fatal(err)
	}

	commitFunc := NewGitCommitFunc(dir)
	err := commitFunc(context.Background(), "test: add hello.txt")
	if err != nil {
		t.Fatalf("commit: %v", err)
	}

	// Verify the commit exists.
	cmd := exec.Command("git", "-C", dir, "log", "--oneline", "-1")
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("git log: %s", out)
	}
	if len(out) == 0 {
		t.Fatal("expected commit in log")
	}
}

func TestIterationCallback(t *testing.T) {
	cfg := &Config{
		MaxIterations:   2,
		StagnationLimit: 0,
	}

	step := func(ctx context.Context, state *State, iteration int) (string, float64, error) {
		return "output", float64(iteration), nil
	}

	ctrl := NewController(cfg, step)

	var callbackCalls []int
	ctrl.SetOnIterationComplete(func(iteration int, _ string, _ float64, _ bool) {
		callbackCalls = append(callbackCalls, iteration)
	})

	if err := ctrl.Run(context.Background()); err != nil {
		t.Fatalf("Run: %v", err)
	}

	if len(callbackCalls) != 2 {
		t.Fatalf("callback called %d times, want 2", len(callbackCalls))
	}
	if callbackCalls[0] != 1 || callbackCalls[1] != 2 {
		t.Fatalf("callback iterations = %v, want [1 2]", callbackCalls)
	}
}

func TestPRDCurrentStory(t *testing.T) {
	prd := &PRD{
		Stories: []Story{
			{ID: "s1", Title: "Story 1", Priority: 1, Passes: false},
			{ID: "s2", Title: "Story 2", Priority: 2, Passes: false},
		},
	}

	// Before NextStory, CurrentStory should be nil.
	if prd.CurrentStory() != nil {
		t.Fatal("expected nil before NextStory")
	}

	// After NextStory, CurrentStory should match.
	story := prd.NextStory()
	if story == nil {
		t.Fatal("expected non-nil from NextStory")
	}
	if prd.CurrentStory() != story {
		t.Fatal("CurrentStory should match last NextStory result")
	}
	if story.ID != "s1" {
		t.Fatalf("expected s1, got %s", story.ID)
	}
}
