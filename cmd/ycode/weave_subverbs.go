package main

import (
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/spf13/cobra"
)

// Per-subverb constructors. Each registers its flags + RunE; the
// orchestration bodies arrive in subsequent N+1 PRs as the related
// substrate operations land (Service.Claim, weavesetup.Run,
// weaveapi.* helpers, etc.).
//
// Today every subverb returns ExitPrecondFail with a clear
// "not yet wired" envelope. The skeleton confirms the surface shape
// — `ycode weave --help` lists every verb, `ycode weave start --json`
// produces a parseable envelope on stderr, etc. — so downstream
// tooling (foreign agents, scripts) can be written and tested
// against the contract before the inner machinery exists.

func newWeaveAddCmd() *cobra.Command {
	var flags weaveOutputFlags
	var title, body, tool, priority string
	var fromFile string
	var verify string
	cmd := &cobra.Command{
		Use:   `add "<title>"`,
		Short: "Seed an issue into the loom queue",
		Long: `Files a new issue into the local queue, tags it loom:todo, and
applies priority + source labels. The next 'weave start' picks it up
according to the priority sort order.`,
		Args: cobra.MaximumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			if len(args) == 1 {
				title = args[0]
			}
			_ = tool
			if fromFile != "" {
				return runWeaveAddFromFile(cmd, fromFile, priority, &flags)
			}
			return runWeaveAdd(cmd, title, body, priority, verify, &flags)
		},
	}
	flags.attach(cmd)
	cmd.Flags().StringVar(&body, "body", "", "Issue body (optional)")
	cmd.Flags().StringVar(&tool, "tool", "", "Pin a specific agentic tool for this issue (label tool:X)")
	cmd.Flags().StringVar(&priority, "priority", "", "Priority tier: p0|p1|p2|p3 (default p2)")
	cmd.Flags().StringVar(&fromFile, "from-file", "", "Bulk seed: markdown (`- [ ] title`) or JSON list of {title,body,priority}")
	cmd.Flags().StringVar(&verify, "verify", "", "Verify command the wrapper runs (`bash -c`) in the sandbox at terminal time; verify_exit/verify_output recorded on the item, non-zero blocks `weave pull`")
	return cmd
}

func newWeaveStartCmd() *cobra.Command {
	var flags weaveOutputFlags
	var issue int64
	var tool string
	var resume bool
	var noSpawn bool
	var ptyMode string
	var idleTimeout time.Duration
	var maxRuntime time.Duration
	var memLimit string
	cmd := &cobra.Command{
		Use:   "start [-- <tool> [args...]]",
		Short: "Allocate a workspace and launch an agentic tool",
		Long: `start atomically claims the top of the loom:todo queue (or the
issue specified with --issue), allocates a sandbox, and launches the
named tool inside it with YCODE_LOOM_* env vars set.

The trailing '-- <tool>' form is the human-natural shape; --tool is
the programmatic alternative.

PTY: by default the subagent runs inside a freshly-allocated PTY
(claude-code, codex, opencode and similar TUIs need one to render).
When stdout is a terminal the PTY passes through interactively;
when it isn't (orchestrator pipe / backgrounded by shell &) the
PTY output goes to a per-issue log file under the queue dir and
the file path appears in the result envelope.

On exit, the queue item's state becomes "submitted" (exit 0) or
"failed" (non-zero), with exit_code and finished_at persisted.
"weave pull" picks up submitted branches; "weave wait --issue N"
blocks until N reaches a terminal state.`,
		RunE: func(cmd *cobra.Command, args []string) error {
			return runWeaveStart(cmd, issue, tool, args, weaveStartOptions{
				noSpawn:     noSpawn,
				resume:      resume,
				pty:         ptyMode,
				idleTimeout: idleTimeout,
				maxRuntime:  maxRuntime,
				memLimit:    memLimit,
			}, &flags)
		},
	}
	flags.attach(cmd)
	cmd.Flags().Int64Var(&issue, "issue", 0, "Claim a specific issue instead of top-of-queue")
	cmd.Flags().StringVar(&tool, "tool", "", "Tool name (alternative to trailing -- <tool>)")
	cmd.Flags().BoolVar(&resume, "resume", false, "Reattach to an existing lease for the given issue")
	cmd.Flags().BoolVar(&noSpawn, "no-spawn", false, "Allocate the workspace but do not exec the tool")
	cmd.Flags().StringVar(&ptyMode, "pty", "auto", "PTY allocation: auto (default) | always | never")
	cmd.Flags().DurationVar(&idleTimeout, "idle-timeout", 0, "Kill the subagent tree if no PTY output for this long (e.g. 5m); default off — caught the claude-TUI stuck case in the dogfood")
	cmd.Flags().DurationVar(&maxRuntime, "max-runtime", 0, "Hard wall-clock ceiling for the subagent (e.g. 30m); unlike --idle-timeout it cannot be reset by spinner output; default off")
	cmd.Flags().StringVar(&memLimit, "mem-limit", "16g", "Kill the subagent tree when its total RSS exceeds this (e.g. 16g, 512m); 0 disables — the OOM backstop")
	return cmd
}

func newWeaveNextCmd() *cobra.Command {
	var flags weaveOutputFlags
	cmd := &cobra.Command{
		Use:   "next",
		Short: "Peek at the next issue 'weave start' would claim (non-mutating)",
		RunE: func(cmd *cobra.Command, args []string) error {
			return runWeaveNext(cmd, &flags)
		},
	}
	flags.attach(cmd)
	return cmd
}

func newWeavePrioCmd() *cobra.Command {
	var flags weaveOutputFlags
	var auto bool
	cmd := &cobra.Command{
		Use:   "prio <issue> p0|p1|p2|p3",
		Short: "Set an issue's priority tier (or --auto to LLM-rank the queue)",
		Args: func(cmd *cobra.Command, args []string) error {
			if auto && len(args) == 0 {
				return nil
			}
			if len(args) != 2 {
				return fmt.Errorf("expected: weave prio <issue> p0|p1|p2|p3")
			}
			return nil
		},
		RunE: func(cmd *cobra.Command, args []string) error {
			if auto {
				return runWeavePrio(cmd, 0, "", true, &flags)
			}
			id, err := strconv.ParseInt(args[0], 10, 64)
			if err != nil {
				return fmt.Errorf("issue must be an integer: %q", args[0])
			}
			return runWeavePrio(cmd, id, args[1], false, &flags)
		},
	}
	flags.attach(cmd)
	cmd.Flags().BoolVar(&auto, "auto", false, "Delegate ranking to an LLM (re-ranks the whole queue)")
	return cmd
}

func newWeaveListCmd() *cobra.Command {
	var flags weaveOutputFlags
	var watch bool
	var history bool
	cmd := &cobra.Command{
		Use:   "list",
		Short: "Show active weaves (or --watch for live state-transition stream)",
		RunE: func(cmd *cobra.Command, args []string) error {
			if watch {
				return runWeaveListWatch(cmd, history, &flags)
			}
			return runWeaveList(cmd, history, &flags)
		},
	}
	flags.attach(cmd)
	cmd.Flags().BoolVar(&watch, "watch", false, "Stream state transitions (NDJSON when paired with --json)")
	cmd.Flags().BoolVar(&history, "history", false, "Include reaped/abandoned leases")
	return cmd
}

func newWeaveLogCmd() *cobra.Command {
	var flags weaveOutputFlags
	var follow bool
	var tailN int
	cmd := &cobra.Command{
		Use:   "log <issue>",
		Short: "Print (or --follow) the captured PTY log of an issue's subagent",
		Long: `log prints the per-issue PTY capture file — everything the subagent
wrote to its terminal. The capture exists whenever 'weave start' ran
non-interactively (orchestrator pipe, backgrounded with &); a start
from a real terminal passes the PTY through instead, so there is
nothing to print.

  ycode weave log 4              # whole log so far
  ycode weave log 4 -n 100       # last 100 lines
  ycode weave log 4 -f           # stream live; exits when the issue
                                 # reaches a terminal state
  ycode weave log 4 -f -n 0      # follow, new output only

Output is the raw PTY byte stream (ANSI escapes included) — pipe
through 'less -R' for paging. Anyone on the host can watch a running
subagent this way; the file persists after the run as the post-
mortem artifact.

NOTE: some tools buffer in non-interactive modes (e.g. 'claude -p'
holds all output until exit) — an empty log under -f means "nothing
emitted yet", not "nothing happening". With --json, emits the log
metadata (path, size, state) instead of the raw stream — agent
callers read the file themselves.`,
		Args: cobra.MinimumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			id, err := strconv.ParseInt(args[0], 10, 64)
			if err != nil {
				return fmt.Errorf("issue must be an integer: %q", args[0])
			}
			return runWeaveLog(cmd, id, follow, tailN, &flags)
		},
	}
	flags.attach(cmd)
	cmd.Flags().BoolVarP(&follow, "follow", "f", false, "Stream appended output until the issue reaches a terminal state")
	cmd.Flags().IntVarP(&tailN, "tail", "n", -1, "Print only the last N lines (0 = none, useful with -f; -1 = whole file)")
	return cmd
}

func newWeaveSayCmd() *cobra.Command {
	var flags weaveOutputFlags
	cmd := &cobra.Command{
		Use:   `say <issue> "<text>"`,
		Short: "Inject a line into a running subagent's terminal",
		Long: `say connects to the running wrapper's per-issue control socket and
types the text into the subagent's PTY, followed by Enter. To the
subagent it is indistinguishable from a human typing into its TUI —
so mid-run steering works the way you'd expect:

  ycode weave say 4 "/btw what is the status? reply in the log"
  ycode weave say 4 "stop exploring; commit what passes and exit"

Anyone on the host (a peer agent, a human in another terminal) can
inject; watch the reaction with 'weave log <issue> -f'.

Caveats: the issue must be state=working with a live wrapper that
allocated a PTY. Tools that don't read terminal input in their
non-interactive modes (e.g. 'claude -p') receive the keystrokes but
ignore them — use a TUI/streaming mode when you plan to steer.
Wrappers started by an older ycode have no control socket.`,
		Args: cobra.MinimumNArgs(2),
		RunE: func(cmd *cobra.Command, args []string) error {
			id, err := strconv.ParseInt(args[0], 10, 64)
			if err != nil {
				return fmt.Errorf("issue must be an integer: %q", args[0])
			}
			return runWeaveSay(cmd, id, strings.Join(args[1:], " "), &flags)
		},
	}
	flags.attach(cmd)
	return cmd
}

func newWeavePullCmd() *cobra.Command {
	var flags weaveOutputFlags
	var watch bool
	cmd := &cobra.Command{
		Use:   "pull",
		Short: "Fast-forward your local main with the merged agent branches",
		RunE: func(cmd *cobra.Command, args []string) error {
			_ = watch
			return runWeavePull(cmd, &flags)
		},
	}
	flags.attach(cmd)
	cmd.Flags().BoolVar(&watch, "watch", false, "Daemonize: fast-forward whenever a PR merges")
	return cmd
}

func newWeaveAbandonCmd() *cobra.Command {
	var flags weaveOutputFlags
	var reason string
	cmd := &cobra.Command{
		Use:   "abandon <issue>",
		Short: "Tear down a weave (sandbox + branch + any running wrapper)",
		Long: `abandon stops the running wrapper (if any) AND removes the sandbox
+ branch. Use this when giving up on an issue entirely.

For "stop the runaway but keep the partial work for inspection",
use ` + "`weave kill`" + ` instead.`,
		Args: cobra.MinimumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			id, err := strconv.ParseInt(args[0], 10, 64)
			if err != nil {
				return fmt.Errorf("issue must be an integer: %q", args[0])
			}
			return runWeaveAbandon(cmd, id, reason, &flags)
		},
	}
	flags.attach(cmd)
	cmd.Flags().StringVar(&reason, "reason", "", "Optional human-readable reason for logs")
	return cmd
}

func newWeaveKillCmd() *cobra.Command {
	var flags weaveOutputFlags
	var reason string
	cmd := &cobra.Command{
		Use:   "kill <issue>",
		Short: "Stop the running wrapper precisely, preserve sandbox + branch",
		Long: `kill SIGTERMs the recorded wrapper PID for the issue and flips the
queue item to state=failed. The sandbox + branch + any commits the
subagent already made are preserved — the orchestrator can:

  - ` + "`weave shell <issue>`" + ` to inspect the partial work
  - ` + "`weave start --resume --issue N -- <tool>`" + ` to retry inside the same sandbox
  - ` + "`weave abandon <issue>`" + ` to throw it all away

IMPORTANT for orchestrator agents: never shell out to ` + "`pkill`" + ` /
` + "`killall`" + ` / ` + "`kill -9`" + ` to stop a stuck subagent. Pattern matchers
also catch peer ycode / claude / codex sessions belonging to OTHER
agents on the same machine. ` + "`weave kill`" + ` reads the recorded
wrapper PID and signals only that process group — safe in shared
agentic environments.`,
		Args: cobra.MinimumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			id, err := strconv.ParseInt(args[0], 10, 64)
			if err != nil {
				return fmt.Errorf("issue must be an integer: %q", args[0])
			}
			return runWeaveKill(cmd, id, reason, &flags)
		},
	}
	flags.attach(cmd)
	cmd.Flags().StringVar(&reason, "reason", "", "Optional human-readable reason for the failure record")
	return cmd
}

func newWeaveShellCmd() *cobra.Command {
	var flags weaveOutputFlags
	cmd := &cobra.Command{
		Use:   "shell <issue>",
		Short: "Drop into a shell inside the issue's sandbox",
		Args:  cobra.MinimumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			id, err := strconv.ParseInt(args[0], 10, 64)
			if err != nil {
				return fmt.Errorf("issue must be an integer: %q", args[0])
			}
			return runWeaveShell(cmd, id, &flags)
		},
	}
	flags.attach(cmd)
	return cmd
}

func newWeaveOpenCmd() *cobra.Command {
	var flags weaveOutputFlags
	var issues, board bool
	var issue int64
	var prFlag bool
	cmd := &cobra.Command{
		Use:   "open [--issues | --issue N | --pr | --board]",
		Short: "Open the relevant forge page in a browser",
		RunE: func(cmd *cobra.Command, args []string) error {
			return runWeaveOpen(cmd, issues, board, prFlag, issue, &flags)
		},
	}
	flags.attach(cmd)
	cmd.Flags().BoolVar(&issues, "issues", false, "Open the label-filtered issue list (default dashboard)")
	cmd.Flags().Int64Var(&issue, "issue", 0, "Open a specific issue page (or, in the local backend, surface the sandbox file:// URL)")
	cmd.Flags().BoolVar(&prFlag, "pr", false, "Open the PR for the issue named in --issue")
	cmd.Flags().BoolVar(&board, "board", false, "Open the kanban (requires 'weave init-board' first)")
	return cmd
}

func newWeaveResetCmd() *cobra.Command {
	var flags weaveOutputFlags
	var yes bool
	cmd := &cobra.Command{
		Use:   "reset",
		Short: "Tear down every weave for this project (preserves labels + issues)",
		RunE: func(cmd *cobra.Command, args []string) error {
			return runWeaveReset(cmd, yes, &flags)
		},
	}
	flags.attach(cmd)
	cmd.Flags().BoolVar(&yes, "yes", false, "Skip the confirm prompt")
	return cmd
}

func newWeaveWaitCmd() *cobra.Command {
	var flags weaveOutputFlags
	var issue int64
	var all bool
	var timeout time.Duration
	cmd := &cobra.Command{
		Use:   "wait [--issue N | --all]",
		Short: "Block until issue(s) reach a terminal state",
		Long: `wait polls the queue every 1s until the target reaches a terminal
state (submitted, failed, done, or abandoned). Use --issue N to wait
on one issue or --all to wait until no working items remain.

Pairs with --detach-style backgrounding (` + "`ycode weave start ... &`" + `).
A typical orchestrator flow:

  ycode weave start --issue 1 -- codex 'fix #1' &
  ycode weave start --issue 2 -- claude-code 'fix #2' &
  ycode weave wait --all --timeout 30m
  ycode weave pull

Default timeout is 1h. On timeout, exits with precondition_failed
(exit code 3) so the caller can react.`,
		RunE: func(cmd *cobra.Command, args []string) error {
			return runWeaveWait(cmd, issue, all, timeout, &flags)
		},
	}
	flags.attach(cmd)
	cmd.Flags().Int64Var(&issue, "issue", 0, "Wait on a specific issue ID")
	cmd.Flags().BoolVar(&all, "all", false, "Wait until no `working` items remain")
	cmd.Flags().DurationVar(&timeout, "timeout", time.Hour, "Maximum wait duration (e.g. 30m, 1h)")
	return cmd
}

func newWeaveInitBoardCmd() *cobra.Command {
	var flags weaveOutputFlags
	cmd := &cobra.Command{
		Use:   "init-board",
		Short: "(Optional) Create a Loom kanban project board on the forge",
		Long: `init-board is an opt-in one-time bootstrap that creates a forge
project board with state-mapped columns. The default dashboard is
the label-filtered issue list; the board is decoration, not load-
bearing — loom does not auto-sync card positions.

Implementation note: the embedded forge's kanban routes are HTML
web-routes with CSRF + session-cookie auth (not v1 REST). This
subverb pulls those in only when invoked; everything else in
'weave' speaks stable v1 REST.

In the local-only backend (no ` + "`" + `ycode serve` + "`" + ` running), this command
emits a precondition_failed envelope explaining the dependency.`,
		RunE: func(cmd *cobra.Command, args []string) error {
			return runWeaveInitBoard(cmd, &flags)
		},
	}
	flags.attach(cmd)
	return cmd
}
