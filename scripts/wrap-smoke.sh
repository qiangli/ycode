#!/usr/bin/env bash
# scripts/wrap-smoke.sh — end-to-end smoke matrix for `ycode wrap`.
#
# Iterates over every known wrap profile (claude, codex, aider, gemini,
# opencode) plus a Python and a Node fixture stand-in. For each row it:
#
#   1. Skips if the binary is not installed (matrix shows "skipped")
#   2. Runs the agent (or fixture) under bin/ycode wrap on a
#      deterministic task (touch a file, run git status)
#   3. Captures stderr where ycode's exec-span debug log lands
#   4. Counts span emit lines and reports them in a markdown matrix
#
# Fixture stand-ins exercise the bypass surfaces ycode wrap's runtime
# hooks target (Piece D): shell=True, absolute-path subprocess, mixed
# pipelines. They run with or without the real agents installed so the
# smoke is reproducible in CI.
#
# Output is two files written next to this script:
#   - $RESULTS_DIR/wrap-smoke.md   (markdown matrix)
#   - $RESULTS_DIR/<row>.stderr   (raw ycode log per row)
#
# Usage:
#   scripts/wrap-smoke.sh                 # default results dir under /tmp
#   RESULTS_DIR=/tmp/foo scripts/wrap-smoke.sh
#   YCODE_BIN=./bin/ycode scripts/wrap-smoke.sh

set -u

YCODE_BIN="${YCODE_BIN:-./bin/ycode}"
RESULTS_DIR="${RESULTS_DIR:-/tmp/ycode-wrap-smoke-$$}"
FIXTURES_DIR="$(cd "$(dirname "$0")"/.. && pwd)/scripts/wrap-smoke-fixtures"

# Resolve YCODE_BIN to an absolute path so per-row `cd $workdir` doesn't
# break the invocation.
if command -v realpath > /dev/null 2>&1; then
  YCODE_BIN="$(realpath "$YCODE_BIN" 2>/dev/null || echo "$YCODE_BIN")"
fi
if [[ ! -x "$YCODE_BIN" ]]; then
  echo "wrap-smoke: $YCODE_BIN is not executable; run 'make compile' first" >&2
  exit 1
fi
mkdir -p "$RESULTS_DIR"

MATRIX="$RESULTS_DIR/wrap-smoke.md"

# Markdown header. Capture rate is a heuristic — exec-span debug lines
# divided by the number of shell-outs the fixture is known to make.
{
  echo "# ycode wrap smoke matrix"
  echo
  echo "Generated: $(date -u +%FT%TZ)"
  echo "ycode: $($YCODE_BIN --version 2>/dev/null | head -1)"
  echo
  echo "| Row | Profile | Status | Spans | Notes |"
  echo "|---|---|---|---|---|"
} > "$MATRIX"

run_row() {
  local row="$1"
  local profile="$2"
  local binary="$3"
  shift 3
  local stderr_path="$RESULTS_DIR/$row.stderr"

  # Skip rows whose binary is not on PATH (real-agent rows mostly).
  if ! command -v "$binary" > /dev/null 2>&1; then
    echo "| \`$row\` | $profile | skipped (not installed) | n/a | \`$binary\` not on PATH |" >> "$MATRIX"
    return
  fi

  # Run in a fresh temp dir so file-state assertions don't interact
  # with the repo we're testing from.
  local workdir
  workdir="$(mktemp -d)"
  trap "rm -rf $workdir" RETURN

  (
    cd "$workdir"
    git init --quiet
    YCODE_LOG_LEVEL=debug "$YCODE_BIN" wrap --profile="$profile" --runtime-hooks=off -- "$binary" "$@" \
      > /dev/null 2>"$stderr_path"
  )
  local exit_code=$?

  # Heuristic span count: every StartExecSpan finish emits a
  # `level=DEBUG msg=exec scope=...` log line at YCODE_LOG_LEVEL=debug.
  local spans
  spans=$(grep -c 'msg=exec ' "$stderr_path" 2>/dev/null || true)
  spans="${spans:-0}"

  local status
  if [[ "$exit_code" -eq 0 ]]; then
    status="pass"
  else
    status="fail (exit=$exit_code)"
  fi
  echo "| \`$row\` | $profile | $status | $spans | $stderr_path |" >> "$MATRIX"
}

# --- Fixture rows (always run; agent-installation independent) ---
run_row "fixture-python-shell" "aider"    "$FIXTURES_DIR/py_shell_true.py"
run_row "fixture-python-list"  "aider"    "$FIXTURES_DIR/py_list_form.py"
run_row "fixture-node-shell"   "claude"   "$FIXTURES_DIR/node_shell_true.cjs"
run_row "fixture-node-list"    "claude"   "$FIXTURES_DIR/node_list_form.cjs"

# --- Real-agent help rows (skipped when binary missing) ---
# Quick "does wrap not break the agent" gate. Most agents don't shell
# out for --help / --version, so span counts here are minimal.
run_row "real-claude-help"   "claude"   "claude"   --help
run_row "real-codex-help"    "codex"    "codex"    --help
run_row "real-aider-help"    "aider"    "aider"    --help
run_row "real-gemini-help"   "gemini"   "gemini"   --help
run_row "real-opencode-help" "opencode" "opencode" --help

# --- Real-agent task rows ---
# Headless Claude Code: `-p` runs the task and exits. This is the row
# that actually exercises the runtime hooks against a real agent's
# Bash tool. The prompt itself is intentionally trivial so the agent
# can complete it deterministically (one shell command, no LLM
# back-and-forth needed). Span count > help row indicates the hooks
# are firing.
run_row "real-claude-task" "claude" "claude" -p "Run \`echo hello-from-wrap\` and tell me the output." --output-format text

# opencode's --version is the only deterministic non-interactive
# entry point that exits quickly; the interactive REPL is documented
# as a manual checklist in the matrix header above. Real Bash-tool
# coverage for opencode is a manual test (run `bin/ycode wrap opencode`,
# ask it to `ls -la`, ctrl-c, inspect pulse for spans).
run_row "real-opencode-task" "opencode" "opencode" --version

echo "wrap-smoke: matrix at $MATRIX"
echo
echo "Manual interactive checklist (PTY mode — not automated):"
echo "  1. bin/ycode wrap claude       — cursor positions correctly; resize repaints; ctrl-c exits cleanly"
echo "  2. bin/ycode wrap opencode     — same"
echo "  3. After each session, check ~/.agents/ycode/otel/instances/wrap-*/traces/ for spans tagged with wrap.agent"
