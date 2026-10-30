---
id: 09ca87a77ca4
kind: task
title: 'S531.D3x genie: ordinary exec (sh, git) runs under bashy''s interpreter guard @guard(net-free cap), never @contain/podman; same on every OS'
seq: 59
status: done
priority: p0
created: 2026-10-29T20:54:06.63716Z
assignee: claude-opus5.5
sprint: 531
sprint_id: f7bc2570-ca8e-59b0-8163-c3c51ba23610
sprint_title: 'Genie core harness: drive vendor agent CLIs as tools'
closed: 2026-10-29T22:14:46.541841Z
closed_by: s531-manager
---

Supersedes the podman design of D3w 64d907c2 (operator-shaping-1029 review 20:46Z: no sandbox unless the app/task explicitly asks; zero setup; same on every OS; enforce net:deny with bashy's interpreter-level guard, as in S525 A5d; never provision podman). Evidence: D3 Windows runs 7+8 10-29 on bashy f413bdcb, elephant. genie policy refused plain 'sh test_slug.sh' and 'git add/commit' as incomplete-preflight; the remedy (examples/genie/agent.yaml ~821) taught '@effects(read,write,exec) @contain(net: deny)', and contain needed podman (exit 125). Fix in genie (ycode): the remedy teaches '@effects("read,write,exec")' + '@guard("read,write,exec")' (bashy/docs/decorators.md: @guard is the in-interpreter effect cap; any command whose atlas effects exceed it, incl. net, is denied before it runs, nested guards only narrow), and genie policy accepts that guarded envelope as discharging the net-deny requirement wherever it accepted @contain(net: deny). @contain/oci only when the task explicitly asks for a sandbox. If bashy's harnessrunner compiler must mark a @guard-capped envelope effect-complete, put that bashy half in a patch file in docs/patches (as D4m did); the manager integrates it under the assignee identity. Red/green: genie harness/policy unit test where 'sh test.sh' and 'git commit' pass through the guarded remedy with no contain/podman (GOOS-independent), and a net command inside the guard is denied. go test -short on the touched ycode packages, GOOS=windows go vet; optionally a cross-compiled test binary run on elephant (C:/Users/liqia/s531-d3 only, under s531-manager's claim). No podman anywhere; no full suites on Dragon; never 'weave pull/salvage' bashy on Dragon.
