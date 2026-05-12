package main

import (
	"fmt"
	"os"

	"github.com/spf13/cobra"

	"github.com/qiangli/ycode/internal/runtime/origin"
	"github.com/qiangli/ycode/internal/runtime/wrap"
)

// newWrapCmd builds `ycode wrap <agent-cmd> [args...]` — the involuntary
// interception axis complementing ycode's voluntary lighthouse beam
// (ycode mcp serve). Foreign agents launched this way run with a
// shim PATH so their bash, rg, git, jq, sed, awk, ... invocations flow
// through ycode for OTel observability and best-effort policy.
//
// See docs/agent-os.md for the broader Agent OS framing (Ring 1
// telemetry, Ring 1.5 WASM sandbox, Ring 2 Linux Landlock+seccomp).
// Today this command implements Ring 1 only — Ring 1.5 and Ring 2
// land in follow-up phases.
func newWrapCmd() *cobra.Command {
	var (
		permission  string
		loom        string
		allowedDirs []string
		extraShims  []string
	)
	cmd := &cobra.Command{
		Use:   "wrap [flags] -- <agent-cmd> [args...]",
		Short: "Launch a third-party agentic tool under ycode's shim PATH",
		Long: "Materialize a temporary shim PATH that routes the wrapped tool's " +
			"shell-outs (bash, rg, git, jq, sed, awk, npm, pip, python, node, ...) " +
			"through ycode for OTel observability and best-effort policy. The " +
			"shim is torn down when the foreign agent exits.\n\n" +
			"Examples:\n" +
			"  ycode wrap -- claude-code\n" +
			"  ycode wrap -- codex --provider openai\n" +
			"  ycode wrap --permission=workspace-write -- aider --no-auto-commits\n\n" +
			"Documented limit: foreign agents that build exec.Command with an " +
			"absolute path (e.g. /bin/bash) bypass the shim. ycode wrap is a " +
			"telemetry + best-effort policy layer, not a security boundary. " +
			"Hard isolation arrives in Phase 2 via Landlock + seccomp_unotify " +
			"on Linux 5.10+.",
		Args:               cobra.MinimumNArgs(1),
		DisableFlagParsing: false,
		RunE: func(cmd *cobra.Command, args []string) error {
			origin.SetAgentTool(origin.ToolWrap)
			ctx := cmd.Context()

			exitCode, err := wrap.Run(ctx, wrap.Options{
				AgentArgs:   args,
				Permission:  permission,
				Loom:        loom,
				AllowedDirs: allowedDirs,
				ExtraShims:  extraShims,
			})
			if err != nil {
				return fmt.Errorf("wrap: %w", err)
			}
			if exitCode != 0 {
				os.Exit(exitCode)
			}
			return nil
		},
	}
	cmd.Flags().StringVar(&permission, "permission", "danger-full-access",
		"Permission ceiling for the wrapped agent: read-only | workspace-write | danger-full-access")
	cmd.Flags().StringVar(&loom, "loom", "off",
		"Loom workspace mode: off (default) | auto | on (Phase 2 — currently warns and runs in cwd)")
	cmd.Flags().StringSliceVar(&allowedDirs, "allow-dir", nil,
		"Additional directories the wrapped agent may write to (reserved for Phase 2 VFS boundary)")
	cmd.Flags().StringSliceVar(&extraShims, "shim", nil,
		"Extra command names to add to the shim catalog (basename only, repeatable)")
	return cmd
}
