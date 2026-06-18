//go:build !windows

package wrap

import (
	"fmt"
	"log/slog"
	"os"
	"os/exec"
	"runtime"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"time"
)

// reapLeakedDescendants enumerates all descendants of wrapperPid and
// SIGKILLs any that are still alive (excluding wrapperPid itself and
// PID <= 1).
//
// Best-effort: errors are logged at debug and never propagated. This
// catches processes that escaped the wrapper's process-group tree-kill
// (via setsid, pgid change, etc.) and would otherwise spin as orphans
// consuming CPU indefinitely.
func reapLeakedDescendants(wrapperPid int) {
	pids, err := enumerateDescendants(wrapperPid)
	if err != nil {
		slog.Debug("wrap: reaper: enumerate descendants failed", "pid", wrapperPid, "err", err)
		return
	}
	reapLeakedPIDs(wrapperPid, pids)
}

func reapLeakedPIDs(wrapperPid int, pids []int) {
	for _, pid := range pids {
		if pid == wrapperPid || pid <= 1 {
			continue
		}
		if !isProcessAlive(pid) {
			continue
		}
		p, err := os.FindProcess(pid)
		if err != nil {
			continue
		}
		if err := p.Signal(syscall.SIGKILL); err != nil {
			slog.Debug("wrap: reaper: SIGKILL failed", "pid", pid, "err", err)
			continue
		}
		slog.Debug("wrap: reaped leaked descendant", "pid", pid)
	}
}

type leakedDescendantTracker struct {
	rootPid int
	stop    chan struct{}
	done    chan struct{}

	mu   sync.Mutex
	seen map[int]struct{}
}

func startLeakedDescendantTracker(rootPid int) *leakedDescendantTracker {
	tracker := &leakedDescendantTracker{
		rootPid: rootPid,
		stop:    make(chan struct{}),
		done:    make(chan struct{}),
		seen:    make(map[int]struct{}),
	}
	tracker.record()
	go tracker.run()
	return tracker
}

func (t *leakedDescendantTracker) run() {
	defer close(t.done)
	ticker := time.NewTicker(100 * time.Millisecond)
	defer ticker.Stop()
	for {
		select {
		case <-ticker.C:
			t.record()
		case <-t.stop:
			t.record()
			return
		}
	}
}

func (t *leakedDescendantTracker) record() {
	pids, err := enumerateDescendants(t.rootPid)
	if err != nil {
		slog.Debug("wrap: reaper: enumerate descendants failed", "pid", t.rootPid, "err", err)
		return
	}
	t.mu.Lock()
	defer t.mu.Unlock()
	for _, pid := range pids {
		if pid != t.rootPid && pid > 1 {
			t.seen[pid] = struct{}{}
		}
	}
}

func (t *leakedDescendantTracker) stopAndSnapshot() []int {
	close(t.stop)
	<-t.done

	t.mu.Lock()
	defer t.mu.Unlock()
	pids := make([]int, 0, len(t.seen))
	for pid := range t.seen {
		pids = append(pids, pid)
	}
	return pids
}

func enumerateDescendants(rootPid int) ([]int, error) {
	switch runtime.GOOS {
	case "linux":
		return enumerateDescendantsLinux(rootPid)
	case "darwin":
		return enumerateDescendantsDarwin(rootPid)
	default:
		return nil, fmt.Errorf("wrap: reaper: unsupported platform: %s", runtime.GOOS)
	}
}

func enumerateDescendantsLinux(rootPid int) ([]int, error) {
	var result []int
	collectChildren(rootPid, &result)
	return result, nil
}

func collectChildren(pid int, result *[]int) {
	childrenPath := "/proc/" + strconv.Itoa(pid) + "/task/" + strconv.Itoa(pid) + "/children"
	data, err := os.ReadFile(childrenPath)
	if err != nil {
		return
	}
	for _, field := range strings.Fields(string(data)) {
		child, err := strconv.Atoi(field)
		if err != nil {
			continue
		}
		*result = append(*result, child)
		collectChildren(child, result)
	}
}

func enumerateDescendantsDarwin(rootPid int) ([]int, error) {
	out, err := exec.Command("ps", "-axo", "pid,ppid").Output()
	if err != nil {
		return nil, err
	}
	children := make(map[int][]int)
	lines := strings.Split(strings.TrimSpace(string(out)), "\n")
	for i, line := range lines {
		if i == 0 {
			continue
		}
		fields := strings.Fields(line)
		if len(fields) < 2 {
			continue
		}
		pid, err := strconv.Atoi(fields[0])
		if err != nil {
			continue
		}
		pp, err := strconv.Atoi(fields[1])
		if err != nil {
			continue
		}
		children[pp] = append(children[pp], pid)
	}
	var result []int
	walkDescendants(rootPid, children, &result)
	return result, nil
}

func walkDescendants(root int, children map[int][]int, result *[]int) {
	for _, c := range children[root] {
		*result = append(*result, c)
		walkDescendants(c, children, result)
	}
}
