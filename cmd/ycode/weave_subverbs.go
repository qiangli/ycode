package main

import (
	"fmt"

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
	cmd := &cobra.Command{
		Use:   `add "<title>"`,
		Short: "Seed an issue into the loom queue",
		Long: `Files a new issue into the local Gitea, tags it loom:todo, and
applies priority + source labels. The next 'weave start' picks it up
according to the priority sort order.`,
		Args: cobra.MaximumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			if len(args) == 1 {
				title = args[0]
			}
			_ = title
			_ = body
			_ = tool
			_ = priority
			_ = fromFile
			return unimplementedStub("weave add", &flags)(cmd, args)
		},
	}
	flags.attach(cmd)
	cmd.Flags().StringVar(&body, "body", "", "Issue body (optional)")
	cmd.Flags().StringVar(&tool, "tool", "", "Pin a specific agentic tool for this issue (label tool:X)")
	cmd.Flags().StringVar(&priority, "priority", "", "Priority tier: p0|p1|p2|p3 (default p2)")
	cmd.Flags().StringVar(&fromFile, "from-file", "", "Bulk seed: markdown (one per `- [ ]`) or JSON list")
	return cmd
}

func newWeaveStartCmd() *cobra.Command {
	var flags weaveOutputFlags
	var issue int64
	var tool string
	var resume bool
	var noSpawn bool
	cmd := &cobra.Command{
		Use:   "start [-- <tool> [args...]]",
		Short: "Allocate a workspace and launch an agentic tool",
		Long: `start atomically claims the top of the loom:todo queue (or the
issue specified with --issue), allocates a sandbox, and launches the
named tool inside it with YCODE_LOOM_* env vars set.

The trailing '-- <tool>' form is the human-natural shape; --tool is
the programmatic alternative. If neither is given, the project's
default_tool from .ycode/loom.yaml is used.`,
		RunE: func(cmd *cobra.Command, args []string) error {
			_ = issue
			_ = tool
			_ = resume
			_ = noSpawn
			return unimplementedStub("weave start", &flags)(cmd, args)
		},
	}
	flags.attach(cmd)
	cmd.Flags().Int64Var(&issue, "issue", 0, "Claim a specific issue instead of top-of-queue")
	cmd.Flags().StringVar(&tool, "tool", "", "Tool name (alternative to trailing -- <tool>)")
	cmd.Flags().BoolVar(&resume, "resume", false, "Reattach to an existing lease for the given issue")
	cmd.Flags().BoolVar(&noSpawn, "no-spawn", false, "Allocate the workspace but do not exec the tool")
	return cmd
}

func newWeaveNextCmd() *cobra.Command {
	var flags weaveOutputFlags
	cmd := &cobra.Command{
		Use:   "next",
		Short: "Peek at the next issue 'weave start' would claim (non-mutating)",
		RunE: func(cmd *cobra.Command, args []string) error {
			return unimplementedStub("weave next", &flags)(cmd, args)
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
			_ = auto
			return unimplementedStub("weave prio", &flags)(cmd, args)
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
			_ = watch
			_ = history
			return unimplementedStub("weave list", &flags)(cmd, args)
		},
	}
	flags.attach(cmd)
	cmd.Flags().BoolVar(&watch, "watch", false, "Stream state transitions (NDJSON when paired with --json)")
	cmd.Flags().BoolVar(&history, "history", false, "Include reaped/abandoned leases")
	return cmd
}

func newWeavePullCmd() *cobra.Command {
	var flags weaveOutputFlags
	var watch bool
	cmd := &cobra.Command{
		Use:   "pull",
		Short: "Fast-forward your local main from the local Gitea's main",
		RunE: func(cmd *cobra.Command, args []string) error {
			_ = watch
			return unimplementedStub("weave pull", &flags)(cmd, args)
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
		Short: "Tear down a weave (sandbox + branch if no open PR)",
		Args:  cobra.MinimumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			_ = reason
			return unimplementedStub("weave abandon", &flags)(cmd, args)
		},
	}
	flags.attach(cmd)
	cmd.Flags().StringVar(&reason, "reason", "", "Optional human-readable reason for logs")
	return cmd
}

func newWeaveShellCmd() *cobra.Command {
	var flags weaveOutputFlags
	cmd := &cobra.Command{
		Use:   "shell <issue>",
		Short: "Drop into a shell inside the issue's sandbox",
		Args:  cobra.MinimumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			return unimplementedStub("weave shell", &flags)(cmd, args)
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
		Short: "Open the relevant Gitea page in a browser",
		RunE: func(cmd *cobra.Command, args []string) error {
			_ = issues
			_ = board
			_ = issue
			_ = prFlag
			return unimplementedStub("weave open", &flags)(cmd, args)
		},
	}
	flags.attach(cmd)
	cmd.Flags().BoolVar(&issues, "issues", false, "Open the label-filtered issue list (default dashboard)")
	cmd.Flags().Int64Var(&issue, "issue", 0, "Open a specific issue page")
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
			_ = yes
			return unimplementedStub("weave reset", &flags)(cmd, args)
		},
	}
	flags.attach(cmd)
	cmd.Flags().BoolVar(&yes, "yes", false, "Skip the confirm prompt")
	return cmd
}

func newWeaveInitBoardCmd() *cobra.Command {
	var flags weaveOutputFlags
	cmd := &cobra.Command{
		Use:   "init-board",
		Short: "(Optional) Create a Loom kanban project board in Gitea",
		Long: `init-board is an opt-in one-time bootstrap that creates a Gitea
project board with state-mapped columns. The default dashboard is
the label-filtered issue list; the board is decoration, not load-
bearing — loom does not auto-sync card positions.

Implementation note: Gitea 1.26's kanban routes are HTML web-routes
with CSRF + session-cookie auth (not v1 REST). This subverb pulls
those in only when invoked; everything else in 'weave' speaks
stable v1 REST.`,
		RunE: func(cmd *cobra.Command, args []string) error {
			return unimplementedStub("weave init-board", &flags)(cmd, args)
		},
	}
	flags.attach(cmd)
	return cmd
}
