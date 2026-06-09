# Loom v2 — Implementation plan

Status: implementation plan. Companion to [`loom-v2-plan.md`](./loom-v2-plan.md) (design) and [`weave-runbook.md`](./weave-runbook.md) (end-to-end UX).

This plan turns the design into a sequenced PR backlog with explicit dependencies, parallelization markers, and risk callouts. It maps directly to the migration phases in the design doc (N+0 / N+1 / N+2) and leans on the principle: **ship the smallest unit that can stand on its own at every step, never break v1.**

## Overall shape

| Phase | Goal | Surface ships? | Estimated PR count |
|---|---|---|---|
| **N+0** — Foundation | Internal substrate ready; v2 MCP verbs alongside v1 | Agent surface (MCP) only; no `weave` CLI | 5–7 |
| **N+1** — Front door | `ycode weave` CLI, auto-attach default-on, Gitea bootstrap, full UX | Everything in the design | 10–14 |
| **N+2** — Cleanup | Remove deprecated v1 verbs, doc pass | API hygiene only | 2–3 |

## Three early spikes — outcomes

All three were executed before N+0 work. Outcomes recorded here drive the PR sizing below.

### Spike 1 — Gitea project-board API → **YELLOW (path)**

**Investigated:** Whether Gitea 1.26.1 (embedded) exposes kanban project-board read/write via REST.

**Finding:** Project boards have **no v1 REST API**. Confirmed by reading `external/gitea/routers/api/v1/api.go` and `templates/swagger/v1_input.json` — no `/projects/*` routes. The web UI drives boards via HTML routes under `/<owner>/<repo>/projects/*` (`external/gitea/routers/web/web.go:1505-1530`): `POST /new`, `POST /columns/new`, `POST /{id}/{columnID}/move`, `POST /issues/{index}/projects/column`. These require **session-cookie auth and CSRF tokens**, and use HTML form encoding — usable but brittle.

**Resolution:** Project board becomes opt-in via `ycode weave init-board` (one-time, web-route based, will accept CSRF + session complexity once). Loom does **not** auto-sync cards on state changes — labels are the source of truth, and the default dashboard is the label-filtered issue list (which uses only the stable v1 REST API). Drag-to-reorder within a tier is dropped as a priority dimension; coarse tier (p0/p1/p2/p3) plus FIFO is enough.

### Spike 2 — Optimistic-concurrency for atomic claim → **GREEN (in-process mutex)**

**Investigated:** Whether Gitea provides ETag / If-Match / CAS on label set, and whether the `AddIssueLabels` endpoint is concurrency-safe.

**Finding:** `POST /repos/{owner}/{repo}/issues/{index}/labels` (`external/gitea/routers/api/v1/repo/issue_label.go:68`) is idempotent and returns the full label set after add; no ETag, no `If-Match`, no CAS primitive. `issue_service.AddLabels` dedupes inserts but doesn't surface "I added it" vs "it was already there" to the caller.

**Resolution:** No external lock store needed. All `weave start` callers route through one `ycode serve` Service instance via MCP, so an in-process **`sync.Mutex`** (per-project, via `sync.Map[projectID]*Mutex`) is sufficient for cross-client atomicity. Service restart recovery reads existing `loom:working` labels from Gitea (durable) and excludes already-claimed candidates. SQLite-backed claim lock — considered as the original fallback — explicitly rejected as unnecessary moving part.

### Spike 3 — Reference clone inside embedded Gitea → **GREEN**

**Investigated:** Whether `git clone --reference <bare> <url>` yields per-clone refs/index/stash with shared object store.

**Finding:** Empirically verified on a local bare + two reference children. `alternates` file points correctly at parent's `objects` dir; per-clone refs/HEAD/index/stash/reflog all isolated; child commits stay private until pushed (parent's `cat-file` returns "could not get object info" for a child-only SHA); child `.git/objects` directory is ~16K (essentially empty alternates pointer file). Sandbox-isolation invariant fully achievable.

**Known constraint:** Never `git gc` the parent bare while children are alive — child-only objects could be pruned if they were referenced only via alternates. Mitigation: Loom tracks active leases and either delays gc or disables auto-gc on the parent. Documented as operational constraint for N1.D1.

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
| N1.A1 | **Gitea API helper package** (`internal/gitserver/weaveapi/`). Wrappers around label set/clear, issue create/update, sticky-comment auto-update — all v1 REST. Smaller scope than originally planned (no project-board helpers; those go to N1.G1). | Spike 1 |
| N1.A2 | **First-run setup orchestrator.** Idempotent function that brings a project to v2-ready state: mirror, label sets (state + priority + source), issue templates, pre-commit hook, `.ycode/loom.yaml` with auto-detected defaults. Tracks completion in the yaml so a retry skips done steps. Does **not** create a project board. | N1.A1 |
| N1.A3 | **Atomic-claim with Service mutex.** Per-project `sync.Mutex` (via `sync.Map[projectID]`) in `pkg/loom.Service`. Sort: (priority_tier, created_at, issue_number). Apply `loom:working` label inside the lock. Recovery on restart: read existing `loom:working` labels and exclude from candidates. No external store. | N1.A1 |

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

### Group G — Opt-in kanban (after F, fully optional)

| # | PR | Depends on |
|---|---|---|
| N1.G1 | **`ycode weave init-board`.** Web-route-based one-time kanban bootstrap. Mints a session cookie via `/user/login` POST, extracts CSRF token, creates project + columns via `POST /<repo>/projects/new` and `POST /<repo>/projects/{id}/columns/new`. Encapsulated in a dedicated `internal/gitserver/weaveboard/` package so the CSRF/session complexity is contained. Loom does **not** auto-sync cards after this; cards drift unless the user manually moves them. | N1.A2 |

**End-of-N+1 state:** the full design ships and matches the runbook. Kanban is opt-in via G1.

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

## Shipped status (running log)

Captured here for reference. Each line is the PR commit + a sentence.

**N+0 — Foundation (complete):**

- `00211c0` N0.1 — Lease-store path unification via `pkg/loom.DefaultLeasePath` helpers.
- `21be522` N0.2 — `Service.SubmitAndWait` + `Service.Rebase` (block-with-deadline + auto-rebase contract).
- `0c38534` N0.3 — `Service.Watch` event channel with drop-on-overflow pub-sub.
- `c43d537` N0.4 — `PolicyLoom` wiring in `internal/service/workspace.go` via pluggable `LoomLeaser`.
- `009d5e5` N0.5+6 — six v2 MCP verbs (loom_checkpoint/submit/abandon + open/terminate/handoff) + `loom://session`/`project` resources.
- `c30cb12` N0.7 — `internal/runtime/wrap.LoomLeaser` seam for `--loom=auto` auto-attach.

**N+1 — Front door (Groups A–G complete except G1's CSRF flow):**

- `9f49699` N1.A1 — `internal/gitserver/weaveapi`: label namespace (state/priority/source) + sticky-comment ops, 6 new Gitea client methods.
- `b4a15fc` N1.A2 — `internal/gitserver/weavesetup`: idempotent first-run orchestrator (mirror + labels + hook + config).
- `785841e` N1.A3 — Atomic claim via per-project `sync.Mutex` + `Backend.ClaimNextIssue` (priority-tier sort).
- `1da4465` N1.B1+B2 — `ycode weave` cobra skeleton with 11 subverbs + `internal/cli/weavecli` envelope/exit-code/agent-mode infra.
- `8ab3f22` N1.C1 — `weave_add` + `weave_prioritize` MCP collab verbs.
- `3af3563` N1.D1 — Reference-clone sandbox seam (opt-in `UseReferenceClone` config).
- `7e89f58` N1.E2+E3+F1 — `IsLoomManaged`/`IsAttached` defense helpers; merger committer-allowlist guard; `wrap --loom` default-on.
- `040e7a3` N1.G1 — `internal/gitserver/weaveboard.Bootstrap` scaffold (CSRF/session flow deferred to a focused follow-up PR).

**N+2 — Cleanup:**

- `040e7a3` N2.1 — v1 MCP verb descriptions prefixed `DEPRECATED (v1; use <v2>)`; backwards-compat preserved this release.
- `040e7a3` N2.2 — `docs/loom.md` banner cross-linking the three v2 docs, body preserved as historical reference.
- (pending) N2.3 — Optional `local` backend GA. `internal/gitserver/loomlocal` package shape lands as scaffold; production logic deferred to a focused follow-up when a real user requests it.

### Body-shipped delegation summary (Group B subverbs)

`ycode weave` registers the 11 subverbs and the agent-friendly envelope conventions per N1.B1+B2. The subverb RunE bodies in `cmd/ycode/weave_subverbs.go` are stubs that emit `precondition_failed` envelopes citing the relevant N+1 PR. Each stub will be activated as its substrate dependency lands; the contract for foreign-agent consumers (envelope shape, exit codes, hint stream) is stable and testable today.

### What's load-bearing for "ready to actually use":

- Forge backend (default) — wired end-to-end via N+0 + N+1 Groups A/C/D/E/F. ✅
- `weave start` subverb body — needs to chain `Service.Claim` → `Service.Lease` → `wrap.Run(LoomLeaser)`. Substrate seams present (N1.A3 + N0.7); orchestration body is the open piece.
- `weave init-board` web-route POST flow — N1.G1 scaffold present; production impl deferred.
- `local` backend production logic — N2.3 scaffold present; deferred.

## References

- [`loom-v2-plan.md`](./loom-v2-plan.md) — design and rationale.
- [`weave-runbook.md`](./weave-runbook.md) — end-to-end user walkthrough.
- [`loom.md`](./loom.md) — v1 contract (current implementation).
