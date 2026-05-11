package main

import (
	"context"
	"fmt"
	"log/slog"
	"net/http"
	"path/filepath"
	"sync"
	"sync/atomic"
	"time"

	"github.com/qiangli/ycode/internal/gitserver"
	gitserverloom "github.com/qiangli/ycode/internal/gitserver/loom"
	"github.com/qiangli/ycode/internal/gitserver/merger"
	"github.com/qiangli/ycode/internal/gitserver/projects"
	"github.com/qiangli/ycode/internal/observability"
	"github.com/qiangli/ycode/internal/runtime/mcp"
	"github.com/qiangli/ycode/internal/selfinit"
	loompkg "github.com/qiangli/ycode/pkg/loom"
)

// loomComponent mounts the loom MCP server at /loom-mcp/ on the
// observability proxy. It also owns one merger goroutine per active
// project — started lazily via the Backend's OnProjectActive callback,
// stopped wholesale when the component shuts down.
type loomComponent struct {
	httpHandler http.Handler
	mcpHandler  mcp.ServerHandler // underlying MCP handler; exposed so the composite /mcp/ endpoint can fan out to loom without a second wrap
	healthy     atomic.Bool

	svc *loompkg.Service

	mergerCancel context.CancelFunc
	mergerWG     sync.WaitGroup

	// Set on the first NotifyProjectActive for each slug; used to dedupe.
	startedMergers sync.Map // slug -> struct{}
}

// buildLoomComponent constructs the loom service + MCP HTTP wrapper +
// the per-project merger spawner. Returns the component to mount on the
// proxy and the underlying *loompkg.Service so callers (manifest writer,
// graceful-shutdown) can inspect or close it.
func buildLoomComponent(_ context.Context, client *gitserver.Client, token, giteaDataDir string) (*loomComponent, *loompkg.Service, error) {
	registry, err := projects.NewRegistry(giteaDataDir)
	if err != nil {
		return nil, nil, fmt.Errorf("loom: registry: %w", err)
	}

	loomDataDir := filepath.Join(giteaDataDir, "loom")
	sandboxRoot := filepath.Join(loomDataDir, "sandboxes")
	leasePath := filepath.Join(loomDataDir, "leases.json")

	store, err := loompkg.NewFileStore(leasePath)
	if err != nil {
		return nil, nil, fmt.Errorf("loom: lease store: %w", err)
	}

	mergerCtx, mergerCancel := context.WithCancel(context.Background())

	c := &loomComponent{
		mergerCancel: mergerCancel,
	}

	backend, err := gitserverloom.NewGiteaBackend(gitserverloom.GiteaBackendOptions{
		Client:          client,
		Registry:        registry,
		Token:           token,
		Logger:          slog.Default(),
		OnProjectActive: c.makeOnProjectActive(mergerCtx, client, token, registry, giteaDataDir),
	})
	if err != nil {
		mergerCancel()
		return nil, nil, fmt.Errorf("loom: backend: %w", err)
	}
	svc, err := loompkg.NewService(loompkg.Options{
		Backend:     backend,
		Store:       store,
		SandboxRoot: sandboxRoot,
		Logger:      slog.Default(),
		// When a foreign tool calls loom_lease for a project ycode
		// hasn't touched yet, self-establish in that repo too. This
		// is the "first-class citizen regardless of entry point" rule
		// — a Claude Code session driving loom from a fresh repo gets
		// the same project-scope footprint as if the user had run
		// `ycode` directly there.
		OnLeaseCwd: onLeaseCwd,
	})
	if err != nil {
		mergerCancel()
		return nil, nil, fmt.Errorf("loom: service: %w", err)
	}
	// Run an immediate reaper pass at startup so leases that outlived a
	// previous `ycode serve` are reaped before they accept new traffic.
	go svc.ReapNow(mergerCtx)

	handler := gitserverloom.NewMCPHandler(svc)
	c.svc = svc
	c.mcpHandler = handler
	c.httpHandler = observability.NewMCPHTTPHandler(handler)
	return c, svc, nil
}

// MCPHandler returns the underlying loom MCP ServerHandler. Used by the
// composite /mcp/ endpoint to fan out without re-wrapping the same logic.
func (c *loomComponent) MCPHandler() mcp.ServerHandler { return c.mcpHandler }

func (c *loomComponent) Name() string { return "loom-mcp" }

func (c *loomComponent) Start(_ context.Context) error {
	c.healthy.Store(true)
	return nil
}

func (c *loomComponent) Stop(_ context.Context) error {
	c.healthy.Store(false)
	if c.mergerCancel != nil {
		c.mergerCancel()
	}
	c.mergerWG.Wait()
	if c.svc != nil {
		_ = c.svc.Close()
	}
	return nil
}

func (c *loomComponent) Healthy() bool             { return c.healthy.Load() }
func (c *loomComponent) HTTPHandler() http.Handler { return c.httpHandler }

// makeOnProjectActive returns the OnProjectActive callback that lazy-
// starts a merger goroutine for each project the first time loom sees
// it. Idempotent per slug.
func (c *loomComponent) makeOnProjectActive(ctx context.Context, client *gitserver.Client, token string, registry *projects.Registry, giteaDataDir string) gitserverloom.ProjectActiveFn {
	return func(_ context.Context, slug, cloneURL string) error {
		if _, loaded := c.startedMergers.LoadOrStore(slug, struct{}{}); loaded {
			return nil
		}
		project := findProjectBySlug(registry, slug)
		if project == nil {
			slog.Warn("loom: merger skipped, project not in registry", "slug", slug)
			return nil
		}
		syncLog, err := projects.NewSyncLog(giteaDataDir, project)
		if err != nil {
			return fmt.Errorf("loom: merger sync log: %w", err)
		}
		m, err := merger.New(merger.Config{
			Client:    client,
			Project:   project,
			SyncLog:   syncLog,
			CloneURL:  cloneURL,
			Token:     token,
			CICommand: "", // unconditional auto-merge for loom v1; per-project CI lands later.
			WorkDir:   filepath.Join(giteaDataDir, "loom", "merger-work-"+slug),
			Logger:    slog.Default(),
		})
		if err != nil {
			return fmt.Errorf("loom: merger.New: %w", err)
		}
		c.mergerWG.Add(1)
		go func() {
			defer c.mergerWG.Done()
			_ = m.Run(ctx, 10*time.Second)
		}()
		slog.Info("loom: merger started", "slug", slug)
		return nil
	}
}

// findProjectBySlug walks the registry's snapshot list looking for a
// matching slug. Linear, but the list is small (one entry per host
// project ever leased) and projects.Registry doesn't expose a slug
// index publicly.
func findProjectBySlug(r *projects.Registry, slug string) *projects.Project {
	for _, p := range r.List() {
		if p.Slug == slug {
			return p
		}
	}
	return nil
}

// onLeaseCwd is the callback the loom service invokes on the first
// successful Lease for a given cwd. We run SelfInit synchronously so
// the foreign tool's first Lease always happens against an already-
// established repo. Errors are logged and never propagated.
func onLeaseCwd(ctx context.Context, cwd string) {
	res, err := selfinit.Run(ctx, selfinit.Options{
		Cwd:          cwd,
		YcodeVersion: version,
		Logger:       slog.Default(),
	})
	if err != nil {
		slog.Warn("loom: selfinit on lease", "cwd", cwd, "err", err)
		return
	}
	if res.Skipped || res.OptedOut {
		return
	}
	if len(res.ProjectFiles) > 0 {
		slog.Info("loom: ycode self-installed in foreign-tool repo",
			"cwd", cwd, "files", res.ProjectFiles)
	}
}
