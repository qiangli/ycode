package computer

import (
	"context"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	"go.opentelemetry.io/otel"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"

	"github.com/qiangli/ycode/internal/runtime/fileops"
)

// recorder collects ended spans for assertions. Implements
// sdktrace.SpanExporter with a thread-safe in-memory slice.
type recorder struct {
	mu    sync.Mutex
	spans []sdktrace.ReadOnlySpan
}

func (r *recorder) ExportSpans(_ context.Context, ss []sdktrace.ReadOnlySpan) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.spans = append(r.spans, ss...)
	return nil
}
func (r *recorder) Shutdown(context.Context) error { return nil }

func (r *recorder) names() []string {
	r.mu.Lock()
	defer r.mu.Unlock()
	out := make([]string, len(r.spans))
	for i, s := range r.spans {
		out[i] = s.Name()
	}
	return out
}

// installRecorder swaps in a SimpleSpanProcessor-backed tracer
// provider for the duration of the test.
func installRecorder(t *testing.T) *recorder {
	t.Helper()
	rec := &recorder{}
	tp := sdktrace.NewTracerProvider(sdktrace.WithSpanProcessor(sdktrace.NewSimpleSpanProcessor(rec)))
	prev := otel.GetTracerProvider()
	otel.SetTracerProvider(tp)
	t.Cleanup(func() {
		_ = tp.Shutdown(context.Background())
		otel.SetTracerProvider(prev)
	})
	return rec
}

func TestSpans_FileRoundTrip(t *testing.T) {
	rec := installRecorder(t)
	c, tmp := newTestComputer(t)
	ctx := context.Background()
	path := filepath.Join(tmp, "trace.txt")

	if err := c.Files().Write(ctx, fileops.WriteFileParams{Path: path, Content: "x"}); err != nil {
		t.Fatalf("Write: %v", err)
	}
	if _, err := c.Files().Read(ctx, fileops.ReadFileParams{Path: path}); err != nil {
		t.Fatalf("Read: %v", err)
	}

	got := rec.names()
	want := map[string]bool{
		"ycode.computer.files.write": false,
		"ycode.computer.files.read":  false,
	}
	for _, n := range got {
		if _, ok := want[n]; ok {
			want[n] = true
		}
	}
	for n, seen := range want {
		if !seen {
			t.Errorf("missing span %q (got %v)", n, got)
		}
	}
}

func TestSpans_FetchErrorRecorded(t *testing.T) {
	rec := installRecorder(t)
	c, _ := newTestComputer(t)
	// 127.0.0.1 is rejected by SSRF guard; we expect a span with
	// status=Error and the http.url attribute populated.
	if _, err := c.Web().Fetch(context.Background(), "http://127.0.0.1:1/never", FetchOpts{}); err == nil {
		t.Fatal("expected SSRF error")
	}
	rec.mu.Lock()
	defer rec.mu.Unlock()
	if len(rec.spans) == 0 {
		t.Fatal("no spans recorded")
	}
	last := rec.spans[len(rec.spans)-1]
	if !strings.HasPrefix(last.Name(), "ycode.computer.web.") {
		t.Errorf("last span name = %q, want prefix ycode.computer.web.", last.Name())
	}
	if last.Status().Code.String() == "Unset" || last.Status().Code.String() == "Ok" {
		t.Errorf("expected Error status, got %v", last.Status().Code)
	}
}
