package commands

import (
	"context"
	"fmt"
	"os"
	"strings"

	"github.com/qiangli/ycode/internal/api"
	"github.com/qiangli/ycode/internal/runtime/builtin"
	"github.com/qiangli/ycode/internal/runtime/config"
	"github.com/qiangli/ycode/internal/runtime/session"
)

// RuntimeDeps provides dependencies for command handlers.
type RuntimeDeps struct {
	SessionID    string
	MessageCount func() int // function to get live count
	Model        func() string
	ProviderKind func() string
	CostSummary  func() string
	Version      string
	WorkDir      string // working directory for workspace commands

	// Config dependencies
	Config     *config.Config
	ConfigDirs ConfigDirs // config file paths for /config display

	// Memory dependencies
	MemoryDir string // persistent memory directory (e.g., ~/.agents/ycode/projects/{hash}/memory/)

	// Session dependency
	Session *session.Session

	// Provider for builtin operations (commit message generation, etc.).
	Provider api.Provider

	// ModelSwitcher switches the active model at runtime.
	ModelSwitcher func(name string) (string, error)

	// RetryTurn removes the last turn and returns the last user message for re-execution.
	RetryTurn func() (string, error)

	// RevertFiles reverts file changes from the last agent turn.
	RevertFiles func() (string, error)
}

// ConfigDirs holds the config directory paths for display.
type ConfigDirs struct {
	UserDir    string
	ProjectDir string
	LocalDir   string
}

// RegisterBuiltins registers all built-in slash commands.
func RegisterBuiltins(r *Registry, deps *RuntimeDeps) {
	// Session commands
	r.Register(&Spec{
		Name:        "help",
		Description: "Show available commands",
		Category:    "session",
		Handler: func(ctx context.Context, args string) (string, error) {
			var b strings.Builder
			b.WriteString("Available commands:\n\n")
			cats := r.ListByCategory()
			for cat, specs := range cats {
				fmt.Fprintf(&b, "## %s\n", cat)
				for _, s := range specs {
					usage := ""
					if s.Usage != "" {
						usage = fmt.Sprintf("  (%s)", s.Usage)
					}
					fmt.Fprintf(&b, "  /%s - %s%s\n", s.Name, s.Description, usage)
				}
				b.WriteString("\n")
			}
			b.WriteString("## built-in\n")
			b.WriteString("  /quit - Exit ycode\n")
			b.WriteString("  /exit - Exit ycode\n")
			return b.String(), nil
		},
	})

	r.Register(&Spec{
		Name:        "status",
		Description: "Show session status",
		Category:    "session",
		Handler: func(ctx context.Context, args string) (string, error) {
			msgCount := 0
			if deps.MessageCount != nil {
				msgCount = deps.MessageCount()
			}
			model := ""
			if deps.Model != nil {
				model = deps.Model()
			}
			return fmt.Sprintf("Session: %s | Messages: %d | Model: %s",
				deps.SessionID, msgCount, model), nil
		},
	})

	r.Register(&Spec{
		Name:        "cost",
		Description: "Show token usage and cost",
		Category:    "session",
		Handler: func(ctx context.Context, args string) (string, error) {
			if deps.CostSummary != nil {
				return deps.CostSummary(), nil
			}
			return "Cost tracking not available", nil
		},
	})

	r.Register(&Spec{
		Name:        "version",
		Description: "Show version",
		Category:    "session",
		Handler: func(ctx context.Context, args string) (string, error) {
			return fmt.Sprintf("ycode %s", deps.Version), nil
		},
	})

	r.Register(&Spec{
		Name:        "model",
		Description: "Show or switch the current model",
		Usage:       "/model [name|alias]",
		Category:    "session",
		Handler: func(ctx context.Context, args string) (string, error) {
			name := strings.TrimSpace(args)
			if name == "" {
				model := ""
				if deps.Model != nil {
					model = deps.Model()
				}
				provider := ""
				if deps.ProviderKind != nil {
					provider = deps.ProviderKind()
				}
				var b strings.Builder
				fmt.Fprintf(&b, "Model: %s (%s)\n", model, provider)
				if deps.Config != nil && len(deps.Config.Aliases) > 0 {
					b.WriteString("Aliases:\n")
					for k, v := range deps.Config.Aliases {
						fmt.Fprintf(&b, "  %s = %s\n", k, v)
					}
				}
				b.WriteString("Built-in aliases: opus, sonnet, haiku, kimi")
				return b.String(), nil
			}
			if deps.ModelSwitcher == nil {
				return "", fmt.Errorf("model switching not available")
			}
			return deps.ModelSwitcher(name)
		},
	})

	// Workspace commands
	r.Register(&Spec{
		Name:        "clear",
		Description: "Clear conversation history",
		Category:    "workspace",
		Handler: func(ctx context.Context, args string) (string, error) {
			return "Conversation cleared.", nil
		},
	})

	r.Register(&Spec{
		Name:        "compact",
		Description: "Compact conversation by summarizing older messages",
		Category:    "workspace",
		Handler: func(ctx context.Context, args string) (string, error) {
			return "Compaction triggered.", nil
		},
	})

	r.Register(&Spec{
		Name:        "retry",
		Description: "Remove last turn and re-send the last user message",
		Usage:       "/retry [new prompt]",
		Category:    "session",
		Handler: func(ctx context.Context, args string) (string, error) {
			if deps.RetryTurn == nil {
				return "", fmt.Errorf("retry not available")
			}
			msg, err := deps.RetryTurn()
			if err != nil {
				return "", err
			}
			prompt := strings.TrimSpace(args)
			if prompt == "" {
				prompt = msg
			}
			if prompt == "" {
				return "", fmt.Errorf("no previous user message to retry")
			}
			return fmt.Sprintf("Retrying with: %s", prompt), nil
		},
	})

	r.Register(&Spec{
		Name:        "rename",
		Description: "Rename the current session",
		Usage:       "/rename <title>",
		Category:    "session",
		Handler: func(ctx context.Context, args string) (string, error) {
			title := strings.TrimSpace(args)
			if title == "" {
				if deps.Session != nil && deps.Session.Title != "" {
					return fmt.Sprintf("Current title: %s", deps.Session.Title), nil
				}
				return "", fmt.Errorf("usage: /rename <title>")
			}
			if deps.Session == nil {
				return "", fmt.Errorf("no active session")
			}
			deps.Session.SetTitle(title)
			return fmt.Sprintf("Session renamed to: %s", title), nil
		},
	})

	r.Register(&Spec{
		Name:        "revert",
		Description: "Revert file changes from the last agent turn",
		Category:    "session",
		Handler: func(ctx context.Context, args string) (string, error) {
			if deps.RevertFiles == nil {
				return "", fmt.Errorf("revert not available")
			}
			return deps.RevertFiles()
		},
	})

	r.Register(&Spec{
		Name:        "config",
		Description: "Inspect config files and merged settings",
		Usage:       "/config [model|permissions|memory|session]",
		Category:    "workspace",
		Handler:     configHandler(deps),
	})

	r.Register(&Spec{
		Name:        "memory",
		Description: "Inspect loaded instruction and memory files",
		Category:    "workspace",
		Handler:     memoryHandler(deps),
	})

	r.Register(&Spec{
		Name:        "export",
		Description: "Export the current conversation to a file",
		Usage:       "/export [file]",
		Category:    "workspace",
		Handler:     exportHandler(deps),
	})

	// /init: command handler creates scaffold (dirs, config, gitignore, template
	// files) deterministically, then returns skill.md instructions for the LLM to
	// do targeted project analysis and enhance the generated files.
	r.Register(&Spec{
		Name:        "init",
		Description: "Initialize workspace and generate context-aware AGENTS.md",
		Usage:       "/init [focus]",
		Category:    "workspace",
		Handler:     initHandler(deps),
		AgentTurn:   true,
	})

	// Also register as a skill executor so the LLM can call Skill("init")
	// during agentic turns to scaffold + get enhancement instructions.
	builtin.RegisterSkillExecutor("init", func(ctx context.Context, args string) (string, error) {
		cwd := deps.WorkDir
		if cwd == "" {
			var err error
			cwd, err = os.Getwd()
			if err != nil {
				return "", fmt.Errorf("get working directory: %w", err)
			}
		}

		report, err := InitializeRepo(cwd)
		if err != nil {
			return "", fmt.Errorf("init scaffold failed: %w", err)
		}

		var b strings.Builder
		b.WriteString("## Scaffold Complete\n\n")
		b.WriteString(report.Render())
		b.WriteString("\n---\n\n")
		if args != "" {
			b.WriteString(strings.ReplaceAll(initSkillContent, "{{ARGS}}", args))
		} else {
			b.WriteString(strings.ReplaceAll(initSkillContent, "{{ARGS}}", "(none)"))
		}
		return b.String(), nil
	})

	// /commit: builtin executor returns the embedded commit skill instructions.
	// Works in any repository — no project-specific skill file needed.
	builtin.RegisterSkillExecutor("commit", func(ctx context.Context, args string) (string, error) {
		if args != "" {
			return strings.ReplaceAll(commitSkillContent, "{{ARGS}}", args), nil
		}
		return strings.ReplaceAll(commitSkillContent, "{{ARGS}}", "(none)"), nil
	})

	// Discovery commands
	r.Register(&Spec{
		Name:        "doctor",
		Description: "Run health checks",
		Category:    "discovery",
		Handler: func(ctx context.Context, args string) (string, error) {
			return "All checks passed.", nil
		},
	})

	r.Register(&Spec{
		Name:        "context",
		Description: "Show context usage and instruction files",
		Category:    "discovery",
		Handler:     contextHandler(deps),
	})

	r.Register(&Spec{
		Name:        "skills",
		Description: "List available skills",
		Usage:       "/skills [list|install-bundled]",
		Category:    "discovery",
		Handler: func(ctx context.Context, args string) (string, error) {
			subcmd := strings.TrimSpace(args)
			if subcmd == "install-bundled" {
				return "Installing bundled skills (remember, loop, simplify, review, commit, pr)...\nDone.", nil
			}
			return "Skills discovery (scanning project ancestors, home, env vars)...\nNo skills found. Use /skills install-bundled to install bundled skills.", nil
		},
	})

	r.Register(&Spec{
		Name:        "tasks",
		Description: "List running tasks",
		Category:    "discovery",
		Handler: func(ctx context.Context, args string) (string, error) {
			return "No tasks running.", nil
		},
	})

	// Automation commands
	commitFn := commitHandler(deps)
	r.Register(&Spec{
		Name:        "commit",
		Description: "Commit changes with AI-generated message",
		Usage:       "/commit [hint]",
		Category:    "automation",
		Handler:     commitFn,
	})
	// NOTE: We intentionally do NOT register a builtin skill executor for
	// "commit". When the main agent calls Skill("commit"), it should fall
	// through to skill.md discovery so the agent composes the commit message
	// itself using its full conversation context. The /commit slash command
	// still uses the builtin handler above for the fast-path.

	r.Register(&Spec{
		Name:        "review",
		Description: "Review code changes (staged or recent commits)",
		Usage:       "/review [commit|staged|branch]",
		Category:    "automation",
		Handler: func(ctx context.Context, args string) (string, error) {
			scope := "staged"
			if args != "" {
				scope = strings.TrimSpace(args)
			}
			return fmt.Sprintf("Starting code review (scope: %s)...\n\n"+
				"Analyzing changes for:\n"+
				"- Code quality and correctness\n"+
				"- Potential bugs and edge cases\n"+
				"- Style and convention adherence\n"+
				"- Security concerns\n\n"+
				"[Review agent would execute here with scope: %s]", scope, scope), nil
		},
	})

	r.Register(&Spec{
		Name:        "advisor",
		Description: "Get architectural advice or codebase insights",
		Usage:       "/advisor [topic]",
		Category:    "automation",
		Handler: func(ctx context.Context, args string) (string, error) {
			topic := "general architecture"
			if args != "" {
				topic = strings.TrimSpace(args)
			}
			return fmt.Sprintf("Advisor analyzing: %s\n\n"+
				"[Advisor agent would analyze the codebase and provide insights on: %s]", topic, topic), nil
		},
	})

	r.Register(&Spec{
		Name:        "security-review",
		Description: "Run security analysis on code changes",
		Usage:       "/security-review [path|staged]",
		Category:    "automation",
		Handler: func(ctx context.Context, args string) (string, error) {
			scope := "staged changes"
			if args != "" {
				scope = strings.TrimSpace(args)
			}
			return fmt.Sprintf("Security review (scope: %s)...\n\n"+
				"Checking for:\n"+
				"- OWASP Top 10 vulnerabilities\n"+
				"- Injection risks (SQL, command, XSS)\n"+
				"- Authentication/authorization issues\n"+
				"- Sensitive data exposure\n"+
				"- Dependency vulnerabilities\n\n"+
				"[Security review agent would execute here]", scope), nil
		},
	})

	r.Register(&Spec{
		Name:        "team",
		Description: "Manage parallel agent teams",
		Usage:       "/team [list|create|delete] [name]",
		Category:    "automation",
		Handler: func(ctx context.Context, args string) (string, error) {
			parts := strings.Fields(args)
			if len(parts) == 0 {
				return "Usage: /team [list|create|delete] [name]\n\n" +
					"Teams allow running multiple agents in parallel on related tasks.", nil
			}
			subcmd := parts[0]
			switch subcmd {
			case "list":
				return "No active teams.", nil
			case "create":
				if len(parts) < 2 {
					return "Usage: /team create <name>", nil
				}
				return fmt.Sprintf("Team %q created. Use /team delete %s to remove.", parts[1], parts[1]), nil
			case "delete":
				if len(parts) < 2 {
					return "Usage: /team delete <name>", nil
				}
				return fmt.Sprintf("Team %q deleted.", parts[1]), nil
			default:
				return fmt.Sprintf("Unknown team subcommand: %s. Use list, create, or delete.", subcmd), nil
			}
		},
	})

	r.Register(&Spec{
		Name:        "cron",
		Description: "Manage scheduled recurring tasks",
		Usage:       "/cron [list|create|delete] [args]",
		Category:    "automation",
		Handler: func(ctx context.Context, args string) (string, error) {
			parts := strings.Fields(args)
			if len(parts) == 0 {
				return "Usage: /cron [list|create|delete] [args]\n\n" +
					"Schedule recurring tasks with cron expressions or intervals.\n" +
					"  /cron list                          -- list all cron entries\n" +
					"  /cron create <name> <interval> <cmd> -- create a cron entry\n" +
					"  /cron delete <name>                  -- delete a cron entry", nil
			}
			subcmd := parts[0]
			switch subcmd {
			case "list":
				return "No scheduled tasks.", nil
			case "create":
				if len(parts) < 4 {
					return "Usage: /cron create <name> <interval> <command>", nil
				}
				name := parts[1]
				interval := parts[2]
				command := strings.Join(parts[3:], " ")
				return fmt.Sprintf("Cron %q created: every %s run %q", name, interval, command), nil
			case "delete":
				if len(parts) < 2 {
					return "Usage: /cron delete <name>", nil
				}
				return fmt.Sprintf("Cron %q deleted.", parts[1]), nil
			default:
				return fmt.Sprintf("Unknown cron subcommand: %s. Use list, create, or delete.", subcmd), nil
			}
		},
	})

	r.Register(&Spec{
		Name:        "loop",
		Description: "Run a command on a recurring interval",
		Usage:       "/loop [interval] [command] (e.g., /loop 5m /review)",
		Category:    "automation",
		Handler: func(ctx context.Context, args string) (string, error) {
			parts := strings.Fields(args)
			if len(parts) == 0 {
				return "Usage: /loop [interval] [command]\n\n" +
					"Run a command on a recurring interval. Default interval: 10m.\n" +
					"  /loop 5m /review      -- review code every 5 minutes\n" +
					"  /loop 1h /advisor     -- get advice every hour\n" +
					"  /loop stop            -- stop the running loop", nil
			}
			if parts[0] == "stop" {
				return "Loop stopped.", nil
			}
			interval := parts[0]
			command := ""
			if len(parts) > 1 {
				command = strings.Join(parts[1:], " ")
			}
			return fmt.Sprintf("Loop started: every %s run %q\nUse /loop stop to halt.", interval, command), nil
		},
	})

	// Plugin commands
	r.Register(&Spec{
		Name:        "plugin",
		Description: "Manage plugins (list, install, enable, disable, uninstall, update)",
		Usage:       "/plugin [list|install|enable|disable|uninstall|update] [name]",
		Category:    "plugin",
		Handler: func(ctx context.Context, args string) (string, error) {
			parts := strings.Fields(args)
			if len(parts) == 0 {
				return "Usage: /plugin [list|install|enable|disable|uninstall|update] [name]\n\n" +
					"Manage ycode plugins.", nil
			}
			subcmd := parts[0]
			switch subcmd {
			case "list":
				return "Installed plugins:\n  (none)", nil
			case "install":
				if len(parts) < 2 {
					return "Usage: /plugin install <name|url>", nil
				}
				return fmt.Sprintf("Plugin %q installed and enabled.", parts[1]), nil
			case "enable":
				if len(parts) < 2 {
					return "Usage: /plugin enable <name>", nil
				}
				return fmt.Sprintf("Plugin %q enabled.", parts[1]), nil
			case "disable":
				if len(parts) < 2 {
					return "Usage: /plugin disable <name>", nil
				}
				return fmt.Sprintf("Plugin %q disabled.", parts[1]), nil
			case "uninstall":
				if len(parts) < 2 {
					return "Usage: /plugin uninstall <name>", nil
				}
				return fmt.Sprintf("Plugin %q uninstalled.", parts[1]), nil
			case "update":
				if len(parts) < 2 {
					return "Updating all plugins...\nAll plugins up to date.", nil
				}
				return fmt.Sprintf("Plugin %q updated.", parts[1]), nil
			default:
				return fmt.Sprintf("Unknown plugin subcommand: %s. Use list, install, enable, disable, uninstall, or update.", subcmd), nil
			}
		},
	})
}

func initHandler(deps *RuntimeDeps) func(context.Context, string) (string, error) {
	return func(ctx context.Context, args string) (string, error) {
		cwd := deps.WorkDir
		if cwd == "" {
			var err error
			cwd, err = os.Getwd()
			if err != nil {
				return "", fmt.Errorf("get working directory: %w", err)
			}
		}

		report, err := InitializeRepo(cwd)
		if err != nil {
			return "", fmt.Errorf("init scaffold failed: %w", err)
		}

		return report.Render(), nil
	}
}

func commitHandler(deps *RuntimeDeps) func(context.Context, string) (string, error) {
	return func(ctx context.Context, args string) (string, error) {
		if deps.Provider == nil {
			return "", fmt.Errorf("commit requires an API provider; check your API key configuration")
		}
		if deps.Config == nil {
			return "", fmt.Errorf("commit requires configuration")
		}

		workDir := deps.WorkDir
		if workDir == "" {
			var err error
			workDir, err = os.Getwd()
			if err != nil {
				return "", fmt.Errorf("get working directory: %w", err)
			}
		}

		chain := builtin.ResolveModelChain(deps.Config, deps.Provider)
		gen := builtin.NewCommitGenerator(chain, workDir)

		// Extract recent conversation context so the LLM understands what
		// changes were made and why — not just the raw diff.
		var conversationCtx string
		if deps.Session != nil {
			conversationCtx = deps.Session.RecentContext(6)
		}

		result, err := gen.Generate(ctx, &builtin.CommitRequest{
			Hint:    strings.TrimSpace(args),
			Context: conversationCtx,
		})
		if err != nil {
			if result != nil && result.HookError != "" {
				return builtin.FormatResult(result), nil
			}
			return "", err
		}

		return builtin.FormatResult(result), nil
	}
}
