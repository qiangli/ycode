package commands

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"

	"github.com/dhnt/dhnt/catalog"
	"github.com/qiangli/ycode/internal/api"
	"github.com/qiangli/ycode/internal/features"
	"github.com/qiangli/ycode/internal/runtime/builtin"
	"github.com/qiangli/ycode/internal/runtime/codegraph"
	"github.com/qiangli/ycode/internal/runtime/config"
	"github.com/qiangli/ycode/internal/runtime/session"
	"github.com/qiangli/ycode/internal/runtime/task"
	"github.com/qiangli/ycode/internal/selfinit"
)

// commitSkillBody returns the markdown body of the upstream commit skill
// (github.com/dhnt/dhnt/catalog → commit/commit/skill.md). The catalog
// entry is embedded into the binary at build time, so this never touches
// the filesystem.
func commitSkillBody() string {
	if s, ok := catalog.Lookup("commit"); ok {
		return s.Body
	}
	// Defensive fallback — should never hit if the catalog dependency is
	// at the expected version. Keep the user moving rather than failing
	// loudly.
	return "Stage changes by name and write a Conventional Commits message."
}

// reviewScope normalizes the optional scope argument shared by the review-style
// commands, falling back to the caller's default when none was given.
func reviewScope(args, fallback string) string {
	if s := strings.TrimSpace(args); s != "" {
		return s
	}
	return fallback
}

// retryPrompt carries the recovered message from /retry's handler to its
// AgentPrompt. A package-level value is acceptable here only because a TUI
// runs one turn at a time; it is written and read within a single dispatch.
var retryPrompt atomic.Value

// PlanModeController manages plan mode state.
type PlanModeController interface {
	EnterPlanMode() (string, error)
	ExitPlanMode() (string, error)
	InPlanMode() bool
}

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

	// CloudboxLister is the callback used by /model (no-args) to surface
	// cloudbox-pooled models alongside the other sources. May be nil —
	// DiscoverModels treats nil as "skip cloudbox".
	CloudboxLister api.CloudboxLister

	// Tasks lists the background tasks this session is tracking. Nil when the
	// host has no task registry (thin client, shell mode).
	Tasks func() []*task.Task

	// AgentSwitcher routes to another agent: attached by default (ycode keeps
	// the screen and proxies), or handing over the terminal under --takeover.
	// Nil outside the interactive TUI — /agent then reports the equivalent
	// `bashy chat` command instead of pretending.
	AgentSwitcher func(ctx context.Context, req SwitchRequest) (string, error)

	// Detacher ends an attached session. Nil when nothing can be attached.
	Detacher func() (string, error)

	// RetryTurn removes the last turn and returns the last user message for re-execution.
	RetryTurn func() (string, error)

	// RevertFiles reverts file changes from the last agent turn.
	RevertFiles func() (string, error)

	// TrackUsage tracks token usage from builtin operations (optional).
	// Called by commands that make LLM calls to report token usage.
	TrackUsage func(inputTokens, outputTokens, cacheCreate, cacheRead int)

	// LogProgress logs a progress message during command execution (optional).
	// Called by commands to show status updates in the TUI.
	LogProgress func(message string)

	// LogDelta streams a text delta during command execution (optional).
	// Unlike LogProgress, deltas are appended without trailing newlines,
	// suitable for streaming LLM output character-by-character.
	LogDelta func(text string)

	// RunAgenticInit runs a mini agentic loop for /init with tool support.
	// The LLM can use graph query tools during AGENTS.md generation.
	// Returns the generated text. If nil, falls back to single-shot.
	RunAgenticInit func(ctx context.Context, systemPrompt, userPrompt string, onDelta func(string), onUsage func(int, int, int, int)) (string, error)

	// GraphManager provides session-level code graph access (optional).
	// When set, /init updates the session graph directly instead of only caching to disk.
	GraphManager *codegraph.Manager

	// PlanMode provides plan mode toggle (optional).
	// When set, enables the /plan slash command.
	PlanMode PlanModeController
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
		Examples: []string{
			"show me the commands",
			"what can I do here",
			"list available commands",
			"help me",
			"what slash commands are there",
		},
		Handler: func(ctx context.Context, args string) (string, error) {
			var b strings.Builder
			b.WriteString("Available commands:\n\n")
			cats := r.ListByCategory()
			// Sorted keys: ranging a map directly made this output differ
			// between runs, which defeats both diffing and golden tests.
			for _, cat := range Categories(cats) {
				fmt.Fprintf(&b, "## %s\n", cat)
				for _, s := range cats[cat] {
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
		Examples: []string{
			"what's the session status",
			"how is the session doing",
			"show session info",
			"current state",
		},
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
		Examples: []string{
			"how much have I spent",
			"show token usage",
			"how many tokens did I use",
			"what's my bill",
			"cost so far",
		},
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
		Examples: []string{
			"what model am I using",
			"switch models",
			"change to opus",
			"current model",
			"swap to a cheaper model",
		},
		Handler: func(ctx context.Context, args string) (string, error) {
			name := strings.TrimSpace(args)
			if name != "" {
				if deps.ModelSwitcher == nil {
					return "", fmt.Errorf("model switching not available")
				}
				return deps.ModelSwitcher(name)
			}

			current := ""
			if deps.Model != nil {
				current = deps.Model()
			}
			provider := ""
			if deps.ProviderKind != nil {
				provider = deps.ProviderKind()
			}
			var aliases map[string]string
			if deps.Config != nil {
				aliases = deps.Config.Aliases
			}
			models := api.DiscoverModels(ctx, aliases, deps.CloudboxLister)

			var b strings.Builder
			fmt.Fprintf(&b, "Model: %s (%s)\n", current, provider)

			grouped := map[string][]api.ModelInfo{}
			for _, m := range models {
				grouped[m.Source] = append(grouped[m.Source], m)
			}
			labels := []struct{ source, heading string }{
				{"builtin", "Built-in"},
				{"config", "Config"},
				{"env", "Env (from *_API_KEY)"},
				{"cloudbox", "Cloudbox (pooled)"},
			}
			for _, l := range labels {
				list := grouped[l.source]
				if len(list) == 0 {
					continue
				}
				fmt.Fprintf(&b, "\n%s:\n", l.heading)
				for _, m := range list {
					marker := "  "
					if (m.ID != "" && m.ID == current) || (m.Alias != "" && m.Alias == current) {
						marker = "● "
					}
					switch {
					case m.Alias != "":
						fmt.Fprintf(&b, "%s%s → %s (%s)\n", marker, m.Alias, m.ID, m.Provider)
					case m.Size != "":
						fmt.Fprintf(&b, "%s%s [%s]\n", marker, m.ID, m.Size)
					default:
						fmt.Fprintf(&b, "%s%s (%s)\n", marker, m.ID, m.Provider)
					}
				}
			}
			return strings.TrimRight(b.String(), "\n"), nil
		},
	})

	// Workspace commands
	r.Register(&Spec{
		Name: "clear",
		// HIDDEN: no session clear/truncate API exists yet — the handler only prints
		Tier:        features.TierWIP,
		Description: "Clear conversation history",
		Category:    "workspace",
		Examples: []string{
			"start over",
			"wipe the conversation",
			"reset the chat",
			"clear my history",
			"new session please",
		},
		Handler: func(ctx context.Context, args string) (string, error) {
			return "Conversation cleared.", nil
		},
	})

	r.Register(&Spec{
		Name: "compact",
		// HIDDEN: App.CompactContext computes a CompactionResult and discards it —
		// neither it nor Runtime.CompactNow writes back to the session. Wiring this
		// up would report "compacted N messages" over an unchanged conversation,
		// which is a better-disguised lie than the placeholder it replaced.
		Tier:        features.TierWIP,
		Description: "Compact conversation by summarizing older messages",
		Category:    "workspace",
		Examples: []string{
			"compress the conversation",
			"summarize older messages",
			"shrink the context",
			"compact history",
		},
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
			// Stash it for AgentPrompt below. RetryTurn has just REMOVED the
			// message from the session, so by the time AgentPrompt runs there is
			// nothing left to read it back from — and AgentPrompt only receives
			// args, which is empty on a bare /retry. The handler is the last
			// place the text exists.
			retryPrompt.Store(prompt)
			return fmt.Sprintf("Retrying with: %s", prompt), nil
		},
		// Without this the turn was removed and never re-sent: "/retry" did the
		// destructive half of its job and stopped.
		AgentPrompt: func(args string) string {
			if v, ok := retryPrompt.Load().(string); ok {
				return v
			}
			return strings.TrimSpace(args)
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

	// /init: command handler creates scaffold (dirs, config, gitignore, AGENTS.md)
	// deterministically, then uses single-shot LLM call to enhance AGENTS.md.
	// This bypasses the expensive agentic turn for 90-95% token savings vs the
	// original multi-tool-call approach.
	r.Register(&Spec{
		Name:        "init",
		Description: "Initialize workspace and generate context-aware AGENTS.md",
		Usage:       "/init [focus]",
		Category:    "workspace",
		Examples: []string{
			"set up AGENTS.md",
			"initialize this repo",
			"create the agent guidance file",
			"bootstrap ycode for this project",
			"scaffold AGENTS.md",
		},
		Handler: initHandler(deps),
	})

	// Also register as a skill executor so the LLM can call Skill("init")
	// during agentic turns. Uses the same optimized single-shot path.
	builtin.RegisterSkillExecutor("init", func(ctx context.Context, args string) (string, error) {
		cwd := deps.WorkDir
		if cwd == "" {
			var err error
			cwd, err = os.Getwd()
			if err != nil {
				return "", fmt.Errorf("get working directory: %w", err)
			}
		}

		var outputParts []string
		// emit appends to the tool result and, when the TUI has wired a
		// LogProgress callback, streams the line live so the user sees
		// each step as it happens (the gfy graph build can take minutes
		// on large repos with no cache).
		emit := func(msg string) {
			outputParts = append(outputParts, msg)
			if deps.LogProgress != nil {
				deps.LogProgress(msg)
			}
		}

		// Phase 1: Scaffold (always shown first).
		report, err := InitializeRepo(cwd)
		if err != nil {
			return "", fmt.Errorf("init scaffold failed: %w", err)
		}
		emit(report.Render())

		// Phase 2: Single-shot enhancement if provider available.
		// Uses opencode-style template with structured investigation guidance.
		if deps.Provider == nil || deps.Config == nil {
			emit("⚠ Skipped LLM enhancement (no API provider configured)")
		} else {
			chain := builtin.ResolveModelChain(deps.Config, deps.Provider)

			// Gather context using opencode-style investigation.
			emit("⧗ Analyzing project structure...")
			gen := builtin.NewInitGenerator(cwd)
			gen.SetProgressFunc(emit)
			if deps.GraphManager != nil {
				gen.SetGraphManager(deps.GraphManager)
			}
			initResult, genErr := gen.Generate(args)
			if genErr != nil {
				emit(fmt.Sprintf("⚠ Failed to gather project context: %v", genErr))
			} else if initResult == nil {
				emit("⚠ Failed to gather project context (no result)")
			} else {
				// Generate AGENTS.md via single LLM call.
				emit("⧗ Generating AGENTS.md via LLM...")
				llmResult, llmErr := chain.SingleShotWithUsageAndTimeout(ctx, initResult.SystemPrompt, initResult.UserPrompt, 4096, builtin.InitSingleShotTimeout)
				if llmErr != nil {
					emit(fmt.Sprintf("⚠ LLM generation failed: %v", llmErr))
				} else if llmResult == nil || llmResult.Text == "" {
					emit("⚠ LLM returned empty response — AGENTS.md not updated")
				} else {
					agentsPath := filepath.Join(cwd, "AGENTS.md")
					cleaned := builtin.CleanInitOutput(llmResult.Text)

					// Compare against existing content to detect no-op updates.
					existing, _ := os.ReadFile(agentsPath)
					if len(existing) > 0 && builtin.ContentUnchanged(string(existing), cleaned) {
						emit("✓ AGENTS.md is already well-structured — no changes needed")
					} else if err := os.WriteFile(agentsPath, []byte(cleaned), 0o644); err != nil {
						emit(fmt.Sprintf("⚠ Failed to write AGENTS.md: %v", err))
					} else {
						emit(fmt.Sprintf("✓ Updated AGENTS.md (analyzed: %v)", initResult.FilesRead))
						if len(initResult.Questions) > 0 {
							emit("")
							emit("Consider answering these questions to improve AGENTS.md:")
							for _, q := range initResult.Questions {
								emit(fmt.Sprintf("  - %s", q))
							}
						}
					}
					// Track usage regardless of whether content changed.
					if deps.TrackUsage != nil {
						deps.TrackUsage(llmResult.InputTokens, llmResult.OutputTokens, llmResult.CacheCreate, llmResult.CacheRead)
					}
					if llmResult.InputTokens > 0 || llmResult.OutputTokens > 0 {
						totalTokens := llmResult.InputTokens + llmResult.OutputTokens
						emit(fmt.Sprintf("  Tokens: %d in, %d out (%d total)",
							llmResult.InputTokens, llmResult.OutputTokens, totalTokens))
					}
				}
			}
		}

		// /init parity with auto-startup SelfInit: append the ycode
		// awareness block (and refresh user-scope foreign-tool configs)
		// so /init always leaves the project in the same shape the
		// auto-init flow would.
		if res, err := selfinit.Run(ctx, selfinit.Options{Cwd: cwd, Force: true}); err != nil {
			emit(fmt.Sprintf("⚠ ycode self-init: %v", err))
		} else if !res.OptedOut {
			if len(res.ProjectFiles) > 0 {
				emit(fmt.Sprintf("✓ ycode self-init: refreshed %d project files", len(res.ProjectFiles)))
			}
			for tool, files := range res.UserFilesByTool {
				emit(fmt.Sprintf("✓ ycode self-init: refreshed %s (%v)", tool, files))
			}
		}

		return strings.Join(outputParts, "\n"), nil
	})

	// /commit: builtin executor returns the catalog-hosted commit skill
	// instructions. The skill body lives upstream at
	// github.com/dhnt/dhnt/catalog/md/commit/commit/skill.md (executor: builtin)
	// so any agent harness sees the same instruction set.
	builtin.RegisterSkillExecutor("commit", func(ctx context.Context, args string) (string, error) {
		body := commitSkillBody()
		if args != "" {
			return strings.ReplaceAll(body, "{{ARGS}}", args), nil
		}
		return strings.ReplaceAll(body, "{{ARGS}}", "(none)"), nil
	})

	// Discovery commands
	r.Register(&Spec{
		Name: "doctor",
		// HIDDEN: the real checks live in the cobra `ycode doctor`; this one returns a fixed "All checks passed."
		Tier:        features.TierExperimental,
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
		Examples: []string{
			"how much context am I using",
			"context usage",
			"show loaded instruction files",
			"how full is the context window",
		},
		Handler: contextHandler(deps),
	})

	r.Register(&Spec{
		Name: "skills",
		// HIDDEN: skillengine discovery is unwired, and `install-bundled` reports success without installing
		Tier:        features.TierExperimental,
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
		Examples: []string{
			"what tasks are running",
			"show me background tasks",
			"list active jobs",
			"any tasks in progress",
		},
		Handler: func(ctx context.Context, args string) (string, error) {
			// This used to return "No tasks running." unconditionally, which
			// reads as a finding but was a constant — it said the same thing
			// with ten tasks in flight.
			if deps.Tasks == nil {
				return "", fmt.Errorf("task tracking is not available in this session")
			}
			tasks := deps.Tasks()
			if len(tasks) == 0 {
				return "No tasks running.", nil
			}
			var b strings.Builder
			fmt.Fprintf(&b, "%d task(s):\n", len(tasks))
			for _, t := range tasks {
				fmt.Fprintf(&b, "  %s  %-9s  %s", truncateID(t.ID), t.Status, t.Description)
				if t.Error != "" {
					fmt.Fprintf(&b, "\n      error: %s", truncateForSummary(t.Error, 160))
				}
				b.WriteString("\n")
			}
			return b.String(), nil
		},
	})

	// Plan mode command
	r.Register(&Spec{
		Name:        "plan",
		Description: "Toggle plan mode or enter with a query",
		Usage:       "/plan [query]",
		Category:    "mode",
		Handler:     planHandler(deps),
		// A bare /plan is a pure toggle and must NOT start a turn; only
		// `/plan <query>` has something to plan. Returning "" suppresses the
		// agentic turn, which is why the empty case is meaningful here.
		AgentPrompt: func(args string) string {
			q := strings.TrimSpace(args)
			if q == "" || q == "status" {
				return ""
			}
			return q
		},
	})

	// Automation commands
	commitFn := commitHandler(deps)
	r.Register(&Spec{
		Name:        "commit",
		Description: "Commit changes with AI-generated message",
		Usage:       "/commit [hint]",
		Category:    "automation",
		Examples: []string{
			"commit my changes",
			"create a git commit",
			"check in this work",
			"commit the staged files",
		},
		Handler: commitFn,
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
		Examples: []string{
			"review my PR",
			"look at my diff",
			"code review the staged changes",
			"give me feedback on this code",
			"check my pull request",
		},
		Handler: func(ctx context.Context, args string) (string, error) {
			return fmt.Sprintf("Reviewing %s...", reviewScope(args, "staged changes")), nil
		},
		// The old handler printed "[Review agent would execute here]" — the
		// text WAS the spec. A review is an agent turn, not something a
		// handler computes, so hand the turn to the agent.
		AgentPrompt: func(args string) string {
			return fmt.Sprintf("Review %s. Report concrete defects with file:line, "+
				"ordered most severe first: correctness bugs and edge cases, then style "+
				"and convention breaks. If you find nothing worth changing, say so plainly "+
				"rather than inventing findings.", reviewScope(args, "the staged changes"))
		},
	})

	r.Register(&Spec{
		Name:        "advisor",
		Description: "Get architectural advice or codebase insights",
		Usage:       "/advisor [topic]",
		Category:    "automation",
		Handler: func(ctx context.Context, args string) (string, error) {
			return fmt.Sprintf("Analyzing %s...", reviewScope(args, "the architecture")), nil
		},
		AgentPrompt: func(args string) string {
			return fmt.Sprintf("Analyze this codebase and give architectural advice on %s. "+
				"Ground every claim in files you actually read, and name them. Prefer a few "+
				"load-bearing observations over a survey.", reviewScope(args, "its overall architecture"))
		},
	})

	r.Register(&Spec{
		Name:        "security-review",
		Description: "Run security analysis on code changes",
		Usage:       "/security-review [path|staged]",
		Category:    "automation",
		Handler: func(ctx context.Context, args string) (string, error) {
			return fmt.Sprintf("Security review of %s...", reviewScope(args, "the staged changes")), nil
		},
		AgentPrompt: func(args string) string {
			return fmt.Sprintf("Perform a security review of %s. Look for injection "+
				"(SQL/command/XSS), authentication and authorization gaps, sensitive-data "+
				"exposure, unsafe deserialization, and risky dependencies. Report each finding "+
				"with file:line and a concrete exploit path — a finding you cannot show a path "+
				"for is a guess, so label it as one.", reviewScope(args, "the staged changes"))
		},
	})

	r.Register(&Spec{
		Name: "team",
		// HIDDEN: the team registry is constructed and discarded (app.go); this CRUD is an echo
		Tier:        features.TierWIP,
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
		Name: "cron",
		// HIDDEN: same as /team — no scheduler is reachable from here
		Tier:        features.TierWIP,
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
		Name: "loop",
		// HIDDEN: no timer and no goroutine; "Loop stopped." stops nothing
		Tier:        features.TierWIP,
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

	// Search command
	RegisterSearchCommand(r, deps)
	registerSwitchCommands(r, deps)

	// Plugin commands
	r.Register(&Spec{
		Name: "plugin",
		// HIDDEN: internal/plugins is never instantiated anywhere
		Tier:        features.TierWIP,
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

func planHandler(deps *RuntimeDeps) func(context.Context, string) (string, error) {
	return func(ctx context.Context, args string) (string, error) {
		if deps.PlanMode == nil {
			return "Plan mode not available (no .agents/ycode/ directory).", nil
		}

		args = strings.TrimSpace(args)

		// /plan with no args: toggle mode.
		if args == "" {
			if deps.PlanMode.InPlanMode() {
				return deps.PlanMode.ExitPlanMode()
			}
			return deps.PlanMode.EnterPlanMode()
		}

		// /plan status: show current mode.
		if args == "status" {
			if deps.PlanMode.InPlanMode() {
				return "Currently in plan mode (read-only). Use /plan or shift+tab to exit.", nil
			}
			return "Currently in build mode (full access). Use /plan or shift+tab to enter plan mode.", nil
		}

		// /plan <query>: enter plan mode (if not already) with context. The
		// query itself is handed to the agent by planAgentPrompt — echoing it
		// back, which is all this used to do, is not planning.
		if !deps.PlanMode.InPlanMode() {
			result, err := deps.PlanMode.EnterPlanMode()
			if err != nil {
				return "", err
			}
			return result, nil
		}
		return "Already in plan mode.", nil
	}
}

func initHandler(deps *RuntimeDeps) func(context.Context, string) (string, error) {
	// log sends a progress message to the TUI immediately (if available),
	// otherwise accumulates for the final return value.
	return func(ctx context.Context, args string) (string, error) {
		cwd := deps.WorkDir
		if cwd == "" {
			var err error
			cwd, err = os.Getwd()
			if err != nil {
				return "", fmt.Errorf("get working directory: %w", err)
			}
		}

		// progress streams a line to the TUI immediately when available.
		var finalParts []string
		progress := func(msg string) {
			if deps.LogProgress != nil {
				deps.LogProgress(msg)
			} else {
				finalParts = append(finalParts, msg)
			}
		}

		// Phase 0: Run selfinit so the TUI /init is a strict superset of
		// `ycode init` (CLI). This installs Foreman protocol scaffolding
		// (docs/backlog.md, docs/backlog/, user-global /foreman skill,
		// .agents/ycode/AGENTS.md) and registers ycode capabilities for
		// any detected foreign agent. Idempotent — no-op if marker matches.
		if siRes, err := selfinit.Run(ctx, selfinit.Options{Cwd: cwd}); err != nil {
			progress(fmt.Sprintf("⚠ selfinit failed: %v", err))
		} else if !siRes.Skipped {
			progress(fmt.Sprintf("✓ selfinit: %d project file(s), %d user-global file(s)",
				len(siRes.ProjectFiles), len(siRes.UserGlobalFiles)))
		}

		// Phase 1: Deterministic project scaffold (shown immediately).
		report, err := InitializeRepo(cwd)
		if err != nil {
			return "", fmt.Errorf("init scaffold failed: %w", err)
		}
		progress(report.Render())

		// Phase 2: LLM enhancement if provider available.
		if deps.Provider == nil || deps.Config == nil {
			progress("⚠ Skipped LLM enhancement (no API provider configured)")
		} else {
			progress("⧗ Analyzing project structure...")
			gen := builtin.NewInitGenerator(cwd)
			gen.SetProgressFunc(progress)
			if deps.GraphManager != nil {
				gen.SetGraphManager(deps.GraphManager)
			}
			initResult, genErr := gen.Generate(args)
			if genErr != nil {
				progress(fmt.Sprintf("⚠ Failed to gather project context: %v", genErr))
			} else if initResult == nil {
				progress("⚠ Failed to gather project context (no result)")
			} else {
				var llmText string
				var llmInputTokens, llmOutputTokens int
				var llmErr error

				// Try agentic init (with graph tools) if available, else single-shot.
				if deps.RunAgenticInit != nil {
					progress("⧗ Generating AGENTS.md via LLM (with graph tools)...")
					llmText, llmErr = deps.RunAgenticInit(ctx, initResult.SystemPrompt, initResult.UserPrompt, func(text string) {
						if deps.LogDelta != nil {
							deps.LogDelta(text)
						}
					}, deps.TrackUsage)
				} else {
					progress("⧗ Generating AGENTS.md via LLM...")
					chain := builtin.ResolveModelChain(deps.Config, deps.Provider)
					llmResult, err := chain.SingleShotStreamingWithTimeout(ctx, initResult.SystemPrompt, initResult.UserPrompt, 4096, builtin.InitSingleShotTimeout, func(text string) {
						if deps.LogDelta != nil {
							deps.LogDelta(text)
						}
					}, deps.TrackUsage)
					if err != nil {
						llmErr = err
					} else if llmResult != nil {
						llmText = llmResult.Text
						llmInputTokens = llmResult.InputTokens
						llmOutputTokens = llmResult.OutputTokens
					}
				}

				if llmErr != nil {
					progress(fmt.Sprintf("⚠ LLM generation failed: %v", llmErr))
				} else if llmText == "" {
					progress("⚠ LLM returned empty response — AGENTS.md not updated")
				} else {
					agentsPath := filepath.Join(cwd, "AGENTS.md")
					cleaned := builtin.CleanInitOutput(llmText)

					// Compare against existing content to detect no-op updates.
					existing, _ := os.ReadFile(agentsPath)
					if len(existing) > 0 && builtin.ContentUnchanged(string(existing), cleaned) {
						progress("✓ AGENTS.md is already well-structured — no changes needed")
					} else if err := os.WriteFile(agentsPath, []byte(cleaned), 0o644); err != nil {
						progress(fmt.Sprintf("⚠ Failed to write AGENTS.md: %v", err))
					} else {
						progress(fmt.Sprintf("✓ Updated AGENTS.md (analyzed: %v)", initResult.FilesRead))
						if len(initResult.Questions) > 0 {
							progress("")
							progress("Consider answering these questions to improve AGENTS.md:")
							for _, q := range initResult.Questions {
								progress(fmt.Sprintf("  - %s", q))
							}
						}
					}
					if llmInputTokens > 0 || llmOutputTokens > 0 {
						totalTokens := llmInputTokens + llmOutputTokens
						progress(fmt.Sprintf("  Tokens: %d in, %d out (%d total)",
							llmInputTokens, llmOutputTokens, totalTokens))
					}
				}
			}
		}

		// When streaming via LogProgress, return empty — output already shown.
		if deps.LogProgress != nil {
			return "", nil
		}
		return strings.Join(finalParts, "\n"), nil
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
