package vfs

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestCopyFile(t *testing.T) {
	dir := t.TempDir()
	v, _ := New([]string{dir}, nil)
	ctx := context.Background()

	src := filepath.Join(dir, "src.txt")
	dst := filepath.Join(dir, "dst.txt")
	os.WriteFile(src, []byte("hello"), 0o644)

	if err := v.CopyFile(ctx, src, dst); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	data, _ := os.ReadFile(dst)
	if string(data) != "hello" {
		t.Fatalf("got %q, want %q", data, "hello")
	}
}

func TestCopyFile_Directory(t *testing.T) {
	dir := t.TempDir()
	v, _ := New([]string{dir}, nil)

	subdir := filepath.Join(dir, "sub")
	os.MkdirAll(subdir, 0o755)

	err := v.CopyFile(context.Background(), subdir, filepath.Join(dir, "dst"))
	if err == nil {
		t.Fatal("expected error copying directory")
	}
}

func TestCopyFile_OutsideAllowed(t *testing.T) {
	dir := t.TempDir()
	other := t.TempDir()
	v, _ := New([]string{dir}, nil)

	src := filepath.Join(dir, "src.txt")
	os.WriteFile(src, []byte("data"), 0o644)

	err := v.CopyFile(context.Background(), src, filepath.Join(other, "dst.txt"))
	if err == nil {
		t.Fatal("expected error for destination outside allowed dirs")
	}
}

func TestMoveFile(t *testing.T) {
	dir := t.TempDir()
	v, _ := New([]string{dir}, nil)
	ctx := context.Background()

	src := filepath.Join(dir, "src.txt")
	dst := filepath.Join(dir, "dst.txt")
	os.WriteFile(src, []byte("moved"), 0o644)

	if err := v.MoveFile(ctx, src, dst); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if _, err := os.Stat(src); !os.IsNotExist(err) {
		t.Fatal("source file should not exist after move")
	}
	data, _ := os.ReadFile(dst)
	if string(data) != "moved" {
		t.Fatalf("got %q, want %q", data, "moved")
	}
}

func TestDeleteFile_File(t *testing.T) {
	dir := t.TempDir()
	v, _ := New([]string{dir}, nil)

	f := filepath.Join(dir, "del.txt")
	os.WriteFile(f, []byte("bye"), 0o644)

	if err := v.DeleteFile(context.Background(), f, false); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if _, err := os.Stat(f); !os.IsNotExist(err) {
		t.Fatal("file should be deleted")
	}
}

func TestDeleteFile_DirNotRecursive(t *testing.T) {
	dir := t.TempDir()
	v, _ := New([]string{dir}, nil)

	subdir := filepath.Join(dir, "sub")
	os.MkdirAll(subdir, 0o755)
	os.WriteFile(filepath.Join(subdir, "f.txt"), []byte("x"), 0o644)

	err := v.DeleteFile(context.Background(), subdir, false)
	if err == nil {
		t.Fatal("expected error deleting dir without recursive flag")
	}
}

func TestDeleteFile_DirRecursive(t *testing.T) {
	dir := t.TempDir()
	v, _ := New([]string{dir}, nil)

	subdir := filepath.Join(dir, "sub")
	os.MkdirAll(subdir, 0o755)
	os.WriteFile(filepath.Join(subdir, "f.txt"), []byte("x"), 0o644)

	if err := v.DeleteFile(context.Background(), subdir, true); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if _, err := os.Stat(subdir); !os.IsNotExist(err) {
		t.Fatal("directory should be deleted")
	}
}

func TestCreateDirectory(t *testing.T) {
	dir := t.TempDir()
	v, _ := New([]string{dir}, nil)

	newDir := filepath.Join(dir, "a", "b", "c")
	if err := v.CreateDirectory(context.Background(), newDir); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	info, err := os.Stat(newDir)
	if err != nil {
		t.Fatalf("directory not created: %v", err)
	}
	if !info.IsDir() {
		t.Fatal("expected directory")
	}
}

func TestListDirectory(t *testing.T) {
	dir := t.TempDir()
	v, _ := New([]string{dir}, nil)

	os.WriteFile(filepath.Join(dir, "a.txt"), []byte("a"), 0o644)
	os.MkdirAll(filepath.Join(dir, "sub"), 0o755)

	out, err := v.ListDirectory(context.Background(), dir)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !strings.Contains(out, "a.txt") {
		t.Fatalf("expected a.txt in output: %s", out)
	}
	if !strings.Contains(out, "sub/") {
		t.Fatalf("expected sub/ in output: %s", out)
	}
}

func TestListDirectory_Empty(t *testing.T) {
	dir := t.TempDir()
	v, _ := New([]string{dir}, nil)

	out, err := v.ListDirectory(context.Background(), dir)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if out != "(empty directory)" {
		t.Fatalf("expected empty marker, got: %s", out)
	}
}

func TestTree(t *testing.T) {
	dir := t.TempDir()
	v, _ := New([]string{dir}, nil)

	os.MkdirAll(filepath.Join(dir, "a", "b"), 0o755)
	os.WriteFile(filepath.Join(dir, "a", "f.txt"), []byte("x"), 0o644)

	out, err := v.Tree(context.Background(), dir, 3, false)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !strings.Contains(out, "a/") {
		t.Fatalf("expected a/ in tree output: %s", out)
	}
	if !strings.Contains(out, "f.txt") {
		t.Fatalf("expected f.txt in tree output: %s", out)
	}
}

func TestGetFileInfo(t *testing.T) {
	dir := t.TempDir()
	v, _ := New([]string{dir}, nil)

	f := filepath.Join(dir, "info.txt")
	os.WriteFile(f, []byte("hello"), 0o644)

	out, err := v.GetFileInfo(context.Background(), f)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !strings.Contains(out, "Size: 5 bytes") {
		t.Fatalf("expected size info: %s", out)
	}
	if !strings.Contains(out, "Type: file") {
		t.Fatalf("expected file type: %s", out)
	}
}

func TestReadMultipleFiles(t *testing.T) {
	dir := t.TempDir()
	v, _ := New([]string{dir}, nil)

	f1 := filepath.Join(dir, "a.txt")
	f2 := filepath.Join(dir, "b.txt")
	os.WriteFile(f1, []byte("aaa"), 0o644)
	os.WriteFile(f2, []byte("bbb"), 0o644)

	out, err := v.ReadMultipleFiles(context.Background(), []string{f1, f2})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !strings.Contains(out, "aaa") || !strings.Contains(out, "bbb") {
		t.Fatalf("expected both file contents: %s", out)
	}
}

func TestReadMultipleFiles_PartialError(t *testing.T) {
	dir := t.TempDir()
	v, _ := New([]string{dir}, nil)

	f1 := filepath.Join(dir, "a.txt")
	os.WriteFile(f1, []byte("ok"), 0o644)
	bad := filepath.Join(dir, "nonexistent.txt")

	out, err := v.ReadMultipleFiles(context.Background(), []string{f1, bad})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !strings.Contains(out, "ok") {
		t.Fatalf("expected successful read: %s", out)
	}
	if !strings.Contains(out, "Error:") {
		t.Fatalf("expected error for missing file: %s", out)
	}
}

func TestReadMultipleFiles_Empty(t *testing.T) {
	v, _ := New([]string{"/tmp"}, nil)
	_, err := v.ReadMultipleFiles(context.Background(), nil)
	if err == nil {
		t.Fatal("expected error for empty paths")
	}
}

func TestListRoots(t *testing.T) {
	v, _ := New([]string{"/tmp", "/home/user"}, nil)
	out := v.ListRoots()
	if !strings.Contains(out, "/tmp") || !strings.Contains(out, "/home/user") {
		t.Fatalf("expected both dirs: %s", out)
	}
}

func TestOpenFile(t *testing.T) {
	dir := t.TempDir()
	v, _ := New([]string{dir}, nil)
	ctx := context.Background()

	// Happy path: open file in allowed dir.
	path := filepath.Join(dir, "sub", "test.log")
	f, err := v.OpenFile(ctx, path, os.O_CREATE|os.O_WRONLY, 0o644)
	if err != nil {
		t.Fatalf("OpenFile: %v", err)
	}
	f.Write([]byte("hello"))
	f.Close()

	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read back: %v", err)
	}
	if string(data) != "hello" {
		t.Fatalf("got %q, want %q", data, "hello")
	}

	// Rejection: path outside allowed dirs.
	outside := filepath.Join(os.TempDir(), "vfs-test-outside-"+t.Name())
	_, err = v.OpenFile(ctx, outside, os.O_CREATE|os.O_WRONLY, 0o644)
	if err == nil {
		os.Remove(outside)
		t.Fatal("expected error for path outside allowed dirs")
	}
}
