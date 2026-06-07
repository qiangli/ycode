# Gap analysis: ycode vs. 12 prior-art coding agents (2026)

## Preamble

This document is a side-by-side comparison of ycode against twelve other coding agents that live in `priorart/`: **aider, cline, codex, continue, gemini-cli, goose, hermes-agent, openclaw, opencode, openhands, plandex, qwen-code**. All twelve are permissive-licensed (MIT or Apache-2.0); they were picked as the top-starred coding agents on GitHub plus the direct ancestor (openclaw) and the experimental memory-rich agent (hermes-agent).

The goal is to identify where ycode is missing features that materially affect agent performance — not to enumerate every difference. The dimensions are the four that most determine how a coding agent behaves on real tasks:

1. **Memory management** — what survives across sessions, how it's recalled, how it decays.
2. **Context engineering / harnessing** — how the prompt is assembled, cached, compacted, kept relevant.
3. **Tool usage** — registry shape, permission tiers, sandboxing, MCP integration.
4. **Workflow / orchestration** — multi-agent, autonomous loops, hooks, scheduling, delivery.

Each dimension section has the same anatomy: (a) ycode's current shape with file paths, (b) a feature matrix where rows are sub-features and columns are ycode + the 12 priorarts, (c) standout approaches with code pointers, (d) gaps for ycode ranked by Impact × Effort into Tier A (do soon) / B (next quarter) / C (research). The closing pulls out cross-cutting patterns and a list of capabilities only ycode has, to balance the gap-only framing.

Earlier per-project gap analyses live under `docs/gap-analysis-<project>-{memory,orchestration,tools}.md` — this doc cites them but doesn't duplicate them. **Seven of the 12 priorarts have no earlier analysis at all** (cline, continue, goose, hermes-agent, openhands, plandex, qwen-code); the data on those projects below comes from fresh inspection of the cloned repos in November 2026.

Legend in feature matrices: ✓ full, ⚠ partial (qualifier in 1–3 words), — absent.

---

## 1. Memory management

### ycode current shape

ycode runs a **5-layer Memex** (`pkg/memex/`) with four storage backends (bbolt KV, SQLite, chromem-go vector, Bleve FTS) plus a Memos wiki layer, **7 memory types** (User / Feedback / Project / Reference / Episodic / Procedural / Task), **4 scopes** (Global / Project / Team / User), and a write/recall pipeline that uses RRF fusion + MMR diversity reranking with adaptive-depth confidence-based deepening (`pkg/memex/memory/`). A **Dreamer** background consolidation pass merges related memories every 30 minutes; a **persona system** (`pkg/memex/memory/persona_store*`) tracks knowledge domains, communication style, and behavior profiles; entity extraction (`pkg/memex/memory/discovery.go`) links memories via named entities. The **skill engine** (`internal/runtime/skillengine/`) tracks per-skill success/failure rates, decays unused skills weekly, and auto-evolves skills with FIX/DERIVED/CAPTURED modes when success drops below 50% after 3+ failures.

### Feature matrix

| Axis | ycode | aider | cline | codex | continue | gemini-cli | goose | hermes-agent | openclaw | opencode | openhands | plandex | qwen-code |
|---|---|---|---|---|---|---|---|---|---|---|---|---|---|
| Persistent memory | ✓ | — | ⚠ team | — | ✓ | — | — | ✓ | ✓ | ⚠ replay | — | — | ✓ |
| KV backend | ✓ bbolt | — | — | — | — | — | — | ⚠ Redis | ✓ SQLite | — | — | — | — |
| SQL backend | ✓ SQLite | — | — | — | ✓ SQLite | — | — | ✓ SQLite | ✓ SQLite | ⚠ Alembic | ✓ Postgres | ⚠ min | — |
| Vector backend | ✓ chromem | ⚠ llama | ⚠ asksage | — | — | ⚠ mock | — | — | ✓ lancedb | — | ⚠ refs | — | ✓ config |
| Graph backend | ⚠ entities | — | — | — | — | — | — | — | — | — | — | — | — |
| FTS / search index | ✓ Bleve | — | — | — | ⚠ meili | — | ⚠ indexed | ✓ FTS5 | ✓ FTS5+BM25 | — | ⚠ elastic | — | — |
| Multi-scope (G/P/T/U) | ✓ 4-tier | — | ✓ team | — | — | — | — | ⚠ U/S | ⚠ agent | — | — | — | — |
| Skill/lesson capture | ✓ auto-evol | — | ✓ global | — | — | — | — | ✓ auto-create | — | — | ✓ microagents | — | — |
| Memory consolidation | ✓ Dreamer | ⚠ summarize | — | — | — | — | — | ✓ cron | ✓ summarize | ⚠ PR sum | — | ⚠ summarize | — |
| Cross-session recall | ✓ RRF+MMR | — | — | — | — | — | — | ✓ FTS5 | ✓ session | — | — | — | — |
| User/persona modeling | ✓ persona | — | ✓ org/pers | — | — | — | — | ✓ Honcho | — | — | — | — | — |
| Entity extraction | ✓ NER | — | ⚠ ts | — | — | — | — | — | — | — | — | — | — |
| TTL / decay / scoring | ✓ dynamic | — | ✓ banner | — | — | — | ⚠ score | ⚠ timeout | ✓ temporal | — | — | — | ✓ pair-exp |
| Plugin / provider ext | ⚠ provider | ⚠ multi | ✓ provider | — | ✓ adapter | — | — | ✓ mem plugins | ⚠ wiki | — | — | ✓ multi | — |

### Standout approaches

**hermes-agent — Honcho dialectic user modeling + FTS5 cross-session search + LLM curator.** Hermes is the only priorart that combines a persistent user model (the Honcho framework captures *contradictory* preferences across turns and refines them over time) with FTS5 session search and an LLM-backed curator that rewrites stale skills weekly. The cron-driven `curator.py` archives skills with `use_count == 0 && last_activity > 14d`, then runs a `weekly_skill_review` LLM pass against survivors. See `priorart/hermes-agent/agent/curator.py`, `priorart/hermes-agent/cron/jobs.py`, `priorart/hermes-agent/hermes_cli/main.py` (FTS5 VACUUM + merge).

**openclaw — Hybrid vector + BM25 with explicit temporal decay and graceful embedding-fallback.** OpenClaw's memory search runs embedding and FTS5 BM25 in parallel, merges with MMR diversity, and applies a 30-day exponential half-life. LanceDB backs the vector path; 11+ embedding providers are supported including Gemini Embedding 2 for multimodal (images/audio). Uniquely, `provider: "none"` explicitly opts out to FTS-only mode rather than silently degrading — operator intent is preserved. See `priorart/openclaw/docs/concepts/memory-search.md:59-89`, `priorart/openclaw/src/plugins/memory-state.test.ts`.

**continue — SQLite + Merkle-tree sync for cross-device state.** Continue's `sync/` Rust subsystem uses SQLite + Merkle trees for incremental conflict-free state reconciliation across devices. This is unusual for a coding agent — most treat memory as device-local. See `priorart/continue/sync/src/sync/merkle.rs`, `priorart/continue/sync/Cargo.toml` (`rusqlite` bundled).

**openhands — Microagents + Alembic-versioned persistent state.** Microagents own their memory via `.openhands/microagents/repo.md` summaries; enterprise deployments use Alembic migrations (PostgreSQL-backed) for schema evolution. The ops framing treats memory as a critical versioned database rather than a cache. See `priorart/openhands/skills/add_repo_inst.md`, `priorart/openhands/AGENTS.md`, `priorart/openhands/openhands/analytics/analytics_service.py`.

**cline — Team-scoped persistent state with org-level skill policy.** Cline's enterprise skill model lets organizations toggle `globalSkills` and `alwaysEnabled`, distinct from per-user discovery; team state survives session restart with org/personal account switching. See `priorart/cline/apps/vscode/src/services/account/ClineAccountService.ts`, `CHANGELOG.md` entries for globalSkills + remote config.

### Gaps & ranked recommendations

| Gap | Best-in-class | Approach | Impact | Effort | Tier |
|---|---|---|---|---|---|
| Dialectic / adversarial user modeling | hermes-agent (Honcho) | Capture *contradictory* user preferences over time, surface the contradiction to the user, refine the persona. ycode's persona is monotonic. | High | L | **B** |
| Multimodal embeddings (images/audio) | openclaw (Gemini Embedding 2) | Extend chromem-go indexing to process screenshots, attached images, audio transcripts via provider-pluggable encoder. | Med | M | **B** |
| Configurable temporal decay curves per memory type | openclaw (30-d half-life) | ycode's importance-scoring is static + access-driven; add per-type half-life config (e.g. Episodic decays fast, Reference slow). | High | M | **A** |
| Cross-device sync via Merkle trees | continue (`sync/`) | Sync memex state across user devices with hash-tree-bounded incremental updates. Unblocks "same agent on laptop + remote box." | Med | L | **C** |
| Team-level shared knowledge / policy | cline (org-config) | ycode has Team scope but no org-side admin tooling — wire memex Team scope into cloudbox standard-asset push so a team can ship a shared knowledge base. | Med | M | **B** |
| Schema-versioned memory migrations | openhands (Alembic) | ycode's SQLStore doesn't have a migration story — schema changes will require ad-hoc backfill. Add Alembic-style or sqlc migrations. | Med | S | **A** |
| Graceful embedding-provider fallback mode | openclaw (`provider: "none"`) | Explicit operator opt-out of vector search (FTS-only) rather than silent degradation when embeddings unavailable. | Low | S | **A** |

**Memory dimension verdict.** ycode's memex is the most sophisticated in the field on architecture — no priorart has 4 backends + 7 types + 4 scopes + consolidation + adaptive recall. The gaps are at the edges: dialectic modeling (genuinely missing primitive), multimodal indexing, decay-curve tunability, and operations (migrations, fallback modes, sync). See `docs/gap-analysis-aider-memory.md`, `docs/gap-analysis-codex-memory.md`, `docs/gap-analysis-geminicli-memory.md`, `docs/gap-analysis-opencode-memory.md` for deeper per-project context.

---

## 2. Context engineering / harnessing

### ycode current shape

ycode assembles the prompt from **18 named sections** with a static/dynamic boundary tuned for prompt caching (`internal/runtime/prompt/sections.go`). Three layered caches: **PromptCache** (5-min TTL, fingerprint-based break detection, `internal/api/prompt_cache.go`), **CacheWarmer** (4.5-min keep-alive pings to beat Anthropic's 5-min TTL, `internal/api/cache_warmer.go`), and **CompletionCache** (30s disk-backed full-response dedup, `internal/api/completion_cache.go`). Compaction (`internal/runtime/session/compaction.go`) is 3-layer (soft trim → hard clear → mask) with **head/tail preservation** and **identifier preservation** (file paths, git hashes, UUIDs survive the summary); microcompaction handles message-level reduction. **CJK-aware token estimation** (0.25 ASCII, 1.3 CJK) is applied per session. JIT discovery (`internal/runtime/prompt/jit.go`) hash-dedupes AGENTS.md and CLAUDE.md hits across the project. **Differential prompting** omits unchanged sections on non-caching providers. **Tool-TTL** (`internal/runtime/conversation/preactivate.go`) evicts deferred tools after an 8-turn inactivity window.

### Feature matrix

| Axis | ycode | aider | cline | codex | continue | gemini-cli | goose | hermes-agent | openclaw | opencode | openhands | plandex | qwen-code |
|---|---|---|---|---|---|---|---|---|---|---|---|---|---|
| Composed (named) prompt sections | ✓ 18 | ✓ | ✓ registry | ✓ | ✓ | ✓ | ✓ | ✓ | ✓ 18+ | ✓ | ✓ | ✓ | ✓ |
| Model-family variants | ⚠ partial | ✓ | ✓ 7-fam | ✓ | ✓ | ✓ | ✓ | ✓ | ✓ | ✓ | ✓ | ✓ | ✓ |
| Prompt caching (provider native) | ✓ Anthropic | ⚠ | ⚠ | ⚠ | ⚠ | ⚠ | ⚠ | ✓ system+3 | ✓ boundary | ⚠ | ⚠ | — | ⚠ |
| Cache warming / TTL extension | ✓ 4.5m ping | — | — | — | — | — | — | ✓ | ⚠ basic | — | — | — | — |
| Session compaction (LLM-driven) | ✓ 3-layer | ⚠ basic | ✓ | ⚠ basic | ⚠ basic | — | — | ✓ struct | ✓ | ✓ | ✓ | ⚠ basic | ✓ |
| Head/tail preservation | ✓ | — | ⚠ | — | — | — | — | ✓ | ✓ | ⚠ | ⚠ | — | ⚠ |
| Identifier preservation | ✓ paths/hash | — | — | — | — | — | — | ✓ sentinel | ✓ | — | — | — | — |
| Microcompaction (message-level) | ✓ | — | — | — | — | — | — | ⚠ basic | ⚠ | — | — | — | — |
| Token budgeting | ✓ CJK-aware | ✓ | ⚠ model | ✓ | ⚠ model | ⚠ model | ✓ | ✓ | ✓ | ✓ | ✓ | ⚠ model | ✓ |
| JIT AGENTS.md/CLAUDE.md discovery | ✓ hash-dedup | — | ✓ | ✓ | ✓ | — | ✓ | ✓ | ✓ | ✓ | ✓ | — | ✓ |
| Repomap / tree-sitter project map | ⚠ | ✓ | ✓ | ✓ | ✓ | ✓ | ✓ | ✓ | ✓ | ✓ | ✓ | ✓ 30-lang | ✓ |
| Lazy file loading | ✓ | ✓ | ✓ | ✓ | ✓ | ✓ | ✓ | ✓ | ✓ | ✓ | ✓ | — | ✓ |
| Effective context claim | 200k | 100k | 100k | 200k | 100k+ | 1M | 100k+ | 1M+ | 1M+ | 200k+ | 200k+ | 200k+ ("2M smart") | 200k+ |
| Differential prompting | ✓ non-caching | — | — | — | — | — | — | ⚠ prefix | — | — | — | — | — |
| Completion / response caching | ✓ 30s dedup | — | — | — | — | — | — | — | — | — | — | — | — |

### Standout approaches

**hermes-agent — "system_and_3" caching layout + structured-summary compaction with re-execution sentinel.** Hermes places a single 5-minute ephemeral cache marker covering system + last 3 non-system messages, claiming ~75% input token reduction on multi-turn flows. Compaction uses a strict template (Resolved / Pending questions / Remaining Work sections) and prepends a sentinel string that blocks the model from accidentally re-executing summarized tool calls. See `priorart/hermes-agent/agent/prompt_caching.py:1-50` and `priorart/hermes-agent/agent/context_compressor.py:37-61`.

**openclaw — Deterministic-ordered prompt composition with explicit cache-boundary marker.** OpenClaw's `system-prompt.ts` assembles 18+ modular sections with a literal `SYSTEM_PROMPT_CACHE_BOUNDARY` marker in the prompt text; `normalizeStructuredPromptSection()` enforces deterministic ordering of maps/registries so fingerprints are stable across runs. A cache-control stream wrapper strips thinking markers from assistant turns to prevent stale reasoning from poisoning future cache hits. See `priorart/openclaw/src/agents/system-prompt.ts:1-80`, `priorart/openclaw/src/llm/providers/stream-wrappers/anthropic-cache-control-payload.test.ts:1-36`.

**cline — Component-registry prompt builder with proto-driven model-family variants.** Cline declares prompt variants per model family (generic, next-gen, xs, gpt-5, hermes, gemini-3, glm) as `componentOrder` arrays; the builder materializes sections in declared sequence and resolves placeholders via a template engine. gRPC proto definitions enforce frontend/backend payload type-safety so a new model family is one config file + one proto enum + a conversion stub. See `priorart/cline/apps/vscode/src/core/prompts/system-prompt/registry/PromptBuilder.ts:12-80`.

**plandex — Cross-provider prompt caching + tree-sitter validation across 30+ languages.** Plandex implements aggressive lazy file loading with tree-sitter syntax validation for every edit (30+ languages), and claims a "2M effective context" via lazy loading + provider-side prompt caching across OpenAI + Anthropic + Google. See `priorart/plandex/app/server/syntax/` and the planner orchestration in `priorart/plandex/app/server/model/plan/`.

### Gaps & ranked recommendations

| Gap | Best-in-class | Approach | Impact | Effort | Tier |
|---|---|---|---|---|---|
| Cache-boundary marker visible in prompt | openclaw | Surface ycode's static/dynamic boundary as a literal sentinel string in the prompt — aids debugging cache breaks. | Low | S | **A** |
| Component-registry prompt assembly | cline | Move ycode's 18-section static layout into a declarative `componentOrder` registry, enabling per-model-family override without code change. | Med | M | **B** |
| Per-model prompt variants for non-Anthropic providers | cline (7-family) | ycode's model-family branching is partial — add explicit variants for gpt-5, gemini-3, glm. | Med | M | **B** |
| Cross-provider prompt caching | plandex | ycode's PromptCache is Anthropic-specific; abstract the cache layer so OpenAI prefix-cache + Gemini provider cache are first-class. | High | M | **A** |
| Cache-control assistant-turn stripping | openclaw | Strip thinking/reasoning markers from cached assistant turns to prevent stale reasoning poisoning. | Med | S | **A** |
| Tool-result pruning before LLM summarization | hermes-agent | Strip verbose tool outputs (raw JSON, log lines) before feeding compaction LLM. Saves compaction tokens at low quality cost. | Med | S | **A** |
| Tree-sitter syntax validation across 30+ languages on edit | plandex | ycode does fuzzy edit matching; add per-language syntax validation before commit to catch broken edits earlier. | Med | M | **B** |

**Context dimension verdict.** ycode is best-in-class on caching infrastructure (the only one with PromptCache + CacheWarmer + CompletionCache triple) and on compaction quality (identifier preservation + microcompaction is rare). Gaps cluster around (a) prompt assembly flexibility for non-Anthropic providers, (b) operator-visible debugging of cache state, and (c) syntax-aware edit validation. See `docs/gap-analysis-aider-tools.md`, `docs/gap-analysis-codex-tools.md`, `docs/gap-analysis-geminicli-tools.md`, `docs/gap-analysis-opencode-tools.md`.

---

## 3. Tool usage

### ycode current shape

ycode ships **130+ tools** in 10+ categories (bash, fileops, git, github, memory, task, worker, agent-control, observability, web, browser, MCP, builtin) registered in `internal/tools/registry.go`. The **AlwaysAvailable + Deferred** model loads expensive tools on-demand via **ToolSearch** (Bleve-indexed semantic search) with an 8-turn TTL after first use (`internal/runtime/conversation/preactivate.go`). **3-tier permissions** (ReadOnly / WorkspaceWrite / DangerFullAccess, `internal/runtime/permission/mode.go`) are enforced on every tool call; a policy engine (`internal/runtime/policy/`) handles Allow/Deny/Ask with async approval routing and 5-min decision caching. **Embedded Podman** (`internal/container/`) runs containers via a pure-Go REST API with three-tier bootstrap (existing socket → in-process API → auto-provisioned VM); the `containertool` abstraction (`internal/runtime/containertool/`) gives every container-backed tool an inline Dockerfile + bind-mount + timeout shape. **VFS** (`internal/runtime/vfs/`) confines writes to allowed dirs with symlink-escape detection and a 10MB cap; **SSRF protection** (`internal/runtime/net/`) blocks RFC1918, loopback, link-local, CGNAT, and cloud-metadata endpoints. File operations (`internal/runtime/fileops/`) use **4-level fuzzy edit matching**, atomic writes, sensitive-file detection, .gitignore respect, per-file edit locking via semaphore, and encoding detection. Per-tool **quality monitoring** (`internal/tools/quality.go`) tracks success/failure rates. **Hooks** (`internal/runtime/hooks/`) fire on PreToolUse / PostToolUse / PostToolFailure / SessionStart / SessionEnd / FileChanged / TurnStart with regex pattern matching and priority ordering.

### Feature matrix

| Axis | ycode | aider | cline | codex | continue | gemini-cli | goose | hermes-agent | openclaw | opencode | openhands | plandex | qwen-code |
|---|---|---|---|---|---|---|---|---|---|---|---|---|---|
| Tool registry pattern | ✓ unified | — | ⚠ filtered | ✓ dynamic | ✓ per-model | ✓ declarative | — | ⚠ toolsets | ✓ policy-pipe | ✓ schema | — | — | ✓ search |
| Always vs deferred | ✓ TTL-evict | always | always | ✓ defer-flag | always | always | always | always | always | always | always | always | ✓ defer |
| Tool count | 130+ | ~12 | ~20 | ~50 | ~20 | ~30+ | ~25 | ~50+ | 100+ | ~20 | ~15 | ~20 | ~40 |
| Permission tiers | ✓ 3-tier | none | — | ✓ 3-tier | — | ⚠ approval | — | ⚠ safelist | ✓ 3-tier policy | — | — | ⚠ levels | ⚠ settings |
| MCP support | ✓ both | — | ✓ client+OAuth | ⚠ elicit | ✓ Singleton | ⚠ subagent | ⚠ config | ✓ serve+lazy | ✓ tool+res | ✓ manifest | ✓ mcp_config | ⚠ litellm | ✓ both+test |
| Sandboxed exec | ✓ Podman | — | — | ✓ WinSandbox | — | ⚠ GEMINI_SBX | — | ⚠ Docker | ⚠ Docker e2e | — | ⚠ NO_GRP | — | ⚠ QWEN_SBX |
| VFS boundary enforcement | ✓ symlink+cap | — | — | — | ⚠ symlink | — | — | ⚠ safety tool | — | — | — | — | — |
| SSRF / network policy | ✓ comprehensive | — | — | — | — | ✓ isPrivateIp | — | ⚠ url_safety | ⚠ js: link | — | — | — | — |
| Browser tools | ✓ ext | ⚠ webbrowser | — | — | ⚠ pptr | ⚠ browser_agent | ✓ playwright | ✓ 8+ tools | — | ⚠ icon | ✓ playwright | — | — |
| Native API tool-calling | ✓ Anthropic+OAI | — | ✓ tool_use | ✓ hooks | ✓ parsing | ✓ TOOL_USE | — | ✓ tokens | ✓ both | ✓ block | ✓ pre/post | ⚠ comp flag | ✓ parent-id |
| Tool quality monitoring | ✓ rate | ⚠ display | ✓ pass@3 | — | ✓ PostHog | ✓ telemetry | ⚠ perf | ⚠ reset | — | ✓ CW | ✓ LLM tokens | ⚠ errors | ✓ telemetry |
| Semantic tool discovery | ✓ Bleve | — | — | ✓ Fuzzy | — | — | — | — | — | — | — | — | ✓ ToolSearch |
| File-edit safety (fuzzy/atomic) | ✓ 4-level | — | ✓ str_replace | ✓ Fuzzy | ✓ atomic multi | ⚠ Atomics.wait | — | ⚠ lazy | — | ⚠ Effect | — | ⚠ block-tag | — |
| Hooks / middleware | ✓ 7-event | ⚠ git | ✓ lifecycle | ✓ /list API | ✓ llmReqHook | — | ⚠ signal | ⚠ webhook | ✓ bundled | ⚠ webhook | ✓ React | ✓ ShutdownHook | ⚠ module |

### Standout approaches

**gemini-cli — Production-grade SSRF defense with rate limiting and fallback retry.** `web-fetch.ts` enforces `isPrivateIp()` blocking, per-hostname rate limits via LRUCache-backed token-bucket windows, structured fallback retry with telemetry hooks, and approval-mode policy evaluation through a declarative `BaseDeclarativeTool` wrapper that emits to a `MessageBus`. See `priorart/gemini-cli/packages/core/src/tools/web-fetch.ts:22-81`. This is more defensive than ycode's IP-range blocker and worth porting.

**openclaw — Policy-pipeline tool gating with audit trail.** OpenClaw's `effective-tool-policy.ts` runs a serial pipeline of role-based + group-scoped + sender-verified policy checks before any tool fires; bundled tools are filtered by session metadata and trusted-group context; audit logs every policy decision (rule matched, actor scope, approval path, latency). Caller-claimed scopes are kept separate from session-derived verification — a hardening trick ycode doesn't currently apply. See `priorart/openclaw/src/agents/embedded-agent-runner/effective-tool-policy.ts:19-61`.

**codex — `deferLoading` flag in the tool schema.** Codex's schema-level `deferLoading?: boolean` field lets clients lazy-load tool definitions in a way that's negotiated in protocol, not just enforced server-side. Combined with `execpolicy-legacy` and `network_policy.rs`, it gives a runtime-evaluated command-safety pre-check before any exec. See `priorart/codex/codex-rs/app-server-protocol/schema/typescript/v2/DynamicToolSpec.ts:6`, `priorart/codex/codex-rs/execpolicy-legacy/`.

**qwen-code — ToolSearch as a first-class deferred-tool mechanism.** qwen-code's `tool-search.test.ts` validates that `select:<name>` and keyword-query both work, that `shouldDefer=true` tools stay out of the initial schema but are discoverable + invokable in the same session, and that lookup semantics are stable under contract. This mirrors ycode's deferred-tool model but with stronger test contracts. See `priorart/qwen-code/integration-tests/cli/tool-search.test.ts:1-49`.

**hermes-agent — Toolsets with read-only safety composition.** `toolsets.py` composes tools into named toolsets (web, research, browser, webhook) with gating functions and composition rules. The webhook toolset is intentionally restricted to read-only safe tools to mitigate prompt injection from untrusted third-party content — multi-tier safety via toolset scoping, not just permission flags. See `priorart/hermes-agent/toolsets.py:31-73`.

### Gaps & ranked recommendations

| Gap | Best-in-class | Approach | Impact | Effort | Tier |
|---|---|---|---|---|---|
| Per-hostname rate limiting with fallback retry | gemini-cli | Replace ycode's flat SSRF block with token-bucket per-host + structured retry telemetry. | Med | S | **A** |
| Policy-decision audit log | openclaw | Log every policy decision (rule matched, actor, latency) — supports compliance review and policy-drift detection. | Med | S | **A** |
| Caller-scope-vs-session-scope separation | openclaw | Don't trust caller-claimed scopes; verify against session-derived identity. Currently ycode mostly trusts caller. | Med | M | **B** |
| Cloud-metadata endpoint blocklist (AWS/GCP/Azure 169.254 + variants) | gemini-cli | ycode blocks 169.254 generically; add explicit per-cloud metadata endpoint set. | Low | S | **A** |
| Toolset composition (named bundles with gating) | hermes-agent | Group related tools into named toolsets with safety composition rules (e.g. webhook toolset = read-only only). | Med | M | **B** |
| Permission tier UX (user-facing profile preview) | codex + openclaw | Surface ReadOnly/WorkspaceWrite/DangerFullAccess as user-facing profiles with rule previews before approval. | Low | M | **B** |
| Browser sandbox isolation with CDP-instrumented security | hermes-agent + openhands | Wrap browser tool with per-tab network policy + CDP-injected XSS/credential-leak guards. | High | L | **C** |

**Tool dimension verdict.** ycode is ahead on most axes (130+ tools, ToolSearch + Bleve, 3-tier permissions, Podman-native sandboxing, comprehensive SSRF, 7-event hook system). Gaps cluster around (a) policy hardening (audit trail, scope separation, per-host rate limits) and (b) UX (permission previews, toolset composition). See `docs/tools-comparison.md` and `docs/tools-summary.md` for the cross-project tool inventory.

---

## 4. Workflow / orchestration

### ycode current shape

ycode runs a **Foreman/Worker** model — the interactive session is Foreman (full privileges, full source tree, backlog at `~/.agents/ycode/projects/<id>/backlog/`); Workers are sandboxed subprocesses pinned to one Gitea issue + one **Loom workspace** (8h lease, isolated git clone). Tools are exposed via MCP: `loom_lease`, `loom_push`, `loom_merge`, `loom_release`, `loom_status`. An **autonomous loop** (`internal/runtime/autoloop/`) runs RESEARCH→PLAN→BUILD→EVALUATE→LEARN with stall-replan and circuit-breaker (closed/open/half-open). **Agent swarms** (`internal/runtime/swarm/`) chain agents with handoff and cycle detection (max 10 hops). **agentdef** YAML DSL (`internal/runtime/agentdef/`) supports 8 flow types — sequence, chain, parallel, loop, fallback, choice, DAG, router — plus AOP advices (before/around/after), output schemas, guardrails, remote A2A delegation. A **5-agent diagnostic mesh** (Diagnoser, Fixer, Learner, Researcher, Trainer) runs on an event bus. The **skill engine** auto-evolves skills with FIX/DERIVED/CAPTURED modes + weekly decay. **Task/Todo** (`internal/runtime/task/`, `taskqueue/`, `todo/`) supports hierarchical task trees with parent-child mailboxes and per-category semaphore pools. **Sprint** (`internal/runtime/sprint/`) is a state machine with two-stage review (initial + refined) — scaffolded but not yet wired. **Self-heal** (`internal/selfheal/`) classifies errors (build, runtime, config, API, tool, inference) with an escalation policy. **Hooks** fire on 7 lifecycle events. **Agent pool** (`internal/runtime/agentpool/`) tracks per-agent liveness (healthy/suspicious/critical/stranded) with orphan recovery. **5 loop detectors** (generic-repeat, unknown-tool, poll-no-progress, ping-pong, global-ceiling) catch degenerate cycles.

### Feature matrix

| Axis | ycode | aider | cline | codex | continue | gemini-cli | goose | hermes-agent | openclaw | opencode | openhands | plandex | qwen-code |
|---|---|---|---|---|---|---|---|---|---|---|---|---|---|
| Multi-agent (parallel invocations) | ✓ swarm/mesh | — | ⚠ Kanban | ⚠ async | — | — | — | ✓ 5-agent | ✓ + TUI | ⚠ plugin | — | — | ⚠ chans |
| Subagent spawning | ✓ Worker | — | ✓ Kanban+CLI | ⚠ spawn_agent | ⚠ bg | ⚠ subagent | ✓ recipes | ✓ delegate | ✓ lifecycle | ⚠ plugin | ⚠ SDK | — | ✓ SubAgent |
| Worktree / workspace isolation | ✓ Loom 8h | — | ✓ per-card | ⚠ git wt | ⚠ temp | — | ⚠ vendor | — | ✓ guard+rec | ⚠ symlink | ⚠ sandbox env | ✓ diff sbx | ✓ iso-param |
| Task queue / Kanban / backlog | ✓ backlog | — | ✓ board | — | — | ✓ sched queues | — | ✓ dispatcher | ⚠ keys | — | — | — | ✓ event-bl |
| Autonomous loop (R→P→B→E→L) | ✓ autoloop | — | — | — | — | ✓ executor | — | ✓ R/P | — | ⚠ cycle | — | ✓ plan→exec | — |
| Stall / loop detection | ✓ 5 detectors | ⚠ retry | ✓ circuit-br | ⚠ guard | ⚠ install | ⚠ fatal | ⚠ recipe | — | ✓ stall+rec | ⚠ cycle | ⚠ stale | ⚠ rollback | — |
| Skill engine + auto-evolution | ✓ decay+FIX | — | — | — | — | — | — | ⚠ curator+decay | — | — | — | — | — |
| Agent definition DSL (declarative) | ✓ 8 flows | — | ✓ SDK spec | ⚠ enum | ⚠ YAML | ⚠ YAML | ✓ recipe | ✓ plugins | ⚠ schema | ⚠ schema | ⚠ frontmatter | — | ✓ event |
| Hook system (pre/post/failure) | ✓ 7 events | — | ✓ lifecycle | ⚠ pyproject | — | ⚠ pre-commit | ⚠ signal | ✓ 6+ | ✓ plugin | ⚠ xform | ⚠ pre-commit | — | ✓ 4+ |
| Cron / scheduled triggers | ⚠ ext | — | ✓ schedule | ⚠ proc | ⚠ tasks | — | — | ✓ NL cron | — | ✓ Effect sch | ✓ K8s CronJob | — | ✓ scheduler |
| Multi-platform delivery | ⚠ planned | — | ✓ CLI/IDE | — | — | — | — | ✓ 14+ chans | — | — | — | — | — |
| Self-heal / error classification | ✓ classified | — | ✓ svc-err | ⚠ enum | — | — | — | — | ⚠ recovery | ⚠ recovery | ✓ auto-debug | — | — |
| Sprint / two-stage review | ⚠ scaffolded | — | — | — | — | — | — | — | — | — | — | ✓ sbx+rollback | — |
| PR-as-conversation resume | ⚠ planned | — | — | — | — | — | — | — | — | — | — | — | — |
| Parallel task execution | ✓ semaphores | — | ✓ Kanban-par | ⚠ async | ⚠ concur | ✓ concur agents | ✓ multi-step | ✓ batches | ✓ par | — | — | — | ✓ phases |

### Standout approaches

**hermes-agent — 14+ messaging platforms + delegation trees + cron + LLM curator.** This is the single most complete orchestration story in the priorart. The unified `gateway/platforms/` directory routes Telegram, Discord, Slack, WhatsApp, Signal, Matrix, Mattermost, email, SMS, DingTalk, WeChat, Feishu, QQBot through one bus. `cron/scheduler.py` accepts natural-language job specs ("every weekday at 9am check my inbox"). `tools/delegate_tool.py` spawns subagents with isolated terminal sessions, role-based depth limits (orchestrator vs leaf), and merged context. `agent/curator.py` runs weekly LLM review on agent-created skills with `use_count == 0 && last_activity > 14d` → archive. See `priorart/hermes-agent/AGENTS.md:48`, `priorart/hermes-agent/cron/scheduler.py`, `priorart/hermes-agent/tools/delegate_tool.py`, `priorart/hermes-agent/agent/curator.py`.

**cline — Kanban with worktree-per-card and triple-layer proxy.** Cline's separate Kanban repo runs parallel agents per card with git worktree isolation and auto-commit per dependency chain. Circuit breaker (1-hour timeout) bounds banner fetches. Model-family system-prompt variants with per-model fallback tower (XS → GENERIC). gRPC/protobuf between extension and webview eliminates silent state resets. Uniquely: triple-layer network proxy support (fetch wrapper, axios settings, client custom fetch) ensures enterprise proxy transparency across VSCode/JetBrains/CLI. See `priorart/cline/apps/cli/src/session/`, `priorart/cline/sdk/packages/core`, `priorart/cline/proto/cline/*.proto`.

**openclaw — Granular stall detection + subagent lifecycle recovery.** OpenClaw has 935 subagent references; active subagent detail tracking surfaces in the status output to prevent zombie processes. Workspace separation, stale-restart avoidance, plugin harness support validation, and graceful timeout-based lock release. Two-stage approval model for risky operations keeps context prompt-local while maintaining session-level overrides. See `priorart/openclaw/appcast.xml`, `priorart/openclaw/src/talk/`.

**plandex — Cumulative diff sandbox with rollback.** Plandex's distinguishing trick is a "cumulative diff sandbox" — staged changes accumulate separately from project files and can be reviewed/rolled back as one unit before applying. Combined with tree-sitter validation, every edit can be reverted at the diff-bundle level. The two-stage review (initial AI draft → human review → refined pass with feedback) is implemented and load-bearing, not scaffolded. See `priorart/plandex/app/server/model/plan/`, `priorart/plandex/app/cli/cmd/apply.go`.

**openhands — Auto-debug + microagents-as-state.** OpenHands integrates auto-debug into the main loop: a failed test triggers a debug sub-loop that gathers stack traces, proposes a fix, retests. Microagents own task-scoped state via `.openhands/microagents/` summaries. Enterprise multi-user RBAC with Alembic migrations treats orchestration state as a database. See `priorart/openhands/openhands/controller/agent_controller.py`, `priorart/openhands/skills/`.

### Gaps & ranked recommendations

| Gap | Best-in-class | Approach | Impact | Effort | Tier |
|---|---|---|---|---|---|
| Two-stage PR review (initial → human → refined) | plandex | Wire the scaffolded `internal/runtime/sprint/` state machine to the real task flow — initial AI draft + sandbox + review checkpoint + refined pass with feedback. Currently scaffolded; finish the integration. | High | M | **A** |
| Trainer agent in 5-agent mesh (currently no-op) | hermes-agent (curator) | Complete the on-failure skill review → patch → test-in-sandbox → archive/update loop. Mesh exists; Trainer endpoint is the missing wiring. | High | M | **A** |
| PR-as-conversation resume | None ship it; planned in ycode | Add `@ycode resume <task-id>` parser in Foreman that re-provisions a Loom workspace from a PR comment with prior transcript context. Differentiator across the field. | High | M | **A** |
| Cumulative diff sandbox + rollback as one unit | plandex | Stage edits in an apply-pending bundle; surface diff for human review; rollback as a unit. Pairs naturally with two-stage review. | High | M | **A** |
| Multi-platform delivery (Slack/Discord/Matrix/Telegram) | hermes-agent (14+ channels) | Port the unified gateway pattern; reuse hermes-agent's `gateway/platforms/` architecture. ycode docs already mention this as planned. | Med | L | **B** |
| Parallel Worker batches on independent task-subtree leaves | hermes + cline Kanban | Current Foreman is single-Worker-at-a-time; extend to `max_concurrent` Workers on independent leaves. Unblocks throughput. | High | M | **A** |
| Natural-language cron with subagent dispatch | hermes-agent | NL job spec → resolved cron + subagent + delivery channel. ycode has `cron` external; integrate as first-class. | Med | M | **B** |
| Auto-debug on test failure | openhands | When a test fails inside autoloop, fork a debug sub-loop that gathers stack/log context and proposes fix. | Med | M | **B** |
| Persistent durable backlog with claim/release semantics | hermes-agent Kanban + ycode backlog | Add claim/release/auto-block-on-repeated-failure to ycode backlog so multiple Workers can pull from one queue. | Med | M | **B** |
| Triple-layer enterprise proxy transparency | cline | Fetch + axios + custom fetch wrappers for HTTP traffic so enterprise users with proxies don't have to configure 3 places. | Low | S | **B** |

**Orchestration dimension verdict.** ycode has the most ambitious orchestration architecture (agentdef DSL with 8 flow types, 5-agent mesh, autoloop, skill engine with auto-evolution, 5 loop detectors, Foreman/Worker, Loom workspaces) — nobody else has anything close. **But several pieces are scaffolded-not-wired**: Trainer agent is a no-op, sprint two-stage review is scaffolded, PR-as-conversation is planned, multi-platform gateway is planned. The top-tier recommendations are about *finishing* what's started, not adding new systems. See per-project deeper analyses: `docs/gap-analysis-aider-orchestration.md`, `docs/gap-analysis-codex-orchestration.md`, `docs/gap-analysis-geminicli-orchestration.md`, `docs/gap-analysis-opencode-orchestration.md`, plus the cross-cutting `docs/gap-analysis-plan-command.md`.

---

## Cross-cutting observations

Five patterns recur across the four dimensions, each worth calling out:

**1. MCP-as-extension-boundary is the only consensus answer to tool extensibility.** 10 of 12 priorarts have at least partial MCP support (gemini-cli's 70+ servers, opencode's plugin-manifest, openhands' mcp_config, qwen-code's both-sides + test server, hermes-agent's serve+lazy-load, cline's client+OAuth, etc.). ycode supports both client and server. The implication: every new tool ycode ships should be reachable via MCP, not just internal Go registration — otherwise the ecosystem won't pick it up.

**2. Subagent spawning is undermined by lack of standard shape.** Six priorarts spawn subagents (hermes-agent delegate-tool, cline Kanban, qwen-code SubAgent skills, goose recipes, openhands SDK, codex spawn_agent), but no two use the same protocol — they bake the contract into the parent agent. ycode's Foreman/Worker is the most disciplined version (Gitea issue + Loom workspace + lease) but no priorart adopts a comparable contract. The opportunity: publish the Foreman/Worker protocol as a standardized contract (effectively, an MCP-like spec for child agents).

**3. Worktree-per-task isolation is the canonical concurrency primitive.** Cline (Kanban-per-card), ycode (Loom), plandex (diff sandbox), qwen-code (isolation param), openclaw (workspace guard), goose (vendor sandbox flag), codex (git worktree stream) all converge on per-task worktrees as the way to run things in parallel safely. The pattern is more universal than the agent shape; ycode is well-positioned to push it.

**4. Operator-visible state (audit logs, fingerprint UI, decision trails) is consistently absent.** Most priorarts have no visibility into "why did the model do that" — cache breaks, policy decisions, tool-search misses, skill-decay events. OpenClaw's policy audit trail is the closest. This is a low-effort + high-value lane for ycode: every state transition can be a logged event, and a "developer view" panel can render the trail.

**5. Scaffolded-not-wired is ycode's recurring failure mode.** Trainer agent (mesh), sprint two-stage review, PR-as-conversation, mesh auto-engagement, completion of skill auto-evolution callback wiring, Dreamer/compaction integration — all exist as code but not connected to the main path. This is the single biggest lever for impact: an audit pass that finishes one scaffolded system per week would compound fast. The plan should treat "wire scaffolded X" as a first-class task category.

---

## What ycode does that nobody else does

To balance the gap-only framing, here is the list of capabilities ycode has that none of the 12 priorarts demonstrate. These are not "ycode is better" claims — they're observations that the design space ycode occupies is mostly empty.

- **5-layer memex with 4 storage backends + 7 memory types + 4 scopes.** No priorart has more than 2 backends, and most have one or none.
- **Dreamer background consolidation pass.** Hermes has a weekly cron-driven curator; ycode's Dreamer runs every 30 minutes and merges related memories with LLM-backed fusion.
- **Adaptive-depth recall with confidence-based deepening and LLM sub-queries.** Hybrid retrieval (keyword + Bleve FTS + vector + entity) with RRF fusion + MMR diversity is unique.
- **Triple-layer caching (PromptCache + CacheWarmer + CompletionCache).** Other agents have at most one of these.
- **18-section prompt assembly with static/dynamic boundary for cache stability.** Closest peer is openclaw with explicit boundary markers, but the section count is similar and the differential-prompting layer for non-caching providers is unique.
- **8-turn deferred-tool TTL eviction.** qwen-code has deferred tools without TTL; codex has the schema flag without runtime eviction.
- **Embedded Podman as a pure-Go REST API with three-tier bootstrap.** Other agents shell out to docker/podman; ycode doesn't need a daemon to exist.
- **agentdef DSL with 8 flow types.** Sequence / chain / parallel / loop / fallback / choice / DAG / router — closest is cline's componentOrder and goose's recipe DSL, neither matching the flow-type taxonomy.
- **5-agent diagnostic mesh on an event bus (Diagnoser / Fixer / Learner / Researcher / Trainer).** No peer has anything comparable; Trainer is the wiring gap.
- **5 distinct loop detectors (generic-repeat / unknown-tool / poll-no-progress / ping-pong / global-ceiling).** Other agents have at most one of these.
- **4-level fuzzy edit matching with sensitive-file detection, .gitignore respect, atomic writes, per-file edit locking, encoding detection.** File-edit safety in ycode is the most thorough in the field.
- **Skill engine with FIX / DERIVED / CAPTURED modes + weekly decay + auto-evolution at <50% success after 3+ failures.** Hermes' curator is the closest; ycode's mode taxonomy is more granular.
- **CJK-aware token estimation.** Specific to ycode given the user base; no other agent surveyed has this.

---

## How this doc relates to other gap analyses

This doc is the cross-cutting view. For per-project depth on the 5 priorarts already covered:

- aider — `docs/gap-analysis-aider-{memory,orchestration,tools}.md`
- codex — `docs/gap-analysis-codex-{memory,orchestration,tools}.md`
- gemini-cli — `docs/gap-analysis-geminicli-{memory,orchestration,tools}.md`
- openclaw — `docs/gap-analysis-openclaw.md`
- opencode — `docs/gap-analysis-opencode.md`, `docs/gap-analysis-opencode-{memory,orchestration,tools}.md`

For the **7 priorarts with no prior analysis** (cline, continue, goose, hermes-agent, openhands, plandex, qwen-code), the standout-approaches sections above are the first written analysis. If any of those warrant deeper coverage, candidates ranked by likely yield: **hermes-agent** (curator + Honcho + 14-platform gateway), **plandex** (cumulative diff sandbox + 2M context + tree-sitter validation), **cline** (Kanban + componentOrder registry + triple-layer proxy).

Cross-cutting references already present:
- `docs/tools-comparison.md` and `docs/tools-summary.md` — tool inventory across 10 projects
- `docs/memory.md` — ycode's 5-layer memex architecture
- `docs/skills.md` and `docs/skills-summary.md` — skill system reference
- `docs/observability.md` — Prometheus/Jaeger/VictoriaLogs/Perses stack
- `docs/strategy.md` — feature-tier policy (stable/experimental/wip) and graduation criteria

---

## Top-priority work suggested by this analysis

If a single planning session adopts only the Tier A recommendations, the work-list is:

1. **Finish the scaffolded systems** (orchestration Tier A). Sprint two-stage review, Trainer agent in mesh, PR-as-conversation resume, cumulative diff sandbox + rollback, parallel Worker batches. These already have code — they're not new systems.
2. **Cross-provider prompt cache abstraction** (context Tier A). Anthropic + OpenAI prefix-cache + Gemini provider cache as one layer. Tool-result pruning before compaction. Cache-control assistant-stripping. Visible cache-boundary marker.
3. **Operator-visible state** (tools Tier A + cross-cutting). Policy decision audit log. Cache-fingerprint UI. Per-host rate limit + fallback retry. Cloud-metadata blocklist.
4. **Memory-engine ops hardening** (memory Tier A). Configurable decay curves per memory type. Schema-versioned migrations. Embedding-provider explicit-fallback mode.

A reasonable second round (Tier B) prioritizes the user-modeling dialectic (hermes/Honcho), prompt component-registry refactor (cline-style), multi-platform delivery (hermes 14-channel gateway), and parallel-Worker semantics around the existing backlog.

Tier C (research bucket): cross-device Merkle sync (continue), browser sandbox isolation with CDP guards (hermes+openhands).
