# Per-OS release gate

The per-OS gate proves the published `vX.Y.Z-dev` candidate on native Linux,
macOS, and Windows before the bare `vX.Y.Z` release is allowed. Promotion
copies candidate assets byte-for-byte and never rebuilds them.

## Candidate matrix

| OS gate | Published targets |
|---|---|
| Linux | `linux-amd64`, `linux-arm64` |
| macOS | `darwin-amd64`, `darwin-arm64` |
| Windows | `windows-amd64` |

Every target is a `ycode-<os>-<arch>.tar.gz` archive listed in the release's
single `SHA256SUMS`. Windows uses `tar.gz` deliberately because Bashy supplies
the extractor on every QA host.

## Executable QA target

`DAG.md` defines the `qa` task run by the standing OS pollers:

```bash
YCODE_TEST_VERSION=vX.Y.Z-dev bashy dag qa
```

It uses a temporary cwd-local `.qa/` directory, removes it on exit, is
LLM-free, and uses the Bashy userland to:

1. identify the native OS and architecture;
2. download that exact archive and `SHA256SUMS` from the candidate release;
3. require a matching checksum entry and verify it before extraction;
4. require the expected `ycode`/`ycode.exe` archive member;
5. strictly compile the absolute canonical `examples/agent.yaml` with one
   dummy, never-used provider credential;
6. verify the binary reports the base `vX.Y.Z` version;
7. verify `--help`; and
8. verify `ycode shell --file <fixture> -c pwd` executes in the workspace
   compiled from that fixture.

Success prints `Results: PASS <candidate> <os>/<arch>` and permits the poller
to create `refs/qa/vX.Y.Z/<os>`. `YCODE_REPO` overrides the default
`qiangli/ycode` repository for forks.

The poller wiring and credentials are steward-owned. This repository owns the
download, checksum, extraction, and smoke contract it invokes.

## Promotion gate

`.github/workflows/promote.yml` fixes the required OS set to:

```text
linux darwin windows
```

The workflow refuses promotion when any required QA ref is absent. It also
refuses a missing/non-prerelease candidate, an existing official release, an
invalid checksum, or an incomplete artifact matrix. Only after those checks
does it create the official release from the downloaded candidate files.

This closes the former inconsistency where Windows evidence was mandatory even
though no Windows artifact was built, and where documentation named a `qa`
target that was absent from the task DAG.
