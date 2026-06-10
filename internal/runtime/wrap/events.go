//go:build !windows

package wrap

import (
	"encoding/json"
	"log/slog"
	"net"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

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
		if json.Unmarshal(buf[:n], &ev) == nil && ev.Ev == "spawn" {
			l.stats.add(ev)
		}
	}
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
