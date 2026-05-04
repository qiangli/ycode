package cli

import (
	"context"
	"fmt"
	"io"
	"log"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"google.golang.org/grpc/grpclog"

	"github.com/qiangli/ycode/internal/api"
	"github.com/qiangli/ycode/internal/commands"
	"github.com/qiangli/ycode/internal/runtime/agentdef"
	"github.com/qiangli/ycode/internal/runtime/agentpool"
	"github.com/qiangli/ycode/internal/runtime/builtin"
	"github.com/qiangli/ycode/internal/runtime/codegraph"
	"github.com/qiangli/ycode/internal/runtime/config"
	"github.com/qiangli/ycode/internal/runtime/conversation"
	"github.com/qiangli/ycode/internal/runtime/git"
	"github.com/qiangli/ycode/internal/runtime/lanes"
	"github.com/qiangli/ycode/internal/runtime/memory"
	"github.com/qiangli/ycode/internal/runtime/permission"
	"github.com/qiangli/ycode/internal/runtime/prompt"
	"github.com/qiangli/ycode/internal/runtime/routing"
	"github.com/qiangli/ycode/internal/runtime/session"
	"github.com/qiangli/ycode/internal/runtime/swarm"
	"github.com/qiangli/ycode/internal/runtime/task"
	"github.com/qiangli/ycode/internal/runtime/taskqueue"
	"github.com/qiangli/ycode/internal/runtime/team"
	"github.com/qiangli/ycode/internal/runtime/usage"
	"github.com/qiangli/ycode/internal/runtime/worker"
	"github.com/qiangli/ycode/internal/storage"
	"github.com/qiangli/ycode/internal/tools"
)

// App is the main interactive application.
type App struct {
	config         *config.Config
	provider       api.Provider
	providerKind   string
	session        *session.Session
	renderer       *Renderer
	commands       *commands.Registry
	toolRegistry   *tools.Registry
	promptCtx      *prompt.ProjectContext
	planMode       tools.PlanModeController
	stdout         io.Writer
	printMode      bool // plain text output, no markdown rendering
	version        string
	workDir        string
	userConfigPath string // path to user settings.json for persisting preferences

	// Storage manager for persistence layer.
	storage *storage.Manager

	// Task registry for background tasks (including background agents).
	taskRegistry *task.Registry

	// Agent pool for tracking active subagents and progress reporting.
	agentPool *agentpool.Pool

	// Custom agent definitions loaded from YAML config.
	agentDefs *agentdef.Registry

	// Lane scheduler for concurrency control across main/subagent/cron work.
	laneScheduler *lanes.Scheduler

	// Memory manager for episodic memory recording in subagents.
	memoryManager *memory.Manager

	// Persona resolver for tailored user experience (lazily initialized).
	personaResolver *memory.PersonaResolver
	currentPersona  *memory.Persona

	// Session tracking for summary reporting.
	usageTracker *usage.Tracker
	sessionStart time.Time

	// Ollama model lister for model discovery (optional).
	ollamaLister api.OllamaLister

	// Inference router for Tier 2 tool pre-activation and model selection.
	inferenceRouter *routing.Router

	// OTEL conversation instrumentation (optional).
	convOTEL  *conversation.OTELConfig
	turnIndex int // monotonically increasing turn counter for OTEL

	// Cleanup functions called on Close (OTEL shutdown, context cancel, etc.).
	cleanupFuncs []func()

	// Code knowledge graph manager — thread-safe, auto-rebuilds on code changes.
	graphManager *codegraph.Manager

	// Progress callback for command status updates (set by TUI).
	progressFunc func(message string)
	// Delta callback for streaming text during command execution (set by TUI).
	deltaFunc func(text string)
}

// AppOptions holds optional configuration for App creation.
type AppOptions struct {
	WorkDir         string
	ConfigDirs      commands.ConfigDirs
	MemoryDir       string
	Version         string
	ProviderKind    string
	PlanMode        tools.PlanModeController
	ToolRegistry    *tools.Registry
	PromptCtx       *prompt.ProjectContext
	UserConfigPath  string
	Storage         *storage.Manager
	ConvOTEL        *conversation.OTELConfig
	OllamaLister    api.OllamaLister
	AgentDefsDir    string // directory containing custom agent YAML definitions
	InferenceRouter *routing.Router
	MemoryManager   *memory.Manager
}

// NewThinApp creates a minimal App for client-mode TUI rendering.
// It does not initialize tools, storage, or provider — all agent logic
// is delegated to the remote server via the client passed to RunInteractiveWithClient.
func NewThinApp(version, workDir string) (*App, error) {
	renderer, err := NewRenderer("")
	if err != nil {
		return nil, err
	}
	if workDir == "" {
		workDir, _ = os.Getwd()
	}
	if version == "" {
		version = "dev"
	}
	cmdRegistry := commands.NewRegistry()
	commands.RegisterBuiltins(cmdRegistry, &commands.RuntimeDeps{
		Version: version,
		WorkDir: workDir,
	})
	return &App{
		renderer:     renderer,
		version:      version,
		workDir:      workDir,
		stdout:       os.Stdout,
		config:       &config.Config{},
		commands:     cmdRegistry,
		taskRegistry: task.NewRegistry(),
		agentPool:    agentpool.New(),
		usageTracker: usage.NewTracker(),
		sessionStart: time.Now(),
	}, nil
}

// NewApp creates a new app instance.
func NewApp(cfg *config.Config, provider api.Provider, sess *session.Session, opts ...AppOptions) (*App, error) {
	renderer, err := NewRenderer("")
	if err != nil {
		return nil, err
	}

	var o AppOptions
	if len(opts) > 0 {
		o = opts[0]
	}
	if o.WorkDir == "" {
		o.WorkDir, _ = os.Getwd()
	}
	if o.Version == "" {
		o.Version = "dev"
	}

	app := &App{
		config:          cfg,
		provider:        provider,
		providerKind:    o.ProviderKind,
		session:         sess,
		renderer:        renderer,
		toolRegistry:    o.ToolRegistry,
		promptCtx:       o.PromptCtx,
		planMode:        o.PlanMode,
		stdout:          os.Stdout,
		version:         o.Version,
		workDir:         o.WorkDir,
		userConfigPath:  o.UserConfigPath,
		storage:         o.Storage,
		usageTracker:    usage.NewTracker(),
		sessionStart:    time.Now(),
		convOTEL:        o.ConvOTEL,
		ollamaLister:    o.OllamaLister,
		inferenceRouter: o.InferenceRouter,
		memoryManager:   o.MemoryManager,
		laneScheduler:   lanes.NewScheduler(),
		taskRegistry:    task.NewRegistry(),
		agentPool:       agentpool.New(),
	}

	// Create code graph manager (loaded from cache if available).
	app.graphManager = codegraph.NewManager(o.WorkDir)

	// Set up command registry.
	cmdRegistry := commands.NewRegistry()
	commands.RegisterBuiltins(cmdRegistry, &commands.RuntimeDeps{
		SessionID:     sess.ID,
		MessageCount:  sess.MessageCount,
		Model:         func() string { return app.config.Model },
		ProviderKind:  func() string { return app.providerKind },
		CostSummary:   func() string { return "Cost tracking not yet available" },
		Version:       o.Version,
		WorkDir:       o.WorkDir,
		Config:        cfg,
		ConfigDirs:    o.ConfigDirs,
		MemoryDir:     o.MemoryDir,
		Session:       sess,
		Provider:      app.provider,
		ModelSwitcher: app.SwitchModel,
		RetryTurn:     app.RetryTurn,
		RevertFiles:   app.RevertFiles,
		TrackUsage: func(inputTokens, outputTokens, cacheCreate, cacheRead int) {
			app.usageTracker.Add(inputTokens, outputTokens, cacheCreate, cacheRead)
		},
		LogProgress: func(message string) {
			app.LogProgress(message)
		},
		LogDelta: func(text string) {
			app.LogDelta(text)
		},
		RunAgenticInit: app.runAgenticInit,
		GraphManager:   app.graphManager,
	})
	app.commands = cmdRegistry

	// Register task, worker, and team tool handlers.
	if app.toolRegistry != nil {
		tools.RegisterTaskHandlers(app.toolRegistry, app.taskRegistry)
		tools.RegisterWorkerHandlers(app.toolRegistry, worker.NewRegistry())
		tools.RegisterTeamHandlers(app.toolRegistry, team.NewRegistry(), team.NewCronRegistry())
		tools.RegisterHandoffHandler(app.toolRegistry)

		// Register code graph query tools with live manager.
		tools.RegisterGraphHandlers(app.toolRegistry, app.graphManager)

		// Chain graph invalidation onto the file write hook so the graph
		// rebuilds automatically when code changes during the session.
		app.toolRegistry.AddFileWriteHook(app.graphManager.NotifyFileChanged)
	}

	// Load custom agent definitions from config directories.
	if o.AgentDefsDir != "" {
		reg, err := agentdef.Load(o.AgentDefsDir)
		if err != nil {
			slog.Warn("failed to load agent definitions", "dir", o.AgentDefsDir, "error", err)
		} else {
			app.agentDefs = reg
		}
	}

	// Wire the agent spawner so the Agent tool can create child runtimes.
	if app.toolRegistry != nil && app.provider != nil {
		caps := api.DetectCapabilities(app.provider.Kind(), cfg.Model)
		spawner := conversation.NewAgentSpawner(&conversation.SpawnerConfig{
			Model:            cfg.Model,
			Provider:         app.provider,
			Registry:         app.toolRegistry,
			PromptCtx:        app.promptCtx,
			CachingSupported: caps.CachingSupported,
			ParallelEnabled:  cfg.Parallel.Enabled,
			MaxStandard:      cfg.Parallel.MaxStandard,
			MaxLLM:           cfg.Parallel.MaxLLM,
			MaxAgent:         cfg.Parallel.MaxAgent,
			AgentPool:        app.agentPool,
			LaneScheduler:    app.laneScheduler,
			MemoryManager:    app.memoryManager,
			SessionID:        sess.ID,
		})
		// Wrap spawner with swarm orchestration: detect handoff signals in
		// agent results and route to the target agent via the Orchestrator.
		wrappedSpawner := func(ctx context.Context, manifest *tools.AgentManifest) (string, error) {
			result, err := spawner(ctx, manifest)
			if err != nil {
				return result, err
			}
			if hr, isHandoff := swarm.DetectHandoff(result); isHandoff && app.agentDefs != nil {
				slog.Info("swarm: handoff detected",
					"from", manifest.Type,
					"to", hr.TargetAgent,
				)
				orch := swarm.NewOrchestrator(&swarm.OrchestratorConfig{
					AgentDefs:   app.agentDefs,
					Spawner:     spawner,
					MaxHandoffs: 10,
				})
				return orch.Run(ctx, hr.TargetAgent, hr.Message)
			}
			return result, nil
		}
		tools.RegisterAgentHandler(app.toolRegistry, app.parentAgentMode, wrappedSpawner, app.taskRegistry, app.agentDefs)
	}

	return app, nil
}

// SetPrintMode enables plain text output mode (no markdown rendering).
func (a *App) SetPrintMode(enabled bool) {
	a.printMode = enabled
}

// conversationRuntime creates a conversation.Runtime from the current app state.
func (a *App) conversationRuntime() *conversation.Runtime {
	rt := conversation.NewRuntime(a.config, a.provider, a.session, a.toolRegistry, a.promptCtx)
	rt.SetPlanMode(a.InPlanMode())
	if a.config.LLMSummarizationEnabled {
		if a.config.WeakModel != "" {
			// Fallback chain: try weak (cheap) model first, then main model.
			rt.SetLLMSummarizer(session.NewLLMSummarizerChain([]session.ModelSpec{
				{Provider: a.provider, Model: a.config.WeakModel},
				{Provider: a.provider, Model: a.config.Model},
			}))
		} else {
			rt.SetLLMSummarizer(session.NewLLMSummarizer(a.provider, a.config.Model))
		}
	}
	if a.convOTEL != nil {
		rt.SetOTEL(a.convOTEL)
	}
	if a.config.CacheWarmingEnabled {
		caps := api.DetectCapabilities(a.provider.Kind(), a.config.Model)
		if caps.CachingSupported {
			rt.SetCacheWarmer(api.NewCacheWarmer(a.provider))
		}
	}
	if a.inferenceRouter != nil {
		rt.SetInferenceRouter(a.inferenceRouter)
	}
	// Wire persona for tailored user experience.
	if a.config.PersonaEnabled {
		if a.currentPersona != nil {
			rt.SetPersona(a.currentPersona)
		} else if a.memoryManager != nil {
			a.resolvePersona()
			if a.currentPersona != nil {
				rt.SetPersona(a.currentPersona)
			}
		}
	}
	// Restore L1 working memory (active topic) from ghost snapshot
	// if this is a resumed session with prior compaction.
	rt.RestoreTopicFromGhost()
	// Inject prior-session diagnostics (summary from last ghost snapshot)
	// so the agent has warm-start context about what happened previously.
	rt.RestoreSessionDiagnostics()
	return rt
}

// resolvePersona lazily initializes the persona resolver and resolves the
// current user's persona from environment signals.
func (a *App) resolvePersona() {
	if a.personaResolver == nil {
		globalStore := a.memoryManager.GlobalStore()
		if globalStore == nil {
			return
		}
		a.personaResolver = memory.NewPersonaResolver(globalStore, nil)
	}

	env := a.collectEnvironmentSignals()
	p, err := a.personaResolver.Resolve(env)
	if err != nil {
		slog.Debug("persona resolve", "error", err)
		return
	}
	a.currentPersona = p
}

// collectEnvironmentSignals gathers environment hints for persona matching.
func (a *App) collectEnvironmentSignals() *memory.EnvironmentSignals {
	env := &memory.EnvironmentSignals{
		Platform: a.promptCtx.Platform,
		Shell:    a.promptCtx.Shell,
	}

	// Git user info from prompt context.
	if a.promptCtx.GitUser != "" {
		env.GitUserName = a.promptCtx.GitUser
	}

	// Git email from git config (best effort).
	if email := gitConfigEmail(a.workDir); email != "" {
		env.GitEmail = email
	}

	if home, err := os.UserHomeDir(); err == nil {
		env.HomeDir = home
	}
	if hostname, err := os.Hostname(); err == nil {
		env.Hostname = hostname
	}

	return env
}

// gitConfigEmail reads the user.email from git config (best effort).
func gitConfigEmail(dir string) string {
	out, err := git.NewGitExec(nil).RunOutput(context.Background(), dir, "config", "user.email")
	if err != nil {
		return ""
	}
	return out
}

// maxToolIterations is the maximum number of tool-use round-trips per turn.
const maxToolIterations = 25

// RunPrompt executes a one-shot prompt with the full agentic loop
// (system prompt, tools, multi-turn tool execution).
func (a *App) RunPrompt(ctx context.Context, userPrompt string) error {
	// Dispatch slash commands (e.g. /init, /config) before the provider check
	// — some commands like /init have a deterministic phase that works without an LLM.
	if strings.HasPrefix(userPrompt, "/") {
		rest := userPrompt[1:]
		name, args, _ := strings.Cut(rest, " ")
		if _, ok := a.commands.Get(name); ok {
			// Enable progressive output in one-shot mode so long-running
			// commands (e.g. /init with LLM call) stream lines immediately.
			if a.progressFunc == nil {
				a.progressFunc = func(message string) {
					fmt.Fprintln(a.stdout, message)
				}
			}
			if a.deltaFunc == nil {
				a.deltaFunc = func(text string) {
					fmt.Fprint(a.stdout, text)
				}
			}
			output, err := a.commands.Execute(ctx, name, args)
			if err != nil {
				return err
			}
			if output != "" {
				fmt.Fprint(a.stdout, output)
			}
			return nil
		}
	}

	if a.provider == nil {
		return fmt.Errorf("no LLM provider configured; set ANTHROPIC_API_KEY, OPENAI_API_KEY, MOONSHOT_API_KEY, or KIMI_API_KEY")
	}

	// Check for high-confidence builtin intent before the expensive agentic loop.
	if intent := builtin.DetectIntent(userPrompt); intent != nil {
		output, err := a.commands.Execute(ctx, intent.Operation, intent.Args)
		if err != nil {
			return err
		}
		fmt.Fprint(a.stdout, output)
		return nil
	}

	// Wire a non-interactive permission prompter for one-shot mode.
	// Without this, tools requiring elevated permissions are silently denied
	// with a confusing error. The non-interactive prompter denies with a clear
	// message directing the user to use --dangerously-skip-permissions or
	// interactive mode.
	if a.toolRegistry != nil {
		a.toolRegistry.SetPermissionPrompter(func(_ context.Context, toolName string, requiredMode permission.Mode) (bool, error) {
			return false, fmt.Errorf("tool %q requires %s permission; use --dangerously-skip-permissions or run in interactive mode to approve",
				toolName, requiredMode)
		})
	}

	rt := a.conversationRuntime()

	// Build conversation history from session + new user message.
	messages := a.sessionMessages()
	messages = append(messages, api.Message{
		Role: api.RoleUser,
		Content: []api.ContentBlock{
			{Type: api.ContentTypeText, Text: userPrompt},
		},
	})

	// Save user message to session.
	_ = a.session.AddMessage(session.ConversationMessage{
		Role: session.RoleUser,
		Content: []session.ContentBlock{
			{Type: session.ContentTypeText, Text: userPrompt},
		},
	})

	// Agentic loop: send → receive → execute tools → repeat until end_turn.
	loopDetector := conversation.NewLoopDetector()
	for i := 0; i < maxToolIterations; i++ {
		a.turnIndex++
		result, recovery, err := rt.InstrumentedTurnWithRecovery(ctx, messages, a.turnIndex)
		if err != nil {
			return fmt.Errorf("turn %d: %w", i+1, err)
		}

		// Track usage from this turn.
		a.usageTracker.Add(
			result.Usage.InputTokens,
			result.Usage.OutputTokens,
			result.Usage.CacheCreationInput,
			result.Usage.CacheReadInput,
		)

		// Show recovery info if context management occurred.
		if recovery != nil {
			if recovery.Pruned {
				fmt.Fprintf(a.stdout, "\n⟳ Context pruned: %d tool results trimmed to save context.\n", recovery.PrunedCount)
			}
			if recovery.CompactedCount > 0 {
				fmt.Fprintf(a.stdout, "\n⚠ Context compacted: %d messages summarized, %d recent messages preserved.\n",
					recovery.CompactedCount, recovery.PreservedCount)
			}
			if recovery.Flushed {
				fmt.Fprintf(a.stdout, "\n⚠ Emergency context flush: conversation restarted with summary + last request.\n")
			}
			fmt.Fprintln(a.stdout)
		}

		// Show LLM call metrics.
		metrics := formatLLMMetrics(result)
		if metrics != "" {
			fmt.Fprint(a.stdout, metrics)
		}

		// Check for stuck loops.
		if result.TextContent != "" {
			loopStatus := loopDetector.Record(result.TextContent)
			switch loopStatus {
			case conversation.LoopWarning:
				fmt.Fprintf(a.stdout, "\n⚠ Loop detected: agent may be stuck. Injecting intervention.\n\n")
			case conversation.LoopBreak:
				fmt.Fprintf(a.stdout, "\n✘ Loop detected: agent is stuck after %d similar responses. Breaking loop.\n\n",
					conversation.LoopHardThreshold)
				a.printSessionSummary()
				return nil
			}
		}

		// Print text output.
		if result.TextContent != "" {
			fmt.Fprint(a.stdout, result.TextContent)
		}

		// Save assistant message to session.
		if result.TextContent != "" {
			_ = a.session.AddMessage(session.ConversationMessage{
				Role: session.RoleAssistant,
				Content: []session.ContentBlock{
					{Type: session.ContentTypeText, Text: result.TextContent},
				},
			})
		}

		// If no tool calls, we're done.
		if len(result.ToolCalls) == 0 {
			fmt.Fprintln(a.stdout)
			a.printSessionSummary()
			return nil
		}

		// Build assistant message with tool_use blocks.
		var assistantBlocks []api.ContentBlock
		if result.ThinkingContent != "" {
			assistantBlocks = append(assistantBlocks, api.ContentBlock{
				Type: api.ContentTypeThinking,
				Text: result.ThinkingContent,
			})
		}
		if result.TextContent != "" {
			assistantBlocks = append(assistantBlocks, api.ContentBlock{
				Type: api.ContentTypeText,
				Text: result.TextContent,
			})
		}
		for _, tc := range result.ToolCalls {
			assistantBlocks = append(assistantBlocks, api.ContentBlock{
				Type:  api.ContentTypeToolUse,
				ID:    tc.ID,
				Name:  tc.Name,
				Input: tc.Input,
			})
			// Show tool detail in one-shot mode.
			fmt.Fprintf(a.stdout, "⚙ %s\n", toolDetail(tc.Name, tc.Input))
		}
		messages = append(messages, api.Message{
			Role:    api.RoleAssistant,
			Content: assistantBlocks,
		})

		// Execute tools (nil progress channel in one-shot mode).
		toolResults := rt.ExecuteTools(ctx, result.ToolCalls, nil)

		// Build tool result message and append to conversation.
		messages = append(messages, api.Message{
			Role:    api.RoleUser,
			Content: toolResults,
		})
	}

	fmt.Fprintln(a.stdout)
	a.printSessionSummary()
	return nil
}

// printSessionSummary outputs a summary of the session (time and tokens).
func (a *App) printSessionSummary() {
	duration := time.Since(a.sessionStart)
	summary := a.usageTracker.Summary()
	fmt.Fprintf(a.stdout, "\nSession Summary: %s | Duration: %s\n", summary, formatDuration(duration))
}

// formatDuration formats a duration in a human-readable way.
func formatDuration(d time.Duration) string {
	if d < time.Second {
		return fmt.Sprintf("%.0fms", float64(d)/float64(time.Millisecond))
	}
	if d < time.Minute {
		return fmt.Sprintf("%.1fs", d.Seconds())
	}
	minutes := int(d.Minutes())
	seconds := int(d.Seconds()) % 60
	return fmt.Sprintf("%dm%ds", minutes, seconds)
}

// RunTurn executes a single agentic turn (used by TUI).
// Returns the result and any tool results that need to be fed back.
func (a *App) RunTurn(ctx context.Context, messages []api.Message) (*conversation.TurnResult, error) {
	rt := a.conversationRuntime()
	a.turnIndex++
	return rt.InstrumentedTurn(ctx, messages, a.turnIndex)
}

// RunTurnWithRecovery executes a turn with automatic recovery from token limit errors.
// Returns the result, recovery info (if compaction occurred), and any error.
func (a *App) RunTurnWithRecovery(ctx context.Context, messages []api.Message) (*conversation.TurnResult, *conversation.RecoveryResult, error) {
	if a.provider == nil {
		return nil, nil, fmt.Errorf("no LLM provider configured; set ANTHROPIC_API_KEY, OPENAI_API_KEY, MOONSHOT_API_KEY, or KIMI_API_KEY")
	}
	rt := a.conversationRuntime()
	a.turnIndex++
	return rt.InstrumentedTurnWithRecovery(ctx, messages, a.turnIndex)
}

// RunTurnWithRecoveryStreaming is like RunTurnWithRecovery but accepts an
// event callback that receives streaming deltas (text.delta, thinking.delta,
// tool_use.start) as they arrive from the LLM. This allows the caller to
// render partial output in real time.
func (a *App) RunTurnWithRecoveryStreaming(
	ctx context.Context,
	messages []api.Message,
	onEvent func(eventType string, data map[string]any),
) (*conversation.TurnResult, *conversation.RecoveryResult, error) {
	if a.provider == nil {
		return nil, nil, fmt.Errorf("no LLM provider configured; set ANTHROPIC_API_KEY, OPENAI_API_KEY, MOONSHOT_API_KEY, or KIMI_API_KEY")
	}
	rt := a.conversationRuntime()
	if onEvent != nil {
		rt.SetEventCallback(onEvent)
	}
	a.turnIndex++
	return rt.InstrumentedTurnWithRecovery(ctx, messages, a.turnIndex)
}

// ExecuteTools runs tool calls and returns tool result content blocks.
// Progress events are sent to the progress channel if non-nil.
func (a *App) ExecuteTools(ctx context.Context, calls []conversation.ToolCall, progress chan<- taskqueue.TaskEvent) []api.ContentBlock {
	rt := a.conversationRuntime()
	return rt.ExecuteTools(ctx, calls, progress)
}

// CompactContext triggers an immediate compaction of the session context.
// Used by the compact_context tool to allow the agent to request compaction on demand.
func (a *App) CompactContext(ctx context.Context) (summary string, compactedCount int, preservedCount int, err error) {
	rt := a.conversationRuntime()
	messages := a.sessionMessages()
	result, err := rt.CompactNow(ctx, messages)
	if err != nil {
		return "", 0, 0, err
	}
	return result.Summary, result.CompactedCount, result.PreservedCount, nil
}

// sessionMessages converts session history to API messages.
func (a *App) sessionMessages() []api.Message {
	if a.session == nil {
		return nil
	}
	var msgs []api.Message
	for _, sm := range a.session.Messages {
		var blocks []api.ContentBlock
		for _, b := range sm.Content {
			blocks = append(blocks, api.ContentBlock{
				Type:      api.ContentType(b.Type),
				Text:      b.Text,
				ID:        b.ID,
				Name:      b.Name,
				Input:     b.Input,
				ToolUseID: b.ToolUseID,
				Content:   b.Content,
				IsError:   b.IsError,
			})
		}
		msgs = append(msgs, api.Message{
			Role:    api.MessageRole(sm.Role),
			Content: blocks,
		})
	}
	return msgs
}

// SwitchModel switches the active model and provider at runtime and persists
// the choice to the user config file so it survives restarts.
func (a *App) SwitchModel(name string) (string, error) {
	resolved := api.ResolveModelWithAliases(name, a.config.Aliases)
	providerCfg, err := api.DetectProvider(resolved)
	if err != nil {
		return "", fmt.Errorf("switch model: %w", err)
	}
	a.provider = api.NewProvider(providerCfg)
	a.config.Model = resolved
	a.providerKind = providerCfg.DisplayKind()

	// Persist to user config so the choice survives restarts.
	if a.userConfigPath != "" {
		if err := config.SetLocalConfigField(a.userConfigPath, "model", resolved); err != nil {
			fmt.Fprintf(a.stdout, "warning: could not save model preference: %v\n", err)
		}
	}

	return fmt.Sprintf("Switched to %s (%s)", resolved, a.providerKind), nil
}

// Model returns the current model ID.
func (a *App) Model() string { return a.config.Model }

// ProviderKind returns the current provider kind.
func (a *App) ProviderKind() string { return a.providerKind }

// InPlanMode returns whether plan mode is active.
func (a *App) InPlanMode() bool {
	if a.planMode == nil {
		return false
	}
	return a.planMode.InPlanMode()
}

// parentAgentMode returns the current agent mode as a tools.AgentMode.
// Used by the agent spawner to enforce plan-mode constraints on subagents.
func (a *App) parentAgentMode() tools.AgentMode {
	if a.InPlanMode() {
		return tools.ModePlan
	}
	return tools.ModeBuild
}

// Commands returns the command registry.
func (a *App) Commands() *commands.Registry { return a.commands }

// ExecuteCommand runs a slash command by name.
func (a *App) ExecuteCommand(ctx context.Context, name string, args string) (string, error) {
	return a.commands.Execute(ctx, name, args)
}

// Config returns the current configuration.
func (a *App) Config() *config.Config { return a.config }

// Version returns the application version string.
func (a *App) Version() string { return a.version }

// SessionID returns the current session ID.
func (a *App) SessionID() string {
	if a.session == nil {
		return ""
	}
	return a.session.ID
}

// MessageCount returns the number of messages in the current session.
func (a *App) MessageCount() int {
	if a.session == nil {
		return 0
	}
	return a.session.MessageCount()
}

// SessionMessages returns the current session messages in API format.
func (a *App) SessionMessages() []api.Message {
	return a.sessionMessages()
}

// Session returns the underlying session.
func (a *App) Session() *session.Session { return a.session }

// ConversationRuntime creates a conversation.Runtime from the current app state.
// Exported for use by the service layer.
func (a *App) ConversationRuntime() *conversation.Runtime {
	return a.conversationRuntime()
}

// RetryTurn removes the last assistant turn and returns the last user message.
func (a *App) RetryTurn() (string, error) {
	if a.session == nil {
		return "", fmt.Errorf("no active session")
	}
	lastMsg := a.session.LastUserMessage()
	removed := a.session.RemoveLastTurn()
	if removed == 0 {
		return "", fmt.Errorf("no turn to retry")
	}
	return lastMsg, nil
}

// RevertFiles reverts uncommitted file changes using git checkout.
func (a *App) RevertFiles() (string, error) {
	ge := git.NewGitExec(nil)
	out, err := ge.RunOutput(context.Background(), a.workDir, "diff", "--stat")
	if err != nil {
		return "", fmt.Errorf("git diff failed: %w", err)
	}
	if out == "" {
		return "No uncommitted changes to revert.", nil
	}

	stats := out

	if err := ge.RunCheck(context.Background(), a.workDir, "checkout", "."); err != nil {
		return "", fmt.Errorf("git checkout failed: %w", err)
	}

	return fmt.Sprintf("Reverted uncommitted changes:\n%s", stats), nil
}

// UsageTracker returns the usage tracker.
func (a *App) UsageTracker() *usage.Tracker { return a.usageTracker }

// AgentPool returns the agent pool for progress tracking.
func (a *App) AgentPool() *agentpool.Pool { return a.agentPool }

// TaskRegistry returns the background task registry.
func (a *App) TaskRegistry() *task.Registry { return a.taskRegistry }

// maxInitToolIterations limits the number of tool-use round-trips during /init.
const maxInitToolIterations = 8

// runAgenticInit runs a mini agentic loop for /init with graph tool support.
// The LLM can query the code knowledge graph during AGENTS.md generation.
// Returns nil if the conversation runtime can't be created (missing deps).
func (a *App) runAgenticInit(ctx context.Context, systemPrompt, userPrompt string, onDelta func(string), onUsage func(int, int, int, int)) (string, error) {
	if a.provider == nil || a.toolRegistry == nil || a.config == nil {
		return "", fmt.Errorf("agentic init requires provider, tool registry, and config")
	}

	// Create a fresh session for the init loop (prevents context bloat).
	initSessionDir := filepath.Join(os.TempDir(), fmt.Sprintf("ycode-init-%d", time.Now().UnixNano()))
	sess, err := session.New(initSessionDir)
	if err != nil {
		return "", fmt.Errorf("create init session: %w", err)
	}
	defer os.RemoveAll(initSessionDir)

	// Create conversation runtime with tool access.
	rt := conversation.NewRuntime(
		a.config,
		a.provider,
		sess,
		a.toolRegistry,
		a.promptCtx,
	)

	// Build initial messages with system prompt in user message
	// (single-shot pattern: system role handled by the runtime's system prompt assembly).
	messages := []api.Message{
		{
			Role: api.RoleUser,
			Content: []api.ContentBlock{
				{Type: api.ContentTypeText, Text: systemPrompt + "\n\n" + userPrompt},
			},
		},
	}

	// Run the mini agentic loop.
	var allText strings.Builder
	for i := 0; i < maxInitToolIterations; i++ {
		result, _, err := rt.TurnWithRecovery(ctx, messages)
		if err != nil {
			return allText.String(), fmt.Errorf("init turn %d: %w", i+1, err)
		}

		// Collect text output.
		if result.TextContent != "" {
			allText.WriteString(result.TextContent)
			if onDelta != nil {
				onDelta(result.TextContent)
			}
		}

		// Track usage.
		if onUsage != nil && (result.Usage.InputTokens > 0 || result.Usage.OutputTokens > 0) {
			onUsage(result.Usage.InputTokens, result.Usage.OutputTokens, result.Usage.CacheCreationInput, result.Usage.CacheReadInput)
		}

		// No tool calls means the LLM is done.
		if len(result.ToolCalls) == 0 {
			break
		}

		slog.Info("init: LLM using tools", "turn", i+1, "tools", len(result.ToolCalls))

		// Build assistant message with tool_use blocks.
		var assistantBlocks []api.ContentBlock
		if result.ThinkingContent != "" {
			assistantBlocks = append(assistantBlocks, api.ContentBlock{
				Type: api.ContentTypeThinking,
				Text: result.ThinkingContent,
			})
		}
		if result.TextContent != "" {
			assistantBlocks = append(assistantBlocks, api.ContentBlock{
				Type: api.ContentTypeText,
				Text: result.TextContent,
			})
		}
		for _, tc := range result.ToolCalls {
			assistantBlocks = append(assistantBlocks, api.ContentBlock{
				Type:  api.ContentTypeToolUse,
				ID:    tc.ID,
				Name:  tc.Name,
				Input: tc.Input,
			})
		}
		messages = append(messages, api.Message{
			Role:    api.RoleAssistant,
			Content: assistantBlocks,
		})

		// Execute tools and append results.
		toolResults := rt.ExecuteTools(ctx, result.ToolCalls, nil)
		messages = append(messages, api.Message{
			Role:    api.RoleUser,
			Content: toolResults,
		})
	}

	return allText.String(), nil
}

// HasCommand checks if a slash command exists in the registry.
func (a *App) HasCommand(name string) bool {
	_, ok := a.commands.Get(name)
	return ok
}

// SetProgressFunc sets the progress callback function (called by TUI).
func (a *App) SetProgressFunc(fn func(message string)) {
	a.progressFunc = fn
}

// LogProgress logs a progress message via the registered callback.
func (a *App) LogProgress(message string) {
	if a.progressFunc != nil {
		a.progressFunc(message)
	}
}

// SetDeltaFunc sets the delta callback function for streaming text (called by TUI).
func (a *App) SetDeltaFunc(fn func(text string)) {
	a.deltaFunc = fn
}

// LogDelta streams a text delta via the registered callback.
func (a *App) LogDelta(text string) {
	if a.deltaFunc != nil {
		a.deltaFunc(text)
	}
}

// TurnIndex returns the current turn index and increments it.
func (a *App) NextTurnIndex() int {
	a.turnIndex++
	return a.turnIndex
}

// RunInteractiveWithClient starts the interactive TUI with an optional client
// for event-driven messaging via the service layer and bus.
func (a *App) RunInteractiveWithClient(ctx context.Context, cl agentClient) error {
	m := NewTUIModel(a)
	m.cl = cl
	p := tea.NewProgram(m, tea.WithAltScreen(), tea.WithContext(ctx))
	m.SetProgram(p)
	suppressLogOutput(p)

	if a.toolRegistry != nil {
		prompter := NewTUIPrompter(p)
		a.toolRegistry.SetPermissionPrompter(prompter.Prompt)
		a.toolRegistry.SetTTYExecutor(NewTUITTYExecutor(p))
	}

	_, err := p.Run()
	if err != nil {
		return err
	}
	a.printSessionSummary()
	if a.storage != nil {
		a.storage.Close()
	}
	return nil
}

// RunInteractive starts the interactive TUI.
func (a *App) RunInteractive(ctx context.Context) error {
	m := NewTUIModel(a)
	p := tea.NewProgram(m, tea.WithAltScreen(), tea.WithContext(ctx))
	m.SetProgram(p)
	suppressLogOutput(p)

	// Wire TUI-based permission prompter and TTY executor into the tool
	// registry so that tools can ask for permission and run interactive
	// commands (ssh, sudo, etc.) through the TUI.
	if a.toolRegistry != nil {
		prompter := NewTUIPrompter(p)
		a.toolRegistry.SetPermissionPrompter(prompter.Prompt)
		a.toolRegistry.SetTTYExecutor(NewTUITTYExecutor(p))
	}

	_, err := p.Run()
	if err != nil {
		return err
	}

	// Print session summary after TUI exits.
	a.printSessionSummary()

	// Storage and OTEL cleanup happen in Close() (called via defer)
	// with appropriate timeouts, so we don't duplicate it here.
	return nil
}

// suppressLogOutput redirects all log output away from stderr to prevent
// corruption of the bubbletea alt-screen display. When a tea.Program is
// provided, log entries are routed through the TUI viewport with formatted
// elapsed time and level indicators. Otherwise, logs are silenced entirely.
func suppressLogOutput(program *tea.Program) {
	if program != nil {
		slog.SetDefault(slog.New(newTUILogHandler(program, slog.LevelInfo)))
	} else {
		slog.SetDefault(slog.New(slog.NewTextHandler(io.Discard, nil)))
	}
	log.SetOutput(io.Discard)
	grpclog.SetLoggerV2(grpclog.NewLoggerV2(io.Discard, io.Discard, io.Discard))
}

// Close shuts down the application and releases resources.
// RegisterCleanup adds a function to be called during Close.
// Used for OTEL shutdown, context cancellation, and other teardown.
func (a *App) RegisterCleanup(fn func()) {
	a.cleanupFuncs = append(a.cleanupFuncs, fn)
}

func (a *App) Close() error {
	// Persist persona updates from the session.
	if a.currentPersona != nil && a.memoryManager != nil {
		memory.UpdatePersonaFromSession(a.currentPersona)
		if store := a.memoryManager.GlobalStore(); store != nil {
			if err := memory.SavePersona(store, a.currentPersona); err != nil {
				slog.Debug("persona save on close", "error", err)
			}
		}
	}

	// Run all cleanup (OTEL flush, rootCancel, storage) with a hard deadline.
	// The process is exiting — don't hang waiting for gRPC flushes or lock releases.
	done := make(chan struct{})
	go func() {
		// Run cleanup functions in reverse order (LIFO).
		for i := len(a.cleanupFuncs) - 1; i >= 0; i-- {
			a.cleanupFuncs[i]()
		}
		if a.storage != nil {
			a.storage.Close()
		}
		close(done)
	}()

	select {
	case <-done:
	case <-time.After(1 * time.Second):
		// Best-effort — process is exiting anyway.
	}
	return nil
}
