---
id: 8ca06fe246d8
kind: bug
title: S557 restore portable file and process contracts in native Windows tests
seq: 63
status: todo
priority: p1
created: 2026-11-01T17:08:42.435405Z
sprint: 557
sprint_id: f5ccf563-f5d1-5b94-8be4-9388e51ef86d
sprint_title: 'Bashy v1.0.0 preparation: split private/public development and retire qiangli/dhnt'
---

Diagnose and fix remaining native Windows Ycode failures from /tmp/s557-native-windows-standalone.log after CRLF orchestration fix, CI isolation a765709 and portable provider6aebcd4. Product bugs include LSP file URIs and concurrent atomic memory index replacement; fixture bugs include slash assumptions, HOME versus USERPROFILE, Unix bash process fixtures, mode expectations. Preserve test coverage, security intent and product contracts; no skips or broad normalizations. Work in isolated weave; parent owns main workspace, all pushes/integration. Use bounded local focused tests/crosscompile only; no full build/suites on dev host. Commit source/story with Sprint557 trailers; do not change go.mod pins unless essential. Do not publish private paths/log details in public docs. Read CLAUDE. Ask manager for hosted test execution; report precise remaining platform risks.
