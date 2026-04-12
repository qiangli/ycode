# VFS Layer — Implementation Checklist

Tracking document for the Virtual Filesystem (VFS) layer described in [vfs.md](vfs.md).

## Core VFS Package (`internal/runtime/vfs/`)

- [x] `vfs.go` — VFS struct, constructor, allowed-dirs management, dedup, normalization
- [x] `validate.go` — Path validation pipeline: null bytes, abs/clean, parent symlink resolution, boundary check, EvalSymlinks, SymlinkPrompter HITL
- [x] `ops.go` — Filesystem operations: CopyFile, MoveFile, DeleteFile, CreateDirectory, ListDirectory, Tree, GetFileInfo, ReadMultipleFiles, ListRoots
- [x] `validate_test.go` — Validation tests: null bytes, empty paths, boundary checks, prefix-attack prevention, symlink resolution, prompter approval/denial
- [x] `ops_test.go` — Operation tests: copy, move, delete, mkdir, list, tree, info, multi-read, partial errors

## Existing Tools Updated for VFS (`internal/tools/`)

- [x] `file.go` — read_file, write_file, edit_file use `vfs.ValidatePath`
- [x] `search.go` — glob_search, grep_search use `vfs.ValidatePath`
- [x] `patch.go` — Patch application uses VFS validation
- [x] `image.go` — Image viewing uses VFS validation

## New Deferred Tools (`internal/tools/vfs_tools.go`)

- [x] `copy_file` — WorkspaceWrite, validates source + destination
- [x] `move_file` — WorkspaceWrite, cross-device fallback via copy+delete
- [x] `delete_file` — WorkspaceWrite, requires `recursive=true` for directories
- [x] `create_directory` — WorkspaceWrite, creates parents
- [x] `list_directory` — ReadOnly, dir/symlink markers
- [x] `tree` — ReadOnly, configurable depth + follow_symlinks
- [x] `get_file_info` — ReadOnly, returns metadata
- [x] `read_multiple_files` — ReadOnly, per-file error reporting
- [x] `list_roots` — ReadOnly, returns allowed directories

## Configuration (`internal/runtime/config/`)

- [x] `AllowedDirectories` field in Config struct (JSON: `allowedDirectories`)
- [x] Merged additively across config tiers (user > project > local)

## Initialization (`cmd/ycode/main.go`)

- [x] Default allowed dirs: `os.TempDir()` + cwd
- [x] Append user-configured `AllowedDirectories`
- [x] Construct VFS and wire into tool handlers
- [x] Pass `AllowedDirs` to prompt context

## System Prompt Integration (`internal/runtime/prompt/`)

- [x] `sections.go` — `FilesystemSection()` emits allowed dirs, absolute-path rule, symlink policy, 10 MB limit, ToolSearch hint
- [x] `builder.go` — `FilesystemSection` registered as dynamic section in `BuildDefault`

## Constraints & Security

- [x] 10 MB max file size (`MaxFileSize`)
- [x] Trailing-separator prefix-attack prevention
- [x] Two-stage symlink resolution (parent dirs, then file)
- [x] HITL prompter for out-of-boundary symlinks
- [x] Thread-safe with RWMutex

## OTEL Storage Path Integration

- [x] `resolveOTELDataDir()` — priority: `OTEL_STORAGE_PATH` env > config `dataDir` > default `~/.ycode/otel`
- [x] OTEL storage path added as third default VFS allowed dir (alongside `/tmp` and `$PWD`)
- [x] Per-instance subdirectories: `instances/{uuid}/` under OTEL storage path
- [x] `session.NewWithID()` — constructor accepting pre-generated instance ID
- [x] Sessions stored under `{otel_storage_path}/instances/{id}/session/`
- [x] `VFS.OpenFile()` — validates path then opens file with parent dir creation
- [x] `FileOpener` interface for VFS-backed file opening in OTEL components
- [x] `RequestLogger` uses `FileOpener` for VFS-validated daily file rotation
- [x] File-based trace/metric exporters use `FileOpener` for VFS-validated writes
- [x] `ProviderConfig.InstanceDir` — per-instance base for file exports
- [x] `LoggerProvider` added to OTEL Provider — gRPC log exporter to collector → VictoriaLogs
- [x] `ConversationLogger` — emits structured OTEL log records with metadata (`log.type`, `session.id`, `instance.id`, `turn.index`, `llm.model`, etc.)
- [x] `LogConversation()` emits both file JSONL and OTEL log records
- [x] `LogToolCall()` emits per-tool structured log records for VictoriaLogs
- [x] `LogChatMessage()` emits per-message structured log records
- [x] Retention cleanup scans `instances/*/logs,traces,metrics` + removes empty instance dirs
- [x] VFS passed through `setupOTEL` → `ProviderConfig` → `RequestLoggerConfig`

## Tests

- [x] `go test -short -race ./internal/runtime/vfs/...` passes
- [x] `TestOpenFile` — happy path + boundary rejection
- [x] `TestNewWithID` / `TestNew` — session constructors
- [x] `TestConversationLoggerLogConversation` — structured log record attributes
- [x] `TestConversationLoggerLogToolCall` — tool call log records
- [x] `TestConversationLoggerNilRecord` — nil safety
- [x] `TestRunCleanupPerInstance` — per-instance retention cleanup
- [x] `TestRunCleanupRemovesEmptyInstanceDir` — empty dir removal
- [x] `make build` passes (tidy → fmt → vet → compile → test → verify)
