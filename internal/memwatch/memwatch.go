// Package memwatch is a lightweight self-instrumentation sampler for
// long-lived ycode processes (`ycode serve`, `ycode mcp serve`).
//
// Motivation: the 2026-06 OOM incidents (200–300GB resident in an
// unidentified process, twice) left no record of which process held
// the memory or when it started growing — Activity Monitor's stats
// were already unreadable by the time a human looked. A once-a-minute
// sample of our own RSS + Go heap, logged through the structured
// logger, costs nothing and turns the next incident into a grep.
//
// Logging policy: Debug when healthy (invisible at default level),
// Warn past 2GB RSS, Error past 8GB. A healthy idle daemon emits
// nothing at default log levels.
package memwatch

import (
	"context"
	"log/slog"
	"os"
	"os/exec"
	"runtime"
	"strconv"
	"strings"
	"time"
)

const sampleInterval = 60 * time.Second

const (
	warnRSS  = 2 << 30
	errorRSS = 8 << 30
)

// Attrs returns extra slog attributes appended to each sample —
// callers use it to expose subsystem counters (bus event rates,
// connection counts) alongside the memory numbers. Called from the
// sampler goroutine only.
type Attrs func() []any

// Start launches the sampler goroutine. It stops when ctx is
// cancelled. logger may be nil (slog.Default()); extra may be nil.
func Start(ctx context.Context, label string, logger *slog.Logger, extra Attrs) {
	if logger == nil {
		logger = slog.Default()
	}
	go func() {
		ticker := time.NewTicker(sampleInterval)
		defer ticker.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				sample(label, logger, extra)
			}
		}
	}()
}

func sample(label string, logger *slog.Logger, extra Attrs) {
	var ms runtime.MemStats
	runtime.ReadMemStats(&ms)
	rss := selfRSSBytes()
	attrs := []any{
		"label", label,
		"rss_mb", rss >> 20,
		"heap_mb", ms.HeapAlloc >> 20,
		"gosys_mb", ms.Sys >> 20,
		"goroutines", runtime.NumGoroutine(),
	}
	if extra != nil {
		attrs = append(attrs, extra()...)
	}
	switch {
	case rss > errorRSS:
		logger.Error("memwatch: process ballooning", attrs...)
	case rss > warnRSS:
		logger.Warn("memwatch: elevated memory", attrs...)
	default:
		logger.Debug("memwatch", attrs...)
	}
}

// selfRSSBytes returns this process's resident set size. `ps` keeps
// it portable across macOS and Linux (no /proc on darwin); at one
// fork a minute the cost is noise. Returns 0 when ps is unavailable
// (Windows) — the Go-heap numbers still flow.
func selfRSSBytes() int64 {
	out, err := exec.Command("ps", "-o", "rss=", "-p", strconv.Itoa(os.Getpid())).Output()
	if err != nil {
		return 0
	}
	kb, err := strconv.ParseInt(strings.TrimSpace(string(out)), 10, 64)
	if err != nil {
		return 0
	}
	return kb << 10
}
