---
id: 8b5d9e16c5ff
kind: task
title: 'Y3 session history: session.commit + session.load (newest boundary, drop system roles, clear old tool results, whole-turn maxTokens, tool-pair repair), history port, fork seed, --session'
seq: 39
status: assigned
priority: p1
created: 2026-09-13T01:31:42.230811Z
weave: 3
assignee: qiangli
sprint: 163
---

Goal: prior turns of a session reach the next prompt (today every turn starts from an empty message list; the REPL reuses one SessionID but each line is a fresh prompt). SOTA common denominator: append-only transcript + read-side projection from the newest compaction boundary; ycode's event log IS the transcript.

- session.commit stage (explicit YAML node after the loop): puts loop.messages (post-compaction, full blocks) into the payload store and appends session.turn-committed {messages_ref, message_count, tokens, compactions}. turnBoundary in pkg/ycode/harness.go gains MessagesRef so Fork can seed a child (session.forked gets parent_messages_ref) — this is what sessions.branches.copyEvents: through-checkpoint means (reference, not copy).
- session.load stage (parallel with context.load): newest session.turn-committed for this session (else the fork seed, else empty) -> drop system-role messages (context and knowledge are re-assembled fresh so cache prefixes stay stable) -> clearToolResults {olderThanTurns, placeholder} -> maxTokens keeping whole turns only -> tool-pair repair (synthesize a result for an orphaned tool_use, drop an orphaned tool_result) -> session.history.loaded event with the projected payload. Fail closed when sessions.<ref>.history is absent or unbounded.
- Compaction summaries are tagged (ContentBlock name compaction-summary, user role per Y4) so session.load keeps them.
- history port in prompt.assemble (order [context, knowledge, history, input]); the turn stage splices full messages at the port position (PromptMessage is text-only).
- ycode prompt|repl --session <id> continues a session; ACP already carries the id.

Files: internal/harness/stages/session/ (new), internal/harness/turn/{stages.go,turn.go}, internal/harness/stages/ioctx/ioctx.go, internal/harness/spec, pkg/ycode/harness.go, cmd/ycode/harness_application.go, examples/agent.yaml (sessions.durable.history: {maxTokens: 20000, unit: turns, clearToolResults: {olderThanTurns: 4}, repair: tool-pairs}).
Gate: unit tests for projection order/whole-turn cut/pair repair/system-role exclusion/fork seed/fail-closed; pkg/ycode test with the stub provider: two Runs on one SessionID -> the second llm.requested payload contains the first turn; Fork child's first request contains the parent's history; a new Harness over the same control root (process restart) still sees it.
Depends on: Y4 (summary tagging); independent of coreutils.
