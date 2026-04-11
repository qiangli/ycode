package vfs

import (
	"context"
	"os"
	"path/filepath"
	"testing"
)

// resolveDir resolves symlinks in a path for test comparison on macOS
// where /var → /private/var.
func resolveDir(t *testing.T, path string) string {
	t.Helper()
	resolved, err := filepath.EvalSymlinks(path)
	if err != nil {
		t.Fatalf("EvalSymlinks(%s): %v", path, err)
	}
	return resolved
}

func TestValidatePath_NullByte(t *testing.T) {
	v, _ := New([]string{"/tmp"}, nil)
	_, err := v.ValidatePath(context.Background(), "/tmp/foo\x00bar")
	if err == nil {
		t.Fatal("expected error for null byte in path")
	}
}

func TestValidatePath_EmptyPath(t *testing.T) {
	v, _ := New([]string{"/tmp"}, nil)
	_, err := v.ValidatePath(context.Background(), "")
	if err == nil {
		t.Fatal("expected error for empty path")
	}
	_, err = v.ValidatePath(context.Background(), "   ")
	if err == nil {
		t.Fatal("expected error for whitespace-only path")
	}
}

func TestValidatePath_WithinAllowed(t *testing.T) {
	dir := t.TempDir()
	v, _ := New([]string{dir}, nil)

	// Create a file within the allowed directory.
	f := filepath.Join(dir, "test.txt")
	os.WriteFile(f, []byte("hello"), 0o644)

	got, err := v.ValidatePath(context.Background(), f)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	want := filepath.Join(resolveDir(t, dir), "test.txt")
	if got != want {
		t.Fatalf("got %s, want %s", got, want)
	}
}

func TestValidatePath_OutsideAllowed(t *testing.T) {
	dir := t.TempDir()
	other := t.TempDir()
	v, _ := New([]string{dir}, nil)

	_, err := v.ValidatePath(context.Background(), filepath.Join(other, "test.txt"))
	if err == nil {
		t.Fatal("expected error for path outside allowed directories")
	}
}

func TestValidatePath_PrefixAttack(t *testing.T) {
	// /tmp/foo should NOT match /tmp/foobar
	dir1 := t.TempDir() // e.g. /tmp/TestXXX1
	dir2 := dir1 + "extra"
	os.MkdirAll(dir2, 0o755)
	defer os.RemoveAll(dir2)

	v, _ := New([]string{dir1}, nil)

	f := filepath.Join(dir2, "secret.txt")
	os.WriteFile(f, []byte("secret"), 0o644)

	_, err := v.ValidatePath(context.Background(), f)
	if err == nil {
		t.Fatal("expected error for prefix attack path")
	}
}

func TestValidatePath_AllowedDirExactMatch(t *testing.T) {
	dir := t.TempDir()
	v, _ := New([]string{dir}, nil)

	// The directory itself should be allowed.
	got, err := v.ValidatePath(context.Background(), dir)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	want := resolveDir(t, dir)
	if got != want {
		t.Fatalf("got %s, want %s", got, want)
	}
}

func TestValidatePath_NonExistentFile(t *testing.T) {
	dir := t.TempDir()
	v, _ := New([]string{dir}, nil)

	// File doesn't exist yet but is within allowed dir — should succeed.
	f := filepath.Join(dir, "newfile.txt")
	got, err := v.ValidatePath(context.Background(), f)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	want := filepath.Join(resolveDir(t, dir), "newfile.txt")
	if got != want {
		t.Fatalf("got %s, want %s", got, want)
	}
}

func TestValidatePath_SymlinkWithinAllowed(t *testing.T) {
	dir := t.TempDir()
	v, _ := New([]string{dir}, nil)

	// Create target file and symlink within the same allowed dir.
	target := filepath.Join(dir, "target.txt")
	os.WriteFile(target, []byte("data"), 0o644)
	link := filepath.Join(dir, "link.txt")
	os.Symlink(target, link)

	got, err := v.ValidatePath(context.Background(), link)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	// Returned path is the link path with parent dirs resolved (not the symlink target).
	want := filepath.Join(resolveDir(t, dir), "link.txt")
	if got != want {
		t.Fatalf("got %s, want %s", got, want)
	}
}

func TestValidatePath_SymlinkResolvesToAllowedDir(t *testing.T) {
	// Symlink is outside allowed dirs, but resolves TO an allowed dir — implicitly allowed.
	allowedDir := t.TempDir()
	outsideDir := t.TempDir()

	target := filepath.Join(allowedDir, "file.txt")
	os.WriteFile(target, []byte("data"), 0o644)

	// Create symlink in outsideDir pointing into allowedDir.
	link := filepath.Join(outsideDir, "link.txt")
	os.Symlink(target, link)

	// Both dirs are allowed, so symlink resolving across them should work.
	v, _ := New([]string{allowedDir, outsideDir}, nil)
	_, err := v.ValidatePath(context.Background(), link)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestValidatePath_SymlinkOutsideNoPrompter(t *testing.T) {
	dir := t.TempDir()
	outside := t.TempDir()
	v, _ := New([]string{dir}, nil)

	// Create symlink in allowed dir pointing outside.
	target := filepath.Join(outside, "secret.txt")
	os.WriteFile(target, []byte("secret"), 0o644)
	link := filepath.Join(dir, "escape.txt")
	os.Symlink(target, link)

	_, err := v.ValidatePath(context.Background(), link)
	if err == nil {
		t.Fatal("expected error for symlink outside with no prompter")
	}
}

func TestValidatePath_SymlinkOutsidePrompterApproves(t *testing.T) {
	dir := t.TempDir()
	outside := t.TempDir()

	prompter := func(ctx context.Context, link, target string) (bool, error) {
		return true, nil
	}
	v, _ := New([]string{dir}, prompter)

	target := filepath.Join(outside, "secret.txt")
	os.WriteFile(target, []byte("secret"), 0o644)
	link := filepath.Join(dir, "escape.txt")
	os.Symlink(target, link)

	_, err := v.ValidatePath(context.Background(), link)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestValidatePath_SymlinkOutsidePrompterDenies(t *testing.T) {
	dir := t.TempDir()
	outside := t.TempDir()

	prompter := func(ctx context.Context, link, target string) (bool, error) {
		return false, nil
	}
	v, _ := New([]string{dir}, prompter)

	target := filepath.Join(outside, "secret.txt")
	os.WriteFile(target, []byte("secret"), 0o644)
	link := filepath.Join(dir, "escape.txt")
	os.Symlink(target, link)

	_, err := v.ValidatePath(context.Background(), link)
	if err == nil {
		t.Fatal("expected error for symlink outside with denier prompter")
	}
}

func TestValidatePathPair(t *testing.T) {
	dir := t.TempDir()
	v, _ := New([]string{dir}, nil)

	src := filepath.Join(dir, "a.txt")
	dst := filepath.Join(dir, "b.txt")
	os.WriteFile(src, []byte("data"), 0o644)

	rdir := resolveDir(t, dir)
	wantSrc := filepath.Join(rdir, "a.txt")
	wantDst := filepath.Join(rdir, "b.txt")

	gotSrc, gotDst, err := v.ValidatePathPair(context.Background(), src, dst)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if gotSrc != wantSrc || gotDst != wantDst {
		t.Fatalf("got (%s, %s), want (%s, %s)", gotSrc, gotDst, wantSrc, wantDst)
	}
}

func TestAllowedDirs(t *testing.T) {
	v, _ := New([]string{"/tmp", "/home/user"}, nil)
	dirs := v.AllowedDirs()
	if len(dirs) != 2 {
		t.Fatalf("expected 2 dirs, got %d", len(dirs))
	}
	// Should not have trailing separators.
	for _, d := range dirs {
		if d[len(d)-1] == os.PathSeparator {
			t.Fatalf("AllowedDirs should strip trailing separator: %s", d)
		}
	}
}
