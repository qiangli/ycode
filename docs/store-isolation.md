# Per-session store isolation — YCODE_DATA_DIR and fail-loud locking

**Status:** implemented. See `internal/runtime/datadir`, `pkg/memex/store`,
and the storage wiring in `cmd/ycode/main.go` (`cmd/ycode/storage_test.go` is
the gate).

## The problem

ycode's persistent state — the bbolt KV store, the SQLite database, the vector
and search indexes, and the Badger memex graph — is **single-writer** and, by
default, **host-global**: every process on the machine wants
`~/.agents/ycode/projects/data`, and only one can have it. bbolt takes an
exclusive file lock; Badger takes a directory lock.

That is correct for one interactive session and wrong for anything that fans
out (weave workers, parallel evals, two terminals). Worse, the failure mode was
*silent*: the second process logged a warning, ran **degraded** — no memory, no
caches, no code graph — and exited 0. A fan-out of workers therefore reported
success while producing nothing, and the only evidence was a warning line
nobody reads.

## The fix

**1. Per-session storage roots.** The storage root is now resolved by
`internal/runtime/datadir` with this precedence:

| Setting | Effect |
| --- | --- |
| `YCODE_DATA_DIR=/path` | absolute storage root — the direct override |
| `YCODE_HOME=/path` | relocates the whole `~/.agents/ycode` tree; root becomes `<it>/projects/data` |
| *(neither)* | the host-global default, `~/.agents/ycode/projects/data` |

Give each concurrent session its own `YCODE_DATA_DIR` and they stop contending
entirely:

```bash
YCODE_DATA_DIR=/tmp/worker-1 ycode --print "task 1" &
YCODE_DATA_DIR=/tmp/worker-2 ycode --print "task 2" &
```

This is what weave/foreman-style fan-outs should do: one root per sandbox.

**2. Fail loud on contention.** Losing the storage lock is now **fatal**.
ycode waits up to `YCODE_STORE_LOCK_TIMEOUT` (default 5s, a Go duration — long
enough to ride out a peer's shutdown) and then exits **non-zero** with a
message naming the directory, who can hold it, and both ways out:

```
storage at ~/.agents/ycode/projects/data is locked by another ycode process (waited 5s).
  Concurrent sessions must not share one data directory. Either:
    • give this session its own store:  YCODE_DATA_DIR=/path/to/session-data ycode ...
      (or relocate the whole tree with YCODE_HOME=/path/to/agents-home)
    • or run without persistence on purpose:  ycode --no-store ...  (YCODE_NO_STORE=1)
```

**3. Degraded mode is opt-in only.** `--no-store` (or `YCODE_NO_STORE=1`) is
the one way a session may continue without a store, and it announces itself at
startup. What used to be the silent default is now an explicit operator choice.

## Knobs

| Control | Default | Effect |
| --- | --- | --- |
| `YCODE_DATA_DIR` | *(unset)* | per-session storage root |
| `YCODE_HOME` | *(unset)* | alternate `~/.agents/ycode` tree |
| `YCODE_STORE_LOCK_TIMEOUT` | `5s` | how long to wait for a contended lock (`0` = fail immediately) |
| `--no-store` / `YCODE_NO_STORE=1` | off | run without persistent storage instead of failing |

## Implementation map

- `internal/runtime/datadir` — root resolution + lock policy (env parsing,
  precedence, opt-in degraded flag).
- `pkg/memex/store/kv` — `OpenWithTimeout`; a lock timeout is reported as
  `store.ErrLocked` so callers can tell contention apart from corruption.
- `pkg/memex/store/manager.go` — `Config.AllowDegraded`; defaults to false so a
  lost lock is an error, not a warning.
- `cmd/ycode/storage.go` — turns the failure into the operator-actionable
  message above (and a non-zero exit).
