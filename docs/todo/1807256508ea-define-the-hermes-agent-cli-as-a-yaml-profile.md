---
id: 1807256508ea
kind: task
title: Define the Hermes Agent CLI as a YAML profile
seq: 35
status: done
priority: p1
created: 2026-09-06T19:13:28.322416Z
assignee: claude-opus4.8
sprint: 132
closed: 2026-09-17T14:37:42.962366Z
closed_by: codex-gpt5.6-sol
---

Using the shared spec.interfaces.cli contract from the ycode foundation story, encode the Hermes Agent-like command hierarchy, flags, aliases, help, stdin/TTY routing, dispatch, and output presentation in YAML. Reuse the generic bootstrap and neutral harness runtime; add no product-specific Go branches. Acceptance: profile validates strictly; golden help/parsing/dispatch/error/completion fixtures pass; supported behavioral compatibility is explicit; unsupported wire/vendor quirks fail clearly rather than being approximated. Dependency: ycode declarative CLI foundation.
