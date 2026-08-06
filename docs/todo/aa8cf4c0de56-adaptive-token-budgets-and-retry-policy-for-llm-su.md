---
id: aa8cf4c0de56
kind: task
title: Adaptive token budgets and retry policy for LLM summarization
seq: 2
status: todo
priority: p2
created: 2026-08-06T15:35:05.848231Z
assignee: steward
---

Background: commit 7e94082 fixed the observed gpt-5.6-terra empty-summary failure by setting reasoning_effort=low, raising LLMSummaryMaxTokens from 1024 to 4096, accepting completed content blocks, and retaining heuristic fallback. Remaining enhancement: (1) keep 4096 as the normal checkpoint ceiling; (2) select 8192 when compacted input exceeds about 50K estimated tokens or has unusually dense tool activity; (3) on an explicit max_tokens/incomplete response with no visible text, retry once at 2x the initial allowance, never on arbitrary provider errors; (4) cap retained visible summary near 2000 tokens without breaking intent_summary structure; (5) define centralized allowances for classification 256-512, extraction/title 512-1024, compaction 4096/8192, and code/review 8192-16384; (6) record requested allowance, actual visible/reasoning usage, retry reason, and fallback in telemetry. Acceptance: table-driven request-budget tests, max-token empty-response retry test, no-retry-on-auth/quota/timeout tests, visible-cap structural test, and existing session/runtime suites remain green.
