package main

import (
	"fmt"
	"os"
	"strings"

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
		permission   string
		loom         string
		allowedDirs  []string
		extraShims   []string
		profile      string
		runtimeHooks string
	)
	cmd := &cobra.Command{
		Use:   "wrap [flags] -- <agent-cmd> [args...]",
		Short: "Launch a third-party agentic tool under ycode's shim PATH",
		Long: "Materialize a temporary shim PATH that routes the wrapped tool's " +
			"shell-outs (bash, rg, git, jq, sed, awk, npm, pip, python, node, ...) " +
			"through ycode for OTel observability and best-effort policy. The " +
			"shim is torn down when the foreign agent exits.\n\n" +
			"Known profiles (auto-detected from the agent command basename): " +
			strings.Join(wrap.ProfileNames(), ", ") + ".\n\n" +
			"Examples:\n" +
			"  ycode wrap -- claude-code\n" +
			"  ycode wrap -- codex --provider openai\n" +
			"  ycode wrap --permission=workspace-write -- aider --no-auto-commits\n" +
			"  ycode wrap --profile=aider --runtime-hooks=off -- aider\n\n" +
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

			hooks, err := parseRuntimeHooks(runtimeHooks)
			if err != nil {
				return fmt.Errorf("wrap: %w", err)
			}

			exitCode, err := wrap.Run(ctx, wrap.Options{
				AgentArgs:    args,
				Permission:   permission,
				Loom:         loom,
				AllowedDirs:  allowedDirs,
				ExtraShims:   extraShims,
				Profile:      profile,
				RuntimeHooks: hooks,
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
	cmd.Flags().StringVar(&permission, "permission", "",
		"Permission ceiling for the wrapped agent: read-only | workspace-write | danger-full-access (default: profile or danger-full-access)")
	cmd.Flags().StringVar(&loom, "loom", "off",
		"Loom workspace mode: off (default) | auto | on (Phase 2 — currently warns and runs in cwd)")
	cmd.Flags().StringSliceVar(&allowedDirs, "allow-dir", nil,
		"Additional directories the wrapped agent may write to (reserved for Phase 2 VFS boundary)")
	cmd.Flags().StringSliceVar(&extraShims, "shim", nil,
		"Extra command names to add to the shim catalog (basename only, repeatable)")
	cmd.Flags().StringVar(&profile, "profile", "",
		"Per-agent profile to apply (claude | opencode | codex | aider | gemini). Auto-detected from the agent's basename when omitted.")
	cmd.Flags().StringVar(&runtimeHooks, "runtime-hooks", "auto",
		"Language runtime hooks to install in the wrapped process: auto (default, follows profile) | off | comma-separated list (python,node)")
	return cmd
}

// parseRuntimeHooks turns the --runtime-hooks flag value into the
// Options.RuntimeHooks slice. "auto" returns nil so wrap.Run lets the
// matched profile populate the list. "off" returns an empty (non-nil)
// slice that wrap.Run treats as an explicit "no hooks" override.
//
// The env var YCODE_WRAP_RUNTIME_HOOKS=off is the escape hatch the
// plan calls out — it converts the flag to "off" before parsing so a
// one-off `YCODE_WRAP_RUNTIME_HOOKS=off ycode wrap aider` disables
// hooks without touching settings files.
func parseRuntimeHooks(flag string) ([]string, error) {
	if env := os.Getenv("YCODE_WRAP_RUNTIME_HOOKS"); env != "" {
		flag = env
	}
	switch strings.ToLower(strings.TrimSpace(flag)) {
	case "", "auto":
		return nil, nil
	case "off", "none", "disabled":
		return []string{}, nil
	}
	var out []string
	for _, part := range strings.Split(flag, ",") {
		v := strings.ToLower(strings.TrimSpace(part))
		if v == "" {
			continue
		}
		if v != "python" && v != "node" {
			return nil, fmt.Errorf("unknown runtime hook %q (known: python, node, off, auto)", v)
		}
		out = append(out, v)
	}
	if len(out) == 0 {
		return []string{}, nil
	}
	return out, nil
}
