package wrap

import (
	"log/slog"
	"os"
	"strings"
)

// initLoggerFromEnv sets slog.Default() to a text handler at the level
// declared by YCODE_LOG_LEVEL (default INFO). Called from both wrap.Run
// (the parent CLI path) and ShimMain (the child shim path) because
// neither runs the cobra+app slog wiring that the TUI does — without
// this call, slog.Debug emits land in the default INFO handler and the
// per-exec ExecScopeWrappedAgent span debug line (emitted via
// telotel.StartExecSpan's finish closure) is invisible no matter what
// level the operator asks for.
//
// The level mapping mirrors what ycode's cli/app.go does at startup so
// `YCODE_LOG_LEVEL=debug` produces identical behavior whether the
// process is a foreground TUI, a shim invocation, or a wrap parent.
func initLoggerFromEnv() {
	lvl := slog.LevelInfo
	switch strings.ToLower(strings.TrimSpace(os.Getenv("YCODE_LOG_LEVEL"))) {
	case "debug", "trace":
		lvl = slog.LevelDebug
	case "warn", "warning":
		lvl = slog.LevelWarn
	case "error":
		lvl = slog.LevelError
	}
	handler := slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: lvl})
	slog.SetDefault(slog.New(handler))
}
