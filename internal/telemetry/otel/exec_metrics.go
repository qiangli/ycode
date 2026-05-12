package otel

import (
	"context"
	"errors"
	"io/fs"
	"log/slog"
	"os"
	"os/exec"
	"path/filepath"
	"syscall"
	"time"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/codes"
	"go.opentelemetry.io/otel/metric"
	"go.opentelemetry.io/otel/trace"
)

// Shared OTEL instrumentation for any external-process spawn (bash
// interpreter dispatch, TTY exec, toolexec host tier, yc sandbox,
// probe Chrome launch, container.Exec). Sits *below* the agent-tool
// metric (ycode.bash.exec.*) so a single bash tool invocation that
// runs `git status && npm test` produces one ycode.bash.exec.* event
// and two ycode.exec.* events with per-binary attribution.
//
// Cardinality discipline:
//
//   - Metric labels: scope (bounded enum), success (bool),
//     exit_class (bounded enum). NEVER raw binary path, args, or
//     exit code.
//   - Span attributes: full binary path, args count, exit code,
//     duration, signal — high-resolution data lives here.

const execTracerName = "ycode.exec"

// Exit classes — small bounded enum suitable as a metric label.
const (
	ExitClassZero     = "zero"      // err==nil, exitCode==0
	ExitClassError    = "error"     // non-zero exit, not signaled
	ExitClassSignaled = "signaled"  // killed by a signal
	ExitClassTimeout  = "timeout"   // ctx canceled before completion
	ExitClassNotFound = "not-found" // exec.Error (binary missing / not executable)
)

// Scopes — bounded enum, one per call-site family.
const (
	ExecScopeBash         = "bash"          // mvdan/sh ExecHandler — agent's bash tool internals
	ExecScopeBashTTY      = "bash-tty"      // ycode shell's TTY exec (ssh, vi, sudo)
	ExecScopeToolexec     = "toolexec"      // 3-tier toolexec host-exec tier
	ExecScopeSandbox      = "sandbox"       // yc sandbox builtin (podman run …)
	ExecScopeProbeLaunch  = "probe-launch"  // ycode browser launch (experimental)
	ExecScopeContainer    = "container"     // Container.Exec REST API
	ExecScopeWrappedAgent = "wrapped-agent" // `ycode wrap` PATH-shim dispatcher
	ExecScopeWASM         = "wasm"          // wazero+WASI sandboxed tool (rg/jq/sed/awk/…)
	ExecScopeMCPGateway   = "mcp-gateway"   // proxy_tool_call federation forward
)

// execMeter looks up the global meter fresh each call. Same pattern
// as browser_metrics.go: some sites (e.g. bash exec inside a shell
// session) fire before setupOTEL runs, so memoizing instruments via
// sync.Once would freeze us on the no-op meter forever. The OTEL SDK
// caches counter creation internally — per-call lookup is cheap.
func execMeter() metric.Meter { return otel.Meter("ycode.exec") }

// ClassifyExit maps a (ctx, exitCode, err) tuple from a finished
// exec.Cmd into a bounded exit_class string, a normalized exit code,
// and an optional signal name. Caller-cancellation takes precedence
// over the child's own exit reason — a child killed by ctx-cancel
// classifies as `timeout` even though it technically signaled.
func ClassifyExit(ctx context.Context, exitCode int, err error) (class string, normalizedCode int, signal string) {
	if ctx != nil && ctx.Err() != nil {
		return ExitClassTimeout, exitCode, ""
	}
	if err == nil {
		return ExitClassZero, 0, ""
	}
	// not-found: covers both *exec.Error (PATH lookup miss) and
	// *fs.PathError ("no such file or directory" for absolute paths).
	var notFound *exec.Error
	if errors.As(err, &notFound) {
		return ExitClassNotFound, 127, ""
	}
	var pathErr *fs.PathError
	if errors.As(err, &pathErr) && errors.Is(err, os.ErrNotExist) {
		return ExitClassNotFound, 127, ""
	}
	var exitErr *exec.ExitError
	if errors.As(err, &exitErr) {
		if status, ok := exitErr.Sys().(syscall.WaitStatus); ok && status.Signaled() {
			sig := status.Signal()
			return ExitClassSignaled, 128 + int(sig), sig.String()
		}
		return ExitClassError, exitErr.ExitCode(), ""
	}
	// Plain error (e.g. ctx already canceled at Wait, network I/O on
	// REST exec). Caller-supplied exitCode is the best we can do.
	return ExitClassError, exitCode, ""
}

// StartExecSpan opens a span around an external process spawn and
// returns a finish closure. The closure ends the span, records the
// duration histogram and the success/exit-class counter, and emits a
// debug (success) or warn (failure) slog line. Use it like:
//
//	ctx, finish := telotel.StartExecSpan(ctx, telotel.ExecScopeBash, binary, args)
//	err = cmd.Run()
//	finish(cmd.ProcessState.ExitCode(), err)
//
// scope MUST be one of the bounded ExecScope* constants. binary may
// be the full path or basename — the metric label uses basename
// only; the span carries the original string verbatim.
func StartExecSpan(ctx context.Context, scope, binary string, args []string) (context.Context, func(exitCode int, err error)) {
	tracer := otel.Tracer(execTracerName)
	spanName := "ycode.exec." + scope
	attrs := []attribute.KeyValue{
		attribute.String("exec.scope", scope),
		attribute.String("exec.binary", binary),
		attribute.Int("exec.args.count", len(args)),
	}
	// Foreign-agent attribution for ycode wrap. The wrap parent and
	// every shim/runtime-hook descendant inherit YCODE_WRAP_AGENT and
	// YCODE_WRAP_PROFILE in env, so any StartExecSpan call inside a
	// wrap session gets these attached without per-call-site plumbing.
	// Bounded cardinality: both are short, fixed-set strings.
	if v := os.Getenv("YCODE_WRAP_AGENT"); v != "" {
		attrs = append(attrs, attribute.String("wrap.agent", v))
	}
	if v := os.Getenv("YCODE_WRAP_PROFILE"); v != "" {
		attrs = append(attrs, attribute.String("wrap.profile", v))
	}
	spanCtx, span := tracer.Start(ctx, spanName, trace.WithAttributes(attrs...))
	start := time.Now()

	return spanCtx, func(exitCode int, err error) {
		dur := time.Since(start)
		class, code, signal := ClassifyExit(spanCtx, exitCode, err)
		success := class == ExitClassZero

		span.SetAttributes(
			attribute.String("exec.exit_class", class),
			attribute.Int("exec.exit_code", code),
			attribute.Int64("exec.duration_ms", dur.Milliseconds()),
		)
		if signal != "" {
			span.SetAttributes(attribute.String("exec.signal", signal))
		}
		switch {
		case err != nil:
			span.RecordError(err)
			span.SetStatus(codes.Error, err.Error())
		case !success:
			span.SetStatus(codes.Error, "exit_class="+class)
		default:
			span.SetStatus(codes.Ok, "")
		}
		span.End()

		recordExecMetric(spanCtx, scope, success, class, dur)
		logExec(scope, binary, class, code, dur, err)
	}
}

// RecordExec is the post-hoc form: emit metrics + log for a process
// that already finished. Use this when the call-site didn't have a
// context at spawn time (or already owns its own span) and just wants
// to publish the result.
func RecordExec(ctx context.Context, scope, binary string, dur time.Duration, exitCode int, err error) {
	class, code, _ := ClassifyExit(ctx, exitCode, err)
	success := class == ExitClassZero
	recordExecMetric(ctx, scope, success, class, dur)
	logExec(scope, binary, class, code, dur, err)
}

func recordExecMetric(ctx context.Context, scope string, success bool, class string, dur time.Duration) {
	m := execMeter()
	counter, err := m.Int64Counter(
		"ycode.exec.total",
		metric.WithDescription("External process invocations, tagged by scope/success/exit_class"),
	)
	if err == nil {
		counter.Add(ctx, 1, metric.WithAttributes(
			attribute.String("scope", scope),
			attribute.Bool("success", success),
			attribute.String("exit_class", class),
		))
	}
	hist, err := m.Float64Histogram(
		"ycode.exec.duration",
		metric.WithUnit("ms"),
		metric.WithDescription("External process duration"),
	)
	if err == nil {
		hist.Record(ctx, float64(dur.Milliseconds()), metric.WithAttributes(
			attribute.String("scope", scope),
			attribute.Bool("success", success),
		))
	}
}

// RecordShellBuiltin emits per-verb attribution for `yc <verb>`
// dispatch. Complements the existing aggregate
// ycode.shell.command.duration (which lumps all dispatch under one
// histogram) with a counter keyed on the actual verb name so
// operators can see which builtins are used and which fail most.
func RecordShellBuiltin(ctx context.Context, verb string, dur time.Duration, exitCode int) {
	success := exitCode == 0
	m := otel.Meter("ycode.shell")
	counter, err := m.Int64Counter(
		"ycode.shell.builtin.total",
		metric.WithDescription("`yc <verb>` dispatches, tagged by verb and success"),
	)
	if err == nil {
		counter.Add(ctx, 1, metric.WithAttributes(
			attribute.String("verb", verb),
			attribute.Bool("success", success),
		))
	}
	hist, err := m.Float64Histogram(
		"ycode.shell.builtin.duration",
		metric.WithUnit("ms"),
		metric.WithDescription("`yc <verb>` dispatch duration"),
	)
	if err == nil {
		hist.Record(ctx, float64(dur.Milliseconds()), metric.WithAttributes(
			attribute.String("verb", verb),
			attribute.Bool("success", success),
		))
	}
}

func logExec(scope, binary, class string, code int, dur time.Duration, err error) {
	base := filepath.Base(binary)
	if class == ExitClassZero {
		slog.Debug("exec",
			"scope", scope,
			"binary", base,
			"exit_code", code,
			"dur_ms", dur.Milliseconds(),
		)
		return
	}
	slog.Warn("exec failed",
		"scope", scope,
		"binary", base,
		"exit_class", class,
		"exit_code", code,
		"dur_ms", dur.Milliseconds(),
		"error", err,
	)
}
