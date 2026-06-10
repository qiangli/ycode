//go:build !windows

package wrap

import (
	"context"
	"encoding/json"
	"log/slog"
	"net"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/trace"

	"github.com/qiangli/ycode/internal/runtime/spawncore"
)

// Spawn-event aggregation: the wrap parent listens on a per-session
// unix datagram socket; every shim dispatch fires one fire-and-forget
// SpawnEvent at it before exec(2)ing the real tool (see
// internal/runtime/spawncore). Datagrams mean no connection state, no
// fd passing, no replies — the shim never waits on us and our death
// never blocks a command.
//
// The aggregate is the spawn-side forensic for the 2026-06 OOM
// family: a per-minute log line naming which tools the wrapped agent
// is launching and how fast, visible while a storm is happening
// instead of reconstructed afterwards.

// spawnRateWarn is the per-minute spawn count past which the summary
// escalates from Info to Warn. Builds legitimately burst; sustained
// hundreds per minute is a runaway loop.
const spawnRateWarn = 600

type spawnStats struct {
	mu     sync.Mutex
	counts map[string]int // per-tool, session total
	total  uint64
}

func (s *spawnStats) add(ev spawncore.SpawnEvent) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.counts[ev.Tool]++
	s.total++
}

// snapshot returns the session total and a "tool=n tool=n" line of
// the top n tools by count.
func (s *spawnStats) snapshot(n int) (uint64, string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	type kv struct {
		tool string
		n    int
	}
	top := make([]kv, 0, len(s.counts))
	for t, c := range s.counts {
		top = append(top, kv{t, c})
	}
	sort.Slice(top, func(i, j int) bool { return top[i].n > top[j].n })
	if len(top) > n {
		top = top[:n]
	}
	parts := make([]string, 0, len(top))
	for _, e := range top {
		parts = append(parts, e.tool+"="+strconv.Itoa(e.n))
	}
	return s.total, strings.Join(parts, " ")
}

// spawnListener owns the per-session event socket plus its read and
// summary goroutines.
type spawnListener struct {
	sockPath string
	conn     *net.UnixConn
	stats    *spawnStats
	done     chan struct{}
	stopOnce sync.Once

	// Span recording for "exit" events (shims in YCODE_WRAP_SPAWN_TRACE
	// fork-and-wait mode). Set via enableSpans after the wrap parent
	// opens its session span; nil until then, so early events degrade
	// to stats-only.
	spanMu  sync.Mutex
	spanCtx context.Context
}

// startSpawnEventListener opens the per-session event socket under
// sessionDir and starts the read + summary goroutines. Errors are
// returned for the caller to log-and-continue: a wrap session without
// spawn telemetry is degraded, not broken.
func startSpawnEventListener(sessionDir string) (*spawnListener, error) {
	sockPath := filepath.Join(sessionDir, "events.sock")
	addr, err := net.ResolveUnixAddr("unixgram", sockPath)
	if err != nil {
		return nil, err
	}
	conn, err := net.ListenUnixgram("unixgram", addr)
	if err != nil {
		return nil, err
	}

	l := &spawnListener{
		sockPath: sockPath,
		conn:     conn,
		stats:    &spawnStats{counts: make(map[string]int)},
		done:     make(chan struct{}),
	}
	go l.readLoop()
	go l.summaryLoop()
	return l, nil
}

func (l *spawnListener) readLoop() {
	buf := make([]byte, 4096)
	for {
		n, _, err := l.conn.ReadFromUnix(buf)
		if err != nil {
			return // conn closed by stop
		}
		var ev spawncore.SpawnEvent
		if json.Unmarshal(buf[:n], &ev) != nil {
			continue
		}
		switch ev.Ev {
		case "spawn":
			l.stats.add(ev)
		case "exit":
			l.recordSpawnSpan(ev)
		}
	}
}

// enableSpans arms exit-event span recording, nesting every spawned-
// tool span under the wrap session span carried by ctx. Called by Run
// once the session span exists.
func (l *spawnListener) enableSpans(ctx context.Context) {
	l.spanMu.Lock()
	l.spanCtx = ctx
	l.spanMu.Unlock()
}

// recordSpawnSpan reconstructs a retroactive OTel span from a span-
// mode shim's "exit" event: the shim observed start/duration/exit
// code as the child's parent (the only process that can, on unix) and
// shipped them here, where a real tracer provider exists. Timestamps
// are back-dated via trace.WithTimestamp so the span covers the
// command's actual lifetime.
func (l *spawnListener) recordSpawnSpan(ev spawncore.SpawnEvent) {
	l.spanMu.Lock()
	ctx := l.spanCtx
	l.spanMu.Unlock()
	if ctx == nil {
		return
	}
	end := time.Now()
	start := end.Add(-time.Duration(ev.DurMs) * time.Millisecond)
	exitCode := 0
	if ev.ExitCode != nil {
		exitCode = *ev.ExitCode
	}
	_, span := otel.Tracer("ycode.wrap.spawn").Start(ctx, "ycode.exec.spawned_tool",
		trace.WithTimestamp(start),
		trace.WithAttributes(
			attribute.String("exec.tool", ev.Tool),
			attribute.Int("exec.depth", ev.Depth),
			attribute.Int("exec.exit_code", exitCode),
			attribute.Int("exec.pid", ev.PID),
			attribute.Int64("exec.duration_ms", ev.DurMs),
		))
	span.End(trace.WithTimestamp(end))
}

func (l *spawnListener) summaryLoop() {
	ticker := time.NewTicker(time.Minute)
	defer ticker.Stop()
	var lastTotal uint64
	for {
		select {
		case <-l.done:
			return
		case <-ticker.C:
			total, top := l.stats.snapshot(5)
			delta := total - lastTotal
			lastTotal = total
			if delta == 0 {
				continue
			}
			if delta > spawnRateWarn {
				slog.Warn("wrap: high spawn rate", "spawns_1m", delta, "total", total, "top", top)
			} else {
				slog.Info("wrap: spawn activity", "spawns_1m", delta, "total", total, "top", top)
			}
		}
	}
}

// stop closes the socket and logs the final session summary.
func (l *spawnListener) stop() {
	l.stopOnce.Do(func() {
		close(l.done)
		_ = l.conn.Close()
		if total, top := l.stats.snapshot(5); total > 0 {
			slog.Info("wrap: session spawn summary", "total", total, "top", top)
		}
	})
}
