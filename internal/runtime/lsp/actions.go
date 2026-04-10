package lsp

import (
	"fmt"
	"strings"
)

// Request represents an LSP action request.
type Request struct {
	Action   Action `json:"action"`
	FilePath string `json:"file_path"`
	Line     int    `json:"line,omitempty"`
	Col      int    `json:"col,omitempty"`
	Language string `json:"language,omitempty"`
}

// Response is a unified LSP response.
type Response struct {
	Action      Action       `json:"action"`
	Hover       *HoverResult `json:"hover,omitempty"`
	Locations   []Location   `json:"locations,omitempty"`
	Symbols     []Symbol     `json:"symbols,omitempty"`
	Diagnostics []Diagnostic `json:"diagnostics,omitempty"`
}

// Execute performs an LSP action on the given client.
func Execute(client *Client, req *Request) (*Response, error) {
	resp := &Response{Action: req.Action}

	switch req.Action {
	case ActionHover:
		result, err := client.Hover(req.FilePath, req.Line, req.Col)
		if err != nil {
			return nil, err
		}
		resp.Hover = result

	case ActionDefinition:
		locs, err := client.Definition(req.FilePath, req.Line, req.Col)
		if err != nil {
			return nil, err
		}
		resp.Locations = locs

	case ActionReferences:
		locs, err := client.References(req.FilePath, req.Line, req.Col)
		if err != nil {
			return nil, err
		}
		resp.Locations = locs

	case ActionSymbols:
		syms, err := client.Symbols(req.FilePath)
		if err != nil {
			return nil, err
		}
		resp.Symbols = syms

	case ActionDiagnostics:
		diags, err := client.Diagnostics(req.FilePath)
		if err != nil {
			return nil, err
		}
		resp.Diagnostics = diags

	default:
		return nil, fmt.Errorf("unknown LSP action: %s", req.Action)
	}

	return resp, nil
}

// FormatResponse formats an LSP response for display.
func FormatResponse(resp *Response) string {
	var b strings.Builder

	switch resp.Action {
	case ActionHover:
		if resp.Hover != nil {
			fmt.Fprintf(&b, "Hover result:\n%s\n", resp.Hover.Contents)
		} else {
			b.WriteString("No hover information available.")
		}

	case ActionDefinition, ActionReferences:
		if len(resp.Locations) == 0 {
			fmt.Fprintf(&b, "No %s found.", resp.Action)
		} else {
			fmt.Fprintf(&b, "%s (%d results):\n", resp.Action, len(resp.Locations))
			for _, loc := range resp.Locations {
				fmt.Fprintf(&b, "  %s:%d:%d\n", loc.URI, loc.StartLine, loc.StartCol)
			}
		}

	case ActionSymbols:
		if len(resp.Symbols) == 0 {
			b.WriteString("No symbols found.")
		} else {
			fmt.Fprintf(&b, "Symbols (%d):\n", len(resp.Symbols))
			for _, sym := range resp.Symbols {
				fmt.Fprintf(&b, "  %s %s (%s:%d)\n", sym.Kind, sym.Name, sym.Location.URI, sym.Location.StartLine)
			}
		}

	case ActionDiagnostics:
		if len(resp.Diagnostics) == 0 {
			b.WriteString("No diagnostics.")
		} else {
			fmt.Fprintf(&b, "Diagnostics (%d):\n", len(resp.Diagnostics))
			for _, d := range resp.Diagnostics {
				fmt.Fprintf(&b, "  [%s] %s:%d: %s\n", d.Severity, d.Location.URI, d.Location.StartLine, d.Message)
			}
		}
	}

	return b.String()
}
