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

Install Bashy and ycode first. From this directory:

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

## Attribution

genie inherits agent-mini's YAML harness, which was informed by
Live-SWE-agent and mini-SWE-agent (both MIT). See `ATTRIBUTION.md` and the
retained `LICENSE-*.md` files; the upstream source snapshots stay in
`../agent-mini`.
