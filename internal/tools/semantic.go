package tools

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/qiangli/ycode/internal/storage"
)

// vectorStore is an optional vector store for semantic search.
var vectorStore storage.VectorStore

// SetVectorStore sets the vector store used for semantic code search.
func SetVectorStore(store storage.VectorStore) {
	vectorStore = store
}

// RegisterSemanticSearchHandler registers the semantic_search tool.
// This tool uses vector similarity to find code semantically,
// complementing the regex-based grep and pattern-based glob tools.
func RegisterSemanticSearchHandler(r *Registry) {
	r.Register(&ToolSpec{
		Name: "semantic_search",
		Description: "Search code semantically by meaning rather than exact text. " +
			"Use when looking for concepts, functionality, or patterns that may not match a simple regex. " +
			"Example: 'authentication middleware', 'error handling for database connections'.",
		InputSchema: json.RawMessage(`{
			"type": "object",
			"properties": {
				"query": {
					"type": "string",
					"description": "Natural language description of what you're looking for"
				},
				"collection": {
					"type": "string",
					"description": "Which collection to search: 'codebase', 'memory', 'sessions', 'docs'. Default: 'codebase'",
					"enum": ["codebase", "memory", "sessions", "docs"]
				},
				"max_results": {
					"type": "integer",
					"description": "Maximum number of results (default: 10)"
				}
			},
			"required": ["query"]
		}`),
		AlwaysAvailable: false, // deferred tool
		Handler: func(ctx context.Context, input json.RawMessage) (string, error) {
			if vectorStore == nil {
				return "Semantic search is not available (vector store not initialized).", nil
			}

			var params struct {
				Query      string `json:"query"`
				Collection string `json:"collection"`
				MaxResults int    `json:"max_results"`
			}
			if err := json.Unmarshal(input, &params); err != nil {
				return "", fmt.Errorf("parse semantic_search input: %w", err)
			}

			if params.Collection == "" {
				params.Collection = "codebase"
			}
			if params.MaxResults <= 0 {
				params.MaxResults = 10
			}

			results, err := vectorStore.QueryByText(ctx, params.Collection, params.Query, params.MaxResults)
			if err != nil {
				return "", fmt.Errorf("semantic search: %w", err)
			}

			if len(results) == 0 {
				return "No semantic matches found.", nil
			}

			var sb strings.Builder
			for _, r := range results {
				path := r.Document.Metadata["path"]
				if path == "" {
					path = r.Document.ID
				}
				score := fmt.Sprintf("%.2f", r.Score)

				// Show file path and score.
				fmt.Fprintf(&sb, "--- %s (score: %s) ---\n", path, score)

				// Show a snippet of the content.
				content := r.Document.Content
				if len(content) > 500 {
					content = content[:500] + "..."
				}
				sb.WriteString(content)
				sb.WriteString("\n\n")
			}

			return sb.String(), nil
		},
	})
}
