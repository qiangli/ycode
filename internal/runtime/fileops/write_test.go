package fileops

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestWriteFile_PreservesCRLF(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "test.txt")

	// Create file with CRLF line endings.
	original := "line1\r\nline2\r\nline3\r\n"
	if err := os.WriteFile(path, []byte(original), 0o644); err != nil {
		t.Fatal(err)
	}

	// Write new content with LF (as LLM would produce).
	err := WriteFile(WriteFileParams{
		Path:    path,
		Content: "hello\nworld\n",
	}, "")
	if err != nil {
		t.Fatal(err)
	}

	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}

	// Should be normalized to CRLF.
	if !strings.Contains(string(data), "\r\n") {
		t.Errorf("expected CRLF line endings, got: %q", data)
	}
	if strings.Contains(string(data), "\n") && !strings.Contains(string(data), "\r\n") {
		t.Errorf("expected CRLF but found bare LF")
	}
}

func TestWriteFile_PreservesBOM(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "bom.txt")

	// Create file with UTF-8 BOM.
	bom := "\xEF\xBB\xBF"
	original := bom + "hello world\n"
	if err := os.WriteFile(path, []byte(original), 0o644); err != nil {
		t.Fatal(err)
	}

	// Write new content without BOM.
	err := WriteFile(WriteFileParams{
		Path:    path,
		Content: "new content\n",
	}, "")
	if err != nil {
		t.Fatal(err)
	}

	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}

	// Should preserve BOM.
	if len(data) < 3 || data[0] != 0xEF || data[1] != 0xBB || data[2] != 0xBF {
		t.Errorf("expected UTF-8 BOM preserved, got first bytes: %x", data[:min(3, len(data))])
	}
}

func TestWriteFile_NewFile_NoTransformation(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "new.txt")

	content := "line1\nline2\n"
	err := WriteFile(WriteFileParams{
		Path:    path,
		Content: content,
	}, "")
	if err != nil {
		t.Fatal(err)
	}

	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}

	// New file should be written as-is.
	if string(data) != content {
		t.Errorf("new file content mismatch: got %q, want %q", data, content)
	}
}

func TestWriteFile_OutsideWorkspace(t *testing.T) {
	dir := t.TempDir()
	workspace := filepath.Join(dir, "workspace")
	if err := os.MkdirAll(workspace, 0o755); err != nil {
		t.Fatal(err)
	}

	err := WriteFile(WriteFileParams{
		Path:    filepath.Join(dir, "outside.txt"),
		Content: "nope",
	}, workspace)
	if err == nil {
		t.Error("expected error for path outside workspace")
	}
}
