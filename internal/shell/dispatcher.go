package shell

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"strings"

	"github.com/qiangli/ycode/internal/runtime/bash"
)

// Sink receives output chunks during dispatch. The skeleton has only one
// implementation (writes to stdout/stderr), but defining an interface lets
// the Bubble Tea TUI route chunks into the viewport in step 8 without
// changing the dispatcher.
type Sink interface {
	Stdout() io.Writer
	Stderr() io.Writer
}

// Result describes the outcome of dispatching an Intent.
type Result struct {
	ExitCode int
	// Body is non-streaming output (slash commands return their full
	// response as a string, not chunked). Empty for IntentBash and the
	// `!`/`?` stubs.
	Body string
}

// IsShellSafeSlash reports whether a slash command name is registered in
// `rt`'s registry with Spec.ShellSafe == true. Used by the dispatcher
// gate and by Tab completion. When `rt` or its registry is nil, all
// names are considered unsafe.
func IsShellSafeSlash(rt *ShellRuntime, name string) bool {
	if rt == nil {
		return false
	}
	r := rt.Registry()
	if r == nil {
		return false
	}
	spec, ok := r.Get(name)
	if !ok {
		return false
	}
	return spec.ShellSafe
}

// Dispatcher routes a parsed Intent to the right handler.
type Dispatcher struct {
	rt *ShellRuntime
}

// NewDispatcher builds a Dispatcher bound to the given runtime.
func NewDispatcher(rt *ShellRuntime) *Dispatcher {
	return &Dispatcher{rt: rt}
}

// Dispatch runs the Intent and returns the Result. Errors are only
// returned for genuine failures (handler not configured, panic-level
// problems). Per-command non-zero exit codes go in Result.ExitCode.
func (d *Dispatcher) Dispatch(ctx context.Context, in Intent, sink Sink) (Result, error) {
	if d == nil || d.rt == nil {
		return Result{}, fmt.Errorf("dispatcher: runtime not configured")
	}

	switch in.Kind {
	case IntentEmpty:
		return Result{ExitCode: 0}, nil

	case IntentBash:
		return d.dispatchBash(ctx, in, sink)

	case IntentSlash:
		return d.dispatchSlash(ctx, in, sink)

	case IntentSkill, IntentSkillPath:
		return d.dispatchSkill(ctx, in, sink)

	case IntentAgentShot:
		return d.dispatchAgentShot(ctx, in, sink)

	case IntentAgentQA:
		return d.dispatchAgentQA(ctx, in, sink)

	default:
		return Result{ExitCode: 2}, fmt.Errorf("dispatcher: unknown intent kind %v", in.Kind)
	}
}

func (d *Dispatcher) dispatchBash(ctx context.Context, in Intent, sink Sink) (Result, error) {
	// Tee stdout into a small buffer so the next `!` invocation can
	// attach last-output as shell context. Capped at agentHistoryCap to
	// keep memory bounded.
	var hist bytes.Buffer
	teedStdout := &cappedTee{w: sink.Stdout(), buf: &hist, cap: agentHistoryCap}

	exit, err := d.rt.session.RunString(ctx, in.Raw, bash.Stdio{
		Stdout: teedStdout,
		Stderr: sink.Stderr(),
	})
	d.rt.RecordHistory(strings.TrimSpace(in.Raw), hist.String())
	return Result{ExitCode: exit}, err
}

// agentHistoryCap is how many bytes of stdout are remembered for the
// `!` sentinel's shell context.
const agentHistoryCap = 4 * 1024

// cappedTee mirrors writes to both the live sink (so the user sees
// output in real time) and a bounded buffer used as agent context.
type cappedTee struct {
	w   io.Writer
	buf *bytes.Buffer
	cap int
}

func (t *cappedTee) Write(p []byte) (int, error) {
	if t.buf.Len() < t.cap {
		take := t.cap - t.buf.Len()
		if take > len(p) {
			take = len(p)
		}
		t.buf.Write(p[:take])
	}
	if t.w == nil {
		return len(p), nil
	}
	return t.w.Write(p)
}

func (d *Dispatcher) dispatchSlash(ctx context.Context, in Intent, sink Sink) (Result, error) {
	registry := d.rt.Registry()
	if registry == nil {
		fmt.Fprintln(sink.Stderr(), "no slash registry configured for this shell")
		return Result{ExitCode: 1}, nil
	}
	if !IsShellSafeSlash(d.rt, in.Name) {
		fmt.Fprintf(sink.Stderr(),
			"/%s is not available in shell mode (use the ycode REPL). Shell-safe commands: %s\n",
			in.Name, listShellSafeSlash(d.rt))
		return Result{ExitCode: 1}, nil
	}

	body, err := registry.Execute(ctx, in.Name, in.Args)
	if err != nil {
		fmt.Fprintf(sink.Stderr(), "/%s: %v\n", in.Name, err)
		return Result{ExitCode: 1, Body: body}, nil
	}
	if body != "" {
		fmt.Fprintln(sink.Stdout(), body)
	}
	return Result{ExitCode: 0, Body: body}, nil
}

func (d *Dispatcher) dispatchSkill(ctx context.Context, in Intent, sink Sink) (Result, error) {
	resolver := d.rt.Skills()
	if resolver == nil {
		fmt.Fprintln(sink.Stderr(), "no skill resolver configured for this shell")
		return Result{ExitCode: 1}, nil
	}

	// If the intent came from a `<bash> | @<sentinel>` form, run the
	// upstream bash first and capture its stdout. The captured output
	// becomes the skill's input/query alongside any explicit args.
	upstream := ""
	if in.Upstream != "" {
		captured, exit, err := d.runUpstream(ctx, in.Upstream, sink)
		if err != nil {
			fmt.Fprintf(sink.Stderr(), "upstream pipe error: %v\n", err)
			return Result{ExitCode: 1}, nil
		}
		if exit != 0 {
			fmt.Fprintf(sink.Stderr(), "upstream pipe exited %d; aborting %s\n", exit, sentinelDisplayLabel(in))
			return Result{ExitCode: exit}, nil
		}
		upstream = captured
	}

	var (
		body string
		err  error
	)
	switch in.Kind {
	case IntentSkill:
		body, err = resolver.Resolve(in.Name)
	case IntentSkillPath:
		body, err = resolver.ResolvePath(in.Path)
	default:
		return Result{ExitCode: 2}, fmt.Errorf("dispatcher: dispatchSkill called with non-skill intent %v", in.Kind)
	}
	if err != nil {
		fmt.Fprintf(sink.Stderr(), "@%s: %v\n", skillLabel(in), err)
		return Result{ExitCode: 1}, nil
	}

	// If the user wrote `@<sentinel> | <bash>`, pipe the skill body
	// through Downstream's bash via stdin and let that bash own the
	// terminal output.
	if in.Downstream != "" {
		toPipe := body
		if upstream != "" {
			// Both forms used at once would be a multi-stage chain
			// (`cmd | @sk | grep`) which we don't support yet. Be
			// explicit rather than producing surprising results.
			fmt.Fprintln(sink.Stderr(),
				"warning: combined upstream + downstream pipes are not yet supported; running downstream with skill body only")
		}
		exit, err := d.runDownstream(ctx, in.Downstream, toPipe, sink)
		if err != nil {
			fmt.Fprintf(sink.Stderr(), "downstream pipe error: %v\n", err)
			return Result{ExitCode: 1}, nil
		}
		return Result{ExitCode: exit, Body: body}, nil
	}

	if body != "" {
		fmt.Fprintln(sink.Stdout(), body)
	}
	if upstream != "" {
		fmt.Fprintf(sink.Stdout(),
			"\n--- input from upstream pipe (%d bytes) ---\n%s\n--- end input ---\n",
			len(upstream), upstream)
	}
	return Result{ExitCode: 0, Body: body}, nil
}

// runDownstream runs `src` (the Y side of an `@x | <bash>` pipeline)
// with `stdin` as its standard input. Stdout/stderr forward live to
// the sink. Returns (exitCode, error).
func (d *Dispatcher) runDownstream(ctx context.Context, src, stdin string, sink Sink) (int, error) {
	return d.rt.session.RunString(ctx, src, bash.Stdio{
		Stdin:  strings.NewReader(stdin),
		Stdout: sink.Stdout(),
		Stderr: sink.Stderr(),
	})
}

// runUpstream runs a bash source (the X side of a pipe) and returns its
// captured stdout. Stderr is forwarded to the sink so the user sees errors
// in real time. Returns (capturedStdout, exitCode, error).
func (d *Dispatcher) runUpstream(ctx context.Context, src string, sink Sink) (string, int, error) {
	var buf bytes.Buffer
	exit, err := d.rt.session.RunString(ctx, src, bash.Stdio{
		Stdout: &buf,
		Stderr: sink.Stderr(),
	})
	return buf.String(), exit, err
}

func sentinelDisplayLabel(in Intent) string {
	if in.Kind == IntentSkillPath {
		return "@" + in.Path
	}
	return "@" + in.Name
}

func (d *Dispatcher) dispatchAgentShot(ctx context.Context, in Intent, sink Sink) (Result, error) {
	provider := d.rt.Provider()
	if provider == nil {
		fmt.Fprintln(sink.Stderr(), ErrNoProvider.Error())
		return Result{ExitCode: 1}, nil
	}
	cwd := d.rt.WorkDir()
	lastCmd, lastOutput := d.rt.History()
	system := agentShotSystem(cwd, lastCmd, lastOutput)

	body, err := OneShot(ctx, provider, d.rt.Model(), system, in.Args, 1024)
	if err != nil {
		if body != "" {
			fmt.Fprintln(sink.Stdout(), body)
		}
		fmt.Fprintf(sink.Stderr(), "!: %v\n", err)
		return Result{ExitCode: 1}, nil
	}
	if strings.TrimSpace(body) == "" {
		fmt.Fprintln(sink.Stderr(), "!: provider returned no text")
		return Result{ExitCode: 1}, nil
	}
	fmt.Fprintln(sink.Stdout(), strings.TrimRight(body, "\n"))
	return Result{ExitCode: 0, Body: body}, nil
}

func (d *Dispatcher) dispatchAgentQA(ctx context.Context, in Intent, sink Sink) (Result, error) {
	provider := d.rt.Provider()
	if provider == nil {
		fmt.Fprintln(sink.Stderr(), ErrNoProvider.Error())
		return Result{ExitCode: 1}, nil
	}
	body, err := OneShot(ctx, provider, d.rt.Model(), agentQASystem, in.Args, 512)
	if err != nil {
		if body != "" {
			fmt.Fprintln(sink.Stdout(), body)
		}
		fmt.Fprintf(sink.Stderr(), "?: %v\n", err)
		return Result{ExitCode: 1}, nil
	}
	if strings.TrimSpace(body) == "" {
		fmt.Fprintln(sink.Stderr(), "?: provider returned no text")
		return Result{ExitCode: 1}, nil
	}
	fmt.Fprintln(sink.Stdout(), strings.TrimRight(body, "\n"))
	return Result{ExitCode: 0, Body: body}, nil
}

func skillLabel(in Intent) string {
	if in.Kind == IntentSkillPath {
		return in.Path
	}
	return in.Name
}

func listShellSafeSlash(rt *ShellRuntime) string {
	if rt == nil || rt.Registry() == nil {
		return "(none)"
	}
	var names []string
	for _, spec := range rt.Registry().List() {
		if spec.ShellSafe {
			names = append(names, "/"+spec.Name)
		}
	}
	if len(names) == 0 {
		return "(none)"
	}
	return strings.Join(names, ", ")
}

// WriterSink adapts a pair of io.Writer values to the Sink interface.
// Used by the --no-tui REPL and tests.
type WriterSink struct {
	StdoutW io.Writer
	StderrW io.Writer
}

// Stdout implements Sink.
func (s WriterSink) Stdout() io.Writer { return s.StdoutW }

// Stderr implements Sink.
func (s WriterSink) Stderr() io.Writer { return s.StderrW }
