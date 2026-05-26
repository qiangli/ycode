---
topic: memex
summary: persistent agent memory — save / recall / list / forget
when: persist a fact across sessions, or look up what was previously saved
audience: agent
max_lines: 90
---

Memex is ycode's persistent agent memory. Each entry has a `name`,
`description`, `type` (one of: user, feedback, project, reference,
episodic, procedural, task), and a `scope` (project or user). Memory
survives across sessions and is searchable by RRF-fused semantic
recall.

## When to use this

- The user tells you something durable about themselves, the project,
  or a pattern they want repeated: save it.
- You're about to make a decision and want to check whether the user
  already told you a preference for this case: recall it.
- You're orienting in an unfamiliar project: read the index.

## Tool surface

- `memex_save` — persist or overwrite a memory by name. Required:
  `name`, `description`, `type`, plus body content.
- `memex_recall` — semantic search. Pass a query string; backend
  fuses results across stores.
- `search_memex` — like recall but with optional backend selection +
  post-filter.
- `memex_list` — every memory across all scopes (heavy; prefer
  recall when you have a query).
- `memex_forget` — remove by name. Irreversible.
- `memex_index` — read `MEMORY.md` verbatim (the curated index file).
- `list_memory_types` — return the seven canonical types (useful for
  validating `type` before save).

Shell-side: `yc remember "<text>"` and `yc recall <query>` are
ergonomic wrappers around save and recall respectively.

## Failure modes

| Symptom | Fix |
|---|---|
| `unknown memory type` | Call `list_memory_types` and use one of those values. |
| Recall returns empty | The fact may not be saved yet; or the query is too narrow. Try `memex_list` once to confirm. |
| Save overwrites unexpectedly | `name` is the primary key; saves with an existing name overwrite. Pick a distinct name. |
| Wrong scope | `scope=user` lives in the user's home; `scope=project` lives next to the cwd. Pick deliberately. |

## Exact calls

- Save: MCP `memex_save` with `{name, description, type, body, scope?}`.
- Quick save from shell: `yc remember "<fact>" --name=<id> --type=project`.
- Recall: MCP `memex_recall` with `{query, limit?}`.
- Quick recall from shell: `yc recall "<query>" --limit=5`.
- Read the index: MCP `memex_index` with `{}`, or just read
  `~/.claude/projects/<project>/memory/MEMORY.md` directly when fs
  access is faster than MCP roundtripping.
- Forget: MCP `memex_forget` with `{name}`.
