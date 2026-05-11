//go:build !experimental

package main

import (
	"context"

	"github.com/qiangli/ycode/internal/runtime/browser"
	"github.com/qiangli/ycode/internal/runtime/config"
)

// setupBrowserBackend is a no-op in the stable build. The experimental
// build replaces this with code that wires live / probe / solo and
// returns a browser.Client. Stable always returns nil — tool handlers
// then surface the friendly "configure browser.mode" message.
func setupBrowserBackend(_ context.Context, _ *config.Config) browser.Client {
	return nil
}
