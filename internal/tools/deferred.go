package tools

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"sort"
	"strings"

	"github.com/qiangli/ycode/internal/storage"
)

// SearchScore represents a tool search result with relevance score.
type SearchScore struct {
	Spec  *ToolSpec
	Score int
}

// bashNativeKeywords are CLI commands that should be run via bash, not discovered
// via ToolSearch. When a query matches, ToolSearch returns a redirect hint instead
// of "No matching tools found" to prevent the agent from looping.
var bashNativeKeywords = []string{
	"ssh", "scp", "rsync", "ping", "curl", "wget", "nc", "netcat", "nmap",
	"telnet", "traceroute", "dig", "nslookup", "host", "ifconfig", "ip",
	"docker", "podman", "kubectl", "terraform", "ansible",
	"tar", "zip", "unzip", "gzip",
	"ps", "top", "kill", "htop", "lsof", "netstat", "ss",
	"connect", "remote", "login",
}

// isBashNativeQuery returns true if the query is asking about a standard CLI command.
func isBashNativeQuery(query string) bool {
	lower := strings.ToLower(query)
	for _, kw := range bashNativeKeywords {
		if strings.Contains(lower, kw) {
			return true
		}
	}
	return false
}

// RegisterToolSearchHandler registers the ToolSearch handler.
func RegisterToolSearchHandler(r *Registry) {
	spec, ok := r.Get("ToolSearch")
	if !ok {
		return
	}
	spec.Handler = func(ctx context.Context, input json.RawMessage) (string, error) {
		var params struct {
			Query      string `json:"query"`
			MaxResults int    `json:"max_results,omitempty"`
		}
		if err := json.Unmarshal(input, &params); err != nil {
			return "", fmt.Errorf("parse ToolSearch input: %w", err)
		}

		maxResults := params.MaxResults
		if maxResults <= 0 {
			maxResults = 5
		}

		results := SearchTools(r, params.Query, maxResults)
		if len(results) == 0 {
			if isBashNativeQuery(params.Query) {
				return "No matching tools found. This looks like a standard CLI operation — use the bash tool directly (e.g., ssh, ping, curl).", nil
			}
			return "No matching tools found.", nil
		}

		// Return full tool schemas so the LLM can invoke discovered tools.
		// Format matches the <functions> block pattern used by the harness.
		var sb strings.Builder
		sb.WriteString("<functions>\n")
		for _, res := range results {
			schema := toolDefJSON(res.Spec)
			fmt.Fprintf(&sb, "<function>%s</function>\n", schema)
		}
		sb.WriteString("</functions>")
		return sb.String(), nil
	}
}

// SearchTools scores and ranks tools against a query.
func SearchTools(r *Registry, query string, maxResults int) []SearchScore {
	queryLower := strings.ToLower(query)
	parts := strings.Fields(queryLower)

	// Handle "select:Name1,Name2" syntax.
	if strings.HasPrefix(queryLower, "select:") {
		names := strings.Split(strings.TrimPrefix(queryLower, "select:"), ",")
		var results []SearchScore
		for _, name := range names {
			name = strings.TrimSpace(name)
			if spec, ok := r.Get(name); ok {
				results = append(results, SearchScore{Spec: spec, Score: 100})
			}
		}
		return results
	}

	// Try Bleve-backed search when available.
	if idx := r.SearchIndex(); idx != nil {
		if results := idx.Search(r, query, maxResults); len(results) > 0 {
			return results
		}
		// Fall through to keyword matching if Bleve returns nothing.
	}

	var scores []SearchScore
	for _, spec := range r.All() {
		score := scoreMatch(spec, parts)
		if score > 0 {
			scores = append(scores, SearchScore{Spec: spec, Score: score})
		}
	}

	sort.Slice(scores, func(i, j int) bool {
		return scores[i].Score > scores[j].Score
	})

	if len(scores) > maxResults {
		scores = scores[:maxResults]
	}
	return scores
}

const toolSearchIndexName = "tools"

// ToolSearchIndex provides Bleve-backed tool discovery.
type ToolSearchIndex struct {
	index storage.SearchIndex
}

// NewToolSearchIndex creates a Bleve-backed tool search index.
func NewToolSearchIndex(index storage.SearchIndex) *ToolSearchIndex {
	return &ToolSearchIndex{index: index}
}

// IndexTools indexes all registered tools in Bleve for semantic discovery.
func (t *ToolSearchIndex) IndexTools(registry *Registry) {
	var docs []storage.Document
	for _, spec := range registry.All() {
		docs = append(docs, storage.Document{
			ID:      spec.Name,
			Content: spec.Name + " " + spec.Description,
			Metadata: map[string]string{
				"name":        spec.Name,
				"description": spec.Description,
				"category":    fmt.Sprintf("%d", spec.Category),
			},
		})
	}
	if len(docs) > 0 {
		ctx := context.Background()
		if err := t.index.BatchIndex(ctx, toolSearchIndexName, docs); err != nil {
			slog.Debug("bleve: index tools", "error", err)
		}
	}
}

// Search finds tools matching a query using Bleve full-text search.
func (t *ToolSearchIndex) Search(registry *Registry, query string, maxResults int) []SearchScore {
	ctx := context.Background()
	results, err := t.index.Search(ctx, toolSearchIndexName, query, maxResults)
	if err != nil {
		slog.Debug("bleve: search tools", "error", err)
		return nil
	}

	var scores []SearchScore
	for _, r := range results {
		name := r.Document.Metadata["name"]
		if spec, ok := registry.Get(name); ok {
			scores = append(scores, SearchScore{
				Spec:  spec,
				Score: int(r.Score * 10), // Scale Bleve scores to match existing scoring range.
			})
		}
	}
	return scores
}

// toolDefJSON returns a compact JSON representation of a tool spec
// suitable for inclusion in ToolSearch results.
func toolDefJSON(spec *ToolSpec) string {
	def := struct {
		Name        string          `json:"name"`
		Description string          `json:"description"`
		Parameters  json.RawMessage `json:"parameters"`
	}{
		Name:        spec.Name,
		Description: spec.Description,
		Parameters:  spec.InputSchema,
	}
	b, err := json.Marshal(def)
	if err != nil {
		return fmt.Sprintf(`{"name":%q,"description":%q}`, spec.Name, spec.Description)
	}
	return string(b)
}

// scoreMatch computes a relevance score.
func scoreMatch(spec *ToolSpec, queryParts []string) int {
	score := 0
	nameLower := strings.ToLower(spec.Name)
	descLower := strings.ToLower(spec.Description)

	for _, part := range queryParts {
		// Exact name match: +12
		if nameLower == part {
			score += 12
		}
		// Name contains: +8
		if strings.Contains(nameLower, part) {
			score += 8
		}
		// Description contains: +4
		if strings.Contains(descLower, part) {
			score += 4
		}
	}

	return score
}
