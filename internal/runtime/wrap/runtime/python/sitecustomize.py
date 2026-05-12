"""ycode wrap — Python subprocess interceptor.

Materialized at $YCODE_WRAP_SHIM_DIR/python/sitecustomize.py by
internal/runtime/wrap. The parent wrap process injects
PYTHONPATH=$YCODE_WRAP_SHIM_DIR/python:$PYTHONPATH into the wrapped
agent's env so this module loads automatically on every Python
interpreter startup.

What it does
============

Wraps subprocess.Popen.__init__, subprocess.run, subprocess.call,
subprocess.check_output, os.system, os.popen. For each call:

  - shell-form (shell=True, os.system): sends the command string on
    stdin to `ycode internal-shell-trace`, which parses with
    shellparse + V01-V12 validators and emits OTel spans for the
    parent + each parsed CommandNode.
  - exec-form (list args, shell=False): sends a JSON argv array
    with --argv so the trace records a single command without
    re-parsing through bash.

The hook never raises into the wrapped agent. On any failure
(subprocess spawn error, timeout, JSON decode failure) it writes a
single line to stderr prefixed `[ycode wrap hook]` and proceeds with
the original call. This is the load-bearing failure mode the plan
locked in: telemetry is best-effort; the wrapped agent is never
broken by a ycode trace bug.

Reentry guard
=============

When the hook itself spawns `ycode internal-shell-trace` via
subprocess.run, the wrapped Popen would normally re-invoke the hook
on that call. Setting _YCODE_HOOK_INFLIGHT=1 in the trace-subprocess
env short-circuits the wrappers so the trace call doesn't recurse.

Lifecycle
=========

Python imports `sitecustomize` automatically before any user code
runs (site.py). PYTHONPATH ordering ensures this module wins over
any same-named module the user might have. If a wrapped agent
already ships its own sitecustomize, the user's one runs *after*
this one (PYTHONPATH prepended), preserving any patches they made.
"""
from __future__ import annotations

import json
import os
import subprocess
import sys
from typing import Any, Iterable

_INFLIGHT_ENV = "_YCODE_HOOK_INFLIGHT"
_YCODE_BIN_ENV = "YCODE_BIN"
_TRACE_TIMEOUT_SEC = 5.0

# Process-local reentry guard. Set to True before _call_trace spawns
# the trace subprocess so the wrapped Popen.__init__ that subprocess.run
# itself runs through doesn't recurse. Set back to False afterwards.
# The _INFLIGHT_ENV is still set so the *trace subprocess* knows to
# skip when it inherits this module via PYTHONPATH; the local flag
# protects the *current* process.
_inflight = False


def _ycode_binary() -> str | None:
    """Resolve the ycode binary path the hook should call back into.

    Precedence: $YCODE_BIN env > "ycode" on PATH > None.
    Returns None when ycode is unreachable; the hook then no-ops.
    """
    explicit = os.environ.get(_YCODE_BIN_ENV)
    if explicit and os.path.exists(explicit):
        return explicit
    # PATH lookup — we expect the wrap shim dir to be ahead of /usr/bin
    # so `ycode` resolves to the real ycode binary (the shim entries
    # for bash/git/rg/... are sibling symlinks, but `ycode` itself is
    # never a shim by design — see internal/runtime/wrap/shim.go).
    for d in os.environ.get("PATH", "").split(os.pathsep):
        candidate = os.path.join(d, "ycode")
        if os.path.exists(candidate) and os.access(candidate, os.X_OK):
            return candidate
    return None


def _warn(msg: str) -> None:
    try:
        sys.stderr.write(f"[ycode wrap hook] {msg}\n")
    except Exception:
        # Even logging is best-effort. A broken stderr can't break us.
        pass


def _call_trace(payload: str, argv_mode: bool) -> None:
    """Spawn `ycode internal-shell-trace` with payload on stdin.

    Fire-and-forget for now: we do not consult the JSON envelope's
    `allow` field because Piece 1.2 commits to fail-open semantics
    everywhere. When per-call policy enforcement lands (Phase 2) the
    envelope's deny path will become the source of truth.
    """
    global _inflight
    ycode = _ycode_binary()
    if ycode is None:
        _warn("ycode binary not found; trace skipped")
        return

    env = os.environ.copy()
    env[_INFLIGHT_ENV] = "1"
    args = [ycode, "internal-shell-trace"]
    if argv_mode:
        args.append("--argv")
    _inflight = True
    try:
        subprocess.run(
            args,
            input=payload,
            env=env,
            timeout=_TRACE_TIMEOUT_SEC,
            stdout=subprocess.DEVNULL,
            stderr=subprocess.DEVNULL,
            text=True,
            check=False,
        )
    except subprocess.TimeoutExpired:
        _warn("internal-shell-trace timed out; continuing fail-open")
    except Exception as exc:  # noqa: BLE001  — log+swallow is the contract
        _warn(f"internal-shell-trace error: {exc}; continuing fail-open")
    finally:
        _inflight = False


def _should_skip() -> bool:
    """True when the wrapper must not run (reentry, opt-out env)."""
    if _inflight:
        return True
    if os.environ.get(_INFLIGHT_ENV) == "1":
        return True
    return False


def _trace_argv(argv: Iterable[Any]) -> None:
    if _should_skip():
        return
    try:
        payload = json.dumps([str(x) for x in argv])
    except Exception as exc:  # noqa: BLE001
        _warn(f"argv encode error: {exc}")
        return
    _call_trace(payload, argv_mode=True)


def _trace_shell(cmd: str) -> None:
    if _should_skip():
        return
    _call_trace(cmd, argv_mode=False)


# --- subprocess.Popen ------------------------------------------------

_original_popen_init = subprocess.Popen.__init__


def _wrapped_popen_init(self, args, *posargs, **kwargs):  # type: ignore[no-untyped-def]
    """Pre-spawn hook for the canonical subprocess constructor.

    Every subprocess.run / check_output / check_call / call eventually
    instantiates a Popen, so wrapping __init__ catches the whole
    family with one shim. The two shapes we care about:

      - shell=True: args is a string. Trace as shell-form.
      - shell=False (default): args is a list. Trace as argv-form.
    """
    try:
        shell = bool(kwargs.get("shell", False))
        if shell and isinstance(args, str):
            _trace_shell(args)
        elif not shell and isinstance(args, (list, tuple)):
            _trace_argv(args)
        elif not shell and isinstance(args, str):
            # exec-form with a string single-binary path — uncommon
            # but valid (Popen("git --version", shell=False) would be
            # an error, but Popen("git", shell=False) works).
            _trace_argv([args])
    except Exception as exc:  # noqa: BLE001
        _warn(f"popen-init hook error: {exc}")
    return _original_popen_init(self, args, *posargs, **kwargs)


subprocess.Popen.__init__ = _wrapped_popen_init  # type: ignore[method-assign]


# --- os.system / os.popen --------------------------------------------

_original_os_system = os.system


def _wrapped_os_system(command):  # type: ignore[no-untyped-def]
    try:
        if isinstance(command, str):
            _trace_shell(command)
    except Exception as exc:  # noqa: BLE001
        _warn(f"os.system hook error: {exc}")
    return _original_os_system(command)


os.system = _wrapped_os_system  # type: ignore[assignment]


_original_os_popen = os.popen


def _wrapped_os_popen(command, *posargs, **kwargs):  # type: ignore[no-untyped-def]
    try:
        if isinstance(command, str):
            _trace_shell(command)
    except Exception as exc:  # noqa: BLE001
        _warn(f"os.popen hook error: {exc}")
    return _original_os_popen(command, *posargs, **kwargs)


os.popen = _wrapped_os_popen  # type: ignore[assignment]
