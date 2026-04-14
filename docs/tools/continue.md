# Continue - Tools & Security Analysis

**Project:** Continue (AI-powered IDE extension)
**Language:** TypeScript
**Repository:** continuedev/continue

---

## Tools (Function Calling) - 20+ tools

### File Operations (7)
| Tool | Description | Read-only |
|------|-------------|-----------|
| `read_file` | Read file at path | Yes |
| `read_file_range` | Read specific line ranges | Yes |
| `read_currently_open_file` | Read active editor file | Yes |
| `create_new_file` | Create new files | No |
| `edit_existing_file` | Single edit to file | No |
| `single_find_and_replace` | Find-and-replace | No |
| `multi_edit` | Multiple sequential edits (agent-model only) | No |

### Search & Navigation (7)
| Tool | Description | Read-only |
|------|-------------|-----------|
| `file_glob_search` | File pattern matching | Yes |
| `grep_search` | Content search | Yes |
| `codebase` | Semantic codebase search (experimental) | Yes |
| `view_repo_map` | Repository structure overview (experimental) | Yes |
| `view_subdirectory` | Directory contents | Yes |
| `ls` | List directory | Yes |
| `view_diff` | Git diff of changes | Yes |

### External Integration (2)
| Tool | Description |
|------|-------------|
| `search_web` | Web search (signed-in users only) |
| `fetch_url_content` | Fetch and parse URL content |

### Configuration & Context (3)
| Tool | Description |
|------|-------------|
| `request_rule` | Retrieve agent-requested rules |
| `read_skill` | Read reusable skill content |
| `create_rule_block` | Create persistent code standards |

### Execution (1)
| Tool | Description |
|------|-------------|
| `run_terminal_command` | Shell commands with security evaluation |

---

## Security & Guardrails

| Mechanism | Description |
|-----------|-------------|
| **Permission Manager** | Event-based tool permission requests with "remember decision" |
| **Permission Checker** | YAML-based policy evaluation |
| **File Access Policy** | Workspace boundary enforcement, external files always need permission |
| **Tool Policy Levels** | disabled, allowedWithoutPermission, allowedWithPermission |
| **Terminal security** | `@continuedev/terminal-security` package for command evaluation |
| **Shell sanitization** | `shell-quote` library prevents injection (`;`, `&&`, `$()`, backticks) |
| **GitHub URL validation** | Rejects traversal, injection metacharacters |
| **Tool call validation** | Schema coercion, safe argument parsing, MCP timeouts (20s) |
| **Error handling** | ContinueError with reason codes, abort signal support |

---

## Notable Patterns

- **Configuration-dependent tools:** Tools conditionally included based on user state and model
- **Indexing service:** Codebase indexing for semantic search

---

## Gap Analysis vs ycode

| Feature | ycode Status | Priority |
|---------|-------------|----------|
| Semantic codebase search | Not implemented | **High** - powerful for large repos |
| Repository map view | Not implemented | **Medium** - useful context |
| `view_diff` (git diff tool) | Not implemented | **Medium** - useful for code review |
| Shell argument sanitization | Not implemented | **Medium** - security |
| Terminal security evaluation | Not implemented | **Medium** |
| `multi_edit` for agent models | Not implemented | Low |
