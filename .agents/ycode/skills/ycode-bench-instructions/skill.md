---
name: bench-instructions
description: Benchmark instruction file quality across agentic tools — static analysis, init generation, and containerized E2E comparison. Runs locally or on a remote host.
---

# /bench-instructions

Compare instruction file quality across agentic tools. Three tiers of evaluation, from fast to thorough. Runs locally or against a remote host on the network.

## Arguments

`{{ARGS}}` — optional flags:

- Tool names: `ycode opencode clawcode codex gemini-cli aider` (default: all six)
- `--static` — Tier 1 only (fast, no LLM)
- `--init` ��� Tier 1 + 2 (static + init generation quality)
- `--e2e` — All three tiers (full containerized comparison)
- `--model <name>` — Override Ollama model (default: auto-detect best for host RAM)
- `--repo <path>` — Test repo for init generation (default: use priorart repos)
- `--host <addr>` — Remote host for Tier 2/3 (default: localhost). Uses SSH for remote ops.
- `--ollama-url <url>` — Ollama endpoint (default: `http://localhost:11434`, or `http://<host>:11434` for remote)
- `--podman-url <url>` — Podman socket (default: auto-detect local, or `ssh://<user>@<host>/run/podman/podman.sock` for remote)

Default with no flags: `--static` (fast, always works, local only).

---

## Execution Modes

### Local execution (default)

Everything runs on the current machine. Podman and Ollama are discovered locally.

```bash
# Tier 1: static analysis (no infra needed)
make eval-agentsmd

# Tier 2: init generation (needs local Ollama)
go test -race -v -run TestInitGeneration -count=1 ./internal/eval/agentsmd/

# Tier 3: full E2E (needs local Podman + Ollama)
make bench-init
```

### Remote execution

Run benchmarks against a remote host on the local network. The remote host provides the compute (Podman containers, Ollama inference). The local machine orchestrates and collects results.

```bash
# Tier 3 on a remote machine
make bench-init HOST=workstation PORT=11434

# Or with explicit URLs
OLLAMA_URL=http://workstation:11434 PODMAN_URL=ssh://user@workstation/run/podman/podman.sock \
  go test -tags benchmark -count=1 -timeout 35m -v ./internal/eval/benchmark/...
```

**Remote setup requirements:**
- Podman installed and socket exposed (`systemctl --user enable --now podman.socket`)
- Ollama running and listening on network interface (`OLLAMA_HOST=0.0.0.0:11434 ollama serve`)
- SSH access from local machine (for Podman remote socket)
- ycode source synced to remote (for build context) or pre-built images available

**How remote works:**
1. Podman connects via SSH socket: `ssh://user@host/run/podman/podman.sock`
2. Container images are built on the remote host
3. Ollama is accessed via HTTP: `http://host:11434`
4. Containers reach Ollama via `host.containers.internal:11434` (same as local)
5. Results are collected back to the local machine via Podman cp

---

## Tier 1: Static Analysis (no LLM, ~2 seconds)

Scores each tool's existing instruction file against the codebase. Works identically on local and remote — only reads files.

```bash
go test -race -v -run TestBenchAllTools -count=1 ./internal/eval/agentsmd/
```

Metrics: command density, guardrail density, boilerplate ratio, path accuracy, command accuracy.
Also runs git commit mining to surface implicit conventions.

**Output**: side-by-side scoring table with per-tool findings.

| Tool | Default instruction file |
|------|------------------------|
| ycode | `./AGENTS.md` |
| opencode | `./priorart/opencode/AGENTS.md` |
| clawcode | `./priorart/clawcode/CLAUDE.md` |
| codex | `./priorart/codex/AGENTS.md` |
| gemini-cli | `./priorart/geminicli/GEMINI.md` |
| aider | `./priorart/aider/CONTRIBUTING.md` |

---

## Tier 2: Init Generation Quality (~5 minutes)

Runs ycode's `InitGenerator` against priorart repos, calls an LLM to produce actual AGENTS.md files, then scores the output vs existing files.

```bash
# Local Ollama
go test -race -v -run TestInitGeneration -count=1 ./internal/eval/agentsmd/

# Remote Ollama
OLLAMA_URL=http://workstation:11434 \
  go test -race -v -run TestInitGeneration -count=1 ./internal/eval/agentsmd/
```

### Model selection

Auto-detect based on host RAM:

| Host RAM | Recommended model | Parameters |
|----------|------------------|------------|
| 8 GB | `qwen2.5-coder:3b` | 3B |
| 16 GB | `qwen2.5-coder:7b` | 7B |
| 24 GB | `qwen2.5-coder:14b` | 14B |
| 32 GB+ | `qwen2.5-coder:32b` | 32B |

Override: `/bench-instructions --init --model llama3.1:8b`

---

## Tier 3: Containerized E2E Comparison (~30 minutes)

The real test: run each tool's actual `/init` command inside a container against test repos, collect the generated AGENTS.md, and score all outputs side-by-side.

### Local

```bash
make bench-init
```

### Remote

```bash
# Remote Podman + remote Ollama
make bench-init HOST=workstation

# Explicit configuration
OLLAMA_URL=http://workstation:11434 \
PODMAN_URL=ssh://user@workstation/run/podman/podman.sock \
  make bench-init
```

### Architecture

```
┌─ Local machine (orchestrator) ──────────────────────┐
│  ycode binary                                       │
│    ├─ Podman REST client ──→ remote or local socket │
│    ├─ Ollama HTTP client ──→ remote or local URL    │
│    └─ agentsmd.Analyze() (scoring, local)           │
└─────────────────────────────────────────────────────┘
         │ REST/SSH                    │ HTTP
         ▼                            ▼
┌─ Target host (compute) ────────────────────────────┐
│  Podman service (containers)                       │
│    ├─ bench-ycode container                        │
│    ├─ bench-opencode container                     │
│    ├─ bench-clawcode container                     │
│    ├─ bench-codex container                        │
│    └─ bench-aider container                        │
│                                                    │
│  Ollama (:11434)                                   │
│    └─ qwen2.5-coder:14b (auto-selected)           │
└────────────��──────────────────────────��────────────┘
```

Local and remote use the same code paths — the only difference is the Podman socket URL and Ollama endpoint URL.

### Container images

Each tool runs in its own container built from its source in `priorart/`:

| Tool | Base image | Init command |
|------|-----------|-------------|
| ycode | `golang:1.23` | `ycode /init` |
| opencode | `oven/bun:1` | `echo '/init' \| opencode` |
| clawcode | `rust:1` | `echo '/init' \| tool` |
| codex | `rust:1` | `tool --message "Generate AGENTS.md..." --approval-mode full-auto` |
| aider | `python:3.12` | `aider --yes --no-git --message "Generate AGENTS.md..."` |
| gemini-cli | — | Skipped (no OpenAI-compat) |

All containers:
- Set `OPENAI_BASE_URL` pointing to Ollama (local or remote)
- Set `OPENAI_API_KEY=ollama`
- Timeout: 3 minutes per tool per repo

### E2E workflow

1. **Connect** to Podman (local socket or remote SSH) and Ollama (local or remote HTTP)
2. **Build images** from embedded Dockerfiles + priorart source as context
3. **For each (tool, repo):** create container → copy repo in → strip existing AGENTS.md → exec init → copy output out → score
4. **Compare** all outputs with `agentsmd.FormatComparison()`

---

## Environment variables

All tiers respect these environment variables for remote/custom configuration:

| Variable | Default | Purpose |
|----------|---------|---------|
| `HOST` | `localhost` | Target host for remote execution |
| `OLLAMA_URL` | `http://localhost:11434` | Ollama HTTP endpoint |
| `PODMAN_URL` | auto-detect local socket | Podman REST API socket (`unix://` or `ssh://`) |
| `BENCH_MODEL` | auto-detect from RAM | Override Ollama model name |
| `BENCH_TIMEOUT` | `30m` | Total benchmark timeout |
| `BENCH_REPOS` | `opencode,clawcode` | Comma-separated test repo names |

---

## Scoring formula

The composite score (0-10) weights:
- Command density (25%) — fenced code blocks with runnable commands / total lines, normalized to 10% = perfect
- Guardrail density (25%) — prohibition lines / total lines, normalized to 10% = perfect
- No boilerplate (20%) — 0% generic advice = perfect, 10%+ = 0
- Path accuracy (15%) — valid paths / total path references
- Command accuracy (15%) — valid make targets / total make target references

## Quick commands

```bash
# Tier 1: static only (always works, fast)
go test -race -v -run TestBenchAllTools -count=1 ./internal/eval/agentsmd/

# Tier 2: init generation (needs Ollama, local or remote)
OLLAMA_URL=http://localhost:11434 \
  go test -race -v -run TestInitGeneration -count=1 ./internal/eval/agentsmd/

# Tier 3: full E2E — local
make bench-init

# Tier 3: full E2E — remote host
make bench-init HOST=workstation

# Tier 3: full E2E — explicit URLs
OLLAMA_URL=http://10.0.1.50:11434 PODMAN_URL=ssh://user@10.0.1.50/run/podman/podman.sock \
  go test -tags benchmark -count=1 -timeout 35m -v ./internal/eval/benchmark/...

# Mine git history for conventions
go test -race -v -run TestBenchAllTools -count=1 ./internal/eval/agentsmd/ | grep "Mined"
```

## Saving results

```bash
mkdir -p docs/benchmarks
go test -race -v -run TestBenchAllTools -count=1 ./internal/eval/agentsmd/ 2>&1 | \
  grep -A 50 "Instruction File Benchmark" > docs/benchmarks/instructions-$(date +%Y-%m-%d).md
```
