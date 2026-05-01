# Agentic Tools Cross-Project Comparison

> Comprehensive comparison of built-in tools across 10 agentic coding projects in `priorart/` vs ycode.
> Generated 2026-05-01. Updated after gap-closure implementation and
> document reading, agent orchestration, and planning tools additions.

---

## Projects Surveyed

| # | Project | Language | Description |
|---|---------|----------|-------------|
| 1 | **aider** | Python | Terminal pair-programming agent; text-based edit formats |
| 2 | **cline** | TypeScript | VS Code AI extension with rich tool surface |
| 3 | **codex** | Rust/TS | OpenAI CLI agent with deep orchestration |
| 4 | **opencode** | TypeScript | Open-source AI coding CLI |
| 5 | **openhands** | Python | Autonomous dev agent platform |
| 6 | **geminicli** | TypeScript | Google Gemini CLI agent |
| 7 | **clawcode** | Rust | Claude Code open-source variant (reference) |
| 8 | **openclaw** | TypeScript | Multi-channel agent gateway |
| 9 | **continue** | TypeScript | VS Code + CLI AI extension |
| 10 | **kimicli** | Python | Kimi CLI agent |

Excluded: **ralph** (no LLM-callable tools; shell orchestrator only), **mini-swe-agent** (single `bash` tool only).

---

## ycode vs Prior Art — Detailed Tool Comparison

**Legend:** ✓ = ycode has this tool, ✗ = ycode does not have this tool

Prior-art column lists which projects have the tool (abbreviated): **ai**=aider, **cl**=cline, **cx**=codex, **oc**=opencode, **oh**=openhands, **gc**=geminicli, **cc**=clawcode, **ow**=openclaw, **co**=continue, **ki**=kimicli

---

### File — Read

| Tool | ycode | Prior Art | Description |
|------|:-----:|-----------|-------------|
| Read file (text, with offset/limit) | ✓ `read_file` | cl, oc, oh, gc, cc, ow, co, ki | Read text file contents with optional line range |
| Read file (range only, start/end line) | ✓ `read_file` | cl, co | Read specific line range from a file |
| Read multiple files (batch) | ✓ `read_multiple_files` | gc | Read and concatenate multiple files by path array |
| Read media file (image/audio/video) | ✓ `view_image` | cx, gc, ow, ki | Read image file; audio/video not supported |
| Read currently open file (IDE) | ✗ | co | Read file currently open in IDE editor |
| Read PDF document | ✓ `read_document` | cl, ow | Extract text from PDF with optional page range |
| Read DOCX (Word) | ✓ `read_document` | — | Extract text from Microsoft Word documents |
| Read XLSX (Excel) | ✓ `read_document` | — | Extract spreadsheet data with sheet/row structure |
| Read PPTX (PowerPoint) | ✓ `read_document` | — | Extract text from presentation slides |
| Read CSV | ✓ `read_document` | — | Read CSV files as structured text |

### File — Write

| Tool | ycode | Prior Art | Description |
|------|:-----:|-----------|-------------|
| Write/create file | ✓ `write_file` | ai, cl, oc, oh, gc, cc, ow, co, ki | Create or overwrite a file |
| Create new file (only if not exists) | ✓ `write_file` | co | Create file, fail if exists (ycode overwrites) |

### File — Edit

| Tool | ycode | Prior Art | Description |
|------|:-----:|-----------|-------------|
| Find-and-replace in file | ✓ `edit_file` | ai, cl, oc, oh, gc, cc, ow, co, ki | Exact string replacement with optional replace-all |
| Multi-edit (batch replacements) | ✓ `edit_file` | co | Multiple find-and-replace in one call (ycode uses replace_all flag) |
| Apply unified diff patch | ✓ `apply_patch` | cl, cx, oc, ow | Apply patch in unified diff or compact format |
| Undo last edit | ✗ | oh | Revert the last file edit operation |

### File — Search & Navigation

| Tool | ycode | Prior Art | Description |
|------|:-----:|-----------|-------------|
| Glob / find files by pattern | ✓ `glob_search` | oc, oh, gc, cc, ow, co, ki | Find files matching glob patterns |
| Grep / regex content search | ✓ `grep_search` | cl, oc, oh, gc, cc, ow, co, ki | Search file contents with regex |
| List directory contents | ✓ `list_directory` | cl, cx, gc, ow, co | List files and subdirectories |
| Tree directory listing | ✓ `tree` | — | Display tree-style directory listing with configurable depth |
| Get file metadata | ✓ `get_file_info` | — | Get file size, permissions, type, modification time |
| List allowed filesystem roots | ✓ `list_roots` | — | List VFS-allowed directories |

### File — Management

| Tool | ycode | Prior Art | Description |
|------|:-----:|-----------|-------------|
| Copy file | ✓ `copy_file` | — | Copy a file from source to destination |
| Move / rename file | ✓ `move_file` | — | Move or rename a file or directory |
| Delete file | ✓ `delete_file` | — | Delete a file or directory |
| Create directory | ✓ `create_directory` | — | Create a directory and parents |

### File — Code Definitions

| Tool | ycode | Prior Art | Description |
|------|:-----:|-----------|-------------|
| List code definitions (top-level symbols) | ✓ `symbol_search` | cl | List classes, functions, methods in files |
| AST structural search | ✓ `ast_search` | — | Tree-sitter-based structural pattern search |
| Find references / callers | ✓ `find_references` | — | Find all references to a symbol across workspace |
| Impact analysis | ✓ `find_impact` | — | Cross-file caller/reference impact analysis |
| Semantic code search | ✓ `semantic_search` | co | Natural language codebase search |

### Shell & Execution

| Tool | ycode | Prior Art | Description |
|------|:-----:|-----------|-------------|
| Bash / shell command | ✓ `bash` | ai, cl, cx, oc, oh, gc, cc, ow, co, ki | Execute shell commands with timeout |
| Background execution | ✓ `bash` (run_in_background) | cx, gc, ow, co | Run command in background, poll later |
| Job poll / retrieve output | ✓ `bash` (job_id) | cx, ow, co | Retrieve output from background job |
| Send signal to job | ✓ `bash` (signal) | cx, ow | Send SIGINT/SIGTERM/SIGKILL to background job |
| Write stdin to ongoing PTY | ✓ `bash` (stdin) | cx | Write to an ongoing interactive session |
| PowerShell (Windows) | ✗ | cx, cc | Windows shell execution |
| Request additional permissions | ✗ | cx | Request FS/network permissions at runtime |
| Run test suite (structured) | ✓ `run_tests` | — | Auto-detect framework, return parsed test results |

### Git — Status & History

| Tool | ycode | Prior Art | Description |
|------|:-----:|-----------|-------------|
| Git status | ✓ `git_status` | — | Show modified, staged, untracked files |
| Git log | ✓ `git_log` | — | Show commit history with filters (author, date, path, grep) |
| Git show (commit details) | ✓ `git_show` | — | Show commit message, author, date, and patch |
| Git grep (search tracked files) | ✓ `git_grep` | — | Search tracked files for pattern |
| View diff | ✓ `view_diff` | co | Show staged/unstaged/commit-range diffs |

### Git — Staging & Committing

| Tool | ycode | Prior Art | Description |
|------|:-----:|-----------|-------------|
| Git add (stage files) | ✓ `git_add` | — | Stage specific files or all changes |
| Git reset (unstage) | ✓ `git_reset` | — | Unstage files from the index |
| Git commit | ✓ `git_commit` | — | Stage files and create commit in one call |
| Git stash (push/pop/list/drop/show) | ✓ `git_stash` | — | Stash or restore working directory changes |

### Git — Branching

| Tool | ycode | Prior Art | Description |
|------|:-----:|-----------|-------------|
| Git branch (list/create/switch/delete) | ✓ `git_branch` | — | Full branch lifecycle management |

### Git Server (Embedded Gitea)

| Tool | ycode | Prior Art | Description |
|------|:-----:|-----------|-------------|
| List repos | ✓ `GitServerRepoList` | — | List repositories on embedded git server |
| Create repo | ✓ `GitServerRepoCreate` | — | Create repository for agent collaboration |
| Create worktree | ✓ `GitServerWorktreeCreate` | — | Create isolated worktree per agent |
| Merge worktree | ✓ `GitServerWorktreeMerge` | — | Merge agent branch back to base |
| Cleanup worktree | ✓ `GitServerWorktreeCleanup` | — | Remove agent worktree and branch |

### GitHub

| Tool | ycode | Prior Art | Description |
|------|:-----:|-----------|-------------|
| Create pull request | ✓ `gh_pr_create` | — | Create PR with title, body, head, base |
| List pull requests | ✓ `gh_pr_list` | — | List PRs filtered by state |
| Get pull request details | ✓ `gh_pr_get` | — | Get PR details with optional diff |
| List PR changed files | ✓ `gh_pr_files` | — | List files changed in a PR |
| Submit PR review | ✓ `gh_pr_review` | — | Submit APPROVE/REQUEST_CHANGES/COMMENT review |
| Comment on PR | ✓ `gh_pr_comment` | — | Add comment to a pull request |
| List issues | ✓ `gh_issue_list` | — | List issues with state/label filters |
| Get issue details | ✓ `gh_issue_get` | — | Get issue details with comments |
| Comment on issue | ✓ `gh_issue_comment` | — | Add comment to an issue |
| Get CI check status | ✓ `gh_checks` | — | Get check run status for a git ref |
| Create issue | ✗ | — | Create a new GitHub issue |
| Merge pull request | ✗ | — | Merge a pull request |
| Close/reopen issue or PR | ✗ | — | Change state of issue or PR |
| Add labels | ✗ | — | Add labels to issue or PR |

### Web & Search

| Tool | ycode | Prior Art | Description |
|------|:-----:|-----------|-------------|
| Web search | ✓ `WebSearch` | cl, cx, oc, gc, cc, ow, co, ki | Search the web (Brave/Tavily/SearXNG/DDG) |
| Web fetch (URL to markdown) | ✓ `WebFetch` | ai, cl, oc, gc, cc, ow, co, ki | Fetch URL, extract content as markdown/text/html |
| Follow link from previous fetch | ✓ `WebFetch` (click_link) | — | Navigate numbered links from prior fetch |
| Code search API (Exa) | ✗ | oc | Search programming docs/APIs via Exa Code |
| Google-native web search | ✗ | gc | Google Search via Gemini API |

### Browser Automation

| Tool | ycode | Prior Art | Description |
|------|:-----:|-----------|-------------|
| Navigate to URL | ✓ `browser_navigate` | cl, oh, ow | Open URL in browser |
| Click element | ✓ `browser_click` | cl, oh, ow | Click a page element |
| Type text | ✓ `browser_type` | cl, oh, ow | Type text into input field |
| Scroll page | ✓ `browser_scroll` | cl, oh, ow | Scroll up or down |
| Take screenshot | ✓ `browser_screenshot` | cl, oh, ow | Capture page screenshot |
| Extract page content | ✓ `browser_extract` | oh, ow | Get page text content |
| Go back | ✓ `browser_back` | oh, ow | Navigate browser back |
| List/switch tabs | ✓ `browser_tabs` | oh, ow | List and switch browser tabs |
| Evaluate JavaScript | ✗ | ow | Run JS in browser context |
| Launch/close browser | ✗ | cl | Explicit browser lifecycle |

### Agent & Orchestration

| Tool | ycode | Prior Art | Description |
|------|:-----:|-----------|-------------|
| Spawn subagent | ✓ `Agent` | cl, cx, oc, gc, cc, ow, co, ki | Launch sub-agent for focused task |
| Handoff to agent | ✓ `Handoff` | — | Transfer control with context vars |
| Swarm orchestration | ✓ `swarm_run` | — | Run multi-agent swarm workflow |
| List live agents | ✓ `AgentList` | cx, ow | List agents with status, tool usage, duration |
| Wait for agent(s) | ✓ `AgentWait` | cx, ow | Block until background agent completes or timeout |
| Close/kill agent | ✓ `AgentClose` | cx, ow | Cancel a running background agent task |
| Send message to agent | ✗ | cx, ow | Send input to an existing running agent |
| Resume closed agent | ✗ | cx | Resume a previously closed agent |
| CSV batch agents | ✗ | cx | Spawn one agent per CSV row |

### Worker / Coding Lane

| Tool | ycode | Prior Art | Description |
|------|:-----:|-----------|-------------|
| Create worker | ✓ `WorkerCreate` | cc | Create a coding worker boot session |
| Get worker state | ✓ `WorkerGet` | cc | Get current worker state and details |
| Observe worker (terminal snapshot) | ✓ `WorkerObserve` | cc | Feed terminal snapshot for boot detection |
| Resolve trust prompt | ✓ `WorkerResolveTrust` | cc | Resolve detected trust/permission prompt |
| Await ready | ✓ `WorkerAwaitReady` | cc | Wait until worker is ready for prompt |
| Send prompt to worker | ✓ `WorkerSendPrompt` | cc | Send task prompt after ready state |
| Restart worker | ✓ `WorkerRestart` | cc | Restart after failed/stale startup |
| Terminate worker | ✓ `WorkerTerminate` | cc | Terminate worker and mark lane finished |
| Observe completion | ✓ `WorkerObserveCompletion` | cc | Check worker finish status |

### Team & Parallel Execution

| Tool | ycode | Prior Art | Description |
|------|:-----:|-----------|-------------|
| Create team | ✓ `TeamCreate` | cc | Create team of parallel sub-agents |
| Delete team | ✓ `TeamDelete` | cc | Delete team and stop its tasks |
| Use subagents (parallel) | ✗ | cl | Run up to 5 in-process subagents in parallel |

### Task Management

| Tool | ycode | Prior Art | Description |
|------|:-----:|-----------|-------------|
| Create background task | ✓ `TaskCreate` | — | Create a background task |
| Get task status | ✓ `TaskGet` | ki | Get status of a task by ID |
| List all tasks | ✓ `TaskList` | ki | List all tasks and their status |
| Update/message task | ✓ `TaskUpdate` | — | Send message to a running task |
| Stop task | ✓ `TaskStop` | ki | Stop a running task |
| Get task output | ✓ `TaskOutput` | ki | Retrieve output from a task |
| Structured todo list | ✓ `TodoWrite` | cx, oc, oh, gc, cc, ow, co, ki | Manage structured task/checklist |

### Tool Discovery

| Tool | ycode | Prior Art | Description |
|------|:-----:|-----------|-------------|
| Search deferred tools | ✓ `ToolSearch` | cx, cc | Search and activate deferred tools by keyword |
| Suggest missing tool/plugin | ✗ | cx | Suggest connector to install for missing capability |

### Planning & Workflow

| Tool | ycode | Prior Art | Description |
|------|:-----:|-----------|-------------|
| Enter plan mode | ✓ `EnterPlanMode` | cl, oc, gc, cc, ki | Switch to read-only planning phase |
| Exit plan mode | ✓ `ExitPlanMode` | cl, oc, gc, cc, ki | Finalize plan and start implementation |
| Update step-by-step plan | ✓ `UpdatePlan` | cx, ow | Update plan with hierarchical steps and statuses |
| List plan | ✓ `ListPlan` | — | Show current plan board as markdown table |
| Set goal | ✓ `SetGoal` | cx | Set task goal with objective and token budget |
| Get goal | ✓ `GetGoal` | cx | Retrieve current goal, status, and budget |
| Set task status (PLANNING/WORKING/DONE) | ✓ `SetTaskStatus` | co | Simple status indicator for UI |

### User Interaction & Session

| Tool | ycode | Prior Art | Description |
|------|:-----:|-----------|-------------|
| Ask user question | ✓ `AskUserQuestion` | cl, cx, oc, gc, cc, co, ki | Ask clarifying question with choice options |
| Send message to user | ✓ `SendUserMessage` | cc, ow | Proactive non-blocking message |
| Attempt completion | ✓ `AttemptCompletion` | cl, oh, gc | Signal task complete with result |
| Think (reasoning scratchpad) | ✓ `Think` | oh, ki | Log reasoning step without side effects |
| Compact/condense context | ✓ `compact_context` | cl, oh | Compress/summarize conversation context |
| Start new task/session | ✗ | cl | Start new session with context summary |
| Context revert (checkpoint) | ✗ | ki | Revert context to earlier checkpoint |
| Report failure | ✗ | co | Report unrecoverable task failure |
| Structured output | ✓ `StructuredOutput` | cc | Return structured JSON output |

### Code Intelligence

| Tool | ycode | Prior Art | Description |
|------|:-----:|-----------|-------------|
| LSP hover (type info) | ✓ `LSP` (action=hover) | oc, cc | Get type/documentation at position |
| LSP go-to-definition | ✓ `LSP` (action=definition) | oc, cc | Jump to symbol definition |
| LSP find references | ✓ `LSP` (action=references) | oc, cc | Find all references to symbol |
| LSP document symbols | ✓ `LSP` (action=symbols) | oc, cc | List symbols in a file |
| LSP diagnostics | ✓ `LSP` (action=diagnostics) | oc, cc | Get file diagnostics/errors |
| LSP workspace symbols | ✗ | cc | Search symbols across entire workspace |
| LSP call hierarchy | ✗ | oc | Incoming/outgoing call hierarchy |
| LSP go-to-implementation | ✗ | oc | Jump to interface implementation |
| Repo map (symbol overview) | ✗ (context injection only) | ai, co | Token-budgeted file-to-symbol overview |
| Lint and fix | ✗ | ai | Run linter and auto-fix errors |

### MCP (Model Context Protocol)

| Tool | ycode | Prior Art | Description |
|------|:-----:|-----------|-------------|
| Call MCP tool | ✓ `MCP` | cl, oh, cc | Invoke tool from connected MCP server |
| List MCP resources | ✓ `ListMcpResources` | cx, gc, cc | List resources from MCP servers |
| Read MCP resource | ✓ `ReadMcpResource` | cx, gc, cc | Read specific MCP resource by URI |
| Authenticate with MCP server | ✓ `McpAuth` | cc | OAuth/credential auth for MCP |
| Load MCP documentation | ✗ | cl | Load docs for creating MCP servers |
| List MCP resource templates | ✗ | cx | List parameterized resource templates |
| Dynamic MCP tools (bridged) | ✓ `mcp__{server}__{tool}` | cl, cx, cc | Auto-discovered tools from MCP servers |

### Scheduling & Remote

| Tool | ycode | Prior Art | Description |
|------|:-----:|-----------|-------------|
| Create cron job | ✓ `CronCreate` | cc, ow | Schedule recurring task with cron expression |
| Delete cron job | ✓ `CronDelete` | cc, ow | Remove scheduled task |
| List cron jobs | ✓ `CronList` | cc, ow | List all scheduled tasks |
| Remote trigger (webhook) | ✓ `RemoteTrigger` | cc | Trigger remote webhook endpoint |

### Memory & Persistence

| Tool | ycode | Prior Art | Description |
|------|:-----:|-----------|-------------|
| Save memory | ✓ `memory_save` | gc | Save user/feedback/project/reference memory |
| Recall memory (search) | ✓ `memory_recall` | — | Semantic/FTS/keyword memory search |
| Forget memory | ✓ `memory_forget` | — | Remove a memory by name |
| List all memories | ✓ `memory_list` | — | List memories with type/limit filter |
| Memory feedback (reward) | ✓ `memory_feedback` | — | Propagate reward signal to memory value score |
| Store memo (long-term) | ✓ `MemosStore` | — | Save memo with #tags to Memos backend |
| Search memos | ✓ `MemosSearch` | — | Search memos by content or tag |
| List memos | ✓ `MemosList` | — | List recent memos with pagination |
| Delete memo | ✓ `MemosDelete` | — | Delete a memo by ID |

### Skills & Rules

| Tool | ycode | Prior Art | Description |
|------|:-----:|-----------|-------------|
| Load/execute skill | ✓ `Skill` | cl, oc, gc, cc, co | Load named skill instructions or run builtin |
| List available skills | ✓ `skill_list` | — | Enumerate skills with optional query filter |
| Create persistent rule | ✓ `CreateRule` | cl, co | Create coding rule in .agents/ycode/rules/ |
| Request agent-requested rule | ✗ | co | Retrieve rule by name based on description |

### Notebook

| Tool | ycode | Prior Art | Description |
|------|:-----:|-----------|-------------|
| Edit notebook cell | ✓ `NotebookEdit` | oh, cc | Replace, insert, or delete notebook cells |
| Read notebook | ✓ `notebook_read` | — | Read notebook cells with optional outputs |
| Execute notebook cell | ✗ | oh | Run a notebook cell and return output |
| Create notebook | ✗ | — | Create new Jupyter notebook |

### Observability & Diagnostics

| Tool | ycode | Prior Art | Description |
|------|:-----:|-----------|-------------|
| Query tool execution metrics | ✓ `query_metrics` | — | Tool stats, failures, session summary, slow tools |
| Query OTEL trace spans | ✓ `query_traces` | — | Recent/slow/error spans, summary views |
| Query conversation logs | ✓ `query_logs` | — | Recent turns, errors, text search, cost summary |

### Configuration & Session

| Tool | ycode | Prior Art | Description |
|------|:-----:|-----------|-------------|
| Get/set config | ✓ `Config` | cc, ow | Get or set runtime configuration |
| Sleep (wait) | ✓ `Sleep` | cc | Wait for specified duration |

### Media & Generation (openclaw-specific)

| Tool | ycode | Prior Art | Description |
|------|:-----:|-----------|-------------|
| Generate image | ✗ | cx, ow | Generate images via AI model |
| Generate video | ✗ | ow | Generate video via AI model |
| Generate music | ✗ | ow | Generate music via AI model |
| Text-to-speech | ✗ | ow | Convert text to speech audio |
| Voice input (transcription) | ✗ | ai, cx, ow | Record and transcribe voice |

### Platform-Specific (openclaw)

| Tool | ycode | Prior Art | Description |
|------|:-----:|-----------|-------------|
| Messaging (Slack/Telegram) | ✗ | ow | Send messages across channels |
| Node control (IoT/desktop) | ✗ | ow | Discover/control paired nodes |
| Canvas (UI control) | ✗ | ow | Control node UI canvases |
| Gateway management | ✗ | ow | Restart/configure gateway |

### Miscellaneous

| Tool | ycode | Prior Art | Description |
|------|:-----:|-----------|-------------|
| Report bug (GitHub issue) | ✗ | cl | Open pre-filled GitHub issue |
| Explain diff (AI comments) | ✗ | cl | Generate inline comments on code changes |
| Update topic context | ✗ | gc | Update narrative context/summary |
| Internal docs | ✗ | gc | Retrieve built-in documentation files |
| Task tracker (full CRUD graph) | ✗ | gc | Create/update/query/visualize task dependency graph |
| Upload artifact | ✗ | co | Upload file to session artifacts |
| REPL (persistent session) | ✗ | cx, oh, cc | Persistent Python/JS REPL subprocess |
| IPython/Jupyter execution | ✗ | oh | Run IPython cells |

---

## Summary

### Tool Counts

| Category | ycode | Unique across prior art |
|----------|:-----:|:-----------------------:|
| File Operations | 20 | 14 |
| Shell & Execution | 2 (with sub-params) | 5 |
| Git (native) | 10 | 0 (others use bash) |
| Git Server | 5 | 0 |
| GitHub | 10 | 0 (others use `gh` CLI) |
| Web & Search | 2 | 4 |
| Browser | 8 | 10 |
| Agent & Orchestration | 3 | 9 |
| Worker | 9 | 9 |
| Team | 2 | 2 |
| Task Management | 7 | 7 |
| Tool Discovery | 1 | 2 |
| Planning | 2 | 5 |
| User Interaction | 5 | 8 |
| Code Intelligence | 9 | 11 |
| MCP | 4+dynamic | 6+dynamic |
| Scheduling | 4 | 4 |
| Memory | 9 | 1 |
| Skills & Rules | 3 | 4 |
| Notebook | 2 | 4 |
| Observability | 3 | 0 |
| Config/Session | 2 | 2 |
| **Total** | **~131** | **~107 unique** |

### ycode Unique Strengths (not in any prior art)

- Native git tools (10 subcommands) — all others use `bash` for git
- Embedded git server with worktrees for agent collaboration
- Native GitHub API tools (no `gh` CLI dependency)
- Observability tools (metrics, traces, logs)
- Memory system depth (9 tools: save/recall/forget/list/feedback + memos CRUD)
- File management tools (copy, move, delete, tree, file info)
- AST impact analysis (`find_impact`, `find_references`)
- Test runner with structured output parsing
- Swarm orchestration

### Remaining Gaps (not yet implemented)

**High value:**
- REPL (persistent Python/JS session) — cx, oh, cc
- Agent send message to running agent — cx, ow
- Repo map as callable tool — ai, co
- PowerShell — cx, cc

**Medium value:**
- LSP workspace symbols, call hierarchy, go-to-implementation
- GitHub issue create, PR merge, label management
- Notebook execute

**Low value / niche:**
- Media generation (image/video/music/TTS) — ow only
- Platform tools (Slack, nodes, canvas) — ow only
- Code search API (Exa) — oc only
- Voice input — ai, cx, ow
