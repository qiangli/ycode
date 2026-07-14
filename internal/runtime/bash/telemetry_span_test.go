package bash

import (
	"context"
	"testing"

	"go.opentelemetry.io/otel"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	"go.opentelemetry.io/otel/sdk/trace/tracetest"
)

// ycode's shell must emit an exec span, like bashy's does.
//
// This test exists because three consecutive end-to-end runs through a live LLM produced
// no span, and each time the plausible explanation was different: the model did not call
// the tool; `echo` is a builtin and never reaches an ExecHandler; there are TWO
// interpreters and only one was wired. Every one of those was true at some point, and
// none of them was the whole story — because a live-LLM run has too many variables to
// isolate anything.
//
// So: no model, no network, no shell tool. One command, one assertion. If this passes and
// the live run still shows nothing, the problem is upstream of the shell entirely.
func TestYcodeShellEmitsAnExecSpan(t *testing.T) {
	sr := tracetest.NewSpanRecorder()
	tp := sdktrace.NewTracerProvider(sdktrace.WithSpanProcessor(sr))
	old := otel.GetTracerProvider()
	otel.SetTracerProvider(tp)
	t.Cleanup(func() { otel.SetTracerProvider(old) })

	// `ls` is NOT a shell builtin, so it must cross the ExecHandler chain. (`echo` is,
	// and is handled by the interpreter before any handler runs — which is why an early
	// version of this check found nothing and looked like a bug.)
	res, err := Execute(context.Background(), ExecParams{Command: "ls /"})
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if res.ExitCode != 0 {
		t.Fatalf("ls / exited %d: %s", res.ExitCode, res.Stderr)
	}

	var execSpans []string
	for _, s := range sr.Ended() {
		for _, kv := range s.Attributes() {
			if string(kv.Key) == "cmd.name" {
				execSpans = append(execSpans, kv.Value.Emit())
			}
		}
	}

	if len(execSpans) == 0 {
		var names []string
		for _, s := range sr.Ended() {
			names = append(names, s.Name())
		}
		t.Fatalf("ycode ran a command and emitted NO exec span.\nspans seen: %v\n\n"+
			"The command crossed no instrumented ExecHandler — so ycode's shell is not "+
			"bashy's shell, and every command ycode runs is invisible on the OTel plane.", names)
	}
	t.Logf("exec spans: %v", execSpans)
}
