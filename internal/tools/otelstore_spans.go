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
		// Top-level span identity fields are stored under their bare names.
		s := traceSpan{
			Name:    rowStr(r, "name", "span.name", "operationName"),
			TraceID: rowStr(r, "trace_id", "traceId", "traceID"),
			SpanID:  rowStr(r, "span_id", "spanId", "spanID"),
		}

		start := rowTime(r, "_time", "start_time", "startTime")
		s.StartTime = start
		// The store records `duration` in MICROSECONDS. Getting the unit wrong here is not a
		// cosmetic slip: a slow-span query thresholds on it, so a 1000x error hides every slow
		// span or flags every fast one.
		if micros := rowNum(r, "duration"); micros > 0 {
			s.EndTime = start.Add(time.Duration(micros) * time.Microsecond)
		} else {
			s.EndTime = rowTime(r, "end_time", "endTime")
		}

		// status_code is numeric in the store (2 = error); ycode's isError() expects the OTel
		// string form. Translate, or a failed span reads as OK.
		switch rowStr(r, "status_code", "status.code") {
		case "2", "STATUS_CODE_ERROR", "Error":
			s.Status = spanStatus{Code: "Error", Description: rowStr(r, "status_message", "status.message")}
		default:
			s.Status = spanStatus{Code: "Ok", Description: rowStr(r, "status_message")}
		}

		// Every attribute becomes a flat attr so attrString("cmd.exit_code") keeps working.
		//
		// The store prefixes attributes by origin (span_attr:, resource_attr:,
		// event:event_attr:NAME:INDEX). Carry them through with the PREFIX STRIPPED — an agent
		// asking for "cmd.exit_code" must not have to know it is really "span_attr:cmd.exit_code".
		// That leakage of the storage schema into the query is exactly the bug that made every
		// otel verb return 0.
		for k, v := range r {
			if v == nil || strings.HasPrefix(k, "_") {
				continue
			}
			key := stripAttrPrefix(k)
			if key == "" {
				continue // structural field (name/trace_id/duration/...) already mapped above
			}
			s.Attributes = append(s.Attributes, spanAttr{
				Key:   key,
				Value: spanValue{Type: "STRING", Value: v},
			})
		}

		// SERVICE is carried explicitly. Without it an agent reading a merged cross-service
		// trace cannot tell WHICH PROCESS a span came from.
		if svc := rowStr(r, "resource_attr:service.name", "service.name", "service"); svc != "" {
			s.Resource = spanResource{Attributes: []spanAttr{{
				Key:   "service.name",
				Value: spanValue{Type: "STRING", Value: svc},
			}}}
		}
		spans = append(spans, s)
	}
	return spans, nil
}

// stripAttrPrefix converts a VictoriaTraces column name to the bare OTel attribute name, or
// returns "" for a structural field that is not an attribute.
//
//	span_attr:cmd.exit_code        -> cmd.exit_code
//	resource_attr:service.name     -> service.name
//	event:event_attr:value.name:0  -> value.name
//	name / trace_id / duration     -> "" (mapped as span identity, not an attribute)
func stripAttrPrefix(k string) string {
	switch {
	case strings.HasPrefix(k, "span_attr:"):
		return strings.TrimPrefix(k, "span_attr:")
	case strings.HasPrefix(k, "resource_attr:"):
		return strings.TrimPrefix(k, "resource_attr:")
	case strings.HasPrefix(k, "event:event_attr:"):
		rest := strings.TrimPrefix(k, "event:event_attr:")
		if i := strings.LastIndex(rest, ":"); i >= 0 {
			return rest[:i] // drop the trailing :INDEX
		}
		return rest
	case strings.HasPrefix(k, "event:"):
		return "" // event_name / event_time — not a queryable attribute
	}
	// Bare structural fields we already mapped as identity.
	switch k {
	case "name", "trace_id", "span_id", "duration", "start_time_unix_nano",
		"end_time_unix_nano", "status_code", "status_message", "scope_name", "flags", "kind":
		return ""
	}
	return k
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
