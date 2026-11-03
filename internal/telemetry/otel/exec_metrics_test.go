package otel

import (
	"bytes"
	"context"
	"errors"
	"log/slog"
	"os/exec"
	"runtime"
	"strings"
	"testing"
	"time"
)

func TestClassifyExit_Zero(t *testing.T) {
	class, code, sig := ClassifyExit(context.Background(), 0, nil)
	if class != ExitClassZero {
		t.Fatalf("class = %q; want %q", class, ExitClassZero)
	}
	if code != 0 {
		t.Fatalf("code = %d; want 0", code)
	}
	if sig != "" {
		t.Fatalf("signal = %q; want empty", sig)
	}
}

func TestClassifyExit_TimeoutBeforeErrShape(t *testing.T) {
	// Caller-cancellation precedence: even if the underlying err is an
	// ExitError, a canceled context must classify as timeout.
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	cmd := exec.Command("/bin/false")
	err := cmd.Run()
	class, _, _ := ClassifyExit(ctx, cmd.ProcessState.ExitCode(), err)
	if class != ExitClassTimeout {
		t.Fatalf("class = %q; want %q (canceled ctx)", class, ExitClassTimeout)
	}
}

func TestClassifyExit_NotFound(t *testing.T) {
	cmd := exec.Command("/this/path/does/not/exist")
	err := cmd.Run()
	if err == nil {
		t.Fatal("expected an error spawning a missing binary")
	}
	class, code, _ := ClassifyExit(context.Background(), 0, err)
	if class != ExitClassNotFound {
		t.Fatalf("class = %q; want %q", class, ExitClassNotFound)
	}
	if code != 127 {
		t.Fatalf("code = %d; want 127", code)
	}
}

func TestClassifyExit_NonZeroExit(t *testing.T) {
	cmd := exec.Command("/bin/sh", "-c", "exit 3")
	err := cmd.Run()
	class, code, sig := ClassifyExit(context.Background(), cmd.ProcessState.ExitCode(), err)
	if class != ExitClassError {
		t.Fatalf("class = %q; want %q", class, ExitClassError)
	}
	if code != 3 {
		t.Fatalf("code = %d; want 3", code)
	}
	if sig != "" {
		t.Fatalf("signal = %q; want empty", sig)
	}
}

func TestClassifyExit_Signaled(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("signal semantics differ on Windows")
	}
	// `kill -9 $$` self-terminates with SIGKILL; the parent observes
	// an ExitError with WaitStatus.Signaled().
	cmd := exec.Command("/bin/sh", "-c", "kill -9 $$")
	err := cmd.Run()
	if err == nil {
		t.Fatal("expected the child to die from a signal")
	}
	class, code, sig := ClassifyExit(context.Background(), 0, err)
	if class != ExitClassSignaled {
		t.Fatalf("class = %q; want %q", class, ExitClassSignaled)
	}
	if code != 137 { // 128 + 9
		t.Fatalf("code = %d; want 137", code)
	}
	if sig == "" {
		t.Fatalf("signal name should be set; got empty")
	}
}

func TestClassifyExit_PlainError(t *testing.T) {
	class, code, _ := ClassifyExit(context.Background(), 7, errors.New("REST API call failed"))
	if class != ExitClassError {
		t.Fatalf("class = %q; want %q", class, ExitClassError)
	}
	if code != 7 {
		t.Fatalf("code = %d; want 7 (passthrough)", code)
	}
}

func TestStartExecSpan_RoundTripDoesNotPanic(t *testing.T) {
	// Without a real OTEL provider the meter/tracer are no-ops; this
	// test exercises the lifecycle so a missing finish-call path
	// (e.g. nil span) would panic loudly. The fact that it passes
	// also confirms the per-call meter lookup doesn't trip on the
	// global no-op provider.
	ctx, finish := StartExecSpan(context.Background(), ExecScopeBash, "/bin/true", []string{"/bin/true"})
	if ctx == nil {
		t.Fatal("StartExecSpan returned a nil context")
	}
	finish(0, nil)
}

func TestStartExecSpan_FailureBranch(t *testing.T) {
	// Run a real failing command so the ExitError has a valid
	// ProcessState (a bare &exec.ExitError{} crashes Sys()).
	cmd := exec.Command("/bin/sh", "-c", "exit 2")
	runErr := cmd.Run()
	ctx, finish := StartExecSpan(context.Background(), ExecScopeBash, "/bin/sh", []string{"/bin/sh", "-c", "exit 2"})
	finish(cmd.ProcessState.ExitCode(), runErr)
	_ = ctx
}

func TestRecordExec_PlainCall(t *testing.T) {
	// Just verify the post-hoc form doesn't panic with arbitrary
	// scope strings or nil/non-nil err.
	RecordExec(context.Background(), ExecScopeContainer, "/usr/bin/podman", 5*time.Millisecond, 0, nil)
	RecordExec(context.Background(), ExecScopeContainer, "/usr/bin/podman", 5*time.Millisecond, 1, errors.New("boom"))
}

// TestLogExec_LevelMapping pins the slog level mapping per exit class.
// A successful spawn whose carried program returned non-zero (lsof
// no-match, grep no-match, test, build failure, …) MUST NOT escalate
// to WARN — that line lands on stderr and corrupts wrapped TUIs like
// `ycode wrap -- claude`. WARN is reserved for genuine exec-layer
// failures: signaled, timeout, not-found.
func TestLogExec_LevelMapping(t *testing.T) {
	cases := []struct {
		name      string
		class     string
		err       error
		wantLevel string
		wantMsg   string
	}{
		{"zero", ExitClassZero, nil, "DEBUG", "exec"},
		{"non-zero exit stays debug", ExitClassError, errors.New("exit status 1"), "DEBUG", "exec"},
		{"signaled escalates to warn", ExitClassSignaled, errors.New("killed"), "WARN", "exec failed"},
		{"timeout escalates to warn", ExitClassTimeout, context.Canceled, "WARN", "exec failed"},
		{"not-found escalates to warn", ExitClassNotFound, errors.New("not found"), "WARN", "exec failed"},
	}
	prev := slog.Default()
	t.Cleanup(func() { slog.SetDefault(prev) })

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			var buf bytes.Buffer
			slog.SetDefault(slog.New(slog.NewTextHandler(&buf, &slog.HandlerOptions{Level: slog.LevelDebug})))
			logExec("bash", "/bin/bash", tc.class, 1, 2*time.Millisecond, tc.err)
			out := buf.String()
			if !strings.Contains(out, "level="+tc.wantLevel) {
				t.Fatalf("class=%q got %q; want level=%s", tc.class, out, tc.wantLevel)
			}
			if !strings.Contains(out, `msg="`+tc.wantMsg+`"`) && !strings.Contains(out, "msg="+tc.wantMsg) {
				t.Fatalf("class=%q got %q; want msg=%s", tc.class, out, tc.wantMsg)
			}
		})
	}
}
