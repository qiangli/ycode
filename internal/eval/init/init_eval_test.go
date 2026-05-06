// Package init_eval drives an aperio-replayed /init invocation against
// the recorded cassette and asserts the streaming pipeline still works
// end-to-end. Records a baseline once (with a real provider key); after
// that, every CI run replays offline and is fully deterministic.
package init_eval

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/qiangli/aperio/runner"
)

// cassettePath is the recorded LLM-API cassette. Recording instructions
// live in testdata/README.md. When the file is missing, the test skips
// rather than failing — recording requires a provider API key and is a
// one-time bootstrapping step.
const cassettePath = "testdata/init.cassette.yaml"

// binaryPath is the ycode binary under test. Built by `make compile`.
const binaryPath = "../../../bin/ycode"

func TestInit_AperioReplay(t *testing.T) {
	if testing.Short() {
		t.Skip("eval-init skipped in -short")
	}

	cassetteAbs, err := filepath.Abs(cassettePath)
	if err != nil {
		t.Fatalf("abs cassette path: %v", err)
	}
	if _, err := os.Stat(cassetteAbs); os.IsNotExist(err) {
		t.Skipf("cassette not yet recorded at %s — see testdata/README.md", cassettePath)
	}

	binAbs, err := filepath.Abs(binaryPath)
	if err != nil {
		t.Fatalf("abs binary path: %v", err)
	}
	if _, err := os.Stat(binAbs); os.IsNotExist(err) {
		t.Skipf("ycode binary not found at %s — run `make compile` first", binaryPath)
	}

	// Fresh tempdir so /init can scaffold without touching the project.
	work := t.TempDir()
	out := filepath.Join(work, "replay-trace.json")

	ctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
	defer cancel()

	// Replay drives the binary with HTTPS_PROXY pointing at aperio's
	// in-process MITM proxy that serves the recorded responses. The
	// binary itself is unmodified — same code path as a live /init.
	err = runner.Replay(ctx, runner.ReplayOptions{
		Command:       []string{binAbs, "--once", "/init"},
		CassettePath:  cassetteAbs,
		OutputPath:    out,
		WorkingDir:    work,
		MatchStrategy: "fingerprint",
	})
	if err != nil {
		t.Fatalf("aperio replay failed: %v", err)
	}

	// Aperio writes the merged trace to OutputPath when replay completes.
	// Existence is the minimal sanity check; richer assertions (specific
	// command-event spans, AGENTS.md file content) come in later
	// iterations as the trace shape stabilizes.
	info, err := os.Stat(out)
	if err != nil {
		t.Fatalf("expected replay trace at %s: %v", out, err)
	}
	if info.Size() == 0 {
		t.Errorf("replay trace is empty")
	}

	// /init should have written AGENTS.md into the working directory.
	if _, err := os.Stat(filepath.Join(work, "AGENTS.md")); err != nil {
		t.Errorf("expected AGENTS.md in working dir after /init replay: %v", err)
	}
}
