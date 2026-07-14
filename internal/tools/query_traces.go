package tools

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"
)

// otelDataDir is the OTEL data directory, set via SetOTELDataDir.
var otelDataDir string

// SetOTELDataDir sets the root OTEL data directory for query tools.
func SetOTELDataDir(dir string) {
	otelDataDir = dir
}

// QueryTraces is the exported entry point for trace queries, usable by MCP server.
func QueryTraces(ctx context.Context, input json.RawMessage) (string, error) {
	return handleQueryTraces(ctx, input)
}

// RegisterQueryTracesHandler wires up the query_traces tool handler.
func RegisterQueryTracesHandler(r *Registry) {
	if spec, ok := r.Get("query_traces"); ok {
		spec.Handler = handleQueryTraces
	}
}

type queryTracesInput struct {
	QueryType   string `json:"query_type"`
	Limit       int    `json:"limit"`
	ThresholdMs int    `json:"threshold_ms"`
	SessionID   string `json:"session_id"`
}

// traceSpan represents a parsed span from OTEL JSONL output.
type traceSpan struct {
	Name       string       `json:"Name"`
	TraceID    string       `json:"TraceID"`
	SpanID     string       `json:"SpanID"`
	StartTime  time.Time    `json:"StartTime"`
	EndTime    time.Time    `json:"EndTime"`
	Status     spanStatus   `json:"Status"`
	Attributes []spanAttr   `json:"Attributes"`
	Resource   spanResource `json:"Resource"`
}

type spanStatus struct {
	Code        string `json:"Code"`
	Description string `json:"Description"`
}

type spanAttr struct {
	Key   string    `json:"Key"`
	Value spanValue `json:"Value"`
}

type spanValue struct {
	Type  string `json:"Type"`
	Value any    `json:"Value"`
}

type spanResource struct {
	Attributes []spanAttr `json:"Attributes"`
}

func (s *traceSpan) durationMs() int64 {
	return s.EndTime.Sub(s.StartTime).Milliseconds()
}

func (s *traceSpan) attrString(key string) string {
	for _, a := range s.Attributes {
		if a.Key == key {
			if str, ok := a.Value.Value.(string); ok {
				return str
			}
			return fmt.Sprintf("%v", a.Value.Value)
		}
	}
	return ""
}

func (s *traceSpan) isError() bool {
	return s.Status.Code == "Error" || s.Status.Code == "STATUS_CODE_ERROR"
}

func handleQueryTraces(ctx context.Context, input json.RawMessage) (string, error) {
	// The store is enough on its own. Requiring a local data dir would refuse to answer from
	// the RICHER source because the poorer one was missing.
	if _, up, why := telemetryStore(ctx); !up && otelDataDir == "" {
		return "", fmt.Errorf("no telemetry available: %s, and no local OTEL data directory is configured", why)
	}

	var params queryTracesInput
	if err := json.Unmarshal(input, &params); err != nil {
		return "", fmt.Errorf("parse query_traces input: %w", err)
	}
	if params.Limit <= 0 {
		params.Limit = 20
	}
	if params.Limit > 100 {
		params.Limit = 100
	}

	// Every answer carries WHERE IT CAME FROM. A result that does not say whether it read the
	// store or only ycode's own files lets an empty answer read as "nothing happened" when it
	// means "nobody looked at the place the answer was."
	banner := sourceOf(ctx).Banner()
	withSource := func(out string, err error) (string, error) {
		if err != nil {
			return "", err
		}
		return banner + "\n\n" + out, nil
	}

	switch params.QueryType {
	case "recent_spans":
		return withSource(queryRecentSpans(ctx, params))
	case "slow_spans":
		return withSource(querySlowSpans(ctx, params))
	case "error_spans":
		return withSource(queryErrorSpans(ctx, params))
	case "summary":
		return withSource(queryTraceSummary(ctx, params))
	default:
		return "", fmt.Errorf("unknown query_type: %q (valid: recent_spans, slow_spans, error_spans, summary)", params.QueryType)
	}
}

// loadSpans returns spans, preferring the TELEMETRY STORE over ycode's local JSONL.
//
// The store holds every service's spans and can assemble a trace that crosses processes. The
// JSONL files hold ycode's own spans and structurally cannot -- a file is written by one
// process. When both exist, reading the files instead would answer a cross-service question
// with a single-process fragment, and say nothing about the difference.
//
// The local files remain the fallback, so an agent with no stack running still gets its own
// spans (local-first). The caller announces WHICH was used; see sourceOf/Banner.
func loadSpans(ctx context.Context, sessionFilter string, maxFiles int) ([]traceSpan, error) {
	if _, up, _ := telemetryStore(ctx); up {
		return storeSpans(ctx, sessionFilter, 1000)
	}
	return loadSpansFromFiles(sessionFilter, maxFiles)
}

// loadSpansFromFiles reads JSONL trace files from the data directory.
// It searches both the shared traces dir and per-instance dirs.
func loadSpansFromFiles(sessionFilter string, maxFiles int) ([]traceSpan, error) {
	var paths []string

	// Shared traces dir.
	sharedDir := filepath.Join(otelDataDir, "traces")
	if entries, err := filepath.Glob(filepath.Join(sharedDir, "traces-*.jsonl")); err == nil {
		paths = append(paths, entries...)
	}

	// Per-instance trace dirs.
	instancesDir := filepath.Join(otelDataDir, "instances")
	if entries, err := os.ReadDir(instancesDir); err == nil {
		for _, e := range entries {
			if !e.IsDir() {
				continue
			}
			if sessionFilter != "" && e.Name() != sessionFilter {
				continue
			}
			instTraces := filepath.Join(instancesDir, e.Name(), "traces")
			if files, err := filepath.Glob(filepath.Join(instTraces, "traces-*.jsonl")); err == nil {
				paths = append(paths, files...)
			}
		}
	}

	// Sort by name descending (most recent first), limit files read.
	sort.Sort(sort.Reverse(sort.StringSlice(paths)))
	if maxFiles > 0 && len(paths) > maxFiles {
		paths = paths[:maxFiles]
	}

	var spans []traceSpan
	for _, p := range paths {
		fileSpans, err := parseTraceFile(p)
		if err != nil {
			continue // skip corrupt files
		}
		spans = append(spans, fileSpans...)
	}
	return spans, nil
}

func parseTraceFile(path string) ([]traceSpan, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()

	var spans []traceSpan
	scanner := bufio.NewScanner(f)
	scanner.Buffer(make([]byte, 1024*1024), 1024*1024) // 1MB line limit
	for scanner.Scan() {
		line := scanner.Bytes()
		if len(line) == 0 {
			continue
		}
		var s traceSpan
		if err := json.Unmarshal(line, &s); err != nil {
			continue // skip unparseable lines
		}
		if s.Name != "" {
			spans = append(spans, s)
		}
	}
	return spans, scanner.Err()
}

func queryRecentSpans(ctx context.Context, p queryTracesInput) (string, error) {
	spans, err := loadSpans(ctx, p.SessionID, 3)
	if err != nil {
		return "", err
	}
	sort.Slice(spans, func(i, j int) bool {
		return spans[i].StartTime.After(spans[j].StartTime)
	})
	if len(spans) > p.Limit {
		spans = spans[:p.Limit]
	}
	if len(spans) == 0 {
		return "No trace spans found.", nil
	}

	var b strings.Builder
	b.WriteString("Recent Spans:\n")
	b.WriteString("Time | Name | Duration(ms) | Status\n")
	b.WriteString("---|---|---|---\n")
	for _, s := range spans {
		status := "OK"
		if s.isError() {
			status = "ERROR"
		}
		fmt.Fprintf(&b, "%s | %s | %d | %s\n",
			s.StartTime.Format("15:04:05"), s.Name, s.durationMs(), status)
	}
	return b.String(), nil
}

func querySlowSpans(ctx context.Context, p queryTracesInput) (string, error) {
	threshold := int64(p.ThresholdMs)
	if threshold <= 0 {
		threshold = 5000
	}

	spans, err := loadSpans(ctx, p.SessionID, 3)
	if err != nil {
		return "", err
	}

	var slow []traceSpan
	for _, s := range spans {
		if s.durationMs() >= threshold {
			slow = append(slow, s)
		}
	}
	sort.Slice(slow, func(i, j int) bool {
		return slow[i].durationMs() > slow[j].durationMs()
	})
	if len(slow) > p.Limit {
		slow = slow[:p.Limit]
	}
	if len(slow) == 0 {
		return fmt.Sprintf("No spans exceeded %dms threshold.", threshold), nil
	}

	var b strings.Builder
	fmt.Fprintf(&b, "Slow Spans (>%dms):\n", threshold)
	for _, s := range slow {
		toolName := s.attrString("tool.name")
		label := s.Name
		if toolName != "" {
			label = fmt.Sprintf("%s(%s)", s.Name, toolName)
		}
		fmt.Fprintf(&b, "- %s: %dms at %s\n", label, s.durationMs(), s.StartTime.Format("15:04:05"))
	}
	return b.String(), nil
}

func queryErrorSpans(ctx context.Context, p queryTracesInput) (string, error) {
	spans, err := loadSpans(ctx, p.SessionID, 3)
	if err != nil {
		return "", err
	}

	var errors []traceSpan
	for _, s := range spans {
		if s.isError() {
			errors = append(errors, s)
		}
	}
	sort.Slice(errors, func(i, j int) bool {
		return errors[i].StartTime.After(errors[j].StartTime)
	})
	if len(errors) > p.Limit {
		errors = errors[:p.Limit]
	}
	if len(errors) == 0 {
		return "No error spans found.", nil
	}

	var b strings.Builder
	b.WriteString("Error Spans:\n")
	for _, s := range errors {
		desc := s.Status.Description
		if desc == "" {
			desc = "unknown error"
		}
		fmt.Fprintf(&b, "- %s at %s: %s (%dms)\n",
			s.Name, s.StartTime.Format("15:04:05"), desc, s.durationMs())
	}
	return b.String(), nil
}

func queryTraceSummary(ctx context.Context, p queryTracesInput) (string, error) {
	spans, err := loadSpans(ctx, p.SessionID, 5)
	if err != nil {
		return "", err
	}
	if len(spans) == 0 {
		return "No trace data found.", nil
	}

	counts := make(map[string]int)
	var totalDur int64
	var errorCount int
	for _, s := range spans {
		counts[s.Name]++
		totalDur += s.durationMs()
		if s.isError() {
			errorCount++
		}
	}

	var b strings.Builder
	fmt.Fprintf(&b, "Trace Summary (%d spans):\n", len(spans))
	fmt.Fprintf(&b, "Total duration: %dms | Errors: %d\n\n", totalDur, errorCount)
	b.WriteString("Span counts:\n")

	type kv struct {
		k string
		v int
	}
	var sorted []kv
	for k, v := range counts {
		sorted = append(sorted, kv{k, v})
	}
	sort.Slice(sorted, func(i, j int) bool { return sorted[i].v > sorted[j].v })
	for _, item := range sorted {
		fmt.Fprintf(&b, "- %s: %d\n", item.k, item.v)
	}
	return b.String(), nil
}
