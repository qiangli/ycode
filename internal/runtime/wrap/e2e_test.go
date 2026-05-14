//go:build !windows

package wrap_test

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/qiangli/ycode/internal/runtime/wrap"
)

// TestE2E_PythonRuntimeHook is the headline integration test for
// Phase 1.2: wrap a real python3 process with --runtime-hooks=python,
// have it issue subprocess.run() calls in shell-form, list-form, and
// list-form-with-absolute-path, and verify the trace subprocess fires
// for each. The fail-open contract is exercised by checking the
// wrapped agent's exit code is 0 and stdout is intact.
//
// Skipped under -short (subprocess + filesystem cost) and on
// hosts that lack python3.
func TestE2E_PythonRuntimeHook(t *testing.T) {
	if testing.Short() {
		t.Skip("e2e: skipped under -short")
	}
	python := mustHave(t, "python3")
	ycodeBin := buildYcodeBin(t)

	// Fixture: three subprocess.run calls — shell-form, list-form,
	// list-form-with-absolute-path. The hook should fire on all three.
	// Stdout is parsed by the test to confirm the wrapped agent ran
	// normally; the trace subprocess emits to stderr (captured below).
	fixture := mustWriteTempScript(t, "fixture.py", `#!/usr/bin/env python3
import subprocess, sys
print("FIX:start")
r1 = subprocess.run("echo shell-form && true", shell=True, capture_output=True, text=True)
print(f"FIX:shell rc={r1.returncode} out={r1.stdout.strip()}")
r2 = subprocess.run(["echo", "list-form"], capture_output=True, text=True)
print(f"FIX:list rc={r2.returncode} out={r2.stdout.strip()}")
abs_echo = "/bin/echo"
r3 = subprocess.run([abs_echo, "abs-path"], capture_output=True, text=True)
print(f"FIX:abs rc={r3.returncode} out={r3.stdout.strip()}")
print("FIX:end")
`)
	// trace-tap: a tiny script that records each invocation so we can
	// assert the hook fired N times. We swap the real ycode bin's
	// internal-shell-trace path via YCODE_BIN, pointing it at this
	// recorder. The recorder forwards to the real ycode so OTel
	// integration still works.
	recordPath := filepath.Join(t.TempDir(), "trace-calls.txt")
	tapBin := mustWriteTempScript(t, "ycode-tap", `#!/bin/sh
# Record every invocation; forward to the real ycode for actual trace.
printf '%s\n' "$*" >> `+recordPath+`
exec `+ycodeBin+` "$@"
`)

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	out := &bytes.Buffer{}
	stderr := &bytes.Buffer{}
	exitCode, err := wrap.Run(ctx, wrap.Options{
		AgentArgs: []string{python, fixture},
		Profile:   "aider",
		// Force python hook on; this test doesn't care about the auto-
		// detect path because that's covered by profile_test.go.
		RuntimeHooks: []string{"python"},
		Stdout:       out,
		Stderr:       stderr,
		// Point the hook at the tap; the tap forwards to the real
		// ycode. The tap goes via env, not via PATH, so the wrap
		// shim doesn't try to rewrite it.
		Env: append(os.Environ(),
			"YCODE_BIN="+tapBin,
			"YCODE_LOG_LEVEL=debug",
		),
	})
	if err != nil {
		t.Fatalf("wrap.Run: %v\nstderr: %s", err, stderr.String())
	}
	if exitCode != 0 {
		t.Fatalf("wrap exit=%d stderr=%s", exitCode, stderr.String())
	}

	// Wrapped agent's stdout must contain all three FIX: lines —
	// proves the hook ran fail-open and didn't break the wrap.
	got := out.String()
	for _, marker := range []string{"FIX:start", "FIX:shell", "FIX:list", "FIX:abs", "FIX:end"} {
		if !strings.Contains(got, marker) {
			t.Errorf("expected stdout to contain %q\nfull stdout:\n%s", marker, got)
		}
	}

	// Trace recorder must have at least three lines — one per
	// subprocess call. Reading via os.ReadFile because the test's
	// subprocess closed its own file handles.
	recorded, err := os.ReadFile(recordPath)
	if err != nil {
		t.Fatalf("read recorder: %v", err)
	}
	lines := nonEmptyLines(string(recorded))
	if len(lines) < 3 {
		t.Errorf("expected >=3 trace invocations, got %d\nrecorded:\n%s",
			len(lines), string(recorded))
	}

	// Spot-check: at least one shell-form invocation and one argv-form.
	gotShell := false
	gotArgv := false
	for _, line := range lines {
		if strings.Contains(line, "--argv") {
			gotArgv = true
		} else if strings.Contains(line, "internal-shell-trace") {
			gotShell = true
		}
	}
	if !gotShell {
		t.Errorf("no shell-form trace invocation recorded\nlines:\n%v", lines)
	}
	if !gotArgv {
		t.Errorf("no argv-form trace invocation recorded\nlines:\n%v", lines)
	}
}

// TestE2E_NodeRuntimeHook mirrors the Python test for Node's
// child_process.spawnSync / exec interceptors. Skipped when node is
// not installed.
func TestE2E_NodeRuntimeHook(t *testing.T) {
	if testing.Short() {
		t.Skip("e2e: skipped under -short")
	}
	node := mustHave(t, "node")
	ycodeBin := buildYcodeBin(t)

	fixture := mustWriteTempScript(t, "fixture.cjs", `#!/usr/bin/env node
const cp = require("node:child_process");
console.log("FIX:start");
const r1 = cp.execSync("echo shell-form && true", { encoding: "utf8" });
console.log("FIX:shell out=" + r1.trim());
const r2 = cp.spawnSync("echo", ["list-form"], { encoding: "utf8" });
console.log("FIX:list out=" + r2.stdout.trim());
const r3 = cp.spawnSync("/bin/echo", ["abs-path"], { encoding: "utf8" });
console.log("FIX:abs out=" + r3.stdout.trim());
console.log("FIX:end");
`)
	recordPath := filepath.Join(t.TempDir(), "trace-calls.txt")
	tapBin := mustWriteTempScript(t, "ycode-tap", `#!/bin/sh
printf '%s\n' "$*" >> `+recordPath+`
exec `+ycodeBin+` "$@"
`)

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	out := &bytes.Buffer{}
	stderr := &bytes.Buffer{}
	exitCode, err := wrap.Run(ctx, wrap.Options{
		AgentArgs:    []string{node, fixture},
		Profile:      "claude",
		RuntimeHooks: []string{"node"},
		Stdout:       out,
		Stderr:       stderr,
		Env: append(os.Environ(),
			"YCODE_BIN="+tapBin,
			"YCODE_LOG_LEVEL=debug",
		),
	})
	if err != nil {
		t.Fatalf("wrap.Run: %v\nstderr: %s", err, stderr.String())
	}
	if exitCode != 0 {
		t.Fatalf("wrap exit=%d stderr=%s", exitCode, stderr.String())
	}

	got := out.String()
	for _, marker := range []string{"FIX:start", "FIX:shell", "FIX:list", "FIX:abs", "FIX:end"} {
		if !strings.Contains(got, marker) {
			t.Errorf("expected stdout to contain %q\nfull stdout:\n%s", marker, got)
		}
	}

	recorded, _ := os.ReadFile(recordPath)
	lines := nonEmptyLines(string(recorded))
	if len(lines) < 3 {
		t.Errorf("expected >=3 trace invocations, got %d\nrecorded:\n%s",
			len(lines), string(recorded))
	}
}

// TestE2E_RuntimeHooksOff exercises the fail-open + opt-out path:
// the same Python fixture, but --runtime-hooks=off. The wrapped agent
// must still complete, and the trace recorder must show zero
// invocations.
func TestE2E_RuntimeHooksOff(t *testing.T) {
	if testing.Short() {
		t.Skip("e2e: skipped under -short")
	}
	python := mustHave(t, "python3")
	ycodeBin := buildYcodeBin(t)

	fixture := mustWriteTempScript(t, "fixture.py", `#!/usr/bin/env python3
import subprocess, sys
print("FIX:start")
r = subprocess.run("echo hi", shell=True, capture_output=True, text=True)
print(f"FIX:hi out={r.stdout.strip()}")
print("FIX:end")
`)
	recordPath := filepath.Join(t.TempDir(), "trace-calls.txt")
	tapBin := mustWriteTempScript(t, "ycode-tap", `#!/bin/sh
printf '%s\n' "$*" >> `+recordPath+`
exec `+ycodeBin+` "$@"
`)

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	out := &bytes.Buffer{}
	stderr := &bytes.Buffer{}
	exitCode, err := wrap.Run(ctx, wrap.Options{
		AgentArgs:    []string{python, fixture},
		Profile:      "aider",
		RuntimeHooks: []string{}, // explicit empty == off
		Stdout:       out,
		Stderr:       stderr,
		Env:          append(os.Environ(), "YCODE_BIN="+tapBin),
	})
	if err != nil || exitCode != 0 {
		t.Fatalf("wrap exit=%d err=%v stderr=%s", exitCode, err, stderr.String())
	}

	if !strings.Contains(out.String(), "FIX:hi") {
		t.Errorf("wrapped agent did not complete: stdout=%s", out.String())
	}

	if data, _ := os.ReadFile(recordPath); len(nonEmptyLines(string(data))) != 0 {
		t.Errorf("--runtime-hooks=off should produce zero trace calls, got: %s", data)
	}
}

// TestWrapClaudePrintTraced exercises the M2 claude profile honesty
// pass: a real `claude` binary wrapped via `ycode wrap` should
//
//  1. exit cleanly (the wrap is fail-open, the Bun runtime hook is a no-op);
//  2. emit the one-line Bun-limitation notice on stderr;
//  3. land at least one ExecScopeWrappedAgent span in the wrap-*
//     instance dir under $HOME/.agents/ycode/otel/instances/.
//
// Uses `claude --version` so no Anthropic API key or network is
// required. Skipped under -short or when claude is not on PATH.
func TestWrapClaudePrintTraced(t *testing.T) {
	if testing.Short() {
		t.Skip("e2e: skipped under -short")
	}
	claude := mustHave(t, "claude")

	// Redirect HOME so the wrap's OTel instance dir lands in the
	// tempdir and we can introspect it without colliding with the
	// user's real ~/.agents/ycode/otel/.
	tmpHome := t.TempDir()
	t.Setenv("HOME", tmpHome)
	t.Setenv("XDG_RUNTIME_DIR", t.TempDir())

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	out := &bytes.Buffer{}
	stderr := &bytes.Buffer{}
	exitCode, err := wrap.Run(ctx, wrap.Options{
		AgentArgs:  []string{claude, "--version"},
		OTelExport: "file",
		Stdout:     out,
		Stderr:     stderr,
	})
	if err != nil {
		t.Fatalf("wrap.Run: %v\nstderr: %s", err, stderr.String())
	}
	if exitCode != 0 {
		t.Fatalf("wrap exit=%d stderr=%s", exitCode, stderr.String())
	}

	// (2) Bun-limitation stderr notice fires.
	if !strings.Contains(stderr.String(), "claude: Bun runtime") {
		t.Errorf("expected Bun-limitation notice on stderr; got:\n%s", stderr.String())
	}

	// (3) OTel instance dir exists.
	dir := filepath.Join(tmpHome, ".agents", "ycode", "otel", "instances")
	entries, readErr := os.ReadDir(dir)
	if readErr != nil {
		t.Fatalf("read instances dir: %v", readErr)
	}
	foundWrap := false
	for _, e := range entries {
		if strings.HasPrefix(e.Name(), "wrap-") {
			foundWrap = true
			break
		}
	}
	if !foundWrap {
		t.Errorf("no wrap-*-prefixed OTel instance dir under %s; entries: %v", dir, entries)
	}
}

// --- helpers ---------------------------------------------------------

// mustHave skips the test when the binary isn't on PATH.
func mustHave(t *testing.T, bin string) string {
	t.Helper()
	path, err := exec.LookPath(bin)
	if err != nil {
		t.Skipf("e2e: %s not on PATH; skipping", bin)
	}
	return path
}

// buildYcodeBin returns the absolute path to the repo's bin/ycode,
// rebuilding it once across all e2e tests in this package (guarded
// by a sync.Once). Skips the test when the build fails so a partial
// repo state doesn't burn the whole suite — the e2e tests are a
// best-effort dogfood layer above the unit tests, not the gate.
func buildYcodeBin(t *testing.T) string {
	t.Helper()
	ycodeBuildOnce.Do(func() {
		root := repoRoot(t)
		out := filepath.Join(root, "bin", "ycode")
		if err := os.MkdirAll(filepath.Dir(out), 0o755); err != nil {
			ycodeBuildErr = err
			return
		}
		cmd := exec.Command("go", "build",
			"-tags", "sqlite,sqlite_unlock_notify,bindata,experimental",
			"-o", out,
			"./cmd/ycode/",
		)
		cmd.Dir = root
		var stderr bytes.Buffer
		cmd.Stderr = &stderr
		if err := cmd.Run(); err != nil {
			ycodeBuildErr = fmt.Errorf("go build ycode: %w\nstderr: %s", err, stderr.String())
			return
		}
		ycodeBuildPath = out
	})
	if ycodeBuildErr != nil {
		t.Skipf("e2e: ycode build failed: %v", ycodeBuildErr)
	}
	return ycodeBuildPath
}

var (
	ycodeBuildOnce sync.Once
	ycodeBuildPath string
	ycodeBuildErr  error
)

// repoRoot walks up from the test's package dir to find the repo
// root (where go.mod lives). The e2e test imports wrap so the
// package dir is internal/runtime/wrap — three levels up.
func repoRoot(t *testing.T) string {
	t.Helper()
	_, thisFile, _, _ := runtime.Caller(0)
	dir := filepath.Dir(thisFile)
	for i := 0; i < 8; i++ {
		if _, err := os.Stat(filepath.Join(dir, "go.mod")); err == nil {
			return dir
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			break
		}
		dir = parent
	}
	t.Fatalf("repo root not found from %s", thisFile)
	return ""
}

// mustWriteTempScript writes content to t.TempDir()/name and chmod
// +x. Returns the absolute path. Fails the test on error.
func mustWriteTempScript(t *testing.T, name, content string) string {
	t.Helper()
	dir := t.TempDir()
	p := filepath.Join(dir, name)
	if err := os.WriteFile(p, []byte(content), 0o755); err != nil {
		t.Fatalf("write %s: %v", p, err)
	}
	// Some filesystems ignore mode bits on WriteFile; chmod again.
	if err := os.Chmod(p, 0o755); err != nil && !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("chmod %s: %v", p, err)
	}
	return p
}

func nonEmptyLines(s string) []string {
	var out []string
	for _, line := range strings.Split(s, "\n") {
		if line = strings.TrimSpace(line); line != "" {
			out = append(out, line)
		}
	}
	return out
}
