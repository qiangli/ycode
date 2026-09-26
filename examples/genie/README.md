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
headroom figures are planning estimates until the bench host measures them.

## Attribution

genie inherits agent-mini's YAML harness, which was informed by
Live-SWE-agent and mini-SWE-agent (both MIT). See `ATTRIBUTION.md` and the
retained `LICENSE-*.md` files; the upstream source snapshots stay in
`../agent-mini`.
