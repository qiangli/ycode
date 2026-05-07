package query

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"
)

// JaegerAdapter implements TracesBackend by calling Jaeger's HTTP query
// API (compatible with v1 and v2). It is intentionally HTTP-based even
// though the Jaeger collector runs in-process: the v2 query extension
// owns its own HTTP server and offers no in-process Go API.
//
// BaseURL is the prefix up to but not including "/api/...", e.g.
// "http://127.0.0.1:16686/traces" if the embedded Jaeger is configured
// with base_path: "/traces".
type JaegerAdapter struct {
	BaseURL string
	Client  *http.Client
}

// NewJaegerAdapter constructs a JaegerAdapter. If client is nil a default
// http.Client with a 10-second timeout is used.
func NewJaegerAdapter(baseURL string, client *http.Client) *JaegerAdapter {
	if client == nil {
		client = &http.Client{Timeout: 10 * time.Second}
	}
	return &JaegerAdapter{BaseURL: strings.TrimRight(baseURL, "/"), Client: client}
}

// Traces fetches traces matching the filter from the Jaeger query API.
func (j *JaegerAdapter) Traces(ctx context.Context, f TraceFilter) ([]Trace, error) {
	if j == nil || j.BaseURL == "" {
		return nil, ErrUnsupported
	}
	if f.TraceID != "" {
		return j.tracesByID(ctx, f.TraceID)
	}
	q := url.Values{}
	if f.Service != "" {
		q.Set("service", f.Service)
	}
	if f.Operation != "" {
		q.Set("operation", f.Operation)
	}
	if !f.Start.IsZero() {
		q.Set("start", strconv.FormatInt(f.Start.UnixMicro(), 10))
	}
	if !f.End.IsZero() {
		q.Set("end", strconv.FormatInt(f.End.UnixMicro(), 10))
	}
	limit := f.Limit
	if limit <= 0 {
		limit = 50
	}
	q.Set("limit", strconv.Itoa(limit))
	if f.SessionID != "" {
		// Jaeger encodes span tags as "key=value" separated by " ".
		q.Set("tags", fmt.Sprintf(`{"session.id":"%s"}`, f.SessionID))
	}
	endpoint := j.BaseURL + "/api/traces?" + q.Encode()
	return j.fetch(ctx, endpoint)
}

func (j *JaegerAdapter) tracesByID(ctx context.Context, traceID string) ([]Trace, error) {
	endpoint := j.BaseURL + "/api/traces/" + url.PathEscape(traceID)
	return j.fetch(ctx, endpoint)
}

func (j *JaegerAdapter) fetch(ctx context.Context, endpoint string) ([]Trace, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint, nil)
	if err != nil {
		return nil, fmt.Errorf("build jaeger request: %w", err)
	}
	resp, err := j.Client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("jaeger request: %w", err)
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("read jaeger response: %w", err)
	}
	if resp.StatusCode/100 != 2 {
		return nil, fmt.Errorf("jaeger status %d: %s", resp.StatusCode, string(body))
	}
	var raw jaegerResponse
	if err := json.Unmarshal(body, &raw); err != nil {
		return nil, fmt.Errorf("decode jaeger response: %w", err)
	}
	traces := make([]Trace, 0, len(raw.Data))
	for _, t := range raw.Data {
		traces = append(traces, decodeJaegerTrace(t))
	}
	return traces, nil
}

// jaegerResponse mirrors Jaeger's standard query API envelope.
type jaegerResponse struct {
	Data []jaegerTrace `json:"data"`
}

type jaegerTrace struct {
	TraceID   string                `json:"traceID"`
	Spans     []jaegerSpan          `json:"spans"`
	Processes map[string]jaegerProc `json:"processes"`
	Warnings  []string              `json:"warnings,omitempty"`
	Other     map[string]any        `json:"-"`
}

type jaegerSpan struct {
	TraceID       string         `json:"traceID"`
	SpanID        string         `json:"spanID"`
	OperationName string         `json:"operationName"`
	References    []jaegerRef    `json:"references"`
	StartTime     int64          `json:"startTime"` // microseconds
	Duration      int64          `json:"duration"`  // microseconds
	Tags          []jaegerTag    `json:"tags"`
	Logs          []jaegerSpanLg `json:"logs"`
	ProcessID     string         `json:"processID"`
}

type jaegerRef struct {
	RefType string `json:"refType"`
	TraceID string `json:"traceID"`
	SpanID  string `json:"spanID"`
}

type jaegerTag struct {
	Key   string `json:"key"`
	Type  string `json:"type"`
	Value any    `json:"value"`
}

type jaegerProc struct {
	ServiceName string      `json:"serviceName"`
	Tags        []jaegerTag `json:"tags"`
}

type jaegerSpanLg struct {
	Timestamp int64       `json:"timestamp"`
	Fields    []jaegerTag `json:"fields"`
}

func decodeJaegerTrace(t jaegerTrace) Trace {
	out := Trace{TraceID: t.TraceID}
	out.Spans = make([]Span, 0, len(t.Spans))
	var minStart, maxEnd time.Time
	for _, s := range t.Spans {
		span := Span{
			SpanID:   s.SpanID,
			Name:     s.OperationName,
			Start:    time.UnixMicro(s.StartTime),
			Duration: time.Duration(s.Duration) * time.Microsecond,
			Attrs:    map[string]string{},
		}
		for _, r := range s.References {
			if r.RefType == "CHILD_OF" {
				span.ParentID = r.SpanID
			}
		}
		for _, tag := range s.Tags {
			span.Attrs[tag.Key] = stringifyTagValue(tag.Value)
			if tag.Key == "otel.status_code" {
				span.Status = stringifyTagValue(tag.Value)
			}
		}
		if proc, ok := t.Processes[s.ProcessID]; ok {
			span.Service = proc.ServiceName
			for _, tag := range proc.Tags {
				if _, exists := span.Attrs[tag.Key]; !exists {
					span.Attrs[tag.Key] = stringifyTagValue(tag.Value)
				}
			}
		}
		if minStart.IsZero() || span.Start.Before(minStart) {
			minStart = span.Start
			out.Service = span.Service
		}
		if end := span.Start.Add(span.Duration); end.After(maxEnd) {
			maxEnd = end
		}
		out.Spans = append(out.Spans, span)
	}
	out.Start = minStart
	if !maxEnd.IsZero() && !minStart.IsZero() {
		out.Duration = maxEnd.Sub(minStart)
	}
	return out
}

func stringifyTagValue(v any) string {
	switch t := v.(type) {
	case nil:
		return ""
	case string:
		return t
	case bool:
		return strconv.FormatBool(t)
	case float64:
		// Jaeger returns ints as JSON numbers (float64). Avoid scientific
		// notation for whole numbers; otherwise show full precision.
		if t == float64(int64(t)) {
			return strconv.FormatInt(int64(t), 10)
		}
		return strconv.FormatFloat(t, 'f', -1, 64)
	default:
		b, _ := json.Marshal(t)
		return string(b)
	}
}
