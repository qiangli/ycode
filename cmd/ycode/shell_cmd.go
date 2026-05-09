package main

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"os"
	"os/signal"
	"sort"
	"strconv"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/spf13/cobra"
	"golang.org/x/term"

	"github.com/qiangli/ycode/internal/api"
	"github.com/qiangli/ycode/internal/commands"
	"github.com/qiangli/ycode/internal/shell"
	"github.com/qiangli/ycode/internal/shell/agentmode"
	"github.com/qiangli/ycode/internal/shell/builtins"
)

// shellFlags collects the per-invocation knobs for `ycode shell`.
type shellFlags struct {
	workDir       string
	permission    string
	noTUI         bool
	command       string // -c "..." one-shot
	quiet         bool   // suppress banner/prompt
	agent         bool   // agent posture: implies quiet, sets env, augments output
	suggest       string // --suggest "..." returns hints only, no exec
	json          bool   // --json envelope output (Phase B1)
	manifestOnly  bool   // --manifest emits the JSON capability dump
	sandbox       bool   // route external commands through podman (Phase B4)
	allowedDirs   []string
	timeoutString string
	offline       bool
	auditLog      string
	mine          string // --mine missed|stats|raw — mining sink reports
	mineFile      string // --mine-history-file overrides the JSONL path
}

func newShellCmd() *cobra.Command {
	f := &shellFlags{}

	cmd := &cobra.Command{
		Use:   "shell",
		Short: "Run an interactive agentic shell (bash + LLM-mediated UX)",
		Long: `ycode shell is an interactive shell that is bash-compatible at the
command layer and LLM-mediated at the UX layer. Bare words go through PATH
exactly like /bin/bash. Agentic features sit above bash via sentinel prefixes:

  /<word>   built-in slash command (e.g. /help)
  @<name>   skill from registry (e.g. @review)
  !<text>   one-shot agent with shell context
  ?<text>   cheap LLM Q&A, no tools

Sentinels only fire as the first non-whitespace token of a logical line.
Inside quotes, heredocs, command-substitution, mid-line, or pipelines they
are literal text. PATH always wins for bare words; nobody can ship a skill
named 'ls' and break muscle memory.

Modes:
  ycode shell                       interactive Bubble Tea TUI
  ycode shell --no-tui              plain stdin/stdout REPL
  ycode shell -c "command"          non-interactive one-shot (matches bash -c)
  ycode shell --manifest            emit JSON capability catalog and exit
  ycode shell --suggest "command"   emit agent-mode hints for command and exit

Use --agent to enable agent-friendly output augmentation (auto-quiet,
hints to stderr, YCODE_SHELL_AGENT=1 env var). See docs/shell-agent.md.`,
		RunE: func(cmd *cobra.Command, args []string) error {
			return runShellCmd(cmd, args, f)
		},
	}

	cmd.Flags().StringVar(&f.workDir, "workdir", "", "Initial working directory (defaults to current)")
	cmd.Flags().StringVar(&f.permission, "permission", "danger-full-access", "Permission mode")
	cmd.Flags().BoolVar(&f.noTUI, "no-tui", false, "Run a plain stdin/stdout REPL instead of the Bubble Tea TUI")
	cmd.Flags().StringVarP(&f.command, "command", "c", "", "Run a single command and exit (matches bash -c)")
	cmd.Flags().BoolVar(&f.quiet, "quiet", false, "Suppress banner and prompt (auto-on when stdout is not a TTY)")
	cmd.Flags().BoolVar(&f.agent, "agent", false, "Agent posture: implies --quiet, sets YCODE_SHELL_AGENT=1, augments output with hints")
	cmd.Flags().StringVar(&f.suggest, "suggest", "", "Emit hints for the given command without executing it")
	cmd.Flags().BoolVar(&f.json, "json", false, "Wrap each command result as a JSON envelope on stdout")
	cmd.Flags().BoolVar(&f.manifestOnly, "manifest", false, "Emit the JSON capability catalog and exit")
	cmd.Flags().BoolVar(&f.sandbox, "sandbox", false, "Route external commands through a podman sandbox")
	cmd.Flags().StringSliceVar(&f.allowedDirs, "allowed-dirs", nil, "Restrict file ops to these directories (comma-separated)")
	cmd.Flags().StringVar(&f.timeoutString, "timeout", "", "Per-command timeout (e.g. 30s, 5m)")
	cmd.Flags().BoolVar(&f.offline, "offline", false, "Block all LLM calls (!/?/skill-LLM)")
	cmd.Flags().StringVar(&f.auditLog, "audit-log", "", "Append every dispatched intent to this JSONL file")
	cmd.Flags().StringVar(&f.mine, "mine", "", "Report on the catalog mining sink: missed | stats | raw")
	cmd.Flags().StringVar(&f.mineFile, "mine-history-file", "", "Override the mining sink path (defaults to $YCODE_SHELL_HISTORY_FILE or ~/.agents/ycode/shell-history.jsonl)")

	return cmd
}

func runShellCmd(_ *cobra.Command, _ []string, f *shellFlags) error {
	// --agent implies --quiet and sets the env var for child processes.
	if f.agent {
		f.quiet = true
		_ = os.Setenv("YCODE_SHELL_AGENT", "1")
	}
	// Auto-quiet when stdout isn't a TTY (piped, redirected, or under an
	// agent that didn't pass --agent explicitly).
	if !f.quiet && !term.IsTerminal(int(os.Stdout.Fd())) {
		f.quiet = true
	}
	// When neither -c nor --no-tui is given, the default branch is the
	// Bubble Tea TUI — which needs a controlling TTY. If stdin is not a
	// TTY (heredoc, pipe, or a long-lived foreign-agent bash subprocess
	// feeding commands on stdin), opening /dev/tty fails and the wrapper
	// exits 1 before running anything. Real bash handles this by reading
	// commands from stdin in non-interactive mode; mirror that behaviour
	// by routing to the --no-tui REPL, which already does exactly that.
	if f.command == "" && !f.noTUI && !term.IsTerminal(int(os.Stdin.Fd())) {
		f.noTUI = true
		f.quiet = true
	}

	if f.workDir == "" {
		var err error
		f.workDir, err = os.Getwd()
		if err != nil {
			return fmt.Errorf("getwd: %w", err)
		}
	}

	if f.sandbox {
		fmt.Fprintln(os.Stderr, "# ycode shell: --sandbox is advisory in this build; use `yc sandbox -- <cmd>` per-call for podman isolation")
	}
	if len(f.allowedDirs) > 0 {
		fmt.Fprintln(os.Stderr, "# ycode shell: --allowed-dirs is advisory; full VFS enforcement is not yet wired in shell mode")
	}

	provider, model := buildShellProvider(f.offline)

	rt, err := shell.New(shell.Options{
		WorkDir:    f.workDir,
		Permission: f.permission,
		Registry:   buildShellRegistry(),
		Skills:     shell.NewSkillResolver(),
		Provider:   provider,
		Model:      model,
	})
	if err != nil {
		return fmt.Errorf("new shell runtime: %w", err)
	}
	defer rt.Close()

	// Install the yc <verb> built-in dispatcher before the standard
	// shell-mode exec handler so foreign agents (and humans) can use
	// `yc symbols ...`, `yc repomap`, etc. as bash-callable commands.
	rt.Session().AddExecMiddleware(builtins.Handler())

	// One-shot pre-exec subcommands (don't need a runner / TTY).
	if f.manifestOnly {
		return runShellManifest(rt)
	}
	if f.suggest != "" {
		return runShellSuggest(rt, f)
	}
	if f.mine != "" {
		return runShellMine(f)
	}

	// One-shot -c "command".
	if f.command != "" {
		rt.Session().SetTTYRunner(shell.NewPTYManager())
		return runShellOneShot(rt, f)
	}

	// Interactive paths.
	if f.noTUI {
		rt.Session().SetTTYRunner(shell.NewPTYManager())
		return runShellREPL(rt, f)
	}
	return runShellTUI(rt)
}

// runShellTUI launches the Bubble Tea shell model.
func runShellTUI(rt *shell.ShellRuntime) error {
	model := shell.NewShellModel(rt)
	prog := tea.NewProgram(model)
	rt.Session().SetTTYRunner(shell.NewTUITTYRunner(prog))
	_, err := prog.Run()
	return err
}

func buildShellRegistry() *commands.Registry {
	r := commands.NewRegistry()

	r.Register(&commands.Spec{
		Name:        "help",
		Description: "Show shell help and the sentinel reference",
		ShellSafe:   true,
		Handler: func(_ context.Context, _ string) (string, error) {
			return shellHelpText(), nil
		},
	})
	r.Register(&commands.Spec{
		Name:        "version",
		Description: "Show ycode version",
		ShellSafe:   true,
		Handler: func(_ context.Context, _ string) (string, error) {
			return version, nil
		},
	})
	r.Register(&commands.Spec{
		Name:        "clear",
		Description: "Clear the screen (sends ANSI clear sequence to stdout)",
		ShellSafe:   true,
		Handler: func(_ context.Context, _ string) (string, error) {
			return "\x1b[2J\x1b[H", nil
		},
	})
	return r
}

func buildShellProvider(offline bool) (api.Provider, string) {
	if offline {
		return nil, ""
	}
	model := modelFlag
	if model == "" {
		model = "claude-sonnet-4-6"
	}
	cfg, err := api.DetectProvider(model)
	if err != nil || cfg == nil {
		return nil, model
	}
	return api.NewProvider(cfg), model
}

func shellHelpText() string {
	return "ycode shell — bash-compatible agentic shell\n\n" +
		"Sentinels (first non-whitespace token only):\n" +
		"  /<word>    slash command — try /help, /version, /clear\n" +
		"  @<name>    skill from registry\n" +
		"  @<path>    skill from filesystem path\n" +
		"  !<text>    one-shot agent with shell context\n" +
		"  ?<text>    cheap LLM Q&A\n\n" +
		"Built-ins (bash-callable, no MCP setup):\n" +
		"  yc help    list yc <verb> built-ins\n" +
		"  yc manifest emit JSON capability catalog\n\n" +
		"Anything else is bash. Pipelines, heredocs, redirections, env vars,\n" +
		"functions, set options, and aliases all persist across submissions.\n"
}

// applyTimeout wraps ctx with f.timeoutString (parsed) when set.
func applyTimeout(ctx context.Context, f *shellFlags) (context.Context, context.CancelFunc, error) {
	if f.timeoutString == "" {
		return ctx, func() {}, nil
	}
	d, err := time.ParseDuration(f.timeoutString)
	if err != nil {
		return nil, nil, fmt.Errorf("invalid --timeout %q: %w", f.timeoutString, err)
	}
	c, cancel := context.WithTimeout(ctx, d)
	return c, cancel, nil
}

// auditEntry is one row in --audit-log JSONL output.
type auditEntry struct {
	Time     time.Time `json:"time"`
	Command  string    `json:"command"`
	ExitCode int       `json:"exit_code"`
	WorkDir  string    `json:"workdir"`
	Sandbox  bool      `json:"sandbox,omitempty"`
	Offline  bool      `json:"offline,omitempty"`
	Source   string    `json:"source"` // "one-shot" | "repl" | "tui"
}

func appendAudit(path string, entry auditEntry) {
	if path == "" {
		return
	}
	f, err := os.OpenFile(path, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o644)
	if err != nil {
		fmt.Fprintf(os.Stderr, "shell: audit-log open: %v\n", err)
		return
	}
	defer f.Close()
	_ = json.NewEncoder(f).Encode(entry)
}

// runShellOneShot dispatches a single -c command and exits with the result's
// exit code. Honors --json, --agent, --timeout, --audit-log, --offline.
func runShellOneShot(rt *shell.ShellRuntime, f *shellFlags) error {
	ctx, cancelTimeout, terr := applyTimeout(context.Background(), f)
	if terr != nil {
		fmt.Fprintln(os.Stderr, terr)
		os.Exit(2)
	}
	defer cancelTimeout()
	ctx, cancel := context.WithCancel(ctx)
	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, os.Interrupt)
	go func() {
		select {
		case <-sigCh:
			cancel()
		case <-ctx.Done():
		}
	}()
	defer signal.Stop(sigCh)
	defer cancel()

	// Pre-exec hints (matched against the raw command string).
	var preHints []shell.Hint
	if f.agent {
		preHints = agentmode.Suggest(rt, f.command)
	}

	if f.json {
		env := shell.DispatchEnvelope(ctx, rt, f.command, preHints)
		if err := shell.WriteEnvelopeJSON(env, os.Stdout); err != nil {
			fmt.Fprintf(os.Stderr, "shell: write envelope: %v\n", err)
		}
		appendAudit(f.auditLog, auditEntry{
			Time:     time.Now(),
			Command:  f.command,
			ExitCode: env.ExitCode,
			WorkDir:  rt.WorkDir(),
			Sandbox:  f.sandbox,
			Offline:  f.offline,
			Source:   "one-shot",
		})
		if env.ExitCode != 0 {
			os.Exit(env.ExitCode)
		}
		return nil
	}

	// Plain mode: dispatch live, augment with hints on stderr.
	d := shell.NewDispatcher(rt)
	sink := shell.WriterSink{StdoutW: os.Stdout, StderrW: os.Stderr}

	dispatchStart := time.Now()
	dispatchCtx, endSpan := shell.StartSpan(ctx, "ycode.shell.dispatch")

	intent, err := shell.Classify(f.command)
	if err != nil {
		endSpan(err, "kind", "classify_error")
		fmt.Fprintf(os.Stderr, "shell: classify: %v\n", err)
		os.Exit(2)
	}
	res, derr := d.Dispatch(dispatchCtx, intent, sink)
	if derr != nil {
		fmt.Fprintf(os.Stderr, "shell: dispatch error: %v\n", derr)
	}

	durationMs := float64(time.Since(dispatchStart).Microseconds()) / 1000.0
	shell.ObserveCommandDuration(intent.Kind.String(), durationMs)
	endSpan(derr, "kind", intent.Kind.String(), "exit_code", strconv.Itoa(res.ExitCode))
	// Debug, not Info: foreign agents (Claude Code, Codex, …) often treat any
	// non-empty stderr as failure regardless of exit code, so a per-command
	// dispatch log on the default stderr handler poisons every successful
	// `ycode shell -c "<cmd>"` run. The OTEL span (endSpan) already captures
	// the same fields when telemetry is wired.
	slog.Debug("shell.command dispatched",
		"intent", intent.Kind.String(),
		"exit_code", res.ExitCode,
		"duration_ms", durationMs,
	)

	if f.agent {
		for _, h := range preHints {
			fmt.Fprintf(os.Stderr, "# ycode hint [%s]: %s\n", h.Category, h.Message)
		}
		for _, h := range agentmode.SuggestPost(rt, res.ExitCode, "") {
			fmt.Fprintf(os.Stderr, "# ycode hint [%s]: %s\n", h.Category, h.Message)
		}
	}

	appendAudit(f.auditLog, auditEntry{
		Time:     time.Now(),
		Command:  f.command,
		ExitCode: res.ExitCode,
		WorkDir:  rt.WorkDir(),
		Sandbox:  f.sandbox,
		Offline:  f.offline,
		Source:   "one-shot",
	})

	if res.ExitCode != 0 {
		os.Exit(res.ExitCode)
	}
	return nil
}

// emitHints prints any matching agent-mode hints to stderr. Pre-exec
// hints fire on the raw command; post-exec hints fire on (exitCode, stderr)
// — but for the skeleton we only have stderr captured in --json mode, so
// the post-exec catalog runs against res.ExitCode only.
func emitHints(rt *shell.ShellRuntime, command string, res *shell.Result) {
	if res == nil {
		return
	}
	for _, h := range agentmode.Suggest(rt, command) {
		fmt.Fprintf(os.Stderr, "# ycode hint [%s]: %s\n", h.Category, h.Message)
	}
	for _, h := range agentmode.SuggestPost(rt, res.ExitCode, "") {
		fmt.Fprintf(os.Stderr, "# ycode hint [%s]: %s\n", h.Category, h.Message)
	}
}

// runShellManifest emits the JSON capability catalog and exits.
func runShellManifest(rt *shell.ShellRuntime) error {
	return shell.WriteManifest(rt, os.Stdout)
}

// runShellSuggest emits hints for f.suggest and exits without executing.
func runShellSuggest(rt *shell.ShellRuntime, f *shellFlags) error {
	return shell.WriteSuggestions(rt, f.suggest, os.Stdout)
}

// runShellREPL is the --no-tui debug REPL. Reads lines from stdin,
// classifies each via the sentinel parser, dispatches, prints the
// result. ^C cancels the current dispatch; ^D (EOF) exits cleanly.
//
// On EOF, exits with the last command's exit status — same as real
// bash in non-interactive mode (`bash <<EOF\nfalse\nEOF` → 1). This
// matters when ycode shell is invoked as a foreign agent's $SHELL and
// commands are fed via stdin: the agent expects the parent process's
// exit code to mirror the last command's.
func runShellREPL(rt *shell.ShellRuntime, f *shellFlags) error {
	d := shell.NewDispatcher(rt)
	sink := shell.WriterSink{StdoutW: os.Stdout, StderrW: os.Stderr}
	scanner := bufio.NewScanner(os.Stdin)
	scanner.Buffer(make([]byte, 0, 64*1024), 1024*1024)

	if !f.quiet {
		fmt.Fprintln(os.Stderr, "ycode shell (skeleton — type /help, ^D to exit)")
	}
	lastExit := 0
	for {
		if !f.quiet {
			fmt.Fprintf(os.Stdout, "ycode:%s$ ", rt.WorkDir())
		}
		if !scanner.Scan() {
			if err := scanner.Err(); err != nil {
				return err
			}
			if !f.quiet {
				fmt.Fprintln(os.Stderr)
			}
			if lastExit != 0 {
				os.Exit(lastExit)
			}
			return nil
		}
		line := scanner.Text()

		intent, err := shell.Classify(line)
		if err != nil {
			if errors.Is(err, shell.ErrSentinelInPipeline) {
				fmt.Fprintf(os.Stderr, "shell: %v\n", err)
				lastExit = 2
				continue
			}
			fmt.Fprintf(os.Stderr, "shell: classify: %v\n", err)
			lastExit = 2
			continue
		}

		ctx, cancel := context.WithCancel(context.Background())
		sigCh := make(chan os.Signal, 1)
		signal.Notify(sigCh, os.Interrupt)
		go func() {
			select {
			case <-sigCh:
				cancel()
			case <-ctx.Done():
			}
		}()

		res, derr := d.Dispatch(ctx, intent, sink)
		signal.Stop(sigCh)
		close(sigCh)
		cancel()

		if derr != nil {
			fmt.Fprintf(os.Stderr, "shell: dispatch error: %v\n", derr)
		}
		lastExit = res.ExitCode
		if f.agent {
			emitHints(rt, line, &res)
		}
	}
}

// runShellMine reports on the catalog mining sink (--mine missed|stats|raw).
// The sink is the JSONL file written by agentmode.RecordPre/RecordPost on
// every Suggest/SuggestPost call.
func runShellMine(f *shellFlags) error {
	path := f.mineFile
	if path == "" {
		path = agentmode.HistoryPath()
	}
	if path == "" {
		return fmt.Errorf("mine: cannot resolve history path (set --mine-history-file or $YCODE_SHELL_HISTORY_FILE)")
	}
	switch f.mine {
	case "missed":
		return mineReportMissed(path)
	case "stats":
		return mineReportStats(path)
	case "raw":
		return mineReportRaw(path)
	default:
		return fmt.Errorf("mine: unknown action %q (want missed | stats | raw)", f.mine)
	}
}

func mineOpen(path string) (*os.File, error) {
	fp, err := os.Open(path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, fmt.Errorf("mine: history file not found: %s\n  the sink populates on `ycode shell --suggest` / `--agent` runs", path)
		}
		return nil, fmt.Errorf("mine: open %s: %w", path, err)
	}
	return fp, nil
}

func mineReportMissed(path string) error {
	fp, err := mineOpen(path)
	if err != nil {
		return err
	}
	defer fp.Close()
	entries, err := agentmode.Missed(fp)
	if err != nil {
		return fmt.Errorf("mine: scan: %w", err)
	}
	if len(entries) == 0 {
		fmt.Println("# no un-hinted commands recorded")
		return nil
	}
	fmt.Printf("%-6s  %s\n", "COUNT", "COMMAND")
	for _, e := range entries {
		sample := e.Sample
		if len(sample) > 100 {
			sample = sample[:97] + "..."
		}
		fmt.Printf("%-6d  %s\n", e.Count, sample)
	}
	return nil
}

func mineReportStats(path string) error {
	fp, err := mineOpen(path)
	if err != nil {
		return err
	}
	defer fp.Close()
	s, err := agentmode.ComputeStats(fp)
	if err != nil {
		return fmt.Errorf("mine: scan: %w", err)
	}
	fmt.Printf("records:        %d (pre=%d post=%d)\n", s.TotalRecords, s.PreRecords, s.PostRecords)
	fmt.Printf("pre hit/miss:   %d / %d\n", s.HitPre, s.MissPre)
	fmt.Printf("pre hit-rate:   %.1f%%\n", s.HitRatePre*100)
	if len(s.ByID) == 0 {
		return nil
	}
	type kv struct {
		k string
		v int
	}
	pairs := make([]kv, 0, len(s.ByID))
	for k, v := range s.ByID {
		pairs = append(pairs, kv{k, v})
	}
	sort.Slice(pairs, func(i, j int) bool {
		if pairs[i].v != pairs[j].v {
			return pairs[i].v > pairs[j].v
		}
		return pairs[i].k < pairs[j].k
	})
	fmt.Println()
	fmt.Println("top hint IDs:")
	limit := min(10, len(pairs))
	for _, p := range pairs[:limit] {
		fmt.Printf("  %5d  %s\n", p.v, p.k)
	}
	return nil
}

func mineReportRaw(path string) error {
	fp, err := mineOpen(path)
	if err != nil {
		return err
	}
	defer fp.Close()
	_, err = io.Copy(os.Stdout, fp)
	return err
}
