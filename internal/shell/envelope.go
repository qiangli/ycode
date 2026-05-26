package shell

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"time"
)

// Envelope is the JSON wrapper emitted by `ycode shell --json -c "..."`
// (and any other path that opts into envelope output). Stdout / stderr
// stay separate fields so agents can parse error messages out of band.
type Envelope struct {
	ExitCode   int     `json:"exit_code"`
	Stdout     string  `json:"stdout"`
	Stderr     string  `json:"stderr"`
	DurationMS int64   `json:"duration_ms"`
	Intent     IntentV `json:"intent"`
	Hints      []Hint  `json:"hints,omitempty"`
	Command    string  `json:"command"`
}

// IntentV is the JSON-serializable view of an Intent. We don't encode
// the IntentKind enum directly (it's an int) — agents need stable
// strings.
type IntentV struct {
	Kind       string `json:"kind"`
	Name       string `json:"name,omitempty"`
	Path       string `json:"path,omitempty"`
	Args       string `json:"args,omitempty"`
	Upstream   string `json:"upstream,omitempty"`
	Downstream string `json:"downstream,omitempty"`
}

func intentView(in Intent) IntentV {
	return IntentV{
		Kind:       in.Kind.String(),
		Name:       in.Name,
		Path:       in.Path,
		Args:       in.Args,
		Upstream:   in.Upstream,
		Downstream: in.Downstream,
	}
}

// DispatchEnvelope runs a command end-to-end and returns the JSON
// Envelope. Used by --json mode in -c invocations and by the MCP
// agent_shell tool. The dispatcher's stdout/stderr go into the
// envelope; nothing is written to w except the final JSON object.
func DispatchEnvelope(
	ctx context.Context,
	rt *ShellRuntime,
	command string,
	hints []Hint,
) Envelope {
	start := time.Now()

	intent, classifyErr := Classify(command)
	if classifyErr != nil {
		return Envelope{
			ExitCode:   2,
			Stderr:     "shell: " + classifyErr.Error() + "\n",
			DurationMS: time.Since(start).Milliseconds(),
			Intent:     intentView(intent),
			Hints:      hints,
			Command:    command,
		}
	}

	d := NewDispatcher(rt)
	var stdout, stderr bytes.Buffer
	sink := WriterSink{StdoutW: &stdout, StderrW: &stderr}

	res, derr := d.Dispatch(ctx, intent, sink)
	if derr != nil {
		fmt.Fprintf(&stderr, "shell: dispatch error: %v\n", derr)
	}

	// Filter pre-exec hints that opted into success-suppression: when
	// the command exited 0 those hints are pure context bloat. Done
	// in-place because hints is owned here.
	if res.ExitCode == 0 {
		hints = filterSkipOnSuccess(hints)
	}
	// Post-exec hints can now consult the captured stderr.
	hints = append(hints, postHints(rt, res.ExitCode, stderr.String())...)

	return Envelope{
		ExitCode:   res.ExitCode,
		Stdout:     stdout.String(),
		Stderr:     stderr.String(),
		DurationMS: time.Since(start).Milliseconds(),
		Intent:     intentView(intent),
		Hints:      hints,
		Command:    command,
	}
}

// filterSkipOnSuccess drops every hint whose SkipOnSuccess flag is set.
// Used by both --json mode and the plain-mode caller to avoid emitting
// "you might prefer yc <verb>" nudges when the user's command worked
// fine. Returns a fresh slice; the input is unmodified.
func filterSkipOnSuccess(in []Hint) []Hint {
	out := in[:0:0]
	for _, h := range in {
		if h.SkipOnSuccess {
			continue
		}
		out = append(out, h)
	}
	return out
}

// postHints calls the agentmode post-exec catalog. Filled in via
// SetPostHintsFunc to avoid an import cycle.
func postHints(rt *ShellRuntime, exitCode int, stderr string) []Hint {
	if postHintsFn == nil {
		return nil
	}
	return postHintsFn(rt, exitCode, stderr)
}

var postHintsFn func(rt *ShellRuntime, exitCode int, stderr string) []Hint

// SetPostHintsFunc is called by internal/shell/agentmode/init() to wire
// the post-exec hint engine.
func SetPostHintsFunc(fn func(rt *ShellRuntime, exitCode int, stderr string) []Hint) {
	postHintsFn = fn
}

// WriteEnvelopeJSON emits the envelope as pretty-printed JSON.
func WriteEnvelopeJSON(env Envelope, w io.Writer) error {
	enc := json.NewEncoder(w)
	enc.SetIndent("", "  ")
	return enc.Encode(env)
}

// DispatchEnvelopeAt is DispatchEnvelope with a per-call working directory.
// When workDir is empty, falls through to DispatchEnvelope (preserves stdio
// behavior of inheriting the runtime's cwd). When workDir is set, validates
// it (must be absolute, must exist, must be a directory) and runs the
// command in a one-shot ShellRuntime rooted there. The shared runtime is
// not mutated, so concurrent HTTP MCP callers each get their own bash
// session at their own cwd.
func DispatchEnvelopeAt(
	ctx context.Context,
	rt *ShellRuntime,
	command string,
	hints []Hint,
	workDir string,
) Envelope {
	if workDir == "" {
		return DispatchEnvelope(ctx, rt, command, hints)
	}
	if err := validateWorkDir(workDir); err != nil {
		return Envelope{
			ExitCode:   2,
			Stderr:     "shell: " + err.Error() + "\n",
			DurationMS: 0,
			Intent:     IntentV{Kind: "Bash"},
			Hints:      hints,
			Command:    command,
		}
	}
	callRT, err := rt.cloneAt(workDir)
	if err != nil {
		return Envelope{
			ExitCode:   2,
			Stderr:     "shell: cloneAt: " + err.Error() + "\n",
			DurationMS: 0,
			Intent:     IntentV{Kind: "Bash"},
			Hints:      hints,
			Command:    command,
		}
	}
	defer func() { _ = callRT.Close() }()
	return DispatchEnvelope(ctx, callRT, command, hints)
}

// validateWorkDir enforces the per-call cwd contract: absolute path that
// exists and is a directory. Returns a structured error otherwise so the
// caller can surface it in the Envelope's stderr.
func validateWorkDir(workDir string) error {
	if !filepath.IsAbs(workDir) {
		return fmt.Errorf("cwd must be an absolute path, got %q", workDir)
	}
	info, err := os.Stat(workDir)
	if err != nil {
		return fmt.Errorf("cwd %q: %w", workDir, err)
	}
	if !info.IsDir() {
		return fmt.Errorf("cwd %q is not a directory", workDir)
	}
	return nil
}
