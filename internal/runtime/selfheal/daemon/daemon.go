// Package daemon scans the synthesized selfheal backlog entries and
// dispatches a Worker per qualifying signature, applying the hard
// guardrails the plan calls for: max concurrent workers, per-
// signature 24h cooldown, and the STOP-file kill switch.
//
// Runs as a goroutine off `ycode serve`. Polling interval is short
// (default 5s) so newly-synthesized backlog entries pick up quickly
// without needing an event channel between the observer and the
// daemon.
package daemon

import (
	"context"
	"encoding/json"
	"log/slog"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"sync"
	"time"

	"github.com/qiangli/ycode/internal/gitserver/backlog"
	"github.com/qiangli/ycode/internal/runtime/selfheal/worker"
	"github.com/qiangli/ycode/internal/runtime/selfheal/workspace"
)

// Config bounds the daemon. Empty values pick safe defaults.
type Config struct {
	BaseDir        string        // ~/.agents/ycode/selfheal — per-signature workspaces live here
	BacklogDir     string        // ~/.agents/ycode/projects/<id>/backlog — scanned for selfheal-*.md
	RepoURL        string        // resolved via workspace.DiscoverFork; injected here so the daemon doesn't reach back into git config
	MaxConcurrent  int           // cap on in-flight workers; default 5
	PollInterval   time.Duration // default 5s
	CooldownPerSig time.Duration // per-signature retry interval after a give-up/rejected outcome; default 24h
	WorkerConfig   worker.Config // template; the daemon fills in Signature-dependent fields per dispatch
}

func (c *Config) applyDefaults() {
	if c.MaxConcurrent == 0 {
		c.MaxConcurrent = 5
	}
	if c.PollInterval == 0 {
		c.PollInterval = 5 * time.Second
	}
	if c.CooldownPerSig == 0 {
		c.CooldownPerSig = 24 * time.Hour
	}
}

// stopFile is the kill-switch sentinel. Touching this file pauses
// dispatch until it's removed; `ycode selfheal off` (Phase 5 CLI)
// is the operator-facing front-end.
const stopFileName = "STOP"

// Daemon is the dispatch coordinator. One per `ycode serve` process.
type Daemon struct {
	cfg    Config
	logger *slog.Logger
	sem    chan struct{}  // worker concurrency semaphore
	wg     sync.WaitGroup // tracks in-flight workers for clean shutdown
	stopCh chan struct{}
}

// New constructs the daemon. Call Start to begin polling.
func New(cfg Config) *Daemon {
	cfg.applyDefaults()
	return &Daemon{
		cfg:    cfg,
		logger: slog.Default(),
		sem:    make(chan struct{}, cfg.MaxConcurrent),
		stopCh: make(chan struct{}),
	}
}

// Start spawns the polling goroutine.
func (d *Daemon) Start(ctx context.Context) {
	go d.runLoop(ctx)
}

// Stop signals the polling loop to exit and waits for in-flight
// workers to finish. Safe to call multiple times.
func (d *Daemon) Stop(ctx context.Context) {
	select {
	case <-d.stopCh:
	default:
		close(d.stopCh)
	}
	done := make(chan struct{})
	go func() { d.wg.Wait(); close(done) }()
	select {
	case <-done:
	case <-ctx.Done():
		// Best-effort: in-flight workers exit when their own timeouts
		// fire. They're guarded by per-step timeouts so this isn't
		// a leak.
	}
}

func (d *Daemon) runLoop(ctx context.Context) {
	t := time.NewTicker(d.cfg.PollInterval)
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-d.stopCh:
			return
		case <-t.C:
			if d.killSwitchActive() {
				continue
			}
			d.scanAndDispatch(ctx)
		}
	}
}

// killSwitchActive reports whether the STOP file is present. Cheap
// stat per tick — much faster than constructing a worker only to bail.
func (d *Daemon) killSwitchActive() bool {
	if _, err := os.Stat(filepath.Join(d.cfg.BaseDir, stopFileName)); err == nil {
		return true
	}
	return false
}

// scanAndDispatch finds open selfheal backlog entries that aren't in
// cooldown and dispatches a worker per signature, up to the
// MaxConcurrent ceiling.
func (d *Daemon) scanAndDispatch(ctx context.Context) {
	items, err := backlog.Load(d.cfg.BacklogDir)
	if err != nil {
		d.logger.Debug("selfheal-daemon: backlog load partial", "err", err)
		// Continue with whatever loaded — partial is better than nothing.
	}
	for _, it := range items {
		if it.State != backlog.StateOpen {
			continue
		}
		sig := signatureFromSlug(it.Slug)
		if sig == "" {
			continue
		}
		if d.inCooldown(sig) {
			continue
		}
		select {
		case d.sem <- struct{}{}:
			d.wg.Add(1)
			go d.runWorker(ctx, sig)
		default:
			// Pool full; will retry next poll.
			return
		}
	}
}

// inCooldown returns true if the per-signature outcome.json shows a
// previous attempt within CooldownPerSig. Read-only stat — keeps the
// hot path cheap.
func (d *Daemon) inCooldown(signature string) bool {
	layout := workspace.PathsFor(d.cfg.BaseDir, signature)
	info, err := os.Stat(layout.Outcome)
	if err != nil {
		return false
	}
	return time.Since(info.ModTime()) < d.cfg.CooldownPerSig
}

// runWorker constructs a Worker, runs it, and releases the
// concurrency slot. Logged with structured context so operators can
// pivot to traces.
func (d *Daemon) runWorker(ctx context.Context, signature string) {
	defer func() {
		<-d.sem
		d.wg.Done()
	}()
	d.logger.Info("selfheal-daemon: worker start", "signature", signature)
	cfg := d.cfg.WorkerConfig
	cfg.BaseDir = d.cfg.BaseDir
	cfg.BacklogDir = d.cfg.BacklogDir
	cfg.RepoURL = d.cfg.RepoURL
	w, err := worker.New(cfg, signature)
	if err != nil {
		d.logger.Warn("selfheal-daemon: worker init", "signature", signature, "err", err)
		return
	}
	out, err := w.Run(ctx)
	if err != nil {
		d.logger.Warn("selfheal-daemon: worker error", "signature", signature, "err", err, "mode", out.Mode)
		return
	}
	d.logger.Info("selfheal-daemon: worker done",
		"signature", signature,
		"mode", out.Mode,
		"iterations", out.Iterations,
		"diff_lines", out.DiffLines,
		"worktree", out.WorktreePath)
	if out.Mode == "success" {
		// Mark the backlog entry as in_progress so the Foreman view
		// reflects worker hand-off. Phase 5 will flip to done after
		// PR creation (or local-only-recorded). Today: in_progress
		// is the most honest state — code is fixed, not yet shared.
		if err := backlog.MarkState(d.cfg.BacklogDir, slugFromSignature(d.cfg.BacklogDir, signature), backlog.StateInProgress); err != nil {
			d.logger.Warn("selfheal-daemon: mark state", "signature", signature, "err", err)
		}
		// Persist a side-by-side outcome.json copy for operators
		// chasing the result via filesystem.
		_ = persistDaemonOutcome(d.cfg.BaseDir, signature, out)
	}
}

// signatureFromSlug parses selfheal-<sig>-<tool>.md to recover <sig>.
// Returns empty for slugs that don't match the pattern (e.g. human-
// authored entries).
func signatureFromSlug(slug string) string {
	const prefix = "selfheal-"
	if !strings.HasPrefix(slug, prefix) {
		return ""
	}
	rest := slug[len(prefix):]
	// signature is the first dash-delimited segment after the prefix.
	if idx := strings.Index(rest, "-"); idx > 0 {
		return rest[:idx]
	}
	return rest
}

// slugFromSignature finds the on-disk slug for a signature by
// globbing the backlog dir. Cheap — backlog dirs hold dozens of
// entries, not thousands.
func slugFromSignature(backlogDir, signature string) string {
	matches, _ := filepath.Glob(filepath.Join(backlogDir, "selfheal-"+signature+"-*.md"))
	if len(matches) == 0 {
		return ""
	}
	return strings.TrimSuffix(filepath.Base(matches[0]), ".md")
}

// persistDaemonOutcome mirrors the worker's outcome.json to a
// daemon-side location. Phase 5 will read these to drive the
// `ycode selfheal list` UX.
func persistDaemonOutcome(baseDir, signature string, out worker.Outcome) error {
	layout := workspace.PathsFor(baseDir, signature)
	b, err := json.MarshalIndent(out, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(layout.Outcome, b, 0o600)
}

// reservedRx documents what the daemon expects in valid signatures —
// hex prefix as written by detector.makeSignature. Currently
// informational; if Phase 6's skill engine starts feeding signatures
// from telemetry triggers, this becomes a real validator.
var reservedRx = regexp.MustCompile(`^[0-9a-f]{12}$`)

// IsValidSignature exposes the format predicate for tests and CLI.
func IsValidSignature(s string) bool { return reservedRx.MatchString(s) }
