package main

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"os"
	"os/signal"

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

	if f.workDir == "" {
		var err error
		f.workDir, err = os.Getwd()
		if err != nil {
			return fmt.Errorf("getwd: %w", err)
		}
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

// runShellOneShot dispatches a single -c command and exits with the result's
// exit code. Honors --json and --agent.
func runShellOneShot(rt *shell.ShellRuntime, f *shellFlags) error {
	d := shell.NewDispatcher(rt)
	sink := shell.WriterSink{StdoutW: os.Stdout, StderrW: os.Stderr}

	intent, err := shell.Classify(f.command)
	if err != nil {
		fmt.Fprintf(os.Stderr, "shell: classify: %v\n", err)
		os.Exit(2)
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
	defer signal.Stop(sigCh)
	defer cancel()

	if f.json {
		// Phase B1 wires this; for now error gracefully so the flag is
		// declared but unimplemented behavior is explicit.
		fmt.Fprintln(os.Stderr, "shell: --json envelope not yet implemented (Phase B1)")
	}

	res, derr := d.Dispatch(ctx, intent, sink)
	if derr != nil {
		fmt.Fprintf(os.Stderr, "shell: dispatch error: %v\n", derr)
	}

	if f.agent {
		emitHints(rt, f.command, &res)
	}

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
func runShellREPL(rt *shell.ShellRuntime, f *shellFlags) error {
	d := shell.NewDispatcher(rt)
	sink := shell.WriterSink{StdoutW: os.Stdout, StderrW: os.Stderr}
	scanner := bufio.NewScanner(os.Stdin)
	scanner.Buffer(make([]byte, 0, 64*1024), 1024*1024)

	if !f.quiet {
		fmt.Fprintln(os.Stderr, "ycode shell (skeleton — type /help, ^D to exit)")
	}
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
			return nil
		}
		line := scanner.Text()

		intent, err := shell.Classify(line)
		if err != nil {
			if errors.Is(err, shell.ErrSentinelInPipeline) {
				fmt.Fprintf(os.Stderr, "shell: %v\n", err)
				continue
			}
			fmt.Fprintf(os.Stderr, "shell: classify: %v\n", err)
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
		if f.agent {
			emitHints(rt, line, &res)
		}
	}
}
