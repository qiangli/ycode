# genie

**genie** ("bashy genie" in public contexts) is the SWE agent that grows. It
was seeded from [`agent-mini`](../agent-mini/) at ycode commit `fcd0e4b` and
is the only agent the growth work changes; `agent-mini` stays as it was — a
small reference example and the permanent baseline arm every genie
improvement is measured against.

Like agent-mini, genie is a ycode YAML declaration (`agent.yaml`) with Bashy
as the only model-visible tool, plus a small Go adapter (`cmd/genie/`) that
turns one prepared SWE-bench instance into one prediction. The goal is to
perform comparably with local models: no external LLM vendor is required.

## What differs from agent-mini

- **Bash# effect envelopes.** An interpreter or test runner cannot be proven
  safe command by command, so bashy's preflight denies a bare `python -m
  pytest`. genie's prompt teaches the model to wrap such runs in a function
  that declares its effects and denies the network:

  ```bsh
  @effects("read,write,exec")
  @contain(net: "deny")
  function run_tests() {
    python3 -m unittest discover -s tests -t .
  }
  run_tests
  ```

  The preflight records the declared effects with scope `declared`, and the
  `declared-envelope` rule in `agent.yaml` allows them when they stay within
  read, write and exec. A declaration without `@contain(net: "deny")` keeps a
  possible network effect and falls through to the network rule.
- **A fixture task** (`fixture/`): a one-file Python bug with failing unit
  tests, the smallest end-to-end check of the loop.
- Environment variables use the `GENIE_` prefix (`GENIE_TASK_JSON`,
  `GENIE_RUN_ID`, `GENIE_MODEL_NAME`, `GENIE_CONFIG`, `GENIE_TIMEOUT`,
  `GENIE_PROFILE`, `GENIE_MODEL_ID`, `GENIE_ARTIFACT_DIR`,
  `GENIE_FIXTURE_DIR`).
- `profile-model` also takes `GENIE_CONTEXT_TOKENS` (the context window
  the model server really holds) and `GENIE_REQUEST_TIMEOUT_MS` (local
  inference is slower than a hosted API).

## Build and run

Install Bashy and ycode first; nothing else. The dag steps use bashy builtins
(no Python) and bashy runs the Go adapter from source as Bash# (no Go
toolchain; `bashy dag -f dag.md build` still makes a native binary when wanted).
From this directory:

```bsh
bashy dag -f dag.md package      # dist/genie.bar (Bashsharp Archive)
bashy dag -f dag.md fixture      # dist/fixture/{repo,task.json}
```

Run the fixture task against any OpenAI-compatible endpoint, for example a
local Ollama:

```bsh
GENIE_PROFILE=qwen3-8b GENIE_MODEL_ID=qwen3:8b \
GENIE_CONTEXT_TOKENS=32768 GENIE_REQUEST_TIMEOUT_MS=600000 \
  bashy dag -f dag.md profile-model
GENIE_TASK_JSON=dist/fixture/task.json \
GENIE_RUN_ID=fixture-001 \
GENIE_MODEL_NAME=qwen3:8b \
GENIE_CONFIG=$PWD/dist/profiles/qwen3-8b/agent.yaml \
OPENAI_BASE_URL=http://127.0.0.1:11434/v1 OPENAI_API_KEY=ollama \
bashy run dist/genie.bar
```

The adapter prints one prediction (`instance_id`, `model_name_or_path`,
`model_patch`) and writes the request, run manifest and ycode session under
`./artifacts/<run>/<instance>/`. The adapter contract is agent-mini's; see
its [README](../agent-mini/README.md) for the request fields, `YCODE_BIN`
and the SWE-bench plan. Benchmark runs go through
[`../bench`](../bench/README.md).

## bashy genie

With bashy installed, genie is one command away. It works in the directory
you are in, on a local model picked for this host (`-m MODEL` overrides):

```bsh
bashy genie build --from path/to/ycode/examples/genie   # once: installs ~/.bashy/genie/genie.bar

bashy genie "what does add() in calc.py return for add(2, 3)?"   # one turn; the answer on stdout
git diff | bashy genie -m qwen3:8b                              # the message from stdin
bashy genie                                                     # interactive, on a terminal
bashy genie web                                                 # browser chat: prints the URL to open
bashy genie resume                                              # continue the latest session
bashy genie session list                                        # also show|export|search|rename|fork

bashy genie solve "the failing test in tests/test_stats.py; fix stats.mean"
bashy genie doctor                                      # bundle, bashy, ycode, and the model pick for this host
```

The chat modes run this bundle's `chat` target: pick the model, start
genie's own model server — bashy's Ollama on a kernel-chosen free port on
127.0.0.1, sharing only the model store, so concurrent runs never collide —
pull the model once under a lock, check it answers, write an instance config
whose workspace is your directory, and hand the input to the engine
(`bashy ycode`). `web` serves the engine's built-in chat page (the `web`
frontend: `ui: chat` on loopback) and prints `http://127.0.0.1:PORT/#token=…`;
the token rides in the URL fragment, which the browser never sends to the
server, and every API call needs it. The server stops when genie exits.

`bashy genie solve TASK` is the bench-style run (the `solve` target): the
same model server, a clean git tree (refused when dirty unless
`GENIE_ALLOW_DIRTY=1`), the change left in the working tree, and the
prediction and run record in `~/.bashy/genie/runs` (`GENIE_ARTIFACT_DIR`).
The engine is bashy's own `bashy ycode`; `YCODE_BIN` names another.

## Host-aware model pick

`bashy dag -f dag.md pick-model` chooses a local Ollama model for this host.
Host facts come from `bashy resources system --json` (CPU, memory, GPUs with
unified or discrete VRAM); `models.json` lists the candidates by tier with
their Q4 weight size, KV-cache estimate, context and rank. A model fits when
weights + KV cache + overhead fit the usable memory (a fraction of unified
memory, discrete VRAM, or RAM on CPU-only hosts); the highest-ranked fitting
model wins and `GENIE_MODEL_ID` always overrides. The choice, its reason and
the facts go to `dist/model-choice.json`; set `GENIE_MODEL_CHOICE` to that
file when running the bundle and the adapter stores it with the run
(`model-choice.json`, referenced from `run.json`). `GENIE_HOST_FACTS` points
the pick at another host's facts. Measured on the dev box (M4 Pro, 24 GB) it
picks tier S; 48 GB and 192 GB hosts get tier M and L. The KV-cache and
headroom figures are planning estimates until the bench host measures them. A GPU too small for
every candidate (an integrated GPU's few-hundred-MB carve-out, a small card)
does not stop the pick: it falls back to the RAM budget and the model runs on
the CPU.

## Host requirements

genie needs only bashy: no ycode, Python, curl or git (bashy provides git,
fetches the official Ollama runtime for a local model, and prepares Python for
a workspace with `.py` files on first use). The requirements depend on where
the model runs.

### With a cloud / external model

`bashy genie -m NAME`, where NAME is an API model in the registry
(`bashy model add NAME --provider openai-compat --kind api --base-url URL
--upstream ID --api-key-ref SECRET`), runs genie against that
OpenAI-compatible endpoint and starts no local model server. The key comes
from the environment or the cloudbox vault (`bashy secret`), never the
command line.

| | Minimum | Measured |
|---|---|---|
| RAM | **1 GB** | peak 410 MB PSS across genie's 7 bashy processes while solving the fixture (Linux, 4 GB droplet, `glm-5.3`) |
| CPU | 1 vCPU | |
| Disk | ~0.5 GB | the bashy binary (~115 MB), the bundle, a Python toolchain when the workspace needs one |
| Network | to the provider | |

A 512 MB host is below this once the OS is counted. This is the path for a
cheap cloud host (a 1 GB droplet) running genie for its owner.

### With a local model (Ollama)

genie picks the model for the host (below) and runs its own Ollama server.

| RAM (CPU-only host) | Tier | Model (context) | Expect |
|---|---|---|---|
| under ~2 GB | — | none: genie refuses and points here | use an external model, or `-m` at your own risk |
| ~2 GB | XS | `qwen3:0.6b` (4k) | runs end to end; close to no coding ability |
| ~4 GB | XS | `qwen3:1.7b` (6k) | runs end to end; weak answers, no reliable fixes |
| ~20 GB, or 16 GB unified | S | `qwen3:8b` (32k), then `gpt-oss:20b`, `devstral:24b` | useful help; small fixes unreliable |
| 48 GB+ (36 GB unified) | M | `qwen3.6:27b`, `qwen3-coder:30b`, `glm-4.7-flash` | fixes the fixture bug |
| 192 GB | L | `gpt-oss:120b` | |

Disk: the model (0.5 GB for XS up to ~65 GB for L) plus ~1–2 GB for the
Ollama runtime. The thresholds follow from `models.json` (weights + KV cache
at the tier's context + overhead, against 60% of RAM on a CPU-only host, 75%
of unified memory, 95% of discrete VRAM). A GPU too small for every model (an
integrated GPU's carve-out) falls back to the RAM budget.

Measured with `bashy genie smoke` (a question and a fixture solve,
2026-09-26): a 36 GB Apple M-series host picks `qwen3.6:27b` and solves the
fixture (question 67 s, solve 88 s); a 4 GB / 2 vCPU Linux droplet picks
`qwen3:1.7b` on the CPU (question 165–276 s; the fixture is not fixed); a
16 GB Windows laptop with an integrated Radeon picks `qwen3:1.7b` on the CPU.
These are "it runs" results; which local models can actually do the work an
operator expects is a separate, per-task validation.

## Attribution

genie inherits agent-mini's YAML harness, which was informed by
Live-SWE-agent and mini-SWE-agent (both MIT). See `ATTRIBUTION.md` and the
retained `LICENSE-*.md` files; the upstream source snapshots stay in
`../agent-mini`.
