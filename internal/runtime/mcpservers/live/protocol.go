//go:build experimental

package live

import "encoding/json"

// wsRequest is the wire envelope ycode → extension. The id is
// generated server-side; the extension echoes it back so callers can
// correlate concurrent calls.
type wsRequest struct {
	ID     int64          `json:"id"`
	Method string         `json:"method"`
	Params map[string]any `json:"params,omitempty"`
}

// wsResponse is the wire envelope extension → ycode. Exactly one of
// Result and Error is non-empty.
type wsResponse struct {
	ID     int64           `json:"id"`
	Result json.RawMessage `json:"result,omitempty"`
	Error  string          `json:"error,omitempty"`
}

// extResult is the inner content the extension returns for tool
// calls. Fields mirror mcpservers.BrowserResult so we can decode
// directly.
type extResult struct {
	Title    string `json:"title,omitempty"`
	URL      string `json:"url,omitempty"`
	Content  string `json:"content,omitempty"`
	Elements string `json:"elements,omitempty"`
	Data     string `json:"data,omitempty"`
	Image    string `json:"image,omitempty"`
}
