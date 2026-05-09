package agentmode_test

import (
	"bytes"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// TestShellMineE2E builds bin/ycode, drives a sequence of `shell --suggest`
// invocations against a temp history file, then drives `shell --mine missed`
// and `--mine stats` and asserts the captured aggregates match expectations.
//
// Skipped under -short because building the binary is slow.
func TestShellMineE2E(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping e2e: -short")
	}

	repoRoot := findRepoRoot(t)
	binDir := t.TempDir()
	binPath := filepath.Join(binDir, "ycode")

	build := exec.Command("go", "build",
		"-tags", "sqlite,sqlite_unlock_notify,bindata",
		"-o", binPath, "./cmd/ycode/")
	build.Dir = repoRoot
	if out, err := build.CombinedOutput(); err != nil {
		t.Fatalf("go build: %v\n%s", err, out)
	}

	historyFile := filepath.Join(t.TempDir(), "h.jsonl")

	run := func(args ...string) (string, string, int) {
		t.Helper()
		cmd := exec.Command(binPath, args...)
		cmd.Env = append(os.Environ(),
			"YCODE_SHELL_HISTORY_FILE="+historyFile,
			"YCODE_SHELL_MINE_DISABLE=", // explicitly enabled
		)
		var stdout, stderr bytes.Buffer
		cmd.Stdout = &stdout
		cmd.Stderr = &stderr
		err := cmd.Run()
		exit := 0
		var ee *exec.ExitError
		if err != nil && !errors.As(err, &ee) {
			t.Fatalf("run %v: %v\nstderr: %s", args, err, stderr.String())
		} else if ee != nil {
			exit = ee.ExitCode()
		}
		return stdout.String(), stderr.String(), exit
	}

	// Populate the sink with three hits and two misses.
	for _, c := range []string{
		"git status",
		"grep -nE '^func' foo.go",
		"git log",
		"awk '/^func/' file.go",
		"sed -n '1,5p' README",
	} {
		_, _, exit := run("shell", "--suggest", c)
		if exit != 0 {
			t.Fatalf("--suggest %q: exit=%d", c, exit)
		}
	}

	// Sanity: the JSONL file exists and has 5 lines.
	data, err := os.ReadFile(historyFile)
	if err != nil {
		t.Fatalf("read history: %v", err)
	}
	if got := strings.Count(string(data), "\n"); got != 5 {
		t.Fatalf("history line count: want 5, got %d\n%s", got, data)
	}

	// --mine missed should surface awk and sed.
	stdout, _, _ := run("shell", "--mine", "missed")
	for _, want := range []string{"awk", "sed"} {
		if !strings.Contains(stdout, want) {
			t.Errorf("--mine missed: missing %q in:\n%s", want, stdout)
		}
	}
	if strings.Contains(stdout, "git") || strings.Contains(stdout, "grep") {
		t.Errorf("--mine missed: hits should not appear, got:\n%s", stdout)
	}

	// --mine stats should report 5 records, 3 hits, 2 misses.
	stdout, _, _ = run("shell", "--mine", "stats")
	for _, want := range []string{
		"records:        5",
		"pre hit/miss:   3 / 2",
		"git-log-status-diff-suggests-yc-git",
		"grep-source-file-suggests-symbols",
	} {
		if !strings.Contains(stdout, want) {
			t.Errorf("--mine stats: missing %q in:\n%s", want, stdout)
		}
	}

	// --mine raw should be a pass-through cat.
	stdout, _, _ = run("shell", "--mine", "raw")
	if stdout != string(data) {
		t.Errorf("--mine raw: output diverges from raw history file\nwant:\n%s\ngot:\n%s", data, stdout)
	}

	// Unknown action returns non-zero with a clear error.
	_, stderr, exit := run("shell", "--mine", "bogus")
	if exit == 0 {
		t.Errorf("expected non-zero exit for --mine bogus; stderr=%s", stderr)
	}
	if !strings.Contains(stderr, "unknown action") {
		t.Errorf("--mine bogus: stderr missing guidance: %s", stderr)
	}
}

// findRepoRoot walks up from the test working directory until it finds
// a go.mod, so the e2e test can build the binary regardless of where the
// `go test` invocation runs from.
func findRepoRoot(t *testing.T) string {
	t.Helper()
	dir, err := os.Getwd()
	if err != nil {
		t.Fatalf("getwd: %v", err)
	}
	for range 10 {
		if _, err := os.Stat(filepath.Join(dir, "go.mod")); err == nil {
			return dir
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			break
		}
		dir = parent
	}
	t.Fatalf("no go.mod found above test cwd")
	return ""
}
