# Agentic Tools Cross-Project Comparison

> Comprehensive comparison of built-in tools across 10 agentic coding projects in `priorart/`.
> Generated 2026-05-01.

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

**Legend:** ✓ = has the tool/capability, ✗ = does not

---

## 1. File Operations

| Tool | aider | cline | codex | opencode | openhands | geminicli | clawcode | openclaw | continue | kimicli | Description |
|------|:-----:|:-----:|:-----:|:--------:|:---------:|:---------:|:--------:|:--------:|:--------:|:-------:|-------------|
| **Read File** | ✗ | ✓ | ✗ | ✓ | ✓ | ✓ | ✓ | ✓ | ✓ | ✓ | Read text file contents, optional line range |
| **Read Many Files** | ✗ | ✗ | ✗ | ✗ | ✗ | ✓ | ✗ | ✗ | ✗ | ✗ | Read/concat multiple files by glob pattern |
| **Read Media File** | ✗ | ✗ | ✓ | ✗ | ✗ | ✓ | ✗ | ✓ | ✗ | ✓ | Read image/audio/video files |
| **Write File** | ✓ | ✓ | ✗ | ✓ | ✓ | ✓ | ✓ | ✓ | ✓ | ✓ | Create or overwrite a file |
| **Edit File** | ✓ | ✓ | ✗ | ✓ | ✓ | ✓ | ✓ | ✓ | ✓ | ✓ | Find-and-replace text in a file |
| **Apply Patch** | ✗ | ✓ | ✓ | ✓ | ✗ | ✗ | ✗ | ✓ | ✗ | ✗ | Apply unified/custom diff patch to files |
| **Glob / Find Files** | ✗ | ✗ | ✗ | ✓ | ✓ | ✓ | ✓ | ✓ | ✓ | ✓ | Find files by glob/name pattern |
| **Grep / Search** | ✗ | ✓ | ✗ | ✓ | ✓ | ✓ | ✓ | ✓ | ✓ | ✓ | Regex search across file contents |
| **List Directory** | ✗ | ✓ | ✓ | ✗ | ✗ | ✓ | ✗ | ✓ | ✓ | ✗ | List files/subdirs in a directory |
| **List Code Definitions** | ✗ | ✓ | ✗ | ✗ | ✗ | ✗ | ✗ | ✗ | ✗ | ✗ | List top-level symbols (classes, funcs) in files |

> *Note: aider uses text-based edit formats (search/replace blocks) rather than structured function-call tools. Its `write_file` and `replace_lines` function tools exist but are deprecated.*

---

## 2. Shell & Execution

| Tool | aider | cline | codex | opencode | openhands | geminicli | clawcode | openclaw | continue | kimicli | Description |
|------|:-----:|:-----:|:-----:|:--------:|:---------:|:---------:|:--------:|:--------:|:--------:|:-------:|-------------|
| **Bash / Shell** | ✓ | ✓ | ✓ | ✓ | ✓ | ✓ | ✓ | ✓ | ✓ | ✓ | Execute shell commands |
| **Interactive PTY** | ✗ | ✗ | ✓ | ✗ | ✗ | ✗ | ✗ | ✗ | ✗ | ✗ | Write stdin to ongoing PTY session |
| **Process Management** | ✗ | ✗ | ✗ | ✗ | ✗ | ✗ | ✗ | ✓ | ✓ | ✗ | List/poll/kill background processes |
| **PowerShell** | ✗ | ✗ | ✓ | ✗ | ✗ | ✗ | ✓ | ✗ | ✗ | ✗ | Windows shell execution |
| **Request Permissions** | ✗ | ✗ | ✓ | ✗ | ✗ | ✗ | ✗ | ✗ | ✗ | ✗ | Request additional FS/network permissions |

---

## 3. Web & Search

| Tool | aider | cline | codex | opencode | openhands | geminicli | clawcode | openclaw | continue | kimicli | Description |
|------|:-----:|:-----:|:-----:|:--------:|:---------:|:---------:|:--------:|:--------:|:--------:|:-------:|-------------|
| **Web Search** | ✗ | ✓ | ✓ | ✓ | ✗ | ✓ | ✓ | ✓ | ✓ | ✓ | Search the web for information |
| **Web Fetch** | ✓ | ✓ | ✗ | ✓ | ✗ | ✓ | ✓ | ✓ | ✓ | ✓ | Fetch URL and extract content as markdown |
| **Code Search (API)** | ✗ | ✗ | ✗ | ✓ | ✗ | ✗ | ✗ | ✗ | ✗ | ✗ | Search programming docs/APIs via Exa Code |
| **Browser Automation** | ✗ | ✓ | ✗ | ✗ | ✓ | ✗ | ✗ | ✓ | ✗ | ✗ | Control a browser (click, type, navigate, screenshot) |

---

## 4. Agent & Orchestration

| Tool | aider | cline | codex | opencode | openhands | geminicli | clawcode | openclaw | continue | kimicli | Description |
|------|:-----:|:-----:|:-----:|:--------:|:---------:|:---------:|:--------:|:--------:|:--------:|:-------:|-------------|
| **Spawn Subagent** | ✗ | ✓ | ✓ | ✓ | ✗ | ✓ | ✓ | ✓ | ✓ | ✓ | Launch a sub-agent for a focused task |
| **Send Message to Agent** | ✗ | ✗ | ✓ | ✗ | ✗ | ✗ | ✗ | ✓ | ✗ | ✗ | Send input/message to an existing agent |
| **Wait for Agent** | ✗ | ✗ | ✓ | ✗ | ✗ | ✗ | ✗ | ✓ | ✗ | ✗ | Block until agent(s) complete |
| **List Agents** | ✗ | ✗ | ✓ | ✗ | ✗ | ✗ | ✗ | ✓ | ✗ | ✗ | List live agents in current tree |
| **Close/Kill Agent** | ✗ | ✗ | ✓ | ✗ | ✗ | ✗ | ✗ | ✓ | ✗ | ✗ | Terminate a running sub-agent |
| **CSV Batch Agents** | ✗ | ✗ | ✓ | ✗ | ✗ | ✗ | ✗ | ✗ | ✗ | ✗ | Spawn one agent per CSV row |
| **Team Create/Delete** | ✗ | ✗ | ✗ | ✗ | ✗ | ✗ | ✓ | ✗ | ✗ | ✗ | Create/delete a team of parallel agents |
| **Tool Discovery** | ✗ | ✗ | ✓ | ✗ | ✗ | ✗ | ✓ | ✗ | ✗ | ✗ | Search deferred tools by keyword/name |
| **Tool Suggest** | ✗ | ✗ | ✓ | ✗ | ✗ | ✗ | ✗ | ✗ | ✗ | ✗ | Suggest missing connector/plugin to install |

---

## 5. Planning & Workflow

| Tool | aider | cline | codex | opencode | openhands | geminicli | clawcode | openclaw | continue | kimicli | Description |
|------|:-----:|:-----:|:-----:|:--------:|:---------:|:---------:|:--------:|:--------:|:--------:|:-------:|-------------|
| **Todo / Checklist** | ✗ | ✗ | ✓ | ✓ | ✓ | ✓ | ✓ | ✓ | ✓ | ✓ | Manage structured task/todo list |
| **Enter Plan Mode** | ✗ | ✓ | ✗ | ✓ | ✗ | ✓ | ✓ | ✗ | ✗ | ✓ | Switch to read-only planning phase |
| **Exit Plan Mode** | ✗ | ✓ | ✗ | ✓ | ✗ | ✓ | ✓ | ✗ | ✗ | ✓ | Finalize plan and start implementation |
| **Update Plan** | ✗ | ✗ | ✓ | ✗ | ✗ | ✗ | ✗ | ✓ | ✗ | ✗ | Update step-by-step plan with statuses |
| **Goal Management** | ✗ | ✗ | ✓ | ✗ | ✗ | ✗ | ✗ | ✗ | ✗ | ✗ | Create/get/update goal with budgets |
| **Task Status** | ✗ | ✗ | ✗ | ✗ | ✗ | ✗ | ✗ | ✗ | ✓ | ✗ | Set status (PLANNING/WORKING/DONE/BLOCKED) |

---

## 6. User Interaction & Session Control

| Tool | aider | cline | codex | opencode | openhands | geminicli | clawcode | openclaw | continue | kimicli | Description |
|------|:-----:|:-----:|:-----:|:--------:|:---------:|:---------:|:--------:|:--------:|:--------:|:-------:|-------------|
| **Ask User Question** | ✗ | ✓ | ✓ | ✓ | ✗ | ✓ | ✓ | ✗ | ✓ | ✓ | Ask clarifying question with options |
| **Attempt Completion** | ✗ | ✓ | ✗ | ✗ | ✓ | ✓ | ✗ | ✗ | ✗ | ✗ | Signal task is complete with result |
| **New Task/Session** | ✗ | ✓ | ✗ | ✗ | ✗ | ✗ | ✗ | ✗ | ✗ | ✗ | Start a new task session with context |
| **Send Message to User** | ✗ | ✗ | ✗ | ✗ | ✗ | ✗ | ✓ | ✓ | ✗ | ✗ | Proactively send a message to user |
| **Think** | ✗ | ✗ | ✗ | ✗ | ✓ | ✗ | ✗ | ✗ | ✗ | ✓ | Log reasoning step without action |
| **Context Revert** | ✗ | ✗ | ✗ | ✗ | ✗ | ✗ | ✗ | ✗ | ✗ | ✓ | Revert context to earlier checkpoint |
| **Report Failure** | ✗ | ✗ | ✗ | ✗ | ✗ | ✗ | ✗ | ✗ | ✓ | ✗ | Report unrecoverable task failure |
| **Condense Context** | ✗ | ✓ | ✗ | ✗ | ✓ | ✗ | ✗ | ✗ | ✗ | ✗ | Compress/summarize conversation context |

---

## 7. Code Intelligence

| Tool | aider | cline | codex | opencode | openhands | geminicli | clawcode | openclaw | continue | kimicli | Description |
|------|:-----:|:-----:|:-----:|:--------:|:---------:|:---------:|:--------:|:--------:|:--------:|:-------:|-------------|
| **LSP Integration** | ✗ | ✗ | ✗ | ✓ | ✗ | ✗ | ✓ | ✗ | ✗ | ✗ | Query language server (definition, refs, hover) |
| **Repo Map** | ✓ | ✗ | ✗ | ✗ | ✗ | ✗ | ✗ | ✗ | ✓ | ✗ | Generate token-budgeted symbol overview |
| **Semantic Code Search** | ✗ | ✗ | ✗ | ✗ | ✗ | ✗ | ✗ | ✗ | ✓ | ✗ | Natural language codebase search |
| **Lint & Fix** | ✓ | ✗ | ✗ | ✗ | ✗ | ✗ | ✗ | ✗ | ✗ | ✗ | Run linter and auto-fix errors |
| **View Diff** | ✗ | ✗ | ✗ | ✗ | ✗ | ✗ | ✗ | ✗ | ✓ | ✗ | Show uncommitted working changes |

---

## 8. MCP & External Integration

| Tool | aider | cline | codex | opencode | openhands | geminicli | clawcode | openclaw | continue | kimicli | Description |
|------|:-----:|:-----:|:-----:|:--------:|:---------:|:---------:|:--------:|:--------:|:--------:|:-------:|-------------|
| **Use MCP Tool** | ✗ | ✓ | ✗ | ✗ | ✓ | ✗ | ✓ | ✗ | ✗ | ✗ | Call a tool from an MCP server |
| **List MCP Resources** | ✗ | ✗ | ✓ | ✗ | ✗ | ✓ | ✓ | ✗ | ✗ | ✗ | List resources from MCP servers |
| **Read MCP Resource** | ✗ | ✗ | ✓ | ✗ | ✗ | ✓ | ✓ | ✗ | ✗ | ✗ | Read a specific MCP resource by URI |
| **MCP Auth** | ✗ | ✗ | ✗ | ✗ | ✗ | ✗ | ✓ | ✗ | ✗ | ✗ | Authenticate with MCP server (OAuth) |
| **MCP Docs** | ✗ | ✓ | ✗ | ✗ | ✗ | ✗ | ✗ | ✗ | ✗ | ✗ | Load docs for creating/installing MCP servers |
| **Cron / Scheduling** | ✗ | ✗ | ✗ | ✗ | ✗ | ✗ | ✓ | ✓ | ✗ | ✗ | Create/manage recurring scheduled tasks |
| **Remote Trigger** | ✗ | ✗ | ✗ | ✗ | ✗ | ✗ | ✓ | ✗ | ✗ | ✗ | Trigger remote webhook endpoint |
| **Messaging (Slack/etc)** | ✗ | ✗ | ✗ | ✗ | ✗ | ✗ | ✗ | ✓ | ✗ | ✗ | Send messages across channels |

---

## 9. Media & Specialized

| Tool | aider | cline | codex | opencode | openhands | geminicli | clawcode | openclaw | continue | kimicli | Description |
|------|:-----:|:-----:|:-----:|:--------:|:---------:|:---------:|:--------:|:--------:|:--------:|:-------:|-------------|
| **View/Analyze Image** | ✗ | ✗ | ✓ | ✗ | ✗ | ✗ | ✗ | ✓ | ✗ | ✗ | View local image file with vision model |
| **Generate Image** | ✗ | ✗ | ✓ | ✗ | ✗ | ✗ | ✗ | ✓ | ✗ | ✗ | Generate images via AI model |
| **Generate Video** | ✗ | ✗ | ✗ | ✗ | ✗ | ✗ | ✗ | ✓ | ✗ | ✗ | Generate video via AI model |
| **Generate Music** | ✗ | ✗ | ✗ | ✗ | ✗ | ✗ | ✗ | ✓ | ✗ | ✗ | Generate music via AI model |
| **Text-to-Speech** | ✗ | ✗ | ✗ | ✗ | ✗ | ✗ | ✗ | ✓ | ✗ | ✗ | Convert text to speech audio |
| **PDF Analysis** | ✗ | ✗ | ✗ | ✗ | ✗ | ✗ | ✗ | ✓ | ✗ | ✗ | Analyze PDF documents |
| **Voice Input** | ✓ | ✗ | ✓ | ✗ | ✗ | ✗ | ✗ | ✓ | ✗ | ✗ | Record and transcribe voice |
| **Notebook Edit** | ✗ | ✗ | ✗ | ✗ | ✓ | ✗ | ✓ | ✗ | ✗ | ✗ | Edit Jupyter notebook cells |
| **REPL** | ✗ | ✗ | ✓ | ✗ | ✓ | ✗ | ✓ | ✗ | ✗ | ✗ | Execute code in REPL subprocess |
| **Memory / Persistence** | ✗ | ✗ | ✗ | ✗ | ✗ | ✓ | ✗ | ✗ | ✗ | ✗ | Save user facts/prefs across sessions |
| **Skill Activation** | ✗ | ✓ | ✗ | ✓ | ✗ | ✓ | ✓ | ✗ | ✓ | ✗ | Load named skill instructions |
| **Internal Docs** | ✗ | ✗ | ✗ | ✗ | ✗ | ✓ | ✗ | ✗ | ✗ | ✗ | Retrieve built-in documentation |
| **Config** | ✗ | ✗ | ✗ | ✗ | ✗ | ✗ | ✓ | ✓ | ✗ | ✗ | Get/set agent settings |
| **Upload Artifact** | ✗ | ✗ | ✗ | ✗ | ✗ | ✗ | ✗ | ✗ | ✓ | ✗ | Upload file to session artifacts |
| **Report Bug** | ✗ | ✓ | ✗ | ✗ | ✗ | ✗ | ✗ | ✗ | ✗ | ✗ | Open pre-filled GitHub issue |
| **Task Tracker (full)** | ✗ | ✗ | ✗ | ✗ | ✗ | ✓ | ✗ | ✗ | ✗ | ✗ | Create/update/query/visualize task graph |
| **Explain Diff** | ✗ | ✓ | ✗ | ✗ | ✗ | ✗ | ✗ | ✗ | ✗ | ✗ | Generate AI comments on code changes |
| **Create Rule** | ✗ | ✓ | ✗ | ✗ | ✗ | ✗ | ✗ | ✗ | ✓ | ✗ | Create persistent coding rule file |
| **Update Topic** | ✗ | ✗ | ✗ | ✗ | ✗ | ✓ | ✗ | ✗ | ✗ | ✗ | Update narrative context/summary |
| **Node Control** | ✗ | ✗ | ✗ | ✗ | ✗ | ✗ | ✗ | ✓ | ✗ | ✗ | Discover/control paired IoT/desktop nodes |
| **Canvas** | ✗ | ✗ | ✗ | ✗ | ✗ | ✗ | ✗ | ✓ | ✗ | ✗ | Control node UI canvases |

---

## Summary: Tool Counts by Project

| Project | File Ops | Shell | Web | Agent | Planning | Interaction | Code Intel | MCP/Ext | Media/Special | **Total** |
|---------|:--------:|:-----:|:---:|:-----:|:--------:|:-----------:|:----------:|:-------:|:-------------:|:---------:|
| **aider** | 2 | 1 | 1 | 0 | 0 | 0 | 2 | 0 | 1 | **~7** |
| **cline** | 6 | 1 | 2 | 1 | 1 | 4 | 1 | 3 | 4 | **~23** |
| **codex** | 1 | 4 | 1 | 8 | 3 | 1 | 0 | 3 | 2 | **~23** |
| **opencode** | 4 | 1 | 2 | 1 | 2 | 1 | 1 | 0 | 1 | **~13** |
| **openhands** | 3 | 1 | 0 | 0 | 1 | 3 | 0 | 1 | 2 | **~11** |
| **geminicli** | 6 | 1 | 2 | 1 | 4 | 1 | 0 | 2 | 3 | **~20** |
| **clawcode** | 4 | 2 | 2 | 2 | 2 | 1 | 1 | 5 | 3 | **~22** |
| **openclaw** | 5 | 2 | 2 | 4 | 1 | 1 | 0 | 1 | 8 | **~24** |
| **continue** | 6 | 2 | 2 | 1 | 1 | 2 | 3 | 0 | 3 | **~20** |
| **kimicli** | 5 | 1 | 2 | 1 | 2 | 2 | 0 | 0 | 0 | **~13** |

---

## Key Observations

1. **File ops + shell are universal** -- every project has at least read/write/edit/bash as the foundational tool set.

2. **Web search/fetch is near-universal** -- only openhands lacks both; all others provide at least one.

3. **Subagent spawning is now standard** -- 8/10 projects support it, with codex and openclaw having the richest orchestration APIs (spawn, message, wait, list, close).

4. **Plan mode is gaining adoption** -- 6/10 projects implement enter/exit plan mode as a structured planning phase.

5. **MCP integration varies widely** -- clawcode has the deepest support (5 tools including auth), while half the projects have no explicit MCP tools.

6. **Browser automation remains niche** -- only 3/10 projects (cline, openhands, openclaw) include browser control tools.

7. **openclaw is the most feature-rich** -- unique media generation tools (video, music, TTS, canvas, node control) push it beyond pure coding into a multi-modal agent platform.

8. **aider is the most minimalist** -- relies on text-based edit formats rather than structured tool calls; most capabilities are internal (repo map, linter, web scraper) rather than LLM-callable tools.

9. **Tool discovery (deferred tools)** -- only codex and clawcode implement lazy tool loading via a `tool_search` mechanism to keep the default tool set lean.

10. **Code intelligence is underserved** -- only 2/10 projects (opencode, clawcode) expose LSP as an LLM-callable tool; aider and continue use repo maps but as context injection rather than callable tools.

---

## Per-Project Tool Details

### aider

LLM function-call tools (deprecated):
- `write_file` -- create/update files (deprecated; text-based edit format used instead)
- `replace_lines` -- find/replace lines in files (deprecated)

Internal capabilities (not LLM-callable but part of the agent):
- Shell runner (`/run`, `/test`, `/git`)
- Web scraper (`/web`) -- Playwright or HTTP, HTML-to-markdown
- Linter -- auto-lint and feed errors to LLM
- Repo map -- tree-sitter-based symbol map
- Voice input -- OpenAI Whisper transcription

### cline

27 tools total (including internal-only):
- **File:** `read_file`, `write_to_file`, `replace_in_file`, `apply_patch`, `list_files`, `list_code_definition_names`, `search_files`, `new_rule`
- **Shell:** `execute_command`
- **Browser:** `browser_action` (launch, click, type, scroll, close)
- **Web:** `web_fetch`, `web_search`
- **MCP:** `use_mcp_tool`, `access_mcp_resource`, `load_mcp_documentation`
- **Agent:** `plan_mode_respond`, `act_mode_respond`, `ask_followup_question`, `attempt_completion`, `new_task`, `use_subagents`, `use_skill`
- **Special:** `generate_explanation`, `focus_chain`, `condense`, `summarize_task`, `report_bug`

### codex

36 tools total:
- **Shell:** `exec_command`, `write_stdin`, `shell`, `shell_command`, `request_permissions`
- **File:** `apply_patch`
- **Agent:** `spawn_agent`, `send_input`, `send_message`, `followup_task`, `resume_agent`, `wait_agent`, `list_agents`, `close_agent`, `spawn_agents_on_csv`, `report_agent_job_result`
- **Goal:** `get_goal`, `create_goal`, `update_goal`
- **Plan:** `update_plan`
- **User:** `request_user_input`
- **Utility:** `list_dir`, `view_image`
- **Code:** `exec` (code mode), `wait` (code mode)
- **Discovery:** `tool_search`, `tool_suggest`
- **MCP:** `list_mcp_resources`, `list_mcp_resource_templates`, `read_mcp_resource`
- **API:** `local_shell`, `image_generation`, `web_search`
- **Realtime:** `background_agent`, `remain_silent`

### opencode

17 tools total:
- **File:** `read`, `write`, `edit`, `grep`, `glob`, `apply_patch`
- **Shell:** `bash`
- **Web:** `webfetch`, `websearch`, `codesearch`
- **Agent:** `task`
- **Planning:** `todowrite`, `plan_exit`
- **Interaction:** `question`
- **Code Intel:** `lsp`
- **Skill:** `skill`
- **Internal:** `invalid`

### openhands

21 V1 action types:
- **Shell:** `ExecuteBashAction`, `TerminalAction`
- **File:** `FileEditorAction`, `StrReplaceEditorAction`, `PlanningFileEditorAction`
- **Search:** `GlobAction`, `GrepAction`
- **Browser:** `BrowserNavigateAction`, `BrowserClickAction`, `BrowserTypeAction`, `BrowserGetStateAction`, `BrowserGetContentAction`, `BrowserScrollAction`, `BrowserGoBackAction`, `BrowserListTabsAction`, `BrowserSwitchTabAction`, `BrowserCloseTabAction`
- **Agent:** `ThinkAction`, `FinishAction`, `TaskTrackerAction`
- **MCP:** `MCPToolAction`

### geminicli

28 tools total:
- **File:** `read_file`, `write_file`, `replace`, `glob`, `list_directory`, `grep_search`, `read_many_files`
- **Shell:** `run_shell_command`
- **Web:** `google_web_search`, `web_fetch`
- **Memory:** `save_memory`
- **Planning:** `write_todos`, `enter_plan_mode`, `exit_plan_mode`, `update_topic`, `complete_task`
- **Docs:** `get_internal_docs`, `activate_skill`
- **Agent:** `invoke_agent`
- **User:** `ask_user`
- **Tracker:** `tracker_create_task`, `tracker_update_task`, `tracker_get_task`, `tracker_list_tasks`, `tracker_add_dependency`, `tracker_visualize`
- **MCP:** `read_mcp_resource`, `list_mcp_resources`

### clawcode

53 named tool specs:
- **Core (always-present):** `bash`, `read_file`, `write_file`, `edit_file`, `glob_search`, `grep_search`
- **Web:** `WebFetch`, `WebSearch`
- **Agent:** `Agent`, `ToolSearch`, `Skill`, `SendUserMessage`/`Brief`, `AskUserQuestion`
- **Task:** `TaskCreate`, `RunTaskPacket`, `TaskGet`, `TaskList`, `TaskStop`, `TaskUpdate`, `TaskOutput`
- **Worker:** `WorkerCreate`, `WorkerGet`, `WorkerObserve`, `WorkerResolveTrust`, `WorkerAwaitReady`, `WorkerSendPrompt`, `WorkerRestart`, `WorkerTerminate`, `WorkerObserveCompletion`
- **Team:** `TeamCreate`, `TeamDelete`
- **Cron:** `CronCreate`, `CronDelete`, `CronList`
- **Code Intel:** `LSP`
- **MCP:** `ListMcpResources`, `ReadMcpResource`, `McpAuth`, `MCP`, `RemoteTrigger`
- **Notebook:** `NotebookEdit`, `REPL`, `PowerShell`
- **Session:** `TodoWrite`, `Config`, `EnterPlanMode`, `ExitPlanMode`, `Sleep`, `StructuredOutput`

### openclaw

33 tools total:
- **Core Coding:** `read`, `write`, `edit`, `ls`, `grep`, `find`
- **Shell:** `exec`, `process`, `apply_patch`
- **Platform:** `message`, `cron`, `nodes`, `canvas`, `gateway`, `tts`, `image`, `image_generate`, `video_generate`, `music_generate`, `pdf`, `web_search`, `web_fetch`
- **Agent:** `agents_list`, `sessions_list`, `sessions_history`, `sessions_send`, `sessions_spawn`, `sessions_yield`, `session_status`, `subagents`
- **Planning:** `update_plan`
- **Browser:** `browser` (via extension)
- **Voice:** `openclaw_agent_consult`

### continue

VS Code extension (20 tools) + CLI (18 tools):
- **File (VS Code):** `read_file`, `read_file_range`, `edit_existing_file`, `single_find_and_replace`, `multi_edit`, `read_currently_open_file`, `create_new_file`, `grep_search`, `file_glob_search`, `ls`, `list_code_definition_names`
- **File (CLI):** `Read`, `Write`, `Edit`, `MultiEdit`, `List`, `Search`
- **Shell:** `run_terminal_command` / `Bash`, `CheckBackgroundJob`
- **Web:** `search_web`, `fetch_url_content` / `Fetch`
- **Agent:** `Subagent`
- **Planning:** `Checklist`, `Status`
- **Interaction:** `AskQuestion`, `Exit`, `ReportFailure`
- **Special:** `view_diff`, `create_rule_block`, `request_rule`, `codebase`, `read_skill`, `view_repo_map`, `view_subdirectory`, `Skills`, `UploadArtifact`

### kimicli

18 tools total:
- **File:** `ReadFile`, `ReadMediaFile`, `WriteFile`, `StrReplaceFile`, `Glob`, `Grep`
- **Shell:** `Shell`
- **Web:** `FetchURL`, `SearchWeb`
- **Agent:** `Agent`
- **Background:** `TaskList`, `TaskOutput`, `TaskStop`
- **Planning:** `EnterPlanMode`, `ExitPlanMode`, `SetTodoList`
- **Interaction:** `AskUserQuestion`, `Think`
- **Context:** `SendDMail` (context revert to checkpoint)
