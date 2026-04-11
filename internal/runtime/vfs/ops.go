package vfs

import (
	"context"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"time"
)

// CopyFile copies a file from src to dst, preserving permissions.
func (v *VFS) CopyFile(ctx context.Context, src, dst string) error {
	absSrc, absDst, err := v.ValidatePathPair(ctx, src, dst)
	if err != nil {
		return err
	}

	info, err := os.Stat(absSrc)
	if err != nil {
		return fmt.Errorf("stat %s: %w", absSrc, err)
	}
	if info.IsDir() {
		return fmt.Errorf("%s is a directory, not a file", absSrc)
	}
	if info.Size() > MaxFileSize {
		return fmt.Errorf("file too large (%d bytes, max %d)", info.Size(), MaxFileSize)
	}

	data, err := os.ReadFile(absSrc)
	if err != nil {
		return fmt.Errorf("read %s: %w", absSrc, err)
	}

	// Create parent directories for destination.
	if err := os.MkdirAll(filepath.Dir(absDst), 0o755); err != nil {
		return fmt.Errorf("create parent dirs for %s: %w", absDst, err)
	}

	if err := os.WriteFile(absDst, data, info.Mode().Perm()); err != nil {
		return fmt.Errorf("write %s: %w", absDst, err)
	}
	return nil
}

// MoveFile moves a file or directory from src to dst.
func (v *VFS) MoveFile(ctx context.Context, src, dst string) error {
	absSrc, absDst, err := v.ValidatePathPair(ctx, src, dst)
	if err != nil {
		return err
	}

	// Create parent directories for destination.
	if err := os.MkdirAll(filepath.Dir(absDst), 0o755); err != nil {
		return fmt.Errorf("create parent dirs for %s: %w", absDst, err)
	}

	if err := os.Rename(absSrc, absDst); err != nil {
		// Cross-device move: copy then delete.
		if linkErr, ok := err.(*os.LinkError); ok && linkErr.Err.Error() == "invalid cross-device link" {
			if cpErr := v.CopyFile(ctx, absSrc, absDst); cpErr != nil {
				return fmt.Errorf("cross-device copy: %w", cpErr)
			}
			return os.Remove(absSrc)
		}
		return fmt.Errorf("rename %s to %s: %w", absSrc, absDst, err)
	}
	return nil
}

// DeleteFile removes a file or directory. Directories require recursive=true.
func (v *VFS) DeleteFile(ctx context.Context, path string, recursive bool) error {
	absPath, err := v.ValidatePath(ctx, path)
	if err != nil {
		return err
	}

	info, err := os.Stat(absPath)
	if err != nil {
		return fmt.Errorf("stat %s: %w", absPath, err)
	}

	if info.IsDir() {
		if !recursive {
			return fmt.Errorf("%s is a directory; set recursive=true to delete", absPath)
		}
		return os.RemoveAll(absPath)
	}

	return os.Remove(absPath)
}

// CreateDirectory creates a directory and all parent directories.
func (v *VFS) CreateDirectory(ctx context.Context, path string) error {
	absPath, err := v.ValidatePath(ctx, path)
	if err != nil {
		return err
	}
	return os.MkdirAll(absPath, 0o755)
}

// ListDirectory lists the contents of a directory.
func (v *VFS) ListDirectory(ctx context.Context, path string) (string, error) {
	absPath, err := v.ValidatePath(ctx, path)
	if err != nil {
		return "", err
	}

	entries, err := os.ReadDir(absPath)
	if err != nil {
		return "", fmt.Errorf("read directory %s: %w", absPath, err)
	}

	var lines []string
	for _, e := range entries {
		name := e.Name()
		if e.IsDir() {
			name += "/"
		} else if e.Type()&fs.ModeSymlink != 0 {
			name += " @"
		}
		lines = append(lines, name)
	}

	if len(lines) == 0 {
		return "(empty directory)", nil
	}
	return strings.Join(lines, "\n"), nil
}

// Tree returns a tree-style listing of a directory.
func (v *VFS) Tree(ctx context.Context, path string, depth int, followSymlinks bool) (string, error) {
	absPath, err := v.ValidatePath(ctx, path)
	if err != nil {
		return "", err
	}

	if depth <= 0 {
		depth = 3
	}

	var buf strings.Builder
	buf.WriteString(absPath)
	buf.WriteByte('\n')
	v.buildTree(&buf, absPath, "", depth, followSymlinks)
	return buf.String(), nil
}

func (v *VFS) buildTree(buf *strings.Builder, dir, prefix string, depth int, followSymlinks bool) {
	if depth <= 0 {
		return
	}

	entries, err := os.ReadDir(dir)
	if err != nil {
		return
	}

	for i, e := range entries {
		isLast := i == len(entries)-1
		connector := "├── "
		childPrefix := "│   "
		if isLast {
			connector = "└── "
			childPrefix = "    "
		}

		name := e.Name()
		if e.IsDir() {
			name += "/"
		} else if e.Type()&fs.ModeSymlink != 0 {
			target, err := os.Readlink(filepath.Join(dir, e.Name()))
			if err == nil {
				name += " -> " + target
			}
			if !followSymlinks {
				buf.WriteString(prefix + connector + name + "\n")
				continue
			}
		}

		buf.WriteString(prefix + connector + name + "\n")

		if e.IsDir() {
			v.buildTree(buf, filepath.Join(dir, e.Name()), prefix+childPrefix, depth-1, followSymlinks)
		}
	}
}

// GetFileInfo returns metadata about a file or directory.
func (v *VFS) GetFileInfo(ctx context.Context, path string) (string, error) {
	absPath, err := v.ValidatePath(ctx, path)
	if err != nil {
		return "", err
	}

	info, err := os.Lstat(absPath)
	if err != nil {
		return "", fmt.Errorf("stat %s: %w", absPath, err)
	}

	var lines []string
	lines = append(lines, fmt.Sprintf("Path: %s", absPath))
	lines = append(lines, fmt.Sprintf("Size: %d bytes", info.Size()))
	lines = append(lines, fmt.Sprintf("Mode: %s", info.Mode()))
	lines = append(lines, fmt.Sprintf("Modified: %s", info.ModTime().Format(time.RFC3339)))

	if info.Mode()&fs.ModeSymlink != 0 {
		target, err := os.Readlink(absPath)
		if err == nil {
			lines = append(lines, fmt.Sprintf("Symlink target: %s", target))
		}
	}

	if info.IsDir() {
		lines = append(lines, "Type: directory")
		entries, err := os.ReadDir(absPath)
		if err == nil {
			lines = append(lines, fmt.Sprintf("Entries: %d", len(entries)))
		}
	} else {
		lines = append(lines, "Type: file")
	}

	return strings.Join(lines, "\n"), nil
}

// ReadMultipleFiles reads multiple files and returns their concatenated contents.
// Individual read errors are reported inline rather than aborting the entire operation.
func (v *VFS) ReadMultipleFiles(ctx context.Context, paths []string) (string, error) {
	if len(paths) == 0 {
		return "", fmt.Errorf("no paths provided")
	}

	var sections []string
	for _, p := range paths {
		absPath, err := v.ValidatePath(ctx, p)
		if err != nil {
			sections = append(sections, fmt.Sprintf("--- %s ---\nError: %s", p, err))
			continue
		}

		info, err := os.Stat(absPath)
		if err != nil {
			sections = append(sections, fmt.Sprintf("--- %s ---\nError: %s", absPath, err))
			continue
		}
		if info.IsDir() {
			sections = append(sections, fmt.Sprintf("--- %s ---\nError: is a directory", absPath))
			continue
		}
		if info.Size() > MaxFileSize {
			sections = append(sections, fmt.Sprintf("--- %s ---\nError: file too large (%d bytes)", absPath, info.Size()))
			continue
		}

		data, err := os.ReadFile(absPath)
		if err != nil {
			sections = append(sections, fmt.Sprintf("--- %s ---\nError: %s", absPath, err))
			continue
		}
		sections = append(sections, fmt.Sprintf("--- %s ---\n%s", absPath, string(data)))
	}

	return strings.Join(sections, "\n\n"), nil
}

// ListRoots returns the list of allowed directories.
func (v *VFS) ListRoots() string {
	dirs := v.AllowedDirs()
	if len(dirs) == 0 {
		return "No allowed directories configured."
	}
	return strings.Join(dirs, "\n")
}
