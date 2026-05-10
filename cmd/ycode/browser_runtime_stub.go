//go:build !experimental

package main

import (
	"context"

	"github.com/qiangli/ycode/internal/runtime/config"
)

// setupBrowserBackend is a no-op in the stable build. The experimental
// build replaces this with code that registers playwright-mcp /
// chrome-devtools-mcp / browsermcp based on cfg.Browser.Provider.
func setupBrowserBackend(_ context.Context, _ *config.Config) {}
