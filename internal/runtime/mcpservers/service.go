//go:build experimental

package mcpservers

import "context"

// Service is the unified interface implemented by every browser
// backend mode (live, probe, solo). The framework guarantees:
//
//   - Available is safe to call before EnsureReady; it must not
//     perform side effects.
//   - EnsureReady is idempotent and may take seconds (Chrome
//     launch, container start). It must complete before Execute.
//   - Execute takes a unified BrowserAction and dispatches it to
//     the underlying driver (CDP, WebSocket-to-extension). Safe
//     for concurrent calls from multiple goroutines.
//   - Stop is best-effort cleanup; callers must tolerate errors.
type Service interface {
	Name() string
	Available(ctx context.Context) bool
	EnsureReady(ctx context.Context) error
	Execute(ctx context.Context, action BrowserAction) (*BrowserResult, error)
	Stop(ctx context.Context) error
}

// Operating modes for ycode's browser stack. Set via
// settings.json's `browser.mode` (see internal/runtime/config).
const (
	ModeLive  = "live"  // ycode-owned Chrome extension; user's real session
	ModeProbe = "probe" // CDP attach to a Chrome started with --remote-debugging-port
	ModeSolo  = "solo"  // chromedp-launched fresh Chrome, full automation
)
