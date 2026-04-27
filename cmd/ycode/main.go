package main

import (
	"context"
	"fmt"
	"io"
	"log/slog"
	"os"
	"os/exec"
	"os/signal"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"time"

	"github.com/google/uuid"
	"github.com/spf13/cobra"

	"github.com/qiangli/ycode/internal/api"
	"github.com/qiangli/ycode/internal/cli"
	"github.com/qiangli/ycode/internal/runtime/health"
	"github.com/qiangli/ycode/internal/commands"
	"github.com/qiangli/ycode/internal/inference"
	"github.com/qiangli/ycode/internal/runtime/config"
	"github.com/qiangli/ycode/internal/runtime/conversation"
	"github.com/qiangli/ycode/internal/runtime/embedding"
	"github.com/qiangli/ycode/internal/runtime/git"
	"github.com/qiangli/ycode/internal/runtime/indexer"
	"github.com/qiangli/ycode/internal/runtime/memory"
	"github.com/qiangli/ycode/internal/runtime/oauth"
	"github.com/qiangli/ycode/internal/runtime/permission"
	"github.com/qiangli/ycode/internal/runtime/prompt"
	"github.com/qiangli/ycode/internal/runtime/session"
	"github.com/qiangli/ycode/internal/runtime/vfs"
	"github.com/qiangli/ycode/internal/selfheal"
	"github.com/qiangli/ycode/internal/storage"
	"github.com/qiangli/ycode/internal/storage/kv"
	"github.com/qiangli/ycode/internal/storage/search"
	sqlstore "github.com/qiangli/ycode/internal/storage/sqlite"
	"github.com/qiangli/ycode/internal/storage/vector"
	"github.com/qiangli/ycode/internal/tools"
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

func main() {
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

// realMain contains the actual main logic.
// It returns errors that may be healable by the self-heal system.
func realMain() error {
	return rootCmd.Execute()
}

func newApp() (*cli.App, error) {
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
	projectDir := filepath.Join(cwd, ".agents", "ycode")

	// Load config.
	loader := config.NewLoader(userDir, projectDir, projectDir)
	cfg, err := loader.Load()
	if err != nil {
		return nil, fmt.Errorf("load config: %w", err)
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
	cfg.Model = api.ResolveModelWithAliases(model, cfg.Aliases)

	var provider api.Provider
	providerCfg, err := api.DetectProvider(cfg.Model)
	if err != nil {
		slog.Warn("no LLM provider available (agent features disabled until API key is configured)", "error", err)
	} else {
		provider = api.NewProvider(providerCfg)
	}
	// Initialize storage manager (Phase 1: KV store is instant).
	storageDataDir := filepath.Join(home, ".agents", "ycode", "projects", "data")
	storageMgr, err := storage.NewManager(context.Background(), storage.Config{
		DataDir: storageDataDir,
		KVFactory: func(ctx context.Context) (storage.KVStore, error) {
			return kv.Open(storageDataDir)
		},
		SQLFactory: func(ctx context.Context) (storage.SQLStore, error) {
			return sqlstore.Open(storageDataDir)
		},
		VectorFactory: func(ctx context.Context) (storage.VectorStore, error) {
			return vector.Open(storageDataDir)
		},
		SearchFactory: func(ctx context.Context) (storage.SearchIndex, error) {
			return search.Open(storageDataDir)
		},
	})
	if err != nil {
		return nil, fmt.Errorf("init storage: %w", err)
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
	instanceDir := filepath.Join(otelDataDir, "instances", instanceID)

	// Create session under the instance directory.
	sessionDir := cfg.SessionDir
	if sessionDir == "" {
		sessionDir = filepath.Join(instanceDir, "session")
	}
	sess, err := session.NewWithID(sessionDir, instanceID)
	if err != nil {
		return nil, fmt.Errorf("create session: %w", err)
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

	// Initialize tool registry with handlers.
	toolReg := tools.NewRegistry()
	tools.RegisterBuiltins(toolReg)
	tools.RegisterBashHandler(toolReg, cwd)
	tools.RegisterFileHandlers(toolReg, v)
	tools.RegisterSearchHandlers(toolReg, v)
	tools.RegisterVFSHandlers(toolReg, v)
	tools.RegisterSleepHandler(toolReg)
	tools.RegisterWebHandlers(toolReg)
	tools.RegisterToolSearchHandler(toolReg)
	tools.RegisterSkillHandler(toolReg)
	tools.RegisterMemosHandlers(toolReg)
	tools.SetMemoryManager(memManager)
	tools.RegisterMemoryHandlers(toolReg)
	tools.RegisterRemoteHandler(toolReg)
	tools.RegisterNotebookHandler(toolReg)
	tools.RegisterModeHandlers(toolReg, planMode)
	tools.RegisterConfigHandler(toolReg, cfg)
	tools.RegisterSemanticSearchHandler(toolReg)
	tools.RegisterGitHandlers(toolReg, cwd)

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

		// Set up Bleve fallback for Grep tool.
		tools.SetCodeSearchIndex(searchStore)

		// Attach session search indexer for compaction indexing.
		sessIndexer := session.NewSearchIndexer(searchStore, sess.ID)
		sess.SetSearchIndexer(sessIndexer)

		// Start background codebase indexer.
		codeIndexer := indexer.New(cwd, searchStore, storageMgr.KV())
		go codeIndexer.Run(rootCtx)
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
		metrics := tools.NewMetricsRecorder(sqlStore, sess.ID)
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
	}()

	// Start background memory consolidation (stale removal, dedup).
	go memory.NewDreamer(memManager, true).Start(rootCtx)

	// Wire OTEL observability.
	// Always-on: file-only mode persists traces/metrics/logs locally.
	// With Observability.Enabled: full mode adds gRPC export to collector.
	var otelRes *otelResult
	if cfg.Observability != nil && cfg.Observability.Enabled {
		otelRes = setupOTEL(cfg, sess, toolReg, provider, v)
	} else {
		otelRes = setupFileOTEL(cfg, sess, toolReg, provider, v)
	}
	var convOTEL *conversation.OTELConfig
	if otelRes != nil {
		convOTEL = otelRes.convOTEL
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
		OllamaLister:   inference.NewOllamaLister(),
	})
	if err != nil {
		return nil, err
	}

	// Register compact_context tool handler — needs the app for session access.
	tools.RegisterCompactContextHandler(toolReg, app.CompactContext)

	// Register cleanup (LIFO order — last registered runs first):
	// 1. rootCancel: stop background goroutines so they stop producing telemetry
	// 2. OTEL shutdown: flush remaining spans/metrics/logs
	if otelRes != nil {
		app.RegisterCleanup(otelRes.shutdown)
	}
	app.RegisterCleanup(rootCancel)

	success = true
	return app, nil
}

// buildPromptContext gathers environment and project metadata for the system prompt.
func buildPromptContext(workDir, model string, configInstructions []string, memManager *memory.Manager) *prompt.ProjectContext {
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

	// Discover instruction files and load memories concurrently.
	var contextFiles []prompt.ContextFile
	var memories []*memory.Memory
	var wg sync.WaitGroup

	wg.Add(1)
	go func() {
		defer wg.Done()
		contextFiles = discoverContextFiles(workDir, ctx.ProjectRoot)
	}()

	if memManager != nil {
		wg.Add(1)
		go func() {
			defer wg.Done()
			if mems, err := memManager.All(); err != nil {
				slog.Debug("memory load", "error", err)
			} else {
				memories = mems
			}
		}()
	}

	wg.Wait()
	ctx.ContextFiles = contextFiles
	ctx.Memories = memories

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
	modelFlag             string
	dangerSkipPermissions bool
	connectURL            string
)

var rootCmd = &cobra.Command{
	Use:   "ycode",
	Short: "ycode – autonomous agent harness for software development",
	Long:  "ycode is a CLI agent harness that provides 50+ tools, MCP/LSP integration, a plugin system, permission enforcement, and session management.",
	RunE: func(cmd *cobra.Command, args []string) error {
		// Dry-run mode: preview session setup without calling the model.
		if dryRun, _ := cmd.Flags().GetBool("dry-run"); dryRun {
			report := health.NewReadinessReport()
			// Check provider.
			if os.Getenv("ANTHROPIC_API_KEY") != "" || os.Getenv("OPENAI_API_KEY") != "" {
				report.Add("provider", health.StatusReady, "API key found")
			} else {
				report.Add("provider", health.StatusBlocked, "No API key (set ANTHROPIC_API_KEY or OPENAI_API_KEY)")
			}
			// Check working directory.
			if _, err := os.Getwd(); err == nil {
				report.Add("workspace", health.StatusReady, "Working directory accessible")
			}
			fmt.Print(report.Format())
			return nil
		}

		// Remote mode: connect to a running ycode server.
		if connectURL != "" {
			return runRemoteTUI(connectURL)
		}

		// Check for piped input.
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
			if printFlag {
				app.SetPrintMode(true)
			}
			return app.RunPrompt(context.Background(), prompt)
		}

		app, err := newApp()
		if err != nil {
			return err
		}
		defer app.Close()
		return app.RunInteractive(context.Background())
	},
}

var promptCmd = &cobra.Command{
	Use:   "prompt [message]",
	Short: "Send a one-shot prompt to the agent",
	Args:  cobra.MinimumNArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		app, err := newApp()
		if err != nil {
			return err
		}
		defer app.Close()
		if printFlag {
			app.SetPrintMode(true)
		}
		prompt := strings.Join(args, " ")
		return app.RunPrompt(context.Background(), prompt)
	},
}

var versionCmd = &cobra.Command{
	Use:   "version",
	Short: "Print version information",
	Run: func(cmd *cobra.Command, args []string) {
		fmt.Printf("ycode %s (%s)\n", version, commit)
	},
}

var loopCmd = &cobra.Command{
	Use:   "loop",
	Short: "Run agent in continuous loop mode",
	RunE: func(cmd *cobra.Command, args []string) error {
		intervalStr, _ := cmd.Flags().GetString("interval")
		promptFile, _ := cmd.Flags().GetString("prompt")

		if intervalStr == "" {
			intervalStr = "10m"
		}

		if promptFile == "" {
			return fmt.Errorf("--prompt flag is required (path to prompt file)")
		}

		interval, err := time.ParseDuration(intervalStr)
		if err != nil {
			return fmt.Errorf("invalid interval %q: %w", intervalStr, err)
		}

		content, err := os.ReadFile(promptFile)
		if err != nil {
			return fmt.Errorf("read prompt file: %w", err)
		}

		app, err := newApp()
		if err != nil {
			return err
		}
		defer app.Close()

		fmt.Printf("Starting loop: every %s with prompt from %s\n", interval, promptFile)
		fmt.Println("Press Ctrl+C to stop.")

		ctx, cancel := context.WithCancel(context.Background())
		defer cancel()

		// Handle signals for graceful shutdown.
		go func() {
			sigCh := make(chan os.Signal, 1)
			signal.Notify(sigCh, os.Interrupt)
			<-sigCh
			fmt.Println("\nStopping loop...")
			cancel()
		}()

		iteration := 0
		for {
			select {
			case <-ctx.Done():
				return nil
			default:
			}

			iteration++
			fmt.Printf("\n--- Iteration %d ---\n", iteration)

			// Re-read prompt file each iteration to pick up changes.
			if data, err := os.ReadFile(promptFile); err == nil {
				content = data
			}

			if err := app.RunPrompt(ctx, string(content)); err != nil {
				if ctx.Err() != nil {
					return nil
				}
				fmt.Fprintf(os.Stderr, "Error: %v\n", err)
			}

			select {
			case <-time.After(interval):
			case <-ctx.Done():
				return nil
			}
		}
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
	Use:   "login",
	Short: "Authenticate with Claude via OAuth",
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
	Use:   "logout",
	Short: "Remove stored OAuth credentials",
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
	rootCmd.PersistentFlags().StringVar(&modelFlag, "model", "", "Model to use (overrides config and env vars)")
	rootCmd.PersistentFlags().BoolVar(&dangerSkipPermissions, "danger-skip-permissions", false, "Skip all permission checks (grants full access to all tools)")
	rootCmd.PersistentFlags().StringVar(&connectURL, "connect", "", "Connect to a remote ycode server (ws:// or nats://)")
	rootCmd.Flags().Bool("dry-run", false, "Preview session setup without calling the model")

	// no-otel: accepted for backward compatibility with integration tests (no-op).
	var noOtel bool
	rootCmd.PersistentFlags().BoolVar(&noOtel, "no-otel", false, "Disable OTEL instrumentation (no-op, kept for compatibility)")
	_ = rootCmd.PersistentFlags().MarkHidden("no-otel")

	loopCmd.Flags().String("interval", "10m", "Loop interval (e.g., 5m, 1h)")
	loopCmd.Flags().String("prompt", "", "Path to prompt file")
	doctorCmd.Flags().Bool("dry-run", false, "Quick readiness check without starting the full system")
	rootCmd.AddCommand(promptCmd, versionCmd, doctorCmd, loopCmd, loginCmd, logoutCmd)

	// Self-heal commands
	healCmd.AddCommand(healStatusCmd, healTestCmd)
	rootCmd.AddCommand(healCmd)

	// Model management commands
	rootCmd.AddCommand(newModelCmd())

	// Container management commands (podman/docker)
	rootCmd.AddCommand(newPodmanCmd())

	// Batch processing
	rootCmd.AddCommand(newBatchCmd())

	// Training and evaluation
	rootCmd.AddCommand(newTrainCmd())

	// Autonomous agent mesh
	rootCmd.AddCommand(newMeshCmd())

	// Evaluation framework
	registerEvalCmd(rootCmd)
}
