# genie — Bashy workflow

bashy genie: the growing SWE agent, seeded from agent-mini. Bashy executes the DAG and its Python fences;
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
~~~py as request
def read_task(path: str) -> str:
    import json
    if not path:
        raise ValueError("set GENIE_TASK_JSON to a task JSON file")
    with open(path, encoding="utf-8") as stream:
        task = json.load(stream)
    return json.dumps({key: task[key] for key in ("instance_id", "problem_statement", "repo_path") if key in task})
~~~
task_json := request.read_task("$GENIE_TASK_JSON")
printf '%s\n' "$task_json"
```

### profile-model
Effects: read, write
Generates: dist/profiles/$GENIE_PROFILE/agent.yaml

```bsh
~~~py as profile
def configure(base: str, name: str, model: str, context: str, timeout: str) -> str:
    import json
    from pathlib import Path
    import shutil
    allowed = "abcdefghijklmnopqrstuvwxyzABCDEFGHIJKLMNOPQRSTUVWXYZ0123456789-_"
    if not name or not model or any(ch not in allowed for ch in name):
        raise ValueError("set GENIE_PROFILE and GENIE_MODEL_ID (profile name: letters, digits, '-' or '_')")
    base = Path(base)
    source = (base / "agent.yaml").read_text(encoding="utf-8")
    old = "      id: gpt-5.6"
    if source.count(old) != 1:
        raise ValueError("expected exactly one default model id in agent.yaml")
    target = base / "dist/profiles" / name
    (target / "prompts").mkdir(parents=True, exist_ok=True)
    configured = source.replace(old, "      id: " + json.dumps(model), 1)
    # A local server holds less context and answers slower than a hosted
    # API: never let the harness assume more than the server has.
    for key, value in (("contextTokens: 128000", context), ("requestTimeoutMs: 120000", timeout), ("timeoutMs: 120000", timeout)):
        if value:
            if not value.isdigit():
                raise ValueError("GENIE_CONTEXT_TOKENS and GENIE_REQUEST_TIMEOUT_MS must be integers")
            configured = configured.replace(" " + key + "\n", " " + key.split(":")[0] + ": " + value + "\n")
    (target / "agent.yaml").write_text(configured, encoding="utf-8")
    shutil.copyfile(base / "prompts/system.md", target / "prompts/system.md")
    return str(target / "agent.yaml")
~~~
profile_path := profile.configure("$PWD", "$GENIE_PROFILE", "$GENIE_MODEL_ID", "${GENIE_CONTEXT_TOKENS:-}", "${GENIE_REQUEST_TIMEOUT_MS:-}")
printf 'Configured model profile: %s\n' "$profile_path"
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
  *) dir="$BASHY_DAG_CALLER_PWD/$dir" ;;
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
  *) task_path="$BASHY_DAG_CALLER_PWD/$task_path" ;;
esac
artifact_dir=${GENIE_ARTIFACT_DIR:-$BASHY_DAG_CALLER_PWD/artifacts}
case "$artifact_dir" in
  /*) ;;
  *) artifact_dir="$BASHY_DAG_CALLER_PWD/$artifact_dir" ;;
esac
~~~py as request
def read_task(path: str, artifact_dir: str, run_id: str, model_name: str) -> str:
    import json
    with open(path, encoding="utf-8") as stream:
        task = json.load(stream)
    task = {key: task[key] for key in ("instance_id", "problem_statement", "repo_path") if key in task}
    task["artifact_dir"] = artifact_dir
    task["run_id"] = run_id
    task["model_name_or_path"] = model_name or task.get("model_name_or_path", "")
    required = ("instance_id", "problem_statement", "repo_path", "artifact_dir", "run_id", "model_name_or_path")
    missing = [key for key in required if not task.get(key)]
    if missing:
        raise ValueError("missing task fields/environment: " + ", ".join(missing))
    return json.dumps(task)
~~~
task_json := request.read_task("$task_path", "$artifact_dir", "$GENIE_RUN_ID", "$GENIE_MODEL_NAME")
config_path=${GENIE_CONFIG:-$PWD/agent.yaml}
printf '%s\n' "$task_json" | dist/bin/genie -config "$config_path"
```
