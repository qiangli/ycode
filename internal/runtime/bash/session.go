package bash

import (
	"strings"
	"sync"
)

// cwdMarker is the sentinel that separates command output from the cwd capture.
const cwdMarker = "__YCODE_CWD__"

// ShellSession tracks persistent shell state (working directory) across
// sequential bash invocations. Each conversation gets one ShellSession.
type ShellSession struct {
	mu      sync.Mutex
	workDir string
}

// NewShellSession creates a session starting at the given directory.
func NewShellSession(initialDir string) *ShellSession {
	return &ShellSession{workDir: initialDir}
}

// WorkDir returns the current working directory.
func (s *ShellSession) WorkDir() string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.workDir
}

// WrapCommand appends a cwd-capture suffix to the command so we can
// detect directory changes after execution. The suffix emits a unique
// marker followed by `pwd`, allowing ParseOutput to extract the new cwd.
func (s *ShellSession) WrapCommand(command string) string {
	s.mu.Lock()
	defer s.mu.Unlock()

	// Wrap the original command in a subshell group so exit code is preserved.
	// After the command runs, emit the marker and pwd.
	return "cd " + shellQuote(s.workDir) + " && { " + command +
		"; }; __ycode_ec=$?; echo; echo " + cwdMarker + "; pwd; exit $__ycode_ec"
}

// ParseOutput extracts the cwd marker from command output, updates the
// session's working directory, and returns the output with the marker
// lines stripped.
func (s *ShellSession) ParseOutput(stdout string) string {
	idx := strings.LastIndex(stdout, cwdMarker)
	if idx < 0 {
		return stdout
	}

	// Everything before the marker is the real command output.
	before := stdout[:idx]
	// Everything after the marker line is the pwd output.
	after := stdout[idx+len(cwdMarker):]

	// The pwd is on the line after the marker.
	lines := strings.SplitN(strings.TrimSpace(after), "\n", 2)
	if len(lines) > 0 && lines[0] != "" {
		newDir := strings.TrimSpace(lines[0])
		s.mu.Lock()
		s.workDir = newDir
		s.mu.Unlock()
	}

	// Clean up trailing whitespace from the real output.
	return strings.TrimRight(before, "\n \t")
}

// shellQuote wraps a string in single quotes for safe shell embedding.
func shellQuote(s string) string {
	// Replace single quotes with '\'' (end quote, escaped quote, start quote).
	return "'" + strings.ReplaceAll(s, "'", "'\\''") + "'"
}
