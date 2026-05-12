/**
 * ycode wrap — Node.js child_process interceptor.
 *
 * Materialized at $YCODE_WRAP_SHIM_DIR/node/ycode-trace.cjs by
 * internal/runtime/wrap. The parent wrap process injects
 *   NODE_OPTIONS=--require=$YCODE_WRAP_SHIM_DIR/node/ycode-trace.cjs
 * so this module loads automatically on every Node interpreter
 * startup (including Bun for opencode — `NODE_OPTIONS=--require` is
 * honored by Bun).
 *
 * What it does
 * ============
 *
 * Wraps child_process.spawn, exec, execFile, fork, plus the sync
 * variants spawnSync / execSync / execFileSync. For each call:
 *
 *   - shell-form (child_process.exec, or spawn with options.shell):
 *     sends the command string on stdin to `ycode internal-shell-trace`,
 *     which parses with shellparse + V01-V12 validators and emits OTel
 *     spans for the parent + each parsed CommandNode.
 *
 *   - exec-form (spawn with array args, fork): sends a JSON argv array
 *     with --argv so the trace records a single command without
 *     re-parsing through bash.
 *
 * Fail-open
 * =========
 *
 * The hook never throws into the wrapped agent. On any failure
 * (spawn error, timeout, JSON decode failure) it writes a single line
 * to stderr prefixed `[ycode wrap hook]` and proceeds with the
 * original call. Same load-bearing failure mode as the Python hook.
 *
 * Reentry guard
 * =============
 *
 * The trace subprocess is itself a child_process.spawnSync call — we
 * set _YCODE_HOOK_INFLIGHT=1 in its env so the wrappers short-circuit
 * for that call and don't recurse.
 */
"use strict";

const child_process = require("node:child_process");

const INFLIGHT_ENV = "_YCODE_HOOK_INFLIGHT";
const YCODE_BIN_ENV = "YCODE_BIN";
const TRACE_TIMEOUT_MS = 5000;

// Process-local reentry guard. Set to true before callTrace's
// spawnSync so the wrapped spawn/spawnSync the trace call itself goes
// through doesn't re-emit a trace. The INFLIGHT_ENV var is also set
// in the child so any *grandchild* Node process inheriting this hook
// via NODE_OPTIONS skips too.
let inflight = false;

function warn(msg) {
  try {
    process.stderr.write(`[ycode wrap hook] ${msg}\n`);
  } catch (_e) {
    // Even logging is best-effort.
  }
}

function ycodeBinary() {
  const explicit = process.env[YCODE_BIN_ENV];
  if (explicit) {
    try {
      require("node:fs").accessSync(explicit, require("node:fs").constants.X_OK);
      return explicit;
    } catch (_) {
      // fall through
    }
  }
  const path = require("node:path");
  const fs = require("node:fs");
  const parts = (process.env.PATH || "").split(path.delimiter);
  for (const dir of parts) {
    const candidate = path.join(dir, "ycode");
    try {
      fs.accessSync(candidate, fs.constants.X_OK);
      return candidate;
    } catch (_) {
      // not here
    }
  }
  return null;
}

function shouldSkip() {
  return inflight || process.env[INFLIGHT_ENV] === "1";
}

function callTrace(payload, argvMode) {
  const bin = ycodeBinary();
  if (!bin) {
    warn("ycode binary not found; trace skipped");
    return;
  }
  const env = { ...process.env, [INFLIGHT_ENV]: "1" };
  const args = ["internal-shell-trace"];
  if (argvMode) args.push("--argv");
  inflight = true;
  try {
    // spawnSync so the hook blocks until trace completes — keeps span
    // ordering deterministic and gives the trace process a chance to
    // emit OTel spans before the wrapped agent proceeds. The wrapped
    // agent never sees the trace's stdout/stderr (devnull).
    child_process.spawnSync(bin, args, {
      input: payload,
      env,
      timeout: TRACE_TIMEOUT_MS,
      stdio: ["pipe", "ignore", "ignore"],
    });
  } catch (e) {
    warn(`internal-shell-trace error: ${e.message}; continuing fail-open`);
  } finally {
    inflight = false;
  }
}

function traceShell(cmd) {
  if (shouldSkip()) return;
  if (typeof cmd !== "string") return;
  callTrace(cmd, false);
}

function traceArgv(file, args) {
  if (shouldSkip()) return;
  const argv = [String(file), ...(Array.isArray(args) ? args.map(String) : [])];
  let payload;
  try {
    payload = JSON.stringify(argv);
  } catch (e) {
    warn(`argv encode error: ${e.message}`);
    return;
  }
  callTrace(payload, true);
}

// --- child_process.exec / execSync (shell-form, single string) -------

const origExec = child_process.exec;
child_process.exec = function patchedExec(command, ...rest) {
  try {
    traceShell(command);
  } catch (e) {
    warn(`exec hook error: ${e.message}`);
  }
  return origExec.call(this, command, ...rest);
};

const origExecSync = child_process.execSync;
child_process.execSync = function patchedExecSync(command, ...rest) {
  try {
    traceShell(command);
  } catch (e) {
    warn(`execSync hook error: ${e.message}`);
  }
  return origExecSync.call(this, command, ...rest);
};

// --- child_process.execFile / execFileSync (exec-form, file + args) --

const origExecFile = child_process.execFile;
child_process.execFile = function patchedExecFile(file, args, ...rest) {
  try {
    traceArgv(file, args);
  } catch (e) {
    warn(`execFile hook error: ${e.message}`);
  }
  return origExecFile.call(this, file, args, ...rest);
};

const origExecFileSync = child_process.execFileSync;
child_process.execFileSync = function patchedExecFileSync(file, args, ...rest) {
  try {
    traceArgv(file, args);
  } catch (e) {
    warn(`execFileSync hook error: ${e.message}`);
  }
  return origExecFileSync.call(this, file, args, ...rest);
};

// --- child_process.spawn / spawnSync ---------------------------------

function isShellOpts(opts) {
  return opts && typeof opts === "object" && Boolean(opts.shell);
}

function traceSpawn(file, args, opts) {
  // spawn("git status", { shell: true }) is shell-form even though
  // args is undefined; spawn("git", ["status"]) is exec-form.
  if (isShellOpts(opts)) {
    if (typeof file === "string") traceShell(file);
    return;
  }
  traceArgv(file, args);
}

const origSpawn = child_process.spawn;
child_process.spawn = function patchedSpawn(file, args, opts) {
  try {
    // spawn accepts (file), (file, args), (file, args, opts), or
    // (file, opts). Normalize.
    let normalizedArgs = args;
    let normalizedOpts = opts;
    if (!Array.isArray(args) && args && typeof args === "object") {
      normalizedOpts = args;
      normalizedArgs = undefined;
    }
    traceSpawn(file, normalizedArgs, normalizedOpts);
  } catch (e) {
    warn(`spawn hook error: ${e.message}`);
  }
  return origSpawn.call(this, file, args, opts);
};

const origSpawnSync = child_process.spawnSync;
child_process.spawnSync = function patchedSpawnSync(file, args, opts) {
  try {
    let normalizedArgs = args;
    let normalizedOpts = opts;
    if (!Array.isArray(args) && args && typeof args === "object") {
      normalizedOpts = args;
      normalizedArgs = undefined;
    }
    traceSpawn(file, normalizedArgs, normalizedOpts);
  } catch (e) {
    warn(`spawnSync hook error: ${e.message}`);
  }
  return origSpawnSync.call(this, file, args, opts);
};

// --- child_process.fork (Node-only, IPC) -----------------------------

const origFork = child_process.fork;
child_process.fork = function patchedFork(modulePath, args, opts) {
  try {
    traceArgv(modulePath, args);
  } catch (e) {
    warn(`fork hook error: ${e.message}`);
  }
  return origFork.call(this, modulePath, args, opts);
};
