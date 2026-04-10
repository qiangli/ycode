package lsp

// Action identifies the LSP operation to perform.
type Action string

const (
	ActionHover       Action = "hover"
	ActionDefinition  Action = "definition"
	ActionReferences  Action = "references"
	ActionSymbols     Action = "symbols"
	ActionDiagnostics Action = "diagnostics"
)

// Location represents a source code location.
type Location struct {
	URI       string `json:"uri"`
	StartLine int    `json:"start_line"`
	StartCol  int    `json:"start_col"`
	EndLine   int    `json:"end_line"`
	EndCol    int    `json:"end_col"`
}

// Diagnostic is a source code diagnostic.
type Diagnostic struct {
	Location Location `json:"location"`
	Severity string   `json:"severity"` // error, warning, info, hint
	Message  string   `json:"message"`
	Source   string   `json:"source,omitempty"`
}

// Symbol represents a code symbol.
type Symbol struct {
	Name     string   `json:"name"`
	Kind     string   `json:"kind"`
	Location Location `json:"location"`
}

// HoverResult is the result of a hover operation.
type HoverResult struct {
	Contents string   `json:"contents"`
	Location Location `json:"location"`
}
