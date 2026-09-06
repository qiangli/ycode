---
id: c99d404d919c
kind: task
title: Align documentation and release contracts
seq: 22
status: done
priority: p0
created: 2026-09-02T20:26:06.530532Z
sprint: 106
---

Wave 4; depends on cutover. Update architecture, usage, pipeline, release and feature docs plus AGENTS.md/CLAUDE.md/GEMINI.md together; run bashy dag build and release checks.

Evidence: README, architecture, usage, pipeline, schema, sprint, release, and
per-OS QA documentation now describe the canonical `examples/agent.yaml`, sole
Bashy model tool, neutral typed lifecycle, physically removed legacy surfaces,
and public Harness API. AGENTS.md, CLAUDE.md, and GEMINI.md are byte-identical.
The feature registry contains only reachable YAML-harness capabilities, the
DAG targets current frontend tests, and release/promote enforce the exact five
archives. YAML/link/source scans, feature tests, CLI validation/help smokes,
and `bashy release check`/`plan` pass; the parent lane owns the final build gate.
