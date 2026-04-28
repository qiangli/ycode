package tools

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"strings"

	"github.com/qiangli/ycode/internal/runtime/indexer"
)

// refGraph is an optional reference graph for find_references/find_impact tools.
var refGraph *indexer.RefGraph

// SetRefGraph sets the reference graph used by reference/impact tools.
func SetRefGraph(g *indexer.RefGraph) {
	refGraph = g
}

// RegisterReferenceHandlers registers find_references and find_impact tools.
func RegisterReferenceHandlers(r *Registry) {
	// find_references: "Who calls this function?" / "Who uses this type?"
	r.Register(&ToolSpec{
		Name: "find_references",
		Description: "Find all references to a symbol — who calls a function, who uses a type. " +
			"Works on Go files. Provide the symbol name (e.g., 'HandleRequest', 'Server', 'pkg.Func').",
		InputSchema: json.RawMessage(`{
			"type": "object",
			"properties": {
				"symbol": {
					"type": "string",
					"description": "Symbol name to find references for (e.g., 'HandleRequest', 'indexer.New')"
				},
				"direction": {
					"type": "string",
					"description": "Direction: 'callers' (who calls this?) or 'callees' (what does this call?). Default: callers",
					"enum": ["callers", "callees"]
				}
			},
			"required": ["symbol"]
		}`),
		AlwaysAvailable: false,
		Handler: func(ctx context.Context, input json.RawMessage) (string, error) {
			if refGraph == nil {
				return "Reference graph is not available (indexer not initialized).", nil
			}

			var params struct {
				Symbol    string `json:"symbol"`
				Direction string `json:"direction"`
			}
			if err := json.Unmarshal(input, &params); err != nil {
				return "", fmt.Errorf("parse find_references input: %w", err)
			}

			if params.Direction == "" {
				params.Direction = "callers"
			}

			if searchInstruments != nil {
				searchInstruments.SearchRefGraphTotal.Add(ctx, 1)
			}
			slog.Debug("find_references", "symbol", params.Symbol, "direction", params.Direction)

			// Try exact match first, then search for matching symbols.
			symbols := []string{params.Symbol}
			matches := refGraph.SymbolMatches(params.Symbol)
			if len(matches) > 0 {
				symbols = matches
			}

			var sb strings.Builder
			for _, sym := range symbols {
				var refs []string
				switch params.Direction {
				case "callees":
					refs = refGraph.FindCallees(sym)
				default:
					refs = refGraph.FindCallers(sym)
				}

				if len(refs) > 0 {
					fmt.Fprintf(&sb, "## %s (%s)\n", sym, params.Direction)
					for _, ref := range refs {
						fmt.Fprintf(&sb, "  - %s\n", ref)
					}
					sb.WriteByte('\n')
				}

				// Also show definitions.
				defs := refGraph.FindDefinitions(sym)
				if len(defs) > 0 {
					fmt.Fprintf(&sb, "Defined at:\n")
					for _, d := range defs {
						fmt.Fprintf(&sb, "  %s:%d\n", d.File, d.Line)
					}
					sb.WriteByte('\n')
				}
			}

			if sb.Len() == 0 {
				return fmt.Sprintf("No references found for %q.", params.Symbol), nil
			}
			return sb.String(), nil
		},
	})

	// find_impact: "If I change X, what else might break?"
	r.Register(&ToolSpec{
		Name: "find_impact",
		Description: "Trace the downstream impact of changing a symbol. " +
			"Shows all symbols transitively affected by a change, up to N levels deep. " +
			"Answers: 'If I change function X, what else might break?'",
		InputSchema: json.RawMessage(`{
			"type": "object",
			"properties": {
				"symbol": {
					"type": "string",
					"description": "Symbol to analyze impact for (e.g., 'HandleRequest', 'indexer.New')"
				},
				"depth": {
					"type": "integer",
					"description": "How many levels deep to trace (default: 3, max: 5)"
				}
			},
			"required": ["symbol"]
		}`),
		AlwaysAvailable: false,
		Handler: func(ctx context.Context, input json.RawMessage) (string, error) {
			if refGraph == nil {
				return "Reference graph is not available (indexer not initialized).", nil
			}

			var params struct {
				Symbol string `json:"symbol"`
				Depth  int    `json:"depth"`
			}
			if err := json.Unmarshal(input, &params); err != nil {
				return "", fmt.Errorf("parse find_impact input: %w", err)
			}

			if params.Depth <= 0 {
				params.Depth = 3
			}
			if params.Depth > 5 {
				params.Depth = 5
			}

			if searchInstruments != nil {
				searchInstruments.SearchRefGraphTotal.Add(ctx, 1)
			}
			slog.Debug("find_impact", "symbol", params.Symbol, "depth", params.Depth)

			// Find matching symbols.
			symbols := refGraph.SymbolMatches(params.Symbol)
			if len(symbols) == 0 {
				symbols = []string{params.Symbol}
			}

			var sb strings.Builder
			for _, sym := range symbols {
				impacted := refGraph.FindImpact(sym, params.Depth)
				if len(impacted) > 0 {
					fmt.Fprintf(&sb, "## Impact of changing %s (depth %d)\n", sym, params.Depth)
					fmt.Fprintf(&sb, "Affected symbols (%d):\n", len(impacted))
					for _, imp := range impacted {
						defs := refGraph.FindDefinitions(imp)
						if len(defs) > 0 {
							fmt.Fprintf(&sb, "  - %s (%s:%d)\n", imp, defs[0].File, defs[0].Line)
						} else {
							fmt.Fprintf(&sb, "  - %s\n", imp)
						}
					}
					sb.WriteByte('\n')
				}
			}

			if sb.Len() == 0 {
				return fmt.Sprintf("No downstream impact found for %q.", params.Symbol), nil
			}
			return sb.String(), nil
		},
	})
}
