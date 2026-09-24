---
id: 770fb586958e
kind: feature
title: 'Compiler: inherit section defaults with null overrides'
seq: 43
status: done
priority: p1
created: 2026-09-23T18:36:09.115553Z
assignee: codex-gpt5.6-sol
sprint: 264
sprint_id: 04832379-25cb-5254-83e6-12f82fa9617a
sprint_title: Ycode configuration absence and default semantics
closed: 2026-09-24T00:30:00.514366Z
closed_by: codex-gpt5.6-sol
---

Implement spec.defaults as explicit shared templates for YAML sections/resource kinds. Omitted optional sections/resources remain disabled when no default applies; explicit local values override inherited values recursively; local null replaces inherited values with null/disabled. Preserve strict unknown-field/reference validation. Keep the implementation limited to this inheritance contract.
