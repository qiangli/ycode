# agent-mini

`agent-mini` is a lightweight SWE-bench harness: a ycode YAML declaration,
Bashy as the only model-visible tool, and a small Go adapter for benchmark
instances. The upstream Live-SWE-agent YAML and mini-SWE-agent source snapshot
are retained as comparison references; the Python runtime is not on the
agent-mini execution path. It has not yet been evaluated and makes no score
claim.

agent-mini is kept unchanged as a small reference example and as the
permanent baseline arm of the benchmark plan. The agent that grows is
[`genie`](../genie/) ("bashy genie"), seeded from this directory; every
improvement lands there and is measured against agent-mini.

## Included

- `agent.yaml`: complete ycode harness declaration, including model tool calls,
  context, session history, memory, policy, and repeat-loop stages.
- `src/minisweagent/`: pinned upstream mini-SWE-agent source for baseline
  reproduction; it is not included in the lightweight runtime archive.
- `config/`: original Live-SWE-agent YAML configurations.
- `cmd/agent-mini/`: Go adapter accepting one SWE-bench task and returning a
  prediction JSON object after ycode runs in the task checkout.
- `dag.md`: Bashy build and packaging graph. The source bundle includes the Go
  adapter source, YAML harness, Bashy launcher, and attribution. Bashy and
  ycode are installed separately.

The copied source revisions are mini-swe-agent
`04d809ceab9df28f9adaed044884180159172930` and live-swe-agent
`8d7dd8634580d1e09320b4c27d70380bc9ae74a8`. MIT licenses and copyrights are
retained in the two `LICENSE-*.md` files.

## Build a downloadable bundle

Install Bashy and ycode first. Bashy supports the Python source fences used
by this workflow out of the box. From this directory, create the portable
source bundle with:

```bsh
bashy dag -f dag.md package
```

The result is `dist/agent-mini.bar`, a gzip-compressed tar archive. `.bar`
means **Bashsharp Archive**. An archive is runnable when its root contains
`dag.md` with a `main` target; no separate manifest is needed. ZIP, TAR,
TAR.GZ, and GZ archives with that same root DAG contract are accepted too.
Bashy extracts each content version under `~/.bashy/cache/bars/` and reuses
unchanged versions. The bundle contains the harness YAML, Bashy launcher, DAG
and Python fence workflow, and Go adapter source. It does not bundle Bashy,
ycode, Python, a model, or a container runtime. Upstream source snapshots and
licenses stay in the source checkout for comparison; the downloadable harness
stays lightweight.

Run the bundle with:

```bsh
AGENT_MINI_TASK_JSON=/path/to/task.json \
AGENT_MINI_RUN_ID=local-smoke-001 \
AGENT_MINI_MODEL_NAME=gpt-5.6 \
OPENAI_API_KEY=… \
bashy run dist/agent-mini.bar
```

The archive's `main` DAG target builds the adapter and runs one prepared task.
The task JSON needs `instance_id`, `problem_statement`, and `repo_path`; set
`OPENAI_BASE_URL` too when using an OpenAI-compatible gateway. The default
artifact directory is `./artifacts` under the directory from which you invoked
Bashy.
You can also run a DAG file or directory directly with `bashy run
/path/to/dag.md` or `bashy run /path/to/project/`; `--target NAME` selects a
target explicitly. Without it, Bashy selects `main` when present, then uses
the DAG's configured default.

To compare models behind the same OpenAI-compatible endpoint, create a named
model config with Bashy:

```bsh
AGENT_MINI_PROFILE=small AGENT_MINI_MODEL_ID=provider/model-name bashy dag -f dag.md profile-model
```

Set `OPENAI_BASE_URL` and `OPENAI_API_KEY` for the compatible gateway. The
generated profile `.bar` archive is created by the `package-profile` target;
when running that archive, select its config with
`AGENT_MINI_CONFIG=profiles/small/agent.yaml`.

Extract the archive, export an OpenAI-compatible API key, then run:

```bsh
export OPENAI_API_KEY=…
bashy ./agent-mini.bsh "Fix this issue: …"
```

To use the benchmark adapter, build it with `GOWORK=off bashy go build -o bin/agent-mini ./cmd/agent-mini`. Provide one JSON request on stdin with `instance_id`, `problem_statement`, `repo_path`, `artifact_dir`, `run_id`, and `model_name_or_path`, then invoke `./bin/agent-mini -config /absolute/path/to/agent.yaml`. The adapter emits SWE-bench prediction fields `instance_id`, `model_name_or_path`, and `model_patch`. It writes the prompt response, run manifest, and ycode session
state under the requested per-instance artifact directory. `YCODE_BIN` selects
another ycode executable; `AGENT_MINI_TIMEOUT` sets the per-instance deadline
(default `30m`). Use a clean, isolated task checkout. The adapter handles one
prepared testbed at a time; a benchmark controller must provision SWE-bench
images and collect evaluator test outputs. Keep `artifact_dir` outside the task
checkout and use a unique `run_id` per model/configuration run.

Example request:

```json
{"instance_id":"org__repo-1","problem_statement":"Fix the reported issue","repo_path":"/work/org-repo","artifact_dir":"/results/agent-mini","run_id":"verified-gpt56-001","model_name_or_path":"gpt-5.6"}
```

## SWE-bench plan

Compare this ycode harness against the copied Live-SWE-agent setup with the
same model, dataset split, token/cost limits, and container settings. Keep
per-instance trajectories, predictions, patches, and test outputs. After
results are reproducible, package and register a run using the current
[SWE-bench submission checklist](https://github.com/SWE-bench/experiments/blob/main/checklist.md)
and [submission CLI](https://github.com/SWE-bench/SWE-bench/blob/main/docs/reference/cli.md).
The current checklist calls for pass@1 evaluation and auditable public
artifacts including reasoning traces. Confirm current requirements before
submitting; the protocol may change. Reproducing or beating Live-SWE-agent
requires controlled benchmark runs and iterative tuning.
