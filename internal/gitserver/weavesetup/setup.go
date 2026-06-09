// Package weavesetup orchestrates the idempotent first-run steps that
// bring a host project to v2-ready state: mirror into the embedded
// Gitea, create the loom:* label set, install the pre-commit hook,
// drop .ycode/loom.yaml with auto-detected defaults.
//
// Each step records its completion in .ycode/loom.yaml so a retry
// skips done work — the file IS the state machine.
//
// The Loom project board (kanban) is deliberately NOT created here;
// it's opt-in via `ycode weave init-board` per the spike-1 design
// fallback. The default dashboard is the label-filtered issue list,
// which uses only the stable v1 REST API.
package weavesetup

import (
	"context"
	"fmt"
	"os"
	"path/filepath"

	"github.com/qiangli/ycode/internal/gitserver/weaveapi"
)

// MirrorBackend is the subset of pkg/loom.Backend the setup needs for
// the mirror step. Decouples weavesetup from importing pkg/loom; the
// concrete Backend satisfies it.
type MirrorBackend interface {
	EnsureProject(ctx context.Context, cwd string) (slug, cloneURL string, err error)
}

// Options is the input to Run.
type Options struct {
	// HostCWD is the absolute path of the user's project checkout.
	// Required.
	HostCWD string

	// Backend mirrors HostCWD into the embedded Gitea. Required.
	Backend MirrorBackend

	// Weave is the Gitea-API helper used to create the loom:* label
	// set. Required.
	Weave *weaveapi.Client

	// DefaultTool is written to .ycode/loom.yaml. Empty falls back to
	// "codex" (the v2 design's hardcoded fallback). Future PRs auto-
	// detect from project files (package.json / go.mod / etc.).
	DefaultTool string
}

// Result describes what setup did, for logging and for surfacing to
// the user via the first-run banner.
type Result struct {
	Slug             string
	CloneURL         string
	LabelsCreated    int
	HookInstalled    bool
	ConfigWritten    bool
	AlreadySetUp     bool
}

// Run is the idempotent first-run entry point. Safe to call multiple
// times; finished steps are skipped via the .ycode/loom.yaml marker.
func Run(ctx context.Context, opts Options) (*Result, error) {
	if opts.HostCWD == "" {
		return nil, fmt.Errorf("weavesetup: HostCWD required")
	}
	if opts.Backend == nil {
		return nil, fmt.Errorf("weavesetup: Backend required")
	}
	if opts.Weave == nil {
		return nil, fmt.Errorf("weavesetup: Weave client required")
	}

	res := &Result{}

	// Check the existing marker first; a fully-setup project skips
	// every step.
	cfg, cfgExists, err := loadConfig(opts.HostCWD)
	if err != nil {
		return nil, fmt.Errorf("weavesetup: read marker: %w", err)
	}
	if cfgExists && cfg.SetupComplete {
		res.Slug = cfg.Slug
		res.AlreadySetUp = true
		return res, nil
	}

	// Step 1 — mirror.
	slug, cloneURL, err := opts.Backend.EnsureProject(ctx, opts.HostCWD)
	if err != nil {
		return nil, fmt.Errorf("weavesetup: ensure project: %w", err)
	}
	res.Slug = slug
	res.CloneURL = cloneURL

	// Step 2 — labels.
	if !cfg.LabelsCreated {
		// Owner is fixed to "admin" matching the loom v1 convention
		// (single admin user in embedded Gitea). The orchestrator
		// stays simple by hard-coding here; per-tier identity wiring
		// (Tier 2 / Tier 3) lands in a follow-up.
		created, err := opts.Weave.EnsureLabels(ctx, "admin", slug)
		if err != nil {
			return nil, fmt.Errorf("weavesetup: ensure labels: %w", err)
		}
		res.LabelsCreated = created
		cfg.LabelsCreated = true
	}

	// Step 3 — pre-commit hook (defense Layer 3).
	if !cfg.HookInstalled {
		if err := installPreCommitHook(opts.HostCWD); err != nil {
			return nil, fmt.Errorf("weavesetup: install hook: %w", err)
		}
		res.HookInstalled = true
		cfg.HookInstalled = true
	}

	// Step 4 — config write.
	cfg.Slug = slug
	cfg.DefaultBaseBranch = "main"
	cfg.IdentityTier = "ephemeral"
	cfg.BackendMode = detectBackendMode(opts.HostCWD)
	if cfg.DefaultTool == "" {
		if opts.DefaultTool != "" {
			cfg.DefaultTool = opts.DefaultTool
		} else {
			cfg.DefaultTool = "codex"
		}
	}
	cfg.SetupComplete = true
	if err := saveConfig(opts.HostCWD, cfg); err != nil {
		return nil, fmt.Errorf("weavesetup: save config: %w", err)
	}
	res.ConfigWritten = true

	return res, nil
}

// detectBackendMode picks `forge` when CI signals exist in the host
// project (Makefile with test target, .github/workflows/*) and
// `local` otherwise. Lightweight heuristic; users override in
// .ycode/loom.yaml.
func detectBackendMode(hostCWD string) string {
	if pathExists(filepath.Join(hostCWD, ".github", "workflows")) {
		return "forge"
	}
	if pathExists(filepath.Join(hostCWD, "Makefile")) {
		// Cheap probe: a Makefile alone isn't proof of CI, but it's a
		// strong-enough signal for the auto-detect default. Users who
		// don't want forge mode override in loom.yaml.
		return "forge"
	}
	return "local"
}

func pathExists(p string) bool {
	_, err := os.Stat(p)
	return err == nil
}
