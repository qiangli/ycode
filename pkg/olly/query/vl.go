package query

import (
	"bufio"
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

// VLAdapter implements LogsBackend by calling VictoriaLogs' /select/logsql
// HTTP API. BaseURL points at the VL HTTP root (the URL just before
// "/select/..."), e.g. "http://127.0.0.1:9428/logs" if VL was started
// with -http.pathPrefix=/logs.
type VLAdapter struct {
	BaseURL string
	Client  *http.Client
}

// NewVLAdapter constructs a VLAdapter. If client is nil a default
// http.Client with a 10-second timeout is used.
func NewVLAdapter(baseURL string, client *http.Client) *VLAdapter {
	if client == nil {
		client = &http.Client{Timeout: 10 * time.Second}
	}
	return &VLAdapter{BaseURL: strings.TrimRight(baseURL, "/"), Client: client}
}

// Logs fetches logs matching the filter. Filters are translated into a
// LogsQL expression; if f.Query is non-empty it is concatenated.
func (v *VLAdapter) Logs(ctx context.Context, f LogFilter) ([]LogRecord, error) {
	if v == nil || v.BaseURL == "" {
		return nil, ErrUnsupported
	}
	expr := buildLogsQL(f)
	q := url.Values{}
	q.Set("query", expr)
	if !f.Start.IsZero() {
		q.Set("start", strconv.FormatInt(f.Start.UnixNano(), 10))
	}
	if !f.End.IsZero() {
		q.Set("end", strconv.FormatInt(f.End.UnixNano(), 10))
	}
	limit := f.Limit
	if limit <= 0 {
		limit = 200
	}
	q.Set("limit", strconv.Itoa(limit))
	endpoint := v.BaseURL + "/select/logsql/query?" + q.Encode()

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint, nil)
	if err != nil {
		return nil, fmt.Errorf("build vl request: %w", err)
	}
	resp, err := v.Client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("vl request: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode/100 != 2 {
		body, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("vl status %d: %s", resp.StatusCode, string(body))
	}

	// VictoriaLogs streams newline-delimited JSON.
	out := make([]LogRecord, 0, limit)
	scanner := bufio.NewScanner(resp.Body)
	scanner.Buffer(make([]byte, 64*1024), 4*1024*1024)
	for scanner.Scan() {
		line := scanner.Bytes()
		if len(line) == 0 {
			continue
		}
		rec, err := decodeVLRecord(line)
		if err != nil {
			continue // skip malformed entries rather than fail the whole batch
		}
		out = append(out, rec)
	}
	if err := scanner.Err(); err != nil {
		return nil, fmt.Errorf("read vl stream: %w", err)
	}
	return out, nil
}

// buildLogsQL composes a LogsQL expression from a LogFilter.
//
// The grammar used here is intentionally conservative — service name
// becomes a stream filter, the rest become field-value filters, and any
// caller-supplied Query string is appended verbatim.
func buildLogsQL(f LogFilter) string {
	var parts []string
	if f.Service != "" {
		parts = append(parts, fmt.Sprintf(`{service.name=%q}`, f.Service))
	}
	if f.TraceID != "" {
		parts = append(parts, fmt.Sprintf(`trace_id:%q`, f.TraceID))
	}
	if f.SessionID != "" {
		parts = append(parts, fmt.Sprintf(`session.id:%q`, f.SessionID))
	}
	if f.Level != "" {
		parts = append(parts, fmt.Sprintf(`severity_text:%q`, f.Level))
	}
	if f.Query != "" {
		parts = append(parts, f.Query)
	}
	if len(parts) == 0 {
		return "*"
	}
	return strings.Join(parts, " ")
}

// decodeVLRecord parses one NDJSON line from VictoriaLogs into a LogRecord.
func decodeVLRecord(line []byte) (LogRecord, error) {
	var raw map[string]any
	if err := json.Unmarshal(line, &raw); err != nil {
		return LogRecord{}, err
	}
	rec := LogRecord{Attrs: make(map[string]string, len(raw))}
	for k, v := range raw {
		s := stringifyTagValue(v)
		switch k {
		case "_time":
			if t, err := time.Parse(time.RFC3339Nano, s); err == nil {
				rec.Time = t
			}
		case "_msg":
			rec.Body = s
		case "service.name":
			rec.Service = s
		case "trace_id":
			rec.TraceID = s
		case "span_id":
			rec.SpanID = s
		case "severity_text":
			rec.Level = s
		default:
			rec.Attrs[k] = s
		}
	}
	return rec, nil
}
