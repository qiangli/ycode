package main

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"strings"

	"github.com/spf13/cobra"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/propagation"
	"go.opentelemetry.io/otel/trace"

	"github.com/qiangli/ycode/internal/runtime/bash"
	"github.com/qiangli/ycode/internal/runtime/bash/shellparse"
	"github.com/qiangli/ycode/internal/runtime/origin"
	"github.com/qiangli/ycode/internal/runtime/wrap"
	telotel "github.com/qiangli/ycode/internal/telemetry/otel"
)

// newShellTraceCmd builds `ycode internal-shell-trace` — a hidden
// subcommand the Python sitecustomize.py and Node ycode-trace.cjs
// hooks call into to:
//
//  1. Parse the intercepted shell-out via shellparse.Parse so the
//     parent agent's shell=True string ("git status && rg foo")
//     becomes a list of structured CommandNodes.
//  2. Run V01-V12 bash validators on the raw string for prompt-
//     injection / dangerous-pattern detection.
//  3. Classify the command intent (ReadOnly / Write / Destructive /
//     Network / ...) via bash.ClassifyCommand.
//  4. Open an ExecScopeWrappedAgent parent OTel span with the full
//     command string as an attribute, plus one child span per parsed
//     CommandNode so the trace shows nested binary/args.
//  5. Honor W3C trace-context propagation: TRACEPARENT env on input
//     nests the spans under the wrapping ycode wrap parent; the
//     traceparent of the emitted parent span is returned in the
//     JSON envelope so the hook can propagate it further if it
//     spawns more processes.
//  6. Return a JSON envelope on stdout the hook treats as the
//     policy decision. Exit 0 on allow; exit non-zero on
//     hard-deny (validator V0X tripped).
//
// The subcommand is hidden so it doesn't clutter `ycode --help` —
// it's an implementation detail of `ycode wrap`'s runtime hooks, not
// a public verb. Callers MUST be the wrap-managed Python/Node hooks.
func newShellTraceCmd() *cobra.Command {
	var (
		argvMode bool
		timeout  string
	)
	cmd := &cobra.Command{
		Use:          "internal-shell-trace [--argv]",
		Short:        "Internal: parse + validate + trace a wrapped agent's shell-out (used by ycode wrap runtime hooks)",
		Hidden:       true,
		SilenceUsage: true,
		RunE: func(cmd *cobra.Command, _ []string) error {
			origin.SetAgentTool(origin.ToolShellTrace)
			_ = timeout
			return runShellTrace(cmd.Context(), cmd.InOrStdin(), cmd.OutOrStdout(), argvMode)
		},
	}
	cmd.Flags().BoolVar(&argvMode, "argv", false,
		"Interpret stdin as a JSON array of argv tokens (exec-form). Default: stdin is the shell string (shell-form).")
	cmd.Flags().StringVar(&timeout, "timeout", "5s",
		"Reserved: per-call upper bound on trace work. Hooks pass --timeout=5s today; enforcement lands when slow validators show up in telemetry.")
	return cmd
}

// shellTraceEnvelope is the JSON the hook reads on stdout.
type shellTraceEnvelope struct {
	Allow       bool                `json:"allow"`
	Reason      string              `json:"reason,omitempty"`
	Parsed      []shellTraceCommand `json:"parsed"`
	Intent      string              `json:"intent"`
	Mode        string              `json:"mode"` // "shell" or "argv"
	Traceparent string              `json:"traceparent,omitempty"`
}

type shellTraceCommand struct {
	Name       string   `json:"name"`
	Args       []string `json:"args,omitempty"`
	InSubshell bool     `json:"in_subshell,omitempty"`
	InPipeline bool     `json:"in_pipeline,omitempty"`
}

func runShellTrace(ctx context.Context, stdin io.Reader, stdout io.Writer, argvMode bool) error {
	raw, err := io.ReadAll(stdin)
	if err != nil {
		return fmt.Errorf("shell-trace: read stdin: %w", err)
	}
	body := strings.TrimRight(string(raw), "\n")

	// When invoked from a `ycode wrap` session (YCODE_WRAP_AGENT in env),
	// install an OTel provider so the parent + child spans emitted below
	// land in the same file (and same collector) as the wrap parent's
	// session span. The TRACEPARENT env nests this subprocess under the
	// wrap session, so spans link across the process boundary.
	if os.Getenv("YCODE_WRAP_AGENT") != "" {
		mode := wrap.ParseExportMode(os.Getenv("YCODE_WRAP_OTEL_EXPORT"))
		shutdown := wrap.SetupOTel(ctx, mode,
			os.Getenv("YCODE_WRAP_AGENT"),
			os.Getenv("YCODE_WRAP_PROFILE"),
		)
		defer shutdown()
	}

	// Honor W3C trace context from env so the parent span nests under
	// the wrapping `ycode wrap` invocation. The hook subprocess inherits
	// TRACEPARENT from the wrapped agent's env, which inherited it
	// from wrap.Run's env injection (when wrap.Run was itself wrapped
	// by an outer trace; otherwise this is a no-op).
	propagator := propagation.TraceContext{}
	ctx = propagator.Extract(ctx, propagation.MapCarrier{
		"traceparent": os.Getenv("TRACEPARENT"),
	})

	var (
		parsed      []shellTraceCommand
		intent      string
		shellString string
	)
	if argvMode {
		// exec-form: stdin is a JSON array of argv tokens. No shell
		// parser involved; just emit a single CommandNode reflecting
		// the literal exec.
		var argv []string
		if err := json.Unmarshal([]byte(body), &argv); err != nil {
			return writeEnvelope(stdout, shellTraceEnvelope{
				Allow:  false,
				Reason: fmt.Sprintf("shell-trace --argv: parse argv JSON: %v", err),
				Mode:   "argv",
			})
		}
		if len(argv) == 0 {
			return writeEnvelope(stdout, shellTraceEnvelope{
				Allow:  false,
				Reason: "shell-trace --argv: empty argv",
				Mode:   "argv",
			})
		}
		parsed = []shellTraceCommand{{Name: argv[0], Args: argv[1:]}}
		// For argv-form we synthesize a shell string for the
		// classifier — best-effort, only used for intent display.
		shellString = strings.Join(argv, " ")
	} else {
		shellString = body
		nodes, perr := shellparse.Parse(shellString)
		if perr != nil {
			// Fall back to "treat the whole string as one opaque
			// command" so the hook still emits a trace.
			parsed = []shellTraceCommand{{Name: firstWord(shellString)}}
		} else {
			parsed = make([]shellTraceCommand, 0, len(nodes))
			for _, n := range nodes {
				parsed = append(parsed, shellTraceCommand{
					Name:       n.Name,
					Args:       n.Args,
					InSubshell: n.InSubshell,
					InPipeline: n.InPipeline,
				})
			}
		}
	}

	// Validators run against the raw shell string. For argv-form they
	// rarely flag anything; we still run them so the call site has a
	// uniform return shape.
	validation := bash.RunAllValidators(shellString)
	intentVal, _ := bash.ClassifyCommand(shellString)
	intent = intentVal.String()

	envelope := shellTraceEnvelope{
		Allow:  validation.OK,
		Parsed: parsed,
		Intent: intent,
		Mode:   modeLabel(argvMode),
	}
	if !validation.OK {
		envelope.Reason = fmt.Sprintf("validator %s: %s", validation.ID, validation.Reason)
	}

	// Emit OTel spans: parent for the whole shell-out, one child per
	// parsed CommandNode. Each child carries binary + args.count so
	// cardinality stays bounded (same discipline as
	// telotel.StartExecSpan). Pick up wrap.agent / wrap.profile from
	// the inherited env so dashboards can slice by foreign agent.
	tracer := otel.Tracer("ycode.wrap.shell-trace")
	parentAttrs := []attribute.KeyValue{
		attribute.String("exec.scope", telotel.ExecScopeWrappedAgent),
		attribute.String("exec.mode", envelope.Mode),
		attribute.String("exec.intent", envelope.Intent),
		attribute.String("exec.cmdline", truncate(shellString, 1024)),
		attribute.Int("exec.parsed.count", len(parsed)),
	}
	if v := os.Getenv("YCODE_WRAP_AGENT"); v != "" {
		parentAttrs = append(parentAttrs, attribute.String("wrap.agent", v))
	}
	if v := os.Getenv("YCODE_WRAP_PROFILE"); v != "" {
		parentAttrs = append(parentAttrs, attribute.String("wrap.profile", v))
	}
	parentCtx, parentSpan := tracer.Start(ctx, "ycode.exec.wrapped-agent",
		trace.WithAttributes(parentAttrs...),
	)
	for _, c := range parsed {
		_, child := tracer.Start(parentCtx, "ycode.exec.wrapped-agent.cmd",
			trace.WithAttributes(
				attribute.String("exec.binary", c.Name),
				attribute.Int("exec.args.count", len(c.Args)),
				attribute.Bool("exec.in_subshell", c.InSubshell),
				attribute.Bool("exec.in_pipeline", c.InPipeline),
			),
		)
		child.End()
	}
	// Capture the parent span's traceparent before End so the hook can
	// pass it down to its own subprocess for nesting.
	carrier := propagation.MapCarrier{}
	propagator.Inject(parentCtx, carrier)
	envelope.Traceparent = carrier["traceparent"]
	parentSpan.End()

	if err := writeEnvelope(stdout, envelope); err != nil {
		return err
	}
	if !envelope.Allow {
		// Non-zero exit signals "deny" to the calling hook. The hook
		// will surface the validation reason to the wrapped agent.
		// Use silent-usage so cobra doesn't print its own error trail
		// — the JSON envelope on stdout is the authoritative result.
		return errShellTraceDeny
	}
	return nil
}

// errShellTraceDeny is the sentinel returned when a validator
// rejected the trace input. Caller (newShellTraceCmd) sets
// SilenceUsage so cobra doesn't print its own banner; the JSON
// envelope on stdout already explains the deny. main.go's
// realMain() turns RunE errors into os.Exit(1).
var errShellTraceDeny = fmt.Errorf("shell-trace: validator denied")

func writeEnvelope(out io.Writer, env shellTraceEnvelope) error {
	if env.Parsed == nil {
		env.Parsed = []shellTraceCommand{}
	}
	enc := json.NewEncoder(out)
	enc.SetEscapeHTML(false)
	return enc.Encode(env)
}

func firstWord(s string) string {
	s = strings.TrimSpace(s)
	if idx := strings.IndexAny(s, " \t\n"); idx > 0 {
		return s[:idx]
	}
	return s
}

func modeLabel(argv bool) string {
	if argv {
		return "argv"
	}
	return "shell"
}

func truncate(s string, max int) string {
	if len(s) <= max {
		return s
	}
	return s[:max] + "…"
}
