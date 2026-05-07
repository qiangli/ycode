package computer

import (
	"context"
	"time"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/codes"
	"go.opentelemetry.io/otel/trace"
)

// tracerName is the OTEL instrumentation scope for the computer
// gateway. All spans emitted from this package use it.
const tracerName = "ycode.computer"

// Span attribute keys. Names follow OpenTelemetry semantic
// conventions where one applies and otherwise mirror the existing
// internal/telemetry/otel/attributes.go style.
var (
	AttrComputerName = attribute.Key("computer.name")
	AttrSurface      = attribute.Key("computer.surface")
	AttrOp           = attribute.Key("computer.op")
	AttrDurationMs   = attribute.Key("computer.duration_ms")
	AttrForked       = attribute.Key("computer.shell.forked")

	AttrCmdBinary  = attribute.Key("cmd.binary")
	AttrCmdLen     = attribute.Key("cmd.length")
	AttrExitCode   = attribute.Key("cmd.exit_code")
	AttrCmdTimeout = attribute.Key("cmd.timeout_ms")

	AttrFilePath  = attribute.Key("file.path")
	AttrFileBytes = attribute.Key("file.bytes")
	AttrFileBin   = attribute.Key("file.binary")

	AttrGlobPattern = attribute.Key("glob.pattern")
	AttrGrepPattern = attribute.Key("grep.pattern")
	AttrMatchCount  = attribute.Key("match.count")

	AttrURL        = attribute.Key("http.url")
	AttrHTTPStatus = attribute.Key("http.status")
	AttrHTTPBytes  = attribute.Key("http.bytes")

	AttrBrowserURL  = attribute.Key("browser.url")
	AttrBrowserSel  = attribute.Key("browser.selector")
	AttrBrowserCond = attribute.Key("browser.wait.cond")
)

// startSpan begins a span named "ycode.computer.<surface>.<op>" with
// the computer name attribute pre-set. Callers add op-specific
// attributes via the returned span and end it via the returned
// finish function which records duration + error and calls End.
func startSpan(ctx context.Context, computerName, surface, op string, attrs ...attribute.KeyValue) (context.Context, trace.Span, func(err error)) {
	tracer := otel.Tracer(tracerName)
	spanName := "ycode.computer." + surface + "." + op
	base := []attribute.KeyValue{
		AttrComputerName.String(computerName),
		AttrSurface.String(surface),
		AttrOp.String(op),
	}
	if len(attrs) > 0 {
		base = append(base, attrs...)
	}
	ctx, span := tracer.Start(ctx, spanName, trace.WithAttributes(base...))
	start := time.Now()
	finish := func(err error) {
		span.SetAttributes(AttrDurationMs.Int64(time.Since(start).Milliseconds()))
		if err != nil {
			span.RecordError(err)
			span.SetStatus(codes.Error, err.Error())
		}
		span.End()
	}
	return ctx, span, finish
}
