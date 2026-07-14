package tools

import (
	"context"
	"fmt"
	"strings"
	"time"
)

// storeSpans fetches spans from the telemetry store and maps them into the same traceSpan
// shape the JSONL reader produces — so every query_type (recent/slow/error/summary) works
// against the store with no further change.
//
// The mapping is the point: the store holds spans from EVERY service (bashy's per-command
// spans, with cmd.exit_code, alongside ycode's LLM spans), and a trace assembled here can
// cross processes. The JSONL reader structurally cannot do that — a file holds one process.
func storeSpans(ctx context.Context, sessionFilter string, limit int) ([]traceSpan, error) {
	c, up, why := telemetryStore(ctx)
	if !up {
		return nil, fmt.Errorf("%s", why)
	}
	if limit <= 0 || limit > 1000 {
		limit = 1000
	}

	// LogsQL. An empty query matches everything; a session filter narrows it.
	query := "*"
	if s := strings.TrimSpace(sessionFilter); s != "" {
		query = fmt.Sprintf("%q", s)
	}

	rows, _, err := c.Spans(ctx, query, 24*time.Hour, limit)
	if err != nil {
		return nil, fmt.Errorf("telemetry store query failed: %w", err)
	}

	spans := make([]traceSpan, 0, len(rows))
	for _, r := range rows {
		s := traceSpan{
			Name:    rowStr(r, "span.name", "name", "operationName"),
			TraceID: rowStr(r, "trace_id", "traceId", "traceID"),
			SpanID:  rowStr(r, "span_id", "spanId", "spanID"),
		}

		start := rowTime(r, "_time", "start_time", "startTime")
		s.StartTime = start
		if d := rowNum(r, "duration_ms", "duration"); d > 0 {
			s.EndTime = start.Add(time.Duration(d) * time.Millisecond)
		} else {
			s.EndTime = rowTime(r, "end_time", "endTime")
		}

		if code := rowStr(r, "status.code", "status_code", "statusCode"); code != "" {
			s.Status = spanStatus{
				Code:        code,
				Description: rowStr(r, "status.message", "status_description"),
			}
		}

		// Everything else becomes an attribute, so attrString() keeps working — including
		// the attributes that only exist in the store, like bashy's cmd.exit_code.
		//
		// The SERVICE is carried as an attribute too. Without it an agent reading a merged
		// trace cannot tell WHICH PROCESS a span came from, and a cross-service trace whose
		// spans are unattributed is harder to reason about than no trace at all.
		for k, v := range r {
			if v == nil || strings.HasPrefix(k, "_") {
				continue
			}
			s.Attributes = append(s.Attributes, spanAttr{
				Key:   k,
				Value: spanValue{Type: "STRING", Value: v},
			})
		}
		if svc := rowStr(r, "service.name", "service"); svc != "" {
			s.Resource = spanResource{Attributes: []spanAttr{{
				Key:   "service.name",
				Value: spanValue{Type: "STRING", Value: svc},
			}}}
		}
		spans = append(spans, s)
	}
	return spans, nil
}

func rowStr(row map[string]any, names ...string) string {
	for _, n := range names {
		if v, ok := row[n]; ok && v != nil {
			if s, ok := v.(string); ok && s != "" {
				return s
			}
			if s := fmt.Sprint(v); s != "" && s != "<nil>" {
				return s
			}
		}
	}
	return ""
}

func rowNum(row map[string]any, names ...string) float64 {
	for _, n := range names {
		v, ok := row[n]
		if !ok || v == nil {
			continue
		}
		switch t := v.(type) {
		case float64:
			return t
		case int64:
			return float64(t)
		case string:
			var f float64
			if _, err := fmt.Sscanf(t, "%g", &f); err == nil {
				return f
			}
		}
	}
	return 0
}

func rowTime(row map[string]any, names ...string) time.Time {
	for _, n := range names {
		s := rowStr(row, n)
		if s == "" {
			continue
		}
		for _, layout := range []string{time.RFC3339Nano, time.RFC3339} {
			if t, err := time.Parse(layout, s); err == nil {
				return t
			}
		}
	}
	return time.Time{}
}
