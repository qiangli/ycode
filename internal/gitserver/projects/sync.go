package projects

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"time"
)

// SyncLog is an append-only log of merge SHAs that landed in the project's
// upstream/main but have not yet been pulled into the user's cwd.
//
// File format: one record per line —
//
//	<RFC3339 timestamp> <40-char SHA> <pr#> <agent-id>
//
// File location: <giteaDataDir>/pending-sync/<slug>.log
type SyncLog struct {
	path string
	mu   sync.Mutex
}

// SyncEntry is a single line in the pending-sync log.
type SyncEntry struct {
	Timestamp time.Time
	SHA       string
	PR        int64
	AgentID   string
}

// NewSyncLog opens (or prepares) the log for the given project.
// giteaDataDir is the same dir used by gitserver.Server (DataDir).
func NewSyncLog(giteaDataDir string, p *Project) (*SyncLog, error) {
	if giteaDataDir == "" {
		return nil, fmt.Errorf("synclog: empty giteaDataDir")
	}
	if p == nil || p.Slug == "" {
		return nil, fmt.Errorf("synclog: nil project or empty slug")
	}
	dir := filepath.Join(giteaDataDir, "pending-sync")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return nil, fmt.Errorf("synclog: mkdir: %w", err)
	}
	return &SyncLog{path: filepath.Join(dir, p.Slug+".log")}, nil
}

// Append records a merge. Concurrency-safe.
func (l *SyncLog) Append(e SyncEntry) error {
	if e.Timestamp.IsZero() {
		e.Timestamp = time.Now().UTC()
	}
	if len(e.SHA) != 40 {
		return fmt.Errorf("synclog: invalid SHA %q", e.SHA)
	}
	l.mu.Lock()
	defer l.mu.Unlock()

	f, err := os.OpenFile(l.path, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o644)
	if err != nil {
		return fmt.Errorf("synclog: open: %w", err)
	}
	defer f.Close()

	line := fmt.Sprintf("%s %s %d %s\n",
		e.Timestamp.Format(time.RFC3339),
		e.SHA, e.PR, e.AgentID)
	if _, err := f.WriteString(line); err != nil {
		return fmt.Errorf("synclog: write: %w", err)
	}
	return nil
}

// Pending reads all entries from the log (oldest first).
func (l *SyncLog) Pending() ([]SyncEntry, error) {
	l.mu.Lock()
	defer l.mu.Unlock()

	f, err := os.Open(l.path)
	if os.IsNotExist(err) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("synclog: open: %w", err)
	}
	defer f.Close()

	var out []SyncEntry
	sc := bufio.NewScanner(f)
	for sc.Scan() {
		line := strings.TrimSpace(sc.Text())
		if line == "" {
			continue
		}
		parts := strings.Fields(line)
		if len(parts) < 3 {
			continue
		}
		ts, err := time.Parse(time.RFC3339, parts[0])
		if err != nil {
			continue
		}
		var pr int64
		if len(parts) >= 3 {
			fmt.Sscanf(parts[2], "%d", &pr)
		}
		agent := ""
		if len(parts) >= 4 {
			agent = parts[3]
		}
		out = append(out, SyncEntry{
			Timestamp: ts,
			SHA:       parts[1],
			PR:        pr,
			AgentID:   agent,
		})
	}
	if err := sc.Err(); err != nil {
		return nil, fmt.Errorf("synclog: scan: %w", err)
	}
	return out, nil
}

// Truncate clears the log, typically after a successful `tasks pull`.
func (l *SyncLog) Truncate() error {
	l.mu.Lock()
	defer l.mu.Unlock()
	return os.Truncate(l.path, 0)
}

// ErrPullDirty is returned by PullFastForward when cwd has uncommitted changes.
var ErrPullDirty = errors.New("projects: cwd has uncommitted changes; commit or stash first (no auto-stash by design)")

// ErrPullNotFastForward is returned when cwd's main has diverged from upstream.
var ErrPullNotFastForward = errors.New("projects: not a fast-forward; resolve manually")

// PullFastForward fast-forwards cwd from admin/<slug>:main on internal
// Gitea. Refuses on dirty cwd or non-fast-forward — the user resolves
// manually. This is the only sanctioned channel for syncing merged
// agent work back into the user's working tree.
//
// Idempotent: configures the "ycode-internal" remote on demand, with
// a token-authed URL.
func PullFastForward(ctx context.Context, cwd, cloneURL, token string) error {
	if cloneURL == "" {
		return fmt.Errorf("projects: empty cloneURL")
	}
	if token == "" {
		return fmt.Errorf("projects: empty token")
	}

	clean, err := IsClean(cwd)
	if err != nil {
		return fmt.Errorf("projects: check cwd: %w", err)
	}
	if !clean {
		return ErrPullDirty
	}

	authURL := injectPullToken(cloneURL, token)
	const remote = "ycode-internal"

	if _, err := runGit(ctx, cwd, "remote", "get-url", remote); err != nil {
		if _, err := runGit(ctx, cwd, "remote", "add", remote, authURL); err != nil {
			return fmt.Errorf("projects: add remote: %w", err)
		}
	} else {
		_, _ = runGit(ctx, cwd, "remote", "set-url", remote, authURL)
	}
	if _, err := runGit(ctx, cwd, "fetch", remote, "main"); err != nil {
		return fmt.Errorf("projects: fetch: %w", err)
	}
	if _, err := runGit(ctx, cwd, "merge", "--ff-only", remote+"/main"); err != nil {
		return fmt.Errorf("%w: %v", ErrPullNotFastForward, err)
	}
	return nil
}

func injectPullToken(rawURL, token string) string {
	if !strings.HasPrefix(rawURL, "http://") && !strings.HasPrefix(rawURL, "https://") {
		return rawURL
	}
	scheme := "http://"
	rest := strings.TrimPrefix(rawURL, scheme)
	if rest == rawURL {
		scheme = "https://"
		rest = strings.TrimPrefix(rawURL, scheme)
	}
	return fmt.Sprintf("%stoken:%s@%s", scheme, token, rest)
}

func runGit(ctx context.Context, dir string, args ...string) (string, error) {
	cmd := exec.CommandContext(ctx, "git", args...)
	cmd.Dir = dir
	out, err := cmd.CombinedOutput()
	if err != nil {
		return string(out), fmt.Errorf("git %s: %w\n%s",
			strings.Join(args, " "), err, string(out))
	}
	return string(out), nil
}
