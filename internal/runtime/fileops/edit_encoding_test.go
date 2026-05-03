package fileops

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestEditFile_PreservesLineEndings(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "crlf.txt")

	// Create file with CRLF.
	original := "hello world\r\nfoo bar\r\n"
	if err := os.WriteFile(path, []byte(original), 0o644); err != nil {
		t.Fatal(err)
	}

	// Edit with LF in new_string.
	err := EditFile(EditFileParams{
		Path:      path,
		OldString: "hello world\r\n",
		NewString: "goodbye world\n",
	})
	if err != nil {
		t.Fatal(err)
	}

	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}

	// Replacement should have been normalized to CRLF.
	content := string(data)
	if !strings.Contains(content, "goodbye world\r\n") {
		t.Errorf("expected CRLF in replacement, got: %q", content)
	}
}

func TestEditFile_PreservesBOM(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "bom.txt")

	bom := "\xEF\xBB\xBF"
	original := bom + "hello world\n"
	if err := os.WriteFile(path, []byte(original), 0o644); err != nil {
		t.Fatal(err)
	}

	err := EditFile(EditFileParams{
		Path:      path,
		OldString: "hello world",
		NewString: "goodbye world",
	})
	if err != nil {
		t.Fatal(err)
	}

	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}

	// BOM should be preserved.
	if len(data) < 3 || data[0] != 0xEF || data[1] != 0xBB || data[2] != 0xBF {
		t.Errorf("BOM not preserved, got first bytes: %x", data[:min(3, len(data))])
	}
	if !strings.Contains(string(data), "goodbye world") {
		t.Errorf("replacement not applied: %q", data)
	}
}
