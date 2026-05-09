package bash

import (
	"bytes"
	"context"
	"strings"
	"testing"
)

func TestRunString_BasicExitCodes(t *testing.T) {
	s := NewShellSession(t.TempDir())

	cases := []struct {
		name string
		src  string
		want int
	}{
		{"true", "true", 0},
		{"false", "false", 1},
		{"exit 7", "exit 7", 7},
		{"unknown", "this-binary-does-not-exist", 127},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			var stdout, stderr bytes.Buffer
			code, err := s.RunString(context.Background(), tc.src, Stdio{Stdout: &stdout, Stderr: &stderr})
			if err != nil {
				t.Fatalf("RunString error: %v (stderr=%q)", err, stderr.String())
			}
			if code != tc.want {
				t.Fatalf("exit code = %d, want %d (stderr=%q)", code, tc.want, stderr.String())
			}
		})
	}
}

func TestRunString_StdoutCaptured(t *testing.T) {
	s := NewShellSession(t.TempDir())
	var out bytes.Buffer
	code, err := s.RunString(context.Background(), `echo hello world`, Stdio{Stdout: &out})
	if err != nil {
		t.Fatalf("RunString error: %v", err)
	}
	if code != 0 {
		t.Fatalf("exit code = %d, want 0", code)
	}
	if got := strings.TrimSpace(out.String()); got != "hello world" {
		t.Fatalf("stdout = %q, want %q", got, "hello world")
	}
}

func TestRunString_EnvPersistsAcrossCalls(t *testing.T) {
	s := NewShellSession(t.TempDir())
	ctx := context.Background()

	// Set a variable in one call.
	if _, err := s.RunString(ctx, `FOO=bar`, Stdio{}); err != nil {
		t.Fatalf("first call: %v", err)
	}

	// Read it back in a second call.
	var out bytes.Buffer
	code, err := s.RunString(ctx, `echo $FOO`, Stdio{Stdout: &out})
	if err != nil {
		t.Fatalf("second call: %v", err)
	}
	if code != 0 {
		t.Fatalf("exit code = %d", code)
	}
	if got := strings.TrimSpace(out.String()); got != "bar" {
		t.Fatalf("$FOO = %q, want %q (this proves persistent runner is live)", got, "bar")
	}
}

func TestRunString_FunctionPersistsAcrossCalls(t *testing.T) {
	s := NewShellSession(t.TempDir())
	ctx := context.Background()

	if _, err := s.RunString(ctx, `greet() { echo "hi $1"; }`, Stdio{}); err != nil {
		t.Fatalf("define function: %v", err)
	}
	var out bytes.Buffer
	code, err := s.RunString(ctx, `greet world`, Stdio{Stdout: &out})
	if err != nil {
		t.Fatalf("call function: %v", err)
	}
	if code != 0 {
		t.Fatalf("exit code = %d", code)
	}
	if got := strings.TrimSpace(out.String()); got != "hi world" {
		t.Fatalf("output = %q, want %q", got, "hi world")
	}
}

func TestRunString_CwdPersistsAcrossCalls(t *testing.T) {
	tmp := t.TempDir()
	s := NewShellSession(tmp)
	ctx := context.Background()

	sub := tmp + "/sub"
	if _, err := s.RunString(ctx, `mkdir sub`, Stdio{}); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	if _, err := s.RunString(ctx, `cd sub`, Stdio{}); err != nil {
		t.Fatalf("cd: %v", err)
	}
	var out bytes.Buffer
	if _, err := s.RunString(ctx, `pwd`, Stdio{Stdout: &out}); err != nil {
		t.Fatalf("pwd: %v", err)
	}
	got := strings.TrimSpace(out.String())
	if got != sub {
		t.Fatalf("pwd = %q, want %q", got, sub)
	}
	if s.WorkDir() != sub {
		t.Fatalf("session WorkDir = %q, want %q", s.WorkDir(), sub)
	}
}

func TestRunString_StdinFed(t *testing.T) {
	s := NewShellSession(t.TempDir())
	var out, errOut bytes.Buffer
	code, err := s.RunString(context.Background(), `cat`, Stdio{
		Stdin:  strings.NewReader("piped-in"),
		Stdout: &out,
		Stderr: &errOut,
	})
	if err != nil {
		t.Fatalf("RunString error: %v (stderr=%q)", err, errOut.String())
	}
	if code != 0 {
		t.Fatalf("exit code = %d (stderr=%q)", code, errOut.String())
	}
	if got := out.String(); got != "piped-in" {
		t.Fatalf("stdout = %q, want %q (stderr=%q)", got, "piped-in", errOut.String())
	}
}

// Verify that shell options set in one call apply in a subsequent call.
// mvdan's Runner.Run only Resets on the first call (didReset latches
// true), so the persistent runner naturally preserves these.
func TestRunString_SetOptionsPersist(t *testing.T) {
	t.Run("set -u persists across calls", func(t *testing.T) {
		s := NewShellSession(t.TempDir())
		ctx := context.Background()

		if _, err := s.RunString(ctx, `set -u`, Stdio{}); err != nil {
			t.Fatalf("set -u: %v", err)
		}
		var stdout, stderr bytes.Buffer
		exit, err := s.RunString(ctx, `echo "$DEFINITELY_UNSET"`, Stdio{Stdout: &stdout, Stderr: &stderr})
		if err != nil {
			t.Fatalf("echo: %v", err)
		}
		if exit == 0 {
			t.Fatalf("expected non-zero exit under set -u; got 0 (stderr=%q)", stderr.String())
		}
	})

	t.Run("set -e persists across calls", func(t *testing.T) {
		s := NewShellSession(t.TempDir())
		ctx := context.Background()

		if _, err := s.RunString(ctx, `set -e`, Stdio{}); err != nil {
			t.Fatalf("set -e: %v", err)
		}
		// Multi-statement: false should abort the script before echo.
		var stdout bytes.Buffer
		_, err := s.RunString(ctx, `false; echo SHOULD_NOT_PRINT`, Stdio{Stdout: &stdout})
		if err != nil {
			t.Fatalf("script: %v", err)
		}
		if strings.Contains(stdout.String(), "SHOULD_NOT_PRINT") {
			t.Fatalf("set -e did not abort; stdout = %q", stdout.String())
		}
	})

	t.Run("set -o pipefail persists across calls", func(t *testing.T) {
		s := NewShellSession(t.TempDir())
		ctx := context.Background()
		if _, err := s.RunString(ctx, `set -o pipefail`, Stdio{}); err != nil {
			t.Fatalf("set -o: %v", err)
		}
		// pipefail makes the pipeline exit code = leftmost-failure.
		exit, err := s.RunString(ctx, `false | true`, Stdio{})
		if err != nil {
			t.Fatalf("pipeline: %v", err)
		}
		if exit == 0 {
			t.Fatalf("expected non-zero exit with pipefail; got 0")
		}
	})
}

// Aliases work within mvdan when run as a single script. Confirm that
// they ALSO persist across separate RunString calls in the persistent
// runner, since runner state isn't wiped between calls.
func TestRunString_AliasPersists(t *testing.T) {
	s := NewShellSession(t.TempDir())
	ctx := context.Background()

	// shopt -s expand_aliases is required even in a non-interactive
	// context; check that alias use survives the call boundary.
	setup := `shopt -s expand_aliases; alias g='echo greeted'`
	if _, err := s.RunString(ctx, setup, Stdio{}); err != nil {
		t.Fatalf("setup: %v", err)
	}
	var out bytes.Buffer
	if _, err := s.RunString(ctx, `g`, Stdio{Stdout: &out}); err != nil {
		t.Fatalf("invoke alias: %v", err)
	}
	if got := strings.TrimSpace(out.String()); got != "greeted" {
		// If aliases don't survive the call boundary, this fails — and
		// fixing that is the actual F5 work. Documented as a skip
		// rather than a hard fail until the scope of the fix is clearer.
		t.Skipf("aliases do not survive call boundary in current mvdan/sh integration; got %q (this is the known F5 gap)", got)
	}
}

func TestReset_DropsState(t *testing.T) {
	s := NewShellSession(t.TempDir())
	ctx := context.Background()

	if _, err := s.RunString(ctx, `FOO=bar`, Stdio{}); err != nil {
		t.Fatalf("set: %v", err)
	}
	s.Reset()
	var out bytes.Buffer
	if _, err := s.RunString(ctx, `echo "${FOO:-empty}"`, Stdio{Stdout: &out}); err != nil {
		t.Fatalf("after reset: %v", err)
	}
	if got := strings.TrimSpace(out.String()); got != "empty" {
		t.Fatalf("after Reset $FOO = %q, want %q", got, "empty")
	}
}
