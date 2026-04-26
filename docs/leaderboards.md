# Leaderboard Reference

> Comprehensive guide to AI coding agent leaderboards: costs, submission processes, and ycode's path to leadership.

---

## Target Leaderboards

### Priority 1: Primary Targets

These test the **full agent system** (scaffolding + model), not just the model. ycode can differentiate through superior tool dispatch, error recovery, and context management.

#### SWE-bench Pro

| | |
|---|---|
| **URL** | [swebench.com](https://www.swebench.com/) / [Scale AI](https://labs.scale.com/leaderboard/swe_bench_pro_public) |
| **Tasks** | 1,865 real GitHub issues across 41 repos |
| **Measures** | Issue resolution: agent reads issue, navigates repo, generates patch, tests pass |
| **Submission** | Free — PR to `SWE-bench/experiments` or via `sb-cli` tool |
| **Est. cost** | **$750–$1,000** per full run (Claude Opus at ~100K tokens/task) |
| **Time** | ~8 hours |
| **Infrastructure** | Docker, 16GB RAM, 120GB disk; or Modal cloud |
| **Why it matters** | Gold standard. Contamination-resistant (unlike Verified). Tests full agent scaffolding. |
| **Current leaders** | Claude Opus 4.7: 64.3%, GPT-5.4: 59.1% |

#### SWE-bench Verified

| | |
|---|---|
| **URL** | [swebench.com](https://www.swebench.com/) |
| **Tasks** | 500 human-validated instances |
| **Submission** | Free — same as Pro |
| **Est. cost** | **$250–$400** per full run |
| **Time** | ~4 hours |
| **Current leaders** | Claude Mythos: 93.9%, Claude Opus 4.7: 87.6% |
| **Caveat** | Contaminated — OpenAI abandoned it Feb 2026. Still widely reported. |

#### Aider Polyglot

| | |
|---|---|
| **URL** | [aider.chat/docs/leaderboards](https://aider.chat/docs/leaderboards/) |
| **Tasks** | 225 Exercism exercises across C++, Go, Java, JavaScript, Python, Rust |
| **Measures** | Code editing with retry: attempt → test → feedback → retry |
| **Submission** | Free — PR to aider repo with benchmark results |
| **Est. cost** | **$20–$40** per full run (Claude Sonnet) |
| **Time** | ~1 hour |
| **Infrastructure** | Docker required for sandboxed test execution |
| **Why it matters** | Tests the edit+retry loop — core agentic capability. Cheapest meaningful benchmark. |
| **Current leaders** | GPT-5: 88%, Refact agent + Claude 3.7: 92.9% |
| **Runner** | `internal/eval/harness/run_aider_bench.py` |

#### Terminal-Bench 2.0

| | |
|---|---|
| **URL** | [tbench.ai](https://www.tbench.ai/leaderboard) |
| **Tasks** | 89 complex terminal tasks in 10 domains |
| **Measures** | Terminal-native tasks: compilation, debugging, system config, data processing |
| **Submission** | Free — via Harbor CLI framework |
| **Est. cost** | **$5–$50** per full run |
| **Time** | ~1 hour |
| **Infrastructure** | Docker Desktop, Harbor framework (`pip install harbor-ai`) |
| **Why it matters** | Directly tests CLI agent behavior. Terminal-native = ycode's natural environment. |
| **Current leaders** | GPT-5.5: 82.7%, Claude Sonnet 4.5: 50% |
| **Runner** | `internal/eval/harness/run_tbench.py` |

#### Sigmabench

| | |
|---|---|
| **URL** | [sigmabench.com](https://sigmabench.com/) |
| **Tasks** | Real production codebases (Python, Java, Go, TypeScript) |
| **Measures** | Patch generation accuracy, speed, consistency on real OSS repos |
| **Submission** | Unclear — may require invitation or API access |
| **Est. cost** | Unknown (proprietary platform) |
| **Why it matters** | Scaffolding causes 30–60% score variance — ycode's competitive advantage. Newest benchmark. |

### Priority 2: Agent Capability Targets

#### BFCL V4 Agentic (Berkeley Function Calling)

| | |
|---|---|
| **URL** | [gorilla.cs.berkeley.edu](https://gorilla.cs.berkeley.edu/leaderboard.html) |
| **Tasks** | 1,000+ function calling tasks (simple, parallel, agentic, REST, SQL) |
| **Measures** | Tool invocation accuracy via AST-based evaluation |
| **Submission** | Free — open-source eval code on GitHub |
| **Est. cost** | **$10–$100** per full run |
| **Time** | ~2 hours |
| **Why it matters** | Directly measures ycode's tool dispatch quality (50 tools). |
| **Runner** | `internal/eval/harness/run_bfcl.py` |

#### HAL (Holistic Agent Leaderboard)

| | |
|---|---|
| **URL** | [hal.cs.princeton.edu](https://hal.cs.princeton.edu/) |
| **Tasks** | 9 benchmarks (SWE-bench, USACO, CORE-bench, Cybench, etc.) |
| **Measures** | Reliability, consistency, predictability, cost-performance tradeoffs |
| **Submission** | Free — `hal-upload` CLI to HuggingFace Hub |
| **Est. cost** | **~$2 per rollout** |
| **Why it matters** | Tests what users care about: reliable + cheap. Cost-aware evaluation. |

#### PR Arena

| | |
|---|---|
| **URL** | [prarena.ai](https://prarena.ai/) |
| **Tasks** | Real GitHub issues from open-source repos |
| **Measures** | PR generation: draft, ready, and merged rates |
| **Submission** | **Fully automatic** — label issues with "pr-arena", agent generates PRs |
| **Est. cost** | **$0** (model cost only, passive monitoring) |
| **Why it matters** | Zero effort. Results appear automatically. Tests real-world artifact production. |

#### LiveCodeBench

| | |
|---|---|
| **URL** | [livecodebench.github.io](https://livecodebench.github.io/) |
| **Tasks** | 1,055 competitive programming problems (refreshed weekly) |
| **Measures** | Code generation, self-repair, test output prediction |
| **Submission** | Free — PR to GitHub repo |
| **Est. cost** | **$5–$50** per full run |
| **Why it matters** | Contamination-free (new problems weekly). Tests agentic self-repair. |

---

## Cost Summary

| Leaderboard | Tasks | Fee | Est. API Cost | Time | Entry Barrier |
|---|---|---|---|---|---|
| PR Arena | Real issues | Free | $0 extra | Passive | None |
| Terminal-Bench 2.0 | 89 | Free | $5–$50 | ~1 hr | Docker |
| Aider Polyglot | 225 | Free | $20–$40 | ~1 hr | Docker |
| LiveCodeBench | 1,055 | Free | $5–$50 | ~2 hrs | None |
| BFCL V4 | 1,000+ | Free | $10–$100 | ~2 hrs | Git clone |
| HAL | 9 benchmarks | Free | ~$2/rollout | Hours | Harbor |
| SWE-bench Verified | 500 | Free | $250–$400 | ~4 hrs | Docker, 16GB RAM |
| SWE-bench Pro | 1,865 | Free | $750–$1,000 | ~8 hrs | Docker, 120GB disk |
| Sigmabench | Variable | Unclear | Unknown | Variable | Invite? |

**Total to appear on 4 leaderboards**: ~$100–$200 (Aider + Terminal-Bench + BFCL + PR Arena)

---

## Submission Strategy

### Phase A: Establish Baselines (local, free)

Run all benchmarks locally with Ollama to understand ycode's current capabilities without spending on API costs. This identifies weakest areas before investing in frontier model runs.

```bash
# Run internal evals with local Ollama
EVAL_PROVIDER=ollama make eval-smoke
EVAL_PROVIDER=ollama make eval-behavioral

# Run external benchmarks with local model
cd internal/eval/harness
python run_aider_bench.py --model qwen2.5-coder:14b --provider ollama --max-problems 20
python run_tbench.py --model qwen2.5-coder:14b --provider ollama --tasks 10
python run_bfcl.py --model qwen2.5-coder:14b --provider ollama --categories simple --max-tasks 50
```

### Phase B: Improve Scaffolding

Target specific weaknesses revealed by baselines:
- **Low tool accuracy** → improve tool descriptions, schema precision
- **Low trajectory score** → improve system prompt guidance
- **Low edit precision** → improve edit_file implementation
- **High flakiness** → stabilize prompt engineering
- **High cost** → optimize token usage, context management

### Phase C: Submit (cheapest first)

1. **PR Arena** ($0) — label GitHub issues, results appear automatically
2. **Aider Polyglot** (~$30) — one paid run with Claude Sonnet for submission
3. **Terminal-Bench 2.0** (~$20) — one paid run via Harbor
4. **BFCL V4** (~$50) — test tool calling accuracy

```bash
# Paid submission runs
EVAL_PROVIDER=anthropic python run_aider_bench.py --model claude-sonnet-4-6-20250514
EVAL_PROVIDER=anthropic python run_tbench.py --model claude-sonnet-4-6-20250514
EVAL_PROVIDER=anthropic python run_bfcl.py --model claude-sonnet-4-6-20250514 --categories all
```

### Phase D: Scale Up

5. **SWE-bench Verified** (~$300) — only when scoring competitively on cheaper benchmarks
6. **SWE-bench Pro** (~$800) — the ultimate target

### Phase E: Continuous Monitoring

Schedule recurring eval runs to track improvement over time:

```bash
ycode eval schedule --interval 24h
ycode eval history
ycode eval compare <baseline.jsonl> <current.jsonl>
```

---

## Competitive Landscape (April 2026)

| Agent | SWE-bench Verified | SWE-bench Pro | Aider Polyglot | Terminal-Bench 2.0 |
|---|---|---|---|---|
| Claude Code (Opus 4.7) | 87.6% | 64.3% | — | — |
| Codex CLI (GPT-5.3) | 85% | 56.8% | — | — |
| GPT-5.5 | — | — | — | 82.7% |
| GPT-5 | — | — | 88.0% | — |
| Aider (best config) | — | — | 92.9% | — |
| **ycode** | **TBD** | **TBD** | **TBD** | **TBD** |

### Key Insight

**Scaffolding quality matters as much as model quality.** The same Claude model with different agent harnesses produces 30–60% score variation on Sigmabench. ycode's embedded infrastructure (50 tools, Ollama, Podman, git server, OTEL, 5-layer memory) is the competitive moat that can differentiate it even when using the same underlying model as competitors.

---

## Internal Eval Infrastructure

| Command | Tier | LLM Required | Est. Time |
|---|---|---|---|
| `make eval-contract` | Contract | No | <10s |
| `make eval-smoke` | Smoke | Yes | <5 min |
| `make eval-behavioral` | Behavioral | Yes | <30 min |
| `make eval-e2e` | E2E | Yes | <45 min |
| `ycode eval run --tier smoke` | Smoke | Yes | <5 min |
| `ycode eval matrix` | Cross-provider | Yes | Varies |
| `ycode eval report` | — | No | Instant |
| `ycode eval history` | — | No | Instant |
| `ycode eval compare` | — | No | Instant |

---

## References

- [SWE-bench](https://www.swebench.com/) — submission guide, CLI tool, leaderboard
- [Aider Leaderboards](https://aider.chat/docs/leaderboards/) — polyglot benchmark, results format
- [Terminal-Bench](https://www.tbench.ai/) — Harbor framework, task domains
- [BFCL](https://gorilla.cs.berkeley.edu/leaderboard.html) — function calling, AST evaluation
- [HAL](https://hal.cs.princeton.edu/) — holistic agent evaluation, cost tracking
- [PR Arena](https://prarena.ai/) — automatic PR generation leaderboard
- [LiveCodeBench](https://livecodebench.github.io/) — contamination-free coding benchmark
- [Sigmabench](https://sigmabench.com/) — real-world production codebase evaluation
- [Anthropic: Demystifying evals](https://www.anthropic.com/engineering/demystifying-evals-for-ai-agents)
- [Anthropic: Infrastructure noise](https://www.anthropic.com/engineering/infrastructure-noise)
- [OpenAI: Why we no longer evaluate SWE-bench Verified](https://openai.com/index/why-we-no-longer-evaluate-swe-bench-verified/)
