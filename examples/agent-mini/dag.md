# agent-mini — Bashy workflow

A lightweight harness package. Bashy executes the DAG and its Python fences;
ycode runs the declared agent; the Go adapter bridges a prepared SWE-bench
instance to ycode and emits one prediction. The package deliberately contains
no Bashy, ycode, Python, model, or container runtime binaries.

## Tasks

### build
Sources: cmd/agent-mini/main.go go.mod dag.md
Effects: read, write, exec, net
Generates: dist/bin/agent-mini

```bsh
mkdir -p dist/bin
GOWORK=off "$BASHY" go build -o dist/bin/agent-mini ./cmd/agent-mini
```

### prepare-request
Effects: read

```bsh
~~~py as request
def read_task(path: str) -> str:
    import json
    if not path:
        raise ValueError("set AGENT_MINI_TASK_JSON to a task JSON file")
    with open(path, encoding="utf-8") as stream:
        task = json.load(stream)
    return json.dumps({key: task[key] for key in ("instance_id", "problem_statement", "repo_path") if key in task})
~~~
task_json := request.read_task("$AGENT_MINI_TASK_JSON")
printf '%s\n' "$task_json"
```

### profile-model
Effects: read, write
Generates: dist/profiles/$AGENT_MINI_PROFILE/agent.yaml

```bsh
~~~py as profile
def configure(name: str, model: str) -> str:
    import json
    from pathlib import Path
    import shutil
    allowed = "abcdefghijklmnopqrstuvwxyzABCDEFGHIJKLMNOPQRSTUVWXYZ0123456789-_"
    if not name or not model or any(ch not in allowed for ch in name):
        raise ValueError("set AGENT_MINI_PROFILE and AGENT_MINI_MODEL_ID (profile name: letters, digits, '-' or '_')")
    source = Path("agent.yaml").read_text(encoding="utf-8")
    old = "      id: gpt-5.6"
    if source.count(old) != 1:
        raise ValueError("expected exactly one default model id in agent.yaml")
    target = Path("dist/profiles") / name
    (target / "prompts").mkdir(parents=True, exist_ok=True)
    configured = source.replace(old, "      id: " + json.dumps(model), 1)
    (target / "agent.yaml").write_text(configured, encoding="utf-8")
    shutil.copyfile("prompts/system.md", target / "prompts/system.md")
    return str(target / "agent.yaml")
~~~
profile_path := profile.configure("$AGENT_MINI_PROFILE", "$AGENT_MINI_MODEL_ID")
printf 'Configured model profile: %s\n' "$profile_path"
```

### package
Sources: agent.yaml agent-mini.bsh prompts/system.md cmd/agent-mini/main.go go.mod README.md ATTRIBUTION.md dag.md
Effects: read, write
Generates: dist/agent-mini.bar

```bsh
~~~py as pkg
def reset() -> str:
    import shutil
    from pathlib import Path
    target = Path("dist/package")
    if target.exists():
        shutil.rmtree(target)
    target.mkdir(parents=True)
    return "ready"
~~~
pkg.reset()
cp agent.yaml agent-mini.bsh README.md ATTRIBUTION.md go.mod dag.md dist/package/
mkdir -p dist/package/cmd/agent-mini
cp cmd/agent-mini/main.go dist/package/cmd/agent-mini/main.go
cp -R prompts dist/package/
tar -czf dist/agent-mini.bar -C dist/package .
```

### package-profile
Requires: profile-model package
Sources: dist/profiles/
Effects: read, write
Generates: dist/agent-mini-profile.bar

```bsh
mkdir -p dist/package/profiles
cp -R "dist/profiles/$AGENT_MINI_PROFILE" dist/package/profiles/
tar -czf dist/agent-mini-profile.bar -C dist/package .
```

### main
Requires: build
Effects: read, write, exec, net, spend

```bsh
if [ -z "${AGENT_MINI_TASK_JSON:-}" ]; then
  printf '%s\n' 'Set AGENT_MINI_TASK_JSON to one SWE-bench task JSON file.' >&2
  exit 2
fi
task_path=$AGENT_MINI_TASK_JSON
case "$task_path" in
  /*) ;;
  *) task_path="$BASHY_DAG_CALLER_PWD/$task_path" ;;
esac
artifact_dir=${AGENT_MINI_ARTIFACT_DIR:-$BASHY_DAG_CALLER_PWD/artifacts}
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
task_json := request.read_task("$task_path", "$artifact_dir", "$AGENT_MINI_RUN_ID", "$AGENT_MINI_MODEL_NAME")
config_path=${AGENT_MINI_CONFIG:-$PWD/agent.yaml}
printf '%s\n' "$task_json" | dist/bin/agent-mini -config "$config_path"
```

### run
Requires: main
Effects: read, write, exec, net, spend

```bsh
:
```
