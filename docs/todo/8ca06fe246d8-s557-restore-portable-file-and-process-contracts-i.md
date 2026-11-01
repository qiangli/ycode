---
id: 8ca06fe246d8
kind: bug
title: S557 restore portable file and process contracts in native Windows tests
seq: 63
status: assigned
priority: p1
created: 2026-11-01T17:08:42.435405Z
weave: 1
assignee: claude-opus5.5
sprint: 557
sprint_id: f5ccf563-f5d1-5b94-8be4-9388e51ef86d
sprint_title: 'Bashy v1.0.0 preparation: split private/public development and retire qiangli/dhnt'
---

Diagnose and fix remaining native Windows Ycode failures from the hosted native Windows standalone lane after CRLF orchestration fix, CI isolation a765709 and portable provider6aebcd4. Product bugs include LSP file URIs and concurrent atomic memory index replacement; fixture bugs include slash assumptions, HOME versus USERPROFILE, Unix bash process fixtures, mode expectations. Preserve test coverage, security intent and product contracts; no skips or broad normalizations. Work in isolated weave; parent owns main workspace, all pushes/integration. Use bounded local focused tests/crosscompile only; no full build/suites on dev host. Commit source/story with Sprint557 trailers; do not change go.mod pins unless essential. Do not publish private paths/log details in public docs. Read CLAUDE. Ask manager for hosted test execution; report precise remaining platform risks.

## Delivery notes

Product fixes:

- LSP `fileURI` emits RFC 8089 URIs (`file:///C:/dir/f.go`, percent-encoded)
  and `uriToPath` restores native drive and UNC paths on Windows.
- Memory `Index` serializes its whole-file read-modify-replace, so concurrent
  saves no longer lose `MEMORY.md` entries (reproduced on macOS as well) and
  Windows no longer refuses the replacing rename.
- `agentswitch.BashyPath` looks for `bashy.exe` in the bin-dir fallback on
  Windows.

Fixture fixes: native-separator expectations (fileops walk, vfs roots, git
mount specs), `USERPROFILE` alongside `HOME`, a re-executed test binary in
place of `/bin/sh` exit fixtures, `bashy.exe` fake binaries, and Windows mode
expectations (writable files report 0666; the control-root mode check is
documented as Unix-only and now pinned as such).

Not changed here: failures whose fixtures match `\n`-literal YAML or
frontmatter (spec mutations, golden traces, memory golden files, resume policy
rewrite) belong to the CRLF checkout fix.

Remaining platform risks:

- Hosted native Windows execution of these packages is still required; local
  verification was macOS tests plus Windows vet and test-binary cross-compile.
- Windows privacy of control and memory files rests on profile ACLs, not mode
  bits; nothing verifies the ACL.
- Memory index replacement is serialized per process only; two processes
  sharing one memory directory can still race (and on Windows hit a refused
  rename).
- Memory and persona frontmatter parsing requires LF; a CRLF-authored memory
  file loads with empty metadata.
- `ci-run.sh` mount specs use `src:dst`, which is ambiguous with Windows drive
  letters; the podman runner is not a native Windows path.
