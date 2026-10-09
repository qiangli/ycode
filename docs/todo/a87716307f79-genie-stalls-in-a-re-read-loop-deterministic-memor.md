---
id: a87716307f79
kind: bug
title: 'genie stalls in a re-read loop: deterministic memory compaction fires every turn, the model forgets what it read, and a weave say steer never reaches the session'
seq: 51
status: todo
priority: p1
labels:
    - genie
created: 2026-10-09T09:29:56.486598Z
sprint: 379
sprint_id: 908c2ac2-e7bc-55fe-86bd-d25046ac4684
sprint_title: 'bashy 1.0.0 feature list: bash + Bash# + Yoke'
---

Sprint 379 conductor 2026-10-09, weave yoke run 3 (genie, model glm-5.3, session b6805ad5-f830-4748-85c6-4c9e19c8d10a), story 21e51889: 33 minutes, 311 bashy.requested, 43 memory.compaction.fallback_deterministic, 48 checkpoint.saved, 0 file edits. After each compaction the model re-ran the same reads (sed -n on pkg/chat/chat.go, cat shell_shim_unix_test.go, grep forceAgentShell) and every bashy.requested appeared twice with identical argv (duplicate tool calls per step). A weave say steer at 09:20Z produced no input.admitted event (only the initial one), so steering a headless genie worker is impossible. Each grep output also carried a bashy-hint-v1 JSON line from stderr, adding noise to every observation. Evidence: events filtered by session id in ~/.bashy/ycode/harness/sessions/events.jsonl (copy kept by the conductor). Expected: (1) compaction keeps a working summary of files read and findings, so reads are not repeated; (2) identical tool calls within one step are deduped or executed once; (3) weave say into a genie run is admitted as user input at the next turn boundary; (4) bashy hints are suppressed in genie command output (BASHY_HINTS=off in the execution env). Red/green tests per item where unit-testable; then re-run a GLM worker on a small yoke story as the live proof.
