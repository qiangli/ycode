package main

import (
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/spf13/cobra"

	"github.com/qiangli/coreutils/pkg/telemetry"

	"github.com/qiangli/ycode/internal/api"
	"github.com/qiangli/ycode/internal/buildinfo"
	"github.com/qiangli/ycode/internal/cli"
	"github.com/qiangli/ycode/internal/commands"
	"github.com/qiangli/ycode/internal/runtime/bash"
	"github.com/qiangli/ycode/internal/runtime/computer"
	"github.com/qiangli/ycode/internal/runtime/config"
	"github.com/qiangli/ycode/internal/runtime/conversation"
	"github.com/qiangli/ycode/internal/runtime/datadir"
	"github.com/qiangli/ycode/internal/runtime/embedding"
	"github.com/qiangli/ycode/internal/runtime/git"
	"github.com/qiangli/ycode/internal/runtime/health"
	"github.com/qiangli/ycode/internal/runtime/indexer"
	"github.com/qiangli/ycode/internal/runtime/lsp"
	"github.com/qiangli/ycode/internal/runtime/oauth"
	"github.com/qiangli/ycode/internal/runtime/origin"
	"github.com/qiangli/ycode/internal/runtime/permission"
	"github.com/qiangli/ycode/internal/runtime/prompt"
	"github.com/qiangli/ycode/internal/runtime/repomap"
	"github.com/qiangli/ycode/internal/runtime/routing"
	"github.com/qiangli/ycode/internal/runtime/session"
	"github.com/qiangli/ycode/internal/runtime/unattended"
	"github.com/qiangli/ycode/internal/runtime/vfs"
	"github.com/qiangli/ycode/internal/runtime/wrap"
	"github.com/qiangli/ycode/internal/selfheal"
	"github.com/qiangli/ycode/internal/tools"
	memexgraph "github.com/qiangli/ycode/pkg/memex/graph"
	"github.com/qiangli/ycode/pkg/memex/memory"
	"github.com/qiangli/ycode/pkg/memex/store"
	"github.com/qiangli/ycode/pkg/memex/store/kv"
	"github.com/qiangli/ycode/pkg/memex/store/search"
	sqlstore "github.com/qiangli/ycode/pkg/memex/store/sqlite"
	"github.com/qiangli/ycode/pkg/memex/store/vector"
)

// Set via -ldflags at build time.
var (
	version = "dev"
	commit  = "unknown"
)

// selfHealEnabled controls whether self-healing is active.
// Can be disabled via YCODE_SELF_HEAL=0 environment variable.
func selfHealEnabled() bool {
	return os.Getenv("YCODE_SELF_HEAL") != "0"
}

// leanStartup reports whether ycode should skip eager, non-essential startup work
// (repo map, code index, embedder) to reach an input-ready state fast. True for a
// one-shot `--print` run, and — critically — when `--events` is set, which marks a
// PROGRAMMATIC DRIVER (a coach / foreman / steerable session) attached: that driver
// races the TUI to deliver the opening prompt over the control socket, and a slow,
// chatty startup makes it lose — the keystrokes are swallowed while LSP/memory/OTel
// churn, the session sits at an empty prompt, and it reads as a model that did
// nothing (a false verdict about the model for a harness-latency bug). Fast startup
// is correctness in controlled mode, not merely speed.
func leanStartup() bool { return printFlag || eventsFile != "" }

func main() {
	buildinfo.Set(version, commit)
	// Internal killable boundary for startup repo-map parsing. This is
	// intercepted before Cobra so it is not a public verb or capability.
	if len(os.Args) == 3 && os.Args[1] == "__startup-repomap" {
		text, err := generateStartupRepoMap(os.Args[2])
		if err != nil {
			fmt.Fprintln(os.Stderr, err)
			os.Exit(1)
		}
		fmt.Fprint(os.Stdout, text)
		return
	}
	// Intercept `ycode wrap` shim invocations. When the ycode binary is
	// exec'd via a symlink named `bash`/`rg`/`git`/... that lives in a
	// wrap-managed shim directory (argv[0]!="ycode" AND YCODE_WRAP_SHIM=1),
	// dispatch to the shim child path which resolves the real binary
	// and exec's it under an ExecScopeWrappedAgent OTel span. See
	// internal/runtime/wrap/.
	if wrap.IsShimInvocation() {
		os.Exit(wrap.ShimMain())
	}

	// Intercept `ycode shell …` before cobra. The wrapper at
	// ~/bin/ycode-wrappers/bash makes ycode stand in for /bin/bash via
	// shebang, so foreign agents pass standard bash flags (-l, -lc,
	// --login, ...). Cobra rejects those as unknown or, worse, binds -l
	// as the value of -c. The interceptor parses argv with bash
	// semantics and dispatches straight into runShellCmd.
	if maybeHandleShellCmd() {
		return
	}

	// Check if self-healing is enabled
	if selfHealEnabled() {
		opts := &selfheal.WrapMainOptions{
			Config: selfheal.DefaultConfig(),
		}
		// Try to set up an AI provider for AI-driven healing.
		// This is best-effort — healing still works without it (API retry only).
		if provider := detectHealingProvider(); provider != nil {
			opts.Provider = provider
		}
		exitCode := selfheal.WrapMainWithOptions(realMain, opts)
		os.Exit(exitCode)
	}

	// Standard execution without self-healing
	if err := realMain(); err != nil {
		// Honor errors that carry a specific exit code (e.g.
		// weavecli's stable exit-code constants surfaced through
		// *exitCodeError). cobra already printed any envelope to
		// stderr, and the error's Error() text is just "exit N",
		// so suppress the duplicate "Error: exit N" line and pass
		// the code through cleanly.
		var ec interface{ ExitCode() int }
		if errors.As(err, &ec) {
			os.Exit(ec.ExitCode())
		}
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

// detectHealingProvider attempts to create an API provider for self-healing.
// Returns nil if no provider can be configured (no API keys, etc.).
func detectHealingProvider() api.Provider {
	// Use a small, fast model for healing to minimize cost and latency.
	providerCfg, err := api.DetectProvider("claude-haiku-4-5-20251001")
	if err != nil {
		return nil
	}
	return api.NewProvider(providerCfg)
}

// detectExtractProvider mirrors detectHealingProvider but resolves
// against the user's configured chat model so JSON extraction
// inherits the same defaults as in-session extraction. Returns
// nil when no API key is present; callers must guard the registration.
func detectExtractProvider(model string) api.Provider {
	if model == "" {
		return nil
	}
	providerCfg, err := api.DetectProvider(model)
	if err != nil {
		return nil
	}
	return api.NewProvider(providerCfg)
}

// realMain contains the actual main logic.
// It returns errors that may be healable by the self-heal system.
func realMain() error {
	// Install the shared telemetry plane from the standard OTEL env vars.
	//
	// ycode imports coreutils/pkg/telemetry (ExecMiddleware and friends) but
	// never called Init, and Init is what installs a TracerProvider. Without it
	// the global provider stays a no-op, so every span ycode produced went
	// nowhere no matter how the environment was configured — which is why
	// setting OTEL_EXPORTER_OTLP_ENDPOINT, the thing every doc tells you to do,
	// exported nothing and did so silently.
	//
	// Init is self-gating: with no exporter selected it installs the propagator
	// and returns, costing nothing. So this is safe on every verb, including
	// the ones that have no business emitting telemetry.
	//
	// This also gives ycode the file sink (OTEL_TRACES_EXPORTER=file) for free,
	// so ycode records spans with no collector running — the same property
	// bashy has, from the same code.
	shutdown := telemetry.Init(context.Background())
	defer func() {
		ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
		defer cancel()
		_ = shutdown(ctx)
	}()

	return rootCmd.Execute()
}

// newApp creates a full App instance. If workDirOverride is non-empty, it is
// used as the project directory instead of os.Getwd(). This enables the server
// to create per-project App instances for multi-session support.
func newApp(workDirOverride ...string) (*cli.App, error) {
	// Root context for all background goroutines — cancelled on App.Close().
	rootCtx, rootCancel := context.WithCancel(context.Background())
	success := false
	defer func() {
		if !success {
			rootCancel() // Clean up if newApp fails before registering cleanup.
		}
	}()

	// Determine config directories.
	home, _ := os.UserHomeDir()
	userDir := filepath.Join(home, ".config", "ycode")
	cwd, _ := os.Getwd()
	if len(workDirOverride) > 0 && workDirOverride[0] != "" {
		cwd = workDirOverride[0]
	}
	projectDir := filepath.Join(cwd, ".agents", "ycode")

	// Load config.
	loader := config.NewLoader(userDir, projectDir, projectDir)
	cfg, err := loader.Load()
	if err != nil {
		return nil, fmt.Errorf("load config: %w", err)
	}
	// `ycode serve --no-persona` is the operator switch that disables the
	// per-user persona system process-wide. Useful when one ycode serve is
	// shared by many end-users (e.g. a third-party host like classgo) and
	// the host wants its own identity model rather than ycode's persona
	// inference. The flag is unused outside the serve path.
	if serveNoPersona {
		cfg.PersonaEnabled = false
	}

	// Model resolution: --model flag > ANTHROPIC_MODEL > YCODE_MODEL > config > default.
	model := cfg.Model
	if envModel := os.Getenv("YCODE_MODEL"); envModel != "" {
		model = envModel
	}
	if envModel := os.Getenv("ANTHROPIC_MODEL"); envModel != "" {
		model = envModel
	}
	if modelFlag != "" {
		model = modelFlag
	}
	// A CASCADE agent (ycode-cascade-x4) is a ladder, not a model. Capture the
	// whole ladder here — this is the only point where the selector still
	// exists; one line down it has collapsed to the base model id, which is
	// precisely how a cascade ended up serving its cheapest rung for an entire
	// session. An explicit config ladder wins over the catalog's.
	if len(cfg.CascadeModels) == 0 {
		if ladder, ok := api.ResolveCascadeLadder(model); ok {
			cfg.CascadeModels = ladder
			slog.Info("cascade agent selected", "agent", model, "ladder", strings.Join(ladder, " → "))
		}
	}
	// A fleet selector (nickname / agent name / band like L3) resolves to a
	// concrete ycode model id; a literal model id passes through unchanged.
	if fm, note := api.ResolveFleetModel(model); fm != model {
		slog.Info("fleet model selection", "from", model, "resolved", note)
		model = fm
	}
	cfg.Model = api.ResolveModelWithAliases(model, cfg.Aliases)

	var provider api.Provider
	providerCfg, err := api.DetectProvider(cfg.Model)
	if err != nil {
		slog.Warn("no LLM provider available (agent features disabled until API key is configured)", "error", err)
	} else {
		// Preflight key-health probe: surface a stale/invalid key at startup —
		// at the door, not mid-run. Non-fatal: the run itself will also fail
		// fast with the same clear error, and transient probe problems pass.
		if err := api.PreflightAuthCheck(rootCtx, providerCfg); err != nil {
			slog.Warn("provider API key failed preflight authentication — runs will fail until the key is fixed", "error", err)
		}
		provider = api.NewProvider(providerCfg)
		if fallbackModel := api.DefaultModelForProvider(*providerCfg); fallbackModel != "" && fallbackModel != cfg.Model {
			fallbackProvider, fallbackErr := api.NewFallbackProvider(api.FallbackConfig{
				Providers:     []api.ProviderConfig{*providerCfg},
				FallbackModel: fallbackModel,
			})
			if fallbackErr != nil {
				slog.Warn("model fallback unavailable", "error", fallbackErr)
			} else {
				provider = fallbackProvider
			}
		}
	}
	// Initialize storage manager (Phase 1: KV store is instant).
	//
	// The root is per-session-overridable (YCODE_DATA_DIR / YCODE_HOME) so
	// concurrent ycode processes need not fight over one host-global
	// single-writer lock, and losing that lock is FATAL unless --no-store was
	// passed. See internal/runtime/datadir and docs/store-isolation.md.
	storageDataDir := datadir.Resolve(home)
	degraded := noStoreFlag || datadir.NoStore()
	lockTimeout := datadir.LockTimeout()
	storageMgr, err := store.NewManager(context.Background(), store.Config{
		DataDir:       storageDataDir,
		AllowDegraded: degraded,
		KVFactory: func(ctx context.Context) (store.KVStore, error) {
			return kv.OpenWithTimeout(storageDataDir, lockTimeout)
		},
		SQLFactory: func(ctx context.Context) (store.SQLStore, error) {
			return sqlstore.Open(storageDataDir)
		},
		VectorFactory: func(ctx context.Context) (store.VectorStore, error) {
			return vector.Open(storageDataDir)
		},
		SearchFactory: func(ctx context.Context) (store.SearchIndex, error) {
			return search.Open(storageDataDir)
		},
	})
	if err != nil {
		return nil, storeInitError(storageDataDir, lockTimeout, err)
	}
	if degraded {
		slog.Warn("running WITHOUT persistent storage (--no-store): memory, caches, and the code graph are disabled for this session",
			"data_dir", storageDataDir)
	}

	// Start background storage initialization (Phase 2: SQLite, Phase 3: vector/search).
	storageMgr.StartBackground(rootCtx)

	// Cache merged config in bbolt for cross-process access and stale detection.
	if kvStore := storageMgr.KV(); kvStore != nil {
		configPaths := []string{
			filepath.Join(userDir, "settings.json"),
			filepath.Join(projectDir, "settings.json"),
			filepath.Join(projectDir, "settings.local.json"),
		}
		configCache := config.NewCache(kvStore, configPaths)
		configCache.Store(cfg)
	}

	// Resolve OTEL storage path (needed before VFS creation).
	otelDataDir := resolveOTELDataDir(cfg.Observability)

	// Pre-generate instance ID so session and OTEL share the same UUID.
	instanceID := uuid.New().String()

	sessionDir := cfg.SessionDir
	if sessionDirFlag != "" {
		sessionDir = sessionDirFlag
	}
	sessionDir = resolveSessionDir(home, sessionDir)
	sess, err := createSessionForRun(sessionDir, instanceID, forkSessionID, resumeSessionID)
	if err != nil {
		return nil, err
	}
	if err := os.Setenv("YCODE_SESSION_ID", sess.ID); err != nil {
		return nil, fmt.Errorf("set YCODE_SESSION_ID: %w", err)
	}

	// Start background prompt cache eviction (waits for SQL to be ready).
	go storageMgr.StartEviction(rootCtx)

	// Memory — persistent cross-session agent memory.
	// Global: ~/.agents/ycode/memory/ (shared across projects).
	// Project: {cwd}/.agents/ycode/memory/ (project-specific).
	globalMemDir := filepath.Join(home, ".agents", "ycode", "memory")
	memoryDir := filepath.Join(cwd, ".agents", "ycode", "memory")
	memManager, err := memory.NewManagerWithGlobal(globalMemDir, memoryDir)
	if err != nil {
		return nil, fmt.Errorf("create memory manager: %w", err)
	}

	// Memex queryable graph (bonsai). Lives next to the SQLite/KV stores
	// under storageDataDir so all persistence shares one root. Best-effort:
	// failure to open is logged but does not block startup; the codegraph
	// gfy pipeline keeps working without DQL queryability.
	graphDir := filepath.Join(storageDataDir, "graph")
	memGraph, err := memexgraph.Open(memexgraph.Options{Dir: graphDir})
	if err != nil {
		// Same single-writer contention as the KV store: badger takes an
		// exclusive lock on the directory, so a second concurrent ycode loses
		// it. Only an explicit --no-store run may shrug that off.
		if !degraded {
			return nil, storeInitError(graphDir, lockTimeout, err)
		}
		slog.Warn("memex graph unavailable; query_graph_dql disabled", "error", err)
		memGraph = nil
	}

	// Plan mode manager.
	ycodeDir := filepath.Join(cwd, ".agents", "ycode")
	planMode := tools.NewPlanModeManager(ycodeDir)

	// Build VFS with allowed directories (includes OTEL storage for cross-instance access).
	allowedDirs := []string{os.TempDir(), cwd, otelDataDir}
	allowedDirs = append(allowedDirs, cfg.AllowedDirectories...)
	v, err := vfs.New(allowedDirs, nil)
	if err != nil {
		return nil, fmt.Errorf("create vfs: %w", err)
	}

	// Initialize tool registry with handlers. When the operator passes
	// --tools-allowlist or --tools-blocklist at `ycode serve` start, register
	// only the permitted built-ins. Allowlist wins when both are set.
	// Operator-level restriction; per-session enforcement (G-E) lands with G-I.
	toolReg := tools.NewRegistry()
	switch {
	case len(serveToolsAllowlist) > 0:
		tools.RegisterBuiltinsFiltered(toolReg, serveToolsAllowlist)
	case len(serveToolsBlocklist) > 0:
		tools.RegisterBuiltinsExcluding(toolReg, serveToolsBlocklist)
	default:
		tools.RegisterBuiltins(toolReg)
	}

	// Quality monitoring: track tool reliability metrics and surface degraded tools
	// in the system prompt diagnostics section.
	qm := tools.NewQualityMonitor(0.7)
	toolReg.SetQualityMonitor(qm)

	jobRegistry := bash.NewJobRegistry()
	// Construct the unified Computer gateway. All agent-driven shell,
	// fs, and web operations route through its surfaces. Execution is
	// host-local; container isolation is delegated to an external host layer.
	var localOpts []computer.LocalOption
	gateway := computer.NewLocal(v, localOpts...)
	bashWorkDir := cwd
	tools.RegisterBashHandler(toolReg, bashWorkDir, jobRegistry, gateway.Shell())
	tools.RegisterFileHandlers(toolReg, gateway.Files())
	tools.RegisterSearchHandlers(toolReg, v)
	tools.RegisterSymbolSearchHandler(toolReg)
	tools.RegisterReferenceHandlers(toolReg)
	tools.RegisterASTSearchHandler(toolReg, &tools.ASTSearchDeps{WorkDir: cwd})
	tools.RegisterVFSHandlers(toolReg, v)
	tools.RegisterSleepHandler(toolReg)
	tools.RegisterWebHandlers(toolReg, gateway.Web())
	tools.RegisterToolSearchHandler(toolReg)
	tools.RegisterSkillHandler(toolReg)
	tools.RegisterMemosHandlers(toolReg)
	tools.SetMemoryManager(memManager)
	tools.RegisterMemoryHandlers(toolReg)
	tools.RegisterRemoteHandler(toolReg)
	tools.RegisterNotebookHandler(toolReg, v)
	tools.RegisterModeHandlers(toolReg, planMode)
	tools.RegisterConfigHandler(toolReg, cfg)
	tools.RegisterSemanticSearchHandler(toolReg)
	tools.RegisterGitHandlers(toolReg, &tools.GitToolsDeps{WorkDir: cwd})
	tools.RegisterTestRunnerHandler(toolReg, cwd)

	// LSP: auto-detect available language servers and register them.
	lspRegistry := lsp.NewClientRegistry()
	for _, serverCfg := range lsp.AutoDetectServers() {
		client := lsp.NewClient(serverCfg)
		client.SetRootDir(cwd)
		lspRegistry.Register(serverCfg.Language, client)
		slog.Info("registered LSP server", "language", serverCfg.Language, "command", serverCfg.Command)
	}
	tools.RegisterLSPHandler(toolReg, lspRegistry)

	// Wire permission enforcement: resolve current mode from the live
	// settings.local.json file so that plan mode toggles take effect immediately.
	localConfigPath := filepath.Join(ycodeDir, "settings.local.json")
	skipPerms := dangerSkipPermissions
	toolReg.SetPermissionResolver(func() permission.Mode {
		// --danger-skip-permissions bypasses all checks.
		if skipPerms {
			return permission.DangerFullAccess
		}
		// Check local override first (plan mode writes here).
		if val, ok := config.GetLocalConfigField(localConfigPath, "permissionMode"); ok {
			if s, ok := val.(string); ok {
				return permission.ParseMode(s)
			}
		}
		// Fall back to in-memory config.
		return permission.ParseMode(cfg.PermissionMode)
	})

	// Persist permission policy in bbolt for cross-session approval history.
	if kvStore := storageMgr.KV(); kvStore != nil {
		permCache := permission.NewCache(kvStore)
		permCache.StorePolicy(permission.NewPolicy(permission.ParseMode(cfg.PermissionMode)))
	}

	// Wire Bleve search index and background codebase indexer once search backend is ready.
	go func() {
		ctx, cancel := context.WithTimeout(rootCtx, 30*time.Second)
		defer cancel()
		searchStore := storageMgr.Search(ctx)
		if searchStore == nil {
			return
		}

		// Index tool descriptions for semantic ToolSearch.
		toolIdx := tools.NewToolSearchIndex(searchStore)
		toolIdx.IndexTools(toolReg)
		toolReg.SetSearchIndex(toolIdx)

		// Wire Bleve-backed full-text search into memory manager.
		if memManager != nil {
			bleveSearcher := memory.NewBleveSearcher(searchStore)
			memManager.SetBleveSearcher(bleveSearcher)
			// Index existing memories for immediate searchability.
			if mems, err := memManager.All(); err == nil && len(mems) > 0 {
				bleveSearcher.IndexAll(mems)
			}
		}

		// Set up Bleve fallback for Grep tool.
		tools.SetCodeSearchIndex(searchStore)

		// Attach session search indexer for compaction indexing.
		sessIndexer := session.NewSearchIndexer(searchStore, sess.ID)
		sess.SetSearchIndexer(sessIndexer)

		// Start background codebase indexer.
		codeIndexer := indexer.New(cwd, searchStore, storageMgr.KV())

		// Wire reference graph to tools.
		if codeIndexer.RefGraph != nil {
			tools.SetRefGraph(codeIndexer.RefGraph)
		}

		// Wire file write hook for incremental re-indexing.
		toolReg.SetFileWriteHook(codeIndexer.NotifyFileChanged)

		// Skip the eager initial index pass for one-shot `--print` runs: it scans +
		// tree-sitter-parses the whole repo (~hundreds of ms) and competes for CPU/DB
		// with the live turn, yet a one-shot task typically just greps/reads/edits.
		// The wiring above stays, so grep (ripgrep) works and NotifyFileChanged still
		// re-indexes lazily; semantic search degrades to empty, not error. Also
		// skipped in controlled mode (--events) so a steerable/coached session starts
		// fast enough that its opening prompt is not swallowed by startup churn.
		if !leanStartup() {
			go codeIndexer.Run(rootCtx)
		}
	}()

	// Attach SQLite dual-writer, index sessions, and start metrics once SQL is ready.
	go func() {
		ctx, cancel := context.WithTimeout(rootCtx, 30*time.Second)
		defer cancel()
		sqlStore := storageMgr.SQL(ctx)
		if sqlStore == nil {
			return
		}

		// Attach dual-writer for current session.
		w := session.NewSQLWriter(sqlStore, sess.ID)
		w.EnsureSession(cfg.Model)
		sess.SetSQLWriter(w)

		// Apply tool usage metrics middleware.
		metrics := tools.NewMetricsRecorder(rootCtx, sqlStore, sess.ID)
		metrics.ApplyToRegistry(toolReg)

		// Wire agent-facing metrics query tool.
		tools.SetMetricsStore(sqlStore)
		tools.SetMetricsSessionID(sess.ID)
		tools.RegisterQueryMetricsHandler(toolReg)

		// Wire agent-facing trace and log query tools.
		tools.SetOTELDataDir(resolveOTELDataDir(cfg.Observability))
		tools.RegisterQueryTracesHandler(toolReg)
		tools.RegisterQueryLogsHandler(toolReg)

		// Index any existing JSONL sessions not yet in SQLite.
		indexer := session.NewIndexer(sqlStore, sessionDir)
		if n, err := indexer.IndexAll(ctx); err != nil {
			slog.Debug("session indexer", "error", err)
		} else if n > 0 {
			slog.Debug("session indexer", "indexed", n)
		}
	}()

	// Build project context for system prompt.
	promptCtx := buildPromptContext(cwd, cfg.Model, cfg.Instructions, memManager)
	promptCtx.AllowedDirs = v.AllowedDirs()

	// Wire vector store and background embedder once vector backend is ready.
	go func() {
		ctx, cancel := context.WithTimeout(rootCtx, 30*time.Second)
		defer cancel()
		vectorStore := storageMgr.Vector(ctx)
		if vectorStore == nil {
			return
		}

		// Detect embedding provider.
		embProvider := embedding.DetectProvider()

		// Wire vector store into semantic search tool.
		tools.SetVectorStore(vectorStore)

		// Wire vector searcher into memory manager.
		if memManager != nil {
			memManager.SetVectorSearcher(memory.NewVectorSearcher(vectorStore))
		}

		// Start background code embedding and doc indexing.
		embedder := embedding.New(embProvider, vectorStore, storageMgr.KV(), cwd)

		// Skip the eager embedding passes for one-shot `--print` runs: they embed up
		// to 500 code files + the doc set, competing for CPU/DB with the live turn,
		// for a semantic index a short task never queries. The vector store wiring
		// above stays, so semantic tools resolve (to empty) rather than nil-panic.
		// Also skipped in controlled mode (--events) for a fast, input-ready startup.
		if !leanStartup() {
			// Embed documentation files (CLAUDE.md, README, etc.).
			go func() {
				embedCtx := rootCtx
				for _, cf := range promptCtx.ContextFiles {
					if cf.Content != "" {
						relPath := cf.Path
						if rel, err := filepath.Rel(cwd, cf.Path); err == nil {
							relPath = rel
						}
						if err := embedder.EmbedDocFile(embedCtx, relPath, cf.Content); err != nil {
							slog.Debug("embedder: doc file", "path", relPath, "error", err)
						}
					}
				}
			}()

			// Embed code files.
			go func() {
				if n, err := embedder.RunCodeEmbedding(rootCtx); err != nil {
					if rootCtx.Err() == nil { // only log if not a shutdown cancellation
						slog.Debug("embedder: code pass", "error", err)
					}
				} else if n > 0 {
					slog.Debug("embedder: code pass", "embedded", n)
				}
			}()
		}
	}()

	// Start background memory consolidation (stale removal, dedup). Opt-in via
	// cfg.AutoDreamEnabled (default off): the SOTA harnesses do not run autonomous
	// consolidation as a default, and a hard-coded `true` here silently ignored the
	// config field — an absence-of-evidence bug (a setting nothing read). Off by
	// default; Start() is a no-op when disabled.
	go memory.NewDreamer(memManager, cfg.AutoDreamEnabled).Start(rootCtx)

	// Resolve project + agent-tool attribution once per process.
	// Carried into the OTEL provider as resource attributes so every
	// trace, metric, and log gets attributed automatically.
	org := origin.Resolve(rootCtx, cwd, cfg)

	// Wire OTEL observability.
	// Always-on: file-only mode persists traces/metrics/logs locally.
	// With Observability enabled (the default in ycode): full mode
	// adds gRPC export to the collector. Disable explicitly by
	// setting `observability.enabled: false` in settings.json.
	var otelRes *otelResult
	if cfg.Observability.IsEnabled() {
		otelRes = setupOTEL(cfg, sess, toolReg, provider, v, org)
	} else {
		otelRes = setupFileOTEL(cfg, sess, toolReg, provider, v, org)
	}
	if otelRes != nil && otelRes.shutdown != nil {
		selfheal.RegisterPanicHook(otelRes.shutdown)
	}
	var convOTEL *conversation.OTELConfig
	if otelRes != nil {
		convOTEL = otelRes.convOTEL
		// Wire OTEL instruments into search tools.
		if convOTEL != nil && convOTEL.Inst != nil {
			tools.SetSearchInstruments(convOTEL.Inst)
		}
	}

	// Inference router: enables Tier 2 LLM-based tool pre-activation and
	// multi-factor model routing for lightweight tasks (classification, summarization).
	// Uses the main provider as the classification candidate.
	inferenceRouter := routing.NewRouter(
		routing.WithStatsProvider(&routing.QualityMonitorStats{Monitor: qm}),
	)
	if provider != nil {
		inferenceRouter.RegisterCandidateForAll(routing.Candidate{
			Provider: provider,
			Model:    cfg.Model,
		})
	}

	app, err := cli.NewApp(cfg, provider, sess, cli.AppOptions{
		WorkDir: cwd,
		ProviderKind: func() string {
			if providerCfg != nil {
				return providerCfg.DisplayKind()
			}
			return "none"
		}(),
		ConfigDirs: commands.ConfigDirs{
			UserDir:    userDir,
			ProjectDir: projectDir,
			LocalDir:   projectDir,
		},
		MemoryDir:      memoryDir,
		Version:        version,
		PlanMode:       planMode,
		ToolRegistry:   toolReg,
		PromptCtx:      promptCtx,
		UserConfigPath: filepath.Join(userDir, "settings.json"),
		Storage:        storageMgr,
		ConvOTEL:       convOTEL,
		CloudboxLister: api.NewCloudboxLister(
			os.Getenv("DHNT_BASE_URL"),
			os.Getenv("DHNT_API_KEY"),
			nil,
		),
		InferenceRouter: inferenceRouter,
		MemoryManager:   memManager,
		MemexGraph:      memGraph,
	})
	if err != nil {
		return nil, err
	}

	// Register compact_context tool handler — needs the app for session access.
	tools.RegisterCompactContextHandler(toolReg, app.CompactContext)

	// Register cleanup (LIFO order — last registered runs first):
	// 1. rootCancel: stop background goroutines so they stop producing telemetry
	// 2. OTEL shutdown: flush remaining spans/metrics/logs
	// 3. LSP servers: shut down language server processes
	app.RegisterCleanup(func() { lspRegistry.Close() })
	if otelRes != nil {
		app.RegisterCleanup(otelRes.shutdown)
	}
	app.RegisterCleanup(rootCancel)

	success = true
	return app, nil
}

// buildPromptContext gathers environment and project metadata for the system prompt.
const promptContextStartupTimeout = 5 * time.Second

const (
	repoMapPreflightTimeout = time.Second
	repoMapStartupMaxFiles  = 2000
	repoMapStartupMaxBytes  = 32 << 20
)

func buildPromptContext(workDir, model string, configInstructions []string, memManager *memory.Manager) *prompt.ProjectContext {
	startupCtx, cancelStartup := context.WithTimeout(context.Background(), promptContextStartupTimeout)
	defer cancelStartup()

	ctx := &prompt.ProjectContext{
		WorkDir:     workDir,
		CurrentDate: time.Now().Format("2006-01-02"),
		Platform:    runtime.GOOS,
		Model:       model,
	}

	// Shell.
	if shell := os.Getenv("SHELL"); shell != "" {
		ctx.Shell = filepath.Base(shell)
	}

	// Git context.
	gitCtx := git.Discover(workDir)
	if gitCtx.IsRepo {
		ctx.IsGitRepo = true
		ctx.ProjectRoot = gitCtx.Root
		ctx.GitBranch = gitCtx.Branch
		ctx.MainBranch = gitCtx.MainBranch
		ctx.GitUser = gitCtx.User
		ctx.GitStatus = gitCtx.Status
		ctx.RecentCommits = gitCtx.RecentCommits
	}
	// Fallback: use workDir as project root if not a git repo.
	if ctx.ProjectRoot == "" {
		ctx.ProjectRoot = workDir
	}

	// Discover instruction files, load memories, and generate repo map concurrently.
	contextFilesCh := make(chan []prompt.ContextFile, 1)
	go func() {
		contextFilesCh <- discoverContextFiles(workDir, ctx.ProjectRoot)
	}()

	memoriesCh := make(chan []*memory.Memory, 1)
	if memManager != nil {
		go func() {
			if mems, err := memManager.All(); err != nil {
				slog.Debug("memory load", "error", err)
				memoriesCh <- nil
			} else {
				memoriesCh <- mems
			}
		}()
	} else {
		close(memoriesCh)
	}

	// Generate the repo map after a bounded preflight, excluding checked-out submodules. A
	// repo map for an umbrella should describe the umbrella, not recursively
	// parse every sibling repository (and its build artifacts) on each prompt.
	repoMapCh := make(chan string, 1)
	if leanStartup() {
		// One-shot `--print` OR controlled mode (--events): skip the repo map entirely.
		// It is enrichment, not a prerequisite, and computing it forks a child process
		// that tree-sitter-parses the tree — several seconds of SYNCHRONOUS pre-turn-1
		// latency the agent may never need (it can grep/glob on demand). In a
		// steerable/coached session that latency is what swallows the opening prompt.
		// Attended interactive sessions (no --events) still get it.
		repoMapCh <- ""
	} else if text, err := startupRepoMap(startupCtx, ctx.ProjectRoot); err != nil {
		slog.Debug("startup repo map skipped", "error", err)
		repoMapCh <- ""
	} else {
		repoMapCh <- text
	}
	close(repoMapCh)

	// Startup context is enrichment, not a prerequisite for reaching the model.
	// Bound the whole phase so a pathological tree or backend cannot make a
	// one-shot command appear to hang before it even opens a network connection.
	awaitPromptContext(startupCtx, ctx, contextFilesCh, memoriesCh, repoMapCh)

	// Config-based instruction paths (relative, absolute, ~/home, URLs).
	if len(configInstructions) > 0 {
		configured := prompt.LoadConfiguredInstructions(configInstructions, ctx.ProjectRoot)
		// Deduplicate against already-discovered files.
		seen := make(map[string]bool, len(ctx.ContextFiles))
		for _, cf := range ctx.ContextFiles {
			if cf.Hash != "" {
				seen[cf.Hash] = true
			}
		}
		for _, cf := range configured {
			if !seen[cf.Hash] {
				seen[cf.Hash] = true
				ctx.ContextFiles = append(ctx.ContextFiles, cf)
			}
		}
	}

	return ctx
}

func awaitPromptContext(
	startupCtx context.Context,
	ctx *prompt.ProjectContext,
	contextFilesCh <-chan []prompt.ContextFile,
	memoriesCh <-chan []*memory.Memory,
	repoMapCh <-chan string,
) {
	for contextFilesCh != nil || memoriesCh != nil || repoMapCh != nil {
		select {
		case files, ok := <-contextFilesCh:
			if ok {
				ctx.ContextFiles = files
			}
			contextFilesCh = nil
		case mems, ok := <-memoriesCh:
			if ok {
				ctx.Memories = mems
			}
			memoriesCh = nil
		case text, ok := <-repoMapCh:
			if ok {
				ctx.RepoMapText = text
			}
			repoMapCh = nil
		case <-startupCtx.Done():
			slog.Warn("prompt context startup timed out; continuing with available context",
				"timeout", promptContextStartupTimeout, "error", startupCtx.Err())
			// Preserve work that completed while the killable repo-map helper
			// consumed the deadline; only abandon channels that are still pending.
			select {
			case files := <-contextFilesCh:
				ctx.ContextFiles = files
			default:
			}
			select {
			case mems := <-memoriesCh:
				ctx.Memories = mems
			default:
			}
			select {
			case text := <-repoMapCh:
				ctx.RepoMapText = text
			default:
			}
			return
		}
	}
}

// startupRepoMap performs a cheap, cancellable inventory before invoking the
// parser. Generate itself has no context-aware API, so starting it in a
// goroutine and abandoning that goroutine at a deadline would only hide the
// hang while it continued consuming CPU and memory. The inventory gives the
// parser an explicit, bounded file set instead.
func startupRepoMap(ctx context.Context, projectRoot string) (string, error) {
	executable, err := os.Executable()
	if err != nil {
		return "", err
	}
	return runStartupRepoMapHelper(ctx, executable, "__startup-repomap", projectRoot)
}

func runStartupRepoMapHelper(ctx context.Context, executable string, args ...string) (string, error) {
	cmd := exec.CommandContext(ctx, executable, args...)
	out, err := cmd.Output()
	if err != nil {
		if ctx.Err() != nil {
			return "", ctx.Err()
		}
		return "", err
	}
	return string(out), nil
}

func generateStartupRepoMap(projectRoot string) (string, error) {
	ctx, cancel := context.WithTimeout(context.Background(), repoMapPreflightTimeout)
	defer cancel()

	submodules := repoMapSubmoduleDirs(projectRoot)
	var files []string
	var totalBytes int64
	err := filepath.WalkDir(projectRoot, func(path string, entry os.DirEntry, walkErr error) error {
		if err := ctx.Err(); err != nil {
			return err
		}
		if walkErr != nil {
			return nil
		}
		rel, err := filepath.Rel(projectRoot, path)
		if err != nil {
			return nil
		}
		if entry.IsDir() {
			if rel == "." {
				return nil
			}
			if _, ok := submodules[filepath.Clean(rel)]; ok {
				return filepath.SkipDir
			}
			switch entry.Name() {
			case ".git", "node_modules", "vendor", "__pycache__", ".tox", ".venv", "dist", "build", "priorart", "external", "reference", "testdata", "fixtures":
				return filepath.SkipDir
			}
			// Also respect undeclared nested worktrees. Git submodule checkouts
			// normally have a .git file rather than a directory.
			if _, err := os.Lstat(filepath.Join(path, ".git")); err == nil {
				return filepath.SkipDir
			}
			return nil
		}
		switch strings.ToLower(filepath.Ext(path)) {
		case ".go", ".py", ".js", ".ts", ".tsx", ".jsx", ".rs", ".java":
		default:
			return nil
		}
		info, err := entry.Info()
		if err != nil {
			return nil
		}
		files = append(files, path)
		totalBytes += info.Size()
		if len(files) > repoMapStartupMaxFiles || totalBytes > repoMapStartupMaxBytes {
			return fmt.Errorf("source inventory exceeds startup budget (%d files, %d bytes)", len(files), totalBytes)
		}
		return nil
	})
	if err != nil {
		return "", err
	}
	rm, err := repomap.GenerateForFiles(projectRoot, files, repomap.DefaultOptions())
	if err != nil {
		return "", err
	}
	return rm.Format(), nil
}

func repoMapSubmoduleDirs(projectRoot string) map[string]struct{} {
	dirs := make(map[string]struct{})
	data, err := os.ReadFile(filepath.Join(projectRoot, ".gitmodules"))
	if err != nil {
		return dirs
	}
	for _, line := range strings.Split(string(data), "\n") {
		key, value, ok := strings.Cut(strings.TrimSpace(line), "=")
		if !ok || strings.TrimSpace(key) != "path" {
			continue
		}
		path := filepath.Clean(strings.TrimSpace(value))
		if path == "." || filepath.IsAbs(path) || path == ".." || strings.HasPrefix(path, ".."+string(filepath.Separator)) {
			continue
		}
		dirs[path] = struct{}{}
	}
	return dirs
}

// discoverContextFiles finds and loads instruction files from:
//  1. Project-level: walk from workDir up to projectRoot (AGENTS.md, CLAUDE.md, etc.)
//  2. Global: user-level instruction files (~/.config/ycode/, ~/.agents/ycode/)
func discoverContextFiles(workDir, projectRoot string) []prompt.ContextFile {
	// Project-level discovery.
	discovered := prompt.DiscoverInstructionFiles(workDir, projectRoot)

	// Global discovery — check user config directories for user-wide instructions.
	// Deduplicate against project-level files by content hash.
	seen := make(map[string]bool, len(discovered))
	for _, f := range discovered {
		if f.Hash != "" {
			seen[f.Hash] = true
		}
	}

	home, _ := os.UserHomeDir()
	globalDirs := []string{
		filepath.Join(home, ".config", "ycode"),
		filepath.Join(home, ".agents", "ycode"),
	}
	for _, dir := range globalDirs {
		for _, gf := range prompt.DiscoverGlobalInstructionFiles(dir) {
			if seen[gf.Hash] {
				continue
			}
			seen[gf.Hash] = true
			discovered = append(discovered, gf)
		}
	}

	var files []prompt.ContextFile
	for _, f := range discovered {
		files = append(files, prompt.ContextFile{
			Path:    f.Path,
			Content: f.Content,
			Hash:    f.Hash,
		})
	}
	return files
}

var (
	printFlag             bool
	noStoreFlag           bool
	traceVerbose          bool
	modelFlag             string
	dangerSkipPermissions bool
	connectURL            string
	eventsFile            string
	sessionDirFlag        string
	forkSessionID         string
	resumeSessionID       string
	noInteractiveFlag     bool
	yesFlag               bool
)

var rootCmd = &cobra.Command{
	Use:   "ycode",
	Short: "ycode – autonomous agent harness for software development",
	Long: "ycode is a CLI agent harness that provides 50+ tools, LSP integration, a plugin system, permission enforcement, and session management.\n\n" +
		"Agent-facing capability prompts: `ycode docs` (curated for LLMs; complement to this human-facing help).",
	// Hide cobra's auto-generated `completion` subcommand from the top-level
	// help — operators who want it can still call `ycode completion <shell>`,
	// it's just not surfaced in the `ycode --help` listing.
	CompletionOptions: cobra.CompletionOptions{HiddenDefaultCmd: true},
	// No PersistentPreRun: ycode does not auto-modify a repo on first
	// invocation. To establish ycode in a repo (write
	// <repo>/.agents/ycode/AGENTS.md), run `ycode init` explicitly.
	RunE: func(cmd *cobra.Command, args []string) error {
		ctx := ctxWithUnattendedFlag(cmd.Context(), cmd)

		// Dry-run mode: preview session setup without calling the model.
		if dryRun, _ := cmd.Flags().GetBool("dry-run"); dryRun {
			printReadinessReport()
			return nil
		}

		// Remote mode: connect to a running ycode server.
		if connectURL != "" {
			return runRemoteTUI(connectURL)
		}

		// Unattended contexts (weave workspace, CI, --no-interactive,
		// --yes) must never open an interactive TUI or prompt on stdin.
		// Piped input and positional args are treated as the prompt; with
		// no prompt we emit a readiness report and exit cleanly.
		if unattended.IsUnattended(ctx) {
			origin.SetAgentTool(origin.ToolPrompt)
			stat, _ := os.Stdin.Stat()
			isPiped := (stat.Mode() & os.ModeCharDevice) == 0

			if isPiped {
				input, err := io.ReadAll(os.Stdin)
				if err != nil {
					return fmt.Errorf("read stdin: %w", err)
				}
				prompt := strings.TrimSpace(string(input))
				if prompt == "" {
					return fmt.Errorf("empty input from stdin")
				}
				app, err := newApp()
				if err != nil {
					return err
				}
				defer app.Close()
				app.SetPrintMode(true)
				return app.RunPrompt(ctx, prompt)
			}

			if len(args) > 0 {
				app, err := newApp()
				if err != nil {
					return err
				}
				defer app.Close()
				app.SetPrintMode(true)
				return app.RunPrompt(ctx, strings.Join(args, " "))
			}

			printReadinessReport()
			return nil
		}

		// Default to TUI for the root invocation. Per-command RunE
		// overrides this before calling newApp() (prompt sets
		// "prompt", etc.).
		origin.SetAgentTool(origin.ToolTUI)

		// Check for piped input.
		stat, _ := os.Stdin.Stat()
		isPiped := (stat.Mode() & os.ModeCharDevice) == 0

		if isPiped {
			origin.SetAgentTool(origin.ToolPrompt)
			input, err := io.ReadAll(os.Stdin)
			if err != nil {
				return fmt.Errorf("read stdin: %w", err)
			}
			prompt := strings.TrimSpace(string(input))
			if prompt == "" {
				return fmt.Errorf("empty input from stdin")
			}
			// Prefer an already-running server; otherwise fall through to
			// in-process. ycode no longer auto-spawns `ycode serve`.
			if os.Getenv("YCODE_NO_SERVER") == "" && !hasSessionContinuationFlags() {
				if baseURL, err := ensureServer(); err == nil {
					return runServerPrompt(baseURL, prompt)
				} else {
					fmt.Fprintf(os.Stderr, "ycode: %s; running in-process (some server-mode features unavailable)\n", err)
				}
			}
			app, err := newApp()
			if err != nil {
				return err
			}
			defer app.Close()
			if printFlag {
				app.SetPrintMode(true)
			}
			if eventsFile != "" {
				if err := app.SetEventsFile(eventsFile); err != nil {
					return fmt.Errorf("events file: %w", err)
				}
			}
			return app.RunPrompt(ctx, prompt)
		}

		// Client-server mode if a server is already running; otherwise
		// fall through to in-process. ycode does not auto-spawn the
		// server — that is the user's responsibility (run `ycode serve`
		// in another terminal, or install it as a system service).
		if os.Getenv("YCODE_NO_SERVER") == "" {
			if _, ok := detectServer(); ok {
				return runThinTUIAsync()
			}
			fmt.Fprintf(os.Stderr, "ycode: %s; running in-process (some server-mode features unavailable)\n", ErrServerNotRunning)
		}

		// In-process mode: full feature set, self-contained TUI.
		app, err := newApp()
		if err != nil {
			return err
		}
		defer app.Close()

		// --events on the INTERACTIVE path. This is the one that mattered and the
		// one that was missing: the flag is persistent, so it parsed fine here, and
		// then nothing wired it up. `ycode --events x` was accepted, ignored, and
		// wrote an empty file — a flag that looks supported and does nothing.
		//
		// It matters because the TUI is what an orchestrator launches when it wants
		// a session it can STEER, so it is exactly the path that most needs to
		// report its turn boundaries. Without this, bashy asked ycode when its turn
		// ended, heard nothing, and fell back to guessing from 25 seconds of silence.
		if eventsFile != "" {
			if err := app.SetEventsFile(eventsFile); err != nil {
				return fmt.Errorf("events file: %w", err)
			}
		}
		return app.RunInteractive(context.Background())
	},
}

func hasSessionContinuationFlags() bool {
	return forkSessionID != "" || resumeSessionID != ""
}

// ctxWithUnattendedFlag applies the global --no-interactive / --yes flags to
// the context so the rest of the binary can query unattended.IsUnattended(ctx).
func ctxWithUnattendedFlag(ctx context.Context, cmd *cobra.Command) context.Context {
	noInteractive, _ := cmd.Flags().GetBool("no-interactive")
	yes, _ := cmd.Flags().GetBool("yes")
	return unattended.WithValue(ctx, noInteractive || yes)
}

// printReadinessReport prints a quick provider/workspace readiness report to
// stdout. It does not construct a full App and is safe to call without a TTY.
func printReadinessReport() {
	report := health.NewReadinessReport()
	if os.Getenv("ANTHROPIC_API_KEY") != "" || os.Getenv("OPENAI_API_KEY") != "" {
		report.Add("provider", health.StatusReady, "API key found")
	} else {
		report.Add("provider", health.StatusBlocked, "No API key (set ANTHROPIC_API_KEY or OPENAI_API_KEY)")
	}
	if _, err := os.Getwd(); err == nil {
		report.Add("workspace", health.StatusReady, "Working directory accessible")
	}
	fmt.Print(report.Format())
}

var promptCmd = &cobra.Command{
	Use:   "prompt [message]",
	Short: "Send a one-shot prompt to the agent",
	Args:  cobra.MinimumNArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		ctx := ctxWithUnattendedFlag(cmd.Context(), cmd)
		origin.SetAgentTool(origin.ToolPrompt)
		app, err := newApp()
		if err != nil {
			return err
		}
		defer app.Close()
		if printFlag {
			app.SetPrintMode(true)
		}
		if eventsFile != "" {
			if err := app.SetEventsFile(eventsFile); err != nil {
				return fmt.Errorf("events file: %w", err)
			}
		}
		prompt := strings.Join(args, " ")
		return app.RunPrompt(ctx, prompt)
	},
}

var versionCmd = &cobra.Command{
	Use:   "version",
	Short: "Print version information",
	Run: func(cmd *cobra.Command, args []string) {
		fmt.Printf("ycode %s (%s)\n", version, commit)
	},
}

var doctorCmd = &cobra.Command{
	Use:   "doctor",
	Short: "Run health checks",
	RunE: func(cmd *cobra.Command, args []string) error {
		// Dry-run mode: quick readiness check without starting the full system.
		if dryRun, _ := cmd.Flags().GetBool("dry-run"); dryRun {
			report := health.NewReadinessReport()
			if os.Getenv("ANTHROPIC_API_KEY") != "" || os.Getenv("OPENAI_API_KEY") != "" || os.Getenv("XAI_API_KEY") != "" {
				report.Add("provider", health.StatusReady, "API key found")
			} else {
				report.Add("provider", health.StatusBlocked, "No API key set")
			}
			if wd, err := os.Getwd(); err == nil {
				report.Add("workspace", health.StatusReady, wd)
			} else {
				report.Add("workspace", health.StatusWarning, "Cannot determine working directory")
			}
			home, _ := os.UserHomeDir()
			configDir := filepath.Join(home, ".config", "ycode")
			if _, err := os.Stat(configDir); err == nil {
				report.Add("config", health.StatusReady, "Config directory exists")
			} else {
				report.Add("config", health.StatusWarning, "Config directory not created yet")
			}
			fmt.Print(report.Format())
			return nil
		}

		fmt.Println("ycode doctor - Health Check")
		fmt.Println("===========================")

		checks := []struct {
			name  string
			check func() (string, bool)
		}{
			{"Go version", func() (string, bool) {
				return "go1.24+ (compiled)", true
			}},
			{"API key", func() (string, bool) {
				if os.Getenv("ANTHROPIC_API_KEY") != "" {
					return "ANTHROPIC_API_KEY set", true
				}
				if os.Getenv("OPENAI_API_KEY") != "" {
					return "OPENAI_API_KEY set", true
				}
				if os.Getenv("XAI_API_KEY") != "" {
					return "XAI_API_KEY set", true
				}
				if os.Getenv("DASHSCOPE_API_KEY") != "" {
					return "DASHSCOPE_API_KEY set", true
				}
				if os.Getenv("MOONSHOT_API_KEY") != "" {
					return "MOONSHOT_API_KEY set", true
				}
				if os.Getenv("KIMI_API_KEY") != "" {
					return "KIMI_API_KEY set", true
				}
				if os.Getenv("DEEPSEEK_API_KEY") != "" {
					return "DEEPSEEK_API_KEY set", true
				}
				if token, err := oauth.LoadCredentials(); err == nil {
					if token.IsExpired() {
						return "OAuth token expired (run: ycode login)", false
					}
					return "OAuth credentials found", true
				}
				return "No API key or OAuth credentials found (set ANTHROPIC_API_KEY or run: ycode login)", false
			}},
			{"Config directory", func() (string, bool) {
				home, _ := os.UserHomeDir()
				dir := filepath.Join(home, ".config", "ycode")
				if _, err := os.Stat(dir); err == nil {
					return dir + " exists", true
				}
				return dir + " (will be created on first use)", true
			}},
			{"Storage backends", func() (string, bool) {
				home, _ := os.UserHomeDir()
				dataDir := filepath.Join(home, ".agents", "ycode", "projects", "data")

				var parts []string
				// Check KV store.
				kvPath := filepath.Join(dataDir, "ycode.kv")
				if _, err := os.Stat(kvPath); err == nil {
					parts = append(parts, "KV(ok)")
				} else {
					parts = append(parts, "KV(not initialized)")
				}
				// Check SQLite.
				dbPath := filepath.Join(dataDir, "ycode.db")
				if _, err := os.Stat(dbPath); err == nil {
					parts = append(parts, "SQLite(ok)")
				} else {
					parts = append(parts, "SQLite(not initialized)")
				}
				// Check vector store.
				vecPath := filepath.Join(dataDir, "vectors")
				if _, err := os.Stat(vecPath); err == nil {
					parts = append(parts, "Vector(ok)")
				} else {
					parts = append(parts, "Vector(not initialized)")
				}

				msg := strings.Join(parts, ", ")
				allOk := !strings.Contains(msg, "not initialized")
				return msg, allOk
			}},
			{"Git", func() (string, bool) {
				if _, err := exec.LookPath("git"); err != nil {
					return "git not found in PATH", false
				}
				return "git available", true
			}},
		}

		allPassed := true
		for _, c := range checks {
			msg, ok := c.check()
			status := "PASS"
			if !ok {
				status = "FAIL"
				allPassed = false
			}
			fmt.Printf("  [%s] %s: %s\n", status, c.name, msg)
		}

		if allPassed {
			fmt.Println("\nAll checks passed.")
		} else {
			fmt.Println("\nSome checks failed. Fix the issues above.")
		}
		return nil
	},
}

var loginCmd = &cobra.Command{
	Use:    "login",
	Short:  "Authenticate with Claude via OAuth",
	Hidden: true,
	RunE: func(cmd *cobra.Command, args []string) error {
		flow := oauth.NewPKCEFlow()

		authURL, err := flow.AuthorizationURL()
		if err != nil {
			return fmt.Errorf("generate authorization URL: %w", err)
		}

		fmt.Println("Starting Claude OAuth login...")
		fmt.Printf("Listening for callback on %s\n", flow.RedirectURI())

		if err := oauth.OpenBrowser(authURL); err != nil {
			fmt.Fprintf(os.Stderr, "warning: failed to open browser automatically: %v\n", err)
			fmt.Printf("Open this URL manually:\n%s\n", authURL)
		}

		callback, err := flow.WaitForCallback()
		if err != nil {
			return fmt.Errorf("wait for callback: %w", err)
		}

		if callback.Error != "" {
			desc := callback.ErrorDescription
			if desc == "" {
				desc = "authorization failed"
			}
			return fmt.Errorf("%s: %s", callback.Error, desc)
		}

		if callback.Code == "" {
			return fmt.Errorf("callback did not include authorization code")
		}

		if err := flow.ValidateState(callback.State); err != nil {
			return err
		}

		token, err := flow.Exchange(context.Background(), callback.Code)
		if err != nil {
			return fmt.Errorf("token exchange: %w", err)
		}

		if err := oauth.SaveCredentials(token); err != nil {
			return fmt.Errorf("save credentials: %w", err)
		}

		fmt.Println("Claude OAuth login complete.")
		return nil
	},
}

var logoutCmd = &cobra.Command{
	Use:    "logout",
	Short:  "Remove stored OAuth credentials",
	Hidden: true,
	RunE: func(cmd *cobra.Command, args []string) error {
		if err := oauth.ClearCredentials(); err != nil {
			return fmt.Errorf("clear credentials: %w", err)
		}
		fmt.Println("Claude OAuth credentials cleared.")
		return nil
	},
}

var healCmd = &cobra.Command{
	Use:   "heal",
	Short: "Self-healing commands and diagnostics",
	Long:  "Commands for viewing and controlling ycode's self-healing capabilities.",
}

var healStatusCmd = &cobra.Command{
	Use:   "status",
	Short: "Show self-healing status",
	Run: func(cmd *cobra.Command, args []string) {
		cfg := selfheal.DefaultConfig()

		fmt.Println("Self-Healing Status")
		fmt.Println("===================")
		fmt.Printf("Enabled:        %v\n", cfg.Enabled)
		fmt.Printf("Max Attempts:   %d\n", cfg.MaxAttempts)
		fmt.Printf("Auto Rebuild:   %v\n", cfg.AutoRebuild)
		fmt.Printf("Auto Restart:   %v\n", cfg.AutoRestart)
		fmt.Printf("Escalation:     %s\n", cfg.EscalationPolicy)
		fmt.Printf("Build Command:  %s\n", cfg.BuildCommand)
		fmt.Printf("Build Timeout:  %v\n", cfg.BuildTimeout)

		fmt.Println("\nHealable Paths:")
		for _, p := range cfg.HealablePaths {
			fmt.Printf("  - %s\n", p)
		}

		fmt.Println("\nProtected Paths:")
		for _, p := range cfg.ProtectedPaths {
			fmt.Printf("  - %s\n", p)
		}

		fmt.Println("\nEnvironment:")
		if selfHealEnabled() {
			fmt.Println("  YCODE_SELF_HEAL: enabled (set to '0' to disable)")
		} else {
			fmt.Println("  YCODE_SELF_HEAL: disabled")
		}
	},
}

var healTestCmd = &cobra.Command{
	Use:   "test [error-message]",
	Short: "Test self-healing with a simulated error",
	Args:  cobra.MinimumNArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		errMsg := strings.Join(args, " ")
		fmt.Printf("Testing self-healing with error: %s\n", errMsg)

		cfg := selfheal.DefaultConfig()
		healer := selfheal.NewHealer(cfg)

		simulatedErr := fmt.Errorf("%s", errMsg)
		canHeal := healer.CanHeal(simulatedErr)

		fmt.Printf("Error Type:    %s\n", selfheal.ClassifyError(simulatedErr))
		fmt.Printf("Can Heal:      %v\n", canHeal)

		if !canHeal {
			fmt.Println("\nThis error type is not healable.")
			return nil
		}

		// Attempt healing (without actually applying fixes)
		ctx := context.Background()
		errInfo := selfheal.ErrorInfo{
			Type:      selfheal.ClassifyError(simulatedErr),
			Error:     simulatedErr,
			Message:   errMsg,
			Timestamp: time.Now(),
		}

		fmt.Println("\nAttempting healing...")
		success, err := healer.AttemptHealing(ctx, errInfo)

		if err != nil {
			fmt.Printf("Healing error: %v\n", err)
		}
		fmt.Printf("Success: %v\n", success)

		return nil
	},
}

func init() {
	rootCmd.PersistentFlags().BoolVar(&printFlag, "print", false, "Output response as plain text (no markdown rendering)")
	rootCmd.PersistentFlags().BoolVar(&noStoreFlag, "no-store", false, "Run without persistent storage instead of failing when the data directory is locked (see YCODE_DATA_DIR)")
	rootCmd.PersistentFlags().BoolVar(&traceVerbose, "trace-verbose", false, "Capture full prompts in the per-turn action log (secrets still redacted; default logs prompt hashes only)")
	rootCmd.PersistentFlags().StringVar(&modelFlag, "model", "", "Model to use (overrides config and env vars)")
	rootCmd.PersistentFlags().BoolVar(&dangerSkipPermissions, "danger-skip-permissions", false, "Skip all permission checks (grants full access to all tools)")
	rootCmd.PersistentFlags().StringVar(&connectURL, "connect", "", "Connect to a remote ycode server (ws:// or nats://)")
	rootCmd.PersistentFlags().StringVar(&eventsFile, "events", "", "Write NDJSON event log to file")
	rootCmd.PersistentFlags().StringVar(&sessionDirFlag, "session-dir", "", "Directory for persisted sessions")
	rootCmd.PersistentFlags().StringVar(&forkSessionID, "fork", "", "Fork an existing session into a new session before running the prompt")
	rootCmd.PersistentFlags().StringVar(&resumeSessionID, "resume", "", "Resume an existing session before running the prompt")
	rootCmd.PersistentFlags().BoolVar(&noInteractiveFlag, "no-interactive", false, "Run non-interactively: skip TUI, trust prompts, and confirmations")
	rootCmd.PersistentFlags().BoolVar(&yesFlag, "yes", false, "Auto-confirm interactive prompts (alias for --no-interactive)")
	rootCmd.Flags().Bool("dry-run", false, "Preview session setup without calling the model")

	// no-otel: accepted for backward compatibility with integration tests (no-op).
	var noOtel bool
	rootCmd.PersistentFlags().BoolVar(&noOtel, "no-otel", false, "Disable OTEL instrumentation (no-op, kept for compatibility)")
	_ = rootCmd.PersistentFlags().MarkHidden("no-otel")

	doctorCmd.Flags().Bool("dry-run", false, "Quick readiness check without starting the full system")
	rootCmd.AddCommand(promptCmd, versionCmd, doctorCmd, loginCmd, logoutCmd)

	// Self-heal commands
	healCmd.AddCommand(healStatusCmd, healTestCmd)
	rootCmd.AddCommand(healCmd)

	// Model management commands
	rootCmd.AddCommand(newModelCmd())
	rootCmd.AddCommand(newSkillCmd())
	rootCmd.AddCommand(newConfigCmd())

	// Feature registry (build tiers — see docs/strategy.md#feature-tiers)
	rootCmd.AddCommand(newFeaturesCmd())

	rootCmd.AddCommand(newInitCmd())

	// `ycode docs` — agent-facing capability prompts (embedded). The
	// human-facing counterpart is `ycode help`; they cross-reference
	// but never share content. See internal/docs/embed.go safeguards.
	rootCmd.AddCommand(newDocsCmd())

	// `ycode memory` — operator surface for memex memory inspection.
	// Agent-callable equivalents live in the `yc remember` / `yc recall`
	// built-ins and the in-session memory_* tools; this command is the
	// human path.
	rootCmd.AddCommand(newMemoryCmd())

	// `ycode tools` — operator surface for "what tools does this binary
	// expose, to whom?". Covers the in-process tool registry and the
	// CLI surface. Operator complement
	// to `ycode docs` (which is curated for agents).
	rootCmd.AddCommand(newToolsCmd())

	// The browser feature has been fully removed from ycode — it now
	// lives in bashy (`bashy browser`, coreutils/pkg/browser). ycode no
	// longer registers browser_* tools or hosts a browser hub.

	// Interactive agentic shell (ycode shell)
	rootCmd.AddCommand(newShellCmd())

	// `ycode wrap <agent-cmd>` — PATH-shim involuntary interception for
	// foreign agentic CLIs (Claude Code, Codex, Aider, Gemini CLI,
	// opencode, ...). Complements the voluntary lighthouse beam that
	// `ycode init` writes into those tools' memory files.
	rootCmd.AddCommand(newWrapCmd())

	// `ycode internal-shell-trace` — hidden subcommand the Python
	// sitecustomize.py and Node ycode-trace.cjs hooks installed by
	// `ycode wrap` call into for parse+validate+trace of every
	// intercepted subprocess shell-out. Not user-facing.
	rootCmd.AddCommand(newShellTraceCmd())

	// Evaluation framework
	registerEvalCmd(rootCmd)
}
