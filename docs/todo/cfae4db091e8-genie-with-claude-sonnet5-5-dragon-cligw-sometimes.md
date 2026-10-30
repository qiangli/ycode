---
id: cfae4db091e8
kind: task
title: genie with claude-sonnet5.5@dragon (cligw) sometimes answers with a tool call written as text instead of a tool call
seq: 60
status: done
priority: p1
created: 2026-10-29T21:54:08.907982Z
assignee: claude-opus5.5
sprint: 531
sprint_id: f7bc2570-ca8e-59b0-8163-c3c51ba23610
sprint_title: 'Genie core harness: drive vendor agent CLIs as tools'
closed: 2026-10-29T23:26:49.099727Z
closed_by: s531-manager
---

Seen in D3 runs 10-29: Windows run 9 (21:39Z, bashy 75908e49): the delegate genie:claude-sonnet5.5@dragon returned exit 0, but its output was a bashy tool call written as text, which never ran, so test_slug.sh was not created (the outer genie wrote it from the draft and said so). Run 8 shows the same markup though that time the file was created. The codex door model is unaffected. Likely the openai-compat tool-call mapping for claude CLI output through cligw (door) or genie's parser not accepting that shape. Reproduce with a fixed transcript fixture; red/green in the genie/provider parser or cligw adapter. This is genie own-loop quality (Sprint 325 domain) and doesn't block S531 D3 acceptance.
