package lsp

import (
	"testing"
)

func TestClientRegistry(t *testing.T) {
	reg := NewClientRegistry()

	client := NewClient(ServerConfig{Language: "go", Command: "gopls"})
	reg.Register("go", client)

	got, ok := reg.Get("go")
	if !ok || got != client {
		t.Error("expected to get registered client")
	}

	_, ok = reg.Get("python")
	if ok {
		t.Error("expected no client for unregistered language")
	}

	langs := reg.Languages()
	if len(langs) != 1 || langs[0] != "go" {
		t.Errorf("unexpected languages: %v", langs)
	}
}

func TestAutoDetectServers(t *testing.T) {
	// Just verify it doesn't panic — detection depends on system state.
	configs := AutoDetectServers()
	for _, cfg := range configs {
		if cfg.Language == "" || cfg.Command == "" {
			t.Errorf("invalid auto-detected config: %+v", cfg)
		}
	}
}

func TestFileURI(t *testing.T) {
	uri := fileURI("/tmp/test.go")
	if uri != "file:///tmp/test.go" {
		t.Errorf("unexpected URI: %s", uri)
	}
}

func TestURIToPath(t *testing.T) {
	path := uriToPath("file:///tmp/test.go")
	if path != "/tmp/test.go" {
		t.Errorf("unexpected path: %s", path)
	}

	// Non-file URI should pass through.
	plain := uriToPath("/tmp/test.go")
	if plain != "/tmp/test.go" {
		t.Errorf("unexpected plain path: %s", plain)
	}
}

func TestSymbolKindName(t *testing.T) {
	if name := symbolKindName(12); name != "Function" {
		t.Errorf("expected Function, got %s", name)
	}
	if name := symbolKindName(999); name != "kind(999)" {
		t.Errorf("expected kind(999), got %s", name)
	}
}

func TestFormatResponse(t *testing.T) {
	// Definition with results.
	resp := &Response{
		Action: ActionDefinition,
		Locations: []Location{
			{URI: "/tmp/test.go", StartLine: 10, StartCol: 5},
		},
	}
	out := FormatResponse(resp)
	if out == "" {
		t.Error("expected non-empty formatted output")
	}

	// Empty symbols.
	resp2 := &Response{Action: ActionSymbols}
	out2 := FormatResponse(resp2)
	if out2 == "" {
		t.Error("expected non-empty formatted output for empty symbols")
	}
}
