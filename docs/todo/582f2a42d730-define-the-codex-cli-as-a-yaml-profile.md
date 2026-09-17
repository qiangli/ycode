---
id: 582f2a42d730
kind: task
title: Define the Codex CLI as a YAML profile
seq: 33
status: done
priority: p1
created: 2026-09-06T19:13:28.19494Z
assignee: codex-gpt5.6-sol
sprint: 132
closed: 2026-09-17T15:05:53.8012Z
closed_by: codex-gpt5.6-sol
---

Using the shared spec.interfaces.cli contract from the ycode foundation story, encode the Codex-like command hierarchy, flags, aliases, help, stdin/TTY routing, dispatch, and output presentation in YAML. Reuse the generic bootstrap and neutral harness runtime; add no product-specific Go branches. Acceptance: profile validates strictly; golden help/parsing/dispatch/error/completion fixtures pass; supported behavioral compatibility is explicit; unsupported wire/vendor quirks fail clearly rather than being approximated. Dependency: ycode declarative CLI foundation.
