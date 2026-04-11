package main

import (
	"context"
	"fmt"
	"io"
	"os"
	"os/exec"
	"os/signal"
	"path/filepath"
	"runtime"
	"strings"
	"time"

	"github.com/spf13/cobra"

	"github.com/qiangli/ycode/internal/api"
	"github.com/qiangli/ycode/internal/cli"
	"github.com/qiangli/ycode/internal/commands"
	"github.com/qiangli/ycode/internal/runtime/config"
	"github.com/qiangli/ycode/internal/runtime/git"
	"github.com/qiangli/ycode/internal/runtime/oauth"
	"github.com/qiangli/ycode/internal/runtime/permission"
	"github.com/qiangli/ycode/internal/runtime/prompt"
	"github.com/qiangli/ycode/internal/runtime/session"
	"github.com/qiangli/ycode/internal/selfheal"
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
	// Determine config directories.
	home, _ := os.UserHomeDir()
	userDir := filepath.Join(home, ".config", "ycode")
	cwd, _ := os.Getwd()
	projectDir := filepath.Join(cwd, ".ycode")

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

	providerCfg, err := api.DetectProvider(cfg.Model)
	if err != nil {
		return nil, err
	}
	provider := api.NewProvider(providerCfg)

	// Create session.
	sessionDir := cfg.SessionDir
	if sessionDir == "" {
		dataDir := filepath.Join(home, ".local", "share", "ycode", "sessions")
		sessionDir = dataDir
	}
	sess, err := session.New(sessionDir)
	if err != nil {
		return nil, fmt.Errorf("create session: %w", err)
	}

	// Memory directory for persistent memories.
	memoryDir := filepath.Join(home, ".ycode", "projects", "memory")

	// Plan mode manager.
	ycodeDir := filepath.Join(cwd, ".ycode")
	planMode := tools.NewPlanModeManager(ycodeDir)

	// Initialize tool registry with handlers.
	toolReg := tools.NewRegistry()
	tools.RegisterBuiltins(toolReg)
	tools.RegisterBashHandler(toolReg, cwd)
	tools.RegisterFileHandlers(toolReg, cwd)
	tools.RegisterSearchHandlers(toolReg)
	tools.RegisterSleepHandler(toolReg)
	tools.RegisterWebHandlers(toolReg)
	tools.RegisterToolSearchHandler(toolReg)
	tools.RegisterSkillHandler(toolReg)
	tools.RegisterRemoteHandler(toolReg)
	tools.RegisterNotebookHandler(toolReg)
	tools.RegisterModeHandlers(toolReg, planMode)
	tools.RegisterConfigHandler(toolReg, cfg)

	// Wire permission enforcement: resolve current mode from the live
	// settings.local.json file so that plan mode toggles take effect immediately.
	localConfigPath := filepath.Join(ycodeDir, "settings.local.json")
	toolReg.SetPermissionResolver(func() permission.Mode {
		// Check local override first (plan mode writes here).
		if val, ok := config.GetLocalConfigField(localConfigPath, "permissionMode"); ok {
			if s, ok := val.(string); ok {
				return permission.ParseMode(s)
			}
		}
		// Fall back to in-memory config.
		return permission.ParseMode(cfg.PermissionMode)
	})

	// Build project context for system prompt.
	promptCtx := buildPromptContext(cwd, cfg.Model)

	return cli.NewApp(cfg, provider, sess, cli.AppOptions{
		WorkDir:      cwd,
		ProviderKind: providerCfg.DisplayKind(),
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
	})
}

// buildPromptContext gathers environment and project metadata for the system prompt.
func buildPromptContext(workDir, model string) *prompt.ProjectContext {
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
		ctx.GitBranch = gitCtx.Branch
		ctx.MainBranch = gitCtx.MainBranch
		ctx.GitUser = gitCtx.User
		ctx.GitStatus = gitCtx.Status
		ctx.RecentCommits = gitCtx.RecentCommits
	}

	// Instruction files (CLAUDE.md, .ycode/instructions.md, etc.).
	ctx.ContextFiles = discoverContextFiles(workDir)

	return ctx
}

// discoverContextFiles finds and loads instruction files.
func discoverContextFiles(workDir string) []prompt.ContextFile {
	discovered := prompt.DiscoverInstructionFiles(workDir)
	var files []prompt.ContextFile
	for _, f := range discovered {
		files = append(files, prompt.ContextFile{
			Path:    f.Path,
			Content: f.Content,
		})
	}
	return files
}

var (
	printFlag bool
	modelFlag string
)

var rootCmd = &cobra.Command{
	Use:   "ycode",
	Short: "ycode – autonomous agent harness for software development",
	Long:  "ycode is a CLI agent harness that provides 50+ tools, MCP/LSP integration, a plugin system, permission enforcement, and session management.",
	RunE: func(cmd *cobra.Command, args []string) error {
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
			if printFlag {
				app.SetPrintMode(true)
			}
			return app.RunPrompt(context.Background(), prompt)
		}

		app, err := newApp()
		if err != nil {
			return err
		}
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
	loopCmd.Flags().String("interval", "10m", "Loop interval (e.g., 5m, 1h)")
	loopCmd.Flags().String("prompt", "", "Path to prompt file")
	rootCmd.AddCommand(promptCmd, versionCmd, doctorCmd, loopCmd, loginCmd, logoutCmd)

	// Self-heal commands
	healCmd.AddCommand(healStatusCmd, healTestCmd)
	rootCmd.AddCommand(healCmd)
}
