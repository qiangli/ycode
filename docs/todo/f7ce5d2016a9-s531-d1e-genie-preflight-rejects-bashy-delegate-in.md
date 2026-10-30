---
id: f7ce5d2016a9
kind: task
title: 'S531.D1e genie preflight rejects bashy delegate (incomplete-preflight), so the remedy wraps it in @contain(net:deny): no door in WSL, needs podman on Windows'
seq: 57
status: done
priority: p0
created: 2026-10-29T18:46:28.103443Z
assignee: claude-opus5.5
sprint: 531
sprint_id: f7bc2570-ca8e-59b0-8163-c3c51ba23610
sprint_title: 'Genie core harness: drive vendor agent CLIs as tools'
closed: 2026-10-29T20:40:58.619918Z
closed_by: s531-manager
---

Found in the D3 live rerun 10-29 18:44Z on candidate bashy 185c790a, elephant. Outer genie (claude-opus5.5@dragon) with BASHY_DELEGATE_ALLOW=genie:codex-gpt6-sol@dragon,genie:claude-sonnet5.5@dragon ran 'bashy delegate --json --agent genie:codex-gpt6-sol@dragon --timeout 8m ...'. Policy refused it (rule incomplete-preflight); the deny remedy (examples/genie/agent.yaml ~805) taught '@effects(read,write,exec) @contain(net: deny)'. WSL: the contained child failed with 'llm env: WSL: find the Windows host address: no default route' (exit 2; envelopes recorded correctly as genie-delegate:genie-<m>@dragon). Native Windows: 'bashy contain: bashy podman is not running' (exit 125). A direct, uncontained delegate works (D3 adversarial E). Fix (operator principle: same on every OS, never require a sandbox, bashy guards govern; see S525.A5w): bashy delegate is the declared delegation mechanism (D0). Under genie it must pass preflight with its declared effects (exec, net, spend: check whether 'spend' outside execution.effectsCeiling makes preflight incomplete), governed by delegate's own guards (nonempty BASHY_DELEGATE_ALLOW, --timeout/--idle-timeout, caps, credential scrub) and the effect cap, not by a network sandbox. Do not widen anything for other commands. The remedy text must not steer a delegate call into @contain(net:deny). Red/green: a genie harness/policy unit test where a plain 'bashy delegate --agent genie:x@door --timeout 2m ...' with a nonempty allow passes preflight and runs with no contain and no podman (GOOS-independent); a non-allowed target is still refused; another unknown net command is still refused. go test -short on touched ycode packages only, plus GOOS=windows go vet.
