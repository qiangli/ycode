package lsp

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"os/exec"
	"sync"
	"time"
)

// ServerConfig describes an LSP server configuration.
type ServerConfig struct {
	Language string   `json:"language"`
	Command  string   `json:"command"`
	Args     []string `json:"args,omitempty"`
}

// Client wraps an LSP server connection.
type Client struct {
	config  ServerConfig
	rootDir string

	mu   sync.Mutex
	conn *conn
}

// NewClient creates a new LSP client.
func NewClient(config ServerConfig) *Client {
	return &Client{config: config}
}

// ClientRegistry manages LSP clients for different languages.
type ClientRegistry struct {
	mu      sync.RWMutex
	clients map[string]*Client
}

// NewClientRegistry creates a new LSP client registry.
func NewClientRegistry() *ClientRegistry {
	return &ClientRegistry{
		clients: make(map[string]*Client),
	}
}

// Register adds an LSP client for a language.
func (r *ClientRegistry) Register(language string, client *Client) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.clients[language] = client
}

// Get returns the LSP client for a language.
func (r *ClientRegistry) Get(language string) (*Client, bool) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	c, ok := r.clients[language]
	return c, ok
}

// Languages returns all registered language names.
func (r *ClientRegistry) Languages() []string {
	r.mu.RLock()
	defer r.mu.RUnlock()
	langs := make([]string, 0, len(r.clients))
	for lang := range r.clients {
		langs = append(langs, lang)
	}
	return langs
}

// Close shuts down all registered LSP servers.
func (r *ClientRegistry) Close() {
	r.mu.Lock()
	defer r.mu.Unlock()
	for _, client := range r.clients {
		client.Close()
	}
}

// Close shuts down the LSP server connection.
func (c *Client) Close() {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.conn != nil {
		// Send shutdown request.
		ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
		defer cancel()
		c.conn.call(ctx, "shutdown", nil)
		c.conn.notify("exit", nil)
		c.conn.close()
		c.conn = nil
	}
}

// SetRootDir sets the workspace root for the LSP server.
func (c *Client) SetRootDir(dir string) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.rootDir = dir
}

// ensureConnected starts the LSP server if not already running.
func (c *Client) ensureConnected(ctx context.Context) (*conn, error) {
	c.mu.Lock()
	defer c.mu.Unlock()

	if c.conn != nil {
		return c.conn, nil
	}

	// Verify the command exists.
	if _, err := exec.LookPath(c.config.Command); err != nil {
		return nil, fmt.Errorf("%s not found in PATH: %w", c.config.Command, err)
	}

	slog.Info("starting LSP server", "language", c.config.Language, "command", c.config.Command)

	conn, err := startServer(ctx, c.config, c.rootDir)
	if err != nil {
		return nil, err
	}

	// Send initialize request.
	initParams := map[string]any{
		"processId": nil,
		"rootUri":   fileURI(c.rootDir),
		"capabilities": map[string]any{
			"textDocument": map[string]any{
				"definition":    map[string]any{},
				"references":    map[string]any{},
				"documentSymbol": map[string]any{},
				"hover":         map[string]any{},
				"publishDiagnostics": map[string]any{},
			},
		},
	}

	initCtx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()

	if _, err := conn.call(initCtx, "initialize", initParams); err != nil {
		conn.close()
		return nil, fmt.Errorf("initialize: %w", err)
	}

	// Send initialized notification.
	if err := conn.notify("initialized", map[string]any{}); err != nil {
		conn.close()
		return nil, fmt.Errorf("initialized notification: %w", err)
	}

	c.conn = conn
	return conn, nil
}

// textDocumentPosition builds the standard LSP text document position params.
func textDocumentPosition(file string, line, col int) map[string]any {
	return map[string]any{
		"textDocument": map[string]any{
			"uri": fileURI(file),
		},
		"position": map[string]any{
			"line":      line, // 0-indexed
			"character": col,  // 0-indexed
		},
	}
}

// Hover performs a hover operation.
func (c *Client) Hover(file string, line, col int) (*HoverResult, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	conn, err := c.ensureConnected(ctx)
	if err != nil {
		return nil, err
	}

	result, err := conn.call(ctx, "textDocument/hover", textDocumentPosition(file, line, col))
	if err != nil {
		return nil, err
	}

	if result == nil || string(result) == "null" {
		return nil, nil
	}

	var hover struct {
		Contents any `json:"contents"`
		Range    *struct {
			Start struct{ Line, Character int } `json:"start"`
			End   struct{ Line, Character int } `json:"end"`
		} `json:"range"`
	}
	if err := json.Unmarshal(result, &hover); err != nil {
		return nil, fmt.Errorf("parse hover: %w", err)
	}

	hr := &HoverResult{}

	// Contents can be string, MarkupContent, or MarkedString.
	switch v := hover.Contents.(type) {
	case string:
		hr.Contents = v
	case map[string]any:
		if val, ok := v["value"]; ok {
			hr.Contents = fmt.Sprintf("%v", val)
		}
	}

	if hover.Range != nil {
		hr.Location = Location{
			URI:       fileURI(file),
			StartLine: hover.Range.Start.Line,
			StartCol:  hover.Range.Start.Character,
			EndLine:   hover.Range.End.Line,
			EndCol:    hover.Range.End.Character,
		}
	}

	return hr, nil
}

// Definition finds the definition of a symbol.
func (c *Client) Definition(file string, line, col int) ([]Location, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	conn, err := c.ensureConnected(ctx)
	if err != nil {
		return nil, err
	}

	result, err := conn.call(ctx, "textDocument/definition", textDocumentPosition(file, line, col))
	if err != nil {
		return nil, err
	}

	return parseLocations(result)
}

// References finds all references to a symbol.
func (c *Client) References(file string, line, col int) ([]Location, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	conn, err := c.ensureConnected(ctx)
	if err != nil {
		return nil, err
	}

	params := textDocumentPosition(file, line, col)
	params["context"] = map[string]any{
		"includeDeclaration": true,
	}

	result, err := conn.call(ctx, "textDocument/references", params)
	if err != nil {
		return nil, err
	}

	return parseLocations(result)
}

// Symbols lists symbols in a file.
func (c *Client) Symbols(file string) ([]Symbol, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	conn, err := c.ensureConnected(ctx)
	if err != nil {
		return nil, err
	}

	params := map[string]any{
		"textDocument": map[string]any{
			"uri": fileURI(file),
		},
	}

	result, err := conn.call(ctx, "textDocument/documentSymbol", params)
	if err != nil {
		return nil, err
	}

	if result == nil || string(result) == "null" {
		return nil, nil
	}

	// Parse DocumentSymbol[] or SymbolInformation[].
	var rawSymbols []json.RawMessage
	if err := json.Unmarshal(result, &rawSymbols); err != nil {
		return nil, fmt.Errorf("parse symbols: %w", err)
	}

	var symbols []Symbol
	for _, raw := range rawSymbols {
		symbols = append(symbols, parseSymbol(raw, file)...)
	}

	return symbols, nil
}

// Diagnostics returns diagnostics for a file.
func (c *Client) Diagnostics(file string) ([]Diagnostic, error) {
	// Diagnostics are pushed via notifications, not pulled.
	// For now, return empty — a full implementation would track
	// publishDiagnostics notifications from the server.
	return nil, nil
}

// parseLocations parses LSP Location or Location[] responses.
func parseLocations(result json.RawMessage) ([]Location, error) {
	if result == nil || string(result) == "null" {
		return nil, nil
	}

	// Try as a single location first.
	var single struct {
		URI   string `json:"uri"`
		Range struct {
			Start struct{ Line, Character int } `json:"start"`
			End   struct{ Line, Character int } `json:"end"`
		} `json:"range"`
	}

	// Try as array.
	var arr []json.RawMessage
	if err := json.Unmarshal(result, &arr); err == nil {
		var locs []Location
		for _, raw := range arr {
			if err := json.Unmarshal(raw, &single); err == nil {
				locs = append(locs, Location{
					URI:       uriToPath(single.URI),
					StartLine: single.Range.Start.Line,
					StartCol:  single.Range.Start.Character,
					EndLine:   single.Range.End.Line,
					EndCol:    single.Range.End.Character,
				})
			}
		}
		return locs, nil
	}

	// Try as single.
	if err := json.Unmarshal(result, &single); err == nil {
		return []Location{{
			URI:       uriToPath(single.URI),
			StartLine: single.Range.Start.Line,
			StartCol:  single.Range.Start.Character,
			EndLine:   single.Range.End.Line,
			EndCol:    single.Range.End.Character,
		}}, nil
	}

	return nil, nil
}

// parseSymbol recursively parses DocumentSymbol or SymbolInformation.
func parseSymbol(raw json.RawMessage, file string) []Symbol {
	// Try DocumentSymbol (has children).
	var docSym struct {
		Name           string           `json:"name"`
		Kind           int              `json:"kind"`
		Range          json.RawMessage  `json:"range"`
		SelectionRange json.RawMessage  `json:"selectionRange"`
		Children       []json.RawMessage `json:"children"`
	}
	if err := json.Unmarshal(raw, &docSym); err == nil && docSym.Name != "" {
		loc := parseRange(docSym.SelectionRange, file)
		symbols := []Symbol{{
			Name:     docSym.Name,
			Kind:     symbolKindName(docSym.Kind),
			Location: loc,
		}}
		for _, child := range docSym.Children {
			symbols = append(symbols, parseSymbol(child, file)...)
		}
		return symbols
	}

	// Try SymbolInformation.
	var symInfo struct {
		Name     string `json:"name"`
		Kind     int    `json:"kind"`
		Location struct {
			URI   string          `json:"uri"`
			Range json.RawMessage `json:"range"`
		} `json:"location"`
	}
	if err := json.Unmarshal(raw, &symInfo); err == nil && symInfo.Name != "" {
		loc := parseRange(symInfo.Location.Range, uriToPath(symInfo.Location.URI))
		return []Symbol{{
			Name:     symInfo.Name,
			Kind:     symbolKindName(symInfo.Kind),
			Location: loc,
		}}
	}

	return nil
}

func parseRange(raw json.RawMessage, file string) Location {
	var r struct {
		Start struct{ Line, Character int } `json:"start"`
		End   struct{ Line, Character int } `json:"end"`
	}
	if err := json.Unmarshal(raw, &r); err == nil {
		return Location{
			URI:       file,
			StartLine: r.Start.Line,
			StartCol:  r.Start.Character,
			EndLine:   r.End.Line,
			EndCol:    r.End.Character,
		}
	}
	return Location{URI: file}
}

// symbolKindName maps LSP SymbolKind numbers to human-readable names.
func symbolKindName(kind int) string {
	names := map[int]string{
		1: "File", 2: "Module", 3: "Namespace", 4: "Package",
		5: "Class", 6: "Method", 7: "Property", 8: "Field",
		9: "Constructor", 10: "Enum", 11: "Interface", 12: "Function",
		13: "Variable", 14: "Constant", 15: "String", 16: "Number",
		17: "Boolean", 18: "Array", 19: "Object", 20: "Key",
		21: "Null", 22: "EnumMember", 23: "Struct", 24: "Event",
		25: "Operator", 26: "TypeParameter",
	}
	if name, ok := names[kind]; ok {
		return name
	}
	return fmt.Sprintf("kind(%d)", kind)
}

// AutoDetectServers returns server configs for language servers found in PATH.
func AutoDetectServers() []ServerConfig {
	var configs []ServerConfig

	// Go: gopls
	if _, err := exec.LookPath("gopls"); err == nil {
		configs = append(configs, ServerConfig{
			Language: "go",
			Command:  "gopls",
			Args:     []string{"serve"},
		})
	}

	// Python: pylsp or pyright
	if _, err := exec.LookPath("pylsp"); err == nil {
		configs = append(configs, ServerConfig{
			Language: "python",
			Command:  "pylsp",
		})
	} else if _, err := exec.LookPath("pyright-langserver"); err == nil {
		configs = append(configs, ServerConfig{
			Language: "python",
			Command:  "pyright-langserver",
			Args:     []string{"--stdio"},
		})
	}

	// TypeScript/JavaScript: typescript-language-server
	if _, err := exec.LookPath("typescript-language-server"); err == nil {
		configs = append(configs, ServerConfig{
			Language: "typescript",
			Command:  "typescript-language-server",
			Args:     []string{"--stdio"},
		})
	}

	return configs
}
