---
id: c80317de674d
kind: task
title: Define the ycode CLI declaratively in agent.yaml
seq: 31
status: todo
priority: p0
created: 2026-09-06T19:13:28.072141Z
sprint: 132
---

Foundation story. Design and implement strict spec.interfaces.cli schema for command trees, positional arguments, inherited/local flags, aliases, help/version text, completion, stdin/TTY behavior, dispatch targets, exit codes, and output presentation. Migrate the current Cobra surface in cmd/ycode/main.go to YAML while retaining only a minimal generic bootstrap (--file/config discovery, schema validation, safe dispatch). Acceptance: unknown fields fail closed; no ycode-specific command wiring remains in Go; golden tests cover root/subcommand help, parsing, dispatch, errors, and completion; documentation includes migration and compatibility rules. This story establishes the shared contract required by the other four CLI-profile stories.
