---
id: b9c796624295
kind: bug
title: S557 isolate unattended detection fixtures from host CI environment
seq: 61
status: assigned
priority: p1
created: 2026-11-01T17:03:27.801511Z
assignee: s557-manager
sprint: 557
sprint_id: f5ccf563-f5d1-5b94-8be4-9388e51ef86d
sprint_title: 'Bashy v1.0.0 preparation: split private/public development and retire qiangli/dhnt'
---

Native hosted tests inherit CI=true but three unattended fixtures clear only YCODE_UNATTENDED and weave markers. Clear CI inside each fixture before assigning its scenario; retain explicit CI true/false coverage and production behavior. Verify with CI=true targeted package tests.
