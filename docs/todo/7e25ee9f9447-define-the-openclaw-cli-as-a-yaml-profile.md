---
id: 7e25ee9f9447
kind: task
title: Define the OpenClaw CLI as a YAML profile
seq: 34
status: todo
priority: p1
created: 2026-09-06T19:13:28.257471Z
sprint: 132
---

Using the shared spec.interfaces.cli contract from the ycode foundation story, encode the OpenClaw-like command hierarchy, flags, aliases, help, stdin/TTY routing, dispatch, and output presentation in YAML. Reuse the generic bootstrap and neutral harness runtime; add no product-specific Go branches. Acceptance: profile validates strictly; golden help/parsing/dispatch/error/completion fixtures pass; supported behavioral compatibility is explicit; unsupported wire/vendor quirks fail clearly rather than being approximated. Dependency: ycode declarative CLI foundation.
