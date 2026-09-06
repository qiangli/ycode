---
id: 0e6063e03ae7
kind: task
title: Prove harness language with prior-art reconstruction fixtures
seq: 23
status: done
priority: p0
created: 2026-09-02T20:39:45.72774Z
sprint: 106
---

Wave 1 contract gate. Add complete mock-provider fixtures for self-hosted ycode plus Codex-like, OpenCode-like, OpenClaw-like, and Hermes-like orchestration. Each fixture must compile and reproduce its golden event trace using only the model-visible bashy tool.

Evidence: five complete documents under `examples/harness-reconstructions/`
compile with the frozen schema and execute through the same typed pipeline
runner and deterministic mock-provider adapter. Their checked-in JSONL golden
traces cover normalized provider usage/text/tool/outcome events and explicit
input, steering, measurement, model, preflight, authorization, fencing,
execution, hook, bounded fan-out and output stages as declared by each profile.
The test asserts every provider request exposes exactly one tool named `bashy`
and a source scan rejects product-specific neutral-kernel branches. Focused
pipeline/spec race tests pass three consecutive runs.
