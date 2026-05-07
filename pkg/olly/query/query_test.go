package query

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func TestComposite_UnsupportedWhenNil(t *testing.T) {
	q := New(Backends{})

	if _, err := q.QueryPromQL(context.Background(), "up", time.Time{}); !errors.Is(err, ErrUnsupported) {
		t.Errorf("expected ErrUnsupported for metrics, got %v", err)
	}
	if _, err := q.QueryPromQLRange(context.Background(), "up", time.Time{}, time.Time{}, 0); !errors.Is(err, ErrUnsupported) {
		t.Errorf("expected ErrUnsupported for metrics range, got %v", err)
	}
	if _, err := q.Traces(context.Background(), TraceFilter{}); !errors.Is(err, ErrUnsupported) {
		t.Errorf("expected ErrUnsupported for traces, got %v", err)
	}
	if _, err := q.Logs(context.Background(), LogFilter{}); !errors.Is(err, ErrUnsupported) {
		t.Errorf("expected ErrUnsupported for logs, got %v", err)
	}
}

func TestBuildLogsQL_StarOnEmpty(t *testing.T) {
	got := buildLogsQL(LogFilter{})
	if got != "*" {
		t.Errorf("buildLogsQL(empty) = %q, want %q", got, "*")
	}
}

func TestBuildLogsQL_Composition(t *testing.T) {
	got := buildLogsQL(LogFilter{
		Service:   "ycode",
		SessionID: "sess-123",
		Level:     "ERROR",
		Query:     "tool=bash",
	})
	wantParts := []string{
		`{service.name="ycode"}`,
		`session.id:"sess-123"`,
		`severity_text:"ERROR"`,
		"tool=bash",
	}
	for _, w := range wantParts {
		if !strings.Contains(got, w) {
			t.Errorf("buildLogsQL output %q missing %q", got, w)
		}
	}
}

func TestStringifyTagValue(t *testing.T) {
	cases := []struct {
		in   any
		want string
	}{
		{nil, ""},
		{"hello", "hello"},
		{true, "true"},
		{float64(42), "42"},
		{1.5, "1.5"},
	}
	for _, c := range cases {
		got := stringifyTagValue(c.in)
		if got != c.want {
			t.Errorf("stringifyTagValue(%v) = %q, want %q", c.in, got, c.want)
		}
	}
}

func TestDecodeJaegerTrace_FlattensProcessAndSpanAttrs(t *testing.T) {
	raw := jaegerTrace{
		TraceID: "abc",
		Processes: map[string]jaegerProc{
			"p1": {
				ServiceName: "ycode",
				Tags:        []jaegerTag{{Key: "host.name", Value: "laptop"}},
			},
		},
		Spans: []jaegerSpan{
			{
				TraceID:       "abc",
				SpanID:        "s1",
				OperationName: "turn",
				StartTime:     1_700_000_000_000_000, // 2023-11-14T22:13:20Z µs
				Duration:      1_000_000,             // 1s
				ProcessID:     "p1",
				Tags: []jaegerTag{
					{Key: "session.id", Value: "sess-123"},
					{Key: "otel.status_code", Value: "OK"},
				},
				References: []jaegerRef{{RefType: "CHILD_OF", SpanID: "root"}},
			},
		},
	}
	tr := decodeJaegerTrace(raw)
	if tr.TraceID != "abc" {
		t.Errorf("TraceID = %q", tr.TraceID)
	}
	if len(tr.Spans) != 1 {
		t.Fatalf("expected 1 span, got %d", len(tr.Spans))
	}
	sp := tr.Spans[0]
	if sp.Service != "ycode" {
		t.Errorf("Service = %q", sp.Service)
	}
	if sp.Status != "OK" {
		t.Errorf("Status = %q", sp.Status)
	}
	if sp.ParentID != "root" {
		t.Errorf("ParentID = %q", sp.ParentID)
	}
	if sp.Attrs["session.id"] != "sess-123" {
		t.Errorf("session.id attr = %q", sp.Attrs["session.id"])
	}
	if sp.Attrs["host.name"] != "laptop" {
		t.Errorf("host.name attr should be flattened from process, got %q", sp.Attrs["host.name"])
	}
	if sp.Duration != time.Second {
		t.Errorf("Duration = %v, want 1s", sp.Duration)
	}
}

func TestJaegerAdapter_TracesRequestShape(t *testing.T) {
	var got *http.Request
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		got = r
		_, _ = w.Write([]byte(`{"data":[]}`))
	}))
	defer srv.Close()

	adapter := NewJaegerAdapter(srv.URL+"/traces", srv.Client())
	_, err := adapter.Traces(context.Background(), TraceFilter{
		Service:   "ycode",
		SessionID: "sess-9",
		Limit:     25,
	})
	if err != nil {
		t.Fatalf("Traces: %v", err)
	}
	if got == nil {
		t.Fatal("server did not receive request")
	}
	q := got.URL.Query()
	if q.Get("service") != "ycode" {
		t.Errorf("service param = %q", q.Get("service"))
	}
	if q.Get("limit") != "25" {
		t.Errorf("limit param = %q", q.Get("limit"))
	}
	if q.Get("tags") == "" || !strings.Contains(q.Get("tags"), "sess-9") {
		t.Errorf("tags param missing session.id, got %q", q.Get("tags"))
	}
	if !strings.HasSuffix(got.URL.Path, "/traces/api/traces") {
		t.Errorf("path = %q", got.URL.Path)
	}
}

func TestJaegerAdapter_TraceByIDPathEscape(t *testing.T) {
	var got *http.Request
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		got = r
		_, _ = w.Write([]byte(`{"data":[]}`))
	}))
	defer srv.Close()

	adapter := NewJaegerAdapter(srv.URL, srv.Client())
	_, err := adapter.Traces(context.Background(), TraceFilter{TraceID: "abc/def"})
	if err != nil {
		t.Fatalf("Traces: %v", err)
	}
	if got == nil || !strings.HasSuffix(got.URL.EscapedPath(), "/api/traces/abc%2Fdef") {
		t.Errorf("expected escaped path, got %q", got.URL.EscapedPath())
	}
}

func TestVLAdapter_LogsParsesNDJSON(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Confirm filter translation reaches the wire.
		got := r.URL.Query().Get("query")
		if !strings.Contains(got, `service.name="ycode"`) {
			t.Errorf("query param missing service filter: %q", got)
		}
		w.Header().Set("Content-Type", "application/json")
		recs := []map[string]any{
			{
				"_time":         "2026-05-06T01:02:03Z",
				"_msg":          "hello",
				"service.name":  "ycode",
				"severity_text": "INFO",
				"trace_id":      "t1",
				"custom_attr":   "x",
			},
			{
				"_time": "2026-05-06T01:02:04Z",
				"_msg":  "world",
			},
		}
		for _, rec := range recs {
			b, _ := json.Marshal(rec)
			w.Write(b)
			w.Write([]byte("\n"))
		}
	}))
	defer srv.Close()

	adapter := NewVLAdapter(srv.URL, srv.Client())
	got, err := adapter.Logs(context.Background(), LogFilter{Service: "ycode", Limit: 10})
	if err != nil {
		t.Fatalf("Logs: %v", err)
	}
	if len(got) != 2 {
		t.Fatalf("got %d records, want 2", len(got))
	}
	if got[0].Service != "ycode" || got[0].Body != "hello" || got[0].Level != "INFO" || got[0].TraceID != "t1" {
		t.Errorf("first record decoded incorrectly: %+v", got[0])
	}
	if got[0].Attrs["custom_attr"] != "x" {
		t.Errorf("custom attr lost: %v", got[0].Attrs)
	}
	if got[0].Time.IsZero() {
		t.Errorf("first record time not parsed")
	}
}

func TestVLAdapter_LogsHandlesNonOK(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
		_, _ = w.Write([]byte("boom"))
	}))
	defer srv.Close()

	adapter := NewVLAdapter(srv.URL, srv.Client())
	_, err := adapter.Logs(context.Background(), LogFilter{})
	if err == nil {
		t.Fatal("expected error from non-2xx response")
	}
	if !strings.Contains(err.Error(), "boom") {
		t.Errorf("error should include body, got %v", err)
	}
}
