---
id: 24f3a906c5db
kind: task
title: Define mini-SWE-agent CLI in Bashy shell and YAML
seq: 36
status: done
priority: p1
created: 2026-09-06T19:16:36.259317Z
assignee: codex-gpt5.6-sol
sprint: 132
closed: 2026-09-17T16:44:43.652681Z
closed_by: codex-gpt5.6-sol
---

Produce two equivalent mini-SWE-agent v2 reconstructions from the upstream SWE-agent/mini-swe-agent contract: (1) a readable Bash script implemented on the Bashy shell/execution boundary, and (2) a strict spec.interfaces.cli YAML profile rendered by the generic ycode bootstrap. Cover mini help and flags (-t/--task, -c/--config, -m/--model, -y/--yolo, -l/--cost-limit, -o/--output, --model-class, --agent-class, --environment-class, --exit-immediately), task prompting, confirm/yolo/human modes and /c /y /u /h /m controls, Ctrl-C interruption, linear trajectory persistence, model/cost limits, output truncation, independent stateless shell actions, and explicit completion/submission. The model-visible execution capability remains Bashy only. Acceptance: shell and YAML versions share fixtures and produce equivalent normalized events/exit outcomes; golden help/parsing/mode/approval/interruption/trajectory tests pass; non-interactive behavior fails closed; upstream-supported behavior versus intentionally unsupported Python/plugin/vendor details is documented; no mini-specific Go branch is added. Dependency: Story #31 shared declarative CLI contract. Upstream reference: https://github.com/SWE-agent/mini-swe-agent (v2 main, pinned to a reviewed commit during implementation).
