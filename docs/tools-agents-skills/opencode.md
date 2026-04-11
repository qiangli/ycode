# OpenCode - Tools, Agents, Skills & Security Analysis

**Project:** OpenCode (AI coding agent CLI)
**Language:** TypeScript (Bun runtime)
**Repository:** opencode-ai/opencode

---

## Tools (Function Calling) - 19 tools

### File Operations (5)
| Tool | Description |
|------|-------------|
| `read` | Read files/directories (2000 lines default, offset/limit) |
| `write` | Create/overwrite files with LSP diagnostic feedback |
| `edit` | Find-replace with old/new string matching, replaceAll flag |
| `apply_patch` | Unified patch format with add/update/delete/move operations |
| `multiedit` | Sequential edit operations on single file |

### Search & Discovery (5)
| Tool | Description |
|------|-------------|
| `glob` | File pattern matching via ripgrep (100 results max) |
| `grep` | Regex content search via ripgrep with include filter |
| `list` | Directory tree with common dir exclusions |
| `websearch` | Web search via Exa AI (live crawl modes) |
| `codesearch` | Code documentation search via Exa MCP |

### Execution (2)
| Tool | Description |
|------|-------------|
| `bash` | Shell commands with timeout (default 2min), OS-aware |
| `task` | Launch subagents with permission inheritance |

### Code Intelligence (2)
| Tool | Description |
|------|-------------|
| `lsp` | LSP operations (definition, references, hover, symbols, call hierarchy) |
| `webfetch` | Fetch/convert web content (text/markdown/html, 5MB limit) |

### Interaction (2)
| Tool | Description |
|------|-------------|
| `question` | Multiple choice or open questions |
| `todowrite` | Session todo list management |

### Special (3)
| Tool | Description |
|------|-------------|
| `skill` | Load SKILL.md definitions with bundled resources |
| `plan_exit` | Transition from plan to build agent |
| `invalid` | Error handler for invalid tool calls |

---

## Agents / Subagents

### Primary Agents (2)
| Agent | Description | Mode |
|-------|-------------|------|
| **build** | Full-access development agent (default) | primary |
| **plan** | Read-only planning/analysis agent | primary |

### Subagents (2)
| Agent | Description | Mode |
|-------|-------------|------|
| **general** | General-purpose, parallel execution | subagent |
| **explore** | Fast codebase exploration (read-only tools) | subagent |

### Hidden Agents (3)
| Agent | Description |
|-------|-------------|
| **compaction** | Session compaction |
| **title** | Session title generation (temp: 0.5) |
| **summary** | Session summary generation |

### Custom Agents
User-defined via config with: name, description, permission, model, prompt, temperature, topP, color, options, steps.

---

## Skills

| Component | Description |
|-----------|-------------|
| **Format** | `SKILL.md` with YAML frontmatter (name, description) |
| **Locations** | `~/.claude/skills/`, `~/.agents/skills/`, `.opencode/skills/`, `.opencode/plans/`, plugins |
| **Loading** | Bundled with up to 10 related files |
| **Dedup** | First-wins on duplicate names |

---

## Security & Guardrails

### Permission System
| Feature | Description |
|---------|-------------|
| **Actions** | allow, deny, ask |
| **Rules** | `{permission, pattern, action}` with wildcard matching |
| **Scopes** | bash, edit, read, write, glob, grep, list, webfetch, websearch, codesearch, question, task, skill, lsp, todowrite, plan_enter/exit, doom_loop, external_directory |
| **Evaluation** | Most-specific match wins (last match) |
| **User responses** | once (single), always (permanent), reject (deny all) |

### Bash Command Security
| Feature | Description |
|---------|-------------|
| **Tree-sitter AST** | Parses bash commands for safety analysis |
| **140+ commands** | Recognized and categorized by arity |
| **File ops detection** | rm, cp, mv, mkdir, touch, chmod, chown, etc. |
| **PowerShell** | Get-Content, Copy-Item, Remove-Item equivalents |
| **Pattern permissions** | Prompts for file path patterns |

### External Directory Protection
| Feature | Description |
|---------|-------------|
| **Boundary enforcement** | Operations must stay within project directory |
| **Symlink blocking** | Prevents symlinks to parent directories |
| **Whitelist** | Skill dirs, plan dirs, truncation dirs |

### Additional Security
| Mechanism | Description |
|-----------|-------------|
| **Default rules** | Everything allowed, but doom_loop/external_directory ask, .env files ask |
| **Output truncation** | Line/byte limits, full output saved to file |
| **Long line truncation** | 2000 char max per line |
| **Process timeouts** | Bash 2min default, web 2min max |
| **Memory limits** | Stream-based, chunk-based output |
| **Zod validation** | Schema validation for all tool parameters |

---

## Notable Patterns

- **Effect-based architecture:** Typed composable async operations via Effect library
- **Plugin system:** Hooks with trigger patterns, built-in auth plugins (Codex, Copilot, Gitlab, Poe, Cloudflare)
- **Bus service:** Async event publishing for permission events and session events
- **InstanceState:** Per-project cleanup semantics with scoped cache
- **Conditional tools:** Edit vs apply_patch based on model capability

---

## Gap Analysis vs ycode

| Feature | ycode Status | Priority |
|---------|-------------|----------|
| Bash AST analysis (tree-sitter) | Not implemented | **High** - superior to regex |
| `multiedit` tool (chained edits) | Not implemented | **Medium** |
| `codesearch` (code-specific web search) | Not implemented | Low - niche |
| External directory protection | Partial (VFS validation) | **Medium** - strengthen |
| Doom loop detection | Not implemented | **Medium** - safety |
| `.env` file protection | Not implemented | **Medium** - security |
| LSP diagnostic feedback after writes | Not implemented | **Medium** - quality |
| Custom agent definitions (config) | Not implemented | **Medium** |
| `plan_exit` tool (agent transition) | Implemented (ExitPlanMode) | Done |
| Output truncation to file | Not implemented | Low |
| Auth plugins (Copilot, Gitlab, etc.) | Not implemented | Low |
