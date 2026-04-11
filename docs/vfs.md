# Virtual Filesystem (VFS) Layer

The VFS layer enforces filesystem access boundaries for all tool operations. Every path is validated against a set of allowed directories before any I/O proceeds.

## Package

`internal/runtime/vfs/`

## Architecture

```
LLM tool call
    │
    ▼
Tool Handler (tools/file.go, tools/vfs_tools.go, tools/search.go)
    │
    ▼
vfs.ValidatePath(ctx, path)
    │
    ├── Reject null bytes / empty paths
    ├── filepath.Abs + filepath.Clean
    ├── Resolve parent symlinks (normalize /var → /private/var on macOS)
    ├── Boundary check against allowed directories
    ├── filepath.EvalSymlinks on existing paths
    ├── Re-check resolved path against allowed dirs
    │     ├── Within allowed → allow (no prompt)
    │     └── Outside allowed → invoke SymlinkPrompter (HITL)
    │           ├── Approved → allow
    │           └── Denied / no prompter → reject
    │
    ▼
Actual filesystem operation (os.ReadFile, os.WriteFile, etc.)
```

## Allowed Directories

Default always-allowed directories (set in `cmd/ycode/main.go`):
- `os.TempDir()` — system temp directory
- `cwd` — current working directory / project root

User-configurable via `settings.json`:
```json
{
  "allowedDirectories": ["/home/user/shared-data", "/opt/assets"]
}
```

Directories from all three config tiers (user > project > local) are merged additively.

### Normalization

At construction time, each allowed directory is:
1. Resolved to an absolute path (`filepath.Abs`)
2. Symlink-resolved (`filepath.EvalSymlinks`) — handles macOS `/var` → `/private/var`
3. Suffixed with `os.PathSeparator` to prevent prefix attacks (`/tmp/foo` must not match `/tmp/foobar`)
4. Deduplicated

## Symlink Policy

- Symlinks whose resolved target is **within** any allowed directory are **implicitly allowed** — no user prompt needed.
  - Example: `/home/user/tmp` → `/tmp`, and `/tmp` is allowed → access granted.
- Symlinks whose resolved target is **outside** all allowed directories trigger the `SymlinkPrompter` (HITL user approval).
- If no prompter is configured, outside-boundary symlinks are denied.

## Tools

### Always-available (VFS-validated)

These existing tools now validate paths through the VFS:

| Tool | Permission | Description |
|------|-----------|-------------|
| `read_file` | ReadOnly | Read a file with offset/limit |
| `write_file` | WorkspaceWrite | Write content to a file |
| `edit_file` | WorkspaceWrite | String replacement in a file |
| `glob_search` | ReadOnly | Find files by glob pattern |
| `grep_search` | ReadOnly | Search file contents by regex |

### Deferred (discoverable via ToolSearch)

| Tool | Permission | Parameters |
|------|-----------|------------|
| `copy_file` | WorkspaceWrite | `source`, `destination` |
| `move_file` | WorkspaceWrite | `source`, `destination` |
| `delete_file` | WorkspaceWrite | `path`, `recursive` (bool, default false) |
| `create_directory` | WorkspaceWrite | `path` |
| `list_directory` | ReadOnly | `path` |
| `tree` | ReadOnly | `path`, `depth` (int, default 3), `follow_symlinks` (bool, default false) |
| `get_file_info` | ReadOnly | `path` |
| `read_multiple_files` | ReadOnly | `paths` (array of strings) |
| `list_roots` | ReadOnly | (none) — returns allowed directories |

## Key Files

| File | Purpose |
|------|---------|
| `internal/runtime/vfs/vfs.go` | VFS struct, constructor, allowed-dirs management |
| `internal/runtime/vfs/validate.go` | Path validation pipeline, parent prefix resolution |
| `internal/runtime/vfs/ops.go` | Filesystem operations (copy, move, delete, mkdir, list, tree, info) |
| `internal/runtime/vfs/validate_test.go` | Validation tests (null bytes, boundaries, prefix attacks, symlinks) |
| `internal/runtime/vfs/ops_test.go` | Operation tests |
| `internal/tools/vfs_tools.go` | Handler registration for 9 new VFS tools |
| `internal/tools/file.go` | Existing file handlers updated to use VFS |
| `internal/tools/search.go` | Existing search handlers updated to use VFS |
| `internal/runtime/config/config.go` | `AllowedDirectories` config field |
| `internal/runtime/prompt/sections.go` | `FilesystemSection()` for system prompt |

## System Prompt Integration

The `FilesystemSection` in `internal/runtime/prompt/sections.go` is added as a dynamic section in `BuildDefault`. It tells the LLM:
- Which directories are allowed
- That all paths must be absolute
- That symlinks within allowed dirs need no approval
- That symlinks outside allowed dirs require user approval
- The 10 MB file size limit
- That additional filesystem tools are available via ToolSearch

## Constraints

- Maximum file size: 10 MB (`vfs.MaxFileSize`)
- `delete_file` on directories requires explicit `recursive=true`
- `move_file` handles cross-device moves via copy + delete fallback
- `read_multiple_files` reports per-file errors inline rather than aborting
