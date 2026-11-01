---
id: 062d9ca4d712
kind: bug
title: S557 share portable TUI provider fixture with Windows tests
seq: 62
status: done
priority: p1
created: 2026-11-01T17:06:19.524168Z
assignee: s557-manager
sprint: 557
sprint_id: f5ccf563-f5d1-5b94-8be4-9388e51ef86d
sprint_title: 'Bashy v1.0.0 preparation: split private/public development and retire qiangli/dhnt'
closed: 2026-11-01T17:11:39.247024Z
closed_by: s557-manager
---

Portable shared-session tests reference tuiProvider currently declared inside a !windows PTY test file. Move unchanged neutral provider fixture to a common test file, retaining PTY platform guards. Verify Windows package cross-compilation and bounded native shared-session cases.
