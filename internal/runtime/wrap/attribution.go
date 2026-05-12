package wrap

import (
	"context"
	"os"

	"go.opentelemetry.io/otel/propagation"
)

// resolvedProfileName returns the profile that was matched for opts
// (whether by explicit --profile or by auto-detect from argv[0]).
// Returns empty string when no profile matched. Used for span
// attribution and for the codex-warn gate.
func resolvedProfileName(opts *Options) string {
	if opts == nil {
		return ""
	}
	if opts.Profile != "" {
		return opts.Profile
	}
	if p, ok := ResolveProfile("", opts.AgentArgs); ok {
		return p.Name
	}
	return ""
}

// isResolvedProfile reports whether the active profile resolves to
// the named one. Convenience over resolvedProfileName for the
// codex-warn and similar profile-gated branches.
func isResolvedProfile(opts *Options, name string) bool {
	return resolvedProfileName(opts) == name
}

// exportEnv is the small "log this and return it" helper used when
// composing the setup logger call site so the agent / profile name
// the wrap parent labeled itself with is also exposed via env to
// every shim child and trace subprocess. Setting the env at this
// layer means callers downstream (the Python sitecustomize.py, the
// Node ycode-trace.cjs, the ycode internal-shell-trace subcommand)
// can attach matching attributes to their own spans without each
// re-resolving the profile.
func exportEnv(key, value string) string {
	if value == "" {
		return ""
	}
	// Already-set env wins; the parent's earlier wrap.Run call (rare
	// re-entry) might have populated these.
	if existing := os.Getenv(key); existing != "" {
		return existing
	}
	_ = os.Setenv(key, value)
	return value
}

// injectTraceparent sets TRACEPARENT in env from the current span
// context. Same W3C trace context propagation pattern used in
// cmd/ycode/shell_trace.go. Called by injectShimEnv so the wrapped
// agent process inherits the parent's trace ID — Python and Node
// hooks then propagate it to their `ycode internal-shell-trace`
// subprocess calls, producing a single nested trace per wrap session.
//
// No-op when there's no active span on ctx (the global no-op tracer
// returns a zero span). The propagator's Inject is a write-through:
// it touches the carrier only when the context carries a valid span.
func injectTraceparent(ctx context.Context, env []string) []string {
	carrier := propagation.MapCarrier{}
	propagation.TraceContext{}.Inject(ctx, carrier)
	tp := carrier["traceparent"]
	if tp == "" {
		return env
	}
	return setEnv(env, "TRACEPARENT", tp)
}
