package treesitter

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/qiangli/ycode/internal/runtime/mcp"
)

func TestMCPHandler_ListSymbols_Go(t *testing.T) {
	t.Parallel()
	h := NewMCPHandler()

	src := `package demo

import "fmt"

type Greeter struct{ name string }

func (g Greeter) Hello() string { return "hi " + g.name }

func TopLevel() {
	fmt.Println("hello")
}
`
	in, _ := json.Marshal(map[string]string{
		"file_path": "demo.go",
		"source":    src,
	})
	out, err := h.HandleToolCall(context.Background(), "list_symbols", in)
	if err != nil {
		t.Fatalf("list_symbols: %v", err)
	}

	var symbols []Symbol
	if err := json.Unmarshal([]byte(out), &symbols); err != nil {
		t.Fatalf("unmarshal: %v\n%s", err, out)
	}
	if len(symbols) == 0 {
		t.Fatalf("expected at least one symbol, got empty list")
	}

	names := map[string]bool{}
	for _, s := range symbols {
		names[s.Name] = true
	}
	for _, want := range []string{"Greeter", "Hello", "TopLevel"} {
		if !names[want] {
			t.Errorf("expected symbol %q in result, got: %v", want, symbols)
		}
	}
}

func TestMCPHandler_ListSymbols_AutoDetectFromExt(t *testing.T) {
	t.Parallel()
	h := NewMCPHandler()

	// Write a Python file to a temp path so language is detected from .py.
	dir := t.TempDir()
	path := filepath.Join(dir, "demo.py")
	if err := os.WriteFile(path, []byte("def hello():\n    return 1\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	in, _ := json.Marshal(map[string]string{"file_path": path})
	out, err := h.HandleToolCall(context.Background(), "list_symbols", in)
	if err != nil {
		t.Fatalf("list_symbols: %v", err)
	}
	if !strings.Contains(out, `"hello"`) {
		t.Fatalf("expected hello in output, got: %s", out)
	}
}

func TestMCPHandler_ListSymbols_UnknownExtension(t *testing.T) {
	t.Parallel()
	h := NewMCPHandler()
	in, _ := json.Marshal(map[string]string{
		"file_path": "demo.unknownext",
		"source":    "anything",
	})
	if _, err := h.HandleToolCall(context.Background(), "list_symbols", in); err == nil {
		t.Fatalf("expected error for unknown extension")
	}
}

func TestMCPHandler_GetSupportedLanguages(t *testing.T) {
	t.Parallel()
	h := NewMCPHandler()

	out, err := h.HandleToolCall(context.Background(), "get_supported_languages", json.RawMessage(`{}`))
	if err != nil {
		t.Fatalf("get_supported_languages: %v", err)
	}

	var langs []string
	if err := json.Unmarshal([]byte(out), &langs); err != nil {
		t.Fatalf("unmarshal: %v\n%s", err, out)
	}
	for _, want := range []string{"go", "python", "rust"} {
		if !slices.Contains(langs, want) {
			t.Errorf("expected %q in supported languages, got: %v", want, langs)
		}
	}
}

func TestMCPHandler_PermissionAware_AllReadOnly(t *testing.T) {
	t.Parallel()
	h := NewMCPHandler()
	for _, tool := range h.ListTools() {
		if got := h.RequiredMode(tool.Name); got != mcp.ModeReadOnly {
			t.Errorf("tool %q: expected ReadOnly, got %s", tool.Name, got)
		}
	}
}

func TestMCPHandler_UnknownTool(t *testing.T) {
	t.Parallel()
	h := NewMCPHandler()
	if _, err := h.HandleToolCall(context.Background(), "nope", json.RawMessage(`{}`)); err == nil {
		t.Fatalf("expected error for unknown tool")
	}
}
