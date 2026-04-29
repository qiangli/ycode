// Package treesitter provides in-process AST parsing and structural
// code search using tree-sitter grammars. It supports multiple languages
// and provides pattern-based code matching.
//
// When built with CGO_ENABLED=1 (the default on native platforms),
// tree-sitter grammars are linked in-process for fast, accurate parsing.
// When built with CGO_ENABLED=0 (cross-compilation), all functions
// return ErrNoCGO and callers should fall back to containerized ast-grep.
package treesitter

import "errors"

// ErrNoCGO is returned by all tree-sitter functions when built without CGO.
// Callers should fall back to containerized ast-grep or other search methods.
var ErrNoCGO = errors.New("tree-sitter requires CGO (build with CGO_ENABLED=1)")

// Match represents a structural code match.
type Match struct {
	File        string            `json:"file"`
	StartLine   int               `json:"start_line"`
	EndLine     int               `json:"end_line"`
	StartCol    int               `json:"start_col"`
	EndCol      int               `json:"end_col"`
	MatchedCode string            `json:"matched_code"`
	Captures    map[string]string `json:"captures,omitempty"`
}

// Symbol represents a top-level code symbol extracted from an AST.
type Symbol struct {
	Name      string `json:"name"`
	Kind      string `json:"kind"`      // func, type, interface, method, class, const, var
	Signature string `json:"signature"` // human-readable signature
	File      string `json:"file"`      // relative path
	Line      int    `json:"line"`
	Exported  bool   `json:"exported"`
}

// Impact represents a dependency relationship found during impact analysis.
type Impact struct {
	Symbol   string `json:"symbol"`   // the affected symbol name
	File     string `json:"file"`     // file containing the affected symbol
	Line     int    `json:"line"`     // line number
	Kind     string `json:"kind"`     // "calls", "called_by", "references"
	Distance int    `json:"distance"` // hops from the target symbol
	Context  string `json:"context"`  // surrounding code context
}
