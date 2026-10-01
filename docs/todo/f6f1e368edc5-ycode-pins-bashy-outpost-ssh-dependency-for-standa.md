---
id: f6f1e368edc5
kind: bug
title: Ycode pins Bashy Outpost SSH dependency for standalone builds
seq: 49
status: done
priority: p1
labels:
    - integration
created: 2026-10-01T02:40:53.24072Z
assignee: codex-gpt6-sol
sprint: 340
sprint_id: 07abaf0d-4c54-57e7-b324-8c926b4c300f
sprint_title: Pure-Go m4, localedef and lp; listing view for the optional external POSIX tools
closed: 2026-10-01T02:45:27.33113Z
closed_by: codex-gpt6-sol
---

Sprint 340 closure exposed that Ycode imports current Bashy, whose peer channel imports github.com/qiangli/outpost/pkg/sshserver. Go replacements are not transitive: Ycode still fetched an older Outpost module without pkg/sshserver, so go test -short ./... failed. Add Ycode local Outpost replace and pinned sibling bootstrap mapping; resync all sibling pins; rerun focused build and full short test gate. Preserve existing external module policy.
