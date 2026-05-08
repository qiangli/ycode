package main

import (
	"context"
	"log/slog"
	"os"

	"github.com/qiangli/ycode/internal/selfinit"
)

// runSelfInit invokes selfinit.Run with the host environment. Errors
// are logged at debug level and never propagated — selfinit must not
// gate the user's command. Per-process: a global env opt-out skips the
// run entirely.
func runSelfInit(ctx context.Context) {
	if os.Getenv("YCODE_NO_SELF_INIT") == "1" {
		return
	}
	res, err := selfinit.Run(ctx, selfinit.Options{
		YcodeVersion: version,
		Logger:       slog.Default(),
	})
	if err != nil {
		slog.Debug("selfinit: run", "err", err)
		return
	}
	if res.Skipped || res.OptedOut {
		return
	}
	// First-time init in a fresh repo — print one informational line so
	// the user knows ycode just did something to their files.
	if res.RepoRoot != "" && len(res.ProjectFiles) > 0 {
		slog.Info("ycode: first-class citizen in this repo",
			"repo", res.RepoRoot,
			"files", res.ProjectFiles)
	}
}
