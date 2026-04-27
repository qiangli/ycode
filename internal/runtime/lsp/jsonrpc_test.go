package lsp

import (
	"bufio"
	"encoding/json"
	"strings"
	"testing"
)

func TestReadHeader(t *testing.T) {
	tests := []struct {
		name    string
		input   string
		want    int
		wantErr bool
	}{
		{
			name:  "standard header",
			input: "Content-Length: 42\r\n\r\n",
			want:  42,
		},
		{
			name:  "extra headers",
			input: "Content-Type: application/json\r\nContent-Length: 100\r\n\r\n",
			want:  100,
		},
		{
			name:    "missing content-length",
			input:   "Content-Type: application/json\r\n\r\n",
			wantErr: true,
		},
		{
			name:    "empty input",
			input:   "",
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			c := &conn{
				stdout: bufio.NewReader(strings.NewReader(tt.input)),
			}
			got, err := c.readHeader()
			if tt.wantErr {
				if err == nil {
					t.Error("expected error")
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if got != tt.want {
				t.Errorf("got %d, want %d", got, tt.want)
			}
		})
	}
}

func TestFileURI_Variations(t *testing.T) {
	tests := []struct {
		path string
		want string
	}{
		{"/tmp/test.go", "file:///tmp/test.go"},
		{"/home/user/project/main.go", "file:///home/user/project/main.go"},
	}
	for _, tt := range tests {
		got := fileURI(tt.path)
		if got != tt.want {
			t.Errorf("fileURI(%q) = %q, want %q", tt.path, got, tt.want)
		}
	}
}

func TestURIToPath_Variations(t *testing.T) {
	tests := []struct {
		uri  string
		want string
	}{
		{"file:///tmp/test.go", "/tmp/test.go"},
		{"file:///home/user/file.py", "/home/user/file.py"},
		{"/plain/path", "/plain/path"},
		{"relative/path", "relative/path"},
	}
	for _, tt := range tests {
		got := uriToPath(tt.uri)
		if got != tt.want {
			t.Errorf("uriToPath(%q) = %q, want %q", tt.uri, got, tt.want)
		}
	}
}

func TestParseLocations(t *testing.T) {
	t.Run("null result", func(t *testing.T) {
		locs, err := parseLocations(nil)
		if err != nil || locs != nil {
			t.Errorf("expected nil, nil for null result")
		}
	})

	t.Run("single location", func(t *testing.T) {
		data := json.RawMessage(`{
			"uri": "file:///tmp/test.go",
			"range": {
				"start": {"line": 10, "character": 5},
				"end": {"line": 10, "character": 15}
			}
		}`)
		locs, err := parseLocations(data)
		if err != nil {
			t.Fatal(err)
		}
		if len(locs) != 1 {
			t.Fatalf("expected 1 location, got %d", len(locs))
		}
		if locs[0].StartLine != 10 || locs[0].StartCol != 5 {
			t.Errorf("unexpected location: %+v", locs[0])
		}
	})

	t.Run("array of locations", func(t *testing.T) {
		data := json.RawMessage(`[
			{"uri": "file:///a.go", "range": {"start": {"line": 1, "character": 0}, "end": {"line": 1, "character": 5}}},
			{"uri": "file:///b.go", "range": {"start": {"line": 20, "character": 3}, "end": {"line": 20, "character": 10}}}
		]`)
		locs, err := parseLocations(data)
		if err != nil {
			t.Fatal(err)
		}
		if len(locs) != 2 {
			t.Fatalf("expected 2 locations, got %d", len(locs))
		}
	})

	t.Run("null json", func(t *testing.T) {
		data := json.RawMessage(`null`)
		locs, err := parseLocations(data)
		if err != nil || locs != nil {
			t.Errorf("expected nil for null json")
		}
	})
}

func TestParseSymbol_DocumentSymbol(t *testing.T) {
	data := json.RawMessage(`{
		"name": "Config",
		"kind": 23,
		"range": {"start": {"line": 5, "character": 0}, "end": {"line": 15, "character": 1}},
		"selectionRange": {"start": {"line": 5, "character": 5}, "end": {"line": 5, "character": 11}},
		"children": [
			{
				"name": "Name",
				"kind": 8,
				"range": {"start": {"line": 6, "character": 1}, "end": {"line": 6, "character": 12}},
				"selectionRange": {"start": {"line": 6, "character": 1}, "end": {"line": 6, "character": 5}}
			}
		]
	}`)

	symbols := parseSymbol(data, "config.go")
	if len(symbols) != 2 {
		t.Fatalf("expected 2 symbols (parent + child), got %d", len(symbols))
	}
	if symbols[0].Name != "Config" || symbols[0].Kind != "Struct" {
		t.Errorf("unexpected parent: %+v", symbols[0])
	}
	if symbols[1].Name != "Name" || symbols[1].Kind != "Field" {
		t.Errorf("unexpected child: %+v", symbols[1])
	}
}

func TestParseSymbol_SymbolInformation(t *testing.T) {
	data := json.RawMessage(`{
		"name": "main",
		"kind": 12,
		"location": {
			"uri": "file:///tmp/main.go",
			"range": {"start": {"line": 3, "character": 5}, "end": {"line": 3, "character": 9}}
		}
	}`)

	symbols := parseSymbol(data, "main.go")
	if len(symbols) != 1 {
		t.Fatalf("expected 1 symbol, got %d", len(symbols))
	}
	if symbols[0].Name != "main" || symbols[0].Kind != "Function" {
		t.Errorf("unexpected symbol: %+v", symbols[0])
	}
}

func TestSymbolKindName_AllKnown(t *testing.T) {
	known := map[int]string{
		1: "File", 5: "Class", 6: "Method", 8: "Field",
		11: "Interface", 12: "Function", 13: "Variable",
		14: "Constant", 23: "Struct",
	}
	for kind, expected := range known {
		got := symbolKindName(kind)
		if got != expected {
			t.Errorf("symbolKindName(%d) = %q, want %q", kind, got, expected)
		}
	}
}

func TestJSONRPCRequest_Marshal(t *testing.T) {
	req := jsonRPCRequest{
		JSONRPC: "2.0",
		ID:      1,
		Method:  "textDocument/definition",
		Params:  map[string]any{"key": "value"},
	}
	data, err := json.Marshal(req)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(data), `"jsonrpc":"2.0"`) {
		t.Errorf("missing jsonrpc field in: %s", data)
	}
	if !strings.Contains(string(data), `"method":"textDocument/definition"`) {
		t.Errorf("missing method field in: %s", data)
	}
}

func TestJSONRPCResponse_Unmarshal(t *testing.T) {
	t.Run("success response", func(t *testing.T) {
		data := `{"jsonrpc":"2.0","id":1,"result":{"uri":"file:///tmp/test.go"}}`
		var resp jsonRPCResponse
		if err := json.Unmarshal([]byte(data), &resp); err != nil {
			t.Fatal(err)
		}
		if resp.ID != 1 || resp.Error != nil {
			t.Errorf("unexpected response: %+v", resp)
		}
	})

	t.Run("error response", func(t *testing.T) {
		data := `{"jsonrpc":"2.0","id":2,"error":{"code":-32600,"message":"Invalid Request"}}`
		var resp jsonRPCResponse
		if err := json.Unmarshal([]byte(data), &resp); err != nil {
			t.Fatal(err)
		}
		if resp.Error == nil {
			t.Fatal("expected error in response")
		}
		if resp.Error.Code != -32600 {
			t.Errorf("expected error code -32600, got %d", resp.Error.Code)
		}
	})
}
