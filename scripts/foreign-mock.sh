#!/usr/bin/env bash
# foreign-mock.sh — contract test that simulates a foreign agentic
# coding tool (Claude Code / OpenCode / Codex / Gemini CLI) using
# loom's workspace-isolation substrate.
#
# Drives the real /loom-mcp/ HTTP endpoint via JSON-RPC: lease a single
# sub-agent workspace, write a sentinel file in it, push, open a PR
# against main, poll status until merged (or timeout), then release.
# Exit 0 = green; exit 1 = anything went wrong.
#
# Prerequisites:
#   - `ycode serve` running locally
#   - jq, curl available on PATH
#
# Usage:
#   bash scripts/foreign-mock.sh
#   FOREIGN_MOCK_CWD=/path/to/repo bash scripts/foreign-mock.sh

set -euo pipefail

MANIFEST=${YCODE_MANIFEST:-"$HOME/.agents/ycode/manifest.json"}
CWD=${FOREIGN_MOCK_CWD:-"$PWD"}
LABEL=${FOREIGN_MOCK_LABEL:-"foreign-mock-$$"}
TIMEOUT_SECS=${FOREIGN_MOCK_TIMEOUT:-60}

if [ ! -f "$MANIFEST" ]; then
  echo "manifest not found: $MANIFEST" >&2
  echo "is 'ycode serve' running?" >&2
  exit 1
fi

LOOM_URL=$(jq -r '.loom.mcp // .mcp.http.loom // ""' "$MANIFEST")
if [ -z "$LOOM_URL" ] || [ "$LOOM_URL" = "null" ]; then
  echo "no loom endpoint in manifest" >&2
  exit 1
fi
echo "loom endpoint: $LOOM_URL"

# call_tool name args -> prints the tool result text.
call_tool() {
  local name=$1 args=$2
  local req
  req=$(jq -nc --arg n "$name" --argjson a "$args" \
    '{jsonrpc:"2.0",id:1,method:"tools/call",params:{name:$n,arguments:$a}}')
  local resp
  resp=$(curl -fsS -H 'Content-Type: application/json' -d "$req" "$LOOM_URL")
  if [ -z "$resp" ]; then
    echo "tool $name: empty response" >&2
    return 1
  fi
  if jq -e '.error' >/dev/null 2>&1 <<<"$resp"; then
    echo "tool $name failed:" >&2
    jq '.error' <<<"$resp" >&2
    return 1
  fi
  jq -r '.result.content[0].text' <<<"$resp"
}

# 1. Lease.
echo "== loom_lease =="
LEASE_JSON=$(call_tool loom_lease "$(jq -nc --arg cwd "$CWD" --arg lbl "$LABEL" \
  '{cwd:$cwd, sub_agent_label:$lbl}')")
echo "$LEASE_JSON" | jq .
LOOM_ID=$(jq -r '.loom_id' <<<"$LEASE_JSON")
SANDBOX=$(jq -r '.path' <<<"$LEASE_JSON")
if [ -z "$LOOM_ID" ] || [ "$LOOM_ID" = "null" ]; then
  echo "no loom_id returned" >&2; exit 1
fi
echo "lease: $LOOM_ID  sandbox: $SANDBOX"

cleanup() {
  echo "== loom_release =="
  call_tool loom_release "$(jq -nc --arg id "$LOOM_ID" '{loom_id:$id}')" || true
}
trap cleanup EXIT

# 2. Sub-agent's "work" — a sentinel file.
SENTINEL=loom-foreign-mock-$$.txt
echo "writing sentinel $SANDBOX/$SENTINEL"
printf "foreign-mock %s @ %s\n" "$LABEL" "$(date -u +%FT%TZ)" > "$SANDBOX/$SENTINEL"

# 3. Push.
echo "== loom_push =="
call_tool loom_push "$(jq -nc --arg id "$LOOM_ID" '{loom_id:$id, message:"foreign-mock sentinel"}')" | jq .

# 4. Merge.
echo "== loom_merge =="
MERGE_JSON=$(call_tool loom_merge "$(jq -nc --arg id "$LOOM_ID" '{loom_id:$id, title:"foreign-mock"}')")
echo "$MERGE_JSON" | jq .
PR_NUM=$(jq -r '.pr_number' <<<"$MERGE_JSON")
echo "pr #$PR_NUM"

# 5. Poll status until merged or timeout.
echo "== loom_status (polling) =="
deadline=$(( $(date +%s) + TIMEOUT_SECS ))
while [ "$(date +%s)" -lt "$deadline" ]; do
  STATUS_JSON=$(call_tool loom_status "$(jq -nc --arg id "$LOOM_ID" '{loom_id:$id}')")
  state=$(jq -r '.[0].state // "unknown"' <<<"$STATUS_JSON")
  echo "state=$state"
  case "$state" in
    merged) echo "OK: merged"; exit 0 ;;
    ci_failed|conflict) echo "FAIL: $state" >&2; jq . <<<"$STATUS_JSON" >&2; exit 1 ;;
  esac
  sleep 2
done

echo "FAIL: timed out waiting for merge after ${TIMEOUT_SECS}s" >&2
call_tool loom_status "$(jq -nc --arg id "$LOOM_ID" '{loom_id:$id}')" | jq . >&2
exit 1
