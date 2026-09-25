# bench — the shared benchmark controller (agent-mini · genie · mini-swe-agent)

Runs one agent over a fixed instance subset, collects SWE-bench predictions and
a run record per instance, then evaluates with the official harness. The
controller is Bash# (bashy builtins: `fetch`, `jq`, `git`); the official
evaluator and the mini-swe-agent baseline are Python, used as-is.

Heavy runs (containers, evaluation, real models) happen on the approved
benchmark host, never the dev box. The dev box only runs `smoke`.

Common variables:

| Variable | Default | Meaning |
|---|---|---|
| `BENCH_SET` | `verified` | `verified` (princeton-nlp/SWE-bench_Verified, test) or `rebench-2026_03` (nebius/SWE-rebench-leaderboard) |
| `BENCH_SUBSET` | `subsets/verified-dev50.txt` | instance list |
| `BENCH_AGENT` | `agent-mini` | `agent-mini`, `genie` or `mini-swe-agent` |
| `BENCH_MODEL` | `qwen3:8b` | Ollama model name |
| `BENCH_RUN_ID` | derived | `<set>-<agent>-<model>-<k>`; one directory per run |
| `BENCH_K` | `1` | repetition index (run K times with distinct ids) |
| `BENCH_MODE` | `host` | `host` (repo checkout on this machine) or `container` (inside the task image) |
| `BENCH_LIMIT` | all | stop after N instances |
| `OLLAMA_BASE` | `http://localhost:11434` | Ollama endpoint (OpenAI-compatible under `/v1`); genie will start its own on a random port (G0.10) |
| `BENCH_CTX` | `32768` | context window the model server was started with (profile `contextTokens`) |
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
(cd "$root" && GOWORK=off "$BASHY" go build -o "$home/bin/host/ycode" ./cmd/ycode)
(cd "$root/examples/agent-mini" && GOWORK=off "$BASHY" go build -o "$home/bin/host/agent-mini" ./cmd/agent-mini)
if [ "${BENCH_MODE:-host}" = container ]; then
  (cd "$root" && CGO_ENABLED=0 GOOS=linux GOARCH=amd64 GOWORK=off "$BASHY" go build -o "$home/bin/linux-amd64/ycode" ./cmd/ycode)
  (cd "$root/examples/agent-mini" && CGO_ENABLED=0 GOOS=linux GOARCH=amd64 GOWORK=off "$BASHY" go build -o "$home/bin/linux-amd64/agent-mini" ./cmd/agent-mini)
fi
printf 'tools in %s/bin\n' "$home"
```

### solve
Requires: requests tools
Effects: read, write, exec, net, spend, cred, destroy

Runs the agent on every request of the current run and appends
`predictions.jsonl` and `records.jsonl`.

```bsh
set -eu
home=${BENCH_HOME:-$HOME/.cache/bashy-bench}
run_id=${BENCH_RUN_ID:-$(cat "$home/runs/.last")}
run="$home/runs/$run_id"
agent=${BENCH_AGENT:-agent-mini}
model=${BENCH_MODEL:-qwen3:8b}
mode=${BENCH_MODE:-host}
limit=${BENCH_LIMIT:-0}
ollama=${OLLAMA_BASE:-http://localhost:11434}
root=$(cd ../.. && pwd)
harness_commit=$(git -C "$root" rev-parse HEAD)
config="$home/profiles/$(printf '%s' "$model" | tr ':/' '__')/agent.yaml"
if [ "$agent" = agent-mini ] && [ ! -f "$config" ]; then
  mkdir -p "$(dirname "$config")"
  # the only per-model change: the model id (the profile-model recipe in agent-mini does the same)
  ctx=${BENCH_CTX:-32768}
  # per-model changes only: model id, the real context window (never let the
  # harness assume more than the server holds), and a local-inference timeout
  sed -e "s|^      id: gpt-5.6$|      id: \"$model\"|" \
      -e "s|^        contextTokens: 128000$|        contextTokens: $ctx|" \
      -e "s|^        requestTimeoutMs: 120000$|        requestTimeoutMs: ${BENCH_REQUEST_TIMEOUT_MS:-600000}|" \
      -e "s|^          timeoutMs: 120000$|          timeoutMs: ${BENCH_REQUEST_TIMEOUT_MS:-600000}|" \
      "$root/examples/agent-mini/agent.yaml" > "$config"
  grep -q "id: \"$model\"" "$config" || { printf 'could not set model id in %s\n' "$config" >&2; exit 1; }
  cp -R "$root/examples/agent-mini/prompts" "$(dirname "$config")/"
fi
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
  status=ok
  case "$mode" in
    host)
      mirror="$home/git/$(printf '%s' "$repo" | tr '/' '_').git"
      [ -d "$mirror" ] || git clone --quiet --mirror "https://github.com/$repo.git" "$mirror"
      ws="$home/work/$run_id/$id"
      rm -rf "$ws"; mkdir -p "$(dirname "$ws")"
      git clone --quiet --shared "$mirror" "$ws"
      git -C "$ws" checkout --quiet "$base"
      case "$agent" in
        agent-mini)
          # ycode resolves runtime.workspace and the roots against the config
          # file's directory, so each task gets a config whose workspace is
          # the checkout (prompts stay beside it).
          icfg="$(dirname "$config")/instances/$id.yaml"
          mkdir -p "$(dirname "$icfg")"
          [ -e "$(dirname "$icfg")/prompts" ] || cp -R "$(dirname "$config")/prompts" "$(dirname "$icfg")/"
          sed -e "s|^    workspace: \.$|    workspace: $ws|" \
              -e "s|^    readableRoots: \[\.\]$|    readableRoots: [$ws]|" \
              -e "s|^    writableRoots: \[\.\]$|    writableRoots: [$ws]|" "$config" > "$icfg"
          grep -q "^    workspace: $ws$" "$icfg" || { printf 'could not set workspace in %s\n' "$icfg" >&2; exit 1; }
          printf '%s' "$req" | B_WS="$ws" B_ART="$run/artifacts" B_RUN="$run_id" B_MODEL="$model" jq -c \
            '{instance_id, problem_statement, repo_path: env.B_WS, artifact_dir: env.B_ART, run_id: env.B_RUN, model_name_or_path: env.B_MODEL}' |
          OPENAI_BASE_URL="$ollama/v1" OPENAI_API_KEY=ollama YCODE_BIN="$home/bin/host/ycode" \
            "$home/bin/host/agent-mini" -config "$icfg" >> "$run/predictions.jsonl" 2>> "$run/stderr.log" || status=failed
          ;;
        mini-swe-agent)
          task=$(printf '%s' "$req" | jq -r .problem_statement)
          (cd "$ws" && OLLAMA_API_BASE="$ollama" MSWEA_CONFIGURED=true \
            uv run --quiet --project "$root/examples/agent-mini" mini -y --exit-immediately \
              -m "ollama_chat/$model" -t "$task" -o "$run/trajs/$id.traj.json" -l 0 >> "$run/stderr.log" 2>&1) || status=failed
          patch=$(git -C "$ws" diff --binary HEAD --)
          B_ID="$id" B_MODEL="$model" B_PATCH="$patch" jq -cn '{instance_id: env.B_ID, model_name_or_path: env.B_MODEL, model_patch: env.B_PATCH}' >> "$run/predictions.jsonl"
          ;;
        *) printf 'agent %s not wired for host mode yet\n' "$agent" >&2; exit 2 ;;
      esac
      ;;
    container)
      printf 'container mode runs on the benchmark host: see README (not on the dev box)\n' >&2; exit 2
      ;;
  esac
  t1=$(date +%s)
  B_ID="$id" B_AGENT="$agent" B_MODEL="$model" B_MODE="$mode" B_RUN="$run_id" B_COMMIT="$harness_commit" \
  B_STARTED="$started" B_STATUS="$status" B_SECS=$((t1 - t0)) jq -cn \
     '{instance_id: env.B_ID, run_id: env.B_RUN, agent: env.B_AGENT, model: env.B_MODEL, mode: env.B_MODE, harness_commit: env.B_COMMIT, started_at_utc: env.B_STARTED, wall_secs: (env.B_SECS | tonumber), status: env.B_STATUS}' \
     >> "$run/records.jsonl"
  printf '%s %s %ss\n' "$id" "$status" "$((t1 - t0))"
done < "$run/requests.jsonl"
printf 'run %s: %s predictions\n' "$run_id" "$(wc -l < "$run/predictions.jsonl" | tr -d ' ')"
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
