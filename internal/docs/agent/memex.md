---
topic: memex
summary: persistent agent memory — save / recall / list / forget
when: persist a fact across sessions, or look up what was previously saved
audience: agent
max_lines: 90
---

Memex is ycode's persistent agent memory. Each entry has a `name`,
`description`, `type` (one of: user, feedback, project, reference,
episodic, procedural, task), and a `scope` (`project` | `user` |
`global` | `team`; default `project`). Memory survives across sessions
and is searchable by RRF-fused semantic recall.

## When to use this

- The user tells you something durable about themselves, the project,
  or a pattern they want repeated: save it.
- You're about to make a decision and want to check whether the user
  already told you a preference for this case: recall it.
- You're orienting in an unfamiliar project: read the index.

## Tool surface

- `memex_save` — persist or overwrite a memory by name. Required:
  `name`, `type`, `description`, `content`. Optional: `scope` (default
  `project`), `importance` (0.0–1.0, default 0.5), `tags` (array).
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
| Wrong scope | `scope=project` writes under `<cwd>/.agents/ycode/memory/`; `global`, `user`, `team` write under `~/.agents/ycode/memory/`. Pick deliberately. |

## Exact calls

- Save: MCP `memex_save` with `{name, type, description, content, scope?, importance?, tags?}`.
- Quick save from shell: `yc remember "<fact>" --name=<id> --type=reference`.
- Recall: MCP `memex_recall` with `{query, max_results?, scope?}`.
- Quick recall from shell: `yc recall "<query>" --limit=5`.
- Read the index: MCP `memex_index` with `{}`, or just read
  `~/.agents/ycode/memory/MEMORY.md` (global) or
  `<cwd>/.agents/ycode/memory/MEMORY.md` (project) directly when fs
  access is faster than MCP roundtripping.
- Forget: MCP `memex_forget` with `{name}`.
