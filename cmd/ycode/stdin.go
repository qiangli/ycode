package main

import (
	"errors"
	"io"
	"os"
	"strings"
	"time"

	"golang.org/x/term"
)

// WHAT STDIN MEANS, AND WHAT IT DOES NOT.
//
// Bare `ycode` used to treat "stdin is not a character device" as "the user
// piped me a prompt", read it, and hard-fail on empty:
//
//	$ ycode                      # under a harness, launchd, CI, an editor
//	Error: empty input from stdin
//	Usage: ...
//
// But a pipe on fd 0 is the NORMAL state for a spawned process. An agent
// harness, setsid, a socket-activated parent and every CI runner hand their
// children a pipe with nothing on it. None of them are asking ycode to answer a
// prompt; they just did not give it a terminal.
//
// So the rule here: a prompt on stdin is something we CONFIRM, never something
// we infer from the file type. No prompt is a routing fact — it tells us to open
// the TUI, or to say plainly that there is nothing to do — and never an error in
// itself. This mirrors the explicit-flag discipline `--print` already follows:
// ycode does not infer headless mode from "stdout is not a terminal" either.

// stdinWaitDefault bounds the wait for the FIRST byte only; once a producer
// starts, the rest is read unbounded, so a slow `cat huge-spec.md | ycode` is
// unaffected. The bound exists because a harness commonly holds the write end of
// its child's stdin open and idle — with an unbounded read, ycode hung at
// startup forever, printing nothing. A lost prompt from a very slow producer is
// recoverable; a silent hang is not.
const stdinWaitDefault = 2 * time.Second

// errNoInput is returned when there is no prompt and no terminal to open a
// session on. Exit non-zero, because a harness that launched ycode expecting
// work must not read success.
//
// The wording is deliberately free of the words the self-healer classifies on
// (config, settings, build, api, connection, timeout, tool, panic): this is a
// usage situation, not a failure to diagnose, and it must not trigger an
// LLM-backed heal.
var errNoInput = errors.New(
	"no prompt on stdin and no terminal attached — pass one as an argument " +
		`(ycode prompt "fix the bug"), pipe one in (echo "fix the bug" | ycode), ` +
		"or attach a pty for an interactive session")

// stdinWait is the first-byte bound, overridable with YCODE_STDIN_WAIT (a Go
// duration; "0" waits forever, restoring the old blocking behaviour).
func stdinWait() time.Duration {
	v := strings.TrimSpace(os.Getenv("YCODE_STDIN_WAIT"))
	if v == "" {
		return stdinWaitDefault
	}
	d, err := time.ParseDuration(v)
	if err != nil {
		return stdinWaitDefault
	}
	return d
}

// readPrompt reports the prompt piped into f, if any.
//
// It never returns an error: an unreadable, closed, idle or whitespace-only
// stdin all mean the same thing — there is no prompt — and that is a fact about
// routing, not a failure.
func readPrompt(f *os.File, wait time.Duration) (string, bool) {
	if f == nil {
		return "", false
	}
	// Stat's error is CHECKED. It used to be discarded, and the following
	// stat.Mode() then dereferenced a nil FileInfo — a startup panic on any fd
	// that could not be stat'd.
	st, err := f.Stat()
	if err != nil {
		return "", false
	}
	// A character device is a terminal, /dev/null, and friends. Nobody piped a
	// prompt through one; reading would consume the user's keystrokes.
	if st.Mode()&os.ModeCharDevice != 0 {
		return "", false
	}

	first := make([]byte, 1)
	type readResult struct {
		n   int
		err error
	}
	done := make(chan readResult, 1)
	go func() {
		n, rerr := f.Read(first)
		done <- readResult{n, rerr}
	}()

	var got readResult
	if wait <= 0 {
		got = <-done
	} else {
		timer := time.NewTimer(wait)
		defer timer.Stop()
		select {
		case got = <-done:
		case <-timer.C:
			// The goroutine stays blocked in read(2) on a pipe nobody is writing
			// to. That is harmless and cannot steal keystrokes: we only get here
			// when stdin is NOT a terminal, so it is not the TUI's input either.
			return "", false
		}
	}
	if got.n == 0 {
		return "", false // EOF on a closed pipe, or a read error
	}

	rest, _ := io.ReadAll(f)
	prompt := strings.TrimSpace(string(first[:got.n]) + string(rest))
	return prompt, prompt != ""
}

// stdinPrompt is readPrompt against the real stdin.
func stdinPrompt() (string, bool) { return readPrompt(os.Stdin, stdinWait()) }

// stdinIsTerminal reports whether a human (or a pty) is on the other end.
//
// This is what the unattended path should have been asking all along. It used to
// pass "stdin is not a character device", which calls `ycode < /dev/null` a
// terminal — /dev/null IS a character device — and so opened a TUI nobody could
// type into.
func stdinIsTerminal() bool { return term.IsTerminal(int(os.Stdin.Fd())) }

// interactiveCapable reports whether a TUI would have somewhere to draw and
// someone to type. Both halves are required: painting an alt-screen into a
// captured pipe and then blocking forever is worse than the error it replaces.
//
// stdin being a terminal covers the normal case AND the pty that agentpty
// allocates for a steerable `bashy chat` session, which is why steering keeps
// working unchanged.
func interactiveCapable() bool {
	return term.IsTerminal(int(os.Stdout.Fd())) && stdinIsTerminal()
}
