package main

import (
	"log/slog"
	"os"
	"path/filepath"

	"go.opentelemetry.io/otel/trace"

	"github.com/qiangli/ycode/internal/observe"
)

// actionLogEnabled reports whether the per-turn action log should be written.
// It follows the same opt-out shape as the rest of observability: on by default,
// disabled by YCODE_ACTION_LOG=off/0/false.
func actionLogEnabled() bool {
	switch os.Getenv("YCODE_ACTION_LOG") {
	case "off", "0", "false", "no":
		return false
	default:
		return true
	}
}

// actionLogVerbose reports whether full prompts should be captured. Driven by
// the --trace-verbose flag, with a YCODE_TRACE_VERBOSE env fallback so the
// setting survives into subprocess agents.
func actionLogVerbose() bool {
	if traceVerbose {
		return true
	}
	switch os.Getenv("YCODE_TRACE_VERBOSE") {
	case "1", "true", "yes", "on":
		return true
	default:
		return false
	}
}

// buildActionRecorder constructs the per-turn action recorder for a session. It
// writes JSONL to <instanceDir>/actions.jsonl and, when tracer is non-nil, also
// emits an agent.turn span per turn. The returned cleanup closes the summary and
// the file; it is safe to call once at shutdown. Returns (nil, noop) when the
// action log is disabled or the file cannot be opened.
func buildActionRecorder(instanceDir, sessionID string, tracer trace.Tracer) (*observe.Recorder, func()) {
	noop := func() {}
	if !actionLogEnabled() {
		return nil, noop
	}
	if err := os.MkdirAll(instanceDir, 0o755); err != nil {
		slog.Debug("action log: mkdir failed", "dir", instanceDir, "error", err)
		return nil, noop
	}
	path := filepath.Join(instanceDir, "actions.jsonl")
	f, err := os.OpenFile(path, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o644)
	if err != nil {
		slog.Debug("action log: open failed", "path", path, "error", err)
		return nil, noop
	}
	rec := observe.New(observe.Options{
		Writer:    f,
		SessionID: sessionID,
		Verbose:   actionLogVerbose(),
		Tracer:    tracer,
	})
	slog.Debug("action log: initialized", "path", path, "verbose", actionLogVerbose())
	cleanup := func() {
		// Write the session summary as the final line, then close the file.
		rec.Finish()
		if err := f.Close(); err != nil {
			slog.Debug("action log: close failed", "error", err)
		}
	}
	return rec, cleanup
}
