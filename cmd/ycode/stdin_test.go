package main

import (
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"

	"github.com/qiangli/ycode/internal/selfheal"
)

// pipeWith returns a read end carrying body; closed reports whether the write
// end is closed (EOF) or left open and idle.
func pipeWith(t *testing.T, body string, closeWriter bool) *os.File {
	t.Helper()
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = r.Close() })
	if body != "" {
		if _, err := w.Write([]byte(body)); err != nil {
			t.Fatal(err)
		}
	}
	if closeWriter {
		_ = w.Close()
	} else {
		t.Cleanup(func() { _ = w.Close() })
	}
	return r
}

// TestReadPromptClosedEmptyPipeIsNotAPrompt is THE regression test for the
// reported bug: a harness hands its child an empty pipe, and that used to become
// `Error: empty input from stdin` plus a usage banner on every bare launch.
func TestReadPromptClosedEmptyPipeIsNotAPrompt(t *testing.T) {
	got, ok := readPrompt(pipeWith(t, "", true), stdinWaitDefault)
	if ok || got != "" {
		t.Fatalf("readPrompt = (%q, %v), want no prompt", got, ok)
	}
}

// TestReadPromptPipedText — the case that must keep working.
func TestReadPromptPipedText(t *testing.T) {
	got, ok := readPrompt(pipeWith(t, "  fix the bug\n", true), stdinWaitDefault)
	if !ok || got != "fix the bug" {
		t.Fatalf("readPrompt = (%q, %v), want (\"fix the bug\", true)", got, ok)
	}
}

// TestReadPromptWhitespaceOnlyIsNotAPrompt — a pipe carrying only a newline is
// not somebody asking a question.
func TestReadPromptWhitespaceOnlyIsNotAPrompt(t *testing.T) {
	if got, ok := readPrompt(pipeWith(t, "\n \t\n", true), stdinWaitDefault); ok {
		t.Fatalf("readPrompt = (%q, true), want no prompt", got)
	}
}

// TestReadPromptIdleOpenPipeDoesNotBlock — a harness that holds the write end of
// its child's stdin open and idle used to hang ycode at startup FOREVER, with no
// output at all. That is strictly worse than the error it would otherwise print,
// so the first byte is bounded.
func TestReadPromptIdleOpenPipeDoesNotBlock(t *testing.T) {
	r := pipeWith(t, "", false) // open, never written, never closed
	done := make(chan struct{})
	go func() {
		defer close(done)
		if got, ok := readPrompt(r, 50*time.Millisecond); ok {
			t.Errorf("readPrompt = (%q, true) from a pipe nobody wrote to", got)
		}
	}()
	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("readPrompt blocked on an idle pipe — this is the startup hang")
	}
}

// TestReadPromptSlowProducerStillDelivers — the bound is on the FIRST byte only.
// Once a producer starts, the rest is read unbounded, so a large or slow prompt
// is not truncated.
func TestReadPromptSlowProducerStillDelivers(t *testing.T) {
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = r.Close() })

	big := strings.Repeat("spec line\n", 20000)
	go func() {
		_, _ = w.Write([]byte("first "))
		time.Sleep(150 * time.Millisecond) // mid-stream stall, well past the bound
		_, _ = w.Write([]byte(big))
		_ = w.Close()
	}()

	got, ok := readPrompt(r, 2*time.Second)
	if !ok {
		t.Fatal("readPrompt found no prompt from a slow producer")
	}
	if !strings.HasPrefix(got, "first ") || len(got) < len(big) {
		t.Fatalf("readPrompt truncated a slow/large prompt: got %d bytes, want > %d", len(got), len(big))
	}
}

// TestReadPromptCharDeviceIsNeverRead — /dev/null and a terminal are character
// devices. Reading one would consume the user's keystrokes; neither is somebody
// piping in a prompt.
func TestReadPromptCharDeviceIsNeverRead(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("no /dev/null")
	}
	f, err := os.Open(os.DevNull)
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	if got, ok := readPrompt(f, stdinWaitDefault); ok {
		t.Fatalf("readPrompt(%s) = (%q, true), want no prompt", os.DevNull, got)
	}
}

// TestReadPromptRegularFile — `ycode < spec.md` is a genuine prompt, and a
// regular file is not pollable, which is why the bound uses a goroutine rather
// than SetReadDeadline.
func TestReadPromptRegularFile(t *testing.T) {
	p := filepath.Join(t.TempDir(), "spec.md")
	if err := os.WriteFile(p, []byte("implement the parser\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	f, err := os.Open(p)
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	got, ok := readPrompt(f, stdinWaitDefault)
	if !ok || got != "implement the parser" {
		t.Fatalf("readPrompt = (%q, %v), want the file's contents", got, ok)
	}
}

// TestReadPromptStatFailureIsNotAPanic — Stat's error used to be discarded and
// the nil FileInfo dereferenced immediately after, which is a startup panic on
// any fd that cannot be stat'd.
func TestReadPromptStatFailureIsNotAPanic(t *testing.T) {
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	_ = w.Close()
	_ = r.Close() // stat on a closed file fails
	got, ok := readPrompt(r, stdinWaitDefault)
	if ok || got != "" {
		t.Fatalf("readPrompt on a closed file = (%q, %v), want no prompt", got, ok)
	}
}

// TestReadPromptNilFile — defensive; must not panic.
func TestReadPromptNilFile(t *testing.T) {
	if _, ok := readPrompt(nil, stdinWaitDefault); ok {
		t.Fatal("readPrompt(nil) claimed a prompt")
	}
}

// TestStdinWaitOverride — an operator can restore the old unbounded behaviour,
// or tighten it, without a rebuild.
func TestStdinWaitOverride(t *testing.T) {
	for _, tc := range []struct {
		env  string
		want time.Duration
	}{
		{"", stdinWaitDefault},
		{"5s", 5 * time.Second},
		{"0", 0},
		{"garbage", stdinWaitDefault},
	} {
		t.Setenv("YCODE_STDIN_WAIT", tc.env)
		if got := stdinWait(); got != tc.want {
			t.Errorf("YCODE_STDIN_WAIT=%q -> %v, want %v", tc.env, got, tc.want)
		}
	}
}

// TestNoInputErrorIsNotSelfHealable pins the WORDING against selfheal's
// substring classifier (healer.go ClassifyError). "no prompt on stdin" is a
// usage situation, not a fault to diagnose — a word like "config", "tool" or
// "connection" slipping into this message would spend an LLM call trying to
// heal a user telling ycode nothing.
func TestNoInputErrorIsNotSelfHealable(t *testing.T) {
	if got := selfheal.ClassifyError(errNoInput); got != selfheal.FailureTypeUnknown {
		t.Errorf("ClassifyError(errNoInput) = %q, want %q — reword the error",
			got, selfheal.FailureTypeUnknown)
	}
}

// TestNoInputErrorNamesEveryWayForward — the message replaces a usage banner, so
// it has to carry the options the banner used to imply.
func TestNoInputErrorNamesEveryWayForward(t *testing.T) {
	msg := errNoInput.Error()
	for _, want := range []string{"argument", "pipe", "pty"} {
		if !strings.Contains(msg, want) {
			t.Errorf("errNoInput does not mention %q:\n%s", want, msg)
		}
	}
}

// TestRootCmdSilencesCobraOutput — cobra printed the error and main printed it
// again; and a RunE failure dragged the whole command listing with it.
func TestRootCmdSilencesCobraOutput(t *testing.T) {
	if !rootCmd.SilenceErrors {
		t.Error("rootCmd.SilenceErrors is false — errors print twice")
	}
	if rootCmd.PersistentPreRunE == nil {
		t.Fatal("rootCmd has no PersistentPreRunE to silence usage on a run failure")
	}
	// Usage suppression must reach SUBCOMMANDS too, since root's
	// PersistentPreRunE is what runs for them.
	child := rootCmd.Commands()[0]
	child.SilenceUsage = false
	if err := rootCmd.PersistentPreRunE(child, nil); err != nil {
		t.Fatalf("PersistentPreRunE returned %v; it must only set a flag", err)
	}
	if !child.SilenceUsage {
		t.Errorf("PersistentPreRunE did not silence usage on %q", child.Name())
	}
}
