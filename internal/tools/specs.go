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
			Description:  "Read multiple files at once, returning their contents concatenated with headers.",
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
			Name:         "Skill",
			Description:  "Load a skill definition.",
			InputSchema:  mustJSON(skillSchema),
			RequiredMode: permission.ReadOnly,
			Source:       SourceBuiltin,
		},
		{
			Name:         "ToolSearch",
			Description:  "Search for deferred tools by name or keyword.",
			InputSchema:  mustJSON(toolSearchSchema),
			RequiredMode: permission.ReadOnly,
			Source:       SourceBuiltin,
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

	applyPatchSchema = `{
		"type": "object",
		"properties": {
			"patch": {"type": "string", "description": "Unified diff patch content"},
			"strip": {"type": "integer", "description": "Number of leading path components to strip (default 0)"}
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
			"commit_range": {"type": "string", "description": "Commit range (e.g., 'HEAD~3..HEAD', 'main..feature')"}
		}
	}`
)
