package service

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
)

func TestExtractEditDetail_WriteFileExisting(t *testing.T) {
	dir := t.TempDir()
	target := filepath.Join(dir, "main.go")
	if err := os.WriteFile(target, []byte("package main\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	input := mustJSON(map[string]any{
		"file_path": "main.go",
		"content":   "package main\n\nfunc main() {}\n",
	})

	got := extractEditDetail("write_file", input, dir)
	if got == nil {
		t.Fatal("expected non-nil EditDetail")
	}
	if got.BeforeText != "package main\n" {
		t.Errorf("before_text = %q", got.BeforeText)
	}
	if got.AfterText != "package main\n\nfunc main() {}\n" {
		t.Errorf("after_text = %q", got.AfterText)
	}
	if got.FilePath != "main.go" {
		t.Errorf("file_path = %q", got.FilePath)
	}
}

func TestExtractEditDetail_WriteFileNew(t *testing.T) {
	dir := t.TempDir()
	input := mustJSON(map[string]any{
		"file_path": "new.txt",
		"content":   "hello",
	})
	got := extractEditDetail("write_file", input, dir)
	if got == nil || got.BeforeText != "" || got.AfterText != "hello" {
		t.Fatalf("unexpected detail %+v", got)
	}
}

func TestExtractEditDetail_EditFileSubstitution(t *testing.T) {
	dir := t.TempDir()
	target := filepath.Join(dir, "f.txt")
	if err := os.WriteFile(target, []byte("foo bar foo"), 0o644); err != nil {
		t.Fatal(err)
	}

	input := mustJSON(map[string]any{
		"file_path":  "f.txt",
		"old_string": "foo",
		"new_string": "FOO",
	})
	got := extractEditDetail("edit_file", input, dir)
	if got == nil {
		t.Fatal("nil EditDetail")
	}
	if got.AfterText != "FOO bar foo" {
		t.Errorf("default replace should be once: got %q", got.AfterText)
	}

	input = mustJSON(map[string]any{
		"file_path":   "f.txt",
		"old_string":  "foo",
		"new_string":  "FOO",
		"replace_all": true,
	})
	got = extractEditDetail("edit_file", input, dir)
	if got == nil || got.AfterText != "FOO bar FOO" {
		t.Errorf("replace_all should hit both: got %+v", got)
	}
}

func TestExtractEditDetail_EditFileMissingFile(t *testing.T) {
	dir := t.TempDir()
	input := mustJSON(map[string]any{
		"file_path":  "nope.txt",
		"old_string": "x",
		"new_string": "y",
	})
	if got := extractEditDetail("edit_file", input, dir); got != nil {
		t.Errorf("expected nil for missing file, got %+v", got)
	}
}

func TestExtractEditDetail_NonEditTool(t *testing.T) {
	if got := extractEditDetail("bash", json.RawMessage(`{"cmd":"ls"}`), ""); got != nil {
		t.Errorf("expected nil for bash, got %+v", got)
	}
}

func TestExtractEditDetail_BadJSON(t *testing.T) {
	if got := extractEditDetail("write_file", json.RawMessage("not json"), ""); got != nil {
		t.Errorf("expected nil for invalid JSON, got %+v", got)
	}
}
