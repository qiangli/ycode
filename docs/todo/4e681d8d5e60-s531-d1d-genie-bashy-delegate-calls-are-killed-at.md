---
id: 4e681d8d5e60
kind: task
title: 'S531.D1d genie: bashy delegate calls are killed at the 60s per-command ceiling (nested delegates always timedOut)'
seq: 56
status: done
priority: p0
created: 2026-10-29T18:11:04.010521Z
assignee: claude-opus5.5
sprint: 531
sprint_id: f7bc2570-ca8e-59b0-8163-c3c51ba23610
sprint_title: 'Genie core harness: drive vendor agent CLIs as tools'
closed: 2026-10-29T18:46:43.444656Z
closed_by: s531-manager
---

Found in the D3 live run 10-29 on elephant WSL (18:02-18:07Z). The outer genie (dragon-door-claude-opus5.5) ran 'bashy delegate --json --agent genie:dragon-door-{codex-gpt6-sol,claude-sonnet5.5} --timeout 8m --idle-timeout 4m ...'. Both came back 'exit 0 (timedOut)' with no envelope; genie then wrote the files itself, honestly reported. The same delegate called directly from a shell succeeds (D3 adversarial E). Cause: internal/harness/bashy/executor.go caps every command at config.TimeoutMS (examples/genie/agent.yaml execution.timeoutMs 60000); jobLifetimeMs 900000 is only a claim expiry. Fix: a command that is the bashy delegate verb (the declared delegation mechanism, D0 contract) gets ceiling = min(its own --timeout + grace, jobLifetimeMs), and with no --timeout the job lifetime; every other command keeps 60s. delegate already enforces its own --timeout/--idle-timeout. Do not widen the ceiling for arbitrary commands. Red/green unit test in internal/harness/bashy (fake delegate sleeping past 60s under a 2m --timeout succeeds; a non-delegate sleep is still killed at the ceiling). go test -short ./internal/harness/... only.
