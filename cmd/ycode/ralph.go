package main

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"strings"

	"github.com/spf13/cobra"

	"github.com/qiangli/ycode/internal/api"
	"github.com/qiangli/ycode/internal/runtime/bash"
	"github.com/qiangli/ycode/internal/runtime/config"
	"github.com/qiangli/ycode/internal/runtime/memory"
	"github.com/qiangli/ycode/internal/runtime/permission"
	"github.com/qiangli/ycode/internal/runtime/ralph"
	"github.com/qiangli/ycode/internal/runtime/vfs"
	"github.com/qiangli/ycode/internal/tools"
)

func newRalphCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "ralph [prompt]",
		Short: "Run the Ralph autonomous loop (step → check → commit → repeat)",
		Long: `Ralph is an autonomous iterative agent loop backed by the full conversation
runtime. Each iteration has access to all ycode tools (file ops, bash, search,
git, memory, web) and runs with fresh context to prevent context bloat.

  1. Executes a step (full agentic loop with tools)
  2. Runs a check command (e.g., go test ./...)
  3. Commits on success (optional)
  4. Persists learnings to memory
  5. Repeats until target score reached or max iterations

Eval-driven termination, stagnation detection, and automatic checkpointing.`,
		Args: cobra.MinimumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			userPrompt := strings.Join(args, " ")
			maxIter, _ := cmd.Flags().GetInt("max-iterations")
			targetScore, _ := cmd.Flags().GetFloat64("target-score")
			checkCmd, _ := cmd.Flags().GetString("check")
			commitOnSuccess, _ := cmd.Flags().GetBool("commit")
			commitMsg, _ := cmd.Flags().GetString("commit-message")
			timeout, _ := cmd.Flags().GetDuration("timeout")
			prdPath, _ := cmd.Flags().GetString("prd")

			// Build runtime dependencies.
			deps, err := newRalphDeps(cmd)
			if err != nil {
				return fmt.Errorf("initialize ralph: %w", err)
			}

			// Configure Ralph loop.
			cfg := ralph.DefaultConfig()
			cfg.MaxIterations = maxIter
			cfg.TargetScore = targetScore
			cfg.CommitOnSuccess = commitOnSuccess
			cfg.CommitMessage = commitMsg
			cfg.Timeout = timeout
			cfg.PRDPath = prdPath
			cfg.FreshContext = true
			cfg.ProgressDir = filepath.Join(deps.SessionDir, "progress")

			// Set up progress log.
			var progressLog *ralph.ProgressLog
			if cfg.ProgressDir != "" {
				if err := os.MkdirAll(cfg.ProgressDir, 0o755); err != nil {
					return fmt.Errorf("create progress dir: %w", err)
				}
				progressLog = ralph.NewProgressLog(filepath.Join(cfg.ProgressDir, "progress.txt"))
			}

			// Load PRD for story-driven mode.
			var prd *ralph.PRD
			if prdPath != "" {
				prd, err = ralph.LoadPRD(prdPath)
				if err != nil {
					slog.Warn("ralph: failed to load PRD, running without story tracking", "error", err)
				}
			}

			// Create the runtime-backed step function.
			stepFunc := ralph.NewRuntimeStepFunc(&ralph.RuntimeStepConfig{
				Deps:        deps,
				UserPrompt:  userPrompt,
				ProgressLog: progressLog,
				StoryProvider: func() *ralph.Story {
					if prd != nil {
						return prd.NextStory()
					}
					return nil
				},
			})

			ctrl := ralph.NewController(cfg, stepFunc)

			// Wire check function.
			if checkCmd != "" {
				ctrl.SetCheck(ralph.NewBashCheckFunc(checkCmd))
			}

			// Wire commit function.
			cwd, _ := os.Getwd()
			if commitOnSuccess {
				ctrl.SetCommit(ralph.NewGitCommitFunc(cwd))
			}

			// Wire memory persistence: save learnings after each iteration.
			ctrl.SetOnIterationComplete(func(iteration int, output string, _ float64, _ bool) {
				if deps.MemoryManager == nil {
					return
				}
				storyID := ""
				if prd != nil {
					if s := prd.CurrentStory(); s != nil {
						storyID = s.ID
					}
				}
				ralph.SaveIterationMemory(deps.MemoryManager, iteration, storyID, output)
			})

			slog.Info("ralph: starting autonomous loop",
				"prompt", userPrompt,
				"max_iterations", maxIter,
				"check", checkCmd,
				"commit", commitOnSuccess,
				"prd", prdPath,
			)

			return ctrl.Run(cmd.Context())
		},
	}

	cmd.Flags().String("model", "sonnet", "Model to use")
	cmd.Flags().Int("max-iterations", 10, "Maximum iterations")
	cmd.Flags().Float64("target-score", 0, "Target score to stop (0 = disabled)")
	cmd.Flags().String("check", "", "Check command to run after each step (e.g., 'go test ./...')")
	cmd.Flags().Bool("commit", false, "Auto-commit on success")
	cmd.Flags().String("commit-message", "ralph: automated iteration", "Commit message template")
	cmd.Flags().Duration("timeout", 0, "Overall timeout (0 = no timeout)")
	cmd.Flags().String("prd", "", "Path to prd.json for story-driven mode")

	return cmd
}

// newRalphDeps sets up the runtime dependencies needed for Ralph iterations.
// This is a lighter-weight setup than newApp() — it creates the config, provider,
// tool registry, prompt context, and memory manager without the full TUI/session stack.
func newRalphDeps(cmd *cobra.Command) (*ralph.RuntimeDeps, error) {
	home, _ := os.UserHomeDir()
	cwd, _ := os.Getwd()

	// Load config.
	userDir := filepath.Join(home, ".config", "ycode")
	projectDir := filepath.Join(cwd, ".agents", "ycode")
	loader := config.NewLoader(userDir, projectDir, projectDir)
	cfg, err := loader.Load()
	if err != nil {
		return nil, fmt.Errorf("load config: %w", err)
	}

	// Resolve model.
	model, _ := cmd.Flags().GetString("model")
	if model == "" {
		model = "sonnet"
	}
	cfg.Model = api.ResolveModelWithAliases(model, cfg.Aliases)

	// Create provider.
	providerCfg, err := api.DetectProvider(cfg.Model)
	if err != nil {
		return nil, fmt.Errorf("detect provider: %w", err)
	}
	provider := api.NewProvider(providerCfg)

	// Create memory manager.
	globalMemDir := filepath.Join(home, ".agents", "ycode", "memory")
	memoryDir := filepath.Join(cwd, ".agents", "ycode", "memory")
	memManager, err := memory.NewManagerWithGlobal(globalMemDir, memoryDir)
	if err != nil {
		return nil, fmt.Errorf("create memory manager: %w", err)
	}

	// Build VFS.
	allowedDirs := []string{os.TempDir(), cwd}
	allowedDirs = append(allowedDirs, cfg.AllowedDirectories...)
	v, err := vfs.New(allowedDirs, nil)
	if err != nil {
		return nil, fmt.Errorf("create vfs: %w", err)
	}

	// Create tool registry with essential handlers.
	toolReg := tools.NewRegistry()
	tools.RegisterBuiltins(toolReg)

	// Register tool handlers.
	jobRegistry := bash.NewJobRegistry()
	tools.RegisterBashHandler(toolReg, cwd, jobRegistry)
	tools.RegisterFileHandlers(toolReg, v)
	tools.RegisterSearchHandlers(toolReg, v)
	tools.RegisterVFSHandlers(toolReg, v)
	tools.RegisterWebHandlers(toolReg)
	tools.RegisterToolSearchHandler(toolReg)
	tools.RegisterSkillHandler(toolReg)
	tools.SetMemoryManager(memManager)
	tools.RegisterMemoryHandlers(toolReg)
	tools.RegisterGitHandlers(toolReg, &tools.GitToolsDeps{WorkDir: cwd})
	tools.RegisterNotebookHandler(toolReg, v)

	// Set permission mode to full access for autonomous operation.
	toolReg.SetPermissionResolver(func() permission.Mode {
		if dangerSkipPermissions {
			return permission.DangerFullAccess
		}
		return permission.ParseMode(cfg.PermissionMode)
	})

	// Auto-approve tool permissions in Ralph mode (non-interactive).
	toolReg.SetPermissionPrompter(func(_ context.Context, toolName string, requiredMode permission.Mode) (bool, error) {
		slog.Info("ralph: auto-approving tool", "tool", toolName, "mode", requiredMode)
		return true, nil
	})

	// Build prompt context.
	promptCtx := buildPromptContext(cwd, cfg.Model, cfg.Instructions, memManager)
	promptCtx.AllowedDirs = v.AllowedDirs()

	// Session directory for Ralph iterations.
	sessionDir := filepath.Join(home, ".agents", "ycode", "ralph-sessions")

	return &ralph.RuntimeDeps{
		Config:        cfg,
		Provider:      provider,
		Registry:      toolReg,
		PromptCtx:     promptCtx,
		MemoryManager: memManager,
		SessionDir:    sessionDir,
		Logger:        slog.Default(),
	}, nil
}
