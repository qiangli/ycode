---
name: eval
description: Run aperio benchmarks comparing ycode against other AI coding agents
user_invocable: true
---

# /eval — Benchmark ycode against other coding agents

Run an aperio benchmark spec that compares ycode against one or more competing AI coding tools. Measures speed, cost, token usage, tool calls, and task completion quality.

`{{ARGS}}` is the spec name or path. If empty, list available specs.

## Available Specs

The specs live in `evals/specs/`:

| Spec | Description |
|------|-------------|
| `ycode-vs-claude` | ycode vs Claude Code |
| `ycode-vs-opencode` | ycode vs opencode |
| `ycode-vs-aider` | ycode vs aider |
| `ycode-vs-agents` | ycode vs opencode, aider, codex, gemini-cli |
| `ycode-vs-all-priorart` | ycode vs all agent tools in priorart/ |

## Execution

### Step 1: Resolve the spec

If `{{ARGS}}` is empty or `list`:

```bash
ls -1 evals/specs/*.yaml 2>/dev/null | sed 's|.*/||;s|\.yaml$||'
```

Show the list and ask the user which spec to run. Stop here.

### Step 2: Sync and build priorart tools (cached)

For each tool with source code in `priorart/`, sync to latest and build with caching. Run this script:

```bash
PRIORART_DIR="priorart"
CACHE_DIR="evals/.build-cache"
mkdir -p "$CACHE_DIR"

sync_and_build() {
  local name="$1" dir="$2" branch="$3" build_cmd="$4" bin_check="$5"

  if [ ! -d "$dir/.git" ]; then
    echo "SKIP $name: not a git repo"
    return
  fi

  # Fetch latest
  git -C "$dir" fetch origin "$branch" --quiet 2>/dev/null

  # Get current and remote commit
  local local_sha=$(git -C "$dir" rev-parse HEAD 2>/dev/null)
  local remote_sha=$(git -C "$dir" rev-parse "origin/$branch" 2>/dev/null || echo "unknown")
  local cache_sha=$(cat "$CACHE_DIR/$name.sha" 2>/dev/null || echo "none")

  # Check if binary exists and cache is valid
  if [ "$local_sha" = "$cache_sha" ] && [ -n "$bin_check" ] && eval "$bin_check" >/dev/null 2>&1; then
    echo "CACHED $name ($(echo $local_sha | cut -c1-8))"
    return
  fi

  # Sync to latest
  if [ "$local_sha" != "$remote_sha" ] && [ "$remote_sha" != "unknown" ]; then
    echo "SYNC $name: $(echo $local_sha | cut -c1-8) -> $(echo $remote_sha | cut -c1-8)"
    git -C "$dir" checkout "$branch" --quiet 2>/dev/null
    git -C "$dir" pull origin "$branch" --quiet 2>/dev/null
  fi

  # Build
  echo "BUILD $name..."
  if (cd "$dir" && eval "$build_cmd") 2>&1 | tail -3; then
    git -C "$dir" rev-parse HEAD > "$CACHE_DIR/$name.sha"
    echo "OK $name"
  else
    echo "FAIL $name build"
  fi
}

# Go projects
sync_and_build "openclaw" "$PRIORART_DIR/openclaw" "main" \
  "pnpm install --frozen-lockfile && pnpm build" \
  "test -f $PRIORART_DIR/openclaw/dist/openclaw.mjs"

# Python projects (pip install -e for editable)
sync_and_build "aider" "$PRIORART_DIR/aider" "main" \
  "pip install -e . --quiet" \
  "which aider"

sync_and_build "openhands" "$PRIORART_DIR/openhands" "main" \
  "pip install -e . --quiet" \
  "python3 -c 'import openhands' 2>/dev/null"

sync_and_build "hermes-agent" "$PRIORART_DIR/hermes-agent" "main" \
  "pip install -e . --quiet" \
  "which hermes"

sync_and_build "mini-swe-agent" "$PRIORART_DIR/mini-swe-agent" "main" \
  "pip install -e . --quiet" \
  "which mini"

# Node/TypeScript projects
sync_and_build "codex" "$PRIORART_DIR/codex" "main" \
  "pnpm install --frozen-lockfile && pnpm build" \
  "test -f $PRIORART_DIR/codex/codex-cli/bin/codex.js"

sync_and_build "geminicli" "$PRIORART_DIR/geminicli" "main" \
  "npm install && npm run build" \
  "test -f $PRIORART_DIR/geminicli/bundle/gemini.js"

sync_and_build "gsd-2" "$PRIORART_DIR/gsd-2" "main" \
  "npm install && npm run build" \
  "test -f $PRIORART_DIR/gsd-2/dist/loader.js"

sync_and_build "opencode" "$PRIORART_DIR/opencode" "dev" \
  "bun install && bun run build" \
  "which opencode"
```

Only run this step if the spec references priorart tools. For `ycode-vs-claude`, skip it entirely.

### Step 3: Find aperio

```bash
which aperio 2>/dev/null || ls ~/bin/aperio ~/go/bin/aperio /opt/homebrew/bin/aperio /Users/qiangli/projects/poc/aperio/bin/aperio 2>/dev/null | head -1
```

If not found, tell the user:
> aperio is not installed. Build it:
> ```
> cd /Users/qiangli/projects/poc/aperio && make build && cp bin/aperio ~/bin/
> ```

### Step 4: Verify tool availability and model flags

Before running, check that tools are installed and can accept the model flag:

```bash
echo "=== Tool availability ==="
for tool in ycode claude opencode aider codex gemini claw hermes gsd mini; do
  path=$(which "$tool" 2>/dev/null)
  [ -n "$path" ] && echo "$tool: $path" || echo "$tool: NOT FOUND"
done

echo ""
echo "=== API keys ==="
[ -n "$ANTHROPIC_API_KEY" ] && echo "ANTHROPIC_API_KEY: set" || echo "ANTHROPIC_API_KEY: MISSING"
[ -n "$OPENAI_API_KEY" ] && echo "OPENAI_API_KEY: set" || echo "OPENAI_API_KEY: MISSING"
[ -n "$GOOGLE_API_KEY" ] && echo "GOOGLE_API_KEY: set" || echo "GOOGLE_API_KEY: MISSING"
```

Report results. Tools not in PATH will fail gracefully during the benchmark.

### Step 5: Run the benchmark

```bash
SPEC="evals/specs/{{ARGS}}.yaml"
[ -f "$SPEC" ] || SPEC="{{ARGS}}"

aperio benchmark "$SPEC" --format all
```

### Step 6: Show results

```bash
RESULTS_DIR="evals/results/$(basename "$SPEC" .yaml)"
aperio leaderboard --path "$RESULTS_DIR/leaderboard.json"
```

Suggest: `open "$RESULTS_DIR/report.html"` for the interactive report.

## Model/Provider Configuration

Each tool in the specs uses explicit `--model` flags to ensure consistent model selection. This table shows the flags used:

| Tool | Model Flag | Example |
|------|-----------|---------|
| ycode | `--model <model>` | `ycode prompt --model claude-sonnet-4 "query"` |
| claude | `--model <model>` | `claude --model sonnet -p "query"` |
| opencode | `-m <provider/model>` | `opencode run -m anthropic/claude-sonnet-4 "query"` |
| aider | `--model <model>` | `aider --model claude-3.5-sonnet --message "query"` |
| codex | `-m <model>` | `codex -m o4-mini "query"` |
| gemini | `--model <model>` | `gemini --model gemini-2.5-pro -p "query"` |
| openhands | `LLM_MODEL=<model>` env | `LLM_MODEL=claude-sonnet-4 openhands ...` |

The specs default to each tool's best available model. Override by editing the spec or passing env vars.

## Error Handling

- **Tool not in PATH**: Benchmark logs `"setup failed"` and continues with other tools.
- **API key missing**: Tool fails with auth error; trace shows 0 LLM calls.
- **Build failed**: Sync-and-build reports failure; tool runs with stale binary or skips.
- **Timeout**: Tool is killed and marked failed.
- **All tools failed**: Report clearly and suggest checking prerequisites.

## Custom Specs

Create `evals/specs/my-benchmark.yaml` and run `/eval my-benchmark`. See existing specs for examples.
