package tools

import (
	"encoding/json"

	"github.com/qiangli/ycode/internal/runtime/permission"
)

// RegisterBuiltins registers all built-in tools with the registry.
func RegisterBuiltins(r *Registry) {
	for _, spec := range builtinSpecs() {
		_ = r.Register(spec)
	}
}

// builtinSpecs returns all built-in tool specifications.
func builtinSpecs() []*ToolSpec {
	return []*ToolSpec{
		// Always-available core tools
		{
			Name:            "bash",
			Description:     "Execute a bash command and return its output.",
			InputSchema:     mustJSON(bashSchema),
			RequiredMode:    permission.DangerFullAccess,
			Source:          SourceBuiltin,
			AlwaysAvailable: true,
		},
		{
			Name:            "read_file",
			Description:     "Read a file from the local filesystem.",
			InputSchema:     mustJSON(readFileSchema),
			RequiredMode:    permission.ReadOnly,
			Source:          SourceBuiltin,
			AlwaysAvailable: true,
		},
		{
			Name:            "write_file",
			Description:     "Write content to a file, creating parent directories as needed.",
			InputSchema:     mustJSON(writeFileSchema),
			RequiredMode:    permission.WorkspaceWrite,
			Source:          SourceBuiltin,
			AlwaysAvailable: true,
		},
		{
			Name:            "edit_file",
			Description:     "Perform exact string replacement in a file.",
			InputSchema:     mustJSON(editFileSchema),
			RequiredMode:    permission.WorkspaceWrite,
			Source:          SourceBuiltin,
			AlwaysAvailable: true,
		},
		{
			Name:            "glob_search",
			Description:     "Find files matching a glob pattern.",
			InputSchema:     mustJSON(globSchema),
			RequiredMode:    permission.ReadOnly,
			Source:          SourceBuiltin,
			AlwaysAvailable: true,
		},
		{
			Name:            "grep_search",
			Description:     "Search file contents using a regex pattern.",
			InputSchema:     mustJSON(grepSchema),
			RequiredMode:    permission.ReadOnly,
			Source:          SourceBuiltin,
			AlwaysAvailable: true,
		},

		// Deferred filesystem tools (discovered via ToolSearch)
		{
			Name:         "copy_file",
			Description:  "Copy a file from source to destination.",
			InputSchema:  mustJSON(copyFileSchema),
			RequiredMode: permission.WorkspaceWrite,
			Source:       SourceBuiltin,
		},
		{
			Name:         "move_file",
			Description:  "Move or rename a file or directory.",
			InputSchema:  mustJSON(moveFileSchema),
			RequiredMode: permission.WorkspaceWrite,
			Source:       SourceBuiltin,
		},
		{
			Name:         "delete_file",
			Description:  "Delete a file or directory. Directories require recursive=true.",
			InputSchema:  mustJSON(deleteFileSchema),
			RequiredMode: permission.WorkspaceWrite,
			Source:       SourceBuiltin,
		},
		{
			Name:         "create_directory",
			Description:  "Create a directory and all parent directories.",
			InputSchema:  mustJSON(createDirectorySchema),
			RequiredMode: permission.WorkspaceWrite,
			Source:       SourceBuiltin,
		},
		{
			Name:         "list_directory",
			Description:  "List the contents of a directory.",
			InputSchema:  mustJSON(listDirectorySchema),
			RequiredMode: permission.ReadOnly,
			Source:       SourceBuiltin,
		},
		{
			Name:         "tree",
			Description:  "Display a tree-style directory listing with configurable depth.",
			InputSchema:  mustJSON(treeSchema),
			RequiredMode: permission.ReadOnly,
			Source:       SourceBuiltin,
		},
		{
			Name:         "get_file_info",
			Description:  "Get metadata about a file or directory (size, permissions, type, modification time).",
			InputSchema:  mustJSON(getFileInfoSchema),
			RequiredMode: permission.ReadOnly,
			Source:       SourceBuiltin,
		},
		{
			Name:         "read_multiple_files",
			Description:  "Read multiple files in one call. Preferred over sequential read_file calls during exploration.",
			InputSchema:  mustJSON(readMultipleFilesSchema),
			RequiredMode: permission.ReadOnly,
			Source:       SourceBuiltin,
		},
		{
			Name:         "list_roots",
			Description:  "List the allowed filesystem directories.",
			InputSchema:  mustJSON(emptySchema),
			RequiredMode: permission.ReadOnly,
			Source:       SourceBuiltin,
		},

		// Deferred tools
		{
			Name:         "WebFetch",
			Description:  "Fetch a URL and convert HTML to text.",
			InputSchema:  mustJSON(webFetchSchema),
			RequiredMode: permission.ReadOnly,
			Source:       SourceBuiltin,
		},
		{
			Name:         "WebSearch",
			Description:  "Search the web and return results.",
			InputSchema:  mustJSON(webSearchSchema),
			RequiredMode: permission.ReadOnly,
			Source:       SourceBuiltin,
		},
		{
			Name:         "AskUserQuestion",
			Description:  "Ask the user a question and wait for their response.",
			InputSchema:  mustJSON(askUserSchema),
			RequiredMode: permission.ReadOnly,
			Source:       SourceBuiltin,
			Category:     CategoryInteractive,
		},
		{
			Name:         "TodoWrite",
			Description:  "Write a structured task list.",
			InputSchema:  mustJSON(todoWriteSchema),
			RequiredMode: permission.WorkspaceWrite,
			Source:       SourceBuiltin,
		},
		{
			Name:            "Skill",
			Description:     "Execute a skill or load a skill definition. Builtin skills (commit, review, pr) run optimized operations directly. Pass the skill name and optional arguments.",
			InputSchema:     mustJSON(skillSchema),
			RequiredMode:    permission.ReadOnly,
			Source:          SourceBuiltin,
			AlwaysAvailable: true,
		},
		{
			Name:            "ToolSearch",
			Description:     "Search for deferred tools by name or keyword. Returns full tool schemas so discovered tools can be invoked.",
			InputSchema:     mustJSON(toolSearchSchema),
			RequiredMode:    permission.ReadOnly,
			Source:          SourceBuiltin,
			AlwaysAvailable: true,
		},
		{
			Name:         "Agent",
			Description:  "Spawn a subagent to handle a complex task.",
			InputSchema:  mustJSON(agentSchema),
			RequiredMode: permission.DangerFullAccess,
			Source:       SourceBuiltin,
			Category:     CategoryLLM,
		},
		{
			Name:         "TaskCreate",
			Description:  "Create a background task.",
			InputSchema:  mustJSON(taskCreateSchema),
			RequiredMode: permission.DangerFullAccess,
			Source:       SourceBuiltin,
			Category:     CategoryLLM,
		},
		{
			Name:         "TaskGet",
			Description:  "Get status of a task.",
			InputSchema:  mustJSON(taskGetSchema),
			RequiredMode: permission.ReadOnly,
			Source:       SourceBuiltin,
		},
		{
			Name:         "TaskList",
			Description:  "List all tasks.",
			InputSchema:  mustJSON(taskListSchema),
			RequiredMode: permission.ReadOnly,
			Source:       SourceBuiltin,
		},
		{
			Name:         "EnterPlanMode",
			Description:  "Enable planning mode.",
			InputSchema:  mustJSON(emptySchema),
			RequiredMode: permission.WorkspaceWrite,
			Source:       SourceBuiltin,
		},
		{
			Name:         "ExitPlanMode",
			Description:  "Exit planning mode.",
			InputSchema:  mustJSON(emptySchema),
			RequiredMode: permission.WorkspaceWrite,
			Source:       SourceBuiltin,
		},
		{
			Name:         "Sleep",
			Description:  "Wait for a specified duration in milliseconds.",
			InputSchema:  mustJSON(sleepSchema),
			RequiredMode: permission.ReadOnly,
			Source:       SourceBuiltin,
		},
		{
			Name:         "NotebookEdit",
			Description:  "Edit a Jupyter notebook cell.",
			InputSchema:  mustJSON(notebookEditSchema),
			RequiredMode: permission.WorkspaceWrite,
			Source:       SourceBuiltin,
		},
		{
			Name:         "RemoteTrigger",
			Description:  "Trigger a remote webhook or endpoint.",
			InputSchema:  mustJSON(remoteTriggerSchema),
			RequiredMode: permission.DangerFullAccess,
			Source:       SourceBuiltin,
		},
		{
			Name:         "Config",
			Description:  "Get or set runtime configuration.",
			InputSchema:  mustJSON(configSchema),
			RequiredMode: permission.WorkspaceWrite,
			Source:       SourceBuiltin,
		},
		{
			Name:         "compact_context",
			Description:  "Request immediate compaction of conversation context. Use when the conversation has accumulated irrelevant context that should be summarized to free up space.",
			InputSchema:  mustJSON(compactContextSchema),
			RequiredMode: permission.ReadOnly,
			Source:       SourceBuiltin,
		},
		{
			Name:         "apply_patch",
			Description:  "Apply a unified diff patch to one or more files. The patch should be in standard unified diff format.",
			InputSchema:  mustJSON(applyPatchSchema),
			RequiredMode: permission.WorkspaceWrite,
			Source:       SourceBuiltin,
		},
		{
			Name:         "view_image",
			Description:  "Read an image file and return its contents for visual analysis. Supports PNG, JPG, GIF, SVG, WebP.",
			InputSchema:  mustJSON(viewImageSchema),
			RequiredMode: permission.ReadOnly,
			Source:       SourceBuiltin,
		},
		{
			Name:         "view_diff",
			Description:  "Show git diff output. Can show staged changes, unstaged changes, or diff between commits.",
			InputSchema:  mustJSON(viewDiffSchema),
			RequiredMode: permission.ReadOnly,
			Source:       SourceBuiltin,
		},

		// Git operation tools
		{
			Name:         "git_status",
			Description:  "Show the working tree status. Returns modified, staged, and untracked files.",
			InputSchema:  mustJSON(gitStatusSchema),
			RequiredMode: permission.ReadOnly,
			Source:       SourceBuiltin,
		},
		{
			Name:         "git_log",
			Description:  "Show commit history. Returns recent commits with hash, author, date, and message.",
			InputSchema:  mustJSON(gitLogSchema),
			RequiredMode: permission.ReadOnly,
			Source:       SourceBuiltin,
		},
		{
			Name:         "git_commit",
			Description:  "Stage files and create a git commit. Use patterns to stage specific files or '.' for all changes.",
			InputSchema:  mustJSON(gitCommitSchema),
			RequiredMode: permission.WorkspaceWrite,
			Source:       SourceBuiltin,
		},
		{
			Name:         "git_branch",
			Description:  "List, create, switch, or delete git branches.",
			InputSchema:  mustJSON(gitBranchSchema),
			RequiredMode: permission.WorkspaceWrite,
			Source:       SourceBuiltin,
		},
		{
			Name:         "git_stash",
			Description:  "Stash or restore working directory changes. Supports push, pop, list, and drop operations.",
			InputSchema:  mustJSON(gitStashSchema),
			RequiredMode: permission.WorkspaceWrite,
			Source:       SourceBuiltin,
		},

		// Memos — persistent long-term memory storage
		{
			Name:         "MemosStore",
			Description:  "Save a memo to persistent long-term memory. Use #tags in the content for categorization (e.g. #project, #decision, #learning). Memos persist across sessions and are searchable.",
			InputSchema:  mustJSON(memosStoreSchema),
			RequiredMode: permission.WorkspaceWrite,
			Source:       SourceBuiltin,
		},
		{
			Name:         "MemosSearch",
			Description:  "Search persistent long-term memories by content or tag. Returns matching memos with their content and metadata.",
			InputSchema:  mustJSON(memosSearchSchema),
			RequiredMode: permission.ReadOnly,
			Source:       SourceBuiltin,
		},
		{
			Name:         "MemosList",
			Description:  "List recent memos from persistent long-term memory. Supports pagination for browsing through stored memories.",
			InputSchema:  mustJSON(memosListSchema),
			RequiredMode: permission.ReadOnly,
			Source:       SourceBuiltin,
		},
		{
			Name:         "MemosDelete",
			Description:  "Delete a memo from persistent long-term memory by its ID.",
			InputSchema:  mustJSON(memosDeleteSchema),
			RequiredMode: permission.WorkspaceWrite,
			Source:       SourceBuiltin,
		},
	}
}

func mustJSON(s string) json.RawMessage {
	return json.RawMessage(s)
}

// JSON schemas for tool input parameters.
var (
	bashSchema = `{
		"type": "object",
		"properties": {
			"command": {"type": "string", "description": "The bash command to execute"},
			"timeout": {"type": "integer", "description": "Timeout in milliseconds (max 600000)"},
			"run_in_background": {"type": "boolean", "description": "Run command in background"},
			"description": {"type": "string", "description": "Description of what this command does"}
		},
		"required": ["command"]
	}`

	readFileSchema = `{
		"type": "object",
		"properties": {
			"file_path": {"type": "string", "description": "Absolute path to the file"},
			"offset": {"type": "integer", "description": "Line number to start reading from (0-based)"},
			"limit": {"type": "integer", "description": "Number of lines to read"}
		},
		"required": ["file_path"]
	}`

	writeFileSchema = `{
		"type": "object",
		"properties": {
			"file_path": {"type": "string", "description": "Absolute path to the file"},
			"content": {"type": "string", "description": "Content to write"}
		},
		"required": ["file_path", "content"]
	}`

	editFileSchema = `{
		"type": "object",
		"properties": {
			"file_path": {"type": "string", "description": "Absolute path to the file"},
			"old_string": {"type": "string", "description": "Text to replace"},
			"new_string": {"type": "string", "description": "Replacement text"},
			"replace_all": {"type": "boolean", "description": "Replace all occurrences"}
		},
		"required": ["file_path", "old_string", "new_string"]
	}`

	globSchema = `{
		"type": "object",
		"properties": {
			"pattern": {"type": "string", "description": "Glob pattern to match files"},
			"path": {"type": "string", "description": "Base directory to search in"}
		},
		"required": ["pattern"]
	}`

	grepSchema = `{
		"type": "object",
		"properties": {
			"pattern": {"type": "string", "description": "Regex pattern to search for"},
			"path": {"type": "string", "description": "File or directory to search in"},
			"glob": {"type": "string", "description": "File glob filter"},
			"type": {"type": "string", "description": "File type filter (go, py, js, etc.)"},
			"output_mode": {"type": "string", "enum": ["content", "files_with_matches", "count"]},
			"context": {"type": "integer", "description": "Context lines around matches"},
			"head_limit": {"type": "integer", "description": "Maximum number of results"}
		},
		"required": ["pattern"]
	}`

	copyFileSchema = `{
		"type": "object",
		"properties": {
			"source": {"type": "string", "description": "Absolute path to the source file"},
			"destination": {"type": "string", "description": "Absolute path to the destination"}
		},
		"required": ["source", "destination"]
	}`

	moveFileSchema = `{
		"type": "object",
		"properties": {
			"source": {"type": "string", "description": "Absolute path to the source file or directory"},
			"destination": {"type": "string", "description": "Absolute path to the destination"}
		},
		"required": ["source", "destination"]
	}`

	deleteFileSchema = `{
		"type": "object",
		"properties": {
			"path": {"type": "string", "description": "Absolute path to the file or directory to delete"},
			"recursive": {"type": "boolean", "description": "Delete directories recursively (default false)", "default": false}
		},
		"required": ["path"]
	}`

	createDirectorySchema = `{
		"type": "object",
		"properties": {
			"path": {"type": "string", "description": "Absolute path of the directory to create"}
		},
		"required": ["path"]
	}`

	listDirectorySchema = `{
		"type": "object",
		"properties": {
			"path": {"type": "string", "description": "Absolute path of the directory to list"}
		},
		"required": ["path"]
	}`

	treeSchema = `{
		"type": "object",
		"properties": {
			"path": {"type": "string", "description": "Absolute path of the root directory"},
			"depth": {"type": "integer", "description": "Maximum depth to traverse (default 3)", "default": 3},
			"follow_symlinks": {"type": "boolean", "description": "Follow symbolic links (default false)", "default": false}
		},
		"required": ["path"]
	}`

	getFileInfoSchema = `{
		"type": "object",
		"properties": {
			"path": {"type": "string", "description": "Absolute path to the file or directory"}
		},
		"required": ["path"]
	}`

	readMultipleFilesSchema = `{
		"type": "object",
		"properties": {
			"paths": {"type": "array", "items": {"type": "string"}, "description": "Array of absolute file paths to read"}
		},
		"required": ["paths"]
	}`

	webFetchSchema = `{
		"type": "object",
		"properties": {
			"url": {"type": "string", "description": "URL to fetch"},
			"prompt": {"type": "string", "description": "Question to answer about the page content"}
		},
		"required": ["url"]
	}`

	webSearchSchema = `{
		"type": "object",
		"properties": {
			"query": {"type": "string", "description": "Search query"},
			"max_results": {"type": "integer", "description": "Maximum results to return"}
		},
		"required": ["query"]
	}`

	askUserSchema = `{
		"type": "object",
		"properties": {
			"question": {"type": "string", "description": "Question to ask the user"},
			"choices": {"type": "array", "items": {"type": "string"}, "description": "Optional multiple choice options"}
		},
		"required": ["question"]
	}`

	todoWriteSchema = `{
		"type": "object",
		"properties": {
			"todos": {"type": "array", "items": {"type": "object"}, "description": "Task list items"}
		},
		"required": ["todos"]
	}`

	skillSchema = `{
		"type": "object",
		"properties": {
			"skill": {"type": "string", "description": "Skill name to load"},
			"args": {"type": "string", "description": "Optional arguments for the skill"}
		},
		"required": ["skill"]
	}`

	toolSearchSchema = `{
		"type": "object",
		"properties": {
			"query": {"type": "string", "description": "Search query for deferred tools"},
			"max_results": {"type": "integer", "description": "Maximum results (default 5)"}
		},
		"required": ["query"]
	}`

	agentSchema = `{
		"type": "object",
		"properties": {
			"description": {"type": "string", "description": "Short description of the task"},
			"prompt": {"type": "string", "description": "Task description for the agent"},
			"subagent_type": {"type": "string", "description": "Agent type"},
			"run_in_background": {"type": "boolean", "description": "Run in background"},
			"model": {"type": "string", "description": "Model override"}
		},
		"required": ["description", "prompt"]
	}`

	taskCreateSchema = `{
		"type": "object",
		"properties": {
			"description": {"type": "string", "description": "Task description"},
			"prompt": {"type": "string", "description": "Task prompt"}
		},
		"required": ["description", "prompt"]
	}`

	taskGetSchema = `{
		"type": "object",
		"properties": {
			"task_id": {"type": "string", "description": "Task ID"}
		},
		"required": ["task_id"]
	}`

	taskListSchema = `{
		"type": "object",
		"properties": {}
	}`

	emptySchema = `{
		"type": "object",
		"properties": {}
	}`

	sleepSchema = `{
		"type": "object",
		"properties": {
			"duration_ms": {"type": "integer", "description": "Duration to sleep in milliseconds"}
		},
		"required": ["duration_ms"]
	}`

	notebookEditSchema = `{
		"type": "object",
		"properties": {
			"notebook_path": {"type": "string", "description": "Path to the notebook"},
			"cell_index": {"type": "integer", "description": "Cell index"},
			"action": {"type": "string", "enum": ["replace", "insert", "delete"]},
			"content": {"type": "string", "description": "New cell content"}
		},
		"required": ["notebook_path", "cell_index", "action"]
	}`

	remoteTriggerSchema = `{
		"type": "object",
		"properties": {
			"url": {"type": "string", "description": "URL to trigger"},
			"method": {"type": "string", "description": "HTTP method"},
			"body": {"type": "string", "description": "Request body"}
		},
		"required": ["url"]
	}`

	configSchema = `{
		"type": "object",
		"properties": {
			"action": {"type": "string", "enum": ["get", "set"]},
			"key": {"type": "string", "description": "Config key"},
			"value": {"description": "Config value (for set action)"}
		},
		"required": ["action", "key"]
	}`

	compactContextSchema = `{
		"type": "object",
		"properties": {
			"reason": {"type": "string", "description": "Brief reason for requesting compaction (e.g., 'topic shift', 'accumulated irrelevant context')"}
		}
	}`

	applyPatchSchema = `{
		"type": "object",
		"properties": {
			"patch": {"type": "string", "description": "Patch content. Supports two formats: (1) Standard unified diff, or (2) Compact format starting with '*** Begin Patch' using *** Add/Delete/Update File headers and @@ context hints. The compact format is preferred for token efficiency."},
			"strip": {"type": "integer", "description": "Number of leading path components to strip (default 0, only for unified diff format)"}
		},
		"required": ["patch"]
	}`

	viewImageSchema = `{
		"type": "object",
		"properties": {
			"file_path": {"type": "string", "description": "Absolute path to the image file"}
		},
		"required": ["file_path"]
	}`

	viewDiffSchema = `{
		"type": "object",
		"properties": {
			"staged": {"type": "boolean", "description": "Show staged changes (--cached)"},
			"path": {"type": "string", "description": "Limit diff to specific file or directory"},
			"commit_range": {"type": "string", "description": "Commit range (e.g., 'HEAD~3..HEAD', 'main..feature')"},
			"merge_base": {"type": "boolean", "description": "Diff from the merge base of current branch against base_branch (accurate PR diff)"},
			"base_branch": {"type": "string", "description": "Base branch for merge_base diff (default: main or master)"}
		}
	}`

	gitStatusSchema = `{
		"type": "object",
		"properties": {
			"short": {"type": "boolean", "description": "Use short format output (default true)"}
		}
	}`

	gitLogSchema = `{
		"type": "object",
		"properties": {
			"count": {"type": "integer", "description": "Number of commits to show (default 10)"},
			"oneline": {"type": "boolean", "description": "One line per commit (default true)"},
			"path": {"type": "string", "description": "Limit to commits affecting this path"},
			"author": {"type": "string", "description": "Filter by author name or email"},
			"since": {"type": "string", "description": "Show commits since date (e.g., '2024-01-01', '1 week ago')"},
			"diff": {"type": "string", "description": "Show diff against a branch or commit (e.g., 'main..HEAD')"}
		}
	}`

	gitCommitSchema = `{
		"type": "object",
		"properties": {
			"message": {"type": "string", "description": "Commit message"},
			"files": {"type": "array", "items": {"type": "string"}, "description": "Files to stage before committing. Use [\".\"] for all changes."},
			"all": {"type": "boolean", "description": "Stage all modified and deleted files (-a flag)"}
		},
		"required": ["message"]
	}`

	gitBranchSchema = `{
		"type": "object",
		"properties": {
			"action": {"type": "string", "enum": ["list", "create", "switch", "delete"], "description": "Branch operation to perform (default: list)"},
			"name": {"type": "string", "description": "Branch name (required for create, switch, delete)"},
			"start_point": {"type": "string", "description": "Starting point for new branch (default: HEAD)"}
		}
	}`

	gitStashSchema = `{
		"type": "object",
		"properties": {
			"action": {"type": "string", "enum": ["push", "pop", "list", "drop", "show"], "description": "Stash operation (default: push)"},
			"message": {"type": "string", "description": "Stash message (for push)"},
			"index": {"type": "integer", "description": "Stash index (for pop, drop, show; default 0)"}
		}
	}`

	memosStoreSchema = `{
		"type": "object",
		"properties": {
			"content": {"type": "string", "description": "Markdown content of the memo. Use #tags in the content for categorization."},
			"visibility": {"type": "string", "enum": ["PRIVATE", "PROTECTED", "PUBLIC"], "description": "Visibility (default: PRIVATE)"}
		},
		"required": ["content"]
	}`

	memosSearchSchema = `{
		"type": "object",
		"properties": {
			"query": {"type": "string", "description": "Search query (matched against memo content)"},
			"tag": {"type": "string", "description": "Filter by #tag name (without the # prefix)"},
			"max_results": {"type": "integer", "description": "Maximum results to return (default: 20)"}
		}
	}`

	memosListSchema = `{
		"type": "object",
		"properties": {
			"page_size": {"type": "integer", "description": "Number of memos to return (default: 20, max: 100)"},
			"page_token": {"type": "string", "description": "Pagination token from previous response"}
		}
	}`

	memosDeleteSchema = `{
		"type": "object",
		"properties": {
			"memo_id": {"type": "string", "description": "The memo ID to delete"}
		},
		"required": ["memo_id"]
	}`
)
