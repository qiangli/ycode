# ycode Strategy

> The wedge, the funnel, and the discipline. Read before every sprint.

This is the **living strategy doc** that aligns work to the wedge → proof → on-ramp → ergonomics funnel. It is not an ephemeral plan. Every PR, sprint, and roadmap change consults this document first.

## How to use this document

- **Before suggesting a new feature**, check the [wedge](#the-wedge-standing-out-in-a-crowded-market) and [feature tiers](#mechanism-to-enforce-the-floor-feature-tiers-via-go-build-tags). Anything that doesn't feed the wedge or doesn't meet graduation criteria stays out of the default build.
- **Before starting a sprint**, re-read [Operating principles](#operating-principles-working-smart-in-a-lightning-market). Update the [Status table](#status-updated-each-sprint).
- **Before a release**, run the [credibility floor checklist](#verifying-credibility-floor-before-tier-1-begins).
- **When competitors ship something noteworthy**, decide within 7 days per [Operating principle 3](#3-track-competitors-weekly-decide-within-7-days).

## Related documents

- `docs/roadmap.md` — the existing **feature gap inventory** (P0/P1/P2 items distilled from gap analyses). Tactical; this document is strategic. Both stay.
- `docs/leaderboards.md` — detailed leaderboard targeting (SWE-bench Pro/Verified, Aider Polyglot, Terminal-Bench): cost, infrastructure, submission process. Phase 1 of this doc's roadmap depends on it.
- `docs/autonomous-loop.md` — the RESEARCH→PLAN→BUILD→EVALUATE→LEARN loop that underpins the [Option B "self-improving"](#option-b-the-ai-agent-that-learns-from-its-own-work) wedge layer.
- `docs/architecture.md` — system architecture. Strategic decisions here override architectural conventions there.

## Alignment with prior decisions

`docs/roadmap.md` already lists "Electron desktop app" as **Decided Against / Deferred Indefinitely** with the reason "CLI-first; web UI covers GUI needs." This document confirms and extends that position — see [The UI architecture: embedded web, no Electron](#the-ui-architecture-embedded-web-no-electron). The portal vision uses the embedded web UI path, not Electron.

---

## Context

The AI coding agent market is crowded: Claude Code, Cursor, Cline, Aider, Codex CLI, Continue, Windsurf, Roo Code, Augment, Devin, plus a half dozen new entrants every month. Developers have **too many options**, all roughly capable, all roughly the same shape. Generic "polish + more features" cannot win — every competitor is doing that.

ycode already has more raw capability than the popular ones (50+ tools, 5-layer memory, embedded Ollama/Gitea/SearXNG, agent swarms, code knowledge graph, multi-provider, single static Go binary). Feature-count is **not** the gap. The gap is:

1. **No wedge.** Without a sharp positioning that names something the others can't do, ycode is "another AI coding agent."
2. **No proof.** No public benchmark scores, no demo videos, no leaderboard presence — investors and developers can't verify the depth that's already there.
3. **No on-ramp.** Build-from-source onboarding kills evaluation; no editor extension means devs never even see it.

This document picks **one wedge** ycode can credibly own, then uses leaderboards as the proof channel and tier-1 ergonomics as the on-ramp. Most tier-1 work is **surfacing and polishing what already exists**, not building new features.

---

## The wedge: standing out in a crowded market

Pick **one** positioning, drive everything else through it. Three honest options for ycode, in decreasing order of defensibility:

### Option A (recommended): "The local-first AI coding agent. One binary. Runs offline. Yours forever."

What it claims:
- Single static Go binary, ~30 MB, no Python, no Node, no Docker required to start
- Runs **fully offline** with embedded Ollama once a model is pulled — air-gap-ready
- Embeds its own git server (Gitea), search (SearXNG), inference (Ollama), and observability (Perses) — zero external services for a private deployment
- Permissive-licenses only (MIT/Apache/BSD/ISC/MPL) — enterprise legal will sign off

Who flocks: enterprise (security/compliance/legal), regulated industries (finance, health, defense, gov), security-conscious indie devs, anyone whose code can't leave the laptop, anyone on a flight, anyone behind a corporate firewall, anyone who's been burned by SaaS dependency.

Why it's defensible: **every popular competitor is cloud-tied or runtime-heavy.** Cursor is closed-source SaaS. Claude Code requires Anthropic API + Node. Aider needs Python + API. Cline needs VS Code + API. None of them can credibly claim "fully offline, single binary, no runtime." ycode can — and the codebase already supports it. This is an unoccupied premium position, not a feature race.

### Option B: "The AI agent that learns from its own work."

What it claims: the autonomous RESEARCH→PLAN→BUILD→EVALUATE→LEARN loop is real (`docs/autonomous-loop.md`), the 5-layer memory and FIX/DERIVED/CAPTURED skill evolution actually backpropagates rewards. Self-improving over time.

Why it's risky: hard to prove in a 30-second demo, easy for skeptics to dismiss as marketing. Strong if paired with leaderboards showing improvement curves over time. Weak as a standalone wedge.

### Option C: "The agent orchestration backbone for teams of agents."

What it claims: agent swarms, NATS mesh, parallel agents, handoffs, cron — ycode is the kubernetes-of-agents.

Why it's risky: the "teams of agents" market is speculative — most developers want one good agent, not ten. Strong with researchers and infra teams, weak with the median dev. Reserve as a B2B/research narrative; not the consumer-dev wedge.

**Recommendation: lead with A, layer B as the "and it gets better over time" story, keep C for enterprise sales decks.** Option A is the only one that's both true today, hard to copy, and resonates with a real underserved segment.

**Long-term ceiling (Option A+):** as Option A matures, the wedge naturally expands from "local-first coding agent" to "**local-first developer cockpit** — code, ops, comms, knowledge. One binary. Runs offline. Your data stays yours." See the [Portal vision](#portal-vision) section. This is **not the immediate wedge** — earning category-defining language requires being undeniably best at coding first. But it's the 10-year ceiling that justifies the long-term investment, and it makes the local-first story dramatically sharper: "your code stays local" is a moat; "your code, email, calendar, drive, and reading list all stay local" is a category.

Everything in the rest of this document should feed the wedge: leaderboards prove it, the install path showcases it, the editor extension surfaces it, the cost UX makes the offline mode discoverable.

---

## Developer daily concerns → ycode gaps

| Daily pain | What developers actually feel | ycode today | Gap |
|---|---|---|---|
| "Will it break my code?" | Anxiety on every long task | Has VFS boundaries, permission tiers | No diff-preview, no per-tool checkpoint+rollback, plan-first not first-class |
| "How much is this costing me?" | Watching the bill tick | Has token tracking | No live $ meter, no budget cap, no auto-router cheap↔expensive |
| "I live in VS Code / JetBrains / Neovim" | Tab-switching to a TUI is friction | TUI + HTTP server, no editor plugin | **Largest single adoption lever** |
| "Installing took an hour" | `make init` + submodules + gzip is a non-starter | Source build only | No `brew`, no `go install`, no prebuilt release artifacts surfaced |
| "It forgot what we were doing" | Lost context between sessions | Episodic memory exists | No `ycode resume <id>`, no per-branch session, no PR-as-thread |
| "Tests broke and it didn't notice" | Manual test/lint after every change | Has bash, can run tests | No auto-watch loop, no auto-revert on red |
| "I want to steer mid-flight" | Restart-or-abort is the only option | Can interrupt | No inline correction / soft-nudge UX |
| "Why did it do that?" | Black-box reasoning | Has OTEL, JSONL transcripts | No replay UI, no "why" annotations on tool calls |
| "I trust nothing without proof" | Need benchmarks, examples, demos | Eval harness exists | No public leaderboard, no demo GIFs, no comparison page |
| "Slack/Linear/Sentry are where my work starts" | Triage happens outside the editor | Slack/Matrix/Email adapters **stubbed** | Finish the chat adapters; add Sentry/Linear MCP |

---

## Precondition: cut the half-baked, ship only the proven

In a crowded market, **a bundle of half-cooked features is worse than a smaller bundle of finished ones.** Developers comparing 10 options dismiss any tool where the first thing they try doesn't work. One stub poisons the whole impression — they don't say "interesting, but that one feature is rough," they say "another half-finished agent" and close the tab. Investors do the same with one extra step: they ask power users, hear "it has a lot of stubs," and pass.

This precondition has to land **before** the tier-1 levers, because everything else (leaderboards, VS Code extension, demo GIFs, README rewrite) just amplifies whatever surface area exists today. Amplifying half-finished surface area is negative ROI.

### Known half-baked surface (audit baseline as of this doc's first commit)

| Item | State | Action |
|---|---|---|
| Slack / Matrix / Email chat adapters | Return "not implemented" (`internal/chat/adapters/`) | Either finish Slack (highest value) and cut the others, or demote all three behind `experimental` build tag |
| Trainer agent | No-op placeholder in `wire.go` | Demote to `wip`; cut from docs/tool list until real |
| Sprint two-stage review | Scaffolded, callbacks not wired | Either finish or remove from `docs/autonomous-loop.md` claims |
| Mesh auto-start in conversation runtime | Wired in `wire.go` but doesn't auto-engage | Either auto-engage or stop advertising mesh in the main loop |
| TUI permission flow | `_ = reqID // TODO` in `internal/cli/tui.go` | Wire it; permission UX is in tier-1 anyway |
| 50+ advertised tools | Some are battle-tested, some are demo-ware | Audit each; demote the bottom decile to `experimental`; mark the rest `stable` |

### The rule going forward

**Nothing ships in the README, leaderboard demo, or VS Code extension until it has:**
1. An integration test that exercises the happy path
2. Hands-on dogfooding — used by ycode itself, on a real ycode PR, with the result visible in `git log`
3. A documented failure mode (what happens when it breaks, how the user recovers)

Anything that fails 1–3 either gets finished now or demoted behind a build tag. **Demote, not "TODO later."** A `TODO` in code is fine; a `TODO` in the marketing surface is fatal.

### Reframing the wedge with this lens

Once the half-baked is gated, ycode's positioning sharpens further: "**fewer features than Cursor, but every one of them is battle-tested and runs offline on a single binary.**" In a market drowning in feature sprawl, "less but proven" is itself a differentiator. Cursor and Claude Code already won by refining a small core; ycode can win the underserved offline/local segment with the same discipline.

### Mechanism to enforce the floor: feature tiers via Go build tags

We do not "cut" half-baked code by deleting it — we **demote it behind a Go build tag** so the default release binary excludes it, while development builds still have access. This makes the credibility floor mechanically enforced rather than a matter of discipline.

#### Three tiers, three build tags

| Tier | Build tag | In default `make build`? | Meaning |
|---|---|---|---|
| `stable` | (none) | Yes — always compiled | Integration-tested, dogfooded ≥2 weeks, failure modes documented, in README and benchmarks |
| `experimental` | `experimental` | No (opt-in) | Compiles, has tests, but new or rough; emits a one-line stderr warning on first invocation per session; not in README; not in benchmarks |
| `wip` | `wip` | No (opt-in) | Active development; may not work; not on default CI; not in any user-facing surface |

#### Source of truth: `internal/features/registry.yaml`

```yaml
features:
  - name: ollama-runtime
    tier: stable
    files: [internal/inference/]
    graduation:
      integration_test: true       # has TestIntegration* in evals/specs/
      dogfood_weeks: 4             # mentioned in git log over ≥4 weeks
      failure_mode_doc: true       # entry in docs/failures.md
  - name: slack-adapter
    tier: experimental
    files: [internal/chat/adapters/slack/]
    blocked_by: [chat-hub-routing]
    graduation:
      integration_test: false
      dogfood_weeks: 0
      failure_mode_doc: false
  - name: trainer-agent
    tier: wip
    files: [internal/mesh/trainer/]
    notes: "no-op placeholder; do not surface"
```

#### Code pattern

Experimental package files carry a build tag:

```go
//go:build experimental
// +build experimental

package slack
```

Tool registration uses split files so the registry call is a no-op when the tag is absent:

```go
// internal/tools/registry_experimental.go
//go:build experimental
package tools
func registerExperimentalTools(r *Registry) { RegisterSlackHandlers(r); ... }

// internal/tools/registry_experimental_stub.go
//go:build !experimental
package tools
func registerExperimentalTools(r *Registry) {} // no-op
```

The main registry always calls `registerExperimentalTools(r)`; what it does depends on the build tag.

#### Make targets

```makefile
build:               # release default — stable only
	go build -tags "sqlite,sqlite_unlock_notify,bindata" -o bin/ycode ./cmd/ycode/

build-experimental:  # internal testing — stable + experimental
	go build -tags "sqlite,sqlite_unlock_notify,bindata,experimental" -o bin/ycode ./cmd/ycode/

build-wip:           # development — everything
	go build -tags "sqlite,sqlite_unlock_notify,bindata,experimental,wip" -o bin/ycode ./cmd/ycode/

verify-features:     # CI gate: registry vs. code vs. graduation criteria
	go run ./cmd/ycode features verify
```

Existing `make build` keeps the same name; only the **content** narrows to stable. `make build-experimental` is what daily contributors use.

#### CI enforcement

A new workflow `verify-feature-registry`:

1. Parses `internal/features/registry.yaml`.
2. For every `tier: stable` entry, asserts:
   - `evals/specs/<feature>/` contains a passing integration test
   - `git log -- <feature.files>` shows commits over ≥`dogfood_weeks` weeks
   - `docs/failures.md` contains an `## <feature>` section
3. For every `tier: experimental` entry, asserts the build tag is actually present in declared files.
4. Fails the `release` workflow if any of the above fail.

Release artifacts (GitHub Releases, brew, npm wrapper) only ship the default build. A user who runs `brew install ycode` cannot accidentally hit a stub.

#### Runtime UX

- `ycode features list` — prints the compiled-in tiers and per-feature status
- `ycode --version` — shows tier set: `ycode 0.4.0 (stable)` for releases, `ycode 0.4.0-dev (stable+experimental)` for local builds
- First invocation of an experimental tool in a session: stderr line `[experimental] <name>: not yet stable, may break — see docs/strategy.md#feature-tiers`
- README's tool catalog is **auto-generated** from the registry, filtered to `tier: stable` — impossible to advertise something that hasn't graduated

#### Graduation flow

A feature graduates from `wip` → `experimental` → `stable` via PR:

1. Author updates `registry.yaml`, flips tier
2. Author moves/removes the build tag in source files
3. CI runs `verify-features`; PR fails if criteria don't pass
4. Reviewer confirms; merge graduates the feature

Demotion uses the same flow in reverse — if a stable feature regresses (tests break, real-world failure), demote with a PR; the next release silently drops it from the surface. No bigger commitment than reverting a flag.

### Verifying credibility floor before tier-1 begins

A simple pre-flight checklist before launching any tier-1 lever:
- [ ] Run `ycode doctor` on three fresh machines (mac, linux, win) — passes with zero TODOs
- [ ] First-run wizard completes a real task on a real public repo end-to-end with no manual intervention
- [ ] Every tool in the README has a corresponding eval entry under `evals/specs/`
- [ ] No file in the project contains a `TODO` that affects an advertised user-facing feature
- [ ] Public-facing docs match the actually-shipped behavior, line by line

Until those check, no leaderboard submission, no marketplace publish, no investor deck. The cost of being seen "early-but-rough" by the market is months of recovery; the cost of waiting two extra weeks for the floor is nothing.

---

## Recommended approach: 6 tier-1 levers

The wedge is positioning. These six are how the wedge converts to adoption. Each maps to existing scaffolding so we're amplifying the codebase, not rewriting it.

### 0. Public leaderboards + visible proof — the dual-audience wedge

**Why it's tier-1, not tier-2.** Developers in a crowded market don't trust marketing — they trust numbers. Investors don't read changelogs — they read leaderboards. A single SOTA score on SWE-bench Verified gets:

- Front page of HN for a day (free traffic spike, install spikes 10–100×)
- Twitter/X coverage in the AI agent thread that runs every week
- Coverage in newsletters (Latent Space, The Sequence, Import AI)
- Recognition by AI infra investors who specifically watch these benchmarks
- A permanent line on the leaderboard page that anyone evaluating tools will see

Each successive improvement = another news cycle. Leaderboards are a **renewable PR resource**, and ycode has the eval infrastructure to compete (`evals/specs/`, `make eval-all-evals`).

**What to ship:**

1. **SWE-bench Verified submission** (the one that matters most). Aim for a credible top-15 score initially, iterate. Already on the roadmap (`docs/autonomous-loop.md` Future Work #8) — just ship it.
2. **Aider polyglot benchmark** — popular among the agent crowd, smaller surface, easier first win.
3. **Terminal-Bench** — plays to ycode's CLI-native strength.
4. **Custom benchmark that nobody else can win: "offline mode SWE-bench."** Same SWE-bench tasks, but the agent runs **with no internet access, only embedded local Ollama.** Almost no competitor can submit. ycode owns this category by construction. *This is the leaderboard angle that proves the wedge.*
5. **Public dashboard at ycode.dev/benchmarks** — live scores, trend lines over time (proves the "self-improving" story from positioning Option B), cost-per-task metrics, per-model breakdown. Generate it from `make eval-all-evals` output on each release.
6. **Leaderboard badge on README** — shields.io style, auto-updated. Every GitHub visitor sees the score before reading anything else.

**Investor-visibility extras:**
- Public usage trajectory (opt-in telemetry, anonymized): sessions/week, retention, completion rate
- "Built with ycode" page: ycode commits merged by ycode itself, visible in `git log` (eat your own dog food, prove it works)
- Live cost-per-task on the dashboard — investors care about unit economics, this is the metric they ask about
- Comparison page: ycode vs. Cursor/Claude Code/Aider/Cline on score, cost-per-task, latency, offline support. Honest about losses; honest about wins.

**Entry points:**
- `evals/specs/` already has SWE-bench / Aider / Terminal-Bench harnesses. See `docs/leaderboards.md` for cost/infra detail.
- `make eval-all-evals` Makefile target exists
- New: `.github/workflows/benchmarks.yml` to run on each tagged release and publish results
- New: `ycode.dev` static site (or GitHub Pages) generated from eval JSON
- README badge: trivial

**Cost:** maybe 2–4 weeks of focused eval-tuning + one weekend for the dashboard. Highest ROI line in this entire plan.

### 1. VS Code extension (and Neovim/JetBrains via the same protocol)

**Why it matters most.** 70%+ of professional devs use VS Code. A TUI-only product self-selects to terminal natives. ycode already runs an HTTP/WebSocket server (`ycode serve`); the extension is a thin client over that.

**What to build:**
- Sidebar panel: chat, plan, diff preview, tool log
- Inline `Apply` / `Reject` buttons on every proposed edit (code lens)
- Status bar: model, session cost, current tool, agent state
- Reuses existing permission flow over WebSocket

**Entry points:**
- Server side already lives in `internal/cli/serve.go` and the WebSocket layer
- Permission integration TODO already noted in `internal/cli/tui.go` (`_ = reqID // TODO: wire RespondPermission via client`)
- Existing Playwright e2e harness in `e2e/` validates the wire protocol — extension can reuse it
- New repo: `ycode-vscode/` (separate marketplace publish cadence)

Neovim and JetBrains piggy-back on the same JSON-RPC the extension uses — one protocol, three clients.

### 2. Cost UX: live meter, budgets, auto-router

**Why it matters.** Token cost is the #1 anxiety for daily AI-agent users. Claude Code's flat-rate Pro plan was the single biggest reason it captured the market — it removed the meter. ycode is BYOK by design (correct), so the answer is **transparent, controllable cost**, not hidden cost.

**What to build:**
- Live cost meter in TUI status bar (in-tokens × price + out-tokens × price, session running total, cache-hit % from Anthropic response headers)
- Soft budget cap per task: `--budget 5.00` → prompt-to-continue when crossed
- Auto-router: cheap model (Haiku / local Ollama) for read/list/grep, expensive (Opus/Sonnet) for design/edit. ycode already has model aliases and the Ollama runner — wire the routing decision into `internal/api/provider.go`
- "Why this model?" hover — surfaces routing decision

**Entry points:**
- Token tracking already in `internal/api/` — surface it
- Model aliases: `internal/api/provider.go`
- Embedded Ollama: `internal/inference/` — already auto-downloads; just route to it for cheap ops
- TUI status bar: `internal/cli/tui.go`

### 3. Trust UX: plan-first default, per-tool checkpoint, one-key rollback, dry-run

**Why it matters.** The biggest reason a developer won't let an agent run unattended is fear of unrecoverable state. Most existing tools handle this by being timid (always asking). The better answer: **make recovery trivial so boldness is safe.**

**What to build:**
- Auto git-stash before each `write_file`/`edit_file`/`bash` mutation; tag with tool-call ID
- TUI keybind: `u` to undo the last tool call, `U` to roll the entire session back to a stash point
- Plan-first mode promoted to a first-class workflow (not just a permission level): every multi-step task produces a plan in the TUI, edits are gated on approval
- Dry-run: `--dry-run` runs the loop but every mutation tool returns a planned diff instead of executing
- Auto-revert on test red: if `make test` was passing and now fails after a tool call, offer one-key revert

**Entry points:**
- Permission tiers: `internal/runtime/permission/`
- Native go-git already has stash: `internal/runtime/toolexec/` (31 NativeFuncs)
- Plan mode infrastructure exists — generalize it
- Tool dispatch: `internal/runtime/conversation/runtime.go`

### 4. Onboarding cliff: prebuilt binaries, one-line install, first-run wizard

**Why it matters.** Discovery → first useful run is where 90% of would-be users drop off. `make init && make build` is fatal for a "let me try it" developer. The README quickstart is for contributors, not users.

**What to build:**
- GitHub Releases with prebuilt binaries for darwin/linux/windows × amd64/arm64 (`make cross` already produces these — just publish on tag)
- Homebrew tap: `brew install ycode/tap/ycode`
- `go install github.com/.../ycode/cmd/ycode@latest` path verified
- npm wrapper: `npx ycode` (downloads platform binary on first run — same trick Claude Code uses)
- First-run wizard on `ycode` with no args in a fresh repo:
  1. Detect provider creds, prompt for any
  2. Run `/init` to generate AGENTS.md
  3. Run `repomap` build
  4. Show 3 starter prompts ("fix the failing test", "explain this codebase", "add a new endpoint")
- Landing page: 30-second GIF, one-line install, single command demo

**Entry points:**
- `make cross` already cross-compiles to `dist/`
- `doctor` command exists and validates env — extend it into the wizard
- `/init` skill exists embedded
- README.md needs a top-of-fold rewrite for users (move build/dev docs below)

### 5. Session resume + PR-as-conversation + per-branch continuity

**Why it matters.** Real dev work spans hours, days, branches, PRs. Today's agents reset every session. ycode has episodic memory in `pkg/memex/memory/` — exposing it via session UX turns it from "feature" into "the reason I picked ycode."

**What to build:**
- `ycode resume [<id>]` lists prior sessions and resumes one (full transcript + memory + working files)
- Per-branch session binding: `git checkout feature/x` automatically loads the agent thread that worked on `feature/x`
- PR-as-conversation: `ycode pr 123` opens an agent thread tied to PR #123 — comments, reviews, CI failures all stream in as agent context. This is the workflow Claude Code/Cursor don't really do well.
- Background mode: `ycode --background "fix CI on PR 123"` — agent runs detached, posts status to PR or Slack

**Entry points:**
- Episodic memory: `pkg/memex/memory/` (JSONL sessions already rotate)
- GitHub PR/issue tools: `internal/runtime/github/`
- `ycode serve` + cron: already supports background tasks
- Slack adapter: **finish the stub** in `internal/chat/adapters/` — agent replies in PR threads + Slack are killer features when paired

---

## Tier-2 (differentiators after tier-1 lands)

- **Sentry / Linear / Jira MCP.** ycode already speaks MCP. A "fix this Sentry error" or "implement this Linear ticket" entry point closes the loop from triage → code.
- **Replay / "why did it do this?"** UI. ycode logs OTEL traces — render them as a developer-friendly timeline, not raw Jaeger.
- **Speculative parallelism.** Dispatch independent reads/searches in parallel speculatively before the model finishes streaming. Already have parallel agents — apply the same pattern at the tool level.
- **Watch mode.** Agent runs `make test` / typechecker on file save and self-corrects without prompting.

## Tier-3 (polish)

- Inline correction (edit a tool call mid-flight, don't restart)
- Honest comparison page vs. Cursor / Cline / Aider / Claude Code (feature matrix + honest weak spots)
- Demo GIFs in README, "Built with ycode" examples
- Workspace / multi-repo mode for monorepo orgs
- Web UI for session management (already have HTTP server, missing the UI) — note: this is subsumed by Phase 6 portal work

---

## Critical files to modify (tier-1)

| Lever | Files |
|---|---|
| Leaderboards (proof) | `evals/specs/` (SWE-bench, Aider, Terminal-Bench harnesses), new `.github/workflows/benchmarks.yml`, new `ycode.dev` static site, README badges |
| VS Code extension | New repo `ycode-vscode/`; existing server `internal/cli/serve.go`, WebSocket layer; permission TODO at `internal/cli/tui.go` |
| Cost UX | `internal/api/provider.go` (router), `internal/api/` (token tracking surface), `internal/cli/tui.go` (status bar), `internal/inference/` (cheap-op routing) |
| Trust UX | `internal/runtime/permission/`, `internal/runtime/conversation/runtime.go` (dispatch hook), `internal/runtime/toolexec/` (stash native func), `internal/cli/tui.go` (rollback keybind) |
| Onboarding | `.github/workflows/release.yml` (publish from `make cross`), README.md rewrite, `internal/cli/doctor.go` → wizard, embed `/init` skill |
| Session/PR | `pkg/memex/memory/` (resume API), `internal/runtime/github/` (PR thread tool), `internal/chat/adapters/` (finish Slack stub), new `ycode resume` / `ycode pr` cobra subcommands in `cmd/ycode/` |

## Reuse, don't rebuild

The whole point of this plan: every tier-1 lever has scaffolding already in the repo. Specifically:
- **Permission tiers + VFS boundaries** (`internal/runtime/permission/`) — checkpoint/rollback hooks here, don't reinvent
- **31 native go-git funcs** (`internal/runtime/toolexec/`) — stash/branch primitives already exist
- **Embedded Ollama** (`internal/inference/`) — cheap-op routing target already wired
- **Episodic JSONL sessions** (`pkg/memex/memory/`) — resume is exposing existing data
- **`make cross`** already cross-compiles — release publishing is the missing step, not the build
- **`doctor` command** — extend to wizard rather than building one fresh
- **HTTP/WebSocket server** (`internal/cli/serve.go`) — VS Code extension is a client of this, no new server

## Verification (how we measure "developers flock")

Strategy plans don't ship code, but each lever has a measurable outcome:

- **VS Code extension:** marketplace install count; >5k installs in 90 days = traction signal
- **Cost UX:** session-cost variance reduction; user surveys — "do you trust running ycode unattended?"
- **Trust UX:** % of sessions completed without manual abort; # of rollback invocations (high = feature working)
- **Onboarding:** time from `brew install` to first successful task; <5 min target. README → first command bounce rate.
- **Session/PR:** % of sessions resumed; # of PR threads with agent participation
- **Public leaderboard:** SWE-bench score posted; rank vs. Aider, Cline, Claude Code at the time of submission

End-to-end smoke test for tier-1 once shipped:

```bash
brew install ycode/tap/ycode                           # onboarding
code .                                                  # editor integration
# In VS Code sidebar: "fix the failing test"
# Observe: plan rendered, cost meter ticking, diff preview, Apply button
ycode resume                                            # list sessions
ycode pr 42                                             # PR-as-thread
```

---

## Portal vision

> "The local-first developer cockpit. Code, ops, comms, knowledge — one binary, runs offline, your data stays yours."

The long-term ceiling. **Not the immediate wedge** — we earn this language by being undeniably the best local-first coding agent first. But every architectural decision from Phase 0 onward should be compatible with this destination, not require a rewrite to reach it.

### Why this matters

What developers actually do all day, in rough proportion:
- ~40% writing/reading code
- ~20% communication (Slack, email, PR threads, meetings)
- ~15% ops (deploys, monitoring, incidents)
- ~15% reading (docs, news, research, bookmarks, papers)
- ~10% planning (calendar, todos, memos)

Today's "AI coding agents" address only the 40%. ycode's existing capability set already overlaps with much of the remaining 60% (MCP, GitHub, observability stack, memory, web search) — but it's hidden inside a coding-agent UX. The portal vision is to surface that latent capability through a developer-centric daily-driver experience.

The portal vision **strengthens** the local-first wedge: "your code stays on your laptop" is a moat. "Your code, email, calendar, files, and reading list all stay on your laptop" is a category. Privacy concerns intensify with personal data — and that intensification is on our side.

### The four pillars

Each pillar maps to existing ycode capability + MCP-based integrations + a UI surface:

**1. Code (already the focus)** — coding agent excellence. The foundation. Tier-1 work above is here.

**2. Ops & deploy**
- DigitalOcean, AWS, GCP, Fly.io, Vercel via MCP
- `ycode "deploy this PR to staging"` → agent runs the deploy, watches health, rolls back on failure
- Surface: deploy panel in the UI showing live status, logs, rollback button
- Existing scaffolding: `internal/runtime/toolexec/`, observability stack, `ycode serve` mesh

**3. Communications**
- Email/calendar via MCP (Gmail MCP, Calendar MCP — both exist)
- Slack/Discord via MCP + the existing (currently stubbed) chat adapters
- Surface: triage inbox, calendar view alongside the agent thread; "schedule a focus block to work on this PR"
- Existing scaffolding: `internal/chat/adapters/` (finish what's started, don't add more half-stubs)

**4. Knowledge & life**
- Cloud drive (Google Drive, Dropbox, S3) via MCP
- Bookmarks/notes/memos — markdown files in a synced directory + the existing 5-layer memory system in `pkg/memex/memory/`
- News/RSS/feeds via MCP or a small native tool
- Surface: daily briefing pane (what changed in your repos overnight, calendar, important emails, summary of saved articles)
- Existing scaffolding: memory subsystem is already over-engineered for this; finally finds its product use case

### The UI architecture: embedded web, no Electron

Browsing email, deploying servers, viewing calendars — this is not a TUI workload. The portal needs a graphical UI. Options considered:

| Option | Bundle size | Single-binary? | Verdict |
|---|---|---|---|
| **Electron** | ~100 MB Chromium runtime | No | **Rejected** — destroys the single-binary wedge that is half the reason to use ycode. (Also previously rejected in `docs/roadmap.md`.) |
| **Tauri** | ~10 MB + Rust dep | Optional wrapper | Acceptable as an *optional* desktop shell layered on top of the web UI |
| **Embedded web UI (SPA bundled in Go binary via `embed.FS`)** | ~2–5 MB SPA inside the Go binary | **Yes** | **Recommended primary** |
| **PWA from the embedded web UI** | Same as above + offline manifest | Yes | Free upgrade — install button in browser → desktop-app feel without any extra runtime |

**Decision:**
- **Primary UI:** static SPA (Svelte or React; whichever has smaller bundles and permissive license — both are MIT) compiled into `internal/webui/dist/`, embedded via `go:embed`, served by `ycode serve` at `localhost:<port>`.
- **PWA enabled** so users can "install" it from the browser → behaves like a desktop app, no second binary needed.
- **Optional Tauri shell** (separate optional download) for users who want a real desktop icon. Tauri just wraps a webview pointed at the local server. Adds ~10 MB if you want it; default install is still single Go binary.
- **Electron rejected.** Recorded here so the decision isn't relitigated. Anyone proposing Electron must first explain how it doesn't break the wedge.

The default `ycode` command keeps doing what it does (CLI/TUI). `ycode portal` (or `ycode ui`) opens the web UI. Power users stay in the terminal; portal users get the GUI; both share the same backend.

### MCP-first integration discipline

The portal expansion **does not** mean ycode builds 20 OAuth flows, 20 API clients, 20 sync engines. That path is the half-baked-feature trap at industrial scale. The discipline:

1. **Default to MCP.** Gmail, Calendar, Drive, GitHub, Linear, Jira, Sentry, Notion, Slack, DigitalOcean, AWS, Vercel — most have maintained MCP servers (community or official). ycode wires them in via the existing MCP client (`internal/runtime/mcp/`).
2. **Maintenance burden moves to the MCP author.** When Gmail's API changes, the MCP server author fixes it; we don't. This is the cheap path to scale.
3. **We own only the wedge.** Local-first runtime, single-binary, offline mode, embedded UI, the agent loop itself. Everything else is an MCP integration.
4. **Build a native fallback only when MCP fails the user** — e.g., MCP requires cloud auth flow that breaks the offline story. In that case, build a thin local shim. Never the default path.
5. **Every integration starts `experimental`.** It graduates to `stable` only after the same criteria as everything else: integration test, dogfooded ≥2 weeks, failure mode documented. The build-tag system already enforces this — portal integrations get the same gate.

### How this respects the credibility floor

The risk: portal expansion = more half-baked feature surface. Mitigations:

- **MCP-first means we ship integration, not implementation.** If the MCP server doesn't exist or doesn't work, we don't promise the integration in the README. Period.
- **Per-pillar graduation.** A pillar (e.g., Communications) is only "stable" when ≥2 integrations within it are stable. The README advertises pillars at the lowest tier of any feature inside them.
- **The default install hides the portal.** First run is still the coding agent. The portal is opt-in via `ycode portal` until the GA gate (Phase 6 below).
- **No portal feature ships unless it works fully offline OR clearly labels its cloud dependency.** Local-first remains the wedge; portal features that violate it must be explicitly opt-in and labeled.

### Why this is the right ceiling, not the right ground floor

- Trying to be a portal before being a great coding agent = "another half-baked everything-tool." (See: every failed all-in-one workspace product.)
- Earning portal language after dominating coding-agent benchmarks = category creation. (See: how Notion expanded from "notes" to "all-in-one workspace" only after notes was undeniably good.)
- The roadmap phases reflect this: Phases 0–3 establish coding-agent dominance + on-ramp. Phase 6 (portal) only begins after Phase 3's exit gate is hit.

## Versioning & releases

ycode follows **semver**. The current major is `0`, meaning the API, CLI surface, config schema, and persisted on-disk formats are all subject to breaking changes between minor versions. Stability promises start at `1.0.0` — and `1.0.0` is gated on Phase 3 exit (see roadmap).

### Tagging convention

| Tag form | Meaning | When |
|---|---|---|
| `v0.0.x` | Patch — bug fix, doc fix, dependency bump | Frequent (≥ weekly during active dev) |
| `v0.x.0` | Minor — new feature graduates to `stable`, or new user-facing capability | Whenever a tracked Phase 0–3 item exits its gate |
| `v1.0.0` | Major — first stable release. Breaking changes promise begins here | Gated on Phase 3 exit (see roadmap) |
| `v*.*.*-rc.N` / `-alpha.N` | Pre-release | Before risky cuts — auto-marked as pre-release in GitHub Releases |

Tags are annotated (`git tag -a`) so the message becomes part of the GitHub Release body. The `v` prefix is required — the release workflow trigger is `v*`.

### Release cadence

Aligned with [Operating principle 1](#1-ship-every-2-weeks-no-exceptions): **every two weeks at minimum**, more often if a load-bearing fix is ready. A long gap between tags is a signal to audit — either the work isn't shipping or the tagging discipline has slipped. Both deserve attention.

### What a release contains

The `release.yml` workflow on every `v*` tag push:
- Builds binaries natively per platform (linux-amd64 on ubuntu, darwin-amd64/arm64 on macos, windows-amd64 on windows)
- Packages tar.gz for Unix and zip for Windows
- Publishes a `SHA256SUMS` file alongside the archives
- Creates a GitHub Release with auto-generated notes from the commit log between tags
- Auto-marks anything tagged `vX.Y.Z-...` as pre-release

### Promotion-to-stable triggers a tag

Per the [feature-tier policy](#mechanism-to-enforce-the-floor-feature-tiers-via-go-build-tags), promoting a feature from `experimental` to `stable` is a user-visible behavior change. It must ship in a tagged release — bumping the minor version. This binds the tier-graduation discipline to the release discipline so the README's "stable features" list is always reproducible from a known tag.

## Roadmap

Phased, dependency-ordered. Weeks are indicative, not contractual; the order is the load-bearing part. Each phase has an exit gate — do not start the next phase until the current gate passes.

This is the **strategic** roadmap. For the tactical feature-gap inventory (P0/P1/P2 items from gap analyses), see `docs/roadmap.md`.

### Phase 0 — Credibility floor (Weeks 1–2)

**Goal:** Mechanically separate "what's ready" from "what's WIP."

- [x] Land `internal/features/registry.yaml` and registry parser
- [x] Add `experimental` and `wip` build tags to existing rough features — Slack/Matrix/Email adapters now behind `experimental`. Trainer agent / sprint two-stage review / mesh auto-start investigated and confirmed already config-gated or honestly documented as planned; no additional tags needed.
- [x] Wire `verify-features` CI workflow
- [x] Resolve TUI permission TODO (`internal/cli/tui.go`) — confirmYes/No now call RespondPermission via the agentClient; regression test in `internal/cli/permission_test.go`
- [ ] Audit and re-classify all 50+ tools — register each as stable/experimental/wip
- [ ] Auto-generate the README tool catalog section from the registry
- [x] Commit `docs/strategy.md` (this document) + reference it from `AGENTS.md` and `README.md`

**Exit gate:** Default `make build` produces a binary where every advertised feature passes graduation criteria. Running `ycode doctor` on three fresh OSes reports zero TODO/stub conditions.

### Phase 1 — Wedge & proof (Weeks 3–6)

**Goal:** Stake the "local-first, single-binary, runs offline" position with hard numbers.

- [ ] SWE-bench Verified submission (eval-tune, submit, publish) — see `docs/leaderboards.md` for cost/infra detail
- [ ] Aider polyglot benchmark submission
- [ ] Terminal-Bench submission
- [ ] **Custom "offline-mode SWE-bench"** (same tasks, no internet, embedded Ollama only) — own this category by construction
- [ ] `ycode.dev/benchmarks` static dashboard with live scores + trend lines + cost-per-task
- [ ] README badges (shields.io) auto-updated from each release
- [ ] Honest comparison page: ycode vs. Cursor / Claude Code / Aider / Cline (score, cost, latency, offline support)
- [ ] README rewrite leading with the wedge, not the architecture
- [ ] One 60-second demo GIF showing offline mode end-to-end

**Exit gate:** A skeptical developer landing on the README sees a verifiable score on a known benchmark + a 60-second demo within 30 seconds of arrival.

### Phase 2 — On-ramp (Weeks 5–10, overlaps Phase 1)

**Goal:** Reduce time-from-discovery-to-first-success to under 5 minutes.

- [x] Prebuilt binaries on GitHub Releases — `.github/workflows/release.yml` matrix-builds linux/darwin/windows × amd64/arm64 on each `v*` tag push, packages tar.gz/zip with SHA256SUMS, creates the Release
- [ ] Homebrew tap: `brew install ycode/tap/ycode`
- [ ] `go install github.com/.../ycode/cmd/ycode@latest` validated
- [ ] `npx ycode` wrapper that downloads platform binary on first run
- [ ] First-run wizard (extends `doctor`): detects creds, runs `/init`, builds repomap, suggests 3 starter prompts
- [ ] VS Code extension v0.1: sidebar chat, plan view, diff preview, Apply/Reject code lenses, status bar
- [ ] Reuse VS Code extension's wire protocol for a Neovim plugin v0.1
- [ ] Landing page (could be `ycode.dev`) with single command demo

**Exit gate:** 30-second video: open VS Code, install extension, paste API key, type a prompt, see a real code change applied. From a fresh machine, no `make`, no source build.

### Phase 3 — Daily ergonomics (Weeks 8–14, overlaps Phase 2)

**Goal:** Convert installs into retention. Address the daily anxieties: cost, trust, continuity.

- [ ] Live cost meter in TUI status bar + VS Code status bar (in/out tokens × price, session running total, cache-hit %)
- [ ] Soft budget cap per task (`--budget 5.00`, prompt-to-continue)
- [ ] Auto-router: cheap model (Haiku / local Ollama) for read/list/grep, expensive for design/edit
- [ ] Auto git-stash before each mutation; tagged with tool-call ID
- [ ] TUI keybinds: `u` = undo last tool call, `U` = roll back to stash point
- [ ] Plan-first as a first-class workflow (always-show-plan mode)
- [ ] `--dry-run` returns planned diffs instead of executing
- [ ] Auto-revert on test red (if `make test` was green and turned red after a tool call)
- [ ] `ycode resume [<id>]` lists and resumes prior sessions
- [ ] Per-branch session binding (`git checkout x` loads agent thread for `x`)
- [ ] `ycode pr <num>` opens PR-as-conversation with comments/reviews/CI streamed in
- [ ] `ycode --background` for detached agents, status posted to PR or Slack
- [ ] Graduate Slack adapter from `experimental` → `stable` (finish, dogfood, document)

**Exit gate:** A user runs ycode for a full week without aborting a session due to cost anxiety, fear of breakage, or lost state. Measured via opt-in telemetry (session-completion rate, rollback usage, resume usage).

### Phase 4 — Differentiator expansion (Weeks 14+)

Tier-2 work, sequenced by ROI:

- [ ] Sentry / Linear / Jira MCP entry points (triage → code loop)
- [ ] Replay UI for past agent decisions (renders OTEL traces as a developer-friendly timeline)
- [ ] Watch mode (auto test/lint on save, self-correct)
- [ ] Speculative parallelism at the tool dispatch level
- [ ] Workspace / multi-repo mode for monorepos
- [ ] Air-gapped deployment guide + signed enterprise binaries

### Phase 5 — Compounding (Ongoing)

- [ ] Continuous benchmark improvement — every release posts a delta on the dashboard
- [ ] "Built with ycode" page: ycode-on-ycode commits in `git log`
- [ ] Tier-3 polish (inline correction, multi-repo, etc.)
- [ ] Quarterly strategy review — update this doc, re-rank tiers based on actual metrics

### Phase 6 — Portal expansion (Weeks 16+, gated on Phase 3 exit)

**Goal:** Convert the coding-agent foundation into the developer's daily-driver cockpit. See [Portal vision](#portal-vision) for the full picture.

Sequenced sub-phases:

**6a. Web UI foundation (W16–20)**
- [ ] `internal/webui/` — static SPA (Svelte or React), embedded via `go:embed`
- [ ] `ycode portal` / `ycode ui` command launches it on localhost
- [ ] Reuses existing WebSocket layer from `ycode serve` for chat + tool stream
- [ ] PWA manifest so it installs from the browser
- [ ] Surfaces existing coding-agent functionality first — sessions, plan view, diff preview, cost meter
- [ ] Ships behind `experimental` build tag until the foundation is solid

**6b. Communications pillar (W20–24)**
- [ ] Gmail MCP integration → triage panel in UI
- [ ] Google Calendar MCP integration → day view + "schedule focus block" action
- [ ] Slack MCP integration (graduate the existing adapter scaffold)
- [ ] All start `experimental`; graduate per pillar

**6c. Ops & deploy pillar (W24–28)**
- [ ] DigitalOcean MCP / AWS MCP / Fly.io / Vercel — pick 2 to start, based on user demand
- [ ] Deploy panel: live status, logs, rollback button
- [ ] Auto-rollback on health-check failure (reuse trust-UX rollback infrastructure from Phase 3)

**6d. Knowledge & life pillar (W28–32)**
- [ ] Google Drive MCP integration → file browser + "save this conversation" action
- [ ] Daily briefing pane: overnight repo changes, calendar, important emails, saved articles
- [ ] Bookmark/memo system on top of existing `pkg/memex/memory/`
- [ ] RSS/news intake (small native tool or MCP)

**6e. Optional Tauri desktop wrapper (W32+)**
- [ ] Separate optional download; webview pointed at the local `ycode serve` instance
- [ ] OS dock icon, native notifications, auto-launch on login
- [ ] Default install remains the single Go binary

**Exit gate:** A developer uses `ycode portal` for a full day instead of switching between editor + Gmail + Calendar + Slack + a separate deploy tool — and prefers it. Measured via opt-in telemetry (pillar usage breadth, daily-active sessions, time-on-portal).

### Status (updated each sprint)

| Phase | Target | Started | Done | Notes |
|---|---|---|---|---|
| 0 — Credibility floor | W1–2 | 2026-05-05 | — | 5/7 done: strategy doc, feature-tier mechanism + CI gate, chat-adapter stubs gated, TUI permission flow wired with regression test. Remaining: full tool audit + README auto-gen. |
| 1 — Wedge & proof | W3–6 | — | — | |
| 2 — On-ramp | W5–10 | 2026-05-05 | — | **v0.1.0 released** (linux-amd64 + darwin-arm64 binaries published with SHA256SUMS). Release workflow has dryrun mode (`workflow_dispatch` / PR) so future fixes validate before tagging. Remaining: brew tap, `go install` verification, npx wrapper, first-run wizard, VS Code extension, README rewrite. |
| 3 — Daily ergonomics | W8–14 | — | — | |
| 4 — Differentiators | W14+ | — | — | |
| 5 — Compounding | Ongoing | — | — | |
| 6 — Portal expansion | W16+ | — | — | Gated on Phase 3 exit |

Update this table at the start of every sprint. Drift becomes visible the moment "Started" date slips past "Target."

## Operating principles: working smart in a lightning market

The AI agent space ships weekly. A 14-week roadmap executed sequentially is obsolete by week 6. The roadmap above is **directional**, not contractual — these principles are how we keep it alive without abandoning focus.

### 1. Ship every 2 weeks, no exceptions

Eight 2-week ships absorb new realities as they emerge. One 4-month ship gets eaten by a competitor's surprise launch. Every phase of the roadmap is decomposed into 2-week increments; if a 2-week slice can't ship something visible, the slice is wrong.

### 2. Eat our own dog food at maximum velocity

ycode's thesis is that AI agents accelerate development. **Every PR landed against ycode should be predominantly authored by ycode itself**, visible in `git log`. If we can't merge our own PRs with ycode, we don't believe our own product, and buyers will see through it instantly.

This is also the cheapest possible velocity multiplier. The roadmap is large; the team is small; the only way the math works is if ycode does most of the work.

### 3. Track competitors weekly, decide within 7 days

A short `docs/intel.md` (private if necessary) updated weekly: what Claude Code / Cursor / Cline / Aider / Codex CLI / Continue / Windsurf / Roo / Devin shipped this week. For each new feature, a one-line decision within 7 days:

- **Copy** — they're right; we should match
- **Ignore** — doesn't fit our wedge
- **Counter** — we should ship something better, fast
- **Drop our equivalent** — they commoditized it; remove our wrapper

Indecision is the expensive option. A wrong decision can be reverted; a missed week of context cannot.

### 4. Bet on durable trends, not current features

Models get cheaper, context grows, local inference catches up, MCP-class protocols standardize. Build assuming these vectors, not against today's Claude Sonnet pricing. Specifically:
- The offline/local-first wedge becomes **stronger** every quarter as local models close the gap — this is a tailwind, not a fixed bet.
- Anything we wrap from a foundation provider (caching, thinking, tool use, memory APIs) is a temporary moat. Build the wrapper, ship it, plan to drop it.

### 5. Commoditize then drop — don't defend yesterday's surface area

When OpenAI/Anthropic/Google ship something we currently wrap, drop the wrapper next release. Defending obsolete code is how feature sprawl returns. Examples to watch: native prompt caching APIs, native long-context, model-side memory, model-side MCP. Each one we lose to the platform = one less thing we have to maintain.

### 6. Parallel agents on the roadmap itself

ycode has agent swarms. The roadmap is the prime use case. Three agents working in parallel on Phase 1 leaderboard tuning, Phase 2 brew formula, and Phase 3 cost meter is not a slogan — it's literally the daily operating mode. If we can't roadmap-by-swarm, the swarm feature is broken (or shouldn't be advertised yet — it should be `experimental`).

### 6.5. MCP-first, single-binary sacred

Two architectural commitments protect the wedge through portal expansion:

- **MCP-first integration.** When a capability exists as an MCP server (Gmail, Calendar, Drive, DigitalOcean, Linear, etc.), wire it via `internal/runtime/mcp/`. We do not build OAuth flows, API clients, or sync engines for things MCP already covers. Maintenance burden moves to the MCP author.
- **Single binary is sacred.** The default install is one Go binary. UI is embedded via `go:embed`. PWA upgrade is free. Optional Tauri shell allowed (separate download). **Electron rejected** — it destroys the wedge and is not worth the bundle cost. Do not relitigate.

Any proposal that adds an external runtime dependency (Node, Python, full Rust stack) to the default install must first explain how the offline / single-binary positioning survives. If it can't, the proposal is wrong, not the wedge.

### 7. Leverage > effort

Don't write what we can adopt:
- Eval harnesses: use existing SWE-bench / Aider / Terminal-Bench code; only customize the offline-mode variant
- Badges: shields.io
- Docs site: GitHub Pages + a static generator, not a custom site
- Telemetry: PostHog or similar, not a homegrown stack
- VS Code extension scaffolding: copy proven patterns from Continue / Cline, don't invent

Reserve "build it ourselves" for the wedge — local-first runtime, single-binary, offline mode. Everything else: integrate, don't reinvent.

### 8. Explicit replan triggers

The roadmap is paused and re-ranked when any of these fire:

| Trigger | Action |
|---|---|
| A competitor ships a feature that subsumes one of our tier-1 levers | Drop that lever; redirect effort to the wedge |
| A new model class lands (≥10× cost or capability shift) | Re-evaluate cost UX, auto-router defaults, and the offline-mode benchmark math |
| A new orchestration primitive becomes standard (next MCP) | Adopt it within one release cycle; deprecate our equivalent |
| A leaderboard gap of >20 points opens vs. SOTA | Drop everything not load-bearing for benchmark recovery |
| Three consecutive 2-week slices ship without visible user-facing change | Stop and audit — we are building scaffolding, not product |

### 9. Hard work is non-negotiable; smart work is what makes it pay

There is no version of this plan that succeeds on smart-but-low-effort. The market is too fast. But effort spent on the wrong axis is wasted effort. The principles above are the filter that decides where the effort goes:

- Effort on the wedge (offline, single binary, local-first) → compounds
- Effort on benchmarks → compounds (each score buys credibility for the next)
- Effort on dogfooding → compounds (faster iteration unlocks faster iteration)
- Effort on me-too feature parity → does not compound (treadmill)
- Effort on wrappers around platform features the platforms will commoditize → negative-sum

Re-read this section before every sprint planning session. The temptation to chase the latest competitor announcement is strong; the discipline to stay on the wedge is what wins.

## Honest take

ycode is a **better engine than most popular agents.** In a crowded market that fact alone wins nothing — every competitor has features. What ycode is missing is **a wedge, proof, and an on-ramp**:

1. **Wedge** — pick "local-first, single-binary, runs offline" and bet the marketing on it. It's the one position the popular cloud-tied agents structurally cannot copy.
2. **Proof** — public leaderboards (SWE-bench Verified, Aider polyglot, Terminal-Bench, plus an "offline-mode" benchmark only ycode can win). This is the single highest-ROI investment for both developer trust and investor attention. Each new score is a free news cycle.
3. **On-ramp** — prebuilt binaries, brew/go-install/npx paths, VS Code extension, first-run wizard. Today the on-ramp is `make init`; that has to die.
4. **Daily ergonomics** — cost meter, checkpoint/rollback, session resume. These keep developers once they arrive.

Order of operations matters:

0. **Credibility floor** — cut/gate half-baked surface. Without this, every later step amplifies a flawed first impression.
1. **Wedge** — gets attention.
2. **Leaderboards** — convert attention to credibility.
3. **On-ramp** — converts credibility to installs.
4. **Ergonomics** — converts installs to retention.

Skip any one and the funnel breaks. Doing them out of order is worse than not doing them — a leaderboard score that draws traffic to a `make init` first-run with stubbed adapters is a brand-damaging spike, not an adoption event.

Most of this work is **surfacing and consolidating what ycode already has**, not building new things. That's the cheapest possible adoption path — and it's why this plan is achievable rather than aspirational.
