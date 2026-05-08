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

	"github.com/qiangli/ycode/internal/api"
	"github.com/qiangli/ycode/internal/commands"
	"github.com/qiangli/ycode/internal/shell"
)

func newShellCmd() *cobra.Command {
	var (
		workDir    string
		permission string
		noTUI      bool
	)

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
named 'ls' and break muscle memory.`,
		RunE: func(cmd *cobra.Command, args []string) error {
			return runShellCmd(cmd, args, workDir, permission, noTUI)
		},
	}

	cmd.Flags().StringVar(&workDir, "workdir", "", "Initial working directory (defaults to current)")
	cmd.Flags().StringVar(&permission, "permission", "danger-full-access", "Permission mode (default: danger-full-access — user is the operator)")
	cmd.Flags().BoolVar(&noTUI, "no-tui", false, "Run a plain stdin/stdout REPL instead of the Bubble Tea TUI")

	return cmd
}

func runShellCmd(_ *cobra.Command, _ []string, workDir, perm string, noTUI bool) error {
	if workDir == "" {
		var err error
		workDir, err = os.Getwd()
		if err != nil {
			return fmt.Errorf("getwd: %w", err)
		}
	}

	provider, model := buildShellProvider()

	rt, err := shell.New(shell.Options{
		WorkDir:    workDir,
		Permission: perm,
		Registry:   buildShellRegistry(),
		Skills:     shell.NewSkillResolver(),
		Provider:   provider,
		Model:      model,
	})
	if err != nil {
		return fmt.Errorf("new shell runtime: %w", err)
	}
	defer rt.Close()

	// Install PTY support so commands like vi / less / top get a real
	// controlling terminal when invoked from --no-tui mode. The Bubble
	// Tea TUI mode steers users to --no-tui for those commands until
	// tea.ExecProcess-based handoff lands.
	if noTUI {
		rt.Session().SetTTYRunner(shell.NewPTYManager())
		return runShellREPL(rt)
	}
	return runShellTUI(rt)
}

// runShellTUI launches the Bubble Tea shell model. The session's
// TTYRunner is wired to a tea.ExecProcess-based runner so commands
// like vi / less / top get a real controlling terminal — Bubble Tea
// suspends, the child runs full-screen, then control returns.
func runShellTUI(rt *shell.ShellRuntime) error {
	model := shell.NewShellModel(rt)
	prog := tea.NewProgram(model)
	rt.Session().SetTTYRunner(shell.NewTUITTYRunner(prog))
	_, err := prog.Run()
	return err
}

// buildShellRegistry returns a slash-command registry suitable for shell
// mode. Per plan §13f decision 5, only a curated subset of handlers is
// registered; others would need an *App context they don't have here.
//
// For the skeleton we register a minimal set that doesn't touch the LLM
// (`/help`, `/version`, `/clear`). Wiring `/init`, `/commit`, `/model`
// requires lifting the existing handler registration helper out of
// internal/cli — left as a follow-up.
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

// buildShellProvider returns the LLM provider + model for the `!`/`?`
// sentinels. Detection follows the standard ycode rules: --model flag
// (modelFlag, defined in main.go) or auto-detect from env vars. Returns
// (nil, "") when no credentials are available — the dispatcher prints a
// helpful error in that case.
func buildShellProvider() (api.Provider, string) {
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
		"  !<text>    one-shot agent with shell context (not yet impl.)\n" +
		"  ?<text>    cheap LLM Q&A (not yet impl.)\n\n" +
		"Anything else is bash. Pipelines, heredocs, redirections, env vars,\n" +
		"functions, and `cd` all work and persist across submissions.\n"
}

// runShellREPL is the headless --no-tui debug REPL. Reads lines from
// stdin, classifies each via the sentinel parser, dispatches, prints
// the result. ^C cancels the current dispatch but does not exit the
// shell; ^D (EOF) exits cleanly.
func runShellREPL(rt *shell.ShellRuntime) error {
	d := shell.NewDispatcher(rt)
	sink := shell.WriterSink{StdoutW: os.Stdout, StderrW: os.Stderr}
	scanner := bufio.NewScanner(os.Stdin)
	scanner.Buffer(make([]byte, 0, 64*1024), 1024*1024)

	fmt.Fprintln(os.Stderr, "ycode shell (skeleton — type /help, ^D to exit)")
	for {
		fmt.Fprintf(os.Stdout, "ycode:%s$ ", rt.WorkDir())
		if !scanner.Scan() {
			if err := scanner.Err(); err != nil {
				return err
			}
			fmt.Fprintln(os.Stderr) // newline after ^D
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

		_, derr := d.Dispatch(ctx, intent, sink)
		signal.Stop(sigCh)
		close(sigCh)
		cancel()

		if derr != nil {
			fmt.Fprintf(os.Stderr, "shell: dispatch error: %v\n", derr)
		}
	}
}
