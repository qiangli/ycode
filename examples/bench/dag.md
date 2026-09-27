# bench — the shared benchmark controller (agent-mini · genie · mini-swe-agent)

Runs one agent over a fixed instance subset, collects SWE-bench predictions and
a run record per instance, then evaluates with the official harness. The
controller is Bash# (bashy builtins: `fetch`, `jq`, `git`); the official
evaluator and the mini-swe-agent baseline are Python, used as-is.

Every model call of every arm goes through the host's model door
(`bashy llm`, port 24556) on a sticky binding (`BENCH_STICKY`): one frozen
identity per comparison, shared by the arms, so they compare harnesses, not
routing. Pooled vendors run as `<tool>:<model>` agent bindings on their
subscription seats (`codex:gpt-5.5`, `claude:opus5`).

Container mode runs each instance's official SWE-bench image as ONE
contained call per arm (`@contain(image: …)`): the image pinned by digest,
bashy injected as its entrypoint, the repo at `/testbed`, the tools
read-only, results in `/out`, no network. The one path out — the door on
`BENCH_STICKY` — is this benchmark's own `@contain` provider: custom,
`bench-container` (a registered command on the bench hosts, with
`doorrelay`): the door token never enters the container. Heavy runs (containers, evaluation, real
models) happen on the bench droplets, never the dev box.

Common variables:

| Variable | Default | Meaning |
|---|---|---|
| `BENCH_SET` | `verified` | `verified` (princeton-nlp/SWE-bench_Verified, test) or `rebench-2026_03` (nebius/SWE-rebench-leaderboard) |
| `BENCH_SUBSET` | `subsets/verified-dev50.txt` | instance list |
| `BENCH_AGENT` | `agent-mini` | `agent-mini`, `genie` or `mini-swe-agent` |
| `BENCH_MODEL` | `qwen3:8b` | the model label recorded on each row (the binding decides what serves) |
| `BENCH_STICKY` | required | the arm's sticky key on the door (`bashy llm sticky create KEY --identity DIGEST --ttl 0 --export`) |
| `BENCH_CTX` | `32768` | the context window the arm's harness assumes |
| `BENCH_RUN_ID` | derived | `<set>-<agent>-<model>-<k>`; one directory per run |
| `BENCH_K` | `1` | repetition index (run K times with distinct ids) |
| `BENCH_MODE` | `host` | `host` (repo checkout on this machine) or `container` (inside the task image) |
| `BENCH_LIMIT` | all | stop after N instances |
| `BASHY_CONTAIN_BASHY` | required (container) | the static linux bashy injected into task images (`make build-bashy-scratch`) |
| `BENCH_DOOR` | required (container) | the door the contained calls reach, `/k/<token>` form, e.g. tunnelled from the dev box (`http://127.0.0.1:24556/k/<token>`) |
| `BENCH_TOOLS` | `build` | `prebuilt`: use linux tools already in `$BENCH_HOME/bin/linux-amd64` (a host without the sibling checkouts) |
| `BENCH_HOME` | `~/.cache/bashy-bench` | datasets, git mirrors, workspaces, runs |

## Tasks

### fetch
Effects: read, write, net

```bsh
set -eu
home=${BENCH_HOME:-$HOME/.cache/bashy-bench}
set_name=${BENCH_SET:-verified}
case "$set_name" in
  verified) ds='princeton-nlp%2FSWE-bench_Verified'; split=test ;;
  rebench-2026_03) ds='nebius%2FSWE-rebench-leaderboard'; split=2026_03 ;;
  *) printf 'unknown BENCH_SET %s\n' "$set_name" >&2; exit 2 ;;
esac
mkdir -p "$home/data"
out="$home/data/$set_name.jsonl"
: > "$out.tmp"
offset=0
while :; do
  page=$(fetch --fail --timeout 120s "https://datasets-server.huggingface.co/rows?dataset=$ds&config=default&split=$split&offset=$offset&length=100")
  n=$(printf '%s' "$page" | jq '.rows | length')
  [ "$n" -eq 0 ] && break
  printf '%s' "$page" | jq -c '.rows[].row | {instance_id, repo, base_commit, problem_statement, version, image: (.docker_image // .image_name // null)}' >> "$out.tmp"
  offset=$((offset + n))
  [ "$n" -lt 100 ] && break
done
mv "$out.tmp" "$out"
printf 'fetched %s instances into %s\n' "$(wc -l < "$out" | tr -d ' ')" "$out"
```

### requests
Requires: fetch
Effects: read, write

```bsh
set -eu
home=${BENCH_HOME:-$HOME/.cache/bashy-bench}
set_name=${BENCH_SET:-verified}
subset=${BENCH_SUBSET:-subsets/verified-dev50.txt}
case "$subset" in /*) ;; *) subset="$PWD/$subset" ;; esac
agent=${BENCH_AGENT:-agent-mini}
model=${BENCH_MODEL:-qwen3:8b}
k=${BENCH_K:-1}
run_id=${BENCH_RUN_ID:-$set_name-$agent-$(printf '%s' "$model" | tr ':/' '__')-k$k}
run="$home/runs/$run_id"
mkdir -p "$run"
# bashy's jq takes values through env (no --arg yet)
BENCH_IDS=$(cat "$subset") jq -c '(env.BENCH_IDS | split("\n") | map(select(length > 0))) as $want | select(.instance_id as $i | $want | index($i))' \
  "$home/data/$set_name.jsonl" > "$run/requests.jsonl"
want=$(grep -c . "$subset")
got=$(wc -l < "$run/requests.jsonl" | tr -d ' ')
if [ "$want" != "$got" ]; then
  printf 'subset lists %s ids but %s were found in %s\n' "$want" "$got" "$set_name" >&2
  exit 1
fi
printf '%s\n' "$run_id" > "$home/runs/.last"
printf 'run %s: %s requests\n' "$run_id" "$got"
```

### tools
Effects: read, write, exec, net

Builds the agent-mini adapter and ycode for this machine (host mode) and for
linux/amd64 (container mode) into `$BENCH_HOME/bin`.

```bsh
set -eu
home=${BENCH_HOME:-$HOME/.cache/bashy-bench}
root=$(cd ../.. && pwd)
mkdir -p "$home/bin/host" "$home/bin/linux-amd64"
if [ "${BENCH_TOOLS:-build}" = prebuilt ]; then
  # A bench host without the umbrella's sibling checkouts: the linux tools
  # were cross-built elsewhere and copied into $home/bin/linux-amd64.
  for t in ycode agent-mini doorrelay; do
    [ -x "$home/bin/linux-amd64/$t" ] || { printf 'BENCH_TOOLS=prebuilt: %s/bin/linux-amd64/%s is missing\n' "$home" "$t" >&2; exit 1; }
  done
  printf 'tools prebuilt in %s/bin/linux-amd64\n' "$home"
  exit 0
fi
(cd "$root" && GOWORK=off "$BASHY" go build -o "$home/bin/host/ycode" ./cmd/ycode)
(cd "$root/examples/agent-mini" && GOWORK=off "$BASHY" go build -o "$home/bin/host/agent-mini" ./cmd/agent-mini)
if [ "${BENCH_MODE:-host}" = container ]; then
  (cd doorrelay && CGO_ENABLED=0 GOOS=linux GOARCH=amd64 GOWORK=off "$BASHY" go build -o "$home/bin/linux-amd64/doorrelay" .)
  (cd "$root" && CGO_ENABLED=0 GOOS=linux GOARCH=amd64 GOWORK=off "$BASHY" go build -o "$home/bin/linux-amd64/ycode" ./cmd/ycode)
  (cd "$root/examples/agent-mini" && CGO_ENABLED=0 GOOS=linux GOARCH=amd64 GOWORK=off "$BASHY" go build -o "$home/bin/linux-amd64/agent-mini" ./cmd/agent-mini)
fi
printf 'tools in %s/bin\n' "$home"
```

### prep-container
Requires: tools
Effects: read, write, exec, net

Container mode, once per host: the mini-swe-agent baseline from its pinned
source in a venv on bashy's uv (a standalone CPython, so it runs in any task
image), mounted read-only into every task container with the tools; the
resolved packages are recorded. Checks the injected bashy.

```bsh
set -eu
home=${BENCH_HOME:-$HOME/.cache/bashy-bench}
root=$(cd ../.. && pwd)
: "${BASHY_CONTAIN_BASHY:?set BASHY_CONTAIN_BASHY to a static linux bashy (make build-bashy-scratch)}"
msa="$home/msa"
# A uv-managed standalone CPython under the bench home — never the host's
# system python (its root would be /usr, mounted over the image's own).
export UV_PYTHON_PREFERENCE=only-managed UV_PYTHON_INSTALL_DIR="$home/python"
rm -rf "$msa"
# --no-editable: the venv carries the package itself, so it runs where the
# checkout is not mounted
UV_PROJECT_ENVIRONMENT="$msa" "$BASHY" uv sync --quiet --no-editable --project "$root/examples/agent-mini" --python 3.12
"$BASHY" uv pip freeze --python "$msa/bin/python" > "$home/msa.freeze.txt"
py=$(readlink -f "$msa/bin/python")
case "$py" in
  "$home/python/"*) ;;
  *) printf 'mini-swe-agent python %s is not a managed CPython under %s/python\n' "$py" "$home" >&2; exit 1 ;;
esac
printf '%s\n' "$home/python" > "$home/msa.python-root"
printf 'mini-swe-agent venv %s (python %s); %s packages\n' "$msa" "$py" "$(wc -l < "$home/msa.freeze.txt" | tr -d ' ')"
# this benchmark's @contain provider: custom (the door path), registered here only
if "$BASHY" commands show bench-container > /dev/null 2>&1; then
  "$BASHY" commands set bench-container --set exec.1="$PWD/bench-container.bsh" > /dev/null
else
  "$BASHY" commands add bench-container --set exec.0="$BASHY" --set exec.1="$PWD/bench-container.bsh" \
    --set synopsis="SWE-bench runner: @contain provider with the model door on one sticky binding" > /dev/null
fi
printf 'registered bench-container (@contain provider: custom)\n'
```

### solve
Requires: requests tools
Effects: read, write, exec, net, spend, cred, destroy

Runs the agent on every request of the current run and appends
`predictions.jsonl` and `records.jsonl`. Every model call goes through the
door on `BENCH_STICKY`. The patch of every arm is taken the same way
(`git add -A && git diff --cached --binary HEAD`, what mini-swe-agent's own
submission does), so the arms differ only in the agent.

```bsh
set -eu
home=${BENCH_HOME:-$HOME/.cache/bashy-bench}
run_id=${BENCH_RUN_ID:-$(cat "$home/runs/.last")}
run="$home/runs/$run_id"
agent=${BENCH_AGENT:-agent-mini}
model=${BENCH_MODEL:-qwen3:8b}
mode=${BENCH_MODE:-host}
limit=${BENCH_LIMIT:-0}
ctx=${BENCH_CTX:-32768}
sticky=${BENCH_STICKY:?every model call goes through the door: set BENCH_STICKY (bashy llm sticky create KEY --identity DIGEST --ttl 0 --export)}
root=$(cd ../.. && pwd)
harness_commit=$(git -C "$root" rev-parse HEAD)
config="$home/profiles/$(printf '%s' "$model" | tr ':/' '__')/agent.yaml"
if [ "$agent" = agent-mini ] && [ ! -f "$config" ]; then
  mkdir -p "$(dirname "$config")"
  # per-model changes only: model id, the real context window (never let the
  # harness assume more than the model holds), and a generous request timeout
  sed -e "s|^      id: gpt-5.6$|      id: \"$model\"|" \
      -e "s|^        contextTokens: 128000$|        contextTokens: $ctx|" \
      -e "s|^        requestTimeoutMs: 120000$|        requestTimeoutMs: ${BENCH_REQUEST_TIMEOUT_MS:-600000}|" \
      -e "s|^          timeoutMs: 120000$|          timeoutMs: ${BENCH_REQUEST_TIMEOUT_MS:-600000}|" \
      "$root/examples/agent-mini/agent.yaml" > "$config"
  grep -q "id: \"$model\"" "$config" || { printf 'could not set model id in %s\n' "$config" >&2; exit 1; }
  cp -R "$root/examples/agent-mini/prompts" "$(dirname "$config")/"
fi
# agent-mini's per-task config: ycode resolves the workspace and roots against
# the config file's directory, so the task's config sits beside its prompts.
agent_mini_config() { # $1 workspace, $2 config path, $3 the config's directory as the agent sees it
  ws=$1 icfg=$2 seen=${3:-$(dirname "$2")}
  mkdir -p "$(dirname "$icfg")"
  [ -e "$(dirname "$icfg")/prompts" ] || cp -R "$(dirname "$config")/prompts" "$(dirname "$icfg")/"
  roots="$ws, $seen"
  sed -e "s|^    workspace: \.$|    workspace: $ws|" \
      -e "s|^    readableRoots: \[\.\]$|    readableRoots: [$roots]|" \
      -e "s|^    writableRoots: \[\.\]$|    writableRoots: [$ws]|" "$config" > "$icfg"
  grep -q "^    workspace: $ws$" "$icfg" || { printf 'could not set workspace in %s\n' "$icfg" >&2; exit 1; }
}

# Container mode: the instance's official image as ONE contained call. The
# image is pinned by digest; bashy is its entrypoint; the only network is the
# door on the arm's binding; the tools are read-only; results land in /out.
ref="" out="" ro="" pass=""
if [ "$mode" = container ]; then
  : "${BASHY_CONTAIN_BASHY:?container mode injects a static linux bashy: set BASHY_CONTAIN_BASHY}"
  : "${BENCH_DOOR:?container mode reaches the door through BENCH_DOOR (http://127.0.0.1:24556/k/<token>)}"
  export BASHY_CONTAIN_CUSTOM=bench-container BENCH_STICKY="$sticky"
  ro="$home/bin/linux-amd64:/bench/bin"
  if [ "$agent" = genie ]; then
    # genie's adapter is Bash# Go source: it needs a Go SDK, and the
    # container has no network to fetch one — bashy's own SDK, read-only.
    GOROOT=$("$BASHY" go env GOROOT); export GOROOT
    ro="$ro,$GOROOT:$GOROOT" pass=GOROOT
  fi
  if [ "$agent" = mini-swe-agent ]; then
    [ -f "$home/msa.python-root" ] || { printf 'run prep-container first\n' >&2; exit 1; }
    ro="$ro,$home/msa:$home/msa,$(cat "$home/msa.python-root"):$(cat "$home/msa.python-root"),$root/examples/agent-mini/src/minisweagent/config:/bench/msa-config"
  fi
fi
@contain(image: "$ref", workdir: "/testbed", out: "$out", ro: "$ro", env: "$pass", provider: "custom")
function bench_arm() { # $1 agent, $2 model, $3 context, $4 mini-swe-agent venv
  # the door, on this run's binding only (bench-container mounted the relay)
  /.door-relay relay /.door/door.sock &
  # The image's own test environment, the same for every arm (the image's
  # ~/.bashrc does exactly this for an interactive shell).
  . /opt/miniconda3/etc/profile.d/conda.sh && conda activate testbed
  export PAGER=cat MANPAGER=cat PIP_PROGRESS_BAR=off TQDM_DISABLE=1
  status=0
  case "$1" in
    genie)
      # the image provides the workspace's toolchains (the testbed env)
      GENIE_TOOLCHAINS=none GENIE_EXTERNAL_MODEL=$2 GENIE_MODEL_ID=$2 GENIE_EXTERNAL_CONTEXT=$3 GENIE_ARTIFACT_DIR=/out/genie \
        bashy genie solve "$(jq -r .problem_statement /out/request.json)" > /out/agent.log 2>&1 || status=$?
      ;;
    agent-mini)
      jq -c '{instance_id, problem_statement, repo_path: "/testbed", artifact_dir: "/out/artifacts", run_id: "bench", model_name_or_path: .model}' /out/request.json |
        YCODE_BIN=/bench/bin/ycode /bench/bin/agent-mini -config /out/config/agent.yaml > /out/adapter.json 2> /out/agent.log || status=$?
      ;;
    mini-swe-agent)
      HF_HUB_OFFLINE=1 HF_DATASETS_OFFLINE=1 HF_HOME=/tmp/hf MSWEA_CONFIGURED=true MSWEA_COST_TRACKING=ignore_errors \
        "$4/bin/mini-extra" swebench-single --subset /out/dataset --split test -i "$(jq -r .instance_id /out/request.json)" \
          -m "openai/$2" --environment-class local -c /bench/msa-config/benchmarks/swebench.yaml \
          -y --exit-immediately -l 0 -o /out/traj.json > /out/agent.log 2>&1 || status=$?
      ;;
    *) printf 'unknown agent %s\n' "$1" > /out/agent.log; status=2 ;;
  esac
  git -C /testbed add -A && git -C /testbed diff --cached --binary HEAD > /out/model.patch
  return "$status"
}

touch "$run/predictions.jsonl" "$run/records.jsonl"
n=0
while IFS= read -r req; do
  id=$(printf '%s' "$req" | jq -r .instance_id)
  if grep -qF "\"instance_id\":\"$id\"" "$run/records.jsonl"; then
    continue
  fi
  n=$((n + 1))
  [ "$limit" -gt 0 ] && [ "$n" -gt "$limit" ] && break
  repo=$(printf '%s' "$req" | jq -r .repo)
  base=$(printf '%s' "$req" | jq -r .base_commit)
  started=$(date -u +%Y-%m-%dT%H:%M:%SZ); t0=$(date +%s)
  status=ok image=""
  case "$mode" in
    host)
      eval "$("$BASHY" llm env --sticky "$sticky")"
      mirror="$home/git/$(printf '%s' "$repo" | tr '/' '_').git"
      [ -d "$mirror" ] || git clone --quiet --mirror "https://github.com/$repo.git" "$mirror"
      ws="$home/work/$run_id/$id"
      rm -rf "$ws"; mkdir -p "$(dirname "$ws")"
      git clone --quiet --shared "$mirror" "$ws"
      git -C "$ws" checkout --quiet "$base"
      case "$agent" in
        agent-mini)
          icfg="$(dirname "$config")/instances/$id.yaml"
          agent_mini_config "$ws" "$icfg"
          printf '%s' "$req" | B_WS="$ws" B_ART="$run/artifacts" B_RUN="$run_id" B_MODEL="$model" jq -c \
            '{instance_id, problem_statement, repo_path: env.B_WS, artifact_dir: env.B_ART, run_id: env.B_RUN, model_name_or_path: env.B_MODEL}' |
          YCODE_BIN="$home/bin/host/ycode" "$home/bin/host/agent-mini" -config "$icfg" > /dev/null 2>> "$run/stderr.log" || status=failed
          ;;
        mini-swe-agent)
          task=$(printf '%s' "$req" | jq -r .problem_statement)
          (cd "$ws" && MSWEA_CONFIGURED=true MSWEA_COST_TRACKING=ignore_errors \
            uv run --quiet --project "$root/examples/agent-mini" mini -y --exit-immediately \
              -m "openai/$model" -t "$task" -o "$run/trajs/$id.traj.json" -l 0 >> "$run/stderr.log" 2>&1) || status=failed
          ;;
        genie)
          (cd "$ws" && GENIE_EXTERNAL_MODEL=$model GENIE_MODEL_ID=$model GENIE_EXTERNAL_CONTEXT=$ctx GENIE_ARTIFACT_DIR="$run/artifacts/$id" \
            "$BASHY" genie solve "$(printf '%s' "$req" | jq -r .problem_statement)" >> "$run/stderr.log" 2>&1) || status=failed
          ;;
        *) printf 'agent %s not wired\n' "$agent" >&2; exit 2 ;;
      esac
      patch=$(git -C "$ws" add -A && git -C "$ws" diff --cached --binary HEAD --)
      ;;
    container)
      name="docker.io/swebench/sweb.eval.x86_64.$(printf '%s' "$id" | sed 's/__/_1776_/'):latest"
      pins="$home/pins.tsv"; touch "$pins"
      ref=$(awk -v n="$name" '$1 == n { print $2 }' "$pins" | tail -1)
      if [ -z "$ref" ]; then
        # pin once: pull the tag, record its digest; every call names the digest
        "$BASHY" podman pull -q "$name" > /dev/null
        digest=$("$BASHY" podman image inspect --format '{{.Digest}}' "$name")
        ref="${name%:*}@$digest"
        printf '%s\t%s\n' "$name" "$ref" >> "$pins"
      fi
      image=$ref
      out="$run/out/$id"
      rm -rf "$out"; mkdir -p "$out/dataset"
      printf '%s' "$req" | B_MODEL="$model" jq -c '. + {model: env.B_MODEL}' > "$out/request.json"
      printf '%s\n' "$req" > "$out/dataset/test.jsonl"
      [ "$agent" = agent-mini ] && agent_mini_config /testbed "$out/config/agent.yaml" /out/config
      # < /dev/null: the container must not read the loop's requests
      bench_arm "$agent" "$model" "$ctx" "$home/msa" < /dev/null || status=failed
      patch=$(cat "$out/model.patch" 2>/dev/null || true)
      # mini-swe-agent's prediction is its own submission (upstream's way):
      # its submit step writes patch.txt into the repo, which a plain
      # `git add -A` diff would nest inside the patch (unappliable)
      if [ "$agent" = mini-swe-agent ] && [ -f "$out/traj.json" ]; then
        patch=$(jq -r '.info.submission // ""' "$out/traj.json")
      fi
      ;;
  esac
  B_ID="$id" B_MODEL="$model" B_PATCH="$patch" jq -cn '{instance_id: env.B_ID, model_name_or_path: env.B_MODEL, model_patch: env.B_PATCH}' >> "$run/predictions.jsonl"
  t1=$(date +%s)
  B_ID="$id" B_AGENT="$agent" B_MODEL="$model" B_MODE="$mode" B_RUN="$run_id" B_COMMIT="$harness_commit" B_STICKY="$sticky" B_IMAGE="$image" \
  B_STARTED="$started" B_STATUS="$status" B_SECS=$((t1 - t0)) jq -cn \
     '{instance_id: env.B_ID, run_id: env.B_RUN, agent: env.B_AGENT, model: env.B_MODEL, sticky: env.B_STICKY, image: env.B_IMAGE, mode: env.B_MODE, harness_commit: env.B_COMMIT, started_at_utc: env.B_STARTED, wall_secs: (env.B_SECS | tonumber), status: env.B_STATUS}' \
     >> "$run/records.jsonl"
  printf '%s %s %ss\n' "$id" "$status" "$((t1 - t0))"
done < "$run/requests.jsonl"
printf 'run %s: %s predictions\n' "$run_id" "$(wc -l < "$run/predictions.jsonl" | tr -d ' ')"
```

### score
Effects: read, write, exec, net, persist

Scores the current run with SWE-bench's own harness
(`swebench.harness.run_evaluation`, the version in `BENCH_SWEBENCH`) on this
host's podman through its Docker-compatible socket: the same official
instance images, no hosted evaluator. Writes the harness report and
`$run/eval/resolved.jsonl` (one line per prediction: resolved or not).

```bsh
set -eu
home=${BENCH_HOME:-$HOME/.cache/bashy-bench}
run_id=${BENCH_RUN_ID:-$(cat "$home/runs/.last")}
run="$home/runs/$run_id"
set_name=${BENCH_SET:-verified}
case "$set_name" in
  verified) ds=princeton-nlp/SWE-bench_Verified; split=test ;;
  *) printf 'score: no official dataset name for %s\n' "$set_name" >&2; exit 2 ;;
esac
[ -s "$run/predictions.jsonl" ] || { printf 'score: %s has no predictions\n' "$run" >&2; exit 1; }
mkdir -p "$run/eval"
sock="$home/podman.sock"
if [ ! -S "$sock" ]; then
  "$BASHY" podman system service --time=0 "unix://$sock" > "$home/podman-service.log" 2>&1 &
  until [ -S "$sock" ]; do sleep 1; done
fi
export DOCKER_HOST="unix://$sock"
# the harness takes a JSON list or JSONL; predictions.jsonl is JSONL
(cd "$run/eval" && "$BASHY" uv run --quiet --no-project --python 3.12 --with "swebench==${BENCH_SWEBENCH:-4.1.0}" \
  python -m swebench.harness.run_evaluation --dataset_name "$ds" --split "$split" \
    --predictions_path "$run/predictions.jsonl" --run_id "$run_id" --max_workers "${BENCH_EVAL_WORKERS:-1}" \
    --namespace swebench --cache_level instance --report_dir "$run/eval") > "$run/eval/harness.log" 2>&1 || {
  tail -20 "$run/eval/harness.log" >&2; exit 1; }
report=$(ls "$run/eval"/*."$run_id".json 2>/dev/null | head -1)
[ -n "$report" ] || { printf 'score: no harness report in %s\n' "$run/eval" >&2; exit 1; }
# bashy's jq takes values through env
B_RESOLVED=$(jq -c '.resolved_ids // []' "$report") jq -c \
  '(env.B_RESOLVED | fromjson) as $r | .instance_id as $i | {instance_id: $i, resolved: ($r | index($i) != null)}' \
  "$run/predictions.jsonl" > "$run/eval/resolved.jsonl"
printf 'run %s: %s of %s resolved (report %s)\n' "$run_id" \
  "$(grep -c '"resolved":true' "$run/eval/resolved.jsonl" || true)" "$(wc -l < "$run/eval/resolved.jsonl" | tr -d ' ')" "$report"
```

### smoke
Effects: read, write, exec, net

The dev-box check: one instance, host mode, a small local model. Proves the
pipeline (fetch → requests → run → prediction), not a score.

```bsh
set -eu
export BENCH_LIMIT=${BENCH_LIMIT:-1}
export BENCH_RUN_ID=${BENCH_RUN_ID:-smoke-${BENCH_AGENT:-agent-mini}-$(date -u +%Y%m%dT%H%M%SZ)}
"$BASHY" dag -f dag.md solve
```
