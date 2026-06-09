# Loom v2 — Implementation plan

Status: implementation plan. Companion to [`loom-v2-plan.md`](./loom-v2-plan.md) (design) and [`weave-runbook.md`](./weave-runbook.md) (end-to-end UX).

This plan turns the design into a sequenced PR backlog with explicit dependencies, parallelization markers, and risk callouts. It maps directly to the migration phases in the design doc (N+0 / N+1 / N+2) and leans on the principle: **ship the smallest unit that can stand on its own at every step, never break v1.**

## Overall shape

| Phase | Goal | Surface ships? | Estimated PR count |
|---|---|---|---|
| **N+0** — Foundation | Internal substrate ready; v2 MCP verbs alongside v1 | Agent surface (MCP) only; no `weave` CLI | 5–7 |
| **N+1** — Front door | `ycode weave` CLI, auto-attach default-on, Gitea bootstrap, full UX | Everything in the design | 10–14 |
| **N+2** — Cleanup | Remove deprecated v1 verbs, doc pass | API hygiene only | 2–3 |

## Three early spikes (before any phase work)

Three unknowns block confident scoping. Each is ~half a day. Doing all three before committing to N+1 saves rework.

### Spike 1 — Gitea project-board API

**Question:** Do Gitea's kanban project-board REST endpoints support card position read/write reliably across the embedded version we ship?

**Approach:** Stand up `ycode serve`, create a project board with columns via API, add issues as cards, query card positions, move a card via API, re-query, observe order. Repeat with concurrent moves.

**Acceptance:** Positions are stable, ordered, and updatable via REST without race-induced corruption.

**Fallback if red:** Switch the "fine-grained priority within tier" dimension from board position to a `loom:rank:N` label. Update the design doc's excluded-features list to remove the "no numeric ranking" exclusion.

### Spike 2 — Optimistic-concurrency for atomic claim

**Question:** Can two near-simultaneous `weave start` calls atomically claim different cards without racing?

**Approach:** Two goroutines try to apply `loom:working` to the same issue concurrently via Gitea label API. Observe whether Gitea returns ETag on issue GET, supports `If-Match` on PATCH, or has any CAS-style label-set primitive. Test with 10× concurrent attempts to detect any silent overwrite.

**Acceptance:** First writer wins deterministically; second observes the existing label and can retry against the next candidate.

**Fallback if red:** Add a SQLite-backed claim lock in `pkg/loom` that mediates between Service callers. Less elegant (extra moving part) but bulletproof. Affects the design's "Gitea is the source of truth" purity but not the user-visible behavior.

### Spike 3 — Reference clone inside embedded Gitea

**Question:** Does `git clone --reference <gitea-bare-on-disk> <gitea-http-url>` produce a working tree with shared object store and per-clone refs/index/stash/reflog, on the embedded Gitea's on-disk layout?

**Approach:** Locate a bare repo on disk under Gitea's data dir; do a reference clone via HTTP URL; verify `.git/objects/info/alternates` is populated, refs are per-clone, two reference clones don't share branch namespaces, basic edit-commit-push works end-to-end.

**Acceptance:** Reference clone works; sandbox-isolation invariant holds (no shared refs/index/stash/reflog between siblings).

**Fallback if red:** Fall back to plain `git clone` (no `--reference`). Disk cost grows by the full object store per lease — fine for small repos, painful for monorepos. Worth confirming exactly because the design relies on this for cost-efficient isolation in the large-repo case.

**Edge:** what happens if Gitea pack-gc runs while a reference clone is alive? Spike 3 also probes this — Loom must coordinate to never gc the parent while children exist.

## N+0 — Foundation

Ordering matters; each PR unblocks the next.

| # | PR | Depends on | Why |
|---|---|---|---|
| N0.1 | **Lease-store path unification.** Route the worker (`cmd/ycode/autopilot.go`) through `pkg/loom.Service` instead of reading `~/.agents/ycode/observability/gitea/loom/leases.json` directly. | — | Smallest PR. Fixes the divergence with `cmd/ycode/loom.go`. Sets up everything downstream. |
| N0.2 | **`Service.SubmitAndWait` + `Service.Rebase`.** Block-with-deadline contract and internal conflict-rebase primitive. Pure additions to `pkg/loom`. Includes conflict-marker-in-sandbox logic. | N0.1 | Load-bearing v2 method. Used by every higher-level verb. |
| N0.3 | **`Service.Watch` channel.** Event stream for transitions. Drives both the MCP resource and the future TUI. Internal-only at this stage. | N0.1 | Required for `loom://session` and `loom://project` resources. |
| N0.4 | **`PolicyLoom` wiring in `internal/service/workspace.go`.** Take the existing "not yet wired" error path and connect it to `Service.Lease`. | N0.1 | Unblocks auto-attach in N0.7. |
| N0.5 | **Sub-agent role MCP verbs** (`loom_checkpoint`, `loom_submit`, `loom_abandon`) + `loom://session` resource. Thin handlers over Service. Register alongside v1 verbs with role-gating from env (`YCODE_LOOM_ID` set). | N0.2, N0.3 | First user-callable surface. |
| N0.6 | **Orchestrator role MCP verbs** (`loom_open`, `loom_terminate`, `loom_handoff`) + `loom://project` resource. | N0.3 | Pairs with N0.5. |
| N0.7 | **`ycode wrap --loom=auto` opt-in.** Reads project config, calls `Service.Lease`, sets env vars (`YCODE_LOOM_ID` etc.), redirects cwd, execs the child agent. Still experimental, opt-in via flag. | N0.4, N0.5 | Where the gnarly env-passing + cwd-redirection lives. |

**End-of-N+0 state:** a savvy user can manually call `ycode wrap --loom=auto -- claude-code` and get the full v2 sub-agent experience. v1 still works untouched. No `weave` CLI yet.

## N+1 — Front door

Five groups; intra-group sequential, inter-group partially parallel.

### Group A — Gitea side (foundational, parallelizable internally)

| # | PR | Depends on |
|---|---|---|
| N1.A1 | **Gitea API helper package** (`internal/gitserver/weaveapi/`). Wrappers around label set/clear, project-board column ops, issue create/update, sticky-comment auto-update. | Spike 1, 2 |
| N1.A2 | **First-run setup orchestrator.** Idempotent function that brings a project to v2-ready state: mirror, label sets (state + priority + source), project board with `todo` column, issue templates, pre-commit hook, `.ycode/loom.yaml` with auto-detected defaults. Tracks completion in the yaml so a retry skips done steps. | N1.A1 |
| N1.A3 | **Atomic-claim algorithm.** Implements the (priority_tier, board_position, created_at, issue_number) sort and the optimistic-concurrency claim. Lives in `pkg/loom`. | N1.A1, Spike 2 outcome |

### Group B — CLI scaffolding (after N1.A1)

| # | PR | Depends on |
|---|---|---|
| N1.B1 | **`ycode weave` cobra command + subverb skeleton.** All 10 subverbs registered; each delegates to a handler (may be unimplemented stub). Cobra `Short`/`Long` per repo conventions. | — |
| N1.B2 | **Agent-friendly CLI conventions infra.** Envelope marshaler with versioned schema, exit code constants, tty detection, `YCODE_AGENT` env handling, hint-stream integration with `internal/shell/agentmode/`. Shared utility used by every subverb. | N1.B1 |
| N1.B3 | **`weave add` + `weave list` + `weave next`.** First three subverbs; easiest to implement and most useful for testing. | N1.B2, N1.A1 |
| N1.B4 | **`weave start` with no-arg claim + tool resolution waterfall.** Calls atomic-claim, then `wrap --loom=auto`, then exec. Load-bearing CLI verb. | N1.B3, N1.A3, N0.7 |
| N1.B5 | **`weave prio` + `weave abandon` + `weave shell` + `weave open` + `weave reset`.** Remaining subverbs; mostly thin wrappers over MCP/Service. | N1.B2, N1.A1 |
| N1.B6 | **`weave pull`.** Fast-forward from local Gitea to user's checkout. Includes stash-on-uncommitted-edits handling and the "refuse to silently rewrite committed divergence" guard. | N1.B2 |

### Group C — MCP collab verbs (parallelizable with B)

| # | PR | Depends on |
|---|---|---|
| N1.C1 | **`weave_add` + `weave_prioritize` MCP tools.** Available to both sub-agent and orchestrator roles. Thin wrappers over the API helpers. | N1.A1 |

### Group D — Sandbox isolation (parallelizable, after Spike 3)

| # | PR | Depends on |
|---|---|---|
| N1.D1 | **Reference-clone sandbox preparation.** Modify gitea backend's `PrepareSandbox` to use `git clone --reference`. Includes coordination guard against `gc` on the parent. Sandbox-isolation invariant becomes load-bearing here. | Spike 3 |

### Group E — Defense-in-depth (after B)

| # | PR | Depends on |
|---|---|---|
| N1.E1 | **Pre-commit hook installer.** Layer 3 — refuses agent-author commits in user's working tree. | N1.A2 |
| N1.E2 | **selfinit refusal hook.** Layer 2 — integrated tools refuse to run unmanaged when cwd is a Loom-managed repo and `YCODE_LOOM_ID` is unset. | N1.A2 |
| N1.E3 | **Merger committer-allowlist guard.** Layer 4 — refuses fast-forward past commits with unexpected committers. | — |

### Group F — Default-on flip (last)

| # | PR | Depends on |
|---|---|---|
| N1.F1 | **`ycode wrap --loom=auto` becomes default.** Flip the flag default to on. Remove the `experimental` warning. Ship `local` backend behind config (or defer to N+1.5 if it grows beyond a single PR). | All of N+1 prior |

**End-of-N+1 state:** the full design ships and matches the runbook.

## N+2 — Cleanup

| # | PR | Depends on |
|---|---|---|
| N2.1 | **Deprecate v1 MCP verbs.** Header warnings shipped throughout N+1; in N+2 remove the handler bindings. Go package API stays for internal callers. | All of N+1 |
| N2.2 | **Documentation pass.** Update `docs/loom.md` to point at v2 as current and v1 as historical; update `usage.md`, `architecture.md`, `selfinit.md` for the new flows. | N2.1 |
| N2.3 | **(Optional) `local` backend GA.** If deferred from N+1, ships here. Reference-clone + local-bare convergence, no Gitea, no merger. | N1.D1 |

## Risks I want to flag explicitly

- **Foreman/worker interaction.** Loom v2 is meant to be the workspace substrate for foreman too. Need to verify N+0 doesn't break the foreman→worker handoff that already exists in `cmd/ycode/foreman.go`. Likely needs a small foreman-side update somewhere between N0.7 and N1.B4. Add to the PR list once N+0 is in flight.
- **MCP session-liveness detection.** The doc says "heartbeat-via-session liveness, not last-verb-call idle." The MCP server has to expose session close events to `Service.touchLease`. Need to confirm the MCP server framework supports this; if not, a fallback heartbeat verb may be unavoidable. Audit during N0.5.
- **`weave wait` verb.** The runbook's programmatic variant uses `weave wait --all --timeout 1h --json`. Not currently in the subverb list. Either add as N1.B5.5 or document the polling fallback in the runbook.
- **First-run setup atomicity.** ~8 steps; if one fails mid-way (network, Gitea down, write fail), the partial state needs to be either resumable or torn-down. Idempotent-at-every-step is the lean; track completion in `.ycode/loom.yaml`.
- **`weave pull` and submodules.** The dhnt umbrella case. Multi-repo orchestration is open-question territory; for v2 the doc says single-repo only. Worth a runbook clarification.
- **Backwards compatibility for `cmd/ycode/loom.go`.** This top-level command exists today; v2 reframes it as the "substrate-admin" surface for developers. We should not break its existing usage. Decide in N0.1 whether to keep its current verbs or repurpose.

## Open questions deferred to in-flight resolution

- Atomic-claim retry-bound: how many retries before exit-4 ("queue contention")? Default 5; revisit if real workloads see frequent contention.
- Project-board column for `proposed` state: create at first-run unconditionally, or only when `agent_filed_default_state: proposed` is set in config? Lean toward unconditional (the column is cheap, presence doesn't change semantics unless config flips).
- Sticky-comment update cadence: how often should the loom-process delta be refreshed in the issue comment? Default 30s; revisit if Gitea API rate limits bite.
- Hint engine integration: where does the agent-mode hint engine in `internal/shell/agentmode/` get the weave-specific hint patterns from? Either inline in `cmd/ycode/weave.go` or a new `internal/shell/agentmode/weave.go`. Decide in N1.B2.

## Sequencing recommendation

1. **Spikes 1, 2, 3 in parallel.** Half a day each; doable in a single sitting.
2. **N+0 in order.** Each PR is small and unblocks the next; no parallelism needed.
3. **N+1 Group A → B/C/D/E in parallel → F.** A is foundational; everything else fans out off A; F is the cap.
4. **N+2 after a real-world shakedown** of N+1 in someone's daily workflow. Don't rush the deprecation.

## References

- [`loom-v2-plan.md`](./loom-v2-plan.md) — design and rationale.
- [`weave-runbook.md`](./weave-runbook.md) — end-to-end user walkthrough.
- [`loom.md`](./loom.md) — v1 contract (current implementation).
