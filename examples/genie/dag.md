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
Sources: agent.yaml genie.bsh prompts/system.md cmd/genie/main.go go.mod README.md ATTRIBUTION.md LICENSE.md LICENSE-live-swe-agent.md LICENSE-mini-swe-agent.md dag.md fixture/task.json fixture/repo/
Effects: read, write
Generates: dist/genie.bar

```bsh
set -e
rm -rf dist/package
mkdir -p dist/package
cp agent.yaml genie.bsh README.md ATTRIBUTION.md LICENSE.md LICENSE-live-swe-agent.md LICENSE-mini-swe-agent.md go.mod dag.md dist/package/
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

### fixture
Sources: fixture/task.json fixture/repo/
Effects: read, write, exec
Generates: dist/fixture/task.json

A one-file Python bug with failing unit tests: the smallest task that
exercises the whole loop (inspect, edit, a contained test run, a patch).
`GENIE_FIXTURE_DIR` picks where the fresh checkout goes (default
`dist/fixture`, replaced on every run).

```bsh
dir=${GENIE_FIXTURE_DIR:-$PWD/dist/fixture}
case "$dir" in
  /*) ;;
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

### main
Requires: build
Effects: read, write, exec, net, spend

```bsh
if [ -z "${GENIE_TASK_JSON:-}" ]; then
  printf '%s\n' 'Set GENIE_TASK_JSON to one SWE-bench task JSON file.' >&2
  exit 2
fi
task_path=$GENIE_TASK_JSON
case "$task_path" in
  /*) ;;
  *) task_path="${BASHY_DAG_CALLER_PWD:-$PWD}/$task_path" ;;
esac
artifact_dir=${GENIE_ARTIFACT_DIR:-${BASHY_DAG_CALLER_PWD:-$PWD}/artifacts}
case "$artifact_dir" in
  /*) ;;
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
printf '%s\n' "$task_json" | dist/bin/genie -config "$config_path"
```
