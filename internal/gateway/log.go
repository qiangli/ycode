package gateway

import "log/slog"

// logServeError reports a backend listener that died unexpectedly. The
// gateway itself stays up so the other backend (if any) keeps serving.
func logServeError(which string, err error) {
	slog.Error("gateway listener exited", "backend", which, "err", err)
}
