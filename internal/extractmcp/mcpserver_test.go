package extractmcp

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/qiangli/ycode/internal/api"
	"github.com/qiangli/ycode/internal/runtime/mcp"
)

func TestDocumentHandlerImplements(t *testing.T) {
	var _ mcp.ServerHandler = (*DocumentHandler)(nil)
	var _ mcp.PermissionAware = (*DocumentHandler)(nil)
}

func TestDocumentHandlerListsOneTool(t *testing.T) {
	tools := NewDocumentHandler().ListTools()
	if len(tools) != 1 || tools[0].Name != "extract_document" {
		t.Fatalf("expected exactly one tool 'extract_document', got %+v", tools)
	}
}

func TestDocumentHandlerRequiresFilePath(t *testing.T) {
	_, err := NewDocumentHandler().HandleToolCall(context.Background(), "extract_document",
		json.RawMessage(`{}`))
	if err == nil || !strings.Contains(err.Error(), "file_path") {
		t.Fatalf("expected file_path error, got: %v", err)
	}
}

func TestDocumentHandlerCSVRoundTrip(t *testing.T) {
	dir := t.TempDir()
	csv := filepath.Join(dir, "sample.csv")
	body := "a,b,c\n1,2,3\n4,5,6\n"
	if err := os.WriteFile(csv, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
	in := json.RawMessage(`{"file_path": "` + csv + `"}`)
	out, err := NewDocumentHandler().HandleToolCall(context.Background(), "extract_document", in)
	if err != nil {
		t.Fatalf("extract_document: %v", err)
	}
	if !strings.Contains(out, "a") || !strings.Contains(out, "5") {
		t.Errorf("output missing expected CSV content: %q", out)
	}
}

func TestDocumentHandlerRejectsUnsupportedExt(t *testing.T) {
	dir := t.TempDir()
	p := filepath.Join(dir, "x.unknown")
	if err := os.WriteFile(p, []byte("hi"), 0o644); err != nil {
		t.Fatal(err)
	}
	_, err := NewDocumentHandler().HandleToolCall(context.Background(), "extract_document",
		json.RawMessage(`{"file_path": "`+p+`"}`))
	if err == nil || !strings.Contains(err.Error(), "unsupported") {
		t.Fatalf("expected unsupported error, got: %v", err)
	}
}

// TestJSONHandlerNilProviderYieldsNilHandler is safeguard for the
// registration pattern in serve.go: a nil provider must produce a nil
// handler, never a handler that panics on call.
func TestJSONHandlerNilProviderYieldsNilHandler(t *testing.T) {
	if h := NewJSONHandler(nil, "", 0); h != nil {
		t.Fatalf("nil provider should yield nil handler, got %+v", h)
	}
}

func TestJSONHandlerListsOneTool(t *testing.T) {
	// Use a stub provider to construct the handler — we're not invoking
	// it, just inspecting the advertised tool.
	h := &JSONHandler{provider: stubProvider{}}
	tools := h.ListTools()
	if len(tools) != 1 || tools[0].Name != "extract_json" {
		t.Fatalf("expected exactly one tool 'extract_json', got %+v", tools)
	}
}

func TestJSONHandlerRequiresPrompt(t *testing.T) {
	h := &JSONHandler{provider: stubProvider{}}
	_, err := h.HandleToolCall(context.Background(), "extract_json", json.RawMessage(`{}`))
	if err == nil || !strings.Contains(err.Error(), "prompt") {
		t.Fatalf("expected prompt error, got: %v", err)
	}
}

func TestRequiredModeReadOnly(t *testing.T) {
	if got := NewDocumentHandler().RequiredMode("extract_document"); got != mcp.ModeReadOnly {
		t.Errorf("DocumentHandler tier = %v, want ReadOnly", got)
	}
	h := &JSONHandler{provider: stubProvider{}}
	if got := h.RequiredMode("extract_json"); got != mcp.ModeReadOnly {
		t.Errorf("JSONHandler tier = %v, want ReadOnly", got)
	}
}

// stubProvider satisfies api.Provider for the tool-list / arg-validation
// tests. Send is never invoked — the tests fail before reaching it.
type stubProvider struct{}

func (stubProvider) Send(_ context.Context, _ *api.Request) (<-chan *api.StreamEvent, <-chan error) {
	panic("stub: Send not expected in unit tests")
}
func (stubProvider) Kind() api.ProviderKind { return "" }
