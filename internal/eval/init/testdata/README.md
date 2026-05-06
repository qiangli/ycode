# /init replay cassette

`init.cassette.yaml` is the recorded LLM-API response stream that backs
`make eval-init`. Once recorded and committed, the eval is fully offline
— CI replays without any provider key.

## Recording the cassette

Recording is a one-time bootstrap step. Re-record only when the LLM
provider's response shape drifts enough that replay starts failing.

### Prerequisites

- A working `bin/ycode` (`make compile`).
- A working `aperio` CLI (`make build` inside `peers/aperio/`, or any
  recent `aperio` on `$PATH`).
- A provider API key in your environment, exposed under the conventional
  name (`ANTHROPIC_API_KEY`). If you have it under a different env var
  locally, alias it for the recording session only:

  ```bash
  export ANTHROPIC_API_KEY="$<YOUR_LOCAL_PROVIDER_KEY_VAR>"
  ```

### Steps

```bash
# Fresh fixture repo so /init scaffolds against an empty tree.
mkdir -p /tmp/ycode-init-fixture && cd /tmp/ycode-init-fixture

# Record. aperio MITM-captures the LLM call and emits a YAML cassette.
aperio record \
  -o /tmp/init-trace.json \
  --cassette $REPO/internal/eval/init/testdata/init.cassette.yaml \
  -- $REPO/bin/ycode --once "/init"

# Sanity-check the cassette: confirm no provider key, no absolute /Users
# paths, no internal hostnames slipped through. Aperio applies a default
# redactor but always verify before committing.
grep -nE '(sk-[A-Za-z0-9_-]{20,}|/Users/|/home/[^/]+/)' \
  $REPO/internal/eval/init/testdata/init.cassette.yaml \
  && echo "FOUND — sanitize before committing" || echo "clean"

# Commit the cassette.
cd $REPO
git add internal/eval/init/testdata/init.cassette.yaml
git commit -m "test(eval): record /init cassette"
```

### Replay

```bash
# CI does this automatically via make eval-init. Local equivalent:
go test -count=1 ./internal/eval/init/...
```

## Why a cassette and not a stub?

A hand-written stub LLM response would not exercise the streaming
delta path (the LLM's response gets chopped into chunks; the cassette
preserves the chunk boundaries) and would drift from the real API
shape over time. Aperio's record/replay is built for exactly this
case — same binary, same code path, deterministic LLM input.

## Refresh policy

If the LLM provider's response shape changes (e.g. a new `tool_use`
block format, a renamed streaming event), `make eval-init` will fail
on a fingerprint mismatch. Re-record with the steps above. Capture
the diff in the same commit so the change is reviewable.
