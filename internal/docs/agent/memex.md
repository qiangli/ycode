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

Two surfaces, both real. There is no MCP server — the `memex_*` tool
names that older prompts mention no longer exist anywhere.

Shell built-ins (any bash routed through `ycode shell -c`):

- `yc remember "<text>" [--name=<id>] [--scope=project|user] [--type=user|project|reference|feedback]`
  — save a fact. `--name` defaults to `note-<unixtime>`; the first line
  of the text becomes the description; importance defaults to 0.5.
- `yc recall <query> [--limit=N] [--json]` — RRF-fused semantic search
  across the memex backends.

In-session ycode tools (available to the model inside a ycode run):

- `memory_save` — persist or overwrite by name. Required: `name`,
  `content`. Optional: `description`, `type` (`user` | `feedback` |
  `project` | `reference`; default `project`).
- `memory_recall` — semantic/full-text search with temporal decay.
  Args: `query`, optional `max_results` (default 5).
- `memory_list` — every memory, optionally filtered by type or scope.
  Heavy; prefer recall when you have a query.
- `memory_forget` — remove by name. Irreversible.

The curated index file (`MEMORY.md`) has no dedicated verb — read it
with your filesystem tool at the paths listed below.

## Failure modes

| Symptom | Fix |
|---|---|
| `unknown memory type` | Use one of `user`, `feedback`, `project`, `reference`. |
| Recall returns empty | The fact may not be saved yet; or the query is too narrow. Run `memory_list` once to confirm. |
| Save overwrites unexpectedly | `name` is the primary key; saves with an existing name overwrite. Pick a distinct name. |
| Wrong scope | `--scope=project` writes under `<cwd>/.agents/ycode/memory/`; `--scope=user` (alias `global`) writes under `~/.agents/ycode/memory/`. Pick deliberately. |

## Exact calls

- Save from shell: `yc remember "<fact>" --name=<id> --type=reference`
- Save to user scope: `yc remember "<fact>" --scope=user`
- Recall from shell: `yc recall "<query>" --limit=5`
- Machine-readable recall: `yc recall "<query>" --json`
- Save in-session: `memory_save` with `{name, content, description?, type?}`
- Recall in-session: `memory_recall` with `{query, max_results?}`
- Forget in-session: `memory_forget` with `{name}`
- Read the index: open `~/.agents/ycode/memory/MEMORY.md` (user) or
  `<cwd>/.agents/ycode/memory/MEMORY.md` (project) with your fs tool.
