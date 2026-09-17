#!/usr/bin/env bash
# Machine-runnable Sprint 106 YAML harness lifecycle conformance gate.
# Assertions live in Go; this file only groups and sequences them.
set -euo pipefail

root="$(cd "$(dirname "$0")/.." && pwd)"
cd "$root"

count="${YCODE_HARNESS_CONFORMANCE_COUNT:-3}"
if ! [[ "$count" =~ ^[1-9][0-9]*$ ]]; then
	echo "harness-conformance: YCODE_HARNESS_CONFORMANCE_COUNT must be a positive integer" >&2
	exit 2
fi

echo "==> contract, inventory, five-profile goldens, and platform rejection"
go test -race -count="$count" \
	./internal/harness/conformance \
	./internal/harness/cli \
	./internal/harness/spec \
	./internal/harness/pipeline

echo "==> providers, Bashy outcomes, durable state, HITL, compaction, subagents, and telemetry"
go test -race -count="$count" \
	./internal/harness/provider \
	./internal/harness/bashy \
	./internal/harness/event \
	./internal/harness/acp \
	./internal/harness/stages/ioctx \
	./internal/harness/stages/hitl \
	./internal/harness/stages/memory \
	./internal/harness/agent \
	./internal/harness/turn \
	./internal/harness/observe

echo "==> local, HTTP, WebSocket, and injected-NATS frontend parity"
go test -race -count="$count" ./internal/harness/frontend

echo "==> public embedding and ACP adapter"
go test -race -count="$count" ./pkg/ycode
go test -race -count="$count" ./cmd/ycode \
	-run 'Test(ACP|ServeACP|YAMLCLI|CLIDiscovery|HarnessValidateCommand|HarnessSchemaCommand|ConfigIsReadOnlyCompiledHarnessInspection|ModelToolsMemoryAndSkillsReadCompiledHarness|ShellOneShotUsesCompiledBashyPolicyBoundary)'

goos="$(go env GOOS)"
case "$goos" in
	darwin|linux)
		echo "==> PTY lifecycle ($goos: supported)"
		go test -race -count=1 -timeout=60s ./internal/harness/frontend \
			-run '^TestPTYREPLProjectsCanonicalEvents$'
		;;
	windows)
		echo "==> PTY lifecycle (windows: intentionally unsupported; ConPTY is outside the frozen contract)"
		;;
	*)
		echo "==> PTY lifecycle ($goos: intentionally unsupported; no declared adapter)"
		;;
esac

echo "harness-conformance: passed"
