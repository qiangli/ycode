# genie — Bashy workflow

bashy genie: the growing SWE agent, seeded from agent-mini. Bashy executes the DAG with its builtins (jq, sed, tar, ...);
ycode runs the declared agent; the Go adapter bridges a prepared SWE-bench
instance to ycode and emits one prediction. The package deliberately contains
no Bashy, ycode, Python, model, or container runtime binaries.

## Tasks

### build
Sources: cmd/genie/main.go go.mod dag.md
Effects: read, write, exec, net
Generates: dist/bin/genie

```bsh
mkdir -p dist/bin
GOWORK=off "$BASHY" go build -o dist/bin/genie ./cmd/genie
```

### prepare-request
Effects: read

```bsh
if [ -z "${GENIE_TASK_JSON:-}" ]; then
  printf '%s\n' 'set GENIE_TASK_JSON to a task JSON file' >&2
  exit 1
fi
jq -c '{instance_id, problem_statement, repo_path} | with_entries(select(.value != null))' "$GENIE_TASK_JSON"
```

### profile-model
Effects: read, write
Generates: dist/profiles/$GENIE_PROFILE/agent.yaml

```bsh
name=${GENIE_PROFILE:-} model=${GENIE_MODEL_ID:-}
case $name in
  '' | *[!A-Za-z0-9_-]*) name= ;;
esac
if [ -z "$name" ] || [ -z "$model" ]; then
  printf '%s\n' "set GENIE_PROFILE and GENIE_MODEL_ID (profile name: letters, digits, '-' or '_')" >&2
  exit 1
fi
for value in "${GENIE_CONTEXT_TOKENS:-}" "${GENIE_REQUEST_TIMEOUT_MS:-}"; do
  case $value in
    *[!0-9]*)
      printf '%s\n' 'GENIE_CONTEXT_TOKENS and GENIE_REQUEST_TIMEOUT_MS must be integers' >&2
      exit 1
      ;;
  esac
done
if [ "$(grep -c '^      id: gpt-5\.6$' agent.yaml)" != 1 ]; then
  printf '%s\n' 'expected exactly one default model id in agent.yaml' >&2
  exit 1
fi
# The model id goes in as a JSON string (valid YAML); escape it for sed.
quoted=$(M=$model jq -n 'env.M')
quoted=${quoted//\\/\\\\}
quoted=${quoted//&/\\&}
quoted=${quoted//|/\\|}
# A local server holds less context and answers slower than a hosted API:
# never let the harness assume more than the server has.
edits=(-e "s|^      id: gpt-5\.6\$|      id: $quoted|")
if [ -n "${GENIE_CONTEXT_TOKENS:-}" ]; then
  edits+=(-e "s|^\( *contextTokens:\) 128000\$|\1 $GENIE_CONTEXT_TOKENS|")
  # The fixed reserves (one response, compaction, recent history, knowledge)
  # are sized for 32k and up; a tier-XS context would leave the prompt a
  # negative budget, so scale them with the context.
  ctx=$GENIE_CONTEXT_TOKENS
  if [ "$ctx" -lt 32768 ]; then
    edits+=(-e "s|^\( *maxOutputTokens:\) 16384\$|\1 $((ctx / 4))|")
    edits+=(-e "s|^\( *reserveTokens:\) 8000\$|\1 $((ctx / 8))|")
    edits+=(-e "s|^\( *preserveRecentTokens:\) 24000\$|\1 $((ctx / 4))|")
    edits+=(-e "s|^\( *preserveUserMessagesTokens:\) 4000\$|\1 $((ctx / 16))|")
    edits+=(-e "s|^\( *maxTokens:\) 3000\$|\1 $((ctx / 8))|")
  fi
fi
if [ -n "${GENIE_REQUEST_TIMEOUT_MS:-}" ]; then
  edits+=(-e "s|^\( *requestTimeoutMs:\) 120000\$|\1 $GENIE_REQUEST_TIMEOUT_MS|")
  edits+=(-e "s|^\( *timeoutMs:\) 120000\$|\1 $GENIE_REQUEST_TIMEOUT_MS|")
fi
target=dist/profiles/$name
mkdir -p "$target/prompts"
sed "${edits[@]}" agent.yaml > "$target/agent.yaml"
cp prompts/system.md "$target/prompts/system.md"
printf 'Configured model profile: %s\n' "$PWD/$target/agent.yaml"
```

### package
Sources: agent.yaml genie.bsh lib/model-server.bsh lib/toolchains.bsh prompts/system.md cmd/genie/main.go go.mod README.md ATTRIBUTION.md LICENSE.md LICENSE-live-swe-agent.md LICENSE-mini-swe-agent.md dag.md models.json fixture/task.json fixture/repo/
Effects: read, write
Generates: dist/genie.bar

```bsh
set -e
rm -rf dist/package
mkdir -p dist/package
cp -R lib dist/package/
cp agent.yaml genie.bsh README.md ATTRIBUTION.md LICENSE.md LICENSE-live-swe-agent.md LICENSE-mini-swe-agent.md go.mod dag.md models.json dist/package/
mkdir -p dist/package/cmd/genie
cp cmd/genie/main.go dist/package/cmd/genie/main.go
cp -R prompts dist/package/
cp -R fixture dist/package/
tar -czf dist/genie.bar -C dist/package .
```

### package-profile
Requires: profile-model package
Sources: dist/profiles/
Effects: read, write
Generates: dist/genie-profile.bar

```bsh
mkdir -p dist/package/profiles
cp -R "dist/profiles/$GENIE_PROFILE" dist/package/profiles/
tar -czf dist/genie-profile.bar -C dist/package .
```

### pick-model
Sources: models.json
Effects: read, write, exec

Host-aware local model pick (G0.8). Host facts come from the rod
`bashy resources system --json` (CPU, memory, GPUs with unified or discrete
VRAM), or from a JSON file in `GENIE_HOST_FACTS` (tests, planning for another
host). Usable memory is a fraction of unified memory, discrete VRAM, or RAM
for CPU-only hosts (`models.json` `headroom`); a model fits when its weights,
its KV cache at the chosen context and the overhead fit. The highest-ranked
fitting model wins; `GENIE_MODEL_ID` always overrides. The choice and the
facts it was made from go to `dist/model-choice.json`. The target declares no
`Generates`, so it always runs: the answer depends on the host, not the sources.

```bsh
set -e
if [ -n "${GENIE_EXTERNAL_MODEL:-}" ]; then
  # A registered API model: nothing to fit on this host.
  mkdir -p dist
  X_NAME=$GENIE_EXTERNAL_MODEL X_ID=$GENIE_MODEL_ID X_CTX=${GENIE_EXTERNAL_CONTEXT:-32768} jq -n '
    {schema: "genie-model-choice/v1", model: env.X_ID, tier: "external", context: (env.X_CTX | tonumber),
     need_gb: 0, reason: ("external model (bashy model " + env.X_NAME + ")"), external: env.X_NAME}' > dist/model-choice.json
  jq -r '"model \(.model) (\(.reason))"' dist/model-choice.json >&2
  exit 0
fi
if [ -n "${GENIE_HOST_FACTS:-}" ]; then
  facts=$(cat "$GENIE_HOST_FACTS")
else
  facts=$("$BASHY" resources system --json)
fi
mkdir -p dist
F_FACTS=$facts F_TABLE=$(cat models.json) F_OVERRIDE=${GENIE_MODEL_ID:-} jq -n '
  def gb: . * 10 | round / 10;
  (env.F_FACTS | fromjson) as $f
  | (env.F_TABLE | fromjson) as $t
  | ($f.gpus // []) as $gpus
  | ([$gpus[] | select(.vram_kind == "unified")] | first) as $unified
  | ([$gpus[] | select(.vram_kind != "unified" and (.vram_bytes // 0) > 0) | .vram_bytes] | max) as $vram
  | (if $unified != null then {kind: "unified", bytes: $unified.vram_bytes, fraction: $t.headroom.unified_fraction}
     elif $vram != null then {kind: "discrete", bytes: $vram, fraction: $t.headroom.discrete_fraction}
     else {kind: "cpu", bytes: $f.memory.total_bytes, fraction: $t.headroom.cpu_fraction} end) as $mem
  # A GPU too small for every model (the carve-out of an integrated GPU, a small
  # card) is no reason to stop: ollama runs the model on the CPU, so fall back
  # to the RAM budget.
  | {kind: "cpu", bytes: $f.memory.total_bytes, fraction: $t.headroom.cpu_fraction} as $cpu
  | [$t.models[] | . + {need_gb: (.weights_gb + .kv_gb_per_32k * .context / 32768 + (.overhead_gb // $t.headroom.overhead_gb) | gb)}] as $needs
  | (if $mem.kind != "cpu" and ([$needs[] | select(.need_gb <= ($mem.bytes / 1e9 * $mem.fraction | gb))] | length) == 0
     then $cpu else $mem end) as $mem
  | ($mem.bytes / 1e9 * $mem.fraction | gb) as $budget
  | [$needs[] | . + {fits: (.need_gb <= $budget)}] as $rows
  | ([$rows[] | select(.fits)] | sort_by(-.rank) | first) as $best
  | (if env.F_OVERRIDE != "" then
       (([$rows[] | select(.id == env.F_OVERRIDE)] | first) // {id: env.F_OVERRIDE}) + {reason: "override (GENIE_MODEL_ID)"}
     elif $best != null then $best + {reason: "highest-ranked model that fits"}
     else null end) as $choice
  | {
      schema: "genie-model-choice/v1",
      model: ($choice.id // null),
      tier: ($choice.tier // null),
      context: ($choice.context // null),
      need_gb: ($choice.need_gb // null),
      reason: ($choice.reason // "no candidate fits the usable memory"),
      memory: {kind: $mem.kind, total_gb: ($mem.bytes / 1e9 | gb), usable_gb: $budget},
      host: {os: $f.os, arch: $f.arch, cpu: $f.cpu.model, logical_cores: $f.cpu.logical_cores,
             ram_gb: ($f.memory.total_bytes / 1e9 | gb), gpus: [$gpus[] | {vendor, name, vram_gb: ((.vram_bytes // 0) / 1e9 | gb), vram_kind}]},
      candidates: [$rows[] | {id, tier, need_gb, fits}]
    }' > dist/model-choice.json
jq -r '"model \(.model // "none") (tier \(.tier // "-"), need \(.need_gb // 0) GB of \(.memory.usable_gb) usable \(.memory.kind)): \(.reason)"' dist/model-choice.json >&2
if ! jq -e '.model != null' dist/model-choice.json > /dev/null; then
  printf '%s\n' 'genie: this host is below genie'"'"'s minimum for a local model (about 2 GB of RAM, qwen3:0.6b); run it on an external model instead (bashy genie -m NAME, NAME a registered API model: about 1 GB of RAM); see "Host requirements" in the genie README' >&2
  exit 1
fi
```

### solve
Requires: pick-model
Effects: read, write, exec, net, spend

The bench-style run (`bashy genie solve "TASK"` runs this target): solve TASK in the git
repository the caller is in, on a local model, with nothing shared. It picks
the model for this host (`pick-model`; `GENIE_MODEL_ID` overrides), starts
genie's OWN model server — bashy's Ollama on a kernel-chosen free port on
127.0.0.1, sharing only the model store — pulls the model once under a lock,
checks it answers, writes a per-model profile, runs the adapter, and stops
the server on exit. The patch stays in the working tree; the prediction and
the run record go to `GENIE_ARTIFACT_DIR` (default `~/.bashy/genie/runs`).
A dirty working tree is refused (the patch is the diff against HEAD) unless
`GENIE_ALLOW_DIRTY=1`.

```bsh
set -e
caller=${BASHY_DAG_CALLER_PWD:-$PWD}
# A host with only bashy (Windows, a bare image) has no git: bashy carries one.
if ! command -v git > /dev/null 2>&1; then
  git() { "$BASHY" git "$@"; }
fi
task=$(printf '%s' "${BASHY_DAG_ARGS_JSON:-[]}" | jq -r 'join(" ")')
if [ -z "$task" ]; then
  printf '%s\n' 'usage: bashy genie solve "describe the task"' >&2
  exit 2
fi
if ! repo=$(git -C "$caller" rev-parse --show-toplevel 2>/dev/null); then
  printf 'genie: %s is not inside a git repository\n' "$caller" >&2
  exit 2
fi
if [ -z "${GENIE_ALLOW_DIRTY:-}" ] && [ -n "$(git -C "$repo" status --porcelain)" ]; then
  printf 'genie: %s has uncommitted changes; commit or stash them first (or set GENIE_ALLOW_DIRTY=1)\n' "$repo" >&2
  exit 2
fi
model=$(jq -r .model dist/model-choice.json)
context=$(jq -r '.context // 32768' dist/model-choice.json)
run_id=${GENIE_RUN_ID:-genie-$(date +%Y%m%d-%H%M%S)}
artifacts=${GENIE_ARTIFACT_DIR:-${BASHY_HOME:-$HOME/.bashy}/genie/runs}
mkdir -p "$artifacts" dist/servers
if [ -z "${GENIE_EXTERNAL_MODEL:-}" ]; then
  . lib/model-server.bsh
  genie_model_server
  OPENAI_BASE_URL=http://$addr/v1 OPENAI_API_KEY=ollama
else
  # An external model (bashy genie -m NAME, NAME a registered API model):
  # bashy exported its endpoint and key; no local server.
  printf 'genie: external model %s (%s)\n' "$GENIE_EXTERNAL_MODEL" "$model" >&2
fi
. lib/toolchains.bsh
genie_toolchains "$repo"

profile=$(printf '%s' "$model" | tr ':/.' '___')
GENIE_PROFILE=$profile GENIE_MODEL_ID=$model GENIE_CONTEXT_TOKENS=$context \
  GENIE_REQUEST_TIMEOUT_MS=${GENIE_REQUEST_TIMEOUT_MS:-600000} "$BASHY" dag -f dag.md profile-model > /dev/null
task_file=$PWD/dist/servers/$run_id.task.json
T_ID="$(basename "$repo")-$run_id" T_TASK=$task T_REPO=$repo jq -cn \
  '{instance_id: env.T_ID, problem_statement: env.T_TASK, repo_path: env.T_REPO}' > "$task_file"
GENIE_TASK_JSON=$task_file GENIE_RUN_ID=$run_id GENIE_MODEL_NAME=$model \
  GENIE_CONFIG=$PWD/dist/profiles/$profile/agent.yaml GENIE_MODEL_CHOICE=$PWD/dist/model-choice.json \
  GENIE_ARTIFACT_DIR=$artifacts OPENAI_BASE_URL=$OPENAI_BASE_URL OPENAI_API_KEY=$OPENAI_API_KEY \
  "$BASHY" dag -f dag.md main
printf 'genie: done; the change is in %s (git diff), the run record in %s\n' "$repo" "$artifacts" >&2
```

### chat
Requires: pick-model
Effects: read, write, exec, net, spend

The front door (`bashy genie` runs this target): genie as a coding assistant
in the directory the caller is in, on a local model picked for this host
(`GENIE_MODEL_ID`, or `bashy genie -m MODEL`, overrides). The mode follows the
input:

- a message (the arguments), or a message piped on stdin: one turn, answer on
  stdout — the one-off mode;
- no message on a terminal: the interactive session (`bashy ycode`'s terminal
  frontend);
- `GENIE_MODE=web` (`bashy genie web`): the browser chat page on a free
  loopback port; the URL to open, with its one-time token, goes to stderr;
- `GENIE_MODE=resume` / `session` (`bashy genie resume`, `bashy genie session
  list`): continue the latest session, or the session views.

Unlike `solve`, the working tree may be dirty and nothing is recorded beyond
the engine's own session log. The model server is genie's own (`solve`'s),
stopped on exit.

```bsh
set -e
caller=${BASHY_DAG_CALLER_PWD:-$PWD}
mode=${GENIE_MODE:-chat}
message=$(printf '%s' "${BASHY_DAG_ARGS_JSON:-[]}" | jq -r 'join(" ")')
if [ "$mode" = chat ] && [ -z "$message" ] && [ ! -t 0 ]; then
  # A piped message: read it before anything else can touch stdin.
  message=$(cat)
  if [ -z "$message" ]; then
    printf '%s\n' 'genie: the piped message is empty' >&2
    exit 2
  fi
fi
model=$(jq -r .model dist/model-choice.json)
context=$(jq -r '.context // 32768' dist/model-choice.json)
profile=$(printf '%s' "$model" | tr ':/.' '___')
GENIE_PROFILE=$profile GENIE_MODEL_ID=$model GENIE_CONTEXT_TOKENS=$context \
  GENIE_REQUEST_TIMEOUT_MS=${GENIE_REQUEST_TIMEOUT_MS:-600000} "$BASHY" dag -f dag.md profile-model > /dev/null
config=$("$BASHY" cmd/genie/main.go -config "$PWD/dist/profiles/$profile/agent.yaml" -workspace "$caller")
if [ "$mode" = session ]; then
  # A read view over the session log: no model needed.
  args=()
  while IFS= read -r arg; do args+=("$arg"); done < <(printf '%s' "${BASHY_DAG_ARGS_JSON:-[]}" | jq -r '.[]')
  exec "$BASHY" ycode -f "$config" session "${args[@]}"
fi
run_id=${GENIE_RUN_ID:-genie-chat-$(date +%Y%m%d-%H%M%S)}
if [ -z "${GENIE_EXTERNAL_MODEL:-}" ]; then
  . lib/model-server.bsh
  genie_model_server
  export OPENAI_BASE_URL=http://$addr/v1 OPENAI_API_KEY=ollama
else
  printf 'genie: external model %s (%s)\n' "$GENIE_EXTERNAL_MODEL" "$model" >&2
fi
. lib/toolchains.bsh
genie_toolchains "$caller"
case $mode in
  web)
    GENIE_WEB_TOKEN=$(od -An -N16 -tx1 /dev/urandom | tr -d ' \n')
    export GENIE_WEB_TOKEN
    "$BASHY" ycode -f "$config" web
    ;;
  resume)
    # The terminal session runs where the caller is: its commands and the
    # agent's work both land in the caller's tree.
    cd "$caller"
    "$BASHY" ycode -f "$config" resume
    ;;
  *)
    if [ -n "$message" ]; then
      "$BASHY" ycode -f "$config" prompt "$message"
    else
      # A terminal: the interactive session (bashy's default agent TUI),
      # in the caller's directory.
      cd "$caller"
      "$BASHY" ycode -f "$config"
    fi
    ;;
esac
```

### fixture
Sources: fixture/task.json fixture/repo/
Effects: read, write, exec

A one-file Python bug with failing unit tests: the smallest task that
exercises the whole loop (inspect, edit, a contained test run, a patch).
`GENIE_FIXTURE_DIR` picks where the fresh checkout goes (default
`dist/fixture`, replaced on every run: no `Generates`, so it always runs).

```bsh
dir=${GENIE_FIXTURE_DIR:-$PWD/dist/fixture}
case "$dir" in
  /* | [A-Za-z]:[\\/]*) ;;  # absolute, POSIX or a Windows drive path
  *) dir="${BASHY_DAG_CALLER_PWD:-$PWD}/$dir" ;;
esac
rm -rf "$dir/repo"
mkdir -p "$dir"
cp -R fixture/repo "$dir/repo"
git -C "$dir/repo" init -q
git -C "$dir/repo" add -A
git -C "$dir/repo" -c user.name=genie -c user.email=genie@example.invalid commit -q -m fixture
F_REPO="$dir/repo" jq -c '. + {repo_path: env.F_REPO}' fixture/task.json > "$dir/task.json"
printf 'Fixture task: %s\n' "$dir/task.json"
```

### smoke
Effects: read, write, exec, net, spend

The host smoke (`bashy genie smoke [-m MODEL]`): on this host, from the
bundle, a one-off question and a bench-style solve of the fixture, each in a
fresh checkout, on the host's own model pick (or `-m`, a local tag or a
registered API model). One JSON line per step and a summary line go to
stdout; each step's log and the solve diff go to `dist/smoke/RUN/`. The same
body runs on Linux, macOS and Windows.

```bsh
set -e
run=${GENIE_RUN_ID:-smoke-$(date +%Y%m%d-%H%M%S)}
out=$PWD/dist/smoke/$run
mkdir -p "$out"
if ! command -v git > /dev/null 2>&1; then
  git() { "$BASHY" git "$@"; }
fi
problem=$(jq -r .problem_statement fixture/task.json)
fresh() {
  rm -rf "$out/$1"
  cp -R fixture/repo "$out/$1"
  git -C "$out/$1" init -q
  git -C "$out/$1" add -A
  git -C "$out/$1" -c user.name=genie -c user.email=genie@example.invalid commit -q -m fixture
}
failed=0
step() {
  local name=$1 start rc
  shift
  start=$(date +%s)
  set +e
  "$@" > "$out/$name.log" 2>&1
  rc=$?
  set -e
  [ "$rc" -eq 0 ] || failed=$((failed + 1))
  S_NAME=$name S_RC=$rc S_SECS=$(( $(date +%s) - start )) jq -cn \
    '{step: env.S_NAME, rc: (env.S_RC | tonumber), secs: (env.S_SECS | tonumber)}'
}
fresh question
step question awd "$out/question" -- "$BASHY" genie "In one sentence: what does stats.py define? Answer from reading the file."
fresh solve
step solve awd "$out/solve" -- "$BASHY" genie solve "$problem"
git -C "$out/solve" diff > "$out/solve.diff"
lines=$(wc -l < "$out/solve.diff" | tr -d ' ')
S_RUN=$run S_OUT=$out S_LINES=$lines S_FAILED=$failed jq -cn \
  '{smoke: env.S_RUN, logs: env.S_OUT, solve_diff_lines: (env.S_LINES | tonumber), failed_steps: (env.S_FAILED | tonumber)}'
[ "$failed" -eq 0 ]
```

### main
Effects: read, write, exec, net, spend

```bsh
if [ -z "${GENIE_TASK_JSON:-}" ]; then
  printf '%s\n' 'Set GENIE_TASK_JSON to one SWE-bench task JSON file.' >&2
  exit 2
fi
task_path=$GENIE_TASK_JSON
case "$task_path" in
  /* | [A-Za-z]:[\\/]*) ;;  # absolute, POSIX or a Windows drive path
  *) task_path="${BASHY_DAG_CALLER_PWD:-$PWD}/$task_path" ;;
esac
artifact_dir=${GENIE_ARTIFACT_DIR:-${BASHY_DAG_CALLER_PWD:-$PWD}/artifacts}
case "$artifact_dir" in
  /* | [A-Za-z]:[\\/]*) ;;  # absolute, POSIX or a Windows drive path
  *) artifact_dir="${BASHY_DAG_CALLER_PWD:-$PWD}/$artifact_dir" ;;
esac
task_json=$(T_ART=$artifact_dir T_RUN=${GENIE_RUN_ID:-} T_MODEL=${GENIE_MODEL_NAME:-} jq -c '
  {instance_id, problem_statement, repo_path} | with_entries(select(.value != null))
  | .artifact_dir = env.T_ART | .run_id = env.T_RUN | .model_name_or_path = env.T_MODEL' "$task_path") || exit 1
missing=$(printf '%s' "$task_json" | jq -r '. as $t
  | ["instance_id", "problem_statement", "repo_path", "artifact_dir", "run_id", "model_name_or_path"]
  | map(select(($t[.] // "") == "")) | join(", ")') || exit 1
if [ -n "$missing" ]; then
  printf 'missing task fields/environment: %s\n' "$missing" >&2
  exit 1
fi
config_path=${GENIE_CONFIG:-$PWD/agent.yaml}
# The adapter is Go source that bashy runs as Bash# (interpreted): the bundle
# needs no Go toolchain. `build` still makes a native binary when wanted.
printf '%s\n' "$task_json" | "$BASHY" cmd/genie/main.go -config "$config_path"
```
