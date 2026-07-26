---
topic: agent-switching
summary: hand this session to another agent, with context
when: the current task suits a different agent, or you want a second opinion from one
audience: agent
max_lines: 130
---

The goal is to move from one agent tool to another as smoothly as possible,
with ycode and bashy as the bridge. ycode can hand a session to any agent in
the fleet — claude, codex, opencode, agy, another ycode — and the conversation
goes with it.

That carried context is the whole point. Running `bashy chat --agent codex`
from a terminal is one line; doing it through ycode is only worth the plumbing
because the work in progress travels.

## When to use this

Read this when the task would be better done by a different agent — a stronger
band for a hard problem, a cheaper one for mechanical work, a second opinion
from another vendor — or before concluding something cannot be done from here.

Not for running a one-off command; that is the bash tool. Switching moves the
whole conversation.

## The four verbs

`/model` changes the MODEL under this session (in-process, free). `/agent`
changes the whole AGENT (tool:model). `/tool` changes the TOOL, using its own
configured model. `/detach` ends an attached session.

With no argument, `/agent` and `/tool` LIST what is available, grouped by band.
That listing is pure, so it works in `--print` and shell mode where switching
itself does not.

## Selectors

| Form | Example | Meaning |
|---|---|---|
| agent name | `/agent codex-gpt-5.5` | exactly that agent |
| nickname | `/agent Arlo` | the same, by its human handle |
| band | `/agent L4` | the strongest agent at that band or above |
| tool | `/tool codex` | that CLI, with whatever model it is configured for |

A band selector prefers a NON-ycode tool — the point is to leave ycode, so
`L4` resolving back into ycode would be a no-op that looks like it worked. An
unresolvable selector is an ERROR naming near matches, never a literal.

## Two modes

**Attach (default).** You stay in the ycode TUI; what you type is forwarded and
the reply rendered here. ycode keeps the screen, keeps recording, stays the
interface. The status bar shows `AGENT  codex:gpt-5.5 → ycode:glm-5.2`, so who
is answering is never ambiguous.

**Takeover (`--takeover`).** ycode suspends and the agent's own full-screen UI
owns the terminal until it exits. Use it when you want the tool's real
interface; the cost is that ycode cannot see what happened — see *Provenance*.

`--fresh` (alias `--no-context`) is the only way to opt out of carrying the
conversation. An unknown flag is rejected rather than swallowed as a selector,
so a misspelled `--fresh` cannot silently keep context you asked to drop.

## Provenance — how much to trust what you read

Every switch asks the target to run `bashy handoff` before exiting — the
fleet-wide way work moves between agentic tools, capturing the brief, the next
action AND the in-flight diff into a record `bashy resume` picks up cold, in
another tool, on another machine. ycode folds it into the transcript. It is an
instruction, not a mechanism, so the transcript states which of three it holds:

- **`… its own report`** — the agent ran `bashy handoff`. Most reliable, and
  the in-flight work is recoverable with `bashy resume <id>`. Still a
  self-report: ycode did not observe it.
- **`… reconstruction`** — no note; scraped from the terminal. A pty merges
  stdout and stderr, so banners survive while some of the answer is lost.
  Indicative, not verbatim.
- **`… NOT captured`** — a takeover with no note. Ask the user rather than
  assuming continuity.

Attached replies are recorded as `[via <agent>]`, so one is never mistaken for
something ycode said.

## Limits

- Windows and `--print`/`ycode shell` are refused: no pty, no terminal. The
  error names the equivalent `bashy chat --agent <name> -i`.
- One level deep (`YCODE_AGENT_DEPTH`), and refused inside a `bashy meet`
  turn — both recurse without bound, and each level is a real session.
- Needs `bashy` on PATH (or `$YCODE_BASHY_BIN`): bashy owns agent resolution,
  the credential firewall and sandboxing. ycode never launches a tool directly.
- ycode advertises no control socket, so `bashy chat steer` cannot reach in.

## Relationship to bashy

`/agent` runs `bashy chat` underneath, on the same catalog: identical names,
nicknames and band selectors. The FLAGS differ and should — `bashy chat`
launches a process (`--cwd`, `--timeout`, `--sandbox`); `/agent` is an
in-session verb that inherits those.

One difference is deliberate: **bashy defaults to NO context and adds it with
`--context`; ycode defaults to CARRYING the conversation and drops it with
`--fresh`.** bashy has no session to carry; ycode does, and carrying it is
the reason to switch from here.

## Exact calls

```bash
# From inside a ycode session (slash verbs, not shell commands):
/agent                            # list agents, grouped by band
/tool                             # list tools
/agent codex-gpt-5.5              # attach, carrying this conversation
/agent Arlo                       # the same agent, by nickname
/agent L4                         # strongest agent at band 4+, preferring non-ycode
/agent L3 --fresh                 # attach with no context carried
/agent claude-opus4.8 --takeover  # hand the terminal to its own UI
/tool codex                       # switch by tool, its own configured model
/detach                           # end the attached session, return to ycode

# The equivalent outside a ycode session, which is what runs underneath:
bashy chat --agent codex-gpt-5.5 -i --context "<prior conversation>"
bashy chat --tool codex -i
bashy agents list                 # the same catalog /agent reads
```
