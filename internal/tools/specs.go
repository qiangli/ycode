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
			Description:  "Fetch a URL and extract readable content as Markdown. Supports output_format (markdown/text/html/metadata_only) and max_length parameters.",
			InputSchema:  mustJSON(webFetchSchema),
			RequiredMode: permission.ReadOnly,
			Source:       SourceBuiltin,
		},
		{
			Name:         "WebSearch",
			Description:  "Search the web and return structured results. Automatically selects the best available search provider (Brave, Tavily, SearXNG, or DuckDuckGo).",
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
			Category:     CategoryAgent,
		},
		{
			Name:         "Handoff",
			Description:  "Transfer control to another agent, passing context variables and a message.",
			InputSchema:  mustJSON(handoffSchema),
			RequiredMode: permission.DangerFullAccess,
			Source:       SourceBuiltin,
			Category:     CategoryAgent,
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
		{
			Name:         "git_add",
			Description:  "Stage file changes for the next commit. Can stage specific files or all changes.",
			InputSchema:  mustJSON(gitAddSchema),
			RequiredMode: permission.WorkspaceWrite,
			Source:       SourceBuiltin,
		},
		{
			Name:         "git_reset",
			Description:  "Unstage files from the index. Removes files from staging without changing the working tree.",
			InputSchema:  mustJSON(gitResetSchema),
			RequiredMode: permission.WorkspaceWrite,
			Source:       SourceBuiltin,
		},
		{
			Name:         "git_show",
			Description:  "Show details of a commit including message, author, date, and patch.",
			InputSchema:  mustJSON(gitShowSchema),
			RequiredMode: permission.ReadOnly,
			Source:       SourceBuiltin,
		},
		{
			Name:         "git_grep",
			Description:  "Search tracked files in git repository for a pattern. Returns matching lines with file paths and line numbers.",
			InputSchema:  mustJSON(gitGrepSchema),
			RequiredMode: permission.ReadOnly,
			Source:       SourceBuiltin,
		},

		// Git server — embedded Gitea for agent collaboration
		{
			Name:         "GitServerRepoList",
			Description:  "List repositories on the embedded git server. Returns repo names, descriptions, and clone URLs. Use to discover shared repos for agent collaboration.",
			InputSchema:  mustJSON(gitServerRepoListSchema),
			RequiredMode: permission.ReadOnly,
			Source:       SourceBuiltin,
		},
		{
			Name:         "GitServerRepoCreate",
			Description:  "Create a new repository on the embedded git server. Use when agents need a shared repo for collaboration, code review, or branch-based workflows.",
			InputSchema:  mustJSON(gitServerRepoCreateSchema),
			RequiredMode: permission.WorkspaceWrite,
			Source:       SourceBuiltin,
		},
		{
			Name:         "GitServerWorktreeCreate",
			Description:  "Create an isolated git worktree for an agent. Each agent gets its own branch and working directory, enabling parallel work without conflicts. Use before starting any file modifications in a multi-agent workflow.",
			InputSchema:  mustJSON(gitServerWorktreeCreateSchema),
			RequiredMode: permission.WorkspaceWrite,
			Source:       SourceBuiltin,
		},
		{
			Name:         "GitServerWorktreeMerge",
			Description:  "Merge an agent's worktree branch back to the base branch. Use after an agent completes its work to integrate changes. Performs a no-fast-forward merge to preserve the agent's commit history.",
			InputSchema:  mustJSON(gitServerWorktreeMergeSchema),
			RequiredMode: permission.WorkspaceWrite,
			Source:       SourceBuiltin,
		},
		{
			Name:         "GitServerWorktreeCleanup",
			Description:  "Remove an agent's worktree and clean up its branch. Use after merging or when abandoning work. Safe to call multiple times.",
			InputSchema:  mustJSON(gitServerWorktreeCleanupSchema),
			RequiredMode: permission.WorkspaceWrite,
			Source:       SourceBuiltin,
		},

		// Memory — persistent cross-session agent memory
		{
			Name:         "memory_save",
			Description:  "Save a memory to persistent storage. Memories persist across sessions and are loaded into the system prompt on startup. Types: user (role/preferences), feedback (corrections/confirmations), project (ongoing work context), reference (pointers to external resources).",
			InputSchema:  mustJSON(memorySaveSchema),
			RequiredMode: permission.WorkspaceWrite,
			Source:       SourceBuiltin,
		},
		{
			Name:         "memory_recall",
			Description:  "Search persistent memories by query. Uses semantic, full-text, or keyword matching with temporal decay scoring.",
			InputSchema:  mustJSON(memoryRecallSchema),
			RequiredMode: permission.ReadOnly,
			Source:       SourceBuiltin,
		},
		{
			Name:         "memory_forget",
			Description:  "Remove a memory from persistent storage by name.",
			InputSchema:  mustJSON(memoryForgetSchema),
			RequiredMode: permission.WorkspaceWrite,
			Source:       SourceBuiltin,
		},

		// Metrics — agent-facing tool execution metrics query
		{
			Name:         "query_metrics",
			Description:  "Query tool execution metrics from the current or past sessions. Use for debugging performance issues, understanding tool usage patterns, or analyzing failures. Supports aggregated stats, failure analysis, session summaries, and slow-tool detection.",
			InputSchema:  mustJSON(queryMetricsSchema),
			RequiredMode: permission.ReadOnly,
			Source:       SourceBuiltin,
		},

		// Traces — agent-facing OTEL trace query
		{
			Name:         "query_traces",
			Description:  "Query OTEL trace spans from local telemetry files. Use for debugging slow operations, finding errors, or understanding execution flow. Supports recent spans, slow span detection, error spans, and summary views.",
			InputSchema:  mustJSON(queryTracesSchema),
			RequiredMode: permission.ReadOnly,
			Source:       SourceBuiltin,
		},

		// Logs — agent-facing conversation log query
		{
			Name:         "query_logs",
			Description:  "Query conversation logs from local telemetry files. Use for reviewing past turns, finding errors, searching response content, or analyzing token usage and cost. Supports recent turns, error analysis, text search, and cost summaries.",
			InputSchema:  mustJSON(queryLogsSchema),
			RequiredMode: permission.ReadOnly,
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
		{
			Name:         "run_tests",
			Description:  "Detect and run the project's test suite, returning structured results with failed test names, failure messages, and file:line locations. Supports Go, Python (pytest), JavaScript/TypeScript (jest/vitest), and Rust (cargo test). Much more useful than running test commands via bash because the output is parsed into structured JSON.",
			InputSchema:  mustJSON(runTestsSchema),
			RequiredMode: permission.DangerFullAccess,
			Source:       SourceBuiltin,
		},
		swarmRunSpec(),
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
			"command": {"type": "string", "description": "The bash command to execute. Mutually exclusive with job_id."},
			"timeout": {"type": "integer", "description": "Timeout in milliseconds (max 600000)"},
			"run_in_background": {"type": "boolean", "description": "Run command in background and return a job ID for later polling"},
			"description": {"type": "string", "description": "Description of what this command does"},
			"stdin": {"type": "string", "description": "Content to pipe to the command's stdin"},
			"job_id": {"type": "string", "description": "Retrieve output from a background job. Mutually exclusive with command."},
			"signal": {"type": "string", "enum": ["SIGINT", "SIGTERM", "SIGKILL"], "description": "Send signal to a background job (requires job_id)"}
		},
		"required": []
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
			"prompt": {"type": "string", "description": "Question to focus extraction on"},
			"output_format": {"type": "string", "enum": ["markdown", "text", "html", "metadata_only"], "description": "Output format (default: markdown)"},
			"max_length": {"type": "integer", "description": "Maximum output length in characters (default: 32768)"},
			"click_link": {"type": "integer", "description": "Follow a numbered link from the previous fetch result (e.g., 1, 2, 3). Use instead of url to navigate links."}
		}
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

	handoffSchema = `{
		"type": "object",
		"properties": {
			"target_agent": {"type": "string", "description": "Name of the agent to hand off control to"},
			"context_vars": {"type": "object", "description": "Key-value pairs to pass as context to the target agent", "additionalProperties": {"type": "string"}},
			"message": {"type": "string", "description": "Instructions or context for the target agent"}
		},
		"required": ["target_agent"]
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
			"base_branch": {"type": "string", "description": "Base branch for merge_base diff (default: main or master)"},
			"context_lines": {"type": "integer", "description": "Number of context lines around changes (default: 3)"}
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
			"until": {"type": "string", "description": "Show commits until date (e.g., '2024-12-31', '1 day ago')"},
			"diff": {"type": "string", "description": "Show diff against a branch or commit (e.g., 'main..HEAD')"},
			"grep": {"type": "string", "description": "Search for commits that add or remove text matching this string (-G flag)"},
			"follow": {"type": "boolean", "description": "Track file renames when filtering by path (--follow)"}
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
			"start_point": {"type": "string", "description": "Starting point for new branch (default: HEAD)"},
			"remote": {"type": "boolean", "description": "List remote-tracking branches (-r flag, for action=list)"},
			"all": {"type": "boolean", "description": "List all branches including remote-tracking (-a flag, for action=list)"},
			"contains": {"type": "string", "description": "Only list branches containing the specified commit (for action=list)"}
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

	gitAddSchema = `{
		"type": "object",
		"properties": {
			"files": {"type": "array", "items": {"type": "string"}, "description": "File paths to stage"},
			"all": {"type": "boolean", "description": "Stage all changes (git add -A)"}
		}
	}`

	gitResetSchema = `{
		"type": "object",
		"properties": {
			"files": {"type": "array", "items": {"type": "string"}, "description": "Files to unstage (empty = unstage all)"}
		}
	}`

	gitShowSchema = `{
		"type": "object",
		"properties": {
			"revision": {"type": "string", "description": "Commit hash or reference to inspect"}
		},
		"required": ["revision"]
	}`

	gitGrepSchema = `{
		"type": "object",
		"properties": {
			"pattern": {"type": "string", "description": "Search pattern to match against tracked file contents"},
			"case_insensitive": {"type": "boolean", "description": "Case insensitive search"},
			"files_only": {"type": "boolean", "description": "Show only filenames of matching files"},
			"path": {"type": "string", "description": "Limit search to files under this path"}
		},
		"required": ["pattern"]
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

	memorySaveSchema = `{
		"type": "object",
		"properties": {
			"name": {"type": "string", "description": "Short name for the memory (used as filename and identifier)"},
			"description": {"type": "string", "description": "One-line description used for relevance matching in future sessions"},
			"content": {"type": "string", "description": "The memory content (markdown)"},
			"type": {"type": "string", "enum": ["user", "feedback", "project", "reference"], "description": "Memory type (default: project)"}
		},
		"required": ["name", "content"]
	}`

	memoryRecallSchema = `{
		"type": "object",
		"properties": {
			"query": {"type": "string", "description": "Search query to find relevant memories"},
			"max_results": {"type": "integer", "description": "Maximum results to return (default: 5)"}
		},
		"required": ["query"]
	}`

	memoryForgetSchema = `{
		"type": "object",
		"properties": {
			"name": {"type": "string", "description": "Name of the memory to remove"}
		},
		"required": ["name"]
	}`

	queryMetricsSchema = `{
		"type": "object",
		"properties": {
			"query_type": {
				"type": "string",
				"enum": ["tool_stats", "recent_failures", "session_summary", "slow_tools"],
				"description": "Type of metrics query: tool_stats (per-tool aggregates), recent_failures (recent failed invocations), session_summary (token usage and tool counts), slow_tools (invocations exceeding a duration threshold)"
			},
			"session_id": {
				"type": "string",
				"description": "Filter by session ID. Omit or 'current' for the current session. 'all' for cross-session data."
			},
			"limit": {
				"type": "integer",
				"description": "Maximum rows to return (default: 20, max: 100)"
			},
			"threshold_ms": {
				"type": "integer",
				"description": "Duration threshold in milliseconds for slow_tools query (default: 5000)"
			}
		},
		"required": ["query_type"]
	}`

	queryTracesSchema = `{
		"type": "object",
		"properties": {
			"query_type": {
				"type": "string",
				"enum": ["recent_spans", "slow_spans", "error_spans", "summary"],
				"description": "Type of trace query: recent_spans (latest spans), slow_spans (spans exceeding threshold), error_spans (spans with errors), summary (aggregated span counts and durations)"
			},
			"limit": {
				"type": "integer",
				"description": "Maximum results to return (default: 20, max: 100)"
			},
			"threshold_ms": {
				"type": "integer",
				"description": "Duration threshold in milliseconds for slow_spans query (default: 5000)"
			},
			"session_id": {
				"type": "string",
				"description": "Filter by session ID. Omit for all sessions."
			}
		},
		"required": ["query_type"]
	}`

	queryLogsSchema = `{
		"type": "object",
		"properties": {
			"query_type": {
				"type": "string",
				"enum": ["recent_turns", "turn_errors", "search", "cost_summary"],
				"description": "Type of log query: recent_turns (latest conversation turns), turn_errors (turns with errors), search (text search in responses), cost_summary (token usage and cost aggregation)"
			},
			"limit": {
				"type": "integer",
				"description": "Maximum results to return (default: 20, max: 100)"
			},
			"pattern": {
				"type": "string",
				"description": "Search pattern for 'search' query type. Case-insensitive text search in responses and tool calls."
			},
			"session_id": {
				"type": "string",
				"description": "Filter by session ID. Omit for all sessions."
			}
		},
		"required": ["query_type"]
	}`

	gitServerRepoListSchema = `{
		"type": "object",
		"properties": {
			"limit": {"type": "integer", "description": "Maximum repos to return (default: 50)"}
		}
	}`

	gitServerRepoCreateSchema = `{
		"type": "object",
		"properties": {
			"name": {"type": "string", "description": "Repository name (lowercase, no spaces)"},
			"description": {"type": "string", "description": "Short description of the repository's purpose"},
			"auto_init": {"type": "boolean", "description": "Initialize with a README (default: true)"}
		},
		"required": ["name"]
	}`

	gitServerWorktreeCreateSchema = `{
		"type": "object",
		"properties": {
			"repo_dir": {"type": "string", "description": "Path to the git repository to create a worktree from"},
			"agent_id": {"type": "string", "description": "Unique agent identifier (used for branch naming: agent/<id>)"}
		},
		"required": ["repo_dir", "agent_id"]
	}`

	gitServerWorktreeMergeSchema = `{
		"type": "object",
		"properties": {
			"repo_dir": {"type": "string", "description": "Original repository directory"},
			"worktree_path": {"type": "string", "description": "Path to the worktree (returned by GitServerWorktreeCreate)"},
			"branch": {"type": "string", "description": "Agent branch name (returned by GitServerWorktreeCreate)"},
			"base_branch": {"type": "string", "description": "Branch to merge into (e.g. 'main')"}
		},
		"required": ["repo_dir", "branch", "base_branch"]
	}`

	gitServerWorktreeCleanupSchema = `{
		"type": "object",
		"properties": {
			"repo_dir": {"type": "string", "description": "Original repository directory"},
			"worktree_path": {"type": "string", "description": "Path to the worktree to remove"}
		},
		"required": ["repo_dir", "worktree_path"]
	}`

	runTestsSchema = `{
		"type": "object",
		"properties": {
			"pattern": {
				"type": "string",
				"description": "Test name pattern to filter (e.g. 'TestFoo' for Go, '-k test_foo' for pytest). Optional — runs all tests if omitted."
			},
			"path": {
				"type": "string",
				"description": "Directory or file path to run tests in. Defaults to project root."
			},
			"framework": {
				"type": "string",
				"enum": ["auto", "go", "pytest", "jest", "vitest", "cargo"],
				"description": "Test framework to use. 'auto' (default) detects from project files."
			},
			"timeout": {
				"type": "integer",
				"description": "Timeout in seconds (default: 120)."
			}
		}
	}`
)
