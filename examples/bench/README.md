# agent-mini benchmark subsets

Fixed instance lists for the agent-mini growth plan. Tune on `dev`; report
on `test` sets only, never tune on them.

| File | Source | Size | Use |
|---|---|---|---|
| `subsets/verified-dev50.txt` | `princeton-nlp/SWE-bench_Verified` (test split) | 50 | dev: every increment is measured here first |
| `subsets/verified-test450.txt` | same, the remaining instances | 450 | test: comparison with the published bash-only (mini-swe-agent) board |
| `subsets/rebench-2026_03.txt` | `nebius/SWE-rebench-leaderboard` split `2026_03` | 110 | test: contamination-resistant headline set |

**Selection (reproducible):** the dev set is stratified by repository in
proportion to its share of the 500 Verified instances (largest remainder),
and within each repository takes the instances with the lowest
`sha256(instance_id)`. The test set is the complement. SWE-rebench uses the
whole monthly split.

SWE-bench Verified is contamination-flagged (retired as a frontier measure in
2026), so it is used for iteration and comparability with published bash-only
results, never as the only evidence; SWE-rebench is the headline test set.

## Runner notes

- `bashy dag -f dag.md fetch|requests|tools|solve|smoke` — see the variable table
  in `dag.md`. `smoke` is the only target meant for the dev box.
- ycode resolves `runtime.workspace` and the readable/writable roots against the
  **config file's directory**. The runner therefore writes one config per task
  whose workspace and roots are the task checkout. (agent-mini's own `.bar`
  keeps `workspace: .`, so run standalone it operates on the bundle directory —
  a known limitation of the unchanged example, fixed in genie.)
