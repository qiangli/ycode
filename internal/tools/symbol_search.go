package tools

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"strings"
	"time"

	"github.com/qiangli/ycode/internal/storage"
)

const symbolIndexName = "symbols"

// RegisterSymbolSearchHandler registers the symbol_search tool for searching
// code symbols (functions, types, interfaces, classes) by name, kind, and language.
func RegisterSymbolSearchHandler(r *Registry) {
	r.Register(&ToolSpec{
		Name: "symbol_search",
		Description: "Search for code symbols (functions, types, interfaces, classes, methods) by name. " +
			"More precise than grep for finding definitions. " +
			"Supports filtering by kind (func, type, interface, method, class, const, var), language, and export status.",
		InputSchema: json.RawMessage(`{
			"type": "object",
			"properties": {
				"query": {
					"type": "string",
					"description": "Symbol name or partial name to search for"
				},
				"kind": {
					"type": "string",
					"description": "Filter by symbol kind",
					"enum": ["func", "type", "interface", "method", "class", "const", "var"]
				},
				"language": {
					"type": "string",
					"description": "Filter by language (go, py, ts, js, rs, java, rb)"
				},
				"exported_only": {
					"type": "boolean",
					"description": "Only return exported/public symbols (default: false)"
				},
				"max_results": {
					"type": "integer",
					"description": "Maximum results to return (default: 50)"
				}
			},
			"required": ["query"]
		}`),
		AlwaysAvailable: false, // deferred tool
		Handler: func(ctx context.Context, input json.RawMessage) (string, error) {
			if codeSearchIndex == nil {
				return "Symbol search is not available (search index not initialized).", nil
			}

			var params struct {
				Query        string `json:"query"`
				Kind         string `json:"kind"`
				Language     string `json:"language"`
				ExportedOnly bool   `json:"exported_only"`
				MaxResults   int    `json:"max_results"`
			}
			if err := json.Unmarshal(input, &params); err != nil {
				return "", fmt.Errorf("parse symbol_search input: %w", err)
			}

			if params.MaxResults <= 0 {
				params.MaxResults = 50
			}

			// Build filters for metadata fields.
			filters := make(map[string]string)
			if params.Kind != "" {
				filters["kind"] = params.Kind
			}
			if params.Language != "" {
				filters["language"] = params.Language
			}
			if params.ExportedOnly {
				filters["exported"] = "true"
			}

			start := time.Now()
			var results []storage.SearchResult
			var err error

			if len(filters) > 0 {
				results, err = codeSearchIndex.SearchWithFilter(ctx, symbolIndexName, params.Query, filters, params.MaxResults)
			} else {
				results, err = codeSearchIndex.Search(ctx, symbolIndexName, params.Query, params.MaxResults)
			}
			dur := time.Since(start)
			if searchInstruments != nil {
				searchInstruments.SearchSymbolTotal.Add(ctx, 1)
				searchInstruments.SearchSymbolDuration.Record(ctx, float64(dur.Milliseconds()))
			}
			slog.Debug("symbol_search", "query", params.Query, "kind", params.Kind,
				"duration_ms", dur.Milliseconds(), "results", len(results))
			if err != nil {
				return "", fmt.Errorf("symbol search: %w", err)
			}

			if len(results) == 0 {
				return "No symbols found.", nil
			}

			var sb strings.Builder
			for _, r := range results {
				file := r.Document.Metadata["file"]
				line := r.Document.Metadata["line"]
				kind := r.Document.Metadata["kind"]
				name := r.Document.Metadata["name"]
				sig := r.Document.Metadata["signature"]

				if sig != "" {
					fmt.Fprintf(&sb, "%s:%s: %s\n", file, line, sig)
				} else {
					fmt.Fprintf(&sb, "%s:%s: %s %s\n", file, line, kind, name)
				}
			}

			return sb.String(), nil
		},
	})
}
