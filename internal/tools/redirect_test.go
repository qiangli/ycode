package tools

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/qiangli/ycode/internal/runtime/permission"
)

func TestRedirect_ReadFilePDF(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping in short mode")
	}

	r := NewRegistry()
	RegisterBuiltins(r)

	// Wire a dummy read_file handler.
	spec, _ := r.Get("read_file")
	spec.Handler = func(_ context.Context, _ json.RawMessage) (string, error) {
		return "file content", nil
	}

	ApplyRedirectMiddleware(r, defaultRedirectRules())

	// Reading a PDF should be redirected.
	result, err := r.Invoke(context.Background(), "read_file", json.RawMessage(`{"file_path":"/docs/report.pdf"}`))
	if err != nil {
		t.Fatalf("redirect failed: %v", err)
	}
	if !strings.Contains(result, "read_document") {
		t.Errorf("expected redirect to read_document, got: %s", result)
	}
	if !strings.Contains(result, "PDF") {
		t.Errorf("expected PDF mention, got: %s", result)
	}

	// Reading a DOCX should be redirected.
	result, err = r.Invoke(context.Background(), "read_file", json.RawMessage(`{"file_path":"/docs/letter.docx"}`))
	if err != nil {
		t.Fatalf("redirect failed: %v", err)
	}
	if !strings.Contains(result, "read_document") {
		t.Errorf("expected redirect to read_document for DOCX, got: %s", result)
	}

	// Reading a .go file should NOT be redirected.
	result, err = r.Invoke(context.Background(), "read_file", json.RawMessage(`{"file_path":"/src/main.go"}`))
	if err != nil {
		t.Fatalf("normal read failed: %v", err)
	}
	if result != "file content" {
		t.Errorf("expected normal result for .go file, got: %s", result)
	}
}

func TestRedirect_ReadFileLarge(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping in short mode")
	}

	tmpDir := t.TempDir()

	// Create a large file (600KB).
	largePath := filepath.Join(tmpDir, "large.go")
	data := make([]byte, 600*1024)
	for i := range data {
		data[i] = 'x'
	}
	if err := os.WriteFile(largePath, data, 0o644); err != nil {
		t.Fatal(err)
	}

	r := NewRegistry()
	RegisterBuiltins(r)

	spec, _ := r.Get("read_file")
	spec.Handler = func(_ context.Context, _ json.RawMessage) (string, error) {
		return "full content", nil
	}

	ApplyRedirectMiddleware(r, defaultRedirectRules())

	// Large file without offset/limit should be redirected.
	input := json.RawMessage(`{"file_path":"` + largePath + `"}`)
	result, err := r.Invoke(context.Background(), "read_file", input)
	if err != nil {
		t.Fatalf("redirect failed: %v", err)
	}
	if !strings.Contains(result, "large") || !strings.Contains(result, "offset") {
		t.Errorf("expected large file redirect with offset suggestion, got: %s", result)
	}

	// Same file WITH offset/limit should NOT be redirected.
	input = json.RawMessage(`{"file_path":"` + largePath + `","offset":0,"limit":100}`)
	result, err = r.Invoke(context.Background(), "read_file", input)
	if err != nil {
		t.Fatalf("chunked read failed: %v", err)
	}
	if result != "full content" {
		t.Errorf("expected normal result for chunked read, got: %s", result)
	}
}

func TestRedirect_BashGrepSymbols(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping in short mode")
	}

	r := NewRegistry()
	RegisterBuiltins(r)

	spec, _ := r.Get("bash")
	spec.RequiredMode = permission.ReadOnly // Allow without prompting.
	spec.Handler = func(_ context.Context, _ json.RawMessage) (string, error) {
		return "grep output", nil
	}

	ApplyRedirectMiddleware(r, defaultRedirectRules())

	// grep for function definitions should be redirected.
	result, err := r.Invoke(context.Background(), "bash", json.RawMessage(`{"command":"grep -r 'func HandleAuth' ./internal/"}`))
	if err != nil {
		t.Fatalf("redirect failed: %v", err)
	}
	if !strings.Contains(result, "find_references") || !strings.Contains(result, "ast_search") {
		t.Errorf("expected redirect to code intelligence tools, got: %s", result)
	}

	// Regular bash command should NOT be redirected.
	result, err = r.Invoke(context.Background(), "bash", json.RawMessage(`{"command":"go build ./..."}`))
	if err != nil {
		t.Fatalf("normal bash failed: %v", err)
	}
	if result != "grep output" {
		t.Errorf("expected normal result for go build, got: %s", result)
	}
}

func TestRedirect_BashFind(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping in short mode")
	}

	r := NewRegistry()
	RegisterBuiltins(r)

	spec, _ := r.Get("bash")
	spec.RequiredMode = permission.ReadOnly
	spec.Handler = func(_ context.Context, _ json.RawMessage) (string, error) {
		return "find output", nil
	}

	ApplyRedirectMiddleware(r, defaultRedirectRules())

	// find -name should be redirected.
	result, err := r.Invoke(context.Background(), "bash", json.RawMessage(`{"command":"find . -name '*.go'"}`))
	if err != nil {
		t.Fatalf("redirect failed: %v", err)
	}
	if !strings.Contains(result, "glob_search") {
		t.Errorf("expected redirect to glob_search, got: %s", result)
	}
}

func TestRedirect_BashCat(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping in short mode")
	}

	r := NewRegistry()
	RegisterBuiltins(r)

	spec, _ := r.Get("bash")
	spec.RequiredMode = permission.ReadOnly
	spec.Handler = func(_ context.Context, _ json.RawMessage) (string, error) {
		return "cat output", nil
	}

	ApplyRedirectMiddleware(r, defaultRedirectRules())

	// cat should be redirected.
	result, err := r.Invoke(context.Background(), "bash", json.RawMessage(`{"command":"cat /etc/config.yaml"}`))
	if err != nil {
		t.Fatalf("redirect failed: %v", err)
	}
	if !strings.Contains(result, "read_file") {
		t.Errorf("expected redirect to read_file, got: %s", result)
	}

	// head should be redirected.
	result, err = r.Invoke(context.Background(), "bash", json.RawMessage(`{"command":"head -20 main.go"}`))
	if err != nil {
		t.Fatalf("redirect failed: %v", err)
	}
	if !strings.Contains(result, "read_file") {
		t.Errorf("expected redirect to read_file, got: %s", result)
	}
}

func TestRedirect_BashSed(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping in short mode")
	}

	r := NewRegistry()
	RegisterBuiltins(r)

	spec, _ := r.Get("bash")
	spec.RequiredMode = permission.ReadOnly
	spec.Handler = func(_ context.Context, _ json.RawMessage) (string, error) {
		return "sed output", nil
	}

	ApplyRedirectMiddleware(r, defaultRedirectRules())

	// sed should be redirected.
	result, err := r.Invoke(context.Background(), "bash", json.RawMessage(`{"command":"sed -i 's/old/new/g' file.txt"}`))
	if err != nil {
		t.Fatalf("redirect failed: %v", err)
	}
	if !strings.Contains(result, "edit_file") {
		t.Errorf("expected redirect to edit_file, got: %s", result)
	}
}

func TestRedirect_XLSX(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping in short mode")
	}

	r := NewRegistry()
	RegisterBuiltins(r)

	spec, _ := r.Get("read_file")
	spec.Handler = func(_ context.Context, _ json.RawMessage) (string, error) {
		return "binary content", nil
	}

	ApplyRedirectMiddleware(r, defaultRedirectRules())

	result, err := r.Invoke(context.Background(), "read_file", json.RawMessage(`{"file_path":"/data/report.xlsx"}`))
	if err != nil {
		t.Fatalf("redirect failed: %v", err)
	}
	if !strings.Contains(result, "read_document") {
		t.Errorf("expected redirect to read_document for XLSX, got: %s", result)
	}
}

func TestRedirect_Helpers(t *testing.T) {
	// Test extractStringField.
	input := json.RawMessage(`{"file_path":"/test/file.go","count":42}`)
	if v := extractStringField(input, "file_path"); v != "/test/file.go" {
		t.Errorf("expected '/test/file.go', got %q", v)
	}
	if v := extractStringField(input, "missing"); v != "" {
		t.Errorf("expected empty for missing field, got %q", v)
	}
	if v := extractStringField(json.RawMessage(`invalid`), "key"); v != "" {
		t.Errorf("expected empty for invalid JSON, got %q", v)
	}

	// Test formatBytes.
	if v := formatBytes(500); v != "500 bytes" {
		t.Errorf("expected '500 bytes', got %q", v)
	}
	if v := formatBytes(1536); !strings.Contains(v, "KB") {
		t.Errorf("expected KB, got %q", v)
	}
	if v := formatBytes(2 * 1024 * 1024); !strings.Contains(v, "MB") {
		t.Errorf("expected MB, got %q", v)
	}
}
