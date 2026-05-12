package wrap

import (
	"os"
	"path/filepath"
	"strconv"
	"testing"
)

func TestIsProcessAlive(t *testing.T) {
	// Current process must be alive.
	if !isProcessAlive(os.Getpid()) {
		t.Errorf("isProcessAlive(self) = false; want true")
	}
	// PID 0 and -1 are never valid.
	if isProcessAlive(0) {
		t.Errorf("isProcessAlive(0) = true; want false")
	}
	if isProcessAlive(-1) {
		t.Errorf("isProcessAlive(-1) = true; want false")
	}
	// A high PID that is overwhelmingly unlikely to exist. If by ill
	// luck a real process owns this PID, the test is flaky; we trade
	// flakiness risk for not having to fork-and-kill a real process
	// just to get a known-dead PID.
	if isProcessAlive(999999) {
		t.Skip("PID 999999 is unexpectedly alive; cannot verify dead-path")
	}
}

func TestReapStaleShimDirs(t *testing.T) {
	root := t.TempDir()
	wrapRoot := filepath.Join(root, "ycode-wrap")

	// Three subdirs: one for an alive PID (current process), one for
	// a definitely-dead PID, one with a non-numeric name (must be left
	// alone — could belong to another tool).
	alivePID := os.Getpid()
	deadPID := 999999

	aliveDir := filepath.Join(wrapRoot, strconv.Itoa(alivePID), "bin")
	deadDir := filepath.Join(wrapRoot, strconv.Itoa(deadPID), "bin")
	junkDir := filepath.Join(wrapRoot, "not-a-pid", "bin")

	for _, d := range []string{aliveDir, deadDir, junkDir} {
		if err := os.MkdirAll(d, 0o755); err != nil {
			t.Fatalf("mkdir %s: %v", d, err)
		}
	}

	reapStaleShimDirs(root)

	if _, err := os.Stat(aliveDir); err != nil {
		t.Errorf("reaper removed alive-PID dir: %v", err)
	}
	if _, err := os.Stat(deadDir); !os.IsNotExist(err) {
		t.Errorf("reaper did not remove dead-PID dir: stat err=%v", err)
	}
	if _, err := os.Stat(junkDir); err != nil {
		t.Errorf("reaper touched non-numeric dir: %v", err)
	}
}

func TestReapStaleShimDirs_NoRoot(t *testing.T) {
	// Must not error or log warns when the wrap root doesn't exist —
	// fresh installs have no ycode-wrap dir at all.
	reapStaleShimDirs(t.TempDir())
}
