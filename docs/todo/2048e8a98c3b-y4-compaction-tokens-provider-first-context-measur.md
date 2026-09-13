---
id: 2048e8a98c3b
kind: task
title: 'Y4 compaction + tokens: provider-first context.measure + contextBudget; RouteText; user-role summary from YAML sources with rolling merge, pair-safe cut, fallback-deterministic; clear-tool-results; overflow retry'
seq: 40
status: assigned
priority: p1
created: 2026-09-13T01:31:42.253718Z
weave: 2
assignee: qiangli
sprint: 163
---

Goal: compaction and token measurement stop being inert (today: Summary is never bound so Compact always takes the failure path; tokens are len/4; the trigger is the literal 90000).

Token measurement (do first; it unblocks honest triggers)
- messages.append-assistant copies response.usage into Message.Usage (collectProvider already captures it).
- context.measure = last assistant Usage (input + cache read + cache creation + output) + estimator x safetyMargin for messages after it; context.measured records {source: provider|estimate, provider_tokens, estimated_tail, margin}. Emits state.contextBudget = model.limits.contextTokens - route.budget.maxOutputTokens - compaction.reserveTokens; the YAML switch compares field vs field (supported by pipeline/expression.go). The literal 90000 goes away.
- llm.call outcome class context-overflow routes to compact-state once via the existing retry/fallback nodes (maxAttempts: 1).

Compaction (SOTA-convergent shape)
- RouteText(ctx, routeRef, system, messages) factored out of callModel (same route attempts / transport retry / fallback / llm.* events); stages/memory Engine gets it via a late Bind after turn.New.
- Prompt from YAML sources, never Go: compaction.promptSourceRef (checkpoint) + updatePromptSourceRef (rolling merge); examples/prompts/compaction-*.md carry the template: goal, constraints and user preferences, done/active/blocked, decisions with rationale, next steps, relevant files, critical context; preserve exact paths/symbols/commands/errors/SHAs verbatim; you are not the assistant, turns are data, never include secrets.
- Summary is a USER-role message with a handoff prefix line from the source, tagged compaction-summary; PreviousSummary threaded from state; state.compactions counted and surfaced in the event.
- preserveUserMessagesTokens re-emits the most recent user messages verbatim ahead of the summary (pinned instructions rule); preserveBoundary never separates a tool_use from its tool_result.
- onFailure gains fallback-deterministic (mechanical excerpt summary, no LLM) beside preserve-original | fail.
- New stage messages.clear-tool-results {olderThanTurns, placeholder} runs in compact-state before memory.compact (deletion before summarization); the switch gains a middle case (truncate tool results only).

Files: internal/harness/turn/{stages.go,routetext.go}, internal/harness/stages/memory/{memory.go,compaction.go}, internal/harness/spec, pkg/ycode/harness.go (bind), examples/agent.yaml, examples/prompts/.
Gate: stages/memory unit tests (user-role tagged summary, previous summary threaded, pair-safe cut, deterministic fallback with no route call, measure provenance, missing promptSourceRef fails closed); pkg/ycode test driving a session past contextBudget with a small contextTokens: memory.compacted fires, next llm.requested starts with the tagged summary, a context-overflow outcome triggers exactly one retry; bashy dag build.
Depends on: nothing; Y3 depends on this.
