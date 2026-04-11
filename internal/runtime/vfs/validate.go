package vfs

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// ValidatePath validates that path is within allowed directories.
// It normalizes the path, checks boundaries, resolves symlinks, and
// re-checks boundaries on the resolved path. If the resolved path is
// outside all allowed directories, the symlink prompter is invoked.
// Returns the cleaned absolute path.
func (v *VFS) ValidatePath(ctx context.Context, path string) (string, error) {
	absPath, err := v.validateRaw(path)
	if err != nil {
		return "", err
	}

	// Resolve symlinks for existing paths and re-check boundaries.
	resolved, err := filepath.EvalSymlinks(absPath)
	if err != nil {
		if os.IsNotExist(err) {
			// File doesn't exist yet — validate the parent directory instead.
			return v.validateParent(ctx, absPath)
		}
		return "", fmt.Errorf("resolve symlinks for %s: %w", absPath, err)
	}

	if v.isWithinAllowed(resolved) {
		return absPath, nil
	}

	// Resolved path is outside allowed dirs — prompt the user.
	prompter := v.getPrompter()
	if prompter == nil {
		return "", fmt.Errorf("path %s resolves to %s which is outside allowed directories", absPath, resolved)
	}

	allowed, err := prompter(ctx, absPath, resolved)
	if err != nil {
		return "", fmt.Errorf("symlink approval prompt: %w", err)
	}
	if !allowed {
		return "", fmt.Errorf("path %s resolves to %s: access denied by user", absPath, resolved)
	}

	return absPath, nil
}

// ValidatePathPair validates both source and destination paths.
func (v *VFS) ValidatePathPair(ctx context.Context, src, dst string) (string, string, error) {
	absSrc, err := v.ValidatePath(ctx, src)
	if err != nil {
		return "", "", fmt.Errorf("source: %w", err)
	}
	absDst, err := v.ValidatePath(ctx, dst)
	if err != nil {
		return "", "", fmt.Errorf("destination: %w", err)
	}
	return absSrc, absDst, nil
}

// validateRaw performs initial validation: null bytes, empty check, normalization,
// and boundary check on the raw path. On systems like macOS where /var is a
// symlink to /private/var, the path components are resolved so the boundary
// check uses the canonical form.
func (v *VFS) validateRaw(path string) (string, error) {
	if strings.ContainsRune(path, 0) {
		return "", fmt.Errorf("path contains null byte")
	}
	if strings.TrimSpace(path) == "" {
		return "", fmt.Errorf("empty path")
	}

	absPath, err := filepath.Abs(filepath.Clean(path))
	if err != nil {
		return "", fmt.Errorf("resolve path %q: %w", path, err)
	}

	// Resolve symlinks in the directory prefix (not the final component).
	// This handles systems like macOS where /var → /private/var, while
	// leaving actual file symlinks to be handled by ValidatePath's
	// EvalSymlinks step (which can invoke the prompter).
	absPath = resolveParentPrefix(absPath)

	if !v.isWithinAllowed(absPath) {
		return "", fmt.Errorf("path %s is outside allowed directories", absPath)
	}

	return absPath, nil
}

// resolveParentPrefix resolves symlinks in the parent directory of a path,
// leaving the final component unresolved. This normalizes system-level
// symlinks (e.g. /var → /private/var on macOS) without resolving file-level
// symlinks that need HITL approval.
func resolveParentPrefix(absPath string) string {
	dir := filepath.Dir(absPath)
	base := filepath.Base(absPath)

	// Walk up to find the deepest existing ancestor.
	if resolved, err := filepath.EvalSymlinks(dir); err == nil {
		return filepath.Join(resolved, base)
	}

	// Parent doesn't fully exist — walk up further.
	remaining := base
	current := dir
	for current != "/" && current != "." {
		parent := filepath.Dir(current)
		remaining = filepath.Join(filepath.Base(current), remaining)
		if resolved, err := filepath.EvalSymlinks(parent); err == nil {
			return filepath.Join(resolved, remaining)
		}
		current = parent
	}
	return absPath
}

// validateParent validates a non-existent path by checking its nearest
// existing ancestor directory.
func (v *VFS) validateParent(ctx context.Context, absPath string) (string, error) {
	dir := filepath.Dir(absPath)
	for dir != "/" && dir != "." {
		resolved, err := filepath.EvalSymlinks(dir)
		if err != nil {
			if os.IsNotExist(err) {
				dir = filepath.Dir(dir)
				continue
			}
			return "", fmt.Errorf("resolve parent symlinks for %s: %w", dir, err)
		}
		if v.isWithinAllowed(resolved) {
			return absPath, nil
		}
		// Parent resolves outside — prompt.
		prompter := v.getPrompter()
		if prompter == nil {
			return "", fmt.Errorf("path %s (parent %s resolves to %s) is outside allowed directories", absPath, dir, resolved)
		}
		allowed, err := prompter(ctx, dir, resolved)
		if err != nil {
			return "", fmt.Errorf("symlink approval prompt: %w", err)
		}
		if !allowed {
			return "", fmt.Errorf("path %s: access denied by user", absPath)
		}
		return absPath, nil
	}
	return "", fmt.Errorf("path %s is outside allowed directories", absPath)
}
