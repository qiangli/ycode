//go:build !windows

package main

import (
	"bufio"
	"encoding/base64"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net"
	"os"
	"os/exec"
	"os/signal"
	"sort"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"syscall"
	"time"

	"github.com/creack/pty/v2"
	"golang.org/x/term"
)

// runWeaveToolPTY launches cmd attached to a freshly-allocated PTY.
//
// Stdin/stdout routing depends on the parent's terminal:
//   - parent stdin IS a TTY: switch to raw mode, bidirectionally
//     copy stdin↔PTY so the user types into the subagent's TUI and
//     sees it render normally. SIGWINCH propagates terminal resizes.
//   - parent stdin is NOT a TTY (orchestrator pipe, backgrounded
//     by shell &): logSink receives the PTY output verbatim and
//     subagent stdin is fed from /dev/null. The orchestrator's
//     pipes are not held open by us — the subagent thinks it has
//     a controlling terminal even though no human is attached.
//
// logSink is only used in the non-TTY path; pass nil for the TTY
// pass-through case. guards carries the three watchdog tripwires
// (idle, wall-clock, memory) — see weaveGuards. Returns the
// subagent's exit code (or 128+N when killed by signal N, matching
// the wrap helper).
func runWeaveToolPTY(cmd *exec.Cmd, logSink io.Writer, guards weaveGuards) (int, error) {
	rows, cols := weavePTYSize()
	ptmx, err := pty.StartWithSize(cmd, &pty.Winsize{Rows: rows, Cols: cols})
	if err != nil {
		return 127, fmt.Errorf("pty.Start: %w", err)
	}
	defer ptmx.Close()

	// killTree terminates the subagent and every transitive child.
	// pty.Start put the subagent in its own session (it must be a
	// session leader to own the PTY), so the wrapper's process
	// group does NOT contain it — and the subagent's descendants
	// (claude's MCP servers, shell shims) may setpgid themselves
	// out of the subagent's group. Signalling only -pid therefore
	// strands grandchildren; the dogfood OOM left an orphaned
	// claude tree running for 15+ minutes after `weave abandon`.
	// We signal the group AND each descendant from a `ps` snapshot,
	// then escalate to SIGKILL after a grace window.
	var killOnce sync.Once
	killTree := func(reason string, grace time.Duration) {
		killOnce.Do(func() {
			if cmd.Process == nil {
				return
			}
			pid := cmd.Process.Pid
			slog.Warn("weave: terminating subagent tree", "pid", pid, "reason", reason)
			if logSink != nil {
				fmt.Fprintf(logSink, "\r\n[weave] terminating subagent: %s\r\n", reason)
				// Forensic snapshot while `ps` still works: the
				// 2026-06 OOM post-mortems had no record of which
				// process actually held the memory. Tree-local AND
				// system-wide, so a culprit outside the subagent
				// tree is still named.
				if tree, system := weaveForensicSnapshot(pid); tree != "" {
					fmt.Fprintf(logSink, "[weave] top tree procs:   %s\r\n", tree)
					fmt.Fprintf(logSink, "[weave] top system procs: %s\r\n", system)
				}
			}
			pids := weaveProcTreePids(pid)
			_ = syscall.Kill(-pid, syscall.SIGTERM)
			for _, p := range pids {
				_ = syscall.Kill(p, syscall.SIGTERM)
			}
			go func() {
				time.Sleep(grace)
				if syscall.Kill(pid, 0) != nil {
					return // leader reaped; Wait() unblocks
				}
				_ = syscall.Kill(-pid, syscall.SIGKILL)
				for _, p := range weaveProcTreePids(pid) {
					_ = syscall.Kill(p, syscall.SIGKILL)
				}
			}()
		})
	}

	// Forward termination signals from the wrapper to the subagent
	// tree. Without this, SIGTERM kills the wrapper instantly
	// (default disposition) and the subagent — in its own session —
	// survives as an orphan that `weave kill`/`abandon` can never
	// reach again. Short grace: weaveStopWrapper SIGKILLs the
	// wrapper 5s after its SIGTERM, and our escalation goroutine
	// must fire before we die.
	termSigs := make(chan os.Signal, 1)
	signal.Notify(termSigs, syscall.SIGTERM, syscall.SIGINT, syscall.SIGHUP)
	go func() {
		for s := range termSigs {
			killTree(fmt.Sprintf("signal %v forwarded from wrapper", s), 2*time.Second)
		}
	}()
	defer func() {
		// Stop must precede close — the signal package does a
		// non-blocking send to registered channels, and a send on
		// a closed channel panics.
		signal.Stop(termSigs)
		close(termSigs)
	}()

	// Idle-timeout watchdog: tracks the time of the most recent PTY
	// write. A background goroutine kills the subagent tree if that
	// timestamp stalls past idleTimeout. The watchdog reads the
	// timestamp via atomic load; the copy loop bumps it on every
	// write.
	var lastWriteUnixNs atomic.Int64
	lastWriteUnixNs.Store(time.Now().UnixNano())
	watchdogStop := make(chan struct{})
	defer close(watchdogStop)
	if guards.idleTimeout > 0 {
		go func() {
			ticker := time.NewTicker(guards.idleTimeout / 4)
			defer ticker.Stop()
			for {
				select {
				case <-watchdogStop:
					return
				case <-ticker.C:
					last := time.Unix(0, lastWriteUnixNs.Load())
					if time.Since(last) >= guards.idleTimeout {
						killTree(fmt.Sprintf("idle %s exceeds --idle-timeout %s",
							time.Since(last).Round(time.Second), guards.idleTimeout), 10*time.Second)
						return
					}
				}
			}
		}()
	}

	// Wall-clock watchdog: --idle-timeout is useless against a
	// runaway TUI whose spinner keeps emitting output; this one
	// cannot be reset by activity.
	if guards.maxRuntime > 0 {
		go func() {
			select {
			case <-watchdogStop:
			case <-time.After(guards.maxRuntime):
				killTree(fmt.Sprintf("runtime exceeds --max-runtime %s", guards.maxRuntime), 10*time.Second)
			}
		}()
	}

	// Memory watchdog: sums RSS across the subagent's process tree
	// every poll and kills the tree over budget. This is the OOM
	// backstop — whatever leaks (orphan storms, runaway builds, a
	// buggy interpreter under test), the agent dies at its budget
	// instead of taking down the machine.
	if guards.memLimitBytes > 0 {
		go func() {
			ticker := time.NewTicker(5 * time.Second)
			defer ticker.Stop()
			// Growth trail: log to the issue log each time the tree's
			// RSS doubles past 256MB. If a runaway dodges the limit
			// (or the machine dies before a kill lands), the log
			// still shows when the ballooning started and how fast
			// it grew.
			var lastLogged int64 = 256 << 20
			for {
				select {
				case <-watchdogStop:
					return
				case <-ticker.C:
					if cmd.Process == nil {
						continue
					}
					rss := weaveProcTreeRSSBytes(cmd.Process.Pid)
					if rss > guards.memLimitBytes {
						killTree(fmt.Sprintf("process-tree RSS %dMB exceeds --mem-limit %dMB",
							rss>>20, guards.memLimitBytes>>20), 10*time.Second)
						return
					}
					if rss >= lastLogged*2 {
						lastLogged = rss
						slog.Warn("weave: subagent tree RSS growing", "pid", cmd.Process.Pid, "rss_mb", rss>>20)
						if logSink != nil {
							tree, system := weaveForensicSnapshot(cmd.Process.Pid)
							fmt.Fprintf(logSink, "\r\n[weave] tree RSS %dMB (limit %dMB) — top tree: %s | top system: %s\r\n",
								rss>>20, guards.memLimitBytes>>20, tree, system)
						}
					}
				}
			}
		}()
	}

	// Control socket for `weave say`: every line received becomes
	// keystrokes on the PTY master (trailing \r = Enter, which the
	// TUI's line discipline reads as submit). Serving it in both
	// the captured and pass-through paths costs nothing; writes to
	// *os.File are serialized by the kernel for these small sizes.
	if guards.ctlSock != "" {
		_ = os.Remove(guards.ctlSock)
		if ln, lnErr := net.Listen("unix", guards.ctlSock); lnErr == nil {
			defer func() {
				_ = ln.Close()
				_ = os.Remove(guards.ctlSock)
			}()
			go func() {
				for {
					conn, acceptErr := ln.Accept()
					if acceptErr != nil {
						return // listener closed at tool exit
					}
					go func(c net.Conn) {
						defer c.Close()
						sc := bufio.NewScanner(c)
						for sc.Scan() {
							weaveWritePTYControlLine(ptmx, sc.Text())
						}
					}(conn)
				}
			}()
		} else {
			if f, err := os.OpenFile(guards.ctlSock, os.O_CREATE|os.O_RDONLY, 0o600); err == nil {
				_ = f.Close()
				defer func() { _ = os.Remove(guards.ctlSock) }()
				go weaveTailPTYControlFile(guards.ctlSock, ptmx)
				slog.Warn("weave: control socket unavailable; using file control fallback", "path", guards.ctlSock, "err", lnErr)
			} else {
				slog.Warn("weave: control socket unavailable; `weave say` disabled for this run", "path", guards.ctlSock, "err", lnErr)
			}
		}
	}

	parentTTY := term.IsTerminal(int(os.Stdin.Fd())) && term.IsTerminal(int(os.Stdout.Fd()))

	// Forward SIGWINCH so terminal resizes propagate into the subagent's
	// PTY. Even in the non-TTY path we install the handler — it costs
	// nothing and means a manual SIGWINCH (rare) still works.
	winch := make(chan os.Signal, 1)
	signal.Notify(winch, syscall.SIGWINCH)
	winchDone := make(chan struct{})
	go func() {
		defer close(winchDone)
		for range winch {
			if parentTTY {
				_ = pty.InheritSize(os.Stdout, ptmx)
			}
		}
	}()
	defer func() {
		signal.Stop(winch)
		close(winch)
		<-winchDone
	}()

	var (
		oldState    *term.State
		restoreOnce sync.Once
	)
	restore := func() {
		if oldState != nil {
			restoreOnce.Do(func() { _ = term.Restore(int(os.Stdout.Fd()), oldState) })
		}
	}
	defer restore()

	// activityTap wraps an io.Writer and bumps lastWriteUnixNs on
	// every successful write. The idle-timeout watchdog reads that
	// timestamp.
	bump := func(n int) {
		if n > 0 {
			lastWriteUnixNs.Store(time.Now().UnixNano())
		}
	}
	tap := func(w io.Writer) io.Writer { return &activityTap{w: w, bump: bump} }

	if parentTTY {
		// Raw mode so the user's keystrokes go straight to the
		// subagent's TTY. Goroutine for stdin→PTY (os.Stdin reads
		// block); PTY→stdout in the foreground (blocks until child
		// closes the slave).
		oldState, err = term.MakeRaw(int(os.Stdout.Fd()))
		if err != nil {
			fmt.Fprintf(os.Stderr, "weave: term.MakeRaw: %v\n", err)
		}
		go func() { _, _ = io.Copy(ptmx, os.Stdin) }()
		_, _ = io.Copy(tap(os.Stdout), ptmx)
	} else {
		// Non-TTY parent (orchestrator pipe / backgrounded by `cmd &`).
		// Subagent gets a PTY but stdin is closed; PTY output is
		// captured to logSink (typically a per-issue log file under
		// the queue dir). We deliberately do NOT copy to os.Stdout —
		// that would feed the subagent's TUI output back into the
		// orchestrator's pipe, the exact pattern that caused the
		// recent OOM incident.
		if logSink == nil {
			logSink = io.Discard
		}
		_, _ = io.Copy(tap(logSink), ptmx)
	}

	waitErr := cmd.Wait()
	restore()

	switch e := waitErr.(type) {
	case nil:
		return 0, nil
	case *exec.ExitError:
		if status, ok := e.Sys().(syscall.WaitStatus); ok {
			if status.Signaled() {
				return 128 + int(status.Signal()), nil
			}
			return status.ExitStatus(), nil
		}
		return 1, nil
	default:
		if errors.Is(waitErr, io.EOF) {
			return 0, nil
		}
		return 1, waitErr
	}
}

func weaveWritePTYControlLine(ptmx *os.File, line string) {
	if line == "" {
		return
	}
	// Verbatim frame: \x00R<base64> — decoded bytes written
	// to PTY exactly as-is (no trailing \r).
	if strings.HasPrefix(line, "\x00R") {
		if decoded, err := base64.StdEncoding.DecodeString(line[2:]); err == nil {
			_, _ = ptmx.Write(decoded)
		}
		return
	}
	// Plain line protocol: append \r for Enter.
	_, _ = ptmx.WriteString(line + "\r")
}

func weaveTailPTYControlFile(path string, ptmx *os.File) {
	f, err := os.Open(path)
	if err != nil {
		return
	}
	defer f.Close()

	r := bufio.NewReader(f)
	for {
		line, err := r.ReadString('\n')
		if line != "" {
			line = strings.TrimSuffix(line, "\n")
			line = strings.TrimSuffix(line, "\r")
			weaveWritePTYControlLine(ptmx, line)
		}
		if err == nil {
			continue
		}
		if errors.Is(err, io.EOF) {
			time.Sleep(50 * time.Millisecond)
			continue
		}
		return
	}
}

// activityTap wraps an io.Writer and calls bump(n) on each write,
// so the watchdog goroutine can detect a stalled subagent. The
// goroutine uses sync/atomic on the timestamp; the writer itself
// stays lock-free.
type activityTap struct {
	w    io.Writer
	bump func(int)
}

func (a *activityTap) Write(p []byte) (int, error) {
	n, err := a.w.Write(p)
	a.bump(n)
	return n, err
}

// weavePTYSize returns the controlling terminal's size, or 24x80 as
// a fallback so backgrounded subagents still get a sensible default.
func weavePTYSize() (uint16, uint16) {
	if w, h, err := term.GetSize(int(os.Stdout.Fd())); err == nil {
		return uint16(h), uint16(w)
	}
	return 24, 80
}

// weaveStdinIsTTY reports whether the calling process's stdin is a
// real terminal. Used to gate the auto-setsid + auto-log-file paths.
func weaveStdinIsTTY() bool {
	return term.IsTerminal(int(os.Stdin.Fd()))
}

// weaveProcSnapshot returns the child-map, RSS (bytes), and command
// name per PID from one `ps` pass. Shelling out to ps is deliberate:
// it's portable across macOS and Linux (no /proc on darwin), and the
// watchdogs poll at multi-second intervals where a fork is noise.
func weaveProcSnapshot() (children map[int][]int, rss map[int]int64, comm map[int]string) {
	out, err := exec.Command("ps", "-axo", "pid=,ppid=,rss=,comm=").Output()
	if err != nil {
		return nil, nil, nil
	}
	children = make(map[int][]int)
	rss = make(map[int]int64)
	comm = make(map[int]string)
	for _, line := range strings.Split(string(out), "\n") {
		f := strings.Fields(line)
		if len(f) < 4 {
			continue
		}
		pid, err1 := strconv.Atoi(f[0])
		ppid, err2 := strconv.Atoi(f[1])
		kb, err3 := strconv.ParseInt(f[2], 10, 64)
		if err1 != nil || err2 != nil || err3 != nil {
			continue
		}
		children[ppid] = append(children[ppid], pid)
		rss[pid] = kb << 10
		comm[pid] = strings.Join(f[3:], " ")
	}
	return children, rss, comm
}

// weaveTopProcs formats the n highest-RSS processes among pids as
// one log-friendly line: "pid:comm=rssMB pid:comm=rssMB ...".
func weaveTopProcs(pids []int, rss map[int]int64, comm map[int]string, n int) string {
	sorted := append([]int(nil), pids...)
	sort.Slice(sorted, func(i, j int) bool { return rss[sorted[i]] > rss[sorted[j]] })
	if len(sorted) > n {
		sorted = sorted[:n]
	}
	parts := make([]string, 0, len(sorted))
	for _, p := range sorted {
		name := comm[p]
		if i := strings.LastIndexByte(name, '/'); i >= 0 {
			name = name[i+1:]
		}
		parts = append(parts, fmt.Sprintf("%d:%s=%dMB", p, name, rss[p]>>20))
	}
	return strings.Join(parts, " ")
}

// weaveForensicSnapshot returns two lines for the issue log: the
// top-RSS processes inside the subagent tree, and system-wide. The
// system-wide line is the one that catches a culprit OUTSIDE the
// tree (the 2026-06 OOMs were attributed to a VSCode process while
// per-process stats in Activity Monitor were already unreadable —
// this snapshot is taken while `ps` still works).
func weaveForensicSnapshot(root int) (tree string, system string) {
	children, rss, comm := weaveProcSnapshot()
	if children == nil {
		return "", ""
	}
	all := make([]int, 0, len(rss))
	for p := range rss {
		all = append(all, p)
	}
	return weaveTopProcs(weaveDescend(root, children), rss, comm, 5),
		weaveTopProcs(all, rss, comm, 5)
}

// weaveDescend walks the snapshot from root, breadth-first, and
// returns root plus every transitive descendant.
func weaveDescend(root int, children map[int][]int) []int {
	pids := []int{root}
	seen := map[int]bool{root: true}
	for i := 0; i < len(pids); i++ {
		for _, c := range children[pids[i]] {
			if !seen[c] {
				seen[c] = true
				pids = append(pids, c)
			}
		}
	}
	return pids
}

// weaveProcTreePids returns root + all transitive children. Best
// effort: processes that double-fork and reparent to init escape
// the tree (they also escape the process group; nothing short of
// cgroups catches those, and macOS has none).
func weaveProcTreePids(root int) []int {
	children, _, _ := weaveProcSnapshot()
	if children == nil {
		return []int{root}
	}
	return weaveDescend(root, children)
}

// weaveProcTreeRSSBytes sums resident memory across the subagent's
// process tree.
func weaveProcTreeRSSBytes(root int) int64 {
	children, rss, _ := weaveProcSnapshot()
	if children == nil {
		return 0
	}
	var total int64
	for _, p := range weaveDescend(root, children) {
		total += rss[p]
	}
	return total
}
