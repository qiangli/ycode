//go:build experimental

// Circuit breaker — 3-level failure tracking that aborts further
// attempts when a pattern of failures emerges. Modeled after
// openchrome's (MIT) breaker:
//
//   - Element: 3 failures within 2 minutes on the same selector
//     short-circuits subsequent calls for that selector for 2 min.
//   - Page: 5 distinct selectors fail on the same URL within 5 min
//     produces a synthetic error suggesting a reload.
//   - Global: 10 failures within 5 min stops all browser activity
//     for 60 seconds.

package reliability

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"sync"
	"time"

	"github.com/qiangli/ycode/internal/runtime/mcpservers"
	telotel "github.com/qiangli/ycode/internal/telemetry/otel"
)

const (
	elementWindow   = 2 * time.Minute
	elementMaxFails = 3
	pageWindow      = 5 * time.Minute
	pageMaxDistinct = 5
	globalWindow    = 5 * time.Minute
	globalMaxFails  = 10
	globalCooldown  = 60 * time.Second
)

type breakerWrapper struct {
	inner *breaker
	next  mcpservers.Service
}

func (b *breakerWrapper) Name() string                       { return b.next.Name() }
func (b *breakerWrapper) Available(ctx context.Context) bool { return b.next.Available(ctx) }
func (b *breakerWrapper) EnsureReady(ctx context.Context) error {
	return b.next.EnsureReady(ctx)
}
func (b *breakerWrapper) Stop(ctx context.Context) error { return b.next.Stop(ctx) }

func (b *breakerWrapper) Execute(ctx context.Context, action mcpservers.BrowserAction) (*mcpservers.BrowserResult, error) {
	if reason := b.inner.gate(action); reason != "" {
		telotel.RecordBrowserBreakerTrip(ctx, b.next.Name(), breakerLevel(reason))
		return &mcpservers.BrowserResult{
			Error: fmt.Sprintf("circuit-breaker: %s", reason),
		}, nil
	}
	res, err := b.next.Execute(ctx, action)
	failed := err != nil || (res != nil && (!res.Success || res.Error != ""))
	b.inner.record(action, res, failed)
	return res, err
}

// breakerLevel parses the gate-reason prefix into one of element /
// page / global for metric attribution.
func breakerLevel(reason string) string {
	switch {
	case strings.HasPrefix(reason, "global"):
		return "global"
	case strings.HasPrefix(reason, "element"):
		return "element"
	case strings.HasPrefix(reason, "page"):
		return "page"
	}
	return "unknown"
}

// breaker holds the failure counters. Concurrent-safe.
type breaker struct {
	mu sync.Mutex

	elementFails map[string][]time.Time // selector → recent fail timestamps
	pageFails    map[string][]string    // url → distinct selectors
	pageFailAt   map[string][]time.Time // url → timestamps of distinct selector failures
	globalFails  []time.Time

	globalPausedUntil time.Time
}

func newBreaker() *breaker {
	return &breaker{
		elementFails: make(map[string][]time.Time),
		pageFails:    make(map[string][]string),
		pageFailAt:   make(map[string][]time.Time),
	}
}

// gate returns a non-empty reason string when the action should be
// short-circuited.
func (b *breaker) gate(action mcpservers.BrowserAction) string {
	b.mu.Lock()
	defer b.mu.Unlock()

	now := time.Now()
	if now.Before(b.globalPausedUntil) {
		left := time.Until(b.globalPausedUntil).Round(time.Second)
		return fmt.Sprintf("global cooldown active (%s remaining); too many recent failures", left)
	}
	if action.Type == mcpservers.ActionClick || action.Type == mcpservers.ActionType {
		key := action.Selector
		if key == "" {
			return ""
		}
		recent := trimOld(b.elementFails[key], now.Add(-elementWindow))
		if len(recent) >= elementMaxFails {
			return fmt.Sprintf("element %q has failed %d times in the last %s; skipping",
				key, len(recent), elementWindow)
		}
	}
	return ""
}

func (b *breaker) record(action mcpservers.BrowserAction, res *mcpservers.BrowserResult, failed bool) {
	b.mu.Lock()
	defer b.mu.Unlock()

	now := time.Now()
	if failed {
		b.globalFails = trimOld(append(b.globalFails, now), now.Add(-globalWindow))
		if len(b.globalFails) >= globalMaxFails {
			b.globalPausedUntil = now.Add(globalCooldown)
		}
	}
	if !failed {
		return
	}
	if action.Type == mcpservers.ActionClick || action.Type == mcpservers.ActionType {
		key := action.Selector
		if key != "" {
			b.elementFails[key] = trimOld(append(b.elementFails[key], now), now.Add(-elementWindow))
		}
	}
	if res != nil && res.URL != "" {
		url := res.URL
		// Record distinct selectors that failed on this URL.
		key := action.Selector
		if key == "" {
			key = action.URL
		}
		seen := false
		for _, prev := range b.pageFails[url] {
			if prev == key {
				seen = true
				break
			}
		}
		if !seen {
			b.pageFails[url] = append(b.pageFails[url], key)
			b.pageFailAt[url] = append(b.pageFailAt[url], now)
		}
		// Drop entries that are older than the window.
		cutoff := now.Add(-pageWindow)
		kept := b.pageFails[url][:0]
		keptAt := b.pageFailAt[url][:0]
		for i, t := range b.pageFailAt[url] {
			if t.After(cutoff) {
				kept = append(kept, b.pageFails[url][i])
				keptAt = append(keptAt, t)
			}
		}
		b.pageFails[url] = kept
		b.pageFailAt[url] = keptAt
		if len(kept) >= pageMaxDistinct && res != nil {
			res.Hints = append(res.Hints,
				fmt.Sprintf("page_unstable: %d distinct selectors failed on %s in the last %s; consider reloading",
					len(kept), url, pageWindow))
		}
	}
}

func trimOld(times []time.Time, cutoff time.Time) []time.Time {
	out := times[:0]
	for _, t := range times {
		if t.After(cutoff) {
			out = append(out, t)
		}
	}
	return out
}

// errBreakerStub keeps the linter happy if a future refactor needs a
// sentinel.
var errBreakerStub = errors.New("circuit-breaker: stub error not used")

var _ = errBreakerStub // intentionally unused; reserved
