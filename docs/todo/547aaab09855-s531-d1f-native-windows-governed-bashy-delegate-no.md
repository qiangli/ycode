---
id: 547aaab09855
kind: task
title: 'S531.D1f native Windows: governed bashy delegate not recognized (unwrapped, front-door incomplete-preflight); D1e works on WSL'
seq: 58
status: done
priority: p0
created: 2026-10-29T19:31:02.689442Z
assignee: codex-gpt6-astra
sprint: 531
sprint_id: f7bc2570-ca8e-59b0-8163-c3c51ba23610
sprint_title: 'Genie core harness: drive vendor agent CLIs as tools'
closed: 2026-10-29T20:06:51.268654Z
closed_by: s531-manager
---

Found in the D3 live runs 3+4 10-29 19:13Z/19:23Z on candidate bashy 81e76eca (ycode 79b348a), elephant native Windows, outer genie claude-opus5.5@dragon, BASHY_DELEGATE_ALLOW=genie:codex-gpt6-sol@dragon,genie:claude-sonnet5.5@dragon exported in the bashy script. 'bashy delegate --json --agent genie:codex-gpt6-sol@dragon --timeout 8m --idle-timeout 4m <instr>' (with and without --cwd) reached bashy preflight UNWRAPPED. Event seq 358 in C:/Users/liqia/.bashy/ycode/harness/sessions/events.jsonl: type bashy.failed, intent.commands=[delegate argv plain], unsupported=[{kind:command, value:delegate, reason:'front-door command requires a trusted digest-pinned executable resolver'}]. Policy event 359 evaluated governed-delegate but did not match. The same script on WSL was wrapped and governed (D3 WSL PASS 5c57bc8; the --cwd variant got the D1e guard refusal). So on Windows delegateRequest/delegateGovernance returned not-delegate or a refusal. Suspects: e.environment() on Windows (BASHY_DELEGATE_CALLER forced? BASHY_DELEGATE_ALLOW passthrough; Windows env name case), the delegateCall literal check, or the windows build path of Executor.request. Note genie_delegate_unix_test.go is //go:build unix, so the governed path has no Windows coverage. Fix the root cause; make the governance tests portable (no unix-only exec); prove it on Windows: GOOS=windows go test -c ./internal/harness/bashy -o bashy-harness.test.exe, copy with 'bashy outpost scp <file> liqiang@elephant.local:C:/Users/liqia/s531-d3/' and run it via 'bashy outpost ssh liqiang@elephant.local -- C:/Users/liqia/s531-d3/bashy-harness.test.exe -test.run Delegate -test.v' (elephant is claimed by s531-manager; only C:/Users/liqia/s531-d3; no builds on elephant). Report red/green. go test -short on touched packages on Dragon; no full suites.
